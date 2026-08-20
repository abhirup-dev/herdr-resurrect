#!/usr/bin/env bash
set -euo pipefail

herdr_bin="${HERDR_BIN_PATH:-herdr}"
exec "$herdr_bin" plugin pane open \
  --plugin herdr-resurrect \
  --entrypoint stop-current \
  --focus
