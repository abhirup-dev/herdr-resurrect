// Package activity summarizes how a session's represented agent population has
// changed across archived full snapshots.
package activity

import (
	"time"

	"github.com/abhirupdas/herdr-archive/internal/manifest"
)

// Summary is presentation-neutral Level 1 activity data.
type Summary struct {
	LatestFullAt       time.Time
	LiveAgents         int
	LiveWorkspaces     int
	ArchivedAgents     int
	ArchivedWorkspaces int
	ArchivedSince      time.Time
}

// ArchivedStateSummaryStrategy allows the archived-state definition to change
// without coupling it to snapshot discovery or TUI presentation.
type ArchivedStateSummaryStrategy interface {
	Summarize(snapshots []*manifest.Snapshot, now time.Time) Summary
}

// RecentPeak compares the latest full snapshot with the largest full snapshot
// inside Window. Positive per-workspace count differences are considered
// inactive; identities deliberately do not participate in this strategy.
type RecentPeak struct {
	Window time.Duration
}

func (r RecentPeak) Summarize(snapshots []*manifest.Snapshot, now time.Time) Summary {
	window := r.Window
	if window <= 0 {
		window = 7 * 24 * time.Hour
	}
	var latest, peak *manifest.Snapshot
	peakAgents := -1
	cutoff := now.Add(-window)
	for _, snapshot := range snapshots {
		if snapshot == nil || snapshot.CaptureScope != nil {
			continue
		}
		if latest == nil || snapshot.CreatedAt.After(latest.CreatedAt) {
			latest = snapshot
		}
		agents := agentCount(snapshot)
		if snapshot.CreatedAt.Before(cutoff) || snapshot.CreatedAt.After(now) {
			continue
		}
		if agents > peakAgents || (agents == peakAgents && (peak == nil || snapshot.CreatedAt.After(peak.CreatedAt))) {
			peak, peakAgents = snapshot, agents
		}
	}
	if latest == nil {
		return Summary{}
	}

	latestCounts := workspaceAgentCounts(latest)
	summary := Summary{
		LatestFullAt:   latest.CreatedAt,
		LiveAgents:     agentCount(latest),
		LiveWorkspaces: populatedWorkspaceCount(latestCounts),
	}
	if peak == nil {
		return summary
	}
	for workspace, count := range workspaceAgentCounts(peak) {
		if difference := count - latestCounts[workspace]; difference > 0 {
			summary.ArchivedAgents += difference
			summary.ArchivedWorkspaces++
		}
	}
	if summary.ArchivedAgents > 0 {
		summary.ArchivedSince = peak.CreatedAt
	}
	return summary
}

func agentCount(snapshot *manifest.Snapshot) int {
	count := 0
	for _, workspace := range snapshot.Workspaces {
		for _, tab := range workspace.Tabs {
			for _, pane := range tab.Panes {
				if pane.Agent != "" && snapshot.CapturesPane(workspace.ID, tab.ID, pane.Key) {
					count++
				}
			}
		}
	}
	return count
}

func workspaceAgentCounts(snapshot *manifest.Snapshot) map[string]int {
	counts := map[string]int{}
	for _, workspace := range snapshot.Workspaces {
		key := workspace.ID
		if workspace.Label != "" {
			key = workspace.Label
		}
		for _, tab := range workspace.Tabs {
			for _, pane := range tab.Panes {
				if pane.Agent != "" && snapshot.CapturesPane(workspace.ID, tab.ID, pane.Key) {
					counts[key]++
				}
			}
		}
	}
	return counts
}

func populatedWorkspaceCount(counts map[string]int) int {
	count := 0
	for _, agents := range counts {
		if agents > 0 {
			count++
		}
	}
	return count
}
