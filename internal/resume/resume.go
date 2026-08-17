// Package resume brings a captured session back env-faithfully.
//
// The sweep is a diff: the disk manifest vs a fresh live capture of the
// same session. The same diff powers dry-run plans, post-attach sweeps, and
// in-place repair of a running session.
package resume

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/abhirupdas/herdr-archive/internal/capture"
	"github.com/abhirupdas/herdr-archive/internal/herdr"
	"github.com/abhirupdas/herdr-archive/internal/manifest"
	"github.com/abhirupdas/herdr-archive/internal/strategy"
)

type Action string

const (
	KeepNative Action = "KEEP-NATIVE" // live pane is faithful; nothing to do
	Replace    Action = "REPLACE"     // env/provider lost: relaunch from manifest
	Relaunch   Action = "RELAUNCH"    // no sid: fresh conversation, argv+env replay
	ShellKeep  Action = "SHELL"       // shell pane; snapshot restore covers it
	Resurrect  Action = "RESURRECT"   // manifest pane with no live counterpart
)

type PanePlan struct {
	Manifest manifest.Pane
	Live     *manifest.Pane // nil when the pane vanished entirely
	Action   Action
	Fresh    bool // true = relaunch without --resume (sid dead or absent)
	Reason   string
}

type Plan struct {
	Session string
	Panes   []PanePlan
}

// Missing reports pane-level conflicts worth surfacing before apply.
func (p *Plan) Missing() []string {
	var out []string
	for _, w := range p.Panes {
		if w.Action == Relaunch {
			out = append(out, fmt.Sprintf("%s: no native session id in manifest; resume = fresh conversation", w.Manifest.Key))
		}
		if w.Manifest.Cwd != "" {
			if _, err := os.Stat(w.Manifest.Cwd); err != nil {
				out = append(out, fmt.Sprintf("%s: cwd %s missing on disk", w.Manifest.Key, w.Manifest.Cwd))
			}
		}
	}
	return out
}

// Diff builds a plan by comparing the manifest against a live capture.
func Diff(snap *manifest.Snapshot, live *manifest.Snapshot) *Plan {
	p := &Plan{Session: snap.Session}
	liveByName, liveByPane := index(live)
	for _, w := range snap.Workspaces {
		for _, t := range w.Tabs {
			for _, mp := range t.Panes {
				pp := PanePlan{Manifest: mp}
				if !strategy.SessionOnDisk(mp.Agent, mp.SID, mp.Env) && mp.Agent != "" {
					pp.Fresh = true
				}
				lp, ok := liveByName[matchKey(mp)]
				if !ok {
					lp, ok = liveByPane[mp.PaneID]
				}
				switch {
				case mp.Agent == "":
					pp.Action, pp.Reason = ShellKeep, "shell pane; herdr snapshot restore handles it"
				case !ok:
					pp.Action = Resurrect
					pp.Reason = resurrectReason(pp.Fresh)
				default:
					pp.Live = &lp
					lost, changed := envDrift(mp.Env, lp.Env)
					// A live pane whose env matches the manifest is
					// faithful as-is — a dead sid alone never justifies
					// replacing a working agent.
					switch {
					case len(lost) > 0:
						pp.Action, pp.Reason = Replace, fmt.Sprintf("lost env: %s", clip(lost))
					case len(changed) > 0:
						pp.Action, pp.Reason = Replace, fmt.Sprintf("provider env changed: %s", clip(changed))
					default:
						pp.Action, pp.Reason = KeepNative, "live env matches manifest"
					}
				}
				p.Panes = append(p.Panes, pp)
			}
		}
	}
	return p
}

func resurrectReason(fresh bool) string {
	if fresh {
		return "no live counterpart; session not on disk, relaunch fresh with env"
	}
	return "no live counterpart"
}

// clip shortens env-drift key lists for one-line plans.
func clip(keys []string) string {
	const max = 6
	if len(keys) <= max {
		return strings.Join(keys, ",")
	}
	return fmt.Sprintf("%s,+%d more", strings.Join(keys[:max], ","), len(keys)-max)
}

