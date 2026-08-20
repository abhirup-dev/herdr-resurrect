package planner

import (
	"testing"

	"github.com/abhirup-dev/herdr-resurrect/internal/manifest"
)

func TestTargetOccupancyCountsOnlyUsableReadyPanes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		snapshot  *manifest.Snapshot
		workspace manifest.Workspace
		live      *manifest.Snapshot
		allowed   Selection
		wantReady int
	}{
		{
			name: "ready panes clamp to missing agents",
			workspace: manifest.Workspace{ID: "captured-work", Label: "work", Tabs: []manifest.Tab{{
				ID: "captured-tab", Label: "tab", Panes: []manifest.Pane{{Key: "agent", Agent: "claude", Name: "agent"}},
			}}},
			live: &manifest.Snapshot{Workspaces: []manifest.Workspace{{
				ID: "live-work", Label: "work", Tabs: []manifest.Tab{{ID: "live-tab", Label: "tab", Panes: []manifest.Pane{
					{Key: "empty-1", PaneID: "p1"},
					{Key: "empty-2", PaneID: "p2"},
					{Key: "empty-3", PaneID: "p3"},
				}}},
			}}},
			wantReady: 1,
		},
		{
			name: "live pane id is reserved",
			workspace: manifest.Workspace{ID: "captured-work", Label: "work", Tabs: []manifest.Tab{{
				ID: "captured-tab", Label: "tab", Panes: []manifest.Pane{
					{Key: "shell", PaneID: "p1"},
					{Key: "agent", Agent: "claude", Name: "agent"},
				},
			}}},
			live: &manifest.Snapshot{Workspaces: []manifest.Workspace{{
				ID: "live-work", Label: "work", Tabs: []manifest.Tab{{
					ID: "live-tab", Label: "tab", Panes: []manifest.Pane{{Key: "p1", PaneID: "p1"}},
				}},
			}}},
			wantReady: 0,
		},
		{
			name: "idle shells cannot cross tabs",
			workspace: manifest.Workspace{ID: "captured-work", Label: "work", Tabs: []manifest.Tab{
				{ID: "captured-a", Label: "A", Panes: []manifest.Pane{{Key: "agent", Agent: "claude", Name: "agent"}}},
				{ID: "captured-b", Label: "B"},
			}},
			live: &manifest.Snapshot{Workspaces: []manifest.Workspace{{
				ID: "live-work", Label: "work", Tabs: []manifest.Tab{
					{ID: "live-a", Label: "A"},
					{ID: "live-b", Label: "B", Panes: []manifest.Pane{{Key: "empty", PaneID: "p2"}}},
				},
			}}},
			wantReady: 0,
		},
		{
			name: "scoped context pane remains reserved",
			workspace: manifest.Workspace{ID: "captured-work", Label: "work", Tabs: []manifest.Tab{{
				ID: "captured-tab", Label: "tab", Panes: []manifest.Pane{
					{Key: "context", PaneID: "p1"},
					{Key: "agent", Agent: "claude", Name: "agent"},
				},
			}}},
			live: &manifest.Snapshot{Workspaces: []manifest.Workspace{{
				ID: "live-work", Label: "work", Tabs: []manifest.Tab{{
					ID: "live-tab", Label: "tab", Panes: []manifest.Pane{{Key: "p1", PaneID: "p1"}},
				}},
			}}},
			allowed:   Selection{"agent": true},
			wantReady: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.snapshot == nil {
				tc.snapshot = &manifest.Snapshot{Workspaces: []manifest.Workspace{tc.workspace}}
			}
			if tc.name == "scoped context pane remains reserved" {
				tc.snapshot.CaptureScope = &manifest.CaptureScope{Panes: []manifest.PaneRef{{
					WorkspaceID: "captured-work", TabID: "captured-tab", PaneKey: "agent",
				}}}
			}
			occupancy := TargetOccupancy(tc.snapshot, tc.workspace, tc.live, tc.allowed)
			if occupancy.ReadyPanes != tc.wantReady {
				t.Fatalf("ready panes = %d, want %d", occupancy.ReadyPanes, tc.wantReady)
			}
			if occupancy.MissingAgents != 1 {
				t.Fatalf("missing agents = %d, want 1", occupancy.MissingAgents)
			}
		})
	}
}
