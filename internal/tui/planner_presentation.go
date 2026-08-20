package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/abhirupdas/herdr-archive/internal/manifest"
	"github.com/abhirupdas/herdr-archive/internal/planner"
	"github.com/abhirupdas/herdr-archive/internal/strategy"
)

const transcriptSizeDisplayThreshold = 400 * 1024

type plannerTargetStats struct {
	agents         int
	shells         int
	tabs           int
	panes          int
	transcriptSize int64
	liveKnown      bool
	liveCount      int
	missing        int
}

func (s plannerTargetStats) workspaceMissing() bool {
	return s.missing > 0 && s.liveCount == 0
}

type plannerDetailsBuilder struct {
	details []string
}

func plannerWorkspaceHeading(key string, width int, missing bool) string {
	if !missing {
		return styTitle.Render(fitLine(key, width))
	}
	labelWidth := max(1, width-ansi.StringWidth(" missing")-1)
	return styTitle.Render(fitLine(key, labelWidth)) + " " + treeMissing("missing")
}

func plannerTargetBadge(target workspaceTarget, live *manifest.Snapshot) string {
	if target.snapshot.CaptureScope != nil {
		return " " + styNice.Render("CURATED")
	}
	if !target.isLast {
		return ""
	}
	if planner.ExactWorkspaceMatch(target.workspace, live) {
		return " " + treeLive("CURRENT")
	}
	return " " + treeMissing("STALE")
}

func plannerTargetDetails(target workspaceTarget, stats plannerTargetStats) string {
	return newPlannerDetailsBuilder(target.snapshot.CreatedAt.Local()).
		withOccupants(stats).
		withLiveState(stats).
		withTranscriptSize(stats.transcriptSize).
		build()
}

func newPlannerDetailsBuilder(createdAt time.Time) *plannerDetailsBuilder {
	return &plannerDetailsBuilder{details: []string{treeNeutral(relTime(createdAt))}}
}

func (b *plannerDetailsBuilder) withOccupants(stats plannerTargetStats) *plannerDetailsBuilder {
	occupants, occupant := stats.agents, "agent"
	if stats.agents == 0 {
		occupants, occupant = stats.shells, "shell"
	}
	summary := fmt.Sprintf("%d %s", occupants, plural(occupants, occupant))
	summarizedTopology := occupants == 1
	switch {
	case occupants > 1 && stats.tabs == 1 && stats.panes == occupants:
		summary += " (in panes)"
		summarizedTopology = true
	case occupants > 1 && stats.tabs == occupants:
		summary += " (in tabs)"
		summarizedTopology = true
	}
	b.details = append(b.details, treeNeutral(summary))
	if !summarizedTopology {
		b.details = append(b.details, treeNeutral(fmt.Sprintf("%d %s", stats.tabs, plural(stats.tabs, "tab"))))
	}
	return b
}

func (b *plannerDetailsBuilder) withLiveState(stats plannerTargetStats) *plannerDetailsBuilder {
	if !stats.liveKnown || stats.workspaceMissing() {
		return b
	}
	liveMeta := treeNeutral(fmt.Sprintf("%d live", stats.liveCount))
	if stats.liveCount > 0 {
		liveMeta = treeLive(fmt.Sprintf("%d live", stats.liveCount))
	}
	b.details = append(b.details, liveMeta)
	if stats.missing > 0 {
		b.details = append(b.details, treeMissing(fmt.Sprintf("%d missing", stats.missing)))
	}
	return b
}

func (b *plannerDetailsBuilder) withTranscriptSize(size int64) *plannerDetailsBuilder {
	if size > transcriptSizeDisplayThreshold {
		b.details = append(b.details, treeNeutral(humanBytes(size)))
	}
	return b
}

func (b *plannerDetailsBuilder) build() string {
	return treeMetadata(b.details...)
}

func (m *model) collectPlannerTargetStats(target workspaceTarget) plannerTargetStats {
	stats := plannerTargetStats{
		tabs:      len(target.workspace.Tabs),
		liveKnown: m.live != nil,
	}
	for _, tab := range target.workspace.Tabs {
		stats.panes += len(tab.Panes)
		for _, pane := range tab.Panes {
			if !target.snapshot.CapturesPane(target.workspace.ID, tab.ID, pane.Key) {
				continue
			}
			if pane.Agent == "" {
				stats.shells++
				continue
			}
			stats.agents++
			stats.transcriptSize += strategy.TranscriptSize(pane.Agent, pane.SID, pane.Env)
		}
	}
	if m.live != nil {
		states := m.targetStates(target)
		_, stats.missing, stats.liveCount = planner.RestorableCountWithin(
			target.workspace, nil, states, targetAllowed(target),
		)
	}
	return stats
}
