// Package planner owns restoration selection semantics shared by the CLI and TUI.
package planner

import "github.com/abhirupdas/herdr-archive/internal/manifest"

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
