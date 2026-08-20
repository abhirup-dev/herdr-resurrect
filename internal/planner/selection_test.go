package planner

import (
	"slices"
	"testing"

	"github.com/abhirup-dev/herdr-resurrect/internal/manifest"
)

func TestSnapshotSelectionHelpers(t *testing.T) {
	snapshot := &manifest.Snapshot{Workspaces: []manifest.Workspace{
		{ID: "one", Tabs: []manifest.Tab{{Panes: []manifest.Pane{
			{Key: "a", PaneID: "p1"},
			{Key: "b", PaneID: "p2"},
		}}}},
		{ID: "two", Tabs: []manifest.Tab{{Panes: []manifest.Pane{
			{Key: "c", PaneID: "p3"},
		}}}},
	}}

	selection := SelectSnapshot(snapshot)
	if selected, total := SnapshotSelectedCount(snapshot, selection); selected != 3 || total != 3 {
		t.Fatalf("selected/total = %d/%d, want 3/3", selected, total)
	}
	if got, want := SelectedPaneIDs(snapshot, Selection{"b": true, "c": true}), []string{"p2", "p3"}; !slices.Equal(got, want) {
		t.Fatalf("pane ids = %v, want %v", got, want)
	}
	if got := SelectSnapshot(nil); len(got) != 0 {
		t.Fatalf("nil snapshot selection = %v, want empty", got)
	}
}

func TestToggleWorkspaceUsesTriStatePolicy(t *testing.T) {
	workspace := manifest.Workspace{Tabs: []manifest.Tab{{Panes: []manifest.Pane{
		{Key: "a"}, {Key: "b"},
	}}}}
	selection := Selection{"a": true}
	ToggleWorkspace(workspace, selection)
	if selected, total := SelectedCount(workspace, selection); selected != total {
		t.Fatalf("partial toggle selected %d/%d, want all", selected, total)
	}
	ToggleWorkspace(workspace, selection)
	if selected, _ := SelectedCount(workspace, selection); selected != 0 {
		t.Fatalf("full toggle selected %d, want none", selected)
	}
}
