# herdr-archive

tmux-resurrect for [herdr](https://herdr.dev): capture, archive, and
env-faithfully resume herdr sessions, workspaces, and agents. A herdr plugin
(Go, stdlib-only) with a plain-CLI entrypoint — the two share one binary and
one archive tree.

## Why

herdr's native restore resumes an agent pane as exactly `claude --resume
<id>` — no flags, no environment. Every env-based launcher (glm → z.ai,
grok → local proxy, …) comes back as the wrong provider, silently, or as a
dead shell. tmux-resurrect has carried the same gap for years
([#109](https://github.com/tmux-plugins/tmux-resurrect/issues/109)).

herdr-archive captures the pane's **live environment verbatim** (`ps Ewww`)
and replays it blindly. It assumes no knowledge of how that env was
produced — wrappers, launchers, or shells.

## Install

From this checkout:

```sh
make install
```

This builds the binary, installs the CLI to `~/.local/bin/herdr-archive`, links
the checkout as a local Herdr plugin, and adds the `prefix + R` keybinding once.
Use `make help` for separate build, check, CLI, plugin, and keybinding targets.
`PREFIX`, `BINDIR`, `GO`, `HERDR`, and `HERDR_CONFIG_PATH` are overridable.

## Verbs

```
herdr-archive capture  [--session N] [--workspace ID] [--name TEXT]
herdr-archive archive  --session N [--name TEXT] [--force]
herdr-archive resume   --session N [selectors] [--yes]
herdr-archive park     --workspace ID [--session N] [--name TEXT] [--yes]
herdr-archive unpark   [selectors] [--session N] [--into N] [--yes]
selectors: --workspace | --tab | --agent   (partial resume)
```

Everything mutating is a dry-run until `--yes`. `resume` and `unpark` compile
the same additive plan used by the TUI: already-live panes are diagnostic only;
execution adds missing panes and never closes or replaces existing ones.

## Storage (resurrect conventions)

```
~/.config/herdr/archives/<session>/
  herdr_<timestamp>.json    append-only snapshots, mode 0600 (they contain
                            live env: API tokens, base URLs, model maps)
  last -> herdr_<ts>.json   repoint to travel in time
```

## How resume works

```
capture: live topology + pane geometry + cwd + argv + environment → manifest
resume:  boot server if needed → capture current live topology
         → match stable agent identities globally
         → compile missing panes into additive operations
         → show projected BEFORE / AFTER trees
         → create only absent workspaces/tabs/panes
         → replay cwd + generic environment + native resume command
         → re-capture; restored panes become live and non-selectable
```

New tabs replay the filtered captured split tree exactly. Omitted leaves collapse
away while retained ordering, split direction, and ratio are preserved. Adding
to an already-populated tab is explicitly best-effort so existing topology stays
untouched.

## Field learnings (herdr 0.8.0, macOS)

1. **Nesting guard is env-based.** A named session can only be attached via
   `herdr session attach` — which refuses inside a herdr pane — unless you
   scrub `HERDR_*` from the environment of the invoking shell.
2. **`ps Ewww -p <pid>`** on macOS dumps a process's full environment for
   own processes; the foreground pid comes from herdr's
   `pane process-info` (`pid == foreground_process_group_id`).
3. **pi scrubs its own environ** at startup. Empty env is a faithful
   capture, never an error.
4. **Zero-turn conversations are unresumable, period.** claude/codex/pi
   persist session files only after the first user turn; a captured sid for
   an un-messaged agent is dead. The relauncher checks sid-on-disk and drops
   `--resume` (fresh conversation, env-faithful) instead of crashing.
5. **codex reports its session id only after the first turn**, too — capture
   before that stores no sid.
6. **Argv may already contain the resume fragment** (pane captured while
   running `codex resume <sid>`): don't append twice.
7. **Pane ids survive snapshot restore** but not workspace recreation;
   agent names are the stable identity, and the relauncher renames new
   agents back to the captured name.
8. Wrapper model flags replay correctly by blind env: `--model opus` +
   captured `ANTHROPIC_DEFAULT_OPUS_MODEL=glm-5.3[1m]` is the wrapper's own
   trick, replayed without knowing it.

## Browsing (TUI)

```
herdr-archive browse                                  # any terminal
herdr plugin pane open --plugin herdr-archive --entrypoint browser
                                                      # popup inside herdr
```

The full-screen flow is:

```
Sessions
  └─ workspace targets + composite restoration plan
       └─ captured workspace tree + hierarchical subset selection
```

At the workspace level, `space` selects every missing pane and `l` inspects a
target. In the inspector, tab selection cascades to missing descendants while
already-live panes are dim, non-focusable, and skipped by `j/k`. `p` previews
the collapsed selected topology. `R` opens the compiled visual BEFORE / AFTER
diff; `y` executes those exact additive operations and refreshes live state.
`backspace` clears the composite selection at either level.

`c` captures the selected session. Capital `C` captures every running session
from Level 1, or all workspaces in the current session from Level 2. The naming
dialog is optional; submitting it empty uses a human-readable capture date and
time.

### Brand images

The official brand marks (claude.ai, openai.com, grok.com, pi.dev, …) are
baked into the binary (`internal/brands/logos/`, fetched once from each
product's own favicon — no runtime network). On kitty-graphics terminals
(Ghostty, kitty) `browse` transmits them quietly before the first frame,
creates virtual placements, and renders **real images inline in the rows** —
plan table, agent rosters, and layout preview — via kitty's unicode
placeholders (U+10EEEE + row diacritics + image id as a 256-color fg).

The placeholders are ordinary styled text, so bubbletea v2 carries them
through frames and repaint diffs for free (verified by tools/puaprobe and
tools/logosmoke under a pty: rune, diacritic cluster, raw SGR, and
paint-over-paint all pass through the cellbuf; each icon cell measures
exactly width 1, so table alignment holds). Terminals without an image
protocol fall back to colored glyphs — there is no way to show real
images in text-only terminals.

**Inside herdr panes** (herdr 0.8.x): not currently possible. herdr
terminates every pane pty itself, and its experimental
`[experimental] kitty_graphics = true` flag does **not** forward pane
graphics to the client — verified empirically (cursor-placed `a=T`
images and unicode placeholders both: escapes consumed, zero image
pixels on screen, confirmed by an independent vision model reading a
screenshot). So `browse` detects `HERDR_ENV=1` and falls back to the
colored glyphs — placeholders would otherwise render as blank cells.
`HERDR_ARCHIVE_FORCE_IMAGES=1` overrides, for testing against a future
herdr that forwards pane graphics. Outside herdr panes (any terminal
that speaks kitty graphics natively — Ghostty, kitty), images render
for real.

## Plugin

`herdr plugin link <repo>` registers the `browser` popup and the
`herdr-archive.open-browser` global action. Bind `prefix + R` in
`~/.config/herdr/config.toml` with Herdr's shifted-key spelling:

```toml
[[keys.command]]
key = "prefix+shift+r"
type = "plugin_action"
command = "herdr-archive.open-browser"
description = "open herdr archive"
```

The popup inherits `HERDR_SOCKET_PATH`, so the invoking Herdr session is
selected automatically at Level 1. `HERDR_BIN_PATH` is honored by the launcher.

## Not yet

- `archive-all` (iterate sessions; the loop is trivial, policy is not)
- `status` verb (resume dry-run covers the audit for now)
- Sixel/iTerm image paths (kitty only for now; rasterm is already a dep)
- Linux (`/proc/<pid>/environ` instead of `ps Ewww`)
- Env values with spaces: `ps` tokenizes unquoted; provider vars in
  practice don't contain spaces
