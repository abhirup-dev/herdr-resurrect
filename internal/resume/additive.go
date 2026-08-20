package resume

import (
	"fmt"
	"time"

	"github.com/abhirup-dev/herdr-resurrect/internal/capture"
	"github.com/abhirup-dev/herdr-resurrect/internal/herdr"
	"github.com/abhirup-dev/herdr-resurrect/internal/manifest"
	"github.com/abhirup-dev/herdr-resurrect/internal/planner"
	"github.com/abhirup-dev/herdr-resurrect/internal/strategy"
)

type additiveState struct {
	workspaceIDs      map[string]string
	tabIDs            map[string]string
	fallbacks         map[string]string
	paneIDs           map[string]string
	liveNames         map[string]bool
	createdWorkspaces map[string]bool
	createdTabs       map[string]bool
}

// ApplyCompiled executes only the additive operations in a compiled planner
// plan. It never closes, replaces, swaps, or moves a live pane.
func ApplyCompiled(plan *planner.Plan) error {
	if plan == nil {
		return fmt.Errorf("nil restoration plan")
	}
	state := &additiveState{
		workspaceIDs:      map[string]string{},
		tabIDs:            map[string]string{},
		fallbacks:         map[string]string{},
		paneIDs:           map[string]string{},
		liveNames:         map[string]bool{},
		createdWorkspaces: map[string]bool{},
		createdTabs:       map[string]bool{},
	}
	live, err := capture.Session(capture.Options{Session: plan.Session})
	if err != nil {
		return fmt.Errorf("live safety capture: %w", err)
	}
	state.indexLive(live)
	for _, operation := range plan.Operations {
		if state.liveNames[paneIdentity(operation.Pane)] {
			continue
		}
		if err := applyAdditiveOperation(plan.Session, state, operation); err != nil {
			return fmt.Errorf("%s: %w", operation.Pane.Key, err)
		}
		state.liveNames[paneIdentity(operation.Pane)] = true
	}

	// Agent terminal titles may update a tab label during launch. Reapply each
	// captured label after all panes settle so a completed restore can match the
	// captured workspace exactly.
	renamed := map[string]bool{}
	for _, operation := range plan.Operations {
		destination := destinationKey(operation.WorkspaceKey, operation.TabKey)
		if renamed[destination] || !state.createdTabs[destination] || operation.TabLabel == "" {
			continue
		}
		tabID := state.tabIDs[destination]
		if tabID == "" {
			return fmt.Errorf("%s: restored tab disappeared before label finalization", operation.TabKey)
		}
		if _, err := herdr.Run(append(herdr.SessionScope(plan.Session), "tab", "rename", tabID, operation.TabLabel)...); err != nil {
			return fmt.Errorf("%s: finalize tab label: %w", operation.TabKey, err)
		}
		renamed[destination] = true
	}
	return nil
}

func (state *additiveState) indexLive(live *manifest.Snapshot) {
	for _, workspace := range live.Workspaces {
		workspaceKey := workspace.ID
		if workspace.Label != "" {
			workspaceKey = workspace.Label
		}
		state.workspaceIDs[workspaceKey] = workspace.ID
		for _, tab := range workspace.Tabs {
			tabKey := tab.ID
			if tab.Label != "" {
				tabKey = tab.Label
			}
			key := destinationKey(workspaceKey, tabKey)
			state.tabIDs[key] = tab.ID
			if len(tab.Panes) > 0 {
				state.fallbacks[key] = tab.Panes[0].PaneID
			}
			for _, pane := range tab.Panes {
				state.liveNames[paneIdentity(pane)] = true
				state.paneIDs[pane.Key] = pane.PaneID
			}
		}
	}
}