func matchKey(p manifest.Pane) string {
	// Agent name is the stable identity; pane ids are the fallback and are
	// informational only unless they happen to align after a snapshot restore.
	if p.Name != "" {
		return "name:" + p.Name
	}
	return "pane:" + p.PaneID
}

func index(s *manifest.Snapshot) (byName, byPane map[string]manifest.Pane) {
	byName, byPane = map[string]manifest.Pane{}, map[string]manifest.Pane{}
	for _, w := range s.Workspaces {
		for _, t := range w.Tabs {
			for _, p := range t.Panes {
				byName[matchKey(p)] = p
				byPane[p.PaneID] = p
			}
		}
	}
	return byName, byPane
}

// Settle waits until the session's agent set stops changing: right after a
// restore, native resumes start at different speeds and an early diff would
// misreport live agents as missing.
func Settle(session string, timeout time.Duration) {
	type snap struct {
		names  string
		states string
	}
	prev := snap{"", ""}
	stable := 0
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) && stable < 3 {
		var list struct {
			Agents []struct {
				Name   string `json:"name"`
				Status string `json:"agent_status"`
			} `json:"agents"`
		}
		if err := herdr.RunInto(&list, append(herdr.SessionScope(session), "agent", "list")...); err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		names := make([]string, 0, len(list.Agents))
		states := make([]string, 0, len(list.Agents))
		for _, a := range list.Agents {
			names = append(names, a.Name)
			states = append(states, a.Status)
		}
		cur := snap{strings.Join(names, ","), strings.Join(states, ",")}
		if cur == prev {
			stable++
		} else {
			stable = 0
			prev = cur
		}
		time.Sleep(2 * time.Second)
	}
}

// envDrift compares manifest env against live env. lost = non-transient
// manifest keys absent live; changed = provider vars present both sides with
// different values.
func envDrift(man, live map[string]string) (lost, changed []string) {
	if len(man) == 0 {
		return nil, nil // nothing captured (e.g. pi scrubs env): faithful by definition
	}
	keys := make([]string, 0, len(man))
	for k := range man {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if strategy.TransientEnv(k) {
			continue
		}
		v, ok := live[k]
		if !ok {
			lost = append(lost, k)
		} else if strategy.ProviderVar(k) && v != man[k] {
			changed = append(changed, k)
		}
	}
	return lost, changed
}

// serverUp reports whether the session's server answers.
func serverUp(session string) bool {
	err := herdr.RunInto(&struct{}{}, append(herdr.SessionScope(session), "workspace", "list")...)
	return err == nil
}

// EnsureServer attaches the named session if its server is down. Requires
// running inside a herdr pane (a boot pane is split off and the attach runs
// there with the nesting guard's env vars scrubbed).
func EnsureServer(session string) error {
	if serverUp(session) {
		return nil
	}
	if os.Getenv("HERDR_ENV") != "1" {
		return fmt.Errorf("session %q server is down and we are not inside a herdr pane; run `herdr session attach %s` manually", session, session)
	}
	var split struct {
		Pane struct {
			PaneID string `json:"pane_id"`
		} `json:"pane"`
	}
	if err := herdr.RunInto(&split, "pane", "split", "--current", "--direction", "down", "--cwd", "/tmp", "--no-focus"); err != nil {
		return fmt.Errorf("boot pane: %w", err)
	}
	boot := split.Pane.PaneID
	scrub := "env -u HERDR_ENV -u HERDR_SOCKET_PATH -u HERDR_PANE_ID -u HERDR_WORKSPACE_ID -u HERDR_TAB_ID -u HERDR_SESSION"
	if _, err := herdr.Run("pane", "run", boot, fmt.Sprintf("%s herdr session attach %s", scrub, session)); err != nil {
		_, _ = herdr.Run("pane", "close", boot)
		return fmt.Errorf("attach: %w", err)
	}
	for i := 0; i < 45; i++ {
		if serverUp(session) {
			break
		}
		time.Sleep(time.Second)
	}
	if !serverUp(session) {
		return fmt.Errorf("session %q server did not come up within 45s", session)
	}
	// Detach the boot client; the server persists.
	_, _ = herdr.Run("pane", "send-keys", boot, "ctrl+b", "q")
	time.Sleep(500 * time.Millisecond)
	_, _ = herdr.Run("pane", "close", boot)
	return nil
}

