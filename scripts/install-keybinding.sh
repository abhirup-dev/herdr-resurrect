#!/usr/bin/env bash
set -euo pipefail

herdr_bin="${HERDR_BIN_PATH:-herdr}"
config="${HERDR_CONFIG_PATH:-${XDG_CONFIG_HOME:-$HOME/.config}/herdr/config.toml}"

mkdir -p "$(dirname "$config")"
touch "$config"

if grep -Fq 'command = "herdr-archive.' "$config"; then
  tmp="$(mktemp "${config}.tmp.XXXXXX")"
  sed \
    -e 's/command = "herdr-archive\.open-browser"/command = "herdr-resurrect.open-browser"/' \
    -e 's/command = "herdr-archive\.stop-current"/command = "herdr-resurrect.stop-current"/' \
    -e 's/# herdr-archive:/# herdr-resurrect:/' \
    -e 's/description = "open herdr archive"/description = "open Herdr Resurrect"/' \
    "$config" >"$tmp"
  cat "$tmp" >"$config"
  rm -f "$tmp"
  printf 'Migrated legacy Herdr keybindings in %s\n' "$config"
fi

if grep -Fq 'command = "herdr-resurrect.open-browser"' "$config"; then
  printf 'Herdr Resurrect browser keybinding already installed in %s\n' "$config"
else
  cat >>"$config" <<'EOF'

# herdr-resurrect: session capture and additive restoration planner.
[[keys.command]]
key = "prefix+shift+r"
type = "plugin_action"
command = "herdr-resurrect.open-browser"
description = "open Herdr Resurrect"
EOF
  printf 'Installed prefix+R Herdr Resurrect keybinding in %s\n' "$config"
fi

if grep -Fq 'command = "herdr-resurrect.stop-current"' "$config"; then
  printf 'Herdr Resurrect capture-and-stop keybinding already installed in %s\n' "$config"
else
  cat >>"$config" <<'EOF'

# Capture the invoking session before asking whether to stop it.
[[keys.command]]
key = "prefix+alt+q"
type = "plugin_action"
command = "herdr-resurrect.stop-current"
description = "capture and stop current session"
EOF
  printf 'Installed prefix+Alt-Q Herdr Resurrect keybinding in %s\n' "$config"
fi

"$herdr_bin" config check
