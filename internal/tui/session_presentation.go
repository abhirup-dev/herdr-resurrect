package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/list"
	"github.com/charmbracelet/x/ansi"
)

const sessionActivityIndent = "    "

var (
	styFullSnapshot     = styDim.Italic(true).Faint(true)
	styCurrentActivity  = styOK.Italic(true).Faint(true)
	styArchivedActivity = styWarn.Italic(true).Faint(true)
)

func newSessionDelegate(wrapped bool) list.DefaultDelegate {
	delegate := list.NewDefaultDelegate()
	delegate.Styles = archiveItemStyles()
	if wrapped {
		delegate.SetHeight(3)
	}
	return delegate
}

func sessionRowsWrap(rows []sessionRow, width int) bool {
	for _, row := range rows {
		if sessionActivityWraps(row, width) {
			return true
		}
	}
	return false
}

func sessionActivityDescription(row sessionRow, width int) string {
	if row.activity.LatestFullAt.IsZero() {
		return styFullSnapshot.Render("no full snapshot")
	}
	primary := styFullSnapshot.Render("full snapshot " + relTime(row.activity.LatestFullAt.Local()))
	if row.running {
		current := styCurrentActivity.Render(fmt.Sprintf("current %d %s, %d %s",
			row.activity.LiveAgents, plural(row.activity.LiveAgents, "agent"),
			row.activity.LiveWorkspaces, plural(row.activity.LiveWorkspaces, "space")))
		primary += styDim.Render(" · ") + current
	}
	if row.activity.ArchivedAgents == 0 {
		return primary
	}
	archived := styArchivedActivity.Render(fmt.Sprintf("archived %d %s, %d %s (since %s)",
		row.activity.ArchivedAgents, plural(row.activity.ArchivedAgents, "agent"),
		row.activity.ArchivedWorkspaces, plural(row.activity.ArchivedWorkspaces, "space"),
		strings.TrimSuffix(relTime(row.activity.ArchivedSince.Local()), " ago")))
	inline := primary + styDim.Render("  · ") + archived
	if ansi.StringWidth(inline) <= max(1, width-4) {
		return inline
	}
	return primary + "\n" + sessionActivityIndent + archived
}

func sessionActivityWraps(row sessionRow, width int) bool {
	return strings.Contains(sessionActivityDescription(row, width), "\n")
}
