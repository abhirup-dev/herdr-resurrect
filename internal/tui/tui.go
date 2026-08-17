// Package tui is the browse picker: sessions -> snapshots -> live diff plan,
// with confirm-to-execute. Minimal Bubble Tea + Lip Gloss.
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
)

var (
	styHeader  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	styDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	stySel     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("6"))
	styKey     = lipgloss.NewStyle().Bold(true)
	styVerdict = map[string]lipgloss.Style{
		"KEEP-NATIVE": lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		"REPLACE":     lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
		"RELAUNCH":    lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		"RESURRECT":   lipgloss.NewStyle().Foreground(lipgloss.Color("5")),
		"SHELL":       lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
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
	snaps   []string // newest first, full paths
	last    string   // snapshot the `last` symlink points at
}

type planData struct {
	snap    *manifest.Snapshot
	path    string
	plan    *resume.Plan // nil when the server is down
	planErr string
}

type model struct {
	mode     mode
	sessions []sessionRow
	sel      int // selection within current view
	cur      *sessionRow
	curSnap  string
	plan     *planData
	execNote string
	err      error
}

type refreshedMsg struct{}

type planMsg struct {
	pd  *planData
	err error
}

// loadPlanCmd builds the plan off the UI thread: the settle-wait can take
// seconds and would freeze Update if run inline.
func loadPlanCmd(session, snapPath string) tea.Cmd {
	return func() tea.Msg {
		pd := &planData{path: snapPath}
		snap, err := manifest.Load(snapPath)
		if err != nil {
			return planMsg{err: err}
		}
		pd.snap = snap
		live, err := capture.Session(capture.Options{Session: session})
		if err != nil {
			pd.planErr = fmt.Sprintf("server down or busy: %v", err)
			return planMsg{pd: pd}
		}
		resume.Settle(session, 10*time.Second)
		pd.plan = resume.Diff(snap, live)
		return planMsg{pd: pd}
	}
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

func (m *model) loadSessions() error {
	sess, err := capture.Sessions()
	if err != nil {
		return err
	}
	// archive dirs may hold sessions that are not currently running
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
		last, _ := filepath.EvalSymlinks(filepath.Join(dir, "last"))
		matches, _ := filepath.Glob(filepath.Join(dir, "herdr_*.json"))
		sort.Sort(sort.Reverse(sort.StringSlice(matches)))
		row.snaps = matches
		row.last = last
		m.sessions = append(m.sessions, row)
	}
	return nil
}

func (m *model) loadPlan() tea.Cmd {
	return loadPlanCmd(m.cur.name, m.curSnap)
}

