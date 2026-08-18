// Package tui is the browse picker: sessions -> snapshots -> plan with
// per-agent restore selection and a layout preview. Built on bubbles
// list / table / spinner primitives.
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/abhirupdas/herdr-archive/internal/brands"
	"github.com/abhirupdas/herdr-archive/internal/capture"
	"github.com/abhirupdas/herdr-archive/internal/kitty"
	"github.com/abhirupdas/herdr-archive/internal/manifest"
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

type planData struct {
	snap    *manifest.Snapshot
	path    string
	plan    *resume.Plan
	planErr string
	running bool
	sel     []bool // per plan.Panes index
}

type model struct {
	mode     mode
	width    int
	height   int
	sessions []sessionRow
	snaps    []snapshotRow
	sessList list.Model
	snapList list.Model
	tbl      table.Model
	spin     spinner.Model
	spinning bool
	preview  bool
	cur      *sessionRow
	curSnap  string
	plan     *planData
	note     string
	err      error
}

type planMsg struct {
	pd  *planData
	err error
}
type refreshedMsg struct{}
type errMsg struct{ err error }

func Run() error {
	m := &model{
		width:  96,
		height: 24,
		spin:   spinner.New(spinner.WithSpinner(spinner.Dot)),
	}
	if err := m.loadSessions(); err != nil {
		return err
	}
	if len(m.sessions) == 0 {
		return fmt.Errorf("no herdr sessions found")
	}
	m.sessList = newList()
	m.snapList = newList()
	m.tbl = table.New(
		table.WithColumns(planColumns),
		table.WithWidth(92),
		table.WithHeight(8),
		table.WithStyles(table.Styles{
			Header:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")),
			Selected: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("6")),
		}),
	)
	// vim keys on the table (bubbles v2 binds only arrows there; lists do
	// bind j/k)
	km := table.DefaultKeyMap()
	km.LineUp = key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("k", "up"))
	km.LineDown = key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("j", "down"))
	m.tbl.KeyMap = km
	// v2 table ignores keys until focused (lists have no focus gate)
	m.tbl.Focus()
	m.syncSessionList()
	m.printLogoStrip()
	_, err := tea.NewProgram(m).Run()
	return err
}

// printLogoStrip writes the brand-image strip as plain pre-TUI output.
// kitty placeholder runes (U+10EEEE) are dropped by bubbletea v2's cellbuf,
// so in-frame images are not possible today; instead the strip is drawn at
// the cursor before the program starts and the TUI runs inline beneath it.
func (m *model) printLogoStrip() {
	if !kitty.Capable() {
		return
	}
	seen := map[string]bool{}
	var items []kitty.StripItem
	for _, s := range m.sessions {
		if s.latest == nil {
			continue
		}
		for _, p := range s.latest.AgentPanes() {
			if p.Agent == "" || seen[p.Agent] {
				continue
			}
			seen[p.Agent] = true
			if b, ok := brands.PNG(p.Agent); ok {
				label := lipgloss.NewStyle().Foreground(lipgloss.Color(strategy.ColorFor(p.Agent))).Render(p.Agent)
				items = append(items, kitty.StripItem{PNG: b, Label: " " + label + "   ", Cols: 2, Rows: 1})
			}
		}
	}
	if len(items) > 0 {
		_ = kitty.Strip(os.Stdout, items)
	}
}

func newList() list.Model {
	l := list.New([]list.Item{}, list.NewDefaultDelegate(), 88, 10)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)
	return l
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
	if m.cur == nil {
		return
	}
	for _, p := range m.cur.snaps {
		r := snapshotRow{path: p}
		r.snap, r.err = manifest.Load(p)
		m.snaps = append(m.snaps, r)
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
		live, err := capture.Session(capture.Options{Session: session})
		pd.running = err == nil
		if err != nil {
			pd.planErr = fmt.Sprintf("%v", err)
			return planMsg{pd: pd}
		}
		resume.Settle(session, 6*time.Second)
		pd.plan = resume.Diff(snap, live)
		pd.sel = make([]bool, len(pd.plan.Panes))
		for i, pp := range pd.plan.Panes {
			pd.sel[i] = pp.Manifest.Agent != ""
		}
		return planMsg{pd: pd}
	}
}

