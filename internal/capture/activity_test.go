package capture

import "testing"

func TestLiveActivityCountsAgentsAndPopulatedWorkspaces(t *testing.T) {
	agents, workspaces := liveActivity([]agentEntry{
		{Name: "named", Agent: "claude", PaneID: "w1:p1"},
		{Agent: "codex", PaneID: "w1:p2"},
		{Name: "other", Agent: "claude", PaneID: "w2:p1"},
		{PaneID: "w3:p1"},
	})
	if agents != 3 || workspaces != 2 {
		t.Fatalf("activity = %d agents/%d workspaces, want 3/2", agents, workspaces)
	}
	if agents, workspaces := liveActivity(nil); agents != 0 || workspaces != 0 {
		t.Fatalf("empty activity = %d/%d, want 0/0", agents, workspaces)
	}
}