func (m *model) rows() []string {
	switch m.mode {
	case viewSessions:
		var out []string
		for _, s := range m.sessions {
			state := styDim.Render("stopped")
			if s.running {
				state = styVerdict["KEEP-NATIVE"].Render("running")
			}
			out = append(out, fmt.Sprintf("  %-16s %-9s %d snapshot(s)", s.name, state, len(s.snaps)))
		}
		return out
	case viewSnapshots:
		var out []string
		for _, p := range m.cur.snaps {
			base := filepath.Base(p)
			ts := strings.TrimSuffix(strings.TrimPrefix(base, "herdr_"), ".json")
			when, _ := time.ParseInLocation("20060102T150405", ts, time.Local)
			human := styDim.Render("(unreadable time)")
			if !when.IsZero() {
				human = when.Format("Jan 02 15:04:05")
			}
			mark := "  "
			if p == m.cur.last {
				mark = styVerdict["KEEP-NATIVE"].Render("●")
			}
			out = append(out, fmt.Sprintf(" %s %s  %s", mark, human, styDim.Render(base)))
		}
		if len(out) == 0 {
			out = append(out, styDim.Render("  (no snapshots — capture first)"))
		}
		return out
	default: // viewPlan
		if m.plan == nil && m.err == nil {
			return []string{styDim.Render("  loading diff…")}
		}
		if m.plan == nil || m.plan.snap == nil {
			if m.err != nil {
				return []string{"  " + styVerdict["REPLACE"].Render(m.err.Error())}
			}
		}
		var out []string
		if m.plan.plan == nil {
			out = append(out, "  "+styVerdict["RELAUNCH"].Render("no live diff: "+m.plan.planErr))
			for _, w := range m.plan.snap.Workspaces {
				for _, t := range w.Tabs {
					for _, p := range t.Panes {
						out = append(out, fmt.Sprintf("  %-12s %-8s cwd=%s env=%d", p.Key, or(p.Agent, "shell"), p.Cwd, len(p.Env)))
					}
				}
			}
			return out
		}
		for _, pp := range m.plan.plan.Panes {
			st, ok := styVerdict[string(pp.Action)]
			if !ok {
				st = styDim
			}
			out = append(out, fmt.Sprintf("  %-12s %-12s %s", st.Render(string(pp.Action)), pp.Manifest.Key, styDim.Render(pp.Reason)))
		}
		for _, c := range m.plan.plan.Missing() {
			out = append(out, "  "+styVerdict["REPLACE"].Render("CONFLICT ")+styDim.Render(c))
		}
		return out
	}
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func (m *model) header() string {
	switch m.mode {
	case viewSessions:
		return styHeader.Render("herdr-archive — sessions")
	case viewSnapshots:
		return styHeader.Render("snapshots — "+m.cur.name) + styDim.Render("   ● = last")
	default:
		return styHeader.Render("plan — "+m.cur.name) + "  " + styDim.Render(filepath.Base(m.path()))
	}
}

func (m *model) path() string {
	if m.plan != nil {
		return m.plan.path
	}
	return ""
}

func (m *model) footer() string {
	switch m.mode {
	case viewSessions:
		return styDim.Render("↑/↓ select · enter snapshots · q quit")
	case viewSnapshots:
		return styDim.Render("enter plan · esc back · q quit")
	default:
		return styDim.Render("y resume --yes · r refresh · esc back · q quit")
	}
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.mode == viewPlan {
				m.mode = viewSnapshots
				m.plan, m.sel = nil, 0
				return m, nil
			}
			if m.mode == viewSnapshots {
				m.mode = viewSessions
				m.cur, m.sel = nil, 0
				return m, nil
			}
			return m, tea.Quit
		case "esc":
			if m.mode == viewPlan {
				m.mode, m.plan, m.sel = viewSnapshots, nil, 0
			} else if m.mode == viewSnapshots {
				m.mode, m.cur, m.sel = viewSessions, nil, 0
			} else {
				return m, tea.Quit
			}
			return m, nil
		case "up", "k":
			if m.sel > 0 {
				m.sel--
			}
		case "down", "j":
			if m.sel < len(m.rows())-1 {
				m.sel++
			}
		case "enter":
			switch m.mode {
			case viewSessions:
				if len(m.sessions) == 0 {
					return m, nil
				}
				m.cur = &m.sessions[m.sel]
				m.mode, m.sel = viewSnapshots, 0
			case viewSnapshots:
				if m.cur == nil || len(m.cur.snaps) == 0 || m.sel >= len(m.cur.snaps) {
					return m, nil
				}
				m.curSnap = m.cur.snaps[m.sel]
				m.mode, m.sel = viewPlan, 0
				return m, m.loadPlan()
			}
		case "y":
			if m.mode == viewPlan && m.plan != nil && m.plan.snap != nil {
				self, err := os.Executable()
				if err != nil {
					m.execNote = err.Error()
					return m, nil
				}
				cmd := exec.Command(self, "resume", "--session", m.cur.name, "--from", m.plan.path, "--yes")
				m.execNote = "ran: resume " + m.cur.name + " --yes"
				return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
					_ = err
					return refreshedMsg{}
				})
			}
		case "r":
			if m.mode == viewPlan && m.plan != nil {
				return m, m.loadPlan()
			}
		}
	case planMsg:
		m.plan, m.err = msg.pd, msg.err
	case refreshedMsg:
		if m.mode == viewPlan {
			return m, m.loadPlan()
		}
	}
	return m, nil
}

func (m *model) View() string {
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n\n")
	rows := m.rows()
	for i, r := range rows {
		if i == m.sel {
			b.WriteString(stySel.Render(clipLine(r, 100)))
		} else {
			b.WriteString(r)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	if m.execNote != "" {
		b.WriteString(styDim.Render(m.execNote) + "\n")
	}
	b.WriteString(m.footer())
	return b.String()
}

// clipLine trims ANSI-heavy lines so the popup stays readable at 80%.
func clipLine(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
