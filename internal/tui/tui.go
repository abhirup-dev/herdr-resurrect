// Package tui is the browse picker: sessions -> snapshots -> plan with
// per-agent restore selection. Minimal Bubble Tea + Lip Gloss.
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/abhirupdas/herdr-archive/internal/capture"
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
	stySel     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("6"))
	styFrame   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("8"))
	styVerdict = map[string]lipgloss.Style{
		"KEEP-NATIVE": styOK,
		"REPLACE":     styBad,
		"RELAUNCH":    styWarn,
		"RESURRECT":   lipgloss.NewStyle().Foreground(lipgloss.Color("5")),
		"SHELL":       styDim,
	}
)

type mode int

const (
	viewSessions mode = iota
	viewSnapshots
	viewPlan
)

const rowsShown = 12

type sessionRow struct {
	name     string
	running  bool
	snaps    []string // newest first, full paths
	last     string   // snapshot the `last` symlink points at
	latest   *manifest.Snapshot
	agents   int
	origName string // captured session name inside the latest snapshot
}

type snapshotRow struct {
	path string
	snap *manifest.Snapshot
	err  error
}

type planRow struct {
	pp       resume.PanePlan
	size     int64
	provider string
	selected bool
}

type model struct {
	mode     mode
	sessions []sessionRow
	snaps    []snapshotRow
	sel      int
	scroll   int
	cur      *sessionRow
	curSnap  string
	plan     *planData
	note     string
	err      error
}

type planData struct {
	snap    *manifest.Snapshot
	path    string
	plan    *resume.Plan
	planErr string
	running bool
	rows    []planRow
}

type refreshedMsg struct{}
type planMsg struct {
	pd  *planData
	err error
}

func Run() error {
	m := &model{}
	if err := m.loadSessions(); err != nil {
		return err
	}
	if len(m.sessions) == 0 {
		return fmt.Errorf("no herdr sessions found")
	}
	_, err := tea.NewProgram(m).Run()
	return err
}

// ---- data loading -----------------------------------------------------

func (m *model) loadSessions() error {
	sess, err := capture.Sessions()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, s := range sess {
		seen[s.Name] = true
	}
	root := manifest.DefaultRoot()
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if !e.IsDir() || seen[e.Name()] {
			continue
		}
		sess = append(sess, capture.SessionInfo{Name: e.Name()})
	}
	for i := range sess {
		row := sessionRow{name: sess[i].Name, running: sess[i].Running}
		dir := manifest.Dir("", row.name)
		row.last, _ = filepath.EvalSymlinks(filepath.Join(dir, "last"))
		matches, _ := filepath.Glob(filepath.Join(dir, "herdr_*.json"))
		sort.Sort(sort.Reverse(sort.StringSlice(matches)))
		row.snaps = matches
		if len(matches) > 0 {
			if snap, err := manifest.Load(matches[0]); err == nil {
				row.latest = snap
				row.agents = len(snap.AgentPanes())
			}
		}
		m.sessions = append(m.sessions, row)
	}
	return nil
}

