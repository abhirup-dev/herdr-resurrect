// herdr-archive — tmux-resurrect-style capture and env-faithful replay for
// herdr sessions, workspaces, and agents.
//
// Design invariants:
//   - faithful replay: capture the live environment (ps Ewww) and replay it
//     blindly; no knowledge of any particular launcher scheme (agents.sh).
//   - verify first: every mutating verb prints a plan and its conflicts;
//     execution requires --yes.
//   - manifest keys panes by agent name/role, never by pane id (ids are not
//     reused across restores).
package main

import (
	"fmt"
	"os"
)

const usage = `herdr-archive — capture, archive, and resume herdr sessions env-faithfully

usage:
  herdr-archive capture  [--session NAME] [--workspace ID]   write a snapshot manifest
  herdr-archive archive  <session>                           capture + session stop
  herdr-archive archive-all [--exclude default]              iterate sessions, one at a time
  herdr-archive resume   <session-or-snapshot> [selectors]   attach + verify + sweep
  herdr-archive park     --workspace <ID> [--session NAME]   capture + workspace close
  herdr-archive unpark   --from <manifest> [--into SESSION]  recreate workspace + relaunch
  herdr-archive status   [--all]                             audit live vs manifests
  herdr-archive action   <id>                                plugin action entrypoint

selectors: --workspace ID | --tab ID | --agent NAME   (partial resume)
all mutating verbs are dry-run unless --yes
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var code int
	switch os.Args[1] {
	case "capture":
		code = cmdCapture(os.Args[2:])
	case "archive":
		code = cmdArchive(os.Args[2:])
	case "archive-all":
		code = notImplemented("archive-all")
	case "resume":
		code = cmdResume(os.Args[2:])
	case "park":
		code = notImplemented("park")
	case "unpark":
		code = notImplemented("unpark")
	case "status":
		code = notImplemented("status")
	case "action":
		code = action(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown verb %q\n%s", os.Args[1], usage)
		code = 2
	}
	os.Exit(code)
}

func notImplemented(verb string) int {
	fmt.Fprintf(os.Stderr, "%s: not implemented yet (phase plan: see README)\n", verb)
	return 1
}
