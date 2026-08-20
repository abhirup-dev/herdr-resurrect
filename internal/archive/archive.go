// Package archive applies a confirmed capture-and-stop plan.
package archive

import (
	"fmt"
	"strings"

	"github.com/abhirupdas/herdr-archive/internal/capture"
	"github.com/abhirupdas/herdr-archive/internal/herdr"
	"github.com/abhirupdas/herdr-archive/internal/manifest"
)

// Apply saves the exact snapshot shown during confirmation, after rejecting a
// topology change, then stops the session. A failed stop leaves the snapshot.
func Apply(planned *manifest.Snapshot, name, root string) (string, error) {
	if err := verify(planned); err != nil {
		return "", err
	}
	snapshot := *planned
	snapshot.Name = strings.TrimSpace(name)
	if snapshot.Name == "" {
		snapshot.Name = manifest.DefaultName(snapshot.CreatedAt)
	}
	path, err := snapshot.Save(root)
	if err != nil {
		return "", fmt.Errorf("save: %w", err)
	}
	if _, err := herdr.Run("session", "stop", snapshot.Session); err != nil {
		return path, fmt.Errorf("stop: %w (snapshot kept at %s)", err, path)
	}
	return path, nil
}

// Stop stops a session whose confirmed snapshot was already saved.
func Stop(planned *manifest.Snapshot) error {
	if err := verify(planned); err != nil {
		return err
	}
	if _, err := herdr.Run("session", "stop", planned.Session); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	return nil
}

func verify(planned *manifest.Snapshot) error {
	if planned == nil {
		return fmt.Errorf("no confirmed capture-and-stop plan")
	}
	current, err := capture.Session(capture.Options{Session: planned.Session})
	if err != nil {
		return fmt.Errorf("freshness capture: %w", err)
	}
	if !sameTopology(planned, current) {
		return fmt.Errorf("stale capture-and-stop plan: live topology changed after confirmation")
	}
	return nil
}

func sameTopology(left, right *manifest.Snapshot) bool {
	if left == nil || right == nil || left.Session != right.Session || len(left.Workspaces) != len(right.Workspaces) {
		return false
	}
	rightWorkspaces := map[string]manifest.Workspace{}
	for _, workspace := range right.Workspaces {
		rightWorkspaces[workspace.ID] = workspace
	}
	for _, workspace := range left.Workspaces {
		other, ok := rightWorkspaces[workspace.ID]
		if !ok || workspace.Label != other.Label || len(workspace.Tabs) != len(other.Tabs) {
			return false
		}
		rightTabs := map[string]manifest.Tab{}
		for _, tab := range other.Tabs {
			rightTabs[tab.ID] = tab
		}
		for _, tab := range workspace.Tabs {
			otherTab, ok := rightTabs[tab.ID]
			if !ok || tab.Label != otherTab.Label || len(tab.Panes) != len(otherTab.Panes) {
				return false
			}
			rightPanes := map[string]manifest.Pane{}
			for _, pane := range otherTab.Panes {
				rightPanes[pane.PaneID] = pane
			}
			for _, pane := range tab.Panes {
				otherPane, ok := rightPanes[pane.PaneID]
				if !ok || pane.Key != otherPane.Key || pane.Agent != otherPane.Agent || pane.Name != otherPane.Name {
					return false
				}
			}
		}
	}
	return true
}
