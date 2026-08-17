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

## Verbs

```
herdr-archive capture  [--session N] [--workspace ID]      snapshot → manifest
herdr-archive archive  <session>                           capture + session stop
herdr-archive resume   <session> [selectors] [--yes]       attach + diff + sweep
herdr-archive park     --workspace ID [--session N] [--yes] capture + workspace close
herdr-archive unpark   [selectors] [--into N] [--yes]      recreate + relaunch
selectors: --workspace | --tab | --agent   (partial resume)
```

Everything mutating is a dry-run until `--yes`. `resume` with no `--yes` is
the audit: per-pane verdicts (KEEP-NATIVE / REPLACE / RELAUNCH / RESURRECT)
with the exact env keys that drifted.

## Storage (resurrect conventions)

```
~/.config/herdr/archives/<session>/
  herdr_<timestamp>.json    append-only snapshots, mode 0600 (they contain
                            live env: API tokens, base URLs, model maps)
  last -> herdr_<ts>.json   repoint to travel in time
```

## How resume works

```
archive: capture → herdr session stop (state dir retained on disk)
resume:  boot server (attach w/ nesting-guard env scrubbed)
         → herdr snapshot restore rebuilds tabs/panes/cwd
         → herdr native-resumes agents (claude --resume <sid>, no env)
         → settle-wait → DIFF manifest vs fresh live capture
         → REPLACE broken panes: split --env <all> → run <argv> [--resume]
           → wait detection → rename to captured name → close old pane
         → verify: re-diff, PASS/FAIL per pane
```

The same diff runs against a live session without stop — in-place repair.

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

## Plugin

`herdr plugin link <repo>` — the manifest currently exposes only a `ping`
smoke action; all real verbs are CLI (deliberate: no automatic capture yet).
`HERDR_BIN_PATH` is honored so the same binary works as a plugin command.

## Not yet

- `archive-all` (iterate sessions; the loop is trivial, policy is not)
- `status` verb (resume dry-run covers the audit for now)
- TUI snapshot browser (`plugin pane` popup + bubbletea)
- Linux (`/proc/<pid>/environ` instead of `ps Ewww`)
- Env values with spaces: `ps` tokenizes unquoted; provider vars in
  practice don't contain spaces