// ---- list / table sync -------------------------------------------------

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
		return "no snapshots — press c to capture"
	}
	_, size, _ := snapshotStats(i.row.latest)
	desc := fmt.Sprintf("last %s · %d %s · %s transcripts · %s",
		relTime(i.row.latest.CreatedAt.Local()), i.row.agents, plural(i.row.agents, "agent"), humanBytes(size), agentRoster(i.row.latest))
	if i.row.liveAgents >= 0 && i.row.liveAgents < i.row.agents {
		desc += styWarn.Render(fmt.Sprintf(" · %d/%d agents live", i.row.liveAgents, i.row.agents))
	}
	return desc
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
	mark := " "
	if i.isLast {
		mark = styOK.Render("●")
	}
	if i.row.err != nil || i.row.snap == nil {
		return mark + " unreadable snapshot"
	}
	_, size, n := snapshotStats(i.row.snap)
	return fmt.Sprintf("%s %s — %d %s · %s · %s", mark, relTime(i.row.snap.CreatedAt.Local()),
		n, plural(n, "agent"), agentRoster(i.row.snap), humanBytes(size))
}
func (i snapshotItem) Description() string { return "" }
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

func (m *model) syncSnapList() {
	items := make([]list.Item, 0, len(m.snaps))
	for _, r := range m.snaps {
		items = append(items, snapshotItem{row: r, isLast: m.cur != nil && r.path == m.cur.last})
	}
	m.snapList.SetItems(items)
}

var planColumns = []table.Column{
	{Title: "sel", Width: 3},
	{Title: "agent", Width: 14},
	{Title: "kind → provider", Width: 22},
	{Title: "verdict", Width: 11},
	{Title: "disk", Width: 7},
	{Title: "why", Width: 34},
}