func validateCompiledDestination(state *additiveState, operation planner.Operation, destination string) error {
	currentWorkspaceID := state.workspaceIDs[operation.WorkspaceKey]
	switch {
	case operation.WorkspaceID == "" && currentWorkspaceID != "" && !state.createdWorkspaces[operation.WorkspaceKey]:
		return fmt.Errorf("stale plan: workspace %q appeared after confirmation", operation.WorkspaceKey)
	case operation.WorkspaceID != "" && currentWorkspaceID != operation.WorkspaceID:
		return fmt.Errorf("stale plan: workspace %q changed after confirmation", operation.WorkspaceKey)
	}
	currentTabID := state.tabIDs[destination]
	switch {
	case operation.TabID == "" && currentTabID != "" && !state.createdTabs[destination]:
		return fmt.Errorf("stale plan: tab %q appeared after confirmation", operation.TabKey)
	case operation.TabID != "" && currentTabID != operation.TabID:
		return fmt.Errorf("stale plan: tab %q changed after confirmation", operation.TabKey)
	}
	return nil
}

func applyAdditiveOperation(session string, state *additiveState, operation planner.Operation) error {
	destination := destinationKey(operation.WorkspaceKey, operation.TabKey)
	if err := validateCompiledDestination(state, operation, destination); err != nil {
		return err
	}
	workspaceID := state.workspaceIDs[operation.WorkspaceKey]
	if workspaceID == "" {
		created, err := createWorkspace(session, operation)
		if err != nil {
			return err
		}
		workspaceID = created.workspaceID
		state.workspaceIDs[operation.WorkspaceKey] = created.workspaceID
		state.createdWorkspaces[operation.WorkspaceKey] = true
		state.tabIDs[destination] = created.tabID
		state.createdTabs[destination] = true
		state.fallbacks[destination] = created.paneID
		if err := launchInPane(session, created.paneID, operation.Pane); err != nil {
			return err
		}
		state.paneIDs[operation.Pane.Key] = created.paneID
		return nil
	}

	tabID := state.tabIDs[destination]
	if tabID == "" {
		created, err := createTab(session, workspaceID, operation)
		if err != nil {
			return err
		}
		state.tabIDs[destination] = created.tabID
		state.createdTabs[destination] = true
		state.fallbacks[destination] = created.paneID
		if err := launchInPane(session, created.paneID, operation.Pane); err != nil {
			return err
		}
		state.paneIDs[operation.Pane.Key] = created.paneID
		return nil
	}

	anchor := state.paneIDs[operation.Placement.AnchorKey]
	if anchor == "" {
		anchor = state.fallbacks[destination]
	}
	if anchor == "" {
		anchor = operation.FallbackPaneID
	}
	if anchor == "" {
		return fmt.Errorf("no additive anchor for %s / %s", operation.WorkspaceKey, operation.TabKey)
	}
	paneID, err := splitAdditivePane(session, anchor, operation)
	if err != nil {
		return err
	}
	if err := launchInPane(session, paneID, operation.Pane); err != nil {
		return err
	}
	state.paneIDs[operation.Pane.Key] = paneID
	return nil
}

type createdDestination struct {
	workspaceID string
	tabID       string
	paneID      string
}

func createWorkspace(session string, operation planner.Operation) (createdDestination, error) {
	cwd := operation.Pane.Cwd
	if cwd == "" {
		cwd = operation.WorkspaceCwd
	}
	label := operation.WorkspaceLabel
	if label == "" {
		label = operation.WorkspaceKey
	}
	args := append(herdr.SessionScope(session), "workspace", "create", "--cwd", cwd, "--label", label, "--no-focus")
	args = appendReplayEnv(args, operation.Pane.Env)
	var result struct {
		Workspace struct {
			WorkspaceID string `json:"workspace_id"`
		} `json:"workspace"`
		Tab struct {
			TabID string `json:"tab_id"`
		} `json:"tab"`
		RootPane struct {
			PaneID string `json:"pane_id"`
		} `json:"root_pane"`
	}
	if err := herdr.RunInto(&result, args...); err != nil {
		return createdDestination{}, fmt.Errorf("workspace create: %w", err)
	}
	tabLabel := operation.TabLabel
	if tabLabel == "" {
		tabLabel = operation.TabKey
	}
	if _, err := herdr.Run(append(herdr.SessionScope(session), "tab", "rename", result.Tab.TabID, tabLabel)...); err != nil {
		return createdDestination{}, fmt.Errorf("rename root tab: %w", err)
	}
	return createdDestination{workspaceID: result.Workspace.WorkspaceID, tabID: result.Tab.TabID, paneID: result.RootPane.PaneID}, nil
}

