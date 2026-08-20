# Herdr Resurrect

Persistence for [Herdr](https://herdr.dev): capture, archive, and env-faithfully restore sessions, workspaces, tabs, panes, and coding agents.

> **Working on this repository?** Read [`AGENTS.md`](AGENTS.md) for architecture, invariants, safety rules, tests, and live-verification guidance. Remote agents should fetch that file explicitly.

## Why

Herdr's native restore can relaunch an agent without the environment and launcher context that selected its provider, model, credentials, or proxy. A pane started through an env-based wrapper may therefore return as the wrong provider or as a dead shell.

Herdr Resurrect captures the pane's replayable process state and restores it without needing to understand how the launcher produced that state.

## What it guarantees

- **Environment-faithful:** preserves stable launch environment, argv, CWD, and native session identity.
- **Additive:** creates or launches only what is missing. Existing live occupants are never closed, replaced, moved, or overwritten.
- **Live-aware:** distinguishes existing topology from actual live agents. Missing agents can reuse positively identified idle shell panes; foreground commands remain untouched.
- **Fail-closed:** stale plans and unavailable live topology block execution.
- **Safe by default:** mutating commands are dry-runs until `--yes`.
- **One implementation:** the CLI and Herdr plugin share the same archive, planner, defaults, and executor.

## Install

From this checkout:

```sh
make install
```

This builds `herdr-resurrect`, installs it to `~/.local/bin`, links the checkout as a local Herdr plugin, and installs these bindings once:

- `prefix + R` — open the archive and restoration planner
- `prefix + Alt-Q` — capture and stop the current session

Use `make help` for separate build, check, CLI, plugin, and keybinding targets. `PREFIX`, `BINDIR`, `GO`, `HERDR`, and `HERDR_CONFIG_PATH` are overridable.

## CLI

```text
herdr-resurrect capture  [--session N] [--workspace ID] [--pane ID] [--name TEXT]
herdr-resurrect archive  --session N [--name TEXT] [--force] [--yes]
herdr-resurrect resume   --session N [selectors] [--yes]
herdr-resurrect park     --workspace ID [--session N] [--name TEXT] [--yes]
herdr-resurrect unpark   [selectors] [--session N] [--into N] [--yes]
herdr-resurrect status   [--all]
herdr-resurrect browse

selectors: --workspace ID | --tab ID | --agent NAME
```

`resume` and `unpark` compile the same additive plan used by the TUI. `archive` captures the complete replayable live state before stopping a session.

## Storage

```text
~/.config/herdr/archives/<session>/
  herdr_<timestamp>.json    append-only snapshot, mode 0600
  last -> herdr_<timestamp>.json
```

Snapshots contain live environment values such as API keys, base URLs, and model mappings. Keep them private.

Version 2 manifests may include `capture_scope`. Scoped captures retain the full topology and geometry as context, but only selected panes carry launch payloads and may be restored. Version 1 manifests remain readable.

## Restoration flow

```text
capture live topology and process state
        ↓
compare captured occupants with current live state
        ↓
compile additive workspace/tab/pane operations
        ↓
show the projected before/after topology
        ↓
revalidate live destinations
        ↓
create missing topology or launch into safe idle shells
```

Captured split geometry is pruned to the selected subset. Omitted branches collapse while retained pane ordering, direction, and ratio are preserved. Adding to an already-populated tab is best-effort so existing topology remains unchanged.

## TUI

Launch directly or through Herdr:

```sh
herdr-resurrect browse
herdr plugin pane open --plugin herdr-resurrect --entrypoint browser
```

The browser provides:

- session history and current/archived activity summaries;
- workspace-target selection;
- tab/pane subset inspection;
- collapsed topology preview;
- compiled additive before/after review;
- live capture selection;
- capture-and-stop with exact snapshot reuse and forced recapture.

Navigation is vim-style: `j/k`, `l` or Enter, `h`, `q`, Space, and `p`. Footers remain pinned while long views scroll.

## Plugin

The local plugin exposes:

- `herdr-resurrect.open-browser`
- `herdr-resurrect.stop-current`

The popup inherits the invoking session's `HERDR_SOCKET_PATH`. `HERDR_BIN_PATH` is honored by the launcher.

## Brand images

Outside Herdr, terminals supporting kitty graphics can display embedded agent logos. Inside Herdr, the TUI currently falls back to colored glyphs because Herdr does not forward pane graphics. `HERDR_RESURRECT_FORCE_IMAGES=1` is available for testing future forwarding support.

## Development

```sh
make build
make check
gofmt -l .
git diff --check
```

See [`AGENTS.md`](AGENTS.md) before changing planner semantics, restoration behavior, capture identity, or the TUI.
