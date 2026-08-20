package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/abhirup-dev/herdr-resurrect/internal/manifest"
	"github.com/abhirup-dev/herdr-resurrect/internal/planner"
)

const transcriptSizeDisplayThreshold = 400 * 1024

type plannerTargetStats struct {
	agents           int
	shells           int
	tabs             int
	panes            int
	transcriptSize   int64
	liveKnown        bool
	workspacePresent bool
	liveAgents       int
	missingAgents    int
	liveShells       int
	missingShells    int
	readyPanes       int
}

func (s plannerTargetStats) workspaceMissing() bool {
	return s.liveKnown && !s.workspacePresent
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
	if stats.liveAgents > 0 {
		b.details = append(b.details, treeLive(fmt.Sprintf("%d %s live", stats.liveAgents, plural(stats.liveAgents, "agent"))))
	}
	if stats.missingAgents > 0 {
		b.details = append(b.details, treeMissing(fmt.Sprintf("%d %s missing", stats.missingAgents, plural(stats.missingAgents, "agent"))))
	}
	if stats.liveShells > 0 {
		b.details = append(b.details, treeLive(fmt.Sprintf("%d %s live", stats.liveShells, plural(stats.liveShells, "shell"))))
	}
	if stats.missingShells > 0 {
		b.details = append(b.details, treeMissing(fmt.Sprintf("%d %s missing", stats.missingShells, plural(stats.missingShells, "shell"))))
	}
	if stats.readyPanes > 0 {
		b.details = append(b.details, treeLive(fmt.Sprintf("%d %s ready", stats.readyPanes, plural(stats.readyPanes, "pane"))))
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
	occupancy := planner.TargetOccupancy(target.snapshot, target.workspace, m.live, targetAllowed(target))
	return plannerTargetStats{
		agents:           occupancy.Agents,
		shells:           occupancy.Shells,
		tabs:             occupancy.Tabs,
		panes:            occupancy.Panes,
		transcriptSize:   occupancy.TranscriptSize,
		liveKnown:        m.live != nil,
		workspacePresent: occupancy.WorkspacePresent,
		liveAgents:       occupancy.LiveAgents,
		missingAgents:    occupancy.MissingAgents,
		liveShells:       occupancy.LiveShells,
		missingShells:    occupancy.MissingShells,
		readyPanes:       occupancy.ReadyPanes,
	}
}
