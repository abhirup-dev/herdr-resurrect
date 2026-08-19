// Package tui is the browse picker: sessions -> snapshots -> plan with
// per-agent restore selection and a layout preview. Built on bubbles
// list and spinner primitives.
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/abhirupdas/herdr-archive/internal/brands"
	"github.com/abhirupdas/herdr-archive/internal/capture"
	"github.com/abhirupdas/herdr-archive/internal/kitty"
	"github.com/abhirupdas/herdr-archive/internal/manifest"
	"github.com/abhirupdas/herdr-archive/internal/planner"
	"github.com/abhirupdas/herdr-archive/internal/resume"
	"github.com/abhirupdas/herdr-archive/internal/strategy"
)

var (
	styTitle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	styDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styOK      = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styWarn    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styBad     = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styNice    = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	styVerdict = map[string]lipgloss.Style{
		"KEEP-NATIVE": styOK,
		"REPLACE":     styBad,
		"RELAUNCH":    styWarn,
		"RESURRECT":   styNice,
		"SHELL":       styDim,
	}
)

type mode int

const (
	viewSessions mode = iota
	viewSnapshots
	viewInspect
	// viewPlan is the unreachable legacy single-snapshot diff UI, retained
	// temporarily as a reference while the additive planner settles.
	viewPlan
)

type sessionRow struct {
	name    string
	running bool
	snaps   []string
	last    string
	latest  *manifest.Snapshot
	agents  int

	// staleness, computed against the live session: how many agents the
	// latest snapshot names are still alive (liveAgents == -1 = unknown,
	// e.g. the query failed or the session has no snapshot).
	liveAgents int
}

type snapshotRow struct {
	path string
	snap *manifest.Snapshot
	err  error
}

type workspaceTarget struct {
	key       string
	workspace manifest.Workspace
	snapshot  *manifest.Snapshot
	path      string
	isLast    bool
}

type workspaceSelection struct {
	target   workspaceTarget
	selected planner.Selection
}

type inspectNode struct {
	tabIndex  int
	paneIndex int // -1 means the tab node
	focusable bool
}

type planData struct {
	snap    *manifest.Snapshot
	path    string
	plan    *resume.Plan
	planErr string
	running bool
	sel     []bool // per plan.Panes index
}

type model struct {
	mode            mode
	width           int
	height          int
	sessions        []sessionRow
	snaps           []snapshotRow
	targets         []workspaceTarget
	workspacePlan   map[string]workspaceSelection
	live            *manifest.Snapshot
	compiled        *planner.Plan
	plannerCursor   int
	inspectTarget   workspaceTarget
	inspectCursor   int
	inspectPreview  bool
	sessList        list.Model
	snapList        list.Model
	planList        list.Model
	captureInput    textinput.Model
	captureSession  string
	captureSessions []string
	namingCapture   bool
	spin            spinner.Model
	spinning        bool
	preview         bool
	previewCursor   int
	confirm         string
	cur             *sessionRow
	curSnap         string
	plan            *planData
	note            string
	err             error
}

type planMsg struct {
	pd  *planData
	err error
}
type refreshedMsg struct {
	err  error
	note string
}

type plannerLiveMsg struct {
	live *manifest.Snapshot
	err  error
}

// icons is the per-kind kitty image id registry, assigned once in Run before
// the program starts. Zero value (no kitty protocol) makes icon() fall back
// to the colored glyph.
var icons kitty.Icons

func Run() error {
	captureInput := textinput.New()
	captureInput.Placeholder = "optional — defaults to date and time"
	captureInput.Prompt = ""
	captureInput.CharLimit = 72
	captureInput.SetWidth(48)
	m := &model{
		width:         96,
		height:        24,
		spin:          spinner.New(spinner.WithSpinner(spinner.Dot)),
		captureInput:  captureInput,
		workspacePlan: map[string]workspaceSelection{},
	}
	if err := m.loadSessions(); err != nil {
		return err
	}
	if len(m.sessions) == 0 {
		return fmt.Errorf("no herdr sessions found")
	}
	m.sessList = newList()
	m.snapList = newSnapshotList()
	m.planList = newList()
	m.planList.SetFilteringEnabled(false)
	m.syncSessionList()
	m.selectInvokingSession()
	// brand images: transmit the embedded logos quietly and create virtual
	// placements; rows then reference them via unicode-placeholder cells,
	// which travel through bubbletea frames as ordinary styled text.
	// Inside a herdr pane the multiplexer eats the graphics protocol (its
	// experimental kitty_graphics flag does not forward pane graphics in
	// 0.8.x), so placeholders would render as blank cells — glyphs there.
	// HERDR_ARCHIVE_FORCE_IMAGES=1 overrides for testing a future herdr.
	if kitty.Capable() && (os.Getenv("HERDR_ENV") != "1" || os.Getenv("HERDR_ARCHIVE_FORCE_IMAGES") == "1") {
		icons = kitty.Setup(os.Stdout, brands.Logo, brands.Kinds)
		defer fmt.Fprint(os.Stdout, kitty.DeleteAll())
	}
	_, err := tea.NewProgram(m).Run()
	return err
}

// icon renders the kind's mark: the real brand image as two placeholder
// cells on kitty-graphics terminals, the colored glyph inside herdr panes
// (herdr eats the graphics protocol) and on terminals with no image
// protocol at all.
func icon(kind, title string) string {
	if id, ok := icons.Icon(kind, 1); ok {
		return kitty.Placeholder(id, 2, 1)[0]
	}
	return strategy.GlyphStyled(kind, title)
}

func newList() list.Model {
	l := list.New([]list.Item{}, list.NewDefaultDelegate(), 88, 10)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)
	return l
}

func newSnapshotList() list.Model {
	delegate := list.NewDefaultDelegate()
	delegate.SetHeight(3)
	delegate.SetSpacing(1)
	l := list.New([]list.Item{}, delegate, 88, 10)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)
	return l
}

func (m *model) setListBackground(isDark bool) {
	styles := list.NewDefaultItemStyles(isDark)
	if !isDark {
		styles.NormalDesc = styles.NormalDesc.Foreground(lipgloss.Color("#666666"))
		styles.DimmedTitle = styles.DimmedTitle.Foreground(lipgloss.Color("#777777"))
		styles.DimmedDesc = styles.DimmedDesc.Foreground(lipgloss.Color("#777777"))
	}

	standard := list.NewDefaultDelegate()
	standard.Styles = styles
	m.sessList.SetDelegate(standard)
	m.planList.SetDelegate(standard)

	snapshots := list.NewDefaultDelegate()
	snapshots.Styles = styles
	snapshots.SetHeight(3)
	snapshots.SetSpacing(1)
	m.snapList.SetDelegate(snapshots)
}

// ---- data -------------------------------------------------------------

func (m *model) loadSessions() error {
	sess, err := capture.Sessions()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, s := range sess {
		seen[s.Name] = true
	}
	entries, _ := os.ReadDir(manifest.DefaultRoot())
	for _, e := range entries {
		if !e.IsDir() || seen[e.Name()] {
			continue
		}
		sess = append(sess, capture.SessionInfo{Name: e.Name()})
	}
	m.sessions = nil
	for i := range sess {
		row := sessionRow{name: sess[i].Name, running: sess[i].Running, liveAgents: -1}
		dir := manifest.Dir("", row.name)
		row.last, _ = filepath.EvalSymlinks(filepath.Join(dir, "last"))
		matches, _ := filepath.Glob(filepath.Join(dir, "herdr_*.json"))
		sort.Sort(sort.Reverse(sort.StringSlice(matches)))
		row.snaps = matches
		if len(matches) > 0 {
			if snap, err := manifest.Load(matches[0]); err == nil {
				row.latest, row.agents = snap, len(snap.AgentPanes())
			}
		}
		if row.running && row.latest != nil {
			if live, err := capture.LiveAgents(row.name); err == nil {
				row.liveAgents = 0
				for _, p := range row.latest.AgentPanes() {
					if p.Name == "" {
						continue
					}
					for _, l := range live {
						if l == p.Name {
							row.liveAgents++
							break
						}
					}
				}
			}
		}
		m.sessions = append(m.sessions, row)
	}
	return nil
}

func (m *model) refreshSessions() {
	name := ""
	if m.mode == viewSessions {
		if i, ok := m.sessList.SelectedItem().(sessionItem); ok {
			name = i.row.name
		}
	}
	if m.cur != nil {
		name = m.cur.name
	}
	_ = m.loadSessions()
	m.syncSessionList()
	if name != "" {
		for i, item := range m.sessList.Items() {
			if s, ok := item.(sessionItem); ok && s.row.name == name {
				m.sessList.Select(i)
				if m.cur != nil {
					m.cur = &m.sessions[i]
					m.loadSnaps()
				}
				break
			}
		}
	}
}

