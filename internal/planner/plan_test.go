package planner

import (
	"slices"
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

func TestAnalyzeTreatsVacatedAgentPaneAsRestorable(t *testing.T) {
	captured := manifest.Workspace{Label: "work", Tabs: []manifest.Tab{{
		Label: "implementation", Panes: []manifest.Pane{{
			Key: "agent", PaneID: "w1:p1", Agent: "claude", Name: "agent", SID: "session",
		}},
	}}}
	live := &manifest.Snapshot{Workspaces: []manifest.Workspace{{
		ID: "w1", Label: "work", Tabs: []manifest.Tab{{
			ID: "w1:t1", Label: "implementation", Panes: []manifest.Pane{{
				Key: "w1:p1", PaneID: "w1:p1",
			}},
		}},
	}}}

	state := Analyze(captured, live)["agent"]
	if state.Availability != Restorable {
		t.Fatalf("availability = %v, want restorable for a vacated agent pane", state.Availability)
	}
	if state.Live != nil {
		t.Fatalf("live occupant = %#v, want nil for an empty shell", state.Live)
	}
}

func TestCompileReusesVacatedPaneForCapturedAgent(t *testing.T) {
	captured := manifest.Workspace{Label: "work", Tabs: []manifest.Tab{{
		Label: "implementation", Panes: []manifest.Pane{{
			Key: "agent", PaneID: "w1:p1", Agent: "claude", Name: "agent", SID: "session",
		}},
	}}}
	live := &manifest.Snapshot{Workspaces: []manifest.Workspace{{
		ID: "w1", Label: "work", Tabs: []manifest.Tab{{
			ID: "w1:t1", Label: "implementation", Panes: []manifest.Pane{{
				Key: "w1:p1", PaneID: "w1:p1",
			}},
		}},
	}}}

	plan := Compile("work", []Target{{
		WorkspaceKey: "work", Workspace: captured, Selected: Selection{"agent": true},
	}}, live)
	if len(plan.Operations) != 1 {
		t.Fatalf("operations = %#v, want one agent launch", plan.Operations)
	}
	op := plan.Operations[0]
	if op.WorkspaceID != "w1" || op.TabID != "w1:t1" {
		t.Fatalf("destination = %s/%s, want existing workspace and tab", op.WorkspaceID, op.TabID)
	}
	if op.DestinationPaneID != "w1:p1" {
		t.Fatalf("destination pane = %q, want vacated w1:p1", op.DestinationPaneID)
	}
	panes := plan.After.Workspaces[0].Tabs[0].Panes
	if len(panes) != 1 || panes[0].Key != "agent" {
		t.Fatalf("projected panes = %#v, want the existing shell replaced by agent", panes)
	}
}

func TestCompileDoesNotReusePaneRunningForegroundCommand(t *testing.T) {
	captured := manifest.Workspace{Label: "work", Tabs: []manifest.Tab{{
		Label: "implementation", Panes: []manifest.Pane{{
			Key: "agent", PaneID: "captured", Agent: "claude", Name: "agent", SID: "session",
		}},
	}}}
	live := &manifest.Snapshot{Workspaces: []manifest.Workspace{{
		ID: "w1", Label: "work", Tabs: []manifest.Tab{{
			ID: "w1:t1", Label: "implementation", Panes: []manifest.Pane{{
				Key: "w1:p1", PaneID: "w1:p1", Shell: true, Argv: []string{"vim"}, Cmdline: "vim notes.md",
			}},
		}},
	}}}

	plan := Compile("work", []Target{{
		WorkspaceKey: "work", Workspace: captured, Selected: Selection{"agent": true},
	}}, live)
	if len(plan.Operations) != 1 {
		t.Fatalf("operations = %#v, want one additive launch", plan.Operations)
	}
	if got := plan.Operations[0].DestinationPaneID; got != "" {
		t.Fatalf("foreground command pane selected as reusable destination: %q", got)
	}
}

func TestEnvironmentDrift(t *testing.T) {
	for _, tc := range []struct {
		name        string
		captured    map[string]string
		live        map[string]string
		wantMissing []string
		wantChanged []string
	}{
		{
			name:        "missing replayable value",
			captured:    map[string]string{"PROJECT_MODE": "review"},
			live:        map[string]string{},
			wantMissing: []string{"PROJECT_MODE"},
		},
		{
			name:        "changed provider value",
			captured:    map[string]string{"ANTHROPIC_BASE_URL": "https://captured"},
			live:        map[string]string{"ANTHROPIC_BASE_URL": "https://live"},
			wantChanged: []string{"ANTHROPIC_BASE_URL"},
		},
		{
			name:        "changed generic replayable value",
			captured:    map[string]string{"PROJECT_MODE": "review"},
			live:        map[string]string{"PROJECT_MODE": "implementation"},
			wantChanged: []string{"PROJECT_MODE"},
		},
		{
			name:     "transient values ignored",
			captured: map[string]string{"TERM": "xterm", "HERDR_PANE_ID": "old"},
			live:     map[string]string{"TERM": "screen", "HERDR_PANE_ID": "new"},
		},
		{
			name:     "empty captured environment is faithful",
			captured: nil,
			live:     map[string]string{"ANTHROPIC_BASE_URL": "https://live"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			missing, changed := EnvironmentDrift(tc.captured, tc.live)
			if !slices.Equal(missing, tc.wantMissing) {
				t.Errorf("missing = %v, want %v", missing, tc.wantMissing)
			}
			if !slices.Equal(changed, tc.wantChanged) {
				t.Errorf("changed = %v, want %v", changed, tc.wantChanged)
			}
		})
	}
}
