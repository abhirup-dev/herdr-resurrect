package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	archiveop "github.com/abhirup-dev/herdr-resurrect/internal/archive"
	"github.com/abhirup-dev/herdr-resurrect/internal/capture"
	"github.com/abhirup-dev/herdr-resurrect/internal/herdr"
	"github.com/abhirup-dev/herdr-resurrect/internal/manifest"
	"github.com/abhirup-dev/herdr-resurrect/internal/planner"
	"github.com/abhirup-dev/herdr-resurrect/internal/resume"
)

type stringsArg []string

func (s *stringsArg) String() string     { return strings.Join(*s, ",") }
func (s *stringsArg) Set(v string) error { *s = append(*s, v); return nil }

func cmdCapture(args []string) int {
	fs := flag.NewFlagSet("capture", flag.ContinueOnError)
	session := fs.String("session", "default", `herdr session name ("default" targets the default session)`)
	name := fs.String("name", "", "durable name for this restoration target")
	out := fs.String("out", "", "archive root (default ~/.config/herdr/archives)")
	var workspaces, panes stringsArg
	fs.Var(&workspaces, "workspace", "capture only this workspace (repeatable)")
	fs.Var(&panes, "pane", "curated capture: hydrate only this live pane id (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	snap, err := capture.Session(capture.Options{Session: *session, WorkspaceIDs: workspaces, PaneIDs: panes})
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture: %v\n", err)
		return 1
	}
	snap.Name = strings.TrimSpace(*name)
	if snap.Name == "" {
		snap.Name = manifest.DefaultName(snap.CreatedAt)
	}
	path, err := snap.Save(*out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture: save: %v\n", err)
		return 1
	}

	label := ""
	if snap.Name != "" {
		label = fmt.Sprintf(" %q", snap.Name)
	}
	fmt.Printf("captured %s%s -> %s (last -> %s)\n", snap.Session, label, path, "last")
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

func pluralWord(n int, singular string) string {
	if n == 1 {
		return singular
	}
	return singular + "s"
}

func compileSnapshotPlan(session, path string, snap, live *manifest.Snapshot) *planner.Plan {
	var targets []planner.Target
	for _, workspace := range snap.Workspaces {
		key := workspace.ID
		if workspace.Label != "" {
			key = workspace.Label
		}
		targets = append(targets, planner.Target{
			WorkspaceKey: key,
			SnapshotName: snap.Name,
			SnapshotPath: path,
			Workspace:    workspace,
			Selected:     planner.DefaultSelection(snap, workspace),
		})
	}
	return planner.Compile(session, targets, live)
}

func printCompiledPlan(plan *planner.Plan) {
	for _, operation := range plan.Operations {
		fmt.Printf("  ADD          %-12s -> %s / %s (%s)\n",
			operation.Pane.Key, operation.WorkspaceKey, operation.TabKey, operation.Placement.Description())
	}
	for _, diagnostic := range plan.Diagnostics {
		fmt.Printf("  UNCHANGED    %s\n", diagnostic)
	}
}

func cmdArchive(args []string) int {
	fs := flag.NewFlagSet("archive", flag.ContinueOnError)
	session := fs.String("session", "", "herdr session to archive (required; 'default' needs --force)")
	name := fs.String("name", "", "durable name for the archive snapshot")
	out := fs.String("out", "", "archive root (default ~/.config/herdr/archives)")
	force := fs.Bool("force", false, "allow archiving the default session")
	yes := fs.Bool("yes", false, "capture and stop the session")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *session == "" {
		fmt.Fprintln(os.Stderr, "archive: --session is required")
		return 2
	}
	if *session == "default" && !*force {
		fmt.Fprintln(os.Stderr, "archive: refusing the default session (it would stop the invoking agent); use --force")
		return 1
	}
	snap, err := capture.Session(capture.Options{Session: *session})
	if err != nil {
		fmt.Fprintf(os.Stderr, "archive: %v\n", err)
		return 1
	}
	_, panes := snap.CapturedPaneCount()
	fmt.Printf("capture and stop session %s (%d %s); state directory retained\n", *session, panes, pluralWord(panes, "pane"))
	for _, workspace := range snap.Workspaces {
		fmt.Printf("  STOP workspace %s\n", or(workspace.Label, workspace.ID))
		for _, tab := range workspace.Tabs {
			fmt.Printf("    STOP tab %s (%d %s)\n", or(tab.Label, tab.ID), len(tab.Panes), pluralWord(len(tab.Panes), "pane"))
		}
	}
	if !*yes {
		fmt.Println("dry run; apply with --yes")
		return 0
	}
	path, err := archiveop.Apply(snap, *name, *out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "archive: %v\n", err)
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
	yes := fs.Bool("yes", false, "apply the additive plan (default: dry-run)")
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
	captured, _ := snap.CapturedPaneCount()
	if len(snap.Workspaces) == 0 || captured == 0 {
		fmt.Fprintln(os.Stderr, "resume: selectors matched nothing")
		return 1
	}
	if err := resume.EnsureServer(*session); err != nil {
		fmt.Fprintf(os.Stderr, "resume: %v\n", err)
		return 1
	}
	resume.Settle(*session, 60*time.Second)
	live, err := capture.Session(capture.Options{Session: *session})
	if err != nil {
		fmt.Fprintf(os.Stderr, "resume: live capture: %v\n", err)
		return 1
	}
	plan := compileSnapshotPlan(*session, path, snap, live)
	fmt.Printf("resume %s from %s\n", *session, path)
	printCompiledPlan(plan)
	if len(plan.Operations) == 0 {
		fmt.Println("nothing missing; existing panes left untouched")
		return 0
	}
	if !*yes {
		fmt.Println("dry run; apply with --yes")
		return 0
	}
	if err := resume.ApplyCompiled(plan); err != nil {
		fmt.Fprintf(os.Stderr, "resume: apply: %v\n", err)
		return 1
	}
	fmt.Println("additive restoration complete")
	return 0
}

func cmdPark(args []string) int {
	fs := flag.NewFlagSet("park", flag.ContinueOnError)
	session := fs.String("session", "default", "herdr session holding the workspace")
	workspace := fs.String("workspace", "", "workspace to park (id or label, required)")
	name := fs.String("name", "", "durable name for the parked snapshot")
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
	snap.Name = strings.TrimSpace(*name)
	if snap.Name == "" {
		snap.Name = manifest.DefaultName(snap.CreatedAt)
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
	yes := fs.Bool("yes", false, "apply the additive plan (default: dry-run)")
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
	captured, _ := snap.CapturedPaneCount()
	if len(snap.Workspaces) == 0 || captured == 0 {
		fmt.Fprintln(os.Stderr, "unpark: selectors matched nothing")
		return 1
	}
	target := or(*into, snap.Session)
	if err := resume.EnsureServer(target); err != nil {
		fmt.Fprintf(os.Stderr, "unpark: %v\n", err)
		return 1
	}
	resume.Settle(target, 60*time.Second)
	live, err := capture.Session(capture.Options{Session: target})
	if err != nil {
		fmt.Fprintf(os.Stderr, "unpark: live capture: %v\n", err)
		return 1
	}
	plan := compileSnapshotPlan(target, path, snap, live)
	fmt.Printf("unpark into %s from %s\n", target, path)
	printCompiledPlan(plan)
	if len(plan.Operations) == 0 {
		fmt.Println("nothing missing; existing panes left untouched")
		return 0
	}
	if !*yes {
		fmt.Println("dry run; apply with --yes")
		return 0
	}
	if err := resume.ApplyCompiled(plan); err != nil {
		fmt.Fprintf(os.Stderr, "unpark: apply: %v\n", err)
		return 1
	}
	fmt.Println("additive restoration complete")
	return 0
}

func short(s string, n int) string {
	if len(s) <= n {
		return or(s, "-")
	}
	return s[:n]
}