// Apply executes REPLACE/RELAUNCH/RESURRECT entries. Each is one pane cycle:
// split a sibling with the manifest env injected, run the composed relaunch
// command, wait for agent detection, close the wrong pane.
func Apply(plan *Plan, dryRun bool) error {
	for _, pp := range plan.Panes {
		if pp.Action != Replace && pp.Action != Relaunch && pp.Action != Resurrect {
			continue
		}
		relaunch := strategy.LaunchCmdline(pp.Manifest.Argv, pp.Manifest.Cmdline, pp.Manifest.Agent, resumeArgsFor(pp))
		fmt.Printf("  %-12s %s\n", pp.Action, pp.Manifest.Key)
		fmt.Printf("               %s\n", relaunch)
		fmt.Printf("               cwd=%s env=%d vars\n", pp.Manifest.Cwd, len(strategy.ReplayEnv(pp.Manifest.Env)))
		if dryRun {
			continue
		}
		anchor := pp.Manifest.PaneID // restored panes keep ids
		if pp.Live != nil {
			anchor = pp.Live.PaneID
		}
		np, err := launchPane(plan.Session, anchor, pp, relaunch)
		if err != nil {
			return fmt.Errorf("%s: %w", pp.Manifest.Key, err)
		}
		_ = np
		if pp.Action != Resurrect && pp.Live != nil {
			if _, err := herdr.Run(append(herdr.SessionScope(plan.Session), "pane", "close", anchor)...); err != nil {
				return fmt.Errorf("%s: close old pane: %w", pp.Manifest.Key, err)
			}
		}
	}
	return nil
}

func resumeArgsFor(pp PanePlan) []string {
	if pp.Fresh {
		return nil // dead or absent sid: a --resume would crash the agent
	}
	args, ok := strategy.ResumeArgs(pp.Manifest.Agent, pp.Manifest.SID)
	if !ok && pp.Manifest.SID != "" {
		fmt.Printf("  !            %s: unknown kind %q; resuming argv verbatim\n", pp.Manifest.Key, pp.Manifest.Agent)
	}
	return args
}

// launchPane splits a new pane beside anchor with the manifest env injected,
// runs the relaunch command, waits for agent detection, and restores the
// captured name. Returns the new pane id.
func launchPane(session, anchor string, pp PanePlan, relaunch string) (string, error) {
	scope := herdr.SessionScope(session)
	args := append(append([]string{}, scope...), "pane", "split", "--pane", anchor,
		"--direction", "right", "--ratio", "0.5", "--cwd", pp.Manifest.Cwd, "--no-focus")
	for k, v := range strategy.ReplayEnv(pp.Manifest.Env) {
		args = append(args, "--env", k+"="+v)
	}
	var split struct {
		Pane struct {
			PaneID string `json:"pane_id"`
		} `json:"pane"`
	}
	if err := herdr.RunInto(&split, args...); err != nil {
		return "", fmt.Errorf("split: %w", err)
	}
	np := split.Pane.PaneID

	if _, err := herdr.Run(append(scope, "pane", "run", np, relaunch)...); err != nil {
		return np, fmt.Errorf("run: %w", err)
	}
	ok := false
	for i := 0; i < 45; i++ {
		var probe struct {
			Agent struct {
				Agent string `json:"agent"`
			} `json:"agent"`
		}
		if err := herdr.RunInto(&probe, append(scope, "agent", "get", np)...); err == nil && probe.Agent.Agent != "" {
			ok = true
			break
		}
		time.Sleep(time.Second)
	}
	if !ok {
		return np, fmt.Errorf("agent not detected in %s within 45s", np)
	}
	// Re-attach the captured name: identity must survive the swap so herdr
	// UX and future diffs match the pane by name again. If the name is
	// taken (e.g. unparking into a session that still holds it), auto-
	// resolve with a numeric suffix rather than failing the restore.
	if pp.Manifest.Name != "" {
		name := pp.Manifest.Name
		for attempt := 0; ; attempt++ {
			candidate := name
			if attempt > 0 {
				// agent names are [a-z][a-z0-9_-]{0,31}: dots are illegal
				candidate = fmt.Sprintf("%s-%d", name, attempt)
			}
			_, err := herdr.Run(append(scope, "agent", "rename", np, candidate)...)
			if err == nil {
				if candidate != name {
					fmt.Printf("  !            %s: name taken, restored as %s\n", name, candidate)
				}
				break
			}
			if attempt >= 5 {
				return np, fmt.Errorf("rename: %w", err)
			}
		}
	}
	return np, nil
}

