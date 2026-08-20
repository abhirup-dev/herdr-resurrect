package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/abhirup-dev/herdr-resurrect/internal/capture"
	"github.com/abhirup-dev/herdr-resurrect/internal/manifest"
	"github.com/abhirup-dev/herdr-resurrect/internal/planner"
)

type captureLiveMsg struct {
	live *manifest.Snapshot
	err  error
}

type captureNode struct {
	kind           nodeKind
	workspaceIndex int
	tabIndex       int
	paneIndex      int
}

func loadCaptureLiveCmd(session string) tea.Cmd {
	return func() tea.Msg {
		live, err := capture.Session(capture.Options{Session: session})
		return captureLiveMsg{live: live, err: err}
	}
}

func captureNodes(snapshot *manifest.Snapshot) []captureNode {
	if snapshot == nil {
		return nil
	}
	var nodes []captureNode
	for workspaceIndex, workspace := range snapshot.Workspaces {
		nodes = append(nodes, captureNode{kind: workspaceNode, workspaceIndex: workspaceIndex})
		for tabIndex, tab := range workspace.Tabs {
			nodes = append(nodes, captureNode{kind: tabNode, workspaceIndex: workspaceIndex, tabIndex: tabIndex})
			for paneIndex := range tab.Panes {
				nodes = append(nodes, captureNode{
					kind:           paneNode,
					workspaceIndex: workspaceIndex,
					tabIndex:       tabIndex,
					paneIndex:      paneIndex,
				})
			}
		}
	}
	return nodes
}

func (m *model) toggleCaptureNode() {
	nodes := captureNodes(m.captureLive)
	if m.captureCursor < 0 || m.captureCursor >= len(nodes) {
		return
	}
	node := nodes[m.captureCursor]
	workspace := m.captureLive.Workspaces[node.workspaceIndex]
	switch node.kind {
	case workspaceNode:
		planner.ToggleWorkspace(workspace, m.captureSelection)
	case tabNode:
		planner.ToggleTab(workspace.Tabs[node.tabIndex], m.captureSelection)
	case paneNode:
		planner.TogglePane(workspace.Tabs[node.tabIndex].Panes[node.paneIndex].Key, m.captureSelection)
	}
}

func (m *model) captureTreeLines() []string {
	if m.captureLive == nil {
		return nil
	}
	var nodes []treeNode
	nodeIndex := 0
	for _, workspace := range m.captureLive.Workspaces {
		workspaceNodeIndex := nodeIndex
		nodeIndex++
		selected, total := planner.SelectedCount(workspace, m.captureSelection)
		workspaceNode := treeNode{
			Focused:  workspaceNodeIndex == m.captureCursor,
			Marker:   triStateMarker(selected, total),
			Label:    styTitle.Render(workspaceKey(workspace)),
			Metadata: []string{treeNeutral(fmt.Sprintf("%d/%d panes", selected, total))},
		}
		for _, tab := range workspace.Tabs {
			tabNodeIndex := nodeIndex
			nodeIndex++
			selected, total := planner.TabSelectedCount(tab, m.captureSelection)
			tabName := tab.Label
			if tabName == "" {
				tabName = tab.ID
			}
			tabNode := treeNode{
				Focused:  tabNodeIndex == m.captureCursor,
				Marker:   triStateMarker(selected, total),
				Label:    styTreeLabel.Render(clipLabel(tabName, 32)),
				Metadata: []string{treeNeutral(layoutLabel(tab))},
			}
			for _, pane := range tab.Panes {
				paneNodeIndex := nodeIndex
				nodeIndex++
				kind, name := pane.Agent, pane.Key
				if kind == "" {
					kind, name = "shell", "shell"
				}
				marker := styDim.Render("○")
				if m.captureSelection[pane.Key] {
					marker = styTitle.Render("◆")
				}
				tabNode.Children = append(tabNode.Children, treeNode{
					Focused: paneNodeIndex == m.captureCursor,
					Marker:  marker,
					Label:   icon(kind, pane.Title) + " " + styTreeLabel.Render(name),
					Metadata: []string{
						treeLive("live"),
						treeNeutral(providerDisplay(pane.Agent, pane.Env)),
					},
				})
			}
			workspaceNode.Children = append(workspaceNode.Children, tabNode)
		}
		nodes = append(nodes, workspaceNode)
	}
	return renderTree(nodes, treeOptions{Controls: true, Width: m.contentWidth()})
}

func (m *model) captureView() string {
	if m.err != nil {
		return styBad.Render(m.err.Error())
	}
	if m.spinning || m.captureLive == nil {
		return m.spin.View() + styDim.Render(" reading live topology…")
	}
	selected, total := planner.SnapshotSelectedCount(m.captureLive, m.captureSelection)
	out := []string{
		styTitle.Render("SELECT LIVE PANES TO CAPTURE"),
		treeMetadata(treeNeutral(fmt.Sprintf("%d/%d selected", selected, total)), treeNeutral("full topology retained as context")),
		"",
	}
	out = append(out, m.captureTreeLines()...)
	if len(m.captureLive.Workspaces) == 0 {
		out = append(out, styDim.Render("No live panes are available."))
	}
	rendered := strings.Join(out, "\n")
	return m.scrollViewport(rendered, 3+m.captureCursor, 3, &m.captureScroll)
}
