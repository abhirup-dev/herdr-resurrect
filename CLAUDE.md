# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`herdr-archive` captures a running [herdr](https://herdr.dev) session (workspaces, tabs, panes,
agents) to a JSON snapshot and restores it **additively**. It exists because herdr's native restore
relaunches an agent as bare `claude --resume <id>` with no environment, so every env-based launcher
(glm → z.ai, grok → local proxy) silently comes back as the wrong provider. This tool captures the
pane's live environment verbatim via `ps Ewww` and replays it blindly, assuming nothing about how
that env was produced.

One binary, two entrypoints: a plain CLI and a herdr plugin (`herdr-plugin.toml`). They share one
archive tree and one code path — never let them diverge on defaults.

## Commands

```sh
make build          # -> ./bin/herdr-archive
make check          # go test ./... + go vet ./...
make install        # build + install CLI + link plugin + add `prefix + R` keybinding
make help           # all targets; PREFIX, BINDIR, GO, HERDR, HERDR_CONFIG_PATH are overridable
```

`make check` does **not** run `gofmt`. Before calling a task done, also run:

```sh
gofmt -l .          # must be silent
git diff --check
```

### There are no tests

The repo contains zero `_test.go` files, by the owner's explicit direction: acceptance testing is
done by driving a real herdr session rather than accumulating scaffolding. So `go test ./...`
(and therefore `make check`) proves only that the packages compile. **Never cite it as behavioural
verification.** If you add a test, ask first.

### Verifying against a live session

Never point a mutating verb at a session you care about. Use an isolated scratch session:

```sh
herdr session attach archlab
HERDR_SOCKET_PATH=~/.config/herdr/sessions/archlab/herdr.sock HERDR_ENV=1 go run . status --session archlab
```

Populate it with several *named* agents across ≥2 workspaces including a nested split, then check
that the dry-run plan matches what execution applies, that omitted branches collapse correctly, that
a second pass adds panes without closing existing ones, that a stale plan is rejected before any
mutation, and that only the scratch session was touched.

Note the blind spot: this harness uses named agents only, so the unnamed-shell-pane identity path
(below) is unexercised. Cover it explicitly when touching identity, anchoring, or geometry.

## Architecture

### The pipeline

Understanding any restoration bug means following this chain; the stages live in different packages
on purpose.

```
capture ──► manifest ──► planner (pure) ──► resume (mutating)
                             ▲
                          tui / verbs
```

1. **Selection** (`planner/selection.go`) — `Selection` is `map[paneKey]bool`. `MapMatching` carries
   a selection across snapshots of the same workspace: keys absent from the new target are dropped,
   new panes stay unselected. Tab toggles are tri-state (partial → all, full → clear).
2. **Analysis** (`planner.Analyze`) — marks each captured pane `Restorable` or `Live` by looking it
   up in the live snapshot, first by agent name then by pane id. `Live` panes are excluded from
   selection and non-focusable in the TUI.
3. **Geometry** (`planner/layout.go`) — rebuilds a binary split tree from the captured
   `Layout.Splits` by matching each split's `Rect` against the union of its panes' rects, prunes it
   to the selected subset, and emits ordered `Placement{AnchorKey, Direction, Ratio}` values.
4. **Compile** (`planner.Compile`) — turns placements into `Operation`s carrying both captured
   identity and live destination ids, plus the Before/After topology the confirmation UI renders.
   A pane absent from the geometry order produces **no operation at all** — silent, so watch for it.
5. **Execute** (`resume.ApplyCompiled`) — re-captures the session as a safety snapshot, rejects
   stale plans, then creates workspaces/tabs/splits and launches agents.

### Invariants that constrain new code

- **Additive only.** Restoration may create and launch. It must never close, replace, swap or move
  anything already live. This is the product's core promise.
- **`internal/planner` is pure.** All selection/analysis/geometry/compile decisions live there so the
  CLI and TUI share one implementation. Never call `internal/herdr` from it. `internal/resume` is the
  only package that mutates a live session.
- **`Pane.Key` is identity, `PaneID` is not.** Key = agent name when present, else the pane id. herdr
  never reuses pane ids, but they change between captures — so identity is stable only for *named
  agents*. Unnamed shell panes get a fresh key every capture, which is why they take a different path
  through anchoring and freshness. Workspace and tab keys follow the same label-else-id rule, so a
  relabel reads as a different destination.
- **Snapshots contain live secrets.** `Pane.Env` is the faithful environment (API keys, base URLs),
  written 0600. Don't log it or widen its scope.
- **Mutating verbs are dry-run until `--yes`.** Keep this for anything new that touches a session.
- **Fail closed.** If live topology can't be captured, disable selection and execution rather than
  assuming panes are missing — assuming-missing would duplicate everything.

### Talking to herdr

`internal/herdr` shells out to the herdr binary; **the CLI is the API**, there is no socket client.
`Bin()` prefers `HERDR_BIN_PATH` (injected by the plugin host). Server errors return JSON on stderr
with exit 1; usage errors exit 2 with no JSON — distinguish them, since exit 2 means the installed
herdr lacks that subcommand. Most subcommands already emit JSON and *reject* `--json`. Capture's
`pane layout` enrichment must degrade to `Layout = nil` rather than abort a capture.

### TUI

`internal/tui/tui.go` holds the model and key dispatch; `tree.go` is the shared responsive tree
renderer. Key handling in `key()` is ordered by precedence — text input → list filter → confirmation
→ per-mode block → global keys — and **a per-mode block that handles a key shadows the global
handler**. That has already orphaned an entire view: `viewPlan` is unreachable legacy, retained with
an explicit comment on the const. Check reachability when adding per-mode keys.

Navigation is vim-style: `j/k` move, `l`/`enter` descend, `h` ascends preserving selection, `q` backs
out one level and quits only at the top, `space` toggles, `p` previews.

The TUI must not re-implement planner semantics. Helpers like `selectedPaneCount` are thin
pass-throughs to `planner` — keep them thin or delete them; don't fork the logic.

### Stack note

Direct TUI deps are **`charm.land/*` v2** (`bubbletea/v2`, `bubbles/v2`, `lipgloss/v2`), migrated
from `github.com/charmbracelet/*`. Do not add the v1 module path — it will compile alongside v2 while
silently not sharing types. The `github.com/charmbracelet/x/*` helpers (`ansi`, `term`) are still
correct. In v2, the model renders via `View() tea.View`, not `string`. Use `tools/keyecho` to see what
a keypress actually produces rather than guessing.

Inline brand logos use the kitty graphics protocol; inside a herdr pane they need
`[experimental] kitty_graphics = true` in herdr's config, and fall back to colored glyphs otherwise.
