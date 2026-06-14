#!/bin/sh
# Resolve OS fonts (Noto) the way hmi_headless.sh does; Phosphor is bundled.
# Then launch the headless host. Build steps are NOT here — binaries are baked.
#
# LAUNCH auto-opens a demo, but `--launch` is resolved via clickhouse-local
# (a hard dependency), which is NOT in this image. So leave LAUNCH EMPTY to
# start the interactive carousel — the viewer opens any demo (sccmap, widgets,
# leeway, ...) from the launcher in the browser, no clickhouse-local needed.
# Set LAUNCH only in the clickhouse-local image variant (see DEPLOY.md).
set -eu

resolve_font() { fc-match -f '%{file}' "$1" 2>/dev/null || true; }
MAIN_FONT="${MAIN_FONT:-$(resolve_font 'Noto Sans')}"
FALLBACK_FONT="${FALLBACK_FONT:-$(resolve_font 'Noto Sans CJK JP')}"
PHOSPHOR_FONT="${PHOSPHOR_FONT:-/app/assets/fonts/phosphor/Phosphor.ttf}"

echo "imzero2 demo: launch=[${LAUNCH:-<carousel>}] listen=${IMZERO2_HEADLESS_LISTEN:-} encoder=[${IMZERO2_HEADLESS_ENCODER_ARGS:-}] main_font=${MAIN_FONT}" >&2

# Positional params so optional flags can't word-split (set -e safe — no &&).
set -- /app/main_go --logFormat=console --logLevel="${LOG_LEVEL:-info}" imzero2 demo \
  --clientBinary /app/imzero2 \
  --clientInitialMainWindowWidth "${WINDOW_W:-1280}" \
  --clientInitialMainWindowHeight "${WINDOW_H:-800}"
if [ -n "${MAIN_FONT}" ];     then set -- "$@" --mainFontTTF     "$MAIN_FONT";     fi
if [ -n "${PHOSPHOR_FONT}" ]; then set -- "$@" --phosphorFontTTF "$PHOSPHOR_FONT"; fi
if [ -n "${FALLBACK_FONT}" ]; then set -- "$@" --fallbackFontTTF "$FALLBACK_FONT"; fi
if [ -n "${LAUNCH:-}" ];      then set -- "$@" --launch          "$LAUNCH";        fi
exec "$@"