func (m *model) syncTable() {
	if m.plan == nil || m.plan.plan == nil {
		m.tbl.SetRows([]table.Row{})
		return
	}
	rows := make([]table.Row, 0, len(m.plan.plan.Panes))
	for i, pp := range m.plan.plan.Panes {
		box := " "
		if m.plan.sel[i] {
			box = "x"
		}
		if pp.Manifest.Agent == "" {
			box = "·"
		}
		kind := pp.Manifest.Agent
		if kind == "" {
			kind = "shell"
		}
		glyphKind := pp.Manifest.Agent
		if glyphKind == "" {
			glyphKind = "shell"
		}
		who := strategy.GlyphFor(glyphKind, pp.Manifest.Title) + " " + kind
		if prov := strategy.ProviderLabel(pp.Manifest.Agent, pp.Manifest.Env); prov != "" && prov != kind {
			who = who + " → " + prov
		}
		size := strategy.TranscriptSize(pp.Manifest.Agent, pp.Manifest.SID, pp.Manifest.Env)
		reason := pp.Reason
		if pp.Action == resume.ShellKeep {
			reason = "shell pane"
		}
		rows = append(rows, table.Row{box, pp.Manifest.Key, who, string(pp.Action), humanBytes(size), reason})
	}
	m.tbl.SetRows(rows)
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

// agentRoster renders the fleet with per-kind icons: ✳glmlab ∞codexlab πpilab.
func agentRoster(s *manifest.Snapshot) string {
	if s == nil {
		return "—"
	}
	var names []string
	for _, p := range s.AgentPanes() {
		names = append(names, strategy.GlyphStyled(p.Agent, p.Title)+p.Key)
	}
	if len(names) > 4 {
		return strings.Join(names[:4], " ") + " …"
	}
	if len(names) == 0 {
		return "shells only"
	}
	return strings.Join(names, " ")
}

func plural(n int, s string) string {
	if n == 1 {
		return s
	}
	return s + "s"
}

// preview renders the target layout: which panes get restored and which
// stay as they are, one tree per workspace.
func (m *model) previewLines() []string {
	if m.plan == nil || m.plan.snap == nil {
		return nil
	}
	selByKey := map[string]bool{}
	if m.plan.plan != nil {
		for i, pp := range m.plan.plan.Panes {
			selByKey[pp.Manifest.Key] = m.plan.sel[i]
		}
	}
	head := "  layout the restore produces (▣ = restored with env replay, · = left as-is/native)"
	var out []string
	for _, w := range m.plan.snap.Workspaces {
		out = append(out, fmt.Sprintf("  %s  %s", styTitle.Render(w.ID), w.Label))
		for ti, t := range w.Tabs {
			branch := "├─"
			if ti == len(w.Tabs)-1 {
				branch = "└─"
			}
			tabName := t.Label
			if tabName == "" {
				tabName = t.ID
			}
			out = append(out, fmt.Sprintf("  %s %s", branch, styDim.Render(tabName)))
			for pi, p := range t.Panes {
				pb := "│  "
				if ti == len(w.Tabs)-1 {
					pb = "   "
				}
				pbranch := "├─"
				if pi == len(t.Panes)-1 {
					pbranch = "└─"
				}
				glyph, act := "·", styDim.Render("in place")
				if selByKey[p.Key] {
					glyph, act = "▣", styOK.Render("env-replay restore")
				} else if p.Agent == "" {
					glyph, act = "□", styDim.Render("shell at cwd")
				}
				prov := strategy.ProviderLabel(p.Agent, p.Env)
				kind := p.Agent
				if kind == "" {
					kind = "shell"
				}
				who := kind
				if prov != "" && prov != kind {
					who = kind + "→" + prov
				}
				iconKind := p.Agent
				if iconKind == "" {
					iconKind = "shell"
				}
				icon := strategy.GlyphStyled(iconKind, p.Title)
				out = append(out, fmt.Sprintf("  %s   %s %-16s %-18s %s", pb, glyph, pbranch+" "+icon+" "+p.Key, who, act))
			}
		}
	}
	return append([]string{head}, out...)
}

// ---- chrome -------------------------------------------------------------

func (m *model) title() string {
	switch m.mode {
	case viewSessions:
		return styTitle.Render("herdr-archive") + styDim.Render("  sessions")
	case viewSnapshots:
		return styTitle.Render("herdr-archive") + styDim.Render("  "+m.cur.name+" › snapshots  (● last)")
	default:
		return styTitle.Render("herdr-archive") + styDim.Render("  "+m.cur.name+" › restore plan")
	}
}

func (m *model) banner() string {
	if m.mode != viewPlan {
		return ""
	}
	if m.plan == nil {
		if m.err != nil {
			return styBad.Render(m.err.Error())
		}
		return m.spin.View() + styDim.Render(" diffing live session…")
	}
	if m.plan.plan == nil {
		return styWarn.Render("session stopped (" + m.plan.planErr + ") — y boots it and restores this snapshot")
	}
	banner := styOK.Render("running") + styDim.Render(" — restore sweeps in place, live panes are kept")
	allKeep := true
	for i, pp := range m.plan.plan.Panes {
		if m.plan.sel[i] && pp.Action != resume.KeepNative && pp.Action != resume.ShellKeep {
			allKeep = false
		}
	}
	if allKeep {
		banner = styOK.Render("running & healthy") + styDim.Render(" — nothing to do")
	}
	return banner
}

func (m *model) footer() string {
	switch m.mode {
	case viewSessions:
		return "enter/l open · j/k move · c capture · / filter · q quit"
	case viewSnapshots:
		return "enter/l plan · j/k move · c capture · h back · q quit"
	default:
		if m.preview {
			return "h/p table · y restore selected · h back"
		}
		return "space select · a all/none · j/k move · p/l preview · y restore · h back · r refresh"
	}
}

// View satisfies tea.Model. Runs inline (no alt screen): the brand strip
// printed before the program stays visible above the frame.
func (m *model) View() tea.View {
	return tea.NewView(m.render())
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
	switch m.mode {
	case viewSessions:
		b.WriteString(m.sessList.View())
	case viewSnapshots:
		b.WriteString(m.snapList.View())
	default:
		if m.preview {
			for _, l := range m.previewLines() {
				b.WriteString(l)
				b.WriteString("\n")
			}
		} else if m.plan != nil && m.plan.plan != nil {
			b.WriteString(m.tbl.View())
		} else if m.plan != nil && m.plan.plan == nil {
			agents, size, n := snapshotStats(m.plan.snap)
			b.WriteString(styDim.Render(fmt.Sprintf("%d %s · %s · %s transcripts", n, plural(n, "agent"), agents, humanBytes(size))))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	if m.note != "" {
		b.WriteString(styWarn.Render(m.note))
		b.WriteString("\n")
	}
	b.WriteString(styDim.Render(m.footer()))
	return b.String()
}

// ---- update -------------------------------------------------------------

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		w, h := msg.Width-4, msg.Height-8
		m.sessList.SetSize(w, h)
		m.snapList.SetSize(w, h)
		m.tbl.SetWidth(w)
		if h < 4 {
			h = 4 // short panes: keep table rows visible, clip instead
		}
		m.tbl.SetHeight(h)
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
		m.tbl.SetRows([]table.Row{})
		m.syncTable()
		th := m.height - 10
		if th < 4 {
			th = 4
		}
		m.tbl.SetHeight(th)
		if m.tbl.Cursor() < 0 {
			m.tbl.SetCursor(0) // land the cursor on the first row
		}
		return m, nil
	case refreshedMsg:
		m.refreshSessions()
		if m.mode == viewPlan && m.plan != nil {
			m.spinning = true
			return m, tea.Batch(loadPlanCmd(m.cur.name, m.curSnap), m.spin.Tick)
		}
		return m, nil
	case tea.KeyMsg:
		return m.key(msg)
	}
	var cmd tea.Cmd
	switch m.mode {
	case viewSessions:
		m.sessList, cmd = m.sessList.Update(msg)
	case viewSnapshots:
		m.snapList, cmd = m.snapList.Update(msg)
	case viewPlan:
		if !m.preview && m.plan != nil && m.plan.plan != nil {
			m.tbl, cmd = m.tbl.Update(msg)
		}
	}
	return m, cmd
}

func (m *model) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q":
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
			i := m.tbl.Cursor()
			// v2 tables report -1 before the cursor lands on a row
			if i >= 0 && i < len(m.plan.sel) && m.plan.plan.Panes[i].Manifest.Agent != "" {
				m.plan.sel[i] = !m.plan.sel[i]
				m.syncTable()
			}
		}
	case "a":
		if m.mode == viewPlan && m.plan != nil && m.plan.plan != nil {
			all := true
			for i, pp := range m.plan.plan.Panes {
				if pp.Manifest.Agent != "" && !m.plan.sel[i] {
					all = false
				}
			}
			for i, pp := range m.plan.plan.Panes {
				if pp.Manifest.Agent != "" {
					m.plan.sel[i] = !all
				}
			}
			m.syncTable()
		}
	case "p":
		if m.mode == viewPlan && m.plan != nil {
			m.preview = !m.preview
		}
	case "y":
		if m.mode == viewPlan && m.plan != nil && m.plan.snap != nil {
			return m.execRestore()
		}
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
		self, _ := os.Executable()
		m.note = "capture: " + name
		return m, tea.ExecProcess(exec.Command(self, "capture", "--session", name), func(err error) tea.Msg {
			return refreshedMsg{}
		})
	case "r":
		if m.mode == viewPlan && m.plan != nil {
			m.plan, m.spinning, m.preview = nil, true, false
			return m, tea.Batch(loadPlanCmd(m.cur.name, m.curSnap), m.spin.Tick)
		}
		if m.mode == viewSessions {
			m.refreshSessions()
		}
		return m, nil
	}
	// everything else (arrows, j/k, filtering, paging) belongs to the
	// active bubbles component
	var cmd tea.Cmd
	switch m.mode {
	case viewSessions:
		m.sessList, cmd = m.sessList.Update(msg)
	case viewSnapshots:
		m.snapList, cmd = m.snapList.Update(msg)
	case viewPlan:
		if !m.preview && m.plan != nil && m.plan.plan != nil {
			m.tbl, cmd = m.tbl.Update(msg)
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
		m.loadSnaps()
		m.mode = viewSnapshots
		return m, nil
	case viewSnapshots:
		item, ok := m.snapList.SelectedItem().(snapshotItem)
		if !ok || item.row.err != nil {
			return m, nil
		}
		m.curSnap = item.row.path
		m.mode, m.plan, m.preview, m.spinning = viewPlan, nil, false, true
		m.tbl.SetRows([]table.Row{})
		return m, tea.Batch(loadPlanCmd(m.cur.name, m.curSnap), m.spin.Tick)
	default: // viewPlan: l digs into the layout preview
		if m.plan != nil {
			m.preview = true
		}
		return m, nil
	}
}

// back is h: one level out. h at the top level does nothing (q quits).
func (m *model) back() (tea.Model, tea.Cmd) {
	switch m.mode {
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
	var agents []string
	total := 0
	if m.plan.plan != nil {
		for i, pp := range m.plan.plan.Panes {
			if pp.Manifest.Agent != "" {
				total++
				if m.plan.sel[i] {
					agents = append(agents, pp.Manifest.Key)
				}
			}
		}
	} else {
		// server down: restore everything the snapshot holds
		for _, p := range m.plan.snap.AgentPanes() {
			agents = append(agents, p.Key)
		}
		total = len(agents)
	}
	if len(agents) == 0 {
		m.note = "select at least one agent (space)"
		return m, nil
	}
	self, err := os.Executable()
	if err != nil {
		m.note = err.Error()
		return m, nil
	}
	args := []string{"resume", "--session", m.cur.name, "--from", m.plan.path, "--yes"}
	if m.plan.plan != nil && len(agents) < total {
		for _, a := range agents {
			args = append(args, "--agent", a)
		}
	}
	m.note = "restore: " + strings.Join(agents, ", ")
	return m, tea.ExecProcess(exec.Command(self, args...), func(err error) tea.Msg {
		return refreshedMsg{}
	})
}
