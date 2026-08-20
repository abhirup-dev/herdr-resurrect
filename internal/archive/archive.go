// Package archive applies a confirmed capture-and-stop plan.
package archive

import (
	"fmt"
	"strings"

	"github.com/abhirup-dev/herdr-resurrect/internal/capture"
	"github.com/abhirup-dev/herdr-resurrect/internal/herdr"
	"github.com/abhirup-dev/herdr-resurrect/internal/manifest"
	"github.com/abhirup-dev/herdr-resurrect/internal/planner"
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
	if !planner.ExactSnapshotMatch(planned, current) {
		return fmt.Errorf("stale capture-and-stop plan: replayable live state changed after confirmation")
	}
	return nil
}