// refreshSessions reloads session stats after an action, preserving the
// selection by name.
func (m *model) refreshSessions() {
	sel := ""
	if m.mode == viewSessions && m.sel < len(m.sessions) {
		sel = m.sessions[m.sel].name
	}
	if m.cur != nil {
		sel = m.cur.name
	}
	_ = m.loadSessions()
	if sel != "" {
		for i := range m.sessions {
			if m.sessions[i].name == sel {
				m.sel = i
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
		return planMsg{pd: pd}
	}
}

// buildRows materializes plan rows with defaults: agents selected, shells not.
func (pd *planData) buildRows() {
	pd.rows = nil
	if pd.plan == nil {
		return
	}
	for _, pp := range pd.plan.Panes {
		r := planRow{
			pp:       pp,
			size:     strategy.TranscriptSize(pp.Manifest.Agent, pp.Manifest.SID, pp.Manifest.Env),
			provider: strategy.ProviderLabel(pp.Manifest.Agent, pp.Manifest.Env),
			selected: pp.Manifest.Agent != "",
		}
		pd.rows = append(pd.rows, r)
	}
}

// ---- formatting helpers ----------------------------------------------

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

// ---- views ------------------------------------------------------------

func (m *model) title() string {
	switch m.mode {
	case viewSessions:
		return styTitle.Render(" herdr-archive ") + styDim.Render(" sessions")
	case viewSnapshots:
		return styTitle.Render(" herdr-archive ") + styDim.Render(" "+m.cur.name+" › snapshots")
	default:
		return styTitle.Render(" herdr-archive ") + styDim.Render(" "+m.cur.name+" › restore plan")
	}
}

// body renders the current view and returns a line map: for each rendered
// line, the index of the selectable thing it represents (-1 = banner/plain
// text the cursor skips).
func (m *model) body() (lines []string, lineMap []int) {
	add := func(s string, sel int) {
		lines = append(lines, s)
		lineMap = append(lineMap, sel)
	}
	switch m.mode {
	case viewSessions:
		for i, s := range m.sessions {
			badge := styBad.Render("stopped")
			if s.running {
				badge = styOK.Render("running")
			}
			info := styDim.Render("no snapshots — press c to capture")
			if s.latest != nil {
				agents, size, _ := snapshotStats(s.latest)
				info = fmt.Sprintf("%s · %s %s", styDim.Render("last "+relTime(s.latest.CreatedAt.Local())),
					styDim.Render(fmt.Sprintf("%d %s", s.agents, plural(s.agents, "agent"))),
					styDim.Render("· "+humanBytes(size)+" transcripts · "+agents))
			}
			add(fmt.Sprintf("  %-14s %-8s %s", s.name, badge, info), i)
		}
		return lines, lineMap
	case viewSnapshots:
		for i, r := range m.snaps {
			if r.err != nil || r.snap == nil {
				add("  "+styBad.Render("unreadable snapshot"), -1)
				continue
			}
			agents, size, n := snapshotStats(r.snap)
			mark := "  "
			if r.path == m.cur.last {
				mark = styOK.Render("●")
			}
			add(fmt.Sprintf(" %s %-10s %s %s", mark, relTime(r.snap.CreatedAt.Local()),
				styDim.Render(fmt.Sprintf("%d %s · %s", n, plural(n, "agent"), agents)),
				styDim.Render("· "+humanBytes(size))), i)
		}
		if len(m.snaps) == 0 {
			add(styDim.Render("  no snapshots yet — press c to capture this session"), -1)
		}
		return lines, lineMap
	default: // viewPlan
		if m.plan == nil {
			if m.err != nil {
				add("  "+styBad.Render(m.err.Error()), -1)
			} else {
				add(styDim.Render("  loading diff…"), -1)
			}
			return lines, lineMap
		}
		if m.plan.plan == nil {
			// server down: show what a restore would bring back
			add("  "+styWarn.Render("session stopped — y will boot it and restore this snapshot"), -1)
			agents, size, n := snapshotStats(m.plan.snap)
			add(styDim.Render(fmt.Sprintf("  %d %s · %s · %s transcripts", n, plural(n, "agent"), agents, humanBytes(size))), -1)
			add("", -1)
			for i, p := range m.plan.snap.AgentPanes() {
				prov := strategy.ProviderLabel(p.Agent, p.Env)
				add(fmt.Sprintf("  %s %-14s %s", styOK.Render("[x]"), p.Key, styDim.Render(p.Agent+"→"+prov)), i)
			}
			return lines, lineMap
		}
		banner := styOK.Render("running") + styDim.Render(" — resume sweeps in place, live panes are kept")
		if m.plan.running {
			allKeep := true
			for _, r := range m.plan.rows {
				if r.pp.Action != resume.KeepNative && r.pp.Action != resume.ShellKeep {
					allKeep = false
				}
			}
			if allKeep {
				banner = styOK.Render("running & healthy") + styDim.Render(" — nothing to do")
			}
		}
		agents, size, n := snapshotStats(m.plan.snap)
		add("  "+banner, -1)
		add(styDim.Render(fmt.Sprintf("  snapshot %s · %d %s · %s · %s transcripts",
			relTime(m.plan.snap.CreatedAt.Local()), n, plural(n, "agent"), agents, humanBytes(size))), -1)
		add("", -1)
		for i, r := range m.plan.rows {
			box := "[ ]"
			if r.selected {
				box = styOK.Render("[x]")
			}
			verdict := styVerdict[string(r.pp.Action)]
			kind := r.pp.Manifest.Agent
			who := kind
			if r.provider != kind && r.provider != "" {
				who = kind + styDim.Render("→") + r.provider
			}
			add(fmt.Sprintf("  %s %-14s %-19s %-11s %-7s %s", box, r.pp.Manifest.Key, who,
				verdict.Render(string(r.pp.Action)), styDim.Render(humanBytes(r.size)),
				styDim.Render(clip(r.reason(), 30))), i)
		}
		return lines, lineMap
	}
}

// selectableAt reports whether line idx carries a cursor target.
func (m *model) selectable(idx int) bool {
	_, lineMap := m.body()
	return idx >= 0 && idx < len(lineMap) && lineMap[idx] >= 0
}

func (m *model) footer() string {
	switch m.mode {
	case viewSessions:
		return "enter snapshots · c capture now · q quit"
	case viewSnapshots:
		return "enter plan · c capture now · esc back · q quit"
	default:
		return "space select · a all/none · y restore selected · r refresh · esc back"
	}
}

func (r planRow) reason() string {
	if r.pp.Action == resume.ShellKeep {
		return "shell pane"
	}
	return r.pp.Reason
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func plural(n int, s string) string {
	if n == 1 {
		return s
	}
	return s + "s"
}

// ---- bubble tea --------------------------------------------------------

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if m.mode == viewPlan {
				m.mode, m.plan, m.sel, m.scroll = viewSnapshots, nil, 0, 0
				return m, nil
			}
			if m.mode == viewSnapshots {
				m.mode, m.cur, m.sel, m.scroll = viewSessions, nil, 0, 0
				return m, nil
			}
			return m, tea.Quit
		case "esc":
			if m.mode == viewPlan {
				m.mode, m.plan, m.sel, m.scroll = viewSnapshots, nil, 0, 0
			} else if m.mode == viewSnapshots {
				m.mode, m.cur, m.sel, m.scroll = viewSessions, nil, 0, 0
			} else {
				return m, tea.Quit
			}
			return m, nil
		case "up", "k":
			for m.sel > 0 {
				m.sel--
				if m.selectable(m.sel) {
					break
				}
			}
			m.fixScroll()
		case "down", "j":
			_, lineMap := m.body()
			for m.sel < len(lineMap)-1 {
				m.sel++
				if m.selectable(m.sel) {
					break
				}
			}
			m.fixScroll()
		case "enter":
			switch m.mode {
			case viewSessions:
				if len(m.sessions) == 0 || !m.selectable(m.sel) {
					return m, nil
				}
				m.cur = &m.sessions[m.sel]
				m.loadSnaps()
				m.mode, m.sel, m.scroll = viewSnapshots, 0, 0
			case viewSnapshots:
				_, lineMap := m.body()
				if !m.selectable(m.sel) {
					return m, nil
				}
				m.curSnap = m.snaps[lineMap[m.sel]].path
				m.mode, m.sel, m.scroll, m.plan = viewPlan, 0, 0, nil
				return m, loadPlanCmd(m.cur.name, m.curSnap)
			}
		case "space", " ":
			if m.mode == viewPlan && m.plan != nil && m.plan.plan != nil {
				_, lineMap := m.body()
				if !m.selectable(m.sel) {
					return m, nil
				}
				row := &m.plan.rows[lineMap[m.sel]]
				if row.pp.Manifest.Agent != "" {
					row.selected = !row.selected
				}
			}
		case "a":
			if m.mode == viewPlan && m.plan != nil && m.plan.plan != nil {
				allSel := true
				for _, r := range m.plan.rows {
					if r.pp.Manifest.Agent != "" && !r.selected {
						allSel = false
					}
				}
				for i := range m.plan.rows {
					if m.plan.rows[i].pp.Manifest.Agent != "" {
						m.plan.rows[i].selected = !allSel
					}
				}
			}
		case "y":
			if m.mode == viewPlan && m.plan != nil && m.plan.snap != nil {
				var agents []string
				total := 0
				for _, r := range m.plan.rows {
					if r.pp.Manifest.Agent != "" {
						total++
						if r.selected {
							agents = append(agents, r.pp.Manifest.Key)
						}
					}
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
				if len(agents) < total {
					for _, a := range agents {
						args = append(args, "--agent", a)
					}
				}
				m.note = "restore: " + strings.Join(agents, ", ")
				cmd := exec.Command(self, args...)
				return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
					_ = err
					return refreshedMsg{}
				})
			}
		case "c":
			if m.mode == viewSessions || m.mode == viewSnapshots {
				name := ""
				if m.mode == viewSessions && m.sel < len(m.sessions) {
					name = m.sessions[m.sel].name
				}
				if m.mode == viewSnapshots && m.cur != nil {
					name = m.cur.name
				}
				if name == "" {
					return m, nil
				}
				self, _ := os.Executable()
				m.note = "capture: " + name
				cmd := exec.Command(self, "capture", "--session", name)
				return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
					_ = err
					return refreshedMsg{}
				})
			}
		case "r":
			if m.mode == viewPlan && m.plan != nil {
				m.plan = nil
				return m, loadPlanCmd(m.cur.name, m.curSnap)
			}
			if m.mode == viewSessions {
				m.refreshSessions()
			}
		}
	case planMsg:
		m.plan, m.err = msg.pd, msg.err
		if m.plan != nil {
			m.plan.buildRows()
		}
	case refreshedMsg:
		m.refreshSessions()
		if m.mode == viewPlan && m.plan != nil {
			return m, loadPlanCmd(m.cur.name, m.curSnap)
		}
	}
	return m, nil
}