func (m *model) loadSnaps() {
	m.snaps = nil
	m.targets = nil
	if m.cur == nil {
		return
	}
	var workspaceOrder []string
	grouped := map[string][]workspaceTarget{}
	for i, path := range m.cur.snaps {
		if i == 3 {
			break
		}
		row := snapshotRow{path: path}
		row.snap, row.err = manifest.Load(path)
		m.snaps = append(m.snaps, row)
		if row.err != nil || row.snap == nil {
			continue
		}
		for _, workspace := range row.snap.Workspaces {
			key := workspaceKey(workspace)
			if _, ok := grouped[key]; !ok {
				workspaceOrder = append(workspaceOrder, key)
			}
			grouped[key] = append(grouped[key], workspaceTarget{
				key:       key,
				workspace: workspace,
				snapshot:  row.snap,
				path:      path,
				isLast:    path == m.cur.last,
			})
		}
	}
	for _, key := range workspaceOrder {
		m.targets = append(m.targets, grouped[key]...)
	}
	if m.plannerCursor >= len(m.targets) {
		m.plannerCursor = max(0, len(m.targets)-1)
	}
	m.syncSnapList()
}

func loadPlanCmd(session, snapPath string) tea.Cmd {
	return func() tea.Msg {
		pd := &planData{path: snapPath}
		snap, err := manifest.Load(snapPath)
		if err != nil {
			return planMsg{err: err}
		}
		pd.snap = snap
		// Native agent restoration is asynchronous. Wait for the session to
		// settle before taking the live capture used by the restoration plan.
		resume.Settle(session, 6*time.Second)
		live, err := capture.Session(capture.Options{Session: session})
		pd.running = err == nil
		if err != nil {
			pd.planErr = fmt.Sprintf("%v", err)
			return planMsg{pd: pd}
		}
		pd.plan = resume.Diff(snap, live)
		pd.sel = make([]bool, len(pd.plan.Panes))
		for i, pp := range pd.plan.Panes {
			switch pp.Action {
			case resume.Replace, resume.Relaunch, resume.Resurrect:
				pd.sel[i] = true
			}
		}
		return planMsg{pd: pd}
	}
}

func captureSessionsCmd(sessions []string, name string) tea.Cmd {
	targets := append([]string(nil), sessions...)
	return func() tea.Msg {
		if len(targets) == 0 {
			return refreshedMsg{err: fmt.Errorf("no running sessions to capture")}
		}
		self, err := os.Executable()
		if err != nil {
			return refreshedMsg{err: err}
		}
		var failures []string
		captured := 0
		for _, session := range targets {
			cmd := exec.Command(self, "capture", "--session", session, "--name", name)
			if err := cmd.Run(); err != nil {
				failures = append(failures, session)
				continue
			}
			captured++
		}
		if len(failures) > 0 {
			return refreshedMsg{err: fmt.Errorf("captured %d/%d sessions; failed: %s",
				captured, len(targets), strings.Join(failures, ", "))}
		}
		note := "captured “" + name + "”"
		if len(targets) > 1 {
			note = fmt.Sprintf("captured “%s” across %d sessions", name, len(targets))
		}
		return refreshedMsg{note: note}
	}
}

// ---- list sync ---------------------------------------------------------

type sessionItem struct{ row sessionRow }

func (i sessionItem) Title() string {
	badge := styBad.Render("stopped")
	switch {
	case i.row.stale():
		badge = styOK.Render("running") + styWarn.Render("/stale")
	case i.row.running:
		badge = styOK.Render("running")
	}
	return fmt.Sprintf("%s  %s", i.row.name, badge)
}
func (i sessionItem) Description() string {
	if i.row.latest == nil {
		return "0 agents"
	}
	return fmt.Sprintf("%s · %d %s",
		relTime(i.row.latest.CreatedAt.Local()), i.row.agents, plural(i.row.agents, "agent"))
}

// stale: the server is up but captured agents have been closed.
func (r *sessionRow) stale() bool {
	return r.running && r.liveAgents >= 0 && r.liveAgents < r.agents
}
func (i sessionItem) FilterValue() string { return i.row.name }

type snapshotItem struct {
	row    snapshotRow
	isLast bool
}

func (i snapshotItem) Title() string {
	badge := styDim.Render("○")
	if i.isLast {
		badge = styOK.Render("● CURRENT")
	}
	if i.row.err != nil || i.row.snap == nil {
		return badge + "  unreadable snapshot"
	}
	created := i.row.snap.CreatedAt.Local()
	return fmt.Sprintf("%s  %s %s", badge, snapshotWhen(created), styDim.Render("· "+relTime(created)))
}
func (i snapshotItem) Description() string {
	if i.row.err != nil {
		return styBad.Render(i.row.err.Error())
	}
	if i.row.snap == nil {
		return ""
	}
	_, size, agents := snapshotStats(i.row.snap)
	tabs := 0
	for _, workspace := range i.row.snap.Workspaces {
		tabs += len(workspace.Tabs)
	}
	transcripts := humanBytes(size) + " transcripts"
	if size == 0 {
		transcripts = "transcripts unavailable"
	}
	metrics := fmt.Sprintf("%d %s · %d %s · %d %s · %s", agents, plural(agents, "agent"),
		len(i.row.snap.Workspaces), plural(len(i.row.snap.Workspaces), "workspace"),
		tabs, plural(tabs, "tab"), transcripts)
	return metrics + "\n" + agentRoster(i.row.snap)
}
func (i snapshotItem) FilterValue() string {
	return filepath.Base(i.row.path)
}

func (m *model) syncSessionList() {
	items := make([]list.Item, 0, len(m.sessions))
	for _, s := range m.sessions {
		items = append(items, sessionItem{row: s})
	}
	m.sessList.SetItems(items)
}

func invokingSessionName() string {
	socket := filepath.Clean(os.Getenv("HERDR_SOCKET_PATH"))
	if socket == "." || socket == "" {
		return ""
	}
	dir := filepath.Dir(socket)
	if filepath.Base(filepath.Dir(dir)) == "sessions" {
		return filepath.Base(dir)
	}
	if filepath.Base(socket) == "herdr.sock" {
		return "default"
	}
	return ""
}

func (m *model) selectInvokingSession() {
	name := invokingSessionName()
	if name == "" {
		return
	}
	for index, session := range m.sessions {
		if session.name == name {
			m.sessList.Select(index)
			return
		}
	}
}

func (m *model) syncSnapList() {
	items := make([]list.Item, 0, len(m.snaps))
	for _, r := range m.snaps {
		items = append(items, snapshotItem{row: r, isLast: m.cur != nil && r.path == m.cur.last})
	}
	m.snapList.SetItems(items)
}

type planItem struct {
	index    int
	pane     resume.PanePlan
	selected bool
}

func (i planItem) Title() string {
	box := "[ ]"
	if i.selected {
		box = "[x]"
	}
	if i.pane.Manifest.Agent == "" {
		box = " · "
	}
	kind := i.pane.Manifest.Agent
	if kind == "" {
		kind = "shell"
	}
	provider := strategy.ProviderLabel(i.pane.Manifest.Agent, i.pane.Manifest.Env)
	context := ""
	if provider != "" && provider != kind {
		context = styDim.Render(" · " + provider)
	}
	action := string(i.pane.Action)
	label := actionLabel(i.pane.Action)
	style, ok := styVerdict[action]
	if !ok {
		style = styDim
	}
	return fmt.Sprintf("%s %s %s%s  %s", box, icon(kind, i.pane.Manifest.Title),
		i.pane.Manifest.Key, context, style.Render(label))
}

func (i planItem) Description() string {
	reason := i.pane.Reason
	switch i.pane.Action {
	case resume.ShellKeep:
		reason = "shell preserved at captured cwd"
	case resume.KeepNative:
		if reason == "" {
			reason = "live environment matches snapshot"
		}
	}
	if size := strategy.TranscriptSize(i.pane.Manifest.Agent, i.pane.Manifest.SID, i.pane.Manifest.Env); size > 0 {
		reason += " · " + humanBytes(size) + " transcript"
	}
	return reason
}

func (i planItem) FilterValue() string {
	return i.pane.Manifest.Key + " " + i.pane.Manifest.Agent + " " +
		strategy.ProviderLabel(i.pane.Manifest.Agent, i.pane.Manifest.Env) + " " +
		string(i.pane.Action) + " " + i.pane.Reason
}

func (m *model) syncPlanList() {
	if m.plan == nil || m.plan.plan == nil {
		m.planList.SetItems(nil)
		return
	}
	cursor := m.planList.Index()
	items := make([]list.Item, 0, len(m.plan.plan.Panes))
	// Put decisions first, healthy panes second, and keep shell-only details in
	// the summary/preview where they are useful without cluttering restoration.
	for _, actionable := range []bool{true, false} {
		for i, pane := range m.plan.plan.Panes {
			isActionable := pane.Action == resume.Replace || pane.Action == resume.Relaunch || pane.Action == resume.Resurrect
			if pane.Action == resume.ShellKeep || isActionable != actionable {
				continue
			}
			items = append(items, planItem{index: i, pane: pane, selected: m.plan.sel[i]})
		}
	}
	m.planList.SetItems(items)
	if cursor >= 0 && cursor < len(items) {
		m.planList.Select(cursor)
	}
}

