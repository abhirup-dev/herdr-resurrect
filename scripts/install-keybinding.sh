#!/usr/bin/env bash
set -euo pipefail

herdr_bin="${HERDR_BIN_PATH:-herdr}"
config="${HERDR_CONFIG_PATH:-${XDG_CONFIG_HOME:-$HOME/.config}/herdr/config.toml}"

mkdir -p "$(dirname "$config")"
touch "$config"

if grep -Fq 'command = "herdr-archive.open-browser"' "$config"; then
  printf 'Herdr keybinding already installed in %s\n' "$config"
else
  cat >>"$config" <<'EOF'

# herdr-archive: session capture and additive restoration planner.
[[keys.command]]
key = "prefix+shift+r"
type = "plugin_action"
command = "herdr-archive.open-browser"
description = "open herdr archive"
EOF
  printf 'Installed prefix+R Herdr keybinding in %s\n' "$config"
fi

if grep -Fq 'command = "herdr-archive.stop-current"' "$config"; then
  printf 'Herdr capture-and-stop keybinding already installed in %s\n' "$config"
else
  cat >>"$config" <<'EOF'

# Capture the invoking session before asking whether to stop it.
[[keys.command]]
key = "prefix+alt+q"
type = "plugin_action"
command = "herdr-archive.stop-current"
description = "capture and stop current session"
EOF
  printf 'Installed prefix+Alt-Q Herdr keybinding in %s\n' "$config"
fi

"$herdr_bin" config check
