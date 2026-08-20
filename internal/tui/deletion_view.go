package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/abhirupdas/herdr-archive/internal/capture"
	"github.com/abhirupdas/herdr-archive/internal/manifest"
	"github.com/abhirupdas/herdr-archive/internal/planner"
)

type archivePlanMsg struct {
	live  *manifest.Snapshot
	path  string
	saved bool
	err   error
}

func loadArchivePlanCmd(session string) tea.Cmd {
	return func() tea.Msg {
		live, err := capture.Session(capture.Options{Session: session})
		return archivePlanMsg{live: live, err: err}
	}
}

func loadSavedStopPlanCmd(session string) tea.Cmd {
	return func() tea.Msg {
		live, err := capture.Session(capture.Options{Session: session})
		if err != nil {
			return archivePlanMsg{err: err}
		}
		live.Name = "Session stop capture · " + manifest.DefaultName(live.CreatedAt)
		path, err := live.Save("")
		return archivePlanMsg{live: live, path: path, saved: err == nil, err: err}
	}
}

func (m *model) startArchiveReview(name string) tea.Cmd {
	m.archiveName = name
	m.archiveLive = nil
	m.archivePath = ""
	m.archivePreSaved = false
	m.archivePreview = false
	m.archiveScroll = 0
	m.confirm = ""
	m.spinning = true
	m.note = "building capture-and-stop plan…"
	return tea.Batch(loadArchivePlanCmd(m.cur.name), m.spin.Tick)
}

func snapshotPaneCount(snapshot *manifest.Snapshot) int {
	if snapshot == nil {
		return 0
	}
	_, total := snapshot.CapturedPaneCount()
	return total
}

func stopSummaryLines(snapshot *manifest.Snapshot, width int) []string {
	var nodes []treeNode
	for _, workspace := range snapshot.Workspaces {
		panes := 0
		for _, tab := range workspace.Tabs {
			panes += len(tab.Panes)
		}
		nodes = append(nodes, treeNode{
			Label: styTreeLabel.Render(workspaceKey(workspace)),
			Metadata: []string{
				treeNeutral(fmt.Sprintf("%d %s", len(workspace.Tabs), plural(len(workspace.Tabs), "tab"))),
				treeNeutral(fmt.Sprintf("%d %s", panes, plural(panes, "pane"))),
			},
		})
	}
	return renderTree(nodes, treeOptions{Width: width})
}

func (m *model) deletionConfirmationBody() string {
	if m.archiveLive == nil || m.cur == nil {
		return styBad.Render("No live deletion plan is available.")
	}
	plan := planner.Compile(m.cur.name, nil, m.archiveLive)
	available := max(40, m.contentWidth())
	name := m.archiveName
	if name == "" {
		name = "automatic date and time"
	}
	var body string
	if m.archivePreSaved {
		body += styOK.Render("CAPTURE SAVED") + styDim.Render(" · "+m.archiveName)
		if m.archivePath != "" {
			body += "\n" + styDim.Render(m.archivePath)
		}
		body += "\n\n"
	}
	sessionLines := stopSummaryLines(m.archiveLive, available)
	if m.archivePreview {
		sessionLines = topologyLines(plan.Before, available)
	}
	current := append([]string{styTitle.Render("CURRENT SESSION")}, sessionLines...)
	body += strings.Join(fitLines(current, available), "\n")
	body += "\n\n" + styOK.Render("snapshot retained")
	body += "\n" + styDim.Render("name: "+name)
	panes := snapshotPaneCount(m.archiveLive)
	body += "\n\n" + styBad.Render(fmt.Sprintf("%d LIVE %s WILL STOP", panes, strings.ToUpper(plural(panes, "pane"))))
	body += "\n" + styDim.Render("The full live session is captured first. Its Herdr state directory is retained.")
	body += "\n" + styWarn.Render("This confirmation stops the entire session, including the pane running this browser if applicable.")
	return body
}
