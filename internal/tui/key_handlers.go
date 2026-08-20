package tui

import (
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/abhirup-dev/herdr-resurrect/internal/manifest"
	"github.com/abhirup-dev/herdr-resurrect/internal/planner"
	"github.com/abhirup-dev/herdr-resurrect/internal/resume"
)

type keyResult struct {
	handled bool
	model   tea.Model
	cmd     tea.Cmd
}

func handled(m tea.Model, cmd tea.Cmd) keyResult {
	return keyResult{handled: true, model: m, cmd: cmd}
}

func unhandled() keyResult {
	return keyResult{}
}

func (m *model) handleNamingKey(msg tea.KeyMsg) keyResult {
	if !m.namingCapture {
		return unhandled()
	}
	switch msg.String() {
	case "esc":
		m.namingCapture = false
		m.captureInput.Blur()
		m.captureInput.Reset()
		m.captureSession = ""
		m.captureSessions = nil
		m.note = ""
		return handled(m, nil)
	case "ctrl+x":
		selected, total := planner.SnapshotSelectedCount(m.captureLive, m.captureSelection)
		if m.captureLive == nil || selected == 0 || selected != total {
			m.note = "capture & stop requires every live pane to be selected"
			return handled(m, nil)
		}
		name := strings.TrimSpace(m.captureInput.Value())
		if name == "" {
			name = manifest.DefaultName(time.Now())
		}
		m.namingCapture = false
		m.captureInput.Blur()
		m.captureInput.Reset()
		m.captureSession = ""
		m.captureSessions = nil
		return handled(m, m.startArchiveReview(name))
	case "enter":
		name := strings.TrimSpace(m.captureInput.Value())
		if name == "" {
			name = manifest.DefaultName(time.Now())
		}
		sessions := append([]string(nil), m.captureSessions...)
		if len(sessions) == 0 && m.captureSession != "" {
			sessions = append(sessions, m.captureSession)
		}
		paneIDs := planner.SelectedPaneIDs(m.captureLive, m.captureSelection)
		fromPicker := m.captureLive != nil
		if selected, total := planner.SnapshotSelectedCount(m.captureLive, m.captureSelection); fromPicker && selected == total {
			paneIDs = nil
		}
		m.namingCapture = false
		m.captureInput.Blur()
		m.captureInput.Reset()
		m.captureSession = ""
		m.captureSessions = nil
		m.captureLive = nil
		m.captureSelection = nil
		if fromPicker {
			m.mode = viewSnapshots
		}
		m.note = "capturing “" + name + "”…"
		return handled(m, captureSessionsCmd(sessions, name, paneIDs))
	default:
		var cmd tea.Cmd
		m.captureInput, cmd = m.captureInput.Update(msg)
		return handled(m, cmd)
	}
}

func (m *model) handleFilterKey(msg tea.KeyMsg) keyResult {
	var cmd tea.Cmd
	switch {
	case m.mode == viewSessions && m.sessList.SettingFilter():
		m.sessList, cmd = m.sessList.Update(msg)
	case m.mode == viewSnapshots && m.snapList.SettingFilter():
		m.snapList, cmd = m.snapList.Update(msg)
	case m.mode == viewPlan && m.planList.SettingFilter():
		m.planList, cmd = m.planList.Update(msg)
	default:
		return unhandled()
	}
	return handled(m, cmd)
}

func (m *model) handleConfirmationKey(msg tea.KeyMsg) keyResult {
	if m.confirm == "" {
		return unhandled()
	}
	switch msg.String() {
	case "C":
		if m.confirm == "archive" && m.cur != nil && !m.spinning {
			m.spinning = true
			m.note = "capturing a fresh full snapshot…"
			return handled(m, tea.Batch(loadForcedStopPlanCmd(m.cur.name), m.spin.Tick))
		}
	case "p":
		if m.confirm == "archive" {
			m.archivePreview = !m.archivePreview
			m.archiveScroll = 0
		}
	case "j", "down":
		if m.confirm == "archive" {
			m.archiveScroll++
		} else {
			m.confirmScroll++
		}
	case "k", "up":
		if m.confirm == "archive" && m.archiveScroll > 0 {
			m.archiveScroll--
		} else if m.confirm != "archive" && m.confirmScroll > 0 {
			m.confirmScroll--
		}
	case "y":
		if m.spinning {
			return handled(m, nil)
		}
		if m.confirm == "archive" {
			model, cmd := m.execArchive()
			return handled(model, cmd)
		}
		model, cmd := m.execRestore()
		return handled(model, cmd)
	case "esc", "q", "h":
		m.archivePreview = false
		m.archiveScroll = 0
		m.confirm = ""
		if m.stopCurrentMode && m.archivePreSaved {
			return handled(m, tea.Quit)
		}
	}
	return handled(m, nil)
}

