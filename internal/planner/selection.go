// Package planner owns restoration selection semantics shared by the CLI and TUI.
package planner

import "github.com/abhirup-dev/herdr-resurrect/internal/manifest"

// Selection is the set of captured pane identities included in a restoration.
type Selection map[string]bool

// PaneKeys returns captured pane identities in manifest order.
func PaneKeys(workspace manifest.Workspace) []string {
	var keys []string
	for _, tab := range workspace.Tabs {
		for _, pane := range tab.Panes {
			keys = append(keys, pane.Key)
		}
	}
	return keys
}

// SelectedCount reports selected and total panes in a workspace target.
func SelectedCount(workspace manifest.Workspace, selection Selection) (selected, total int) {
	for _, key := range PaneKeys(workspace) {
		total++
		if selection[key] {
			selected++
		}
	}
	return
}

// SelectWhole creates a selection containing every pane in the target.
func SelectWhole(workspace manifest.Workspace) Selection {
	selection := Selection{}
	for _, key := range PaneKeys(workspace) {
		selection[key] = true
	}
	return selection
}

// SelectSnapshot selects every pane represented by a live snapshot.
func SelectSnapshot(snapshot *manifest.Snapshot) Selection {
	selection := Selection{}
	if snapshot == nil {
		return selection
	}
	for _, workspace := range snapshot.Workspaces {
		for _, key := range PaneKeys(workspace) {
			selection[key] = true
		}
	}
	return selection
}

// SnapshotSelectedCount reports selected and total panes across a snapshot.
func SnapshotSelectedCount(snapshot *manifest.Snapshot, selection Selection) (selected, total int) {
	if snapshot == nil {
		return 0, 0
	}
	for _, workspace := range snapshot.Workspaces {
		count, workspaceTotal := SelectedCount(workspace, selection)
		selected += count
		total += workspaceTotal
	}
	return selected, total
}

// SelectedPaneIDs returns selected pane ids in snapshot topology order.
func SelectedPaneIDs(snapshot *manifest.Snapshot, selection Selection) []string {
	if snapshot == nil {
		return nil
	}
	var paneIDs []string
	for _, workspace := range snapshot.Workspaces {
		for _, tab := range workspace.Tabs {
			for _, pane := range tab.Panes {
				if selection[pane.Key] {
					paneIDs = append(paneIDs, pane.PaneID)
				}
			}
		}
	}
	return paneIDs
}

// ToggleWorkspace follows the shared tri-state policy: partial selects all;
// full clears all.
func ToggleWorkspace(workspace manifest.Workspace, selection Selection) {
	selected, total := SelectedCount(workspace, selection)
	selectAll := selected != total
	for _, key := range PaneKeys(workspace) {
		if selectAll {
			selection[key] = true
		} else {
			delete(selection, key)
		}
	}
}

// MapMatching carries selected pane identities into another snapshot target.
// Entries absent from the new target are dropped; new entries stay unselected.
func MapMatching(previous Selection, workspace manifest.Workspace) Selection {
	selection := Selection{}
	for _, key := range PaneKeys(workspace) {
		if previous[key] {
			selection[key] = true
		}
	}
	return selection
}

// DefaultSelection returns the snapshot's persisted capture scope for one
// workspace. Legacy snapshots without a scope select the complete workspace.
func DefaultSelection(snapshot *manifest.Snapshot, workspace manifest.Workspace) Selection {
	keys := snapshot.CapturedPaneKeys(workspace.ID)
	if keys == nil {
		return SelectWhole(workspace)
	}
	selection := Selection{}
	for _, key := range PaneKeys(workspace) {
		if keys[key] {
			selection[key] = true
		}
	}
	return selection
}

// RestrictSelection enforces a persisted capture scope as a hard upper bound.
func RestrictSelection(selection Selection, allowed Selection) {
	if allowed == nil {
		return
	}
	for key := range selection {
		if !allowed[key] {
			delete(selection, key)
		}
	}
}

// TabSelectedCount reports selected and total panes beneath a tab node.
func TabSelectedCount(tab manifest.Tab, selection Selection) (selected, total int) {
	for _, pane := range tab.Panes {
		total++
		if selection[pane.Key] {
			selected++
		}
	}
	return
}

// ToggleTab selects all panes beneath a partially selected tab, or clears a
// fully selected tab.
func ToggleTab(tab manifest.Tab, selection Selection) {
	selected, total := TabSelectedCount(tab, selection)
	selectAll := selected != total
	for _, pane := range tab.Panes {
		if selectAll {
			selection[pane.Key] = true
		} else {
			delete(selection, pane.Key)
		}
	}
}

// TogglePane toggles one captured pane identity.
func TogglePane(key string, selection Selection) {
	if selection[key] {
		delete(selection, key)
	} else {
		selection[key] = true
	}
}
