package planner

import (
	"fmt"
	"sort"
	"strings"

	"github.com/abhirupdas/herdr-archive/internal/manifest"
	"github.com/abhirupdas/herdr-archive/internal/strategy"
)

// Availability describes whether a captured pane can be restored additively.
type Availability int

const (
	Restorable Availability = iota
	Live
)

// LivePane locates a captured identity in the current session topology.
type LivePane struct {
	Pane         manifest.Pane
	WorkspaceID  string
	WorkspaceKey string
	TabID        string
	TabKey       string
}

// PaneState is the live-aware state of one captured pane.
type PaneState struct {
	Availability Availability
	Live         *LivePane
	Drift        []string
}

// Target is one selected captured workspace and its pane subset.
type Target struct {
	WorkspaceKey string
	SnapshotName string
	SnapshotPath string
	Workspace    manifest.Workspace
	Selected     Selection
}

// GeometryMode tells the executor how faithfully it can place a pane.
type GeometryMode string

const (
	GeometryExact      GeometryMode = "exact"
	GeometryBestEffort GeometryMode = "best-effort"
)

// Placement is the captured split to use when adding a pane.
type Placement struct {
	Mode      GeometryMode
	AnchorKey string
	Direction string
	Ratio     float64
}

// Description is the shared CLI/TUI rendering of a compiled placement.
func (placement Placement) Description() string {
	if placement.AnchorKey == "" {
		return string(placement.Mode) + " geometry · root"
	}
	return fmt.Sprintf("%s geometry · %s of %s · %.0f%%", placement.Mode,
		placement.Direction, placement.AnchorKey, placement.Ratio*100)
}

// Operation is one additive pane restoration. It is the unit shown by dry-run
// output and consumed unchanged by the executor.
type Operation struct {
	WorkspaceKey     string
	WorkspaceID      string
	WorkspaceLabel   string
	WorkspaceCwd     string
	TabKey           string
	TabID            string
	TabLabel         string
	FallbackPaneID   string
	Pane             manifest.Pane
	Placement        Placement
	SnapshotName     string
	CapturedTab      manifest.Tab
	CapturedSelected Selection
}

// Change annotates a node in the projected topology.
type Change string

const (
	Unchanged Change = "unchanged"
	Added     Change = "added"
	Expanded  Change = "expanded"
)

// Topology is a display-oriented session tree.
type Topology struct {
	Workspaces []TopologyWorkspace
}

type TopologyWorkspace struct {
	ID     string
	Key    string
	Change Change
	Tabs   []TopologyTab
}

type TopologyTab struct {
	ID     string
	Key    string
	Change Change
	Panes  []TopologyPane
}

type TopologyPane struct {
	Key    string
	Pane   manifest.Pane
	Change Change
}

// Plan is a non-destructive restoration plan. Before, After, Operations, and
// execution all derive from the same compiled value.
type Plan struct {
	Session     string
	Before      Topology
	After       Topology
	Operations  []Operation
	Diagnostics []string
}

// Analyze reports live/restorable state for every pane in a captured target.
func Analyze(workspace manifest.Workspace, live *manifest.Snapshot) map[string]PaneState {
	byName, byPane := livePaneIndex(live)
	states := map[string]PaneState{}
	for _, tab := range workspace.Tabs {
		for _, pane := range tab.Panes {
			state := PaneState{Availability: Restorable}
			lp, ok := byName[paneIdentity(pane)]
			if !ok && pane.PaneID != "" {
				lp, ok = byPane[pane.PaneID]
			}
			if ok {
				state.Availability = Live
				copy := lp
				state.Live = &copy
				state.Drift = environmentDrift(pane.Env, lp.Pane.Env)
			}
			states[pane.Key] = state
		}
	}
	return states
}

// RestorableCount reports selected and available panes in a workspace target.
func RestorableCount(workspace manifest.Workspace, selection Selection, states map[string]PaneState) (selected, total, live int) {
	for _, key := range PaneKeys(workspace) {
		if states[key].Availability == Live {
			live++
			continue
		}
		total++
		if selection[key] {
			selected++
		}
	}
	return
}

// SelectRestorable selects only panes that are absent from the live session.
func SelectRestorable(workspace manifest.Workspace, states map[string]PaneState) Selection {
	selection := Selection{}
	for _, key := range PaneKeys(workspace) {
		if states[key].Availability != Live {
			selection[key] = true
		}
	}
	return selection
}

// MapRestorable carries matching selected identities and drops panes that are
// absent from the new target or already live.
func MapRestorable(previous Selection, workspace manifest.Workspace, states map[string]PaneState) Selection {
	selection := MapMatching(previous, workspace)
	PruneLive(selection, states)
	return selection
}