func (m *model) handleSnapshotKey(msg tea.KeyMsg) keyResult {
	if m.mode != viewSnapshots {
		return unhandled()
	}
	switch msg.String() {
	case "backspace":
		m.workspacePlan = map[string]workspaceSelection{}
		m.compileRestorationPlan()
		m.note = ""
	case "j", "down":
		if m.plannerCursor+1 < len(m.targets) {
			m.plannerCursor++
		}
	case "k", "up":
		if m.plannerCursor > 0 {
			m.plannerCursor--
		}
	case "space", " ", "tab":
		if m.spinning || m.live == nil {
			m.note = "wait for a current live topology before selecting"
			return handled(m, nil)
		}
		if m.plannerCursor >= 0 && m.plannerCursor < len(m.targets) {
			target := m.targets[m.plannerCursor]
			previous, ok := m.workspacePlan[target.key]
			if ok && previous.target.path != target.path {
				previous = mapTargetSelection(previous, target, m.live)
			}
			if ok {
				selected, total, _ := selectedPaneCount(previous, m.live)
				switch {
				case total == 0:
					m.workspacePlan[target.key] = previous
					m.note = "all panes in this target are already live"
				case selected == total:
					delete(m.workspacePlan, target.key)
				default:
					m.workspacePlan[target.key] = selectWholeTarget(target, m.live)
				}
			} else {
				selection := selectWholeTarget(target, m.live)
				_, total, _ := selectedPaneCount(selection, m.live)
				if total == 0 {
					m.note = "all panes in this target are already live"
				} else {
					m.workspacePlan[target.key] = selection
				}
			}
			m.compileRestorationPlan()
		}
	case "enter", "l":
		if m.spinning || m.live == nil {
			m.note = "wait for a current live topology before inspecting"
			return handled(m, nil)
		}
		if m.plannerCursor >= 0 && m.plannerCursor < len(m.targets) {
			target := m.targets[m.plannerCursor]
			selection, ok := m.workspacePlan[target.key]
			if !ok {
				selection = workspaceSelection{target: target, selected: planner.Selection{}}
			} else if selection.target.path != target.path {
				selection = mapTargetSelection(selection, target, m.live)
			}
			planner.PruneLive(selection.selected, m.targetStates(target))
			m.workspacePlan[target.key] = selection
			m.inspectTarget = target
			m.inspectPreview = false
			m.mode = viewInspect
			m.inspectCursor = firstFocusableNode(inspectNodes(target, m.live))
			m.note = ""
		}
	case "R":
		m.reviewRestoration()
	case "r":
		if m.cur != nil {
			m.spinning = true
			m.note = "refreshing live topology…"
			return handled(m, tea.Batch(loadPlannerLiveCmd(m.cur.name), m.spin.Tick))
		}
	default:
		return unhandled()
	}
	return handled(m, nil)
}

func (m *model) handleInspectKey(msg tea.KeyMsg) keyResult {
	if m.mode != viewInspect {
		return unhandled()
	}
	if m.inspectPreview {
		switch msg.String() {
		case "p":
			m.inspectPreview = false
			m.inspectScroll = 0
			return handled(m, nil)
		case "j", "down":
			m.inspectScroll++
			return handled(m, nil)
		case "k", "up":
			if m.inspectScroll > 0 {
				m.inspectScroll--
			}
			return handled(m, nil)
		case "space", " ", "tab":
			return handled(m, nil)
		}
	}
	nodes := inspectNodes(m.inspectTarget, m.live)
	switch msg.String() {
	case "backspace":
		m.workspacePlan = map[string]workspaceSelection{
			m.inspectTarget.key: {target: m.inspectTarget, selected: planner.Selection{}},
		}
		m.compileRestorationPlan()
		m.note = ""
	case "j", "down":
		m.inspectCursor = moveInspectCursor(nodes, m.inspectCursor, 1)
	case "k", "up":
		m.inspectCursor = moveInspectCursor(nodes, m.inspectCursor, -1)
	case "space", " ", "tab":
		if m.inspectCursor >= 0 && m.inspectCursor < len(nodes) && nodes[m.inspectCursor].focusable {
			selection, ok := m.workspacePlan[m.inspectTarget.key]
			if !ok || selection.selected == nil {
				selection = workspaceSelection{target: m.inspectTarget, selected: planner.Selection{}}
			}
			node := nodes[m.inspectCursor]
			tab := m.inspectTarget.workspace.Tabs[node.tabIndex]
			states := m.targetStates(m.inspectTarget)
			allowed := targetAllowed(m.inspectTarget)
			switch node.kind {
			case tabNode:
				planner.ToggleRestorableTabWithin(tab, selection.selected, states, allowed)
			case paneNode:
				pane := tab.Panes[node.paneIndex]
				if (allowed == nil || allowed[pane.Key]) && states[pane.Key].Availability == planner.Restorable {
					planner.TogglePane(pane.Key, selection.selected)
				}
			case workspaceNode:
				return handled(m, nil)
			}
			m.workspacePlan[m.inspectTarget.key] = selection
			m.compileRestorationPlan()
		}
	case "p":
		m.inspectPreview = !m.inspectPreview
		m.inspectScroll = 0
	case "R":
		m.reviewRestoration()
	case "enter", "l":
	default:
		return unhandled()
	}
	return handled(m, nil)
}

