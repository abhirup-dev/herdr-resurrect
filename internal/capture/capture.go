// Package capture walks a herdr session and records a faithful snapshot.
// Faithful means: the pane's live environment is captured verbatim from the
// process table (ps Ewww) and replayed blindly. Capture assumes no knowledge
// of how that environment was produced — launchers, wrappers, or shells.
package capture

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/abhirup-dev/herdr-resurrect/internal/herdr"
	"github.com/abhirup-dev/herdr-resurrect/internal/manifest"
)

type wsEntry struct {
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
}
type tabEntry struct {
	TabID       string `json:"tab_id"`
	Label       string `json:"label"`
	WorkspaceID string `json:"workspace_id"`
}
type paneEntry struct {
	PaneID      string `json:"pane_id"`
	TabID       string `json:"tab_id"`
	Cwd         string `json:"cwd"`
	WorkspaceID string `json:"workspace_id"`
}
type agentEntry struct {
	Name          string `json:"name"`
	Agent         string `json:"agent"`
	Status        string `json:"agent_status"`
	Cwd           string `json:"cwd"`
	PaneID        string `json:"pane_id"`
	TerminalTitle string `json:"terminal_title"`
	AgentSession  *struct {
		Value  string `json:"value"`
		Source string `json:"source"`
	} `json:"agent_session"`
}
type processInfo struct {
	ForegroundProcessGroupID int `json:"foreground_process_group_id"`
	ForegroundProcesses      []struct {
		Argv    []string `json:"argv"`
		Cmdline string   `json:"cmdline"`
		Cwd     string   `json:"cwd"`
		Pid     int      `json:"pid"`
	} `json:"foreground_processes"`
	ShellPid int `json:"shell_pid"`
}

type sessionEntry struct {
	Name       string `json:"name"`
	Default    bool   `json:"default"`
	Running    bool   `json:"running"`
	SessionDir string `json:"session_dir"`
}

// Options controls a capture run.
type Options struct {
	Session      string // "" or "default" = default session
	WorkspaceIDs []string
	PaneIDs      []string // non-empty creates a curated snapshot with full topology context
}

var envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Session captures the live session named opts.Session.
func Session(opts Options) (*manifest.Snapshot, error) {
	name := opts.Session
	if name == "" {
		name = "default"
	}
	sess, err := findSession(name)
	if err != nil {
		return nil, err
	}
	scope := herdr.SessionScope(sess.Name)
	if !sess.Running {
		return nil, fmt.Errorf("session %q is not running; nothing to capture", name)
	}

	snap := &manifest.Snapshot{
		Version:    manifest.Version,
		CreatedAt:  time.Now().UTC(),
		Session:    sess.Name,
		SessionDir: sess.SessionDir,
	}
	curated := len(opts.PaneIDs) > 0
	selectedPanes := map[string]bool{}
	for _, paneID := range opts.PaneIDs {
		selectedPanes[paneID] = true
	}
	if curated {
		snap.CaptureScope = &manifest.CaptureScope{}
	}

	var wsList struct {
		Workspaces []wsEntry `json:"workspaces"`
	}
	if err := herdr.RunInto(&wsList, append(scope, "workspace", "list")...); err != nil {
		return nil, err
	}

	var agents struct {
		Agents []agentEntry `json:"agents"`
	}
	if err := herdr.RunInto(&agents, append(scope, "agent", "list")...); err != nil {
		return nil, err
	}
	agentsByPane := map[string]agentEntry{}
	for _, a := range agents.Agents {
		agentsByPane[a.PaneID] = a
	}

	for _, w := range wsList.Workspaces {
		if len(opts.WorkspaceIDs) > 0 && !contains(opts.WorkspaceIDs, w.WorkspaceID) {
			continue
		}
		var tabs struct {
			Tabs []tabEntry `json:"tabs"`
		}
		if err := herdr.RunInto(&tabs, append(scope, "tab", "list", "--workspace", w.WorkspaceID)...); err != nil {
			return nil, err
		}
		var panes struct {
			Panes []paneEntry `json:"panes"`
		}
		if err := herdr.RunInto(&panes, append(scope, "pane", "list", "--workspace", w.WorkspaceID)...); err != nil {
			return nil, err
		}
		panesByTab := map[string][]paneEntry{}
		for _, p := range panes.Panes {
			panesByTab[p.TabID] = append(panesByTab[p.TabID], p)
		}

		mw := manifest.Workspace{ID: w.WorkspaceID, Label: w.Label}
		for _, t := range tabs.Tabs {
			mt := manifest.Tab{ID: t.TabID, Label: t.Label}
			if tabPanes := panesByTab[t.TabID]; len(tabPanes) > 0 {
				var layout struct {
					Layout manifest.Layout `json:"layout"`
				}
				if err := herdr.RunInto(&layout, append(scope, "pane", "layout", "--pane", tabPanes[0].PaneID)...); err == nil {
					mt.Layout = &layout.Layout
				}
			}
			for _, p := range panesByTab[t.TabID] {
				hydrate := !curated || selectedPanes[p.PaneID]
				mp, err := capturePane(scope, p, agentsByPane[p.PaneID], hydrate)
				if err != nil {
					return nil, err
				}
				if mw.Cwd == "" {
					mw.Cwd = mp.Cwd
				}
				mt.Panes = append(mt.Panes, mp)
				if hydrate && snap.CaptureScope != nil {
					snap.CaptureScope.Panes = append(snap.CaptureScope.Panes, manifest.PaneRef{
						WorkspaceID: w.WorkspaceID,
						TabID:       t.TabID,
						PaneKey:     mp.Key,
					})
					delete(selectedPanes, p.PaneID)
				}
			}
			mw.Tabs = append(mw.Tabs, mt)
		}
		snap.Workspaces = append(snap.Workspaces, mw)
	}
	if len(selectedPanes) > 0 {
		var missing []string
		for _, paneID := range opts.PaneIDs {
			if selectedPanes[paneID] {
				missing = append(missing, paneID)
			}
		}
		return nil, fmt.Errorf("selected panes not found: %s", strings.Join(missing, ", "))
	}
	return snap, nil
}

// capturePane records one pane. An agent pane keeps argv + live env; a shell
// pane keeps only cwd (herdr snapshot restore already brings shells back).
func capturePane(scope []string, p paneEntry, agent agentEntry, hydrate bool) (manifest.Pane, error) {
	mp := manifest.Pane{
		PaneID: p.PaneID,
		Cwd:    p.Cwd,
	}
	if agent.Agent != "" {
		mp.Agent = agent.Agent
		mp.Name = agent.Name
		mp.State = agent.Status
		mp.Title = agent.TerminalTitle
		mp.Cwd = firstNonEmpty(agent.Cwd, p.Cwd)
		if agent.AgentSession != nil {
			mp.SID = agent.AgentSession.Value
			mp.SIDSource = agent.AgentSession.Source
		}
	}
	mp.Key = firstNonEmpty(mp.Name, p.PaneID)
	if !hydrate {
		return mp, nil
	}

	var info struct {
		ProcessInfo processInfo `json:"process_info"`
	}
	if err := herdr.RunInto(&info, append(scope, "pane", "process-info", "--pane", p.PaneID)...); err != nil {
		return mp, fmt.Errorf("pane %s: %w", p.PaneID, err)
	}
	fg := foreground(&info.ProcessInfo, agent.Agent)
	if fg == nil {
		mp.Shell = true
		return mp, nil
	}
	mp.Argv = fg.Argv
	mp.Cmdline = fg.Cmdline
	if mp.Agent == "" {
		// No agent, but a foreground command may still be running.
		mp.Shell = fg.Pid != info.ProcessInfo.ShellPid
		if mp.Shell {
			env, _ := psEnv(fg.Pid)
			mp.Env = env
		}
		return mp, nil
	}
	env, err := psEnv(fg.Pid)
	if err != nil {
		return mp, fmt.Errorf("pane %s env: %w", p.PaneID, err)
	}
	mp.Env = env
	return mp, nil
}

