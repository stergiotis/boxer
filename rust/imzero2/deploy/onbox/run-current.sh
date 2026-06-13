#!/bin/sh
# ExecStart for imzero2-demo.service (ADR-0085): run the CURRENT release.
# Resolves OS fonts like hmi_headless.sh; no --launch → interactive carousel
# (so no clickhouse-local is needed). The deploy tool swaps `current` and
# restarts this unit.
set -eu
CUR="${IMZERO2_CURRENT:-/opt/imzero2/current}"

resolve_font() { fc-match -f '%{file}' "$1" 2>/dev/null || true; }
MAIN_FONT="${MAIN_FONT:-$(resolve_font 'Noto Sans')}"
FALLBACK_FONT="${FALLBACK_FONT:-$(resolve_font 'Noto Sans CJK JP')}"
PHOSPHOR_FONT="${PHOSPHOR_FONT:-$CUR/assets/fonts/phosphor/Phosphor.ttf}"

set -- "$CUR/main_go" --logFormat=console --logLevel="${LOG_LEVEL:-info}" imzero2 demo \
  --clientBinary "$CUR/imzero2" \
  --clientInitialMainWindowWidth "${WINDOW_W:-1280}" \
  --clientInitialMainWindowHeight "${WINDOW_H:-800}"
if [ -n "$MAIN_FONT" ];     then set -- "$@" --mainFontTTF     "$MAIN_FONT";     fi
if [ -n "$PHOSPHOR_FONT" ]; then set -- "$@" --phosphorFontTTF "$PHOSPHOR_FONT"; fi
if [ -n "$FALLBACK_FONT" ]; then set -- "$@" --fallbackFontTTF "$FALLBACK_FONT"; fi
exec "$@"
