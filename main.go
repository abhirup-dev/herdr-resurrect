// herdr-resurrect — tmux-resurrect-style capture and env-faithful replay for
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

	"github.com/abhirup-dev/herdr-resurrect/internal/tui"
)

const usage = `herdr-resurrect — capture, archive, and resume herdr sessions env-faithfully

usage:
  herdr-resurrect capture  [--session NAME] [--workspace ID] [--pane ID]  write a snapshot
  herdr-resurrect archive  --session NAME [--force] [--yes]                capture + stop
  herdr-resurrect resume   <session-or-snapshot> [selectors]   attach + verify + sweep
  herdr-resurrect park     --workspace <ID> [--session NAME]   capture + workspace close
  herdr-resurrect unpark   --from <manifest> [--into SESSION]  recreate workspace + relaunch
  herdr-resurrect status   [--all]                             audit live vs manifests
  herdr-resurrect browse                                       snapshot picker TUI
  herdr-resurrect action   <id>                                plugin action entrypoint

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
	case "resume":
		code = cmdResume(os.Args[2:])
	case "park":
		code = cmdPark(os.Args[2:])
	case "unpark":
		code = cmdUnpark(os.Args[2:])
	case "status":
		code = notImplemented("status")
	case "browse":
		options := tui.Options{}
		for _, arg := range os.Args[2:] {
			if arg != "--stop-current" {
				fmt.Fprintf(os.Stderr, "browse: unknown option %q\n", arg)
				code = 2
				break
			}
			options.StopCurrent = true
		}
		if code == 0 {
			if err := tui.Run(options); err != nil {
				fmt.Fprintf(os.Stderr, "browse: %v\n", err)
				code = 1
			}
		}
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