// foreground picks the pane's foreground process: the process whose pid leads
// the foreground process group. Falls back to matching the detected agent
// name in argv[0].
func foreground(info *processInfo, agentKind string) *struct {
	Argv    []string `json:"argv"`
	Cmdline string   `json:"cmdline"`
	Cwd     string   `json:"cwd"`
	Pid     int      `json:"pid"`
} {
	for i := range info.ForegroundProcesses {
		p := &info.ForegroundProcesses[i]
		if info.ForegroundProcessGroupID != 0 && p.Pid == info.ForegroundProcessGroupID {
			return p
		}
	}
	for i := range info.ForegroundProcesses {
		p := &info.ForegroundProcesses[i]
		if agentKind != "" && len(p.Argv) > 0 && strings.Contains(p.Argv[0], agentKind) {
			return p
		}
	}
	return nil
}

// psEnv reads a process's live environment. macOS-only for now
// (`ps Ewww`); Linux would use /proc/<pid>/environ.
func psEnv(pid int) (map[string]string, error) {
	out, err := exec.Command("ps", "Ewww", "-p", fmt.Sprint(pid), "-o", "command=").Output()
	if err != nil {
		return nil, err
	}
	env := map[string]string{}
	fields := strings.Fields(string(out))
	for _, f := range fields[1:] { // fields[0] is argv0
		if k, v, ok := strings.Cut(f, "="); ok && envKey.MatchString(k) && k != "" {
			env[k] = v
		}
	}
	// Empty env is legitimate: some agents (pi) scrub their own environ at
	// startup, and capturing that faithfully is the whole point.
	return env, nil
}

// SessionInfo is the exported session-listing row.
type SessionInfo sessionEntry

// LiveAgents returns the names of agents currently alive in a session, so
// callers can tell a "running" session whose fleet died (stale) from a
// faithful one.
func LiveAgents(session string) ([]string, error) {
	entries, err := liveAgentEntries(session)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, agent := range entries {
		if agent.Name != "" {
			names = append(names, agent.Name)
		}
	}
	return names, nil
}

// LiveActivity reports recognized live agents and the workspaces they occupy.
func LiveActivity(session string) (agents, workspaces int, err error) {
	entries, err := liveAgentEntries(session)
	if err != nil {
		return 0, 0, err
	}
	agents, workspaces = liveActivity(entries)
	return agents, workspaces, nil
}

func liveAgentEntries(session string) ([]agentEntry, error) {
	var list struct {
		Agents []agentEntry `json:"agents"`
	}
	if err := herdr.RunInto(&list, append(herdr.SessionScope(session), "agent", "list")...); err != nil {
		return nil, err
	}
	return list.Agents, nil
}

func liveActivity(entries []agentEntry) (agents, workspaces int) {
	populated := map[string]bool{}
	for _, agent := range entries {
		if agent.Agent == "" {
			continue
		}
		agents++
		if workspace, _, ok := strings.Cut(agent.PaneID, ":p"); ok && workspace != "" {
			populated[workspace] = true
		}
	}
	return agents, len(populated)
}

// Sessions lists every herdr session on the machine.
func Sessions() ([]SessionInfo, error) {
	var list struct {
		Sessions []SessionInfo `json:"sessions"`
	}
	if err := herdr.RunRawInto(&list, "session", "list", "--json"); err != nil {
		return nil, err
	}
	return list.Sessions, nil
}

func findSession(name string) (*sessionEntry, error) {
	sessions, err := Sessions()
	if err != nil {
		return nil, err
	}
	for i := range sessions {
		s := (*sessionEntry)(&sessions[i])
		if s.Default && name == "default" || s.Name == name {
			return s, nil
		}
	}
	return nil, fmt.Errorf("session %q not found", name)
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}
