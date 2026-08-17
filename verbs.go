package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/abhirupdas/herdr-archive/internal/capture"
	"github.com/abhirupdas/herdr-archive/internal/herdr"
	"github.com/abhirupdas/herdr-archive/internal/manifest"
	"github.com/abhirupdas/herdr-archive/internal/resume"
)

type stringsArg []string

func (s *stringsArg) String() string     { return strings.Join(*s, ",") }
func (s *stringsArg) Set(v string) error { *s = append(*s, v); return nil }

func cmdCapture(args []string) int {
	fs := flag.NewFlagSet("capture", flag.ContinueOnError)
	session := fs.String("session", "default", `herdr session name ("default" targets the default session)`)
	out := fs.String("out", "", "archive root (default ~/.config/herdr/archives)")
	var workspaces stringsArg
	fs.Var(&workspaces, "workspace", "capture only this workspace (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	snap, err := capture.Session(capture.Options{Session: *session, WorkspaceIDs: workspaces})
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture: %v\n", err)
		return 1
	}
	path, err := snap.Save(*out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture: save: %v\n", err)
		return 1
	}

	fmt.Printf("captured %s -> %s (last -> %s)\n", snap.Session, path, "last")
	for _, w := range snap.Workspaces {
		fmt.Printf("  %s %-20s %d tab(s)\n", w.ID, w.Label, len(w.Tabs))
		for _, t := range w.Tabs {
			for _, p := range t.Panes {
				sid := "-"
				if p.SID != "" {
					sid = p.SID[:8]
				}
				fmt.Printf("    %-12s %-8s %-6s sid=%-9s env=%-4d %s\n",
					p.Key, or(p.Agent, "shell"), or(p.State, "-"), sid,
					len(p.Env), p.Cwd)
			}
		}
	}
	return 0
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func cmdArchive(args []string) int {
	fs := flag.NewFlagSet("archive", flag.ContinueOnError)
	session := fs.String("session", "", "herdr session to archive (required; 'default' needs --force)")
	out := fs.String("out", "", "archive root (default ~/.config/herdr/archives)")
	force := fs.Bool("force", false, "allow archiving the default session")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *session == "" {
		fmt.Fprintln(os.Stderr, "archive: --session is required")
		return 2
	}
	if *session == "default" && !*force {
		fmt.Fprintln(os.Stderr, "archive: refusing the default session (it would kill the invoking agent); use --force")
		return 1
	}

	snap, err := capture.Session(capture.Options{Session: *session})
	if err != nil {
		fmt.Fprintf(os.Stderr, "archive: %v\n", err)
		return 1
	}
	path, err := snap.Save(*out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "archive: save: %v\n", err)
		return 1
	}
	if _, err := herdr.Run("session", "stop", *session); err != nil {
		fmt.Fprintf(os.Stderr, "archive: stop: %v (snapshot kept at %s)\n", err, path)
		return 1
	}
	fmt.Printf("archived %s -> %s (server stopped; state dir retained)\n", *session, path)
	return 0
}

func cmdResume(args []string) int {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	session := fs.String("session", "default", `herdr session to resume`)
	from := fs.String("from", "", "explicit snapshot path (default: latest for --session)")
	out := fs.String("out", "", "archive root (default ~/.config/herdr/archives)")
	yes := fs.Bool("yes", false, "apply the plan (default: dry-run)")
	wsSel := fs.String("workspace", "", "partial: only this workspace (id or label)")
	tabSel := fs.String("tab", "", "partial: only this tab (id or label)")
	var agentSel stringsArg
	fs.Var(&agentSel, "agent", "partial: only this agent (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	path := *from
	if path == "" {
		var err error
		path, err = manifest.Latest(*out, *session)
		if err != nil {
			fmt.Fprintf(os.Stderr, "resume: %v\n", err)
			return 1
		}
	}
	full, err := manifest.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resume: %v\n", err)
		return 1
	}
	snap := full.Filter(*wsSel, *tabSel, agentSel)
	if len(snap.Workspaces) == 0 {
		fmt.Fprintln(os.Stderr, "resume: selectors matched nothing")
		return 1
	}

	if err := resume.EnsureServer(snap.Session); err != nil {
		fmt.Fprintf(os.Stderr, "resume: %v\n", err)
		return 1
	}
	resume.Settle(snap.Session, 60*time.Second)
	live, err := capture.Session(capture.Options{Session: snap.Session})
	if err != nil {
		fmt.Fprintf(os.Stderr, "resume: live capture: %v\n", err)
		return 1
	}
	plan := resume.Diff(snap, live)

	fmt.Printf("resume %s from %s\n", snap.Session, path)
	for _, pp := range plan.Panes {
		fmt.Printf("  %-12s %-12s %s\n", pp.Action, pp.Manifest.Key, pp.Reason)
	}
	for _, m := range plan.Missing() {
		fmt.Printf("  CONFLICT     %s\n", m)
	}
	if !*yes {
		fmt.Println("dry run; apply with --yes")
		return 0
	}
	if err := resume.Apply(plan, false); err != nil {
		fmt.Fprintf(os.Stderr, "resume: apply: %v\n", err)
		return 1
	}
	fmt.Println("verify:")
	if err := resume.Report(snap); err != nil {
		fmt.Fprintf(os.Stderr, "resume: verify: %v\n", err)
		return 1
	}
	return 0
}

