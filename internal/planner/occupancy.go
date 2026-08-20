package planner

import (
	"github.com/abhirup-dev/herdr-resurrect/internal/manifest"
	"github.com/abhirup-dev/herdr-resurrect/internal/strategy"
)

// Occupancy summarizes captured occupants against their live destination.
type Occupancy struct {
	Agents           int
	Shells           int
	Tabs             int
	Panes            int
	TranscriptSize   int64
	WorkspacePresent bool
	LiveAgents       int
	MissingAgents    int
	LiveShells       int
	MissingShells    int
	ReadyPanes       int
}

// TargetOccupancy classifies captured panes and counts idle destination shells
// that can host missing agents without changing existing live occupants.
func TargetOccupancy(snapshot *manifest.Snapshot, workspace manifest.Workspace, live *manifest.Snapshot, allowed Selection) Occupancy {
	occupancy := Occupancy{Tabs: len(workspace.Tabs)}
	states := Analyze(workspace, live)
	workspaceKey := manifestWorkspaceKey(workspace)
	workspaceByKey, tabByWorkspace := liveTopologyIndex(live)
	_, occupancy.WorkspacePresent = workspaceByKey[workspaceKey]

	for _, tab := range workspace.Tabs {
		occupancy.Panes += len(tab.Panes)
		missingAgents := Selection{}
		for _, pane := range tab.Panes {
			if snapshot == nil || !snapshot.CapturesPane(workspace.ID, tab.ID, pane.Key) {
				continue
			}
			if pane.Agent == "" {
				occupancy.Shells++
			} else {
				occupancy.Agents++
				occupancy.TranscriptSize += strategy.TranscriptSize(pane.Agent, pane.SID, pane.Env)
			}
			if live == nil || (allowed != nil && !allowed[pane.Key]) {
				continue
			}
			state := states[pane.Key]
			if state.Availability == Live {
				if pane.Agent == "" {
					occupancy.LiveShells++
				} else {
					occupancy.LiveAgents++
				}
			} else if pane.Agent == "" {
				occupancy.MissingShells++
			} else {
				occupancy.MissingAgents++
				missingAgents[pane.Key] = true
			}
		}
		if live == nil || len(missingAgents) == 0 {
			continue
		}
		tabKey, tabID, _ := compiledTabDestination(workspaceKey, tab, states, tabByWorkspace)
		if tabID == "" {
			continue
		}
		assignments := reusableDestinationPanes(
			tab, missingAgents, states, tabByWorkspace[workspaceKey][tabKey],
		)
		occupancy.ReadyPanes += len(assignments)
	}
	return occupancy
}