// PruneLive removes identities that additive execution must leave untouched.
func PruneLive(selection Selection, states map[string]PaneState) {
	for key, state := range states {
		if state.Availability == Live {
			delete(selection, key)
		}
	}
}

// ToggleRestorableTab toggles only missing panes beneath a tab.
func ToggleRestorableTab(tab manifest.Tab, selection Selection, states map[string]PaneState) {
	selected, total := 0, 0
	for _, pane := range tab.Panes {
		if states[pane.Key].Availability == Live {
			continue
		}
		total++
		if selection[pane.Key] {
			selected++
		}
	}
	selectAll := selected != total
	for _, pane := range tab.Panes {
		if states[pane.Key].Availability == Live {
			delete(selection, pane.Key)
			continue
		}
		if selectAll {
			selection[pane.Key] = true
		} else {
			delete(selection, pane.Key)
		}
	}
}

// Compile builds an additive plan against one live session snapshot.
func Compile(session string, targets []Target, live *manifest.Snapshot) *Plan {
	plan := &Plan{Session: session, Before: topologyFromSnapshot(live)}
	workspaceByKey, tabByWorkspace := liveTopologyIndex(live)
	liveByName, liveByPane := livePaneIndex(live)

	for _, target := range targets {
		states := Analyze(target.Workspace, live)
		workspaceID := ""
		if workspace, ok := workspaceByKey[target.WorkspaceKey]; ok {
			workspaceID = workspace.ID
		}
		for _, tab := range target.Workspace.Tabs {
			tabKey := manifestTabKey(tab)
			tabID, fallbackPaneID := "", ""
			if liveTab, ok := tabByWorkspace[target.WorkspaceKey][tabKey]; ok {
				tabID = liveTab.ID
				if len(liveTab.Panes) > 0 {
					fallbackPaneID = liveTab.Panes[0].PaneID
				}
			}
			selected, included := Selection{}, Selection{}
			paneByKey := map[string]manifest.Pane{}
			for _, pane := range tab.Panes {
				paneByKey[pane.Key] = pane
				state := states[pane.Key]
				if target.Selected[pane.Key] && state.Availability != Live {
					selected[pane.Key] = true
					included[pane.Key] = true
				}
				if state.Availability == Live && state.Live != nil &&
					state.Live.WorkspaceKey == target.WorkspaceKey && state.Live.TabKey == tabKey {
					included[pane.Key] = true
				}
			}
			mode := GeometryExact
			if tabID != "" {
				mode = GeometryBestEffort
			}
			order, placements := layoutPlacements(tab, included, mode)
			for _, paneKey := range order {
				if !selected[paneKey] {
					continue
				}
				pane := paneByKey[paneKey]
				plan.Operations = append(plan.Operations, Operation{
					WorkspaceKey:     target.WorkspaceKey,
					WorkspaceID:      workspaceID,
					WorkspaceLabel:   target.Workspace.Label,
					WorkspaceCwd:     target.Workspace.Cwd,
					TabKey:           tabKey,
					TabID:            tabID,
					TabLabel:         tab.Label,
					FallbackPaneID:   fallbackPaneID,
					Pane:             pane,
					Placement:        placements[paneKey],
					SnapshotName:     target.SnapshotName,
					CapturedTab:      tab,
					CapturedSelected: selected,
				})
			}
		}
	}

	for _, target := range targets {
		for _, tab := range target.Workspace.Tabs {
			for _, pane := range tab.Panes {
				if !target.Selected[pane.Key] {
					continue
				}
				lp, ok := liveByName[paneIdentity(pane)]
				if !ok && pane.PaneID != "" {
					lp, ok = liveByPane[pane.PaneID]
				}
				if ok {
					detail := "already live; left untouched"
					if drift := environmentDrift(pane.Env, lp.Pane.Env); len(drift) > 0 {
						detail += "; environment drift: " + strings.Join(drift, ", ")
					}
					plan.Diagnostics = append(plan.Diagnostics, fmt.Sprintf("%s: %s", pane.Key, detail))
				}
			}
		}
	}
	plan.After = projectedTopology(plan.Before, plan.Operations)
	return plan
}

func paneIdentity(pane manifest.Pane) string {
	if pane.Name != "" {
		return pane.Name
	}
	return pane.Key
}

func manifestWorkspaceKey(workspace manifest.Workspace) string {
	if workspace.Label != "" {
		return workspace.Label
	}
	return workspace.ID
}

func manifestTabKey(tab manifest.Tab) string {
	if tab.Label != "" {
		return tab.Label
	}
	return tab.ID
}