func (m *model) handleCaptureKey(msg tea.KeyMsg) keyResult {
	if m.mode != viewCapture {
		return unhandled()
	}
	nodes := captureNodes(m.captureLive)
	switch msg.String() {
	case "h", "q", "esc":
		m.mode = m.captureReturnMode
		m.captureLive = nil
		m.captureSelection = nil
		m.err = nil
		m.note = ""
	case "r":
		if m.cur != nil {
			m.spinning = true
			m.err = nil
			m.note = "reading live topology…"
			return handled(m, tea.Batch(loadCaptureLiveCmd(m.cur.name), m.spin.Tick))
		}
	case "j", "down":
		if m.captureCursor+1 < len(nodes) {
			m.captureCursor++
		}
	case "k", "up":
		if m.captureCursor > 0 {
			m.captureCursor--
		}
	case "space", " ", "tab":
		m.toggleCaptureNode()
		m.note = ""
	case "a":
		m.captureSelection = planner.SelectSnapshot(m.captureLive)
		m.note = ""
	case "backspace":
		m.captureSelection = planner.Selection{}
		m.note = ""
	case "enter", "l":
		selected, _ := planner.SnapshotSelectedCount(m.captureLive, m.captureSelection)
		if selected == 0 {
			m.note = "select at least one live pane"
			return handled(m, nil)
		}
		m.captureSession = m.cur.name
		m.captureSessions = nil
		m.namingCapture = true
		m.captureInput.Reset()
		m.note = ""
		return handled(m, m.captureInput.Focus())
	default:
		return unhandled()
	}
	return handled(m, nil)
}

func (m *model) handleLegacyPlanKey(msg tea.KeyMsg) keyResult {
	if m.mode != viewPlan || !m.preview {
		return unhandled()
	}
	switch msg.String() {
	case "j", "down":
		if m.previewCursor+1 < m.previewPaneCount() {
			m.previewCursor++
		}
	case "k", "up":
		if m.previewCursor > 0 {
			m.previewCursor--
		}
	default:
		return unhandled()
	}
	return handled(m, nil)
}