func selectedPaneCount(selection workspaceSelection, live *manifest.Snapshot) (selected, total, liveCount int) {
	return planner.RestorableCount(selection.target.workspace, selection.selected, planner.Analyze(selection.target.workspace, live))
}

func selectWholeTarget(target workspaceTarget, live *manifest.Snapshot) workspaceSelection {
	states := planner.Analyze(target.workspace, live)
	return workspaceSelection{target: target, selected: planner.SelectRestorable(target.workspace, states)}
}

func mapTargetSelection(previous workspaceSelection, target workspaceTarget, live *manifest.Snapshot) workspaceSelection {
	states := planner.Analyze(target.workspace, live)
	return workspaceSelection{target: target, selected: planner.MapRestorable(previous.selected, target.workspace, states)}
}

func selectionMarker(selection workspaceSelection, live *manifest.Snapshot) string {
	selected, total, _ := selectedPaneCount(selection, live)
	switch {
	case selected == 0:
		return styTitle.Render("◇")
	case selected == total:
		return styTitle.Render("◆")
	default:
		return styWarn.Render("◐")
	}
}

func (m *model) selectedWorkspaceTargets() []workspaceSelection {
	seen := map[string]bool{}
	var selected []workspaceSelection
	for _, target := range m.targets {
		if seen[target.key] {
			continue
		}
		planned, ok := m.workspacePlan[target.key]
		if !ok {
			continue
		}
		count, _, _ := selectedPaneCount(planned, m.live)
		if count == 0 {
			continue
		}
		selected = append(selected, planned)
		seen[target.key] = true
	}
	return selected
}

func loadPlannerLiveCmd(session string) tea.Cmd {
	return func() tea.Msg {
		resume.Settle(session, 2*time.Second)
		live, err := capture.Session(capture.Options{Session: session})
		return plannerLiveMsg{live: live, err: err}
	}
}

func (m *model) targetStates(target workspaceTarget) map[string]planner.PaneState {
	return planner.Analyze(target.workspace, m.live)
}

func (m *model) compileRestorationPlan() {
	if m.live == nil {
		m.compiled = nil
		return
	}
	var targets []planner.Target
	for _, selection := range m.selectedWorkspaceTargets() {
		targets = append(targets, planner.Target{
			WorkspaceKey: selection.target.key,
			SnapshotName: snapshotName(selection.target.snapshot),
			SnapshotPath: selection.target.path,
			Workspace:    selection.target.workspace,
			Selected:     selection.selected,
		})
	}
	session := ""
	if m.cur != nil {
		session = m.cur.name
	}
	m.compiled = planner.Compile(session, targets, m.live)
}

func firstFocusableNode(nodes []inspectNode) int {
	for index, node := range nodes {
		if node.focusable {
			return index
		}
	}
	return -1
}

func moveInspectCursor(nodes []inspectNode, current, delta int) int {
	for next := current + delta; next >= 0 && next < len(nodes); next += delta {
		if nodes[next].focusable {
			return next
		}
	}
	return current
}

func inspectNodes(target workspaceTarget, live *manifest.Snapshot) []inspectNode {
	states := planner.Analyze(target.workspace, live)
	var nodes []inspectNode
	for tabIndex, tab := range target.workspace.Tabs {
		tabFocusable := false
		for _, pane := range tab.Panes {
			if states[pane.Key].Availability == planner.Restorable {
				tabFocusable = true
				break
			}
		}
		nodes = append(nodes, inspectNode{tabIndex: tabIndex, paneIndex: -1, focusable: tabFocusable})
		for paneIndex, pane := range tab.Panes {
			nodes = append(nodes, inspectNode{tabIndex: tabIndex, paneIndex: paneIndex,
				focusable: states[pane.Key].Availability == planner.Restorable})
		}
	}
	return nodes
}

func triStateMarker(selected, total int) string {
	switch {
	case selected == 0:
		return styDim.Render("○")
	case selected == total:
		return styTitle.Render("◆")
	default:
		return styWarn.Render("◐")
	}
}

func (m *model) inspectorTreeLines(preview bool) []string {
	selection, ok := m.workspacePlan[m.inspectTarget.key]
	if !ok {
		selection = workspaceSelection{target: m.inspectTarget, selected: planner.Selection{}}
	}
	states := m.targetStates(m.inspectTarget)
	var nodes []treeNode
	nodeIndex := 0
	for _, tab := range m.inspectTarget.workspace.Tabs {
		tabNodeIndex := nodeIndex
		nodeIndex++
		selectedCount, restorableCount, liveCount := 0, 0, 0
		for _, pane := range tab.Panes {
			if states[pane.Key].Availability == planner.Live {
				liveCount++
				continue
			}
			restorableCount++
			if selection.selected[pane.Key] {
				selectedCount++
			}
		}

		tabName := tab.Label
		if tabName == "" {
			tabName = tab.ID
		}
		marker := triStateMarker(selectedCount, restorableCount)
		if restorableCount == 0 {
			marker = styOK.Render("●")
		}
		layout := layoutLabel(tab)
		if preview {
			layout = filteredLayoutLabel(tab, selection.selected)
		}
		tabNode := treeNode{
			Focused: !preview && tabNodeIndex == m.inspectCursor,
			Marker:  marker,
			Label:   styTreeLabel.Render(clipLabel(tabName, 28)),
			Metadata: []string{
				treeNeutral(layout),
			},
		}
		if liveCount > 0 && !preview {
			tabNode.Metadata = append(tabNode.Metadata, treeLive(fmt.Sprintf("%d live", liveCount)))
		}

		for _, pane := range tab.Panes {
			paneNodeIndex := nodeIndex
			nodeIndex++
			if preview && !selection.selected[pane.Key] {
				continue
			}
			kind, name := pane.Agent, pane.Key
			if kind == "" {
				kind, name = "shell", "shell"
			}
			paneNode := treeNode{
				Focused: !preview && paneNodeIndex == m.inspectCursor,
				Label:   icon(kind, pane.Title) + " " + styTreeLabel.Render(name),
			}
			if preview {
				paneNode.Metadata = []string{treeNeutral(providerDisplay(pane.Agent, pane.Env))}
				tabNode.Children = append(tabNode.Children, paneNode)
				continue
			}
			state := states[pane.Key]
			if state.Availability == planner.Live {
				paneNode.Marker = styOK.Render("●")
				paneNode.Metadata = []string{treeLive("live"), treeNeutral(providerDisplay(pane.Agent, pane.Env))}
				if len(state.Drift) > 0 {
					paneNode.Metadata = append(paneNode.Metadata, treeWarning("env drift"))
				}
			} else {
				paneNode.Marker = styDim.Render("○")
				if selection.selected[pane.Key] {
					paneNode.Marker = styTitle.Render("◆")
				}
				paneNode.Metadata = []string{treeMissing("missing"), treeNeutral(providerDisplay(pane.Agent, pane.Env))}
			}
			tabNode.Children = append(tabNode.Children, paneNode)
		}
		if preview && selectedCount == 0 {
			continue
		}
		nodes = append(nodes, tabNode)
	}
	if len(nodes) == 0 {
		return []string{styDim.Render("Nothing selected from this workspace target.")}
	}
	return renderTree(nodes, treeOptions{Controls: !preview, Width: m.contentWidth()})
}

func (m *model) inspectorView() string {
	selection := m.workspacePlan[m.inspectTarget.key]
	selected, total, liveCount := selectedPaneCount(selection, m.live)
	var out []string
	if m.inspectPreview {
		out = append(out, styTitle.Render("SELECTED TOPOLOGY"),
			treeMetadata(treeNeutral(fmt.Sprintf("%d/%d selected", selected, total)), treeNeutral("branches compacted")), "")
		out = append(out, m.inspectorTreeLines(true)...)
		return strings.Join(out, "\n")
	}
	summary := []string{
		treeNeutral(snapshotName(m.inspectTarget.snapshot)),
		treeNeutral(fmt.Sprintf("%d/%d selected", selected, total)),
	}
	if liveCount > 0 {
		summary = append(summary, treeLive(fmt.Sprintf("%d live", liveCount)))
	}
	out = append(out, styTitle.Render("SELECT TABS / PANES"), treeMetadata(summary...), "")
	out = append(out, m.inspectorTreeLines(false)...)
	return strings.Join(out, "\n")
}