func (m *model) fixScroll() {
	if m.sel < m.scroll {
		m.scroll = m.sel
	}
	if m.sel >= m.scroll+rowsShown {
		m.scroll = m.sel - rowsShown + 1
	}
}

func (m *model) View() string {
	lines, _ := m.body()
	start, end := 0, len(lines)
	if end > rowsShown {
		start, end = m.scroll, m.scroll+rowsShown
		if m.sel < start || m.sel >= end {
			start = m.sel
			end = start + rowsShown
		}
	}
	var b strings.Builder
	b.WriteString(m.title())
	b.WriteString("\n")
	b.WriteString(styDim.Render(strings.Repeat("─", 72)))
	b.WriteString("\n")
	for i, line := range lines[start:end] {
		idx := start + i
		text := clipANSI(line, 96)
		if idx == m.sel {
			b.WriteString(stySel.Render(pad(text, 98)))
		} else {
			b.WriteString(text)
		}
		b.WriteString("\n")
	}
	b.WriteString(styDim.Render(strings.Repeat("─", 72)))
	b.WriteString("\n")
	if m.note != "" {
		b.WriteString(styWarn.Render(clipANSI(m.note, 72)) + "\n")
	}
	b.WriteString(styDim.Render(" " + m.footer()))
	return styFrame.Render(b.String())
}

// clipANSI trims a styled line to n visible cells without breaking escapes.
func clipANSI(s string, n int) string {
	var b strings.Builder
	cells := 0
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			b.WriteRune(r)
			continue
		}
		if inEsc {
			b.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		if cells >= n {
			return b.String()
		}
		cells++
		b.WriteRune(r)
	}
	return b.String()
}

// pad pads a styled line to n visible cells for full-row highlight.
func pad(s string, n int) string {
	cells := 0
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		cells++
	}
	if cells >= n {
		return s
	}
	return s + strings.Repeat(" ", n-cells)
}