func createTab(session, workspaceID string, operation planner.Operation) (createdDestination, error) {
	label := operation.TabLabel
	if label == "" {
		label = operation.TabKey
	}
	cwd := operation.Pane.Cwd
	if cwd == "" {
		cwd = operation.WorkspaceCwd
	}
	args := append(herdr.SessionScope(session), "tab", "create", "--workspace", workspaceID,
		"--cwd", cwd, "--label", label, "--no-focus")
	args = appendReplayEnv(args, operation.Pane.Env)
	var result struct {
		Tab struct {
			TabID string `json:"tab_id"`
		} `json:"tab"`
		RootPane struct {
			PaneID string `json:"pane_id"`
		} `json:"root_pane"`
	}
	if err := herdr.RunInto(&result, args...); err != nil {
		return createdDestination{}, fmt.Errorf("tab create: %w", err)
	}
	return createdDestination{workspaceID: workspaceID, tabID: result.Tab.TabID, paneID: result.RootPane.PaneID}, nil
}

func splitAdditivePane(session, anchor string, operation planner.Operation) (string, error) {
	direction := operation.Placement.Direction
	if direction != "down" {
		direction = "right"
	}
	ratio := operation.Placement.Ratio
	if ratio <= 0 || ratio >= 1 {
		ratio = 0.5
	}
	cwd := operation.Pane.Cwd
	if cwd == "" {
		cwd = operation.WorkspaceCwd
	}
	args := append(herdr.SessionScope(session), "pane", "split", "--pane", anchor,
		"--direction", direction, "--ratio", fmt.Sprintf("%.4f", ratio),
		"--cwd", cwd, "--no-focus")
	args = appendReplayEnv(args, operation.Pane.Env)
	var result struct {
		Pane struct {
			PaneID string `json:"pane_id"`
		} `json:"pane"`
	}
	if err := herdr.RunInto(&result, args...); err != nil {
		return "", fmt.Errorf("split: %w", err)
	}
	return result.Pane.PaneID, nil
}

func appendReplayEnv(args []string, env map[string]string) []string {
	for key, value := range strategy.ReplayEnv(env) {
		args = append(args, "--env", key+"="+value)
	}
	return args
}

func launchInPane(session, paneID string, pane manifest.Pane) error {
	if pane.Agent == "" {
		return nil
	}
	pp := PanePlan{Manifest: pane, Action: Resurrect, Fresh: !strategy.SessionOnDisk(pane.Agent, pane.SID, pane.Env)}
	command := strategy.LaunchCmdline(pane.Argv, pane.Cmdline, pane.Agent, resumeArgsFor(pp))
	if _, err := herdr.Run(append(herdr.SessionScope(session), "pane", "run", paneID, command)...); err != nil {
		return fmt.Errorf("run: %w", err)
	}
	for attempt := 0; attempt < 45; attempt++ {
		var probe struct {
			Agent struct {
				Agent string `json:"agent"`
			} `json:"agent"`
		}
		if err := herdr.RunInto(&probe, append(herdr.SessionScope(session), "agent", "get", paneID)...); err == nil && probe.Agent.Agent != "" {
			return renameRestoredAgent(session, paneID, pane.Name)
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("agent not detected in %s within 45s", paneID)
}

func renameRestoredAgent(session, paneID, name string) error {
	if name == "" {
		return nil
	}
	for attempt := 0; attempt <= 5; attempt++ {
		candidate := name
		if attempt > 0 {
			candidate = fmt.Sprintf("%s-%d", name, attempt)
		}
		if _, err := herdr.Run(append(herdr.SessionScope(session), "agent", "rename", paneID, candidate)...); err == nil {
			return nil
		} else if attempt == 5 {
			return fmt.Errorf("rename: %w", err)
		}
	}
	return nil
}

func destinationKey(workspaceKey, tabKey string) string {
	return workspaceKey + "\x00" + tabKey
}

func paneIdentity(pane manifest.Pane) string {
	if pane.Name != "" {
		return "name:" + pane.Name
	}
	if pane.SID != "" {
		return "sid:" + pane.Agent + ":" + pane.SID
	}
	return "pane:" + pane.Key
}
