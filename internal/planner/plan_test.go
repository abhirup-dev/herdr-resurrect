package planner

import (
	"testing"

	"github.com/abhirup-dev/herdr-resurrect/internal/manifest"
)

func TestCompileAddsMissingPaneBesideLiveCapturedSibling(t *testing.T) {
	captured := manifest.Workspace{
		ID:    "w3",
		Label: "recovery",
		Tabs: []manifest.Tab{{
			ID:    "w3:t1",
			Label: "archive implementation · review",
			Panes: []manifest.Pane{
				{Key: "w3:p2", PaneID: "w3:p2", Agent: "claude", SID: "implementation-session"},
				{Key: "w3:pX", PaneID: "w3:pX", Agent: "claude", SID: "review-session"},
			},
			Layout: &manifest.Layout{
				Panes: []manifest.LayoutPane{
					{PaneID: "w3:p2", Rect: manifest.Rect{X: 0, Y: 0, Width: 50, Height: 100}},
					{PaneID: "w3:pX", Rect: manifest.Rect{X: 50, Y: 0, Width: 50, Height: 100}},
				},
				Splits: []manifest.Split{{
					Direction: "right", Ratio: 0.5,
					Rect: manifest.Rect{X: 0, Y: 0, Width: 100, Height: 100},
				}},
			},
		}},
	}
	live := &manifest.Snapshot{Workspaces: []manifest.Workspace{{
		ID: "wB", Label: "recovery",
		Tabs: []manifest.Tab{{
			ID: "wB:t1", Label: "◐ archive implementation",
			Panes: []manifest.Pane{{
				Key: "wB:p1", PaneID: "wB:p1", Agent: "claude", SID: "implementation-session",
			}},
		}},
	}}}

	plan := Compile("default", []Target{{
		WorkspaceKey: "recovery", Workspace: captured,
		Selected: Selection{"w3:p2": true, "w3:pX": true},
	}}, live)

	if len(plan.Operations) != 1 {
		t.Fatalf("operations = %d, want just the missing review pane: %#v", len(plan.Operations), plan.Operations)
	}
	op := plan.Operations[0]
	if op.Pane.Key != "w3:pX" {
		t.Errorf("restored pane = %q, want w3:pX", op.Pane.Key)
	}
	if op.WorkspaceID != "wB" || op.TabID != "wB:t1" {
		t.Errorf("destination = %s/%s, want wB/wB:t1", op.WorkspaceID, op.TabID)
	}
	if op.TabKey != "◐ archive implementation" {
		t.Errorf("tab key = %q, want live tab label", op.TabKey)
	}
	if op.Placement.AnchorKey != "wB:p1" {
		t.Errorf("anchor = %q, want live implementation pane", op.Placement.AnchorKey)
	}
}

func TestCompileDoesNotChooseBetweenConflictingLiveTabs(t *testing.T) {
	captured := manifest.Workspace{Label: "recovery", Tabs: []manifest.Tab{{
		Label: "captured", Panes: []manifest.Pane{
			{Key: "first", Agent: "claude", SID: "first-session"},
			{Key: "second", Agent: "claude", SID: "second-session"},
			{Key: "missing", Agent: "claude", SID: "missing-session"},
		},
	}}}
	live := &manifest.Snapshot{Workspaces: []manifest.Workspace{{
		ID: "wB", Label: "recovery", Tabs: []manifest.Tab{
			{ID: "wB:t1", Label: "one", Panes: []manifest.Pane{{Key: "live-one", Agent: "claude", SID: "first-session"}}},
			{ID: "wB:t2", Label: "two", Panes: []manifest.Pane{{Key: "live-two", Agent: "claude", SID: "second-session"}}},
		},
	}}}

	plan := Compile("default", []Target{{
		WorkspaceKey: "recovery", Workspace: captured,
		Selected: Selection{"first": true, "second": true, "missing": true},
	}}, live)
	if len(plan.Operations) != 1 {
		t.Fatalf("operations = %#v, want just the missing pane", plan.Operations)
	}
	if plan.Operations[0].TabID != "" {
		t.Errorf("conflicting live tabs resolved to %q; want a new tab instead", plan.Operations[0].TabID)
	}
}