func (m *model) plannerView() string {
	available := m.contentWidth()
	m.compileRestorationPlan()
	selected := m.selectedWorkspaceTargets()
	operationCount := 0
	if m.compiled != nil {
		operationCount = len(m.compiled.Operations)
	}

	showPlan := len(selected) > 0
	split := showPlan && available >= 91
	leftWidth, rightWidth := available, available
	if split {
		leftWidth = max(42, min(available*44/100, available-49))
		rightWidth = available - leftWidth - 3
		if rightWidth < 46 {
			split = false
			leftWidth, rightWidth = available, available
		}
	}

	left := []string{styTitle.Render("WORKSPACES / TARGETS"), ""}
	lastWorkspace := ""
	for index, target := range m.targets {
		if target.key != lastWorkspace {
			if lastWorkspace != "" {
				left = append(left, "")
			}
			left = append(left, styTitle.Render(fitLine(target.key, leftWidth)))
			lastWorkspace = target.key
		}
		marker := styDim.Render("○")
		if planned, ok := m.workspacePlan[target.key]; ok && planned.target.path == target.path {
			marker = selectionMarker(planned, m.live)
		}
		badge := ""
		if target.isLast {
			if planner.ExactWorkspaceMatch(target.workspace, m.live) {
				badge = " " + treeLive("CURRENT")
			} else {
				badge = " " + treeMissing("STALE")
			}
		}
		nameBudget := max(8, leftWidth-ansi.StringWidth(marker)-ansi.StringWidth(badge)-2)
		targetLine := marker + " " + styTreeLabel.Render(fitLine(snapshotName(target.snapshot), nameBudget)) + badge
		if index == m.plannerCursor {
			targetLine = highlightLine(targetLine, leftWidth)
		}
		left = append(left, targetLine)

		agents, tabs, size := workspaceStats(target.workspace)
		left = append(left, "  "+treeMetadata(
			treeNeutral(relTime(target.snapshot.CreatedAt.Local())),
			treeNeutral(fmt.Sprintf("%d %s", agents, plural(agents, "agent"))),
			treeNeutral(fmt.Sprintf("%d %s", tabs, plural(tabs, "tab"))),
		))
		if m.live != nil {
			states := m.targetStates(target)
			_, missing, liveCount := planner.RestorableCount(target.workspace, nil, states)
			liveMeta := treeNeutral(fmt.Sprintf("%d live", liveCount))
			if liveCount > 0 {
				liveMeta = treeLive(fmt.Sprintf("%d live", liveCount))
			}
			missingMeta := treeNeutral("0 missing")
			if missing > 0 {
				missingMeta = treeMissing(fmt.Sprintf("%d missing", missing))
			}
			left = append(left, "  "+treeMetadata(liveMeta, missingMeta, treeNeutral(humanBytes(size))))
		} else {
			left = append(left, "  "+treeNeutral(humanBytes(size)))
		}
	}
	if len(m.targets) == 0 {
		left = append(left, styDim.Render("No workspace targets in the latest snapshots."))
	}

	if !showPlan {
		if m.live == nil {
			left = append(left, "", styWarn.Render("Live topology unavailable."),
				styDim.Render("Press r to refresh before selecting or restoring."))
		}
		return strings.Join(fitLines(left, available), "\n")
	}

	right := []string{styTitle.Render("RESTORATION PLAN"),
		styDim.Render(fmt.Sprintf("%d %s · %d additive %s", len(selected), plural(len(selected), "workspace"),
			operationCount, plural(operationCount, "operation"))), ""}
	if m.live == nil {
		right = append(right, styWarn.Render("Live topology unavailable."),
			styDim.Render("Press r to refresh before selecting or restoring."))
	}
	var workspaceNodes []treeNode
	for _, selection := range selected {
		target := selection.target
		workspaceNode := treeNode{
			Label: styTitle.Render(target.key),
			Metadata: []string{
				treeNeutral(snapshotName(target.snapshot)),
				treeNeutral(relTime(target.snapshot.CreatedAt.Local())),
			},
		}
		for _, tab := range target.workspace.Tabs {
			selectedCount := 0
			for _, pane := range tab.Panes {
				if selection.selected[pane.Key] {
					selectedCount++
				}
			}
			if selectedCount == 0 {
				continue
			}
			tabName := tab.Label
			if tabName == "" {
				tabName = tab.ID
			}
			tabNode := treeNode{
				Label:    styTreeLabel.Render(tabName),
				Metadata: []string{treeNeutral(filteredLayoutLabel(tab, selection.selected))},
			}
			for _, pane := range tab.Panes {
				if !selection.selected[pane.Key] {
					continue
				}
				kind, name := pane.Agent, pane.Key
				if kind == "" {
					kind, name = "shell", "shell"
				}
				tabNode.Children = append(tabNode.Children, treeNode{
					Label: icon(kind, pane.Title) + " " + styTreeLabel.Render(name),
					Metadata: []string{
						treeAdded("add"),
						treeNeutral(providerDisplay(pane.Agent, pane.Env)),
					},
				})
			}
			workspaceNode.Children = append(workspaceNode.Children, tabNode)
		}
		workspaceNodes = append(workspaceNodes, workspaceNode)
	}
	right = append(right, renderTree(workspaceNodes, treeOptions{Width: rightWidth})...)

	if !split {
		return strings.Join(fitLines(left, available), "\n") + "\n\n" + strings.Join(fitLines(right, available), "\n")
	}
	return joinColumns(left, right, leftWidth, rightWidth)
}

func topologyStyle(change planner.Change) lipgloss.Style {
	switch change {
	case planner.Added:
		return styOK
	case planner.Expanded:
		return styWarn
	default:
		return styDim
	}
}

func topologyChangeMetadata(change planner.Change) []string {
	switch change {
	case planner.Added:
		return []string{treeAdded("added")}
	case planner.Expanded:
		return []string{treeExpanded("expanded")}
	default:
		return nil
	}
}

func topologyLines(topology planner.Topology, width int) []string {
	if len(topology.Workspaces) == 0 {
		return []string{styDim.Render("(empty session)")}
	}
	var workspaceNodes []treeNode
	for _, workspace := range topology.Workspaces {
		workspaceNode := treeNode{
			Label:    topologyStyle(workspace.Change).Bold(true).Render(workspace.Key),
			Metadata: topologyChangeMetadata(workspace.Change),
		}
		for _, tab := range workspace.Tabs {
			tabNode := treeNode{
				Label:    topologyStyle(tab.Change).Bold(true).Render(tab.Key),
				Metadata: topologyChangeMetadata(tab.Change),
			}
			for _, pane := range tab.Panes {
				kind, name := pane.Pane.Agent, pane.Key
				if kind == "" {
					kind, name = "shell", "shell"
				}
				metadata := topologyChangeMetadata(pane.Change)
				metadata = append(metadata, treeNeutral(providerDisplay(pane.Pane.Agent, pane.Pane.Env)))
				tabNode.Children = append(tabNode.Children, treeNode{
					Label:    topologyStyle(pane.Change).Bold(true).Render(icon(kind, pane.Pane.Title) + " " + name),
					Metadata: metadata,
				})
			}
			workspaceNode.Children = append(workspaceNode.Children, tabNode)
		}
		workspaceNodes = append(workspaceNodes, workspaceNode)
	}
	return renderTree(workspaceNodes, treeOptions{Width: width})
}

func (m *model) restorationConfirmationBody() string {
	m.compileRestorationPlan()
	if m.compiled == nil {
		return styBad.Render("No restoration plan is available.")
	}
	available := max(40, m.contentWidth())
	body := ""
	if available >= 90 {
		leftWidth := (available - 3) / 2
		rightWidth := available - leftWidth - 3
		before := append([]string{styTitle.Render("BEFORE")}, topologyLines(m.compiled.Before, leftWidth)...)
		after := append([]string{styTitle.Render("AFTER")}, topologyLines(m.compiled.After, rightWidth)...)
		body = joinColumns(before, after, leftWidth, rightWidth)
	} else {
		before := append([]string{styTitle.Render("BEFORE")}, topologyLines(m.compiled.Before, available)...)
		after := append([]string{styTitle.Render("AFTER")}, topologyLines(m.compiled.After, available)...)
		body = strings.Join(fitLines(before, available), "\n") + "\n\n" + strings.Join(fitLines(after, available), "\n")
	}
	body += "\n\n" + styTitle.Render(fmt.Sprintf("%d ADDITIVE OPERATIONS", len(m.compiled.Operations)))
	for _, operation := range m.compiled.Operations {
		body += "\n" + styOK.Render("+ "+operation.Pane.Key) + styDim.Render(" → "+operation.WorkspaceKey+" / "+operation.TabKey+" ["+operation.Placement.Description()+"]")
	}
	for _, diagnostic := range m.compiled.Diagnostics {
		body += "\n" + styWarn.Render("! "+diagnostic)
	}
	body += "\n\n" + styDim.Render("Execution only adds missing panes. Existing panes are never closed or replaced.")
	return body
}

func (m *model) planCounts() (selected, actionable, healthy, shells int) {
	if m.plan == nil || m.plan.plan == nil {
		return
	}
	for i, pane := range m.plan.plan.Panes {
		switch pane.Action {
		case resume.Replace, resume.Relaunch, resume.Resurrect:
			actionable++
			if m.plan.sel[i] {
				selected++
			}
		case resume.KeepNative:
			healthy++
		case resume.ShellKeep:
			shells++
		}
	}
	return
}

