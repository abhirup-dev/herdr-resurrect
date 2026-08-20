# AGENTS.md

## Project

`herdr-resurrect` captures a running [herdr](https://herdr.dev) session and restores it additively while preserving each agent pane's launch environment. The CLI and Herdr plugin share one binary, archive tree, defaults, planner, and execution path.

Snapshots contain live environment values, including credentials. Never print `Pane.Env`, loosen snapshot permissions, or copy secrets into tests or logs.

## Commands

```sh
make build          # ./bin/herdr-resurrect
make check          # go test ./... + go vet ./...
make install        # build, install CLI, link plugin, install keybindings
make help

gofmt -l .          # must be silent before completion
git diff --check
```

`make check` does not run `gofmt`. Focused tests exist in planner, resume, capture, and strategy; green tests do not replace isolated live verification for mutating or interactive behavior.

## Architecture

```text
capture ──► manifest ──► planner (pure) ──► resume (mutating)
                             ▲
                          tui / verbs
```

- `internal/capture`: queries Herdr and the process table; captures topology, geometry, argv, CWD, session identity, and environment.
- `internal/manifest`: versioned snapshots under `~/.config/herdr/archives`.
- `internal/planner`: all selection, live/restorable classification, occupancy, geometry, and compile decisions. It must not call `internal/herdr`.
- `internal/resume`: the only package that mutates a live session.
- `internal/tui`: presentation and input routing only; keep planner semantics out of it.

Planner flow:

1. Selection is `map[paneKey]bool`. Partial tab/workspace toggles select all; full toggles clear.
2. `Analyze` classifies captured occupants as `Restorable` or `Live`.
3. Layout reconstruction builds a binary split tree, prunes omitted leaves, and emits ordered placements. Malformed geometry must fall back without dropping panes.
4. `Compile` emits additive operations with captured identity and live destination IDs.
5. `ApplyCompiled` re-captures live state, rejects stale destinations, then creates or launches only what is absent.

## Non-negotiable invariants

- **Additive only:** never close, replace, move, swap, or overwrite a live occupant.
- Existing idle shell panes may be reused, but foreground commands may not. `planner.ReusableShellPane` is the shared safety predicate.
- **Fail closed:** if live topology cannot be captured or a destination changed, disable or reject execution rather than assuming it is missing.
- Mutating verbs remain dry-run until `--yes`.
- Scoped snapshots retain full topology as context, but only captured panes carry launch payloads and may restore.
- A pane missing from the compiled geometry order receives no operation. Tests must guard against silent pane drops.

## Identity

`Pane.Key` is the planner identity: agent name when available, otherwise pane ID. Pane IDs are stable within a live session but change when topology is recreated. Named agents are therefore the stable cross-capture path; unnamed panes require conservative pane-ID and occupancy checks.

Workspace and tab keys use label-else-ID. Relabeling creates a different logical destination.

Do not treat shared topology as a live occupant. A captured agent whose old pane is now an idle shell is missing and may launch into that shell. A pane running a non-agent foreground command is occupied and must remain untouched.

## Environment replay

Capture reads the foreground process environment via `ps Ewww` and replay filters in `internal/strategy`.

- Empty captured environment is faithful by definition; some agents scrub their own environ.
- Replay excludes transient Herdr, multiplexer, terminal, process, and stale hook values.
- Any changed replayable value is drift, not only provider-prefixed values.
- Never replay cmux's temporary `NODE_OPTIONS` preload.
- Preserve launcher argv and avoid appending duplicate resume fragments.
- A zero-turn agent may have no durable session file; relaunch fresh rather than passing a dead resume ID.

## Talking to Herdr

`internal/herdr` shells out to the installed `herdr` CLI; there is no socket client. `Bin()` prefers `HERDR_BIN_PATH`, which the plugin host injects.

Most commands already emit JSON and reject `--json`. Server errors return JSON on stderr with exit 1; usage or unsupported-subcommand errors exit 2 without JSON. Capture's optional `pane layout` enrichment must degrade to `Layout = nil` rather than fail the whole capture.

## Live verification

Never mutate a session you care about. Use the `archlab` scratch session:

```sh
herdr session attach archlab
HERDR_SOCKET_PATH=~/.config/herdr/sessions/archlab/herdr.sock HERDR_ENV=1 go run . status --session archlab
```

When touching restoration, verify:

- dry-run matches execution;
- subset branches collapse without losing selected panes;
- a second pass adds without closing existing panes;
- stale plans reject before mutation;
- idle shells can host missing agents, while foreground commands are preserved;
- only `archlab` changed.

Use named agents across at least two workspaces and a nested split. Explicitly cover unnamed shell identity when changing matching, anchoring, occupancy, or geometry.

## TUI

Direct dependencies are Charm v2 modules: `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`, and `charm.land/lipgloss/v2`. Do not add Charm v1 module paths. `github.com/charmbracelet/x/ansi` and `x/term` remain valid. Models render with `View() tea.View`.

Key precedence is:

```text
ctrl+c → naming input → active list filter → confirmation → mode handler → global handler → list fallback
```

A handled mode key shadows global behavior. Preserve intentional case distinctions such as `r`, `R`, and `C`. `viewPlan` is unreachable legacy and remains explicitly marked.

Navigation is vim-style: `j/k`, `l` or Enter, `h`, `q`, Space, and `p`. Long custom views use the shared Bubbles viewport and a pinned footer. Use `tools/keyecho` to inspect actual key messages instead of guessing.

Inline logos use kitty graphics outside Herdr. Inside Herdr, fall back to glyphs unless a future host forwards graphics; `HERDR_RESURRECT_FORCE_IMAGES=1` is the test override.