func cmdPark(args []string) int {
	fs := flag.NewFlagSet("park", flag.ContinueOnError)
	session := fs.String("session", "default", "herdr session holding the workspace")
	workspace := fs.String("workspace", "", "workspace to park (id or label, required)")
	out := fs.String("out", "", "archive root (default ~/.config/herdr/archives)")
	yes := fs.Bool("yes", false, "actually close the workspace (default: dry-run)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *workspace == "" {
		fmt.Fprintln(os.Stderr, "park: --workspace is required")
		return 2
	}
	snap, err := capture.Session(capture.Options{Session: *session, WorkspaceIDs: []string{*workspace}})
	if err != nil {
		fmt.Fprintf(os.Stderr, "park: %v\n", err)
		return 1
	}
	// Label-based selection: resolve to the captured id.
	if len(snap.Workspaces) == 0 {
		fmt.Fprintf(os.Stderr, "park: workspace %q not found\n", *workspace)
		return 1
	}
	wid := snap.Workspaces[0].ID
	for _, t := range snap.Workspaces[0].Tabs {
		for _, p := range t.Panes {
			fmt.Printf("  park %-12s %-8s sid=%-9s env=%d\n", p.Key, or(p.Agent, "shell"), short(p.SID, 8), len(p.Env))
		}
	}
	if !*yes {
		fmt.Println("dry run; apply with --yes (workspace stays open)")
		return 0
	}
	if _, err := snap.Save(*out); err != nil {
		fmt.Fprintf(os.Stderr, "park: save: %v\n", err)
		return 1
	}
	if _, err := herdr.Run(append(herdr.SessionScope(*session), "workspace", "close", wid)...); err != nil {
		fmt.Fprintf(os.Stderr, "park: close: %v\n", err)
		return 1
	}
	fmt.Printf("parked %s (snapshot kept under archives/%s)\n", wid, snap.Session)
	return 0
}

func cmdUnpark(args []string) int {
	fs := flag.NewFlagSet("unpark", flag.ContinueOnError)
	session := fs.String("session", "default", "source session of the snapshot")
	into := fs.String("into", "", "target session to recreate in (default: same as --session)")
	from := fs.String("from", "", "explicit snapshot path (default: latest for --session)")
	out := fs.String("out", "", "archive root (default ~/.config/herdr/archives)")
	yes := fs.Bool("yes", false, "actually recreate (default: dry-run)")
	wsSel := fs.String("workspace", "", "only this workspace (id or label)")
	tabSel := fs.String("tab", "", "only this tab (id or label)")
	var agentSel stringsArg
	fs.Var(&agentSel, "agent", "only this agent (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	path := *from
	if path == "" {
		var err error
		path, err = manifest.Latest(*out, *session)
		if err != nil {
			fmt.Fprintf(os.Stderr, "unpark: %v\n", err)
			return 1
		}
	}
	full, err := manifest.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unpark: %v\n", err)
		return 1
	}
	snap := full.Filter(*wsSel, *tabSel, agentSel)
	if len(snap.Workspaces) == 0 {
		fmt.Fprintln(os.Stderr, "unpark: selectors matched nothing")
		return 1
	}
	target := or(*into, snap.Session)
	if err := resume.EnsureServer(target); err != nil {
		fmt.Fprintf(os.Stderr, "unpark: %v\n", err)
		return 1
	}
	if err := resume.Unpark(target, snap, !*yes); err != nil {
		fmt.Fprintf(os.Stderr, "unpark: %v\n", err)
		return 1
	}
	if !*yes {
		fmt.Println("dry run; apply with --yes")
	}
	return 0
}

func short(s string, n int) string {
	if len(s) <= n {
		return or(s, "-")
	}
	return s[:n]
}