func (m *model) restorationAgents() []string {
	if m.plan == nil || m.plan.snap == nil {
		return nil
	}
	var agents []string
	if m.plan.plan == nil {
		for _, pane := range m.plan.snap.AgentPanes() {
			agents = append(agents, pane.Key)
		}
		return agents
	}
	for i, pane := range m.plan.plan.Panes {
		if pane.Manifest.Agent != "" && m.plan.sel[i] {
			agents = append(agents, pane.Manifest.Key)
		}
	}
	return agents
}

func (m *model) previewPaneCount() int {
	if m.plan == nil || m.plan.snap == nil {
		return 0
	}
	count := 0
	for _, workspace := range m.plan.snap.Workspaces {
		for _, tab := range workspace.Tabs {
			count += len(tab.Panes)
		}
	}
	return count
}

// ---- formatting ---------------------------------------------------------

func relTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func snapshotWhen(t time.Time) string {
	now := time.Now()
	today := now.Year() == t.Year() && now.YearDay() == t.YearDay()
	yesterday := now.AddDate(0, 0, -1)
	switch {
	case today:
		return "Today " + t.Format("15:04")
	case yesterday.Year() == t.Year() && yesterday.YearDay() == t.YearDay():
		return "Yesterday " + t.Format("15:04")
	case now.Year() == t.Year():
		return t.Format("Jan 2, 15:04")
	default:
		return t.Format("2006-01-02 15:04")
	}
}

func snapshotName(snapshot *manifest.Snapshot) string {
	if snapshot == nil {
		return "unnamed capture"
	}
	if name := strings.TrimSpace(snapshot.Name); name != "" {
		return name
	}
	return snapshotWhen(snapshot.CreatedAt.Local())
}

func humanBytes(n int64) string {
	switch {
	case n == 0:
		return "—"
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
	default:
		return fmt.Sprintf("%.1fGB", float64(n)/(1024*1024*1024))
	}
}

func snapshotStats(s *manifest.Snapshot) (agents string, size int64, n int) {
	if s == nil {
		return "—", 0, 0
	}
	var names []string
	for _, p := range s.AgentPanes() {
		names = append(names, p.Key)
		size += strategy.TranscriptSize(p.Agent, p.SID, p.Env)
	}
	if len(names) > 4 {
		agents = strings.Join(names[:4], " ") + " …"
	} else {
		agents = strings.Join(names, " ")
	}
	if agents == "" {
		agents = "shells only"
	}
	return agents, size, len(names)
}

// agentRoster renders the fleet with per-kind brand icons before each name.
func agentRoster(s *manifest.Snapshot) string {
	if s == nil {
		return "—"
	}
	var names []string
	for _, pane := range s.AgentPanes() {
		names = append(names, icon(pane.Agent, pane.Title)+" "+pane.Key)
	}
	if len(names) > 4 {
		return strings.Join(names[:4], "  ") + "  …"
	}
	if len(names) == 0 {
		return "shells only"
	}
	return strings.Join(names, "  ")
}

func plural(n int, s string) string {
	if n == 1 {
		return s
	}
	return s + "s"
}

func clipLabel(s string, width int) string {
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}

func actionLabel(action resume.Action) string {
	switch action {
	case resume.KeepNative:
		return "UNCHANGED"
	case resume.Replace:
		return "RESTART"
	case resume.Relaunch:
		return "START FRESH"
	case resume.Resurrect:
		return "RESTORE"
	case resume.ShellKeep:
		return "PRESERVE"
	default:
		return string(action)
	}
}

func workspaceKey(workspace manifest.Workspace) string {
	if workspace.Label != "" {
		return workspace.Label
	}
	return workspace.ID
}

func workspaceStats(workspace manifest.Workspace) (agents, tabs int, size int64) {
	tabs = len(workspace.Tabs)
	for _, tab := range workspace.Tabs {
		for _, pane := range tab.Panes {
			if pane.Agent == "" {
				continue
			}
			agents++
			size += strategy.TranscriptSize(pane.Agent, pane.SID, pane.Env)
		}
	}
	return
}

func layoutLabel(tab manifest.Tab) string {
	count := len(tab.Panes)
	base := fmt.Sprintf("%d %s", count, plural(count, "pane"))
	if count <= 1 || tab.Layout == nil || len(tab.Layout.Splits) == 0 {
		return base
	}
	direction := tab.Layout.Splits[0].Direction
	for _, split := range tab.Layout.Splits[1:] {
		if split.Direction != direction {
			return base + " · mixed"
		}
	}
	switch direction {
	case "right":
		return base + " · horizontal"
	case "down":
		return base + " · vertical"
	default:
		return base
	}
}

func filteredLayoutLabel(tab manifest.Tab, selected map[string]bool) string {
	count := 0
	for _, pane := range tab.Panes {
		if selected[pane.Key] {
			count++
		}
	}
	if count == len(tab.Panes) {
		return layoutLabel(tab)
	}
	base := fmt.Sprintf("%d %s", count, plural(count, "pane"))
	if count > 1 {
		base += " · compacted"
	}
	return base
}

func providerDisplay(kind string, env map[string]string) string {
	model := strings.ToLower(env["ANTHROPIC_DEFAULT_OPUS_MODEL"] + " " + env["ANTHROPIC_MODEL"])
	switch {
	case strings.Contains(model, "gpt"):
		return "GPT"
	case strings.Contains(model, "glm"):
		return "GLM"
	case kind == "claude":
		return "Claude"
	case kind == "":
		return "shell"
	default:
		return strings.ToUpper(kind[:1]) + kind[1:]
	}
}

func fitLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(line) <= width {
		return line
	}
	return ansi.Truncate(line, width, "…")
}

func fitLines(lines []string, width int) []string {
	fitted := make([]string, len(lines))
	for index, line := range lines {
		fitted[index] = fitLine(line, width)
	}
	return fitted
}

func fitBlock(block string, width int) string {
	return strings.Join(fitLines(strings.Split(block, "\n"), width), "\n")
}