func (m *model) handleGlobalKey(msg tea.KeyMsg) keyResult {
	switch msg.String() {
	case "q":
		if m.mode == viewInspect {
			m.mode, m.inspectPreview = viewSnapshots, false
			return handled(m, nil)
		}
		if m.mode == viewPlan {
			m.mode, m.plan, m.preview = viewSnapshots, nil, false
			return handled(m, nil)
		}
		if m.mode == viewSnapshots {
			m.mode, m.cur = viewSessions, nil
			return handled(m, nil)
		}
		return handled(m, tea.Quit)
	case "esc":
		if m.mode == viewInspect {
			m.mode, m.inspectPreview = viewSnapshots, false
			return handled(m, nil)
		}
		if m.mode == viewPlan {
			if m.preview {
				m.preview = false
				return handled(m, nil)
			}
			m.mode, m.plan = viewSnapshots, nil
		} else if m.mode == viewSnapshots {
			m.mode, m.cur = viewSessions, nil
		} else {
			return handled(m, tea.Quit)
		}
	case "enter", "l":
		model, cmd := m.digIn()
		return handled(model, cmd)
	case "h":
		model, cmd := m.back()
		return handled(model, cmd)
	case "space", " ", "tab":
		if m.mode == viewPlan && !m.preview && m.plan != nil && m.plan.plan != nil {
			if item, ok := m.planList.SelectedItem().(planItem); ok {
				i := item.index
				switch m.plan.plan.Panes[i].Action {
				case resume.Replace, resume.Relaunch, resume.Resurrect:
					m.plan.sel[i] = !m.plan.sel[i]
					m.syncPlanList()
				}
			}
		}
	case "a":
		if m.mode == viewPlan && m.plan != nil && m.plan.plan != nil {
			all := true
			for i, pane := range m.plan.plan.Panes {
				switch pane.Action {
				case resume.Replace, resume.Relaunch, resume.Resurrect:
					if !m.plan.sel[i] {
						all = false
					}
				}
			}
			for i, pane := range m.plan.plan.Panes {
				switch pane.Action {
				case resume.Replace, resume.Relaunch, resume.Resurrect:
					m.plan.sel[i] = !all
				}
			}
			m.syncPlanList()
		}
	case "p":
		if m.mode == viewPlan && m.plan != nil {
			m.preview = !m.preview
			if m.preview {
				m.previewCursor = 0
				m.previewScroll = 0
			}
		}
	case "y":
		if m.mode == viewPlan && m.plan != nil && m.plan.snap != nil {
			if len(m.restorationAgents()) == 0 {
				m.note = "select at least one repair (space)"
				return handled(m, nil)
			}
			m.confirm = "restore"
		}
	case "x":
		if m.mode == viewSessions {
			m.selectCurrentSession()
		}
		if m.mode == viewCapture {
			if m.spinning || m.captureLive == nil {
				m.note = "wait for the live topology before capture & stop"
				return handled(m, nil)
			}
			selected, total := planner.SnapshotSelectedCount(m.captureLive, m.captureSelection)
			if selected != total {
				m.note = "capture & stop requires every live pane to be selected"
				return handled(m, nil)
			}
		}
		if (m.mode == viewSessions || m.mode == viewSnapshots || m.mode == viewInspect || m.mode == viewCapture) &&
			m.cur != nil && m.cur.running {
			return handled(m, m.startArchiveReview(""))
		}
		m.note = "selected session is not running"
	case "C":
		var sessions []string
		switch m.mode {
		case viewSessions:
			for _, session := range m.sessions {
				if session.running {
					sessions = append(sessions, session.name)
				}
			}
		case viewSnapshots:
			if m.cur != nil && m.cur.running {
				sessions = append(sessions, m.cur.name)
			}
		}
		if len(sessions) == 0 {
			m.note = "no running sessions to capture"
			return handled(m, nil)
		}
		m.captureSession = ""
		m.captureSessions = sessions
		m.namingCapture = true
		m.captureInput.Reset()
		m.note = ""
		return handled(m, m.captureInput.Focus())
	case "c":
		if m.mode == viewSessions {
			m.selectCurrentSession()
		}
		if (m.mode != viewSessions && m.mode != viewSnapshots && m.mode != viewInspect) || m.cur == nil {
			return handled(m, nil)
		}
		if !m.cur.running {
			m.note = "session is not running; nothing to capture"
			return handled(m, nil)
		}
		m.captureReturnMode = m.mode
		m.mode = viewCapture
		m.captureLive = nil
		m.captureSelection = planner.Selection{}
		m.captureCursor = 0
		m.err = nil
		m.spinning = true
		m.note = "reading live topology…"
		return handled(m, tea.Batch(loadCaptureLiveCmd(m.cur.name), m.spin.Tick))
	case "r":
		m.note = ""
		if m.mode == viewPlan && m.plan != nil {
			m.plan, m.spinning, m.preview = nil, true, false
			return handled(m, tea.Batch(loadPlanCmd(m.cur.name, m.curSnap), m.spin.Tick))
		}
		if m.mode == viewSessions {
			m.refreshSessions()
		}
	default:
		return unhandled()
	}
	return handled(m, nil)
}

func (m *model) selectCurrentSession() {
	item, ok := m.sessList.SelectedItem().(sessionItem)
	if !ok {
		return
	}
	for i := range m.sessions {
		if m.sessions[i].name == item.row.name {
			m.cur = &m.sessions[i]
			return
		}
	}
}

func (m *model) updateActiveList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.mode {
	case viewSessions:
		m.sessList, cmd = m.sessList.Update(msg)
	case viewSnapshots:
		m.snapList, cmd = m.snapList.Update(msg)
	case viewPlan:
		if !m.preview && m.plan != nil && m.plan.plan != nil {
			m.planList, cmd = m.planList.Update(msg)
		}
	}
	return m, cmd
}