func livePaneIndex(live *manifest.Snapshot) (map[string]LivePane, map[string]LivePane) {
	byName, byPane := map[string]LivePane{}, map[string]LivePane{}
	if live == nil {
		return byName, byPane
	}
	for _, workspace := range live.Workspaces {
		workspaceKey := manifestWorkspaceKey(workspace)
		for _, tab := range workspace.Tabs {
			tabKey := manifestTabKey(tab)
			for _, pane := range tab.Panes {
				lp := LivePane{Pane: pane, WorkspaceID: workspace.ID, WorkspaceKey: workspaceKey, TabID: tab.ID, TabKey: tabKey}
				byName[paneIdentity(pane)] = lp
				if pane.PaneID != "" {
					byPane[pane.PaneID] = lp
				}
			}
		}
	}
	return byName, byPane
}

func liveTopologyIndex(live *manifest.Snapshot) (map[string]manifest.Workspace, map[string]map[string]manifest.Tab) {
	workspaces := map[string]manifest.Workspace{}
	tabs := map[string]map[string]manifest.Tab{}
	if live == nil {
		return workspaces, tabs
	}
	for _, workspace := range live.Workspaces {
		workspaceKey := manifestWorkspaceKey(workspace)
		workspaces[workspaceKey] = workspace
		tabs[workspaceKey] = map[string]manifest.Tab{}
		for _, tab := range workspace.Tabs {
			tabs[workspaceKey][manifestTabKey(tab)] = tab
		}
	}
	return workspaces, tabs
}

func environmentDrift(captured, live map[string]string) []string {
	want := strategy.ReplayEnv(captured)
	have := strategy.ReplayEnv(live)
	var drift []string
	for key, value := range want {
		if current, ok := have[key]; !ok {
			drift = append(drift, key+" missing")
		} else if current != value {
			drift = append(drift, key+" changed")
		}
	}
	sort.Strings(drift)
	return drift
}

func topologyFromSnapshot(snapshot *manifest.Snapshot) Topology {
	var topology Topology
	if snapshot == nil {
		return topology
	}
	for _, workspace := range snapshot.Workspaces {
		wn := TopologyWorkspace{ID: workspace.ID, Key: manifestWorkspaceKey(workspace), Change: Unchanged}
		for _, tab := range workspace.Tabs {
			tn := TopologyTab{ID: tab.ID, Key: manifestTabKey(tab), Change: Unchanged}
			for _, pane := range tab.Panes {
				tn.Panes = append(tn.Panes, TopologyPane{Key: pane.Key, Pane: pane, Change: Unchanged})
			}
			wn.Tabs = append(wn.Tabs, tn)
		}
		topology.Workspaces = append(topology.Workspaces, wn)
	}
	return topology
}

func projectedTopology(before Topology, operations []Operation) Topology {
	after := cloneTopology(before)
	for _, operation := range operations {
		workspaceIndex := -1
		for i := range after.Workspaces {
			if after.Workspaces[i].Key == operation.WorkspaceKey || (operation.WorkspaceID != "" && after.Workspaces[i].ID == operation.WorkspaceID) {
				workspaceIndex = i
				break
			}
		}
		if workspaceIndex < 0 {
			after.Workspaces = append(after.Workspaces, TopologyWorkspace{Key: operation.WorkspaceKey, Change: Added})
			workspaceIndex = len(after.Workspaces) - 1
		} else if after.Workspaces[workspaceIndex].Change == Unchanged {
			after.Workspaces[workspaceIndex].Change = Expanded
		}
		workspace := &after.Workspaces[workspaceIndex]
		tabIndex := -1
		for i := range workspace.Tabs {
			if workspace.Tabs[i].Key == operation.TabKey || (operation.TabID != "" && workspace.Tabs[i].ID == operation.TabID) {
				tabIndex = i
				break
			}
		}
		if tabIndex < 0 {
			workspace.Tabs = append(workspace.Tabs, TopologyTab{Key: operation.TabKey, Change: Added})
			tabIndex = len(workspace.Tabs) - 1
		} else if workspace.Tabs[tabIndex].Change == Unchanged {
			workspace.Tabs[tabIndex].Change = Expanded
		}
		workspace.Tabs[tabIndex].Panes = append(workspace.Tabs[tabIndex].Panes,
			TopologyPane{Key: operation.Pane.Key, Pane: operation.Pane, Change: Added})
	}
	return after
}

func cloneTopology(topology Topology) Topology {
	clone := Topology{Workspaces: make([]TopologyWorkspace, len(topology.Workspaces))}
	for i, workspace := range topology.Workspaces {
		clone.Workspaces[i] = workspace
		clone.Workspaces[i].Tabs = make([]TopologyTab, len(workspace.Tabs))
		for j, tab := range workspace.Tabs {
			clone.Workspaces[i].Tabs[j] = tab
			clone.Workspaces[i].Tabs[j].Panes = append([]TopologyPane(nil), tab.Panes...)
		}
	}
	return clone
}