func joinColumns(left, right []string, leftWidth, rightWidth int) string {
	left = fitLines(left, leftWidth)
	right = fitLines(right, rightWidth)
	height := max(len(left), len(right))
	var out strings.Builder
	for i := 0; i < height; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		padding := max(0, leftWidth-ansi.StringWidth(l))
		out.WriteString(l)
		out.WriteString(strings.Repeat(" ", padding))
		out.WriteString(styDim.Render(" │ "))
		out.WriteString(r)
		if i+1 < height {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// preview renders the target layout: which panes get restored and which
// stay as they are, one tree per workspace.
func (m *model) previewLines() []string {
	if m.plan == nil || m.plan.snap == nil {
		return nil
	}
	type effect struct {
		selected bool
		action   resume.Action
		reason   string
	}
	effects := map[string]effect{}
	if m.plan.plan != nil {
		for i, pane := range m.plan.plan.Panes {
			effects[pane.Manifest.Key] = effect{selected: m.plan.sel[i], action: pane.Action, reason: pane.Reason}
		}
	}
	type meta struct {
		key        string
		mark       string
		label      string
		style      lipgloss.Style
		reason     string
		replaysEnv bool
	}
	paneMeta := func(pane manifest.Pane) meta {
		if pane.Agent == "" {
			return meta{key: "preserve", mark: "·", label: "PRESERVE", style: styDim, reason: "shell stays at its captured working directory"}
		}
		e := effects[pane.Key]
		if m.plan.plan == nil {
			e = effect{selected: true, action: resume.Resurrect, reason: "session is stopped"}
		}
		switch {
		case e.action == resume.KeepNative:
			return meta{key: "unchanged", mark: "●", label: actionLabel(e.action), style: styOK, reason: "live environment already matches this restoration target"}
		case !e.selected:
			return meta{key: "skipped", mark: "○", label: "SKIPPED", style: styDim, reason: "this pane is excluded from restoration"}
		case e.action == resume.Replace:
			return meta{key: "restart", mark: "●", label: actionLabel(e.action), style: styBad, reason: e.reason, replaysEnv: true}
		case e.action == resume.Relaunch:
			return meta{key: "fresh", mark: "●", label: actionLabel(e.action), style: styWarn, reason: e.reason, replaysEnv: true}
		case e.action == resume.Resurrect:
			return meta{key: "restore", mark: "●", label: actionLabel(e.action), style: styNice, reason: e.reason, replaysEnv: true}
		default:
			return meta{key: "unchanged", mark: "●", label: "UNCHANGED", style: styOK, reason: "no change required"}
		}
	}

	present := map[string]bool{}
	var out []string
	paneCursor := 0
	selectedDetail := ""
	renderPane := func(prefix, branch, tabName string, pane manifest.Pane, collapsed bool) {
		mta := paneMeta(pane)
		present[mta.key] = true
		kind := pane.Agent
		if kind == "" {
			kind = "shell"
		}
		provider := strategy.ProviderLabel(pane.Agent, pane.Env)
		who := pane.Key
		if pane.Agent == "" {
			who = "shell"
		} else if provider != "" && provider != kind {
			who += styDim.Render(" · " + provider)
		}
		cursor := " "
		if paneCursor == m.previewCursor {
			cursor = styTitle.Render("›")
			reason := mta.reason
			if reason == "" {
				reason = "no additional action details"
			}
			selectedDetail = mta.style.Render(mta.label) + styDim.Render(" — "+reason)
			if mta.replaysEnv {
				suffix := "captured environment will be replayed"
				if mta.key == "restart" {
					suffix = "start the corrected pane, then close the current one"
				}
				selectedDetail += styDim.Render(" · " + suffix)
			}
		}
		tabSuffix := ""
		if collapsed {
			tabSuffix = styDim.Render("  " + clipLabel(tabName, 18))
		}
		out = append(out, fmt.Sprintf("  %s%s %s %s %s %s%s", prefix, branch, cursor,
			mta.style.Render(mta.mark), icon(kind, pane.Title), who, tabSuffix))
		paneCursor++
	}

	for _, workspace := range m.plan.snap.Workspaces {
		label := workspace.Label
		if label == "" {
			label = workspace.ID
		}
		paneCount := 0
		for _, tab := range workspace.Tabs {
			paneCount += len(tab.Panes)
		}
		out = append(out, styTitle.Render("WORKSPACE ")+label+
			styDim.Render(fmt.Sprintf(" · %d %s · %d %s", len(workspace.Tabs), plural(len(workspace.Tabs), "tab"),
				paneCount, plural(paneCount, "pane"))))
		for tabIndex, tab := range workspace.Tabs {
			lastTab := tabIndex == len(workspace.Tabs)-1
			tabBranch, childPrefix := "├─", "│  "
			if lastTab {
				tabBranch, childPrefix = "└─", "   "
			}
			tabName := tab.Label
			if tabName == "" {
				tabName = tab.ID
			}
			if len(tab.Panes) == 1 {
				renderPane("", tabBranch, tabName, tab.Panes[0], true)
				continue
			}
			out = append(out, "  "+tabBranch+" "+styDim.Render(clipLabel(tabName, 18)))
			for paneIndex, pane := range tab.Panes {
				paneBranch := "├─"
				if paneIndex == len(tab.Panes)-1 {
					paneBranch = "└─"
				}
				renderPane(childPrefix, paneBranch, "", pane, false)
			}
		}
		out = append(out, "")
	}
	if selectedDetail != "" {
		out = append(out, "  "+selectedDetail)
	}

	type legendEntry struct {
		key, label, mark string
		style            lipgloss.Style
	}
	entries := []legendEntry{
		{key: "restart", label: "restart", mark: "●", style: styBad},
		{key: "fresh", label: "start fresh", mark: "●", style: styWarn},
		{key: "restore", label: "restore", mark: "●", style: styNice},
		{key: "unchanged", label: "unchanged", mark: "●", style: styOK},
		{key: "skipped", label: "skipped", mark: "○", style: styDim},
		{key: "preserve", label: "shell", mark: "·", style: styDim},
	}
	var legend []string
	for _, entry := range entries {
		if present[entry.key] {
			legend = append(legend, entry.style.Render(entry.mark)+styDim.Render(" "+entry.label))
		}
	}
	if len(legend) > 0 {
		out = append([]string{strings.Join(legend, "  "), ""}, out...)
	}
	return out
}

// ---- chrome -------------------------------------------------------------

func (m *model) title() string {
	base := styTitle.Render("herdr-archive")
	if m.namingCapture {
		return base + styDim.Render("  › name capture")
	}
	if m.confirm == "archive" {
		return base + styDim.Render("  › archive session")
	}
	if m.confirm == "restore" && m.mode == viewSnapshots {
		return base + styDim.Render("  › "+m.cur.name+" › review additive restoration")
	}
	switch m.mode {
	case viewSessions:
		return base + styDim.Render("  › sessions")
	case viewSnapshots:
		return base + styDim.Render("  › "+m.cur.name+" › workspace planner")
	case viewInspect:
		page := "tabs / panes"
		if m.inspectPreview {
			page = "selected topology"
		}
		return base + styDim.Render("  › "+m.cur.name+" › "+m.inspectTarget.key+" › "+page)
	default:
		page := "restoration plan"
		if m.confirm == "restore" {
			page = "confirm restoration"
		} else if m.preview {
			page = "planned effects"
		}
		return base + styDim.Render("  › "+m.cur.name+" › "+page)
	}
}

func (m *model) banner() string {
	if m.confirm != "" || m.mode != viewPlan {
		return ""
	}
	if m.plan == nil {
		if m.err != nil {
			return styBad.Render(m.err.Error())
		}
		return m.spin.View() + styDim.Render(" waiting for the session to settle, then comparing live panes…")
	}
	if m.plan.plan == nil {
		if os.Getenv("HERDR_ENV") == "1" {
			return styWarn.Render("session stopped") + styDim.Render(" · restoration can boot it and replay the captured environment")
		}
		return styWarn.Render("session stopped") + styDim.Render(" · attach it in Herdr, then refresh before restoring")
	}
	selected, actionable, healthy, shells := m.planCounts()
	if actionable == 0 {
		return styOK.Render("running & healthy") + styDim.Render(fmt.Sprintf(" · %d unchanged · %d %s preserved", healthy, shells, plural(shells, "shell")))
	}
	return styOK.Render("running") + styDim.Render(fmt.Sprintf(" · %d/%d repairs selected · %d unchanged · %d %s preserved",
		selected, actionable, healthy, shells, plural(shells, "shell")))
}

func (m *model) footer() string {
	if m.namingCapture {
		return renderHints(hintItem{"enter", "capture"}, hintItem{"esc", "cancel"})
	}
	if m.confirm == "restore" {
		return renderHints(hintItem{"y", "confirm restoration"}, hintItem{"esc", "cancel"})
	}
	if m.confirm == "archive" {
		return renderHints(hintItem{"y", "capture & archive"}, hintItem{"esc", "cancel"})
	}
	switch m.mode {
	case viewSessions:
		return renderHints(
			hintItem{"enter/l", "open"}, hintItem{"j/k", "move"}, hintItem{"c", "capture"},
			hintItem{"C", "all sessions"}, hintItem{"x", "archive"}, hintItem{"/", "filter"}, hintItem{"q", "quit"},
		)
	case viewSnapshots:
		if m.width < 100 {
			return renderHints(
				hintItem{"space", "select"}, hintItem{"l", "inspect"}, hintItem{"C", "capture all"},
				hintItem{"R", "review"}, hintItem{"backspace", "clear"}, hintItem{"h", "back"},
			)
		}
		return renderHints(
			hintItem{"space", "select missing"}, hintItem{"l", "inspect subset"}, hintItem{"j/k", "move"},
			hintItem{"C", "capture all workspaces"}, hintItem{"r", "refresh"}, hintItem{"R", "review"},
			hintItem{"backspace", "clear"}, hintItem{"h", "back"},
		)
	case viewInspect:
		if m.inspectPreview {
			return renderHints(hintItem{"p", "selection"}, hintItem{"backspace", "clear"}, hintItem{"h", "keep & back"})
		}
		return renderHints(
			hintItem{"space", "toggle"}, hintItem{"j/k", "move"},
			hintItem{"p", "preview"}, hintItem{"backspace", "clear"}, hintItem{"h", "keep & back"},
		)
	default:
		if m.plan == nil {
			return renderHints(hintItem{"h", "back"}, hintItem{"r", "retry"})
		}
		if m.preview {
			return renderHints(hintItem{"p", "plan"}, hintItem{"y", "restore"}, hintItem{"h", "back"})
		}
		return renderHints(
			hintItem{"space", "toggle"}, hintItem{"a", "all repairs"}, hintItem{"j/k", "move"},
			hintItem{"p", "preview"}, hintItem{"y", "restore"}, hintItem{"h", "back"}, hintItem{"r", "refresh"},
		)
	}
}

func (m *model) confirmationBody() string {
	if m.confirm == "restore" && m.mode == viewSnapshots {
		return m.restorationConfirmationBody()
	}
	if m.confirm == "archive" {
		name := "session"
		if m.cur != nil {
			name = m.cur.name
		}
		return styWarn.Render("Archive "+name+"?") + "\n\n" +
			"Capture all live panes and their environment, then stop the session.\n" +
			styDim.Render("The snapshot remains available as a restoration target.")
	}
	agents := m.restorationAgents()
	selected, _, _, _ := m.planCounts()
	if m.plan != nil && m.plan.plan == nil {
		selected = len(agents)
	}
	return styWarn.Render(fmt.Sprintf("Restore %d %s?", selected, plural(selected, "agent"))) + "\n\n" +
		strings.Join(agents, " · ") + "\n\n" +
		styDim.Render("Captured environment variables will be replayed generically. Affected live panes may be replaced.")
}

// View satisfies tea.Model. Runs inline (no alt screen): the brand strip
// printed before the program stays visible above the frame.
func (m *model) contentWidth() int {
	frameWidth := max(1, m.width-4)
	frame := lipgloss.NewStyle().Width(frameWidth).Padding(1, 2)
	return max(1, frameWidth-frame.GetHorizontalFrameSize())
}

func (m *model) View() tea.View {
	frameWidth := max(1, m.width-4)
	contentWidth := m.contentWidth()
	innerHeight := m.height - 2
	if innerHeight < 1 {
		innerHeight = 1
	}
	content := lipgloss.NewStyle().
		Width(frameWidth).
		Height(innerHeight).
		Padding(1, 2).
		Render(fitBlock(m.render(), contentWidth))
	view := tea.NewView(content)
	view.AltScreen = true
	view.WindowTitle = "herdr-archive"
	return view
}

func (m *model) render() string {
	var b strings.Builder
	b.WriteString(m.title())
	b.WriteString("\n")
	if ban := m.banner(); ban != "" {
		b.WriteString(ban)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	if m.namingCapture {
		b.WriteString(styTitle.Render("Name this capture"))
		b.WriteString("\n")
		b.WriteString(styDim.Render("This name stays with the layout as a durable restoration target."))
		b.WriteString("\n\n")
		b.WriteString(m.captureInput.View())
	} else if m.confirm != "" {
		b.WriteString(m.confirmationBody())
	} else {
		switch m.mode {
		case viewSessions:
			b.WriteString(m.sessList.View())
		case viewSnapshots:
			b.WriteString(m.plannerView())
		case viewInspect:
			b.WriteString(m.inspectorView())
		default:
			if m.preview {
				for _, line := range m.previewLines() {
					b.WriteString(line)
					b.WriteString("\n")
				}
			} else if m.plan != nil && m.plan.plan != nil {
				b.WriteString(m.planList.View())
			} else if m.plan != nil && m.plan.plan == nil {
				agents, size, n := snapshotStats(m.plan.snap)
				b.WriteString(styDim.Render(fmt.Sprintf("Target contains %d %s · %s · %s transcripts",
					n, plural(n, "agent"), agents, humanBytes(size))))
				b.WriteString("\n")
			}
		}
	}
	b.WriteString("\n\n")
	if m.note != "" {
		b.WriteString(styWarn.Render(m.note))
		b.WriteString("\n")
	}
	b.WriteString(m.footer())
	return b.String()
}

// ---- update -------------------------------------------------------------

func (m *model) Init() tea.Cmd { return tea.RequestBackgroundColor }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		m.setListBackground(msg.IsDark())
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		w, h := m.contentWidth(), msg.Height-8
		if h < 1 {
			h = 1
		}
		m.sessList.SetSize(w, h)
		m.snapList.SetSize(w, h)
		m.planList.SetSize(w, h)
		return m, nil
	case spinner.TickMsg:
		if m.spinning {
			var cmd tea.Cmd
			m.spin, cmd = m.spin.Update(msg)
			return m, cmd
		}
		return m, nil
	case planMsg:
		m.spinning, m.plan, m.err = false, msg.pd, msg.err
		m.syncPlanList()
		return m, nil
	case plannerLiveMsg:
		m.spinning = false
		if msg.err != nil {
			m.err = msg.err
			m.live, m.compiled = nil, nil
			m.workspacePlan = map[string]workspaceSelection{}
			m.mode, m.inspectPreview, m.inspectCursor = viewSnapshots, false, -1
			m.note = "live topology unavailable; selection and execution disabled: " + msg.err.Error()
			return m, nil
		}
		m.err = nil
		m.note = ""
		m.live = msg.live
		for key, selection := range m.workspacePlan {
			states := planner.Analyze(selection.target.workspace, m.live)
			planner.PruneLive(selection.selected, states)
			m.workspacePlan[key] = selection
		}
		m.compileRestorationPlan()
		if m.mode == viewInspect {
			nodes := inspectNodes(m.inspectTarget, m.live)
			if m.inspectCursor < 0 || m.inspectCursor >= len(nodes) || !nodes[m.inspectCursor].focusable {
				m.inspectCursor = firstFocusableNode(nodes)
			}
		}
		return m, nil
	case refreshedMsg:
		m.confirm = ""
		if msg.err != nil {
			m.note = "failed: " + msg.err.Error()
		} else {
			m.note = msg.note
		}
		currentSession := ""
		if m.cur != nil {
			currentSession = m.cur.name
		}
		m.refreshSessions()
		if currentSession != "" {
			for i := range m.sessions {
				if m.sessions[i].name == currentSession {
					m.cur = &m.sessions[i]
					break
				}
			}
		}
		if m.mode == viewSnapshots {
			if msg.err == nil {
				m.loadSnaps()
			}
			m.spinning = true
			return m, tea.Batch(loadPlannerLiveCmd(m.cur.name), m.spin.Tick)
		}
		if m.mode == viewPlan && m.plan != nil && msg.err == nil {
			m.plan, m.spinning, m.preview = nil, true, false
			return m, tea.Batch(loadPlanCmd(m.cur.name, m.curSnap), m.spin.Tick)
		}
		return m, nil
	case tea.KeyMsg:
		return m.key(msg)
	}
	if m.namingCapture {
		var cmd tea.Cmd
		m.captureInput, cmd = m.captureInput.Update(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	switch m.mode {
	case viewSessions:
		m.sessList, cmd = m.sessList.Update(msg)
	case viewSnapshots:
		m.snapList, cmd = m.snapList.Update(msg)
	case viewPlan:
		if !m.preview && m.confirm == "" && m.plan != nil && m.plan.plan != nil {
			m.planList, cmd = m.planList.Update(msg)
		}
	}
	return m, cmd
}

func (m *model) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.namingCapture {
		switch msg.String() {
		case "esc":
			m.namingCapture = false
			m.captureInput.Blur()
			m.captureInput.Reset()
			m.captureSession = ""
			m.captureSessions = nil
			m.note = ""
			return m, nil
		case "enter":
			name := strings.TrimSpace(m.captureInput.Value())
			if name == "" {
				name = manifest.DefaultName(time.Now())
			}
			sessions := append([]string(nil), m.captureSessions...)
			if len(sessions) == 0 && m.captureSession != "" {
				sessions = append(sessions, m.captureSession)
			}
			m.namingCapture = false
			m.captureInput.Blur()
			m.captureInput.Reset()
			m.captureSession = ""
			m.captureSessions = nil
			m.note = "capturing “" + name + "”…"
			return m, captureSessionsCmd(sessions, name)
		default:
			var cmd tea.Cmd
			m.captureInput, cmd = m.captureInput.Update(msg)
			return m, cmd
		}
	}
	// While a Bubbles list owns the filter editor, let it consume text before
	// global navigation keys such as q, h, l, c, and r.
	if m.mode == viewSessions && m.sessList.SettingFilter() {
		var cmd tea.Cmd
		m.sessList, cmd = m.sessList.Update(msg)
		return m, cmd
	}
	if m.mode == viewSnapshots && m.snapList.SettingFilter() {
		var cmd tea.Cmd
		m.snapList, cmd = m.snapList.Update(msg)
		return m, cmd
	}
	if m.confirm != "" {
		switch msg.String() {
		case "y":
			if m.confirm == "archive" {
				return m.execArchive()
			}
			return m.execRestore()
		case "esc", "q", "h":
			m.confirm = ""
		}
		return m, nil
	}
	if m.mode == viewSnapshots {
		switch msg.String() {
		case "backspace":
			m.workspacePlan = map[string]workspaceSelection{}
			m.compileRestorationPlan()
			m.note = ""
			return m, nil
		case "j", "down":
			if m.plannerCursor+1 < len(m.targets) {
				m.plannerCursor++
			}
			return m, nil
		case "k", "up":
			if m.plannerCursor > 0 {
				m.plannerCursor--
			}
			return m, nil
		case "space", " ":
			if m.spinning || m.live == nil {
				m.note = "wait for a current live topology before selecting"
				return m, nil
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
			return m, nil
		case "enter", "l":
			if m.spinning || m.live == nil {
				m.note = "wait for a current live topology before inspecting"
				return m, nil
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
			return m, nil
		case "R":
			if m.spinning || m.live == nil {
				m.note = "wait for a current live topology before restoring"
				return m, nil
			}
			m.compileRestorationPlan()
			if m.compiled == nil || len(m.compiled.Operations) == 0 {
				m.note = "select at least one missing pane"
				return m, nil
			}
			m.confirm = "restore"
			return m, nil
		case "r":
			if m.cur != nil {
				m.spinning = true
				m.note = "refreshing live topology…"
				return m, tea.Batch(loadPlannerLiveCmd(m.cur.name), m.spin.Tick)
			}
			return m, nil
		}
	}
	if m.mode == viewInspect {
		if m.inspectPreview {
			switch msg.String() {
			case "p":
				m.inspectPreview = false
				return m, nil
			case "j", "down", "k", "up", "space", " ":
				return m, nil
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
			return m, nil
		case "j", "down":
			m.inspectCursor = moveInspectCursor(nodes, m.inspectCursor, 1)
			return m, nil
		case "k", "up":
			m.inspectCursor = moveInspectCursor(nodes, m.inspectCursor, -1)
			return m, nil
		case "space", " ":
			if m.inspectCursor >= 0 && m.inspectCursor < len(nodes) && nodes[m.inspectCursor].focusable {
				selection, ok := m.workspacePlan[m.inspectTarget.key]
				if !ok || selection.selected == nil {
					selection = workspaceSelection{target: m.inspectTarget, selected: planner.Selection{}}
				}
				node := nodes[m.inspectCursor]
				tab := m.inspectTarget.workspace.Tabs[node.tabIndex]
				states := m.targetStates(m.inspectTarget)
				if node.paneIndex < 0 {
					planner.ToggleRestorableTab(tab, selection.selected, states)
				} else {
					pane := tab.Panes[node.paneIndex]
					if states[pane.Key].Availability == planner.Restorable {
						planner.TogglePane(pane.Key, selection.selected)
					}
				}
				m.workspacePlan[m.inspectTarget.key] = selection
				m.compileRestorationPlan()
			}
			return m, nil
		case "p":
			m.inspectPreview = !m.inspectPreview
			return m, nil
		case "enter", "l":
			return m, nil
		}
	}
	if m.mode == viewPlan && m.preview {
		switch msg.String() {
		case "j", "down":
			if m.previewCursor+1 < m.previewPaneCount() {
				m.previewCursor++
			}
			return m, nil
		case "k", "up":
			if m.previewCursor > 0 {
				m.previewCursor--
			}
			return m, nil
		}
	}

	switch msg.String() {
	case "q":
		if m.mode == viewInspect {
			m.mode, m.inspectPreview = viewSnapshots, false
			return m, nil
		}
		if m.mode == viewPlan {
			m.mode, m.plan, m.preview = viewSnapshots, nil, false
			return m, nil
		}
		if m.mode == viewSnapshots {
			m.mode, m.cur = viewSessions, nil
			return m, nil
		}
		return m, tea.Quit
	case "esc":
		if m.mode == viewInspect {
			m.mode, m.inspectPreview = viewSnapshots, false
			return m, nil
		}
		if m.mode == viewPlan {
			if m.preview {
				m.preview = false
				return m, nil
			}
			m.mode, m.plan = viewSnapshots, nil
		} else if m.mode == viewSnapshots {
			m.mode, m.cur = viewSessions, nil
		} else {
			return m, tea.Quit
		}
		return m, nil
	case "enter", "l":
		return m.digIn()
	case "h":
		return m.back()
	case "space", " ":
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
			}
		}
	case "y":
		if m.mode == viewPlan && m.plan != nil && m.plan.snap != nil {
			if len(m.restorationAgents()) == 0 {
				m.note = "select at least one repair (space)"
				return m, nil
			}
			m.confirm = "restore"
		}
	case "x":
		if m.mode == viewSessions {
			if item, ok := m.sessList.SelectedItem().(sessionItem); ok {
				for i := range m.sessions {
					if m.sessions[i].name == item.row.name {
						m.cur = &m.sessions[i]
						break
					}
				}
			}
		}
		if (m.mode == viewSessions || m.mode == viewSnapshots) && m.cur != nil && m.cur.running {
			m.confirm = "archive"
		}
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
			return m, nil
		}
		m.captureSession = ""
		m.captureSessions = sessions
		m.namingCapture = true
		m.captureInput.Reset()
		m.note = ""
		return m, m.captureInput.Focus()
	case "c":
		name := ""
		if m.mode == viewSessions {
			if item, ok := m.sessList.SelectedItem().(sessionItem); ok {
				name = item.row.name
			}
		} else if m.mode == viewSnapshots && m.cur != nil {
			name = m.cur.name
		}
		if name == "" {
			return m, nil
		}
		m.captureSession = name
		m.captureSessions = nil
		m.namingCapture = true
		m.captureInput.Reset()
		m.note = ""
		return m, m.captureInput.Focus()
	case "r":
		m.note = ""
		if m.mode == viewPlan && m.plan != nil {
			m.plan, m.spinning, m.preview = nil, true, false
			return m, tea.Batch(loadPlanCmd(m.cur.name, m.curSnap), m.spin.Tick)
		}
		if m.mode == viewSessions {
			m.refreshSessions()
		}
		return m, nil
	}
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

// digIn is enter/l: go one level deeper.
func (m *model) digIn() (tea.Model, tea.Cmd) {
	switch m.mode {
	case viewSessions:
		item, ok := m.sessList.SelectedItem().(sessionItem)
		if !ok {
			return m, nil
		}
		for i := range m.sessions {
			if m.sessions[i].name == item.row.name {
				m.cur = &m.sessions[i]
				break
			}
		}
		m.workspacePlan = map[string]workspaceSelection{}
		m.live, m.compiled = nil, nil
		m.plannerCursor = 0
		m.loadSnaps()
		m.mode = viewSnapshots
		m.spinning = true
		m.note = "reading live topology…"
		return m, tea.Batch(loadPlannerLiveCmd(m.cur.name), m.spin.Tick)
	case viewSnapshots:
		item, ok := m.snapList.SelectedItem().(snapshotItem)
		if !ok || item.row.err != nil {
			return m, nil
		}
		m.curSnap = item.row.path
		m.mode, m.plan, m.preview, m.spinning = viewPlan, nil, false, true
		m.planList.SetItems(nil)
		m.note = ""
		return m, tea.Batch(loadPlanCmd(m.cur.name, m.curSnap), m.spin.Tick)
	default:
		if m.plan != nil {
			m.preview = true
			m.previewCursor = 0
		}
		return m, nil
	}
}

// back is h: one level out. h at the top level does nothing (q quits).
func (m *model) back() (tea.Model, tea.Cmd) {
	switch m.mode {
	case viewInspect:
		m.mode = viewSnapshots
		m.inspectPreview = false
	case viewPlan:
		if m.preview {
			m.preview = false
			return m, nil
		}
		m.mode, m.plan = viewSnapshots, nil
	case viewSnapshots:
		m.mode, m.cur = viewSessions, nil
	}
	return m, nil
}

func (m *model) execRestore() (tea.Model, tea.Cmd) {
	if m.mode == viewSnapshots {
		m.compileRestorationPlan()
		if m.compiled == nil || len(m.compiled.Operations) == 0 {
			m.confirm = ""
			m.note = "select at least one missing pane"
			return m, nil
		}
		compiled := m.compiled
		m.confirm = ""
		m.spinning = true
		m.note = fmt.Sprintf("restoring %d missing %s…", len(compiled.Operations), plural(len(compiled.Operations), "pane"))
		apply := func() tea.Msg {
			err := resume.ApplyCompiled(compiled)
			return refreshedMsg{err: err, note: "additive restoration complete"}
		}
		return m, tea.Batch(apply, m.spin.Tick)
	}
	agents := m.restorationAgents()
	total := 0
	if m.plan != nil && m.plan.plan != nil {
		for _, pane := range m.plan.plan.Panes {
			if pane.Manifest.Agent != "" {
				total++
			}
		}
	} else {
		total = len(agents)
	}
	if len(agents) == 0 {
		m.confirm = ""
		m.note = "select at least one repair (space)"
		return m, nil
	}
	self, err := os.Executable()
	if err != nil {
		m.confirm = ""
		m.note = err.Error()
		return m, nil
	}
	args := []string{"resume", "--session", m.cur.name, "--from", m.plan.path, "--yes"}
	if m.plan.plan != nil && len(agents) < total {
		for _, agent := range agents {
			args = append(args, "--agent", agent)
		}
	}
	m.confirm = ""
	m.note = "restoring " + strings.Join(agents, ", ") + "…"
	return m, tea.ExecProcess(exec.Command(self, args...), func(err error) tea.Msg {
		return refreshedMsg{err: err, note: "restoration complete"}
	})
}

func (m *model) execArchive() (tea.Model, tea.Cmd) {
	if m.cur == nil || !m.cur.running {
		m.confirm = ""
		m.note = "selected session is not running"
		return m, nil
	}
	self, err := os.Executable()
	if err != nil {
		m.confirm = ""
		m.note = err.Error()
		return m, nil
	}
	name := m.cur.name
	m.confirm = ""
	m.note = "capturing and archiving " + name + "…"
	return m, tea.ExecProcess(exec.Command(self, "archive", "--session", name), func(err error) tea.Msg {
		return refreshedMsg{err: err, note: "archived " + name}
	})
}