// Unpark recreates a filtered snapshot (one workspace, its tabs and panes)
// inside a live target session. Each captured tab becomes a real tab; panes
// split off the tab's root anchor, which is closed at the end.
func Unpark(target string, snap *manifest.Snapshot, dryRun bool) error {
	scope := herdr.SessionScope(target)
	for _, w := range snap.Workspaces {
		label := w.Label
		if label == "" {
			label = w.ID
		}
		cwd := w.Cwd
		if cwd == "" && len(w.Tabs) > 0 && len(w.Tabs[0].Panes) > 0 {
			cwd = w.Tabs[0].Panes[0].Cwd
		}
		fmt.Printf("  unpark %s (label=%s cwd=%s, %d tab(s))\n", w.ID, label, cwd, len(w.Tabs))
		if dryRun {
			for _, t := range w.Tabs {
				for _, p := range t.Panes {
					fmt.Printf("    %-12s %s\n", p.Key, strategy.Describe(p.Argv))
				}
			}
			continue
		}
		var wsc struct {
			Workspace struct {
				WorkspaceID string `json:"workspace_id"`
			} `json:"workspace"`
		}
		if err := herdr.RunInto(&wsc, append(scope, "workspace", "create", "--cwd", cwd, "--label", label, "--no-focus")...); err != nil {
			return fmt.Errorf("workspace create: %w", err)
		}
		for _, t := range w.Tabs {
			tabCwd := cwd
			if len(t.Panes) > 0 {
				tabCwd = t.Panes[0].Cwd
			}
			tlabel := t.Label
			if tlabel == "" {
				tlabel = t.ID
			}
			var tc struct {
				RootPane struct {
					PaneID string `json:"pane_id"`
				} `json:"root_pane"`
			}
			if err := herdr.RunInto(&tc, append(scope, "tab", "create", "--workspace", wsc.Workspace.WorkspaceID, "--cwd", tabCwd, "--label", tlabel)...); err != nil {
				return fmt.Errorf("tab create: %w", err)
			}
			anchor := tc.RootPane.PaneID
			for _, p := range t.Panes {
				// Shell panes materialize as the tab root itself; only
				// agents are launched.
				if p.Agent == "" {
					continue
				}
				pp := PanePlan{Manifest: p, Action: Resurrect, Fresh: !strategy.SessionOnDisk(p.Agent, p.SID, p.Env)}
				relaunch := strategy.LaunchCmdline(p.Argv, p.Cmdline, p.Agent, resumeArgsFor(pp))
				fmt.Printf("    %-12s %s\n", p.Key, relaunch)
				np, err := launchPane(target, anchor, pp, relaunch)
				if err != nil {
					return fmt.Errorf("%s: %w", p.Key, err)
				}
				anchor = np
			}
			anyAgent := false
			for _, p := range t.Panes {
				if p.Agent != "" {
					anyAgent = true
				}
			}
			if anyAgent {
				if _, err := herdr.Run(append(scope, "pane", "close", tc.RootPane.PaneID)...); err != nil {
					return fmt.Errorf("close anchor: %w", err)
				}
			}
		}
	}
	return nil
}

// Report re-diffs after apply and prints per-pane verdicts.
func Report(snap *manifest.Snapshot) error {
	live, err := capture.Session(capture.Options{Session: snap.Session})
	if err != nil {
		return err
	}
	plan := Diff(snap, live)
	for _, pp := range plan.Panes {
		if pp.Manifest.Agent == "" {
			continue
		}
		status := "PASS"
		if pp.Action != KeepNative && pp.Action != ShellKeep {
			status = fmt.Sprintf("FAIL(%s)", pp.Action)
		}
		fmt.Printf("  %-12s %s %s\n", status, pp.Manifest.Key, pp.Reason)
	}
	return nil
}
