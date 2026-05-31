#!/bin/bash
#set -ev
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
clientDir="$here/target/release/"
VSYNC="${VSYNC:-on}"
MAIN_FONT="${MAIN_FONT:-/usr/share/fonts/google-noto-vf/NotoSans[wght].ttf}"
# MONO_FONT is empty by default; the Rust loader then re-uses MAIN_FONT
# as the FontFamily::Monospace primary (preserves pre-split UX). Set it
# (e.g. via hmi-fonts-pragmatapro.sh) to scope a mono override.
MONO_FONT="${MONO_FONT:-}"
# ADR-0044 iconography: PHOSPHOR_FONT is the single icon font (Phosphor
# regular). Vendored from the `stergiotis/ids-fonts` v0.2.4 release at
# `assets/fonts/phosphor/`. No download fallback needed.
PHOSPHOR_FONT="${PHOSPHOR_FONT:-$here/assets/fonts/phosphor/Phosphor.ttf}"
FALLBACK_FONT="${FALLBACK_FONT:-/usr/share/fonts/google-noto-sans-mono-cjk-vf-fonts/NotoSansMonoCJK-VF.ttc}"

# IMZERO2_SCREENSHOT_SIZE=WxH widens the eframe viewport to fit the
# requested tour capture rect (ADR-0008 SD5). The Go-side parser is
# authoritative; this regex must accept the same shape so the viewport
# and the Go-side stage rect stay in sync.
WINDOW_W=1800
WINDOW_H=1024
if [[ -n "$IMZERO2_SCREENSHOT_SIZE" ]]; then
    if [[ "$IMZERO2_SCREENSHOT_SIZE" =~ ^([0-9]+)[xX]([0-9]+)$ ]]; then
        req_w="${BASH_REMATCH[1]}"
        req_h="${BASH_REMATCH[2]}"
        (( req_w > WINDOW_W )) && WINDOW_W="$req_w"
        (( req_h > WINDOW_H )) && WINDOW_H="$req_h"
    else
        echo "hmi.sh: ignoring malformed IMZERO2_SCREENSHOT_SIZE='$IMZERO2_SCREENSHOT_SIZE' (expected WxH)" >&2
    fi
fi

./build_rust.sh || exit 1
./build_go.sh || exit 1
export BOXER_LOG_OS_PID_ON_START="true"
export BOXER_LOG_OS_HOST_ON_START="true"
#export BOXER_LOG_OS_ARGS_ON_START="true"
export BOXER_LOG_VCS_REVISION_ON_START="true"
export BOXER_LOG_MODULE_INFO_IN_START="true"
#export BOXER_LOG_CORRELATION_ID=""
export GOCOVERDIR="/tmp/spinnakercover"
rm -rf "$GOCOVERDIR"
mkdir -p "$GOCOVERDIR/legacy"
flightRecord="$here/flightRecorder.trace"
rm -f "$flightRecord"
	#--waitForDebugger \
#export BOXER_IMZERO_DEBUG_MODE="flamegraph"
export HN_EXPLORER_CLICKHOUSE_URL="http://default:hack@localhost:8123/"
# regex_explorer uses `clickhouse local` via subprocess — no server
# needed. Set REGEX_EXPLORER_CLICKHOUSE_LOCAL_BIN to override the
# binary path; default resolves "clickhouse-local" through $PATH.

# Launch from the Go module root rather than rust/imzero2 so apps that
# shell out to the toolchain at runtime (e.g. godepview's go/packages
# collection) see the module. The build steps above ran relative to
# $here; the launch below uses absolute binary paths and cd's to the
# project root.
projectRoot="$here"
while [ "$projectRoot" != "/" ] && [ ! -f "$projectRoot/go.mod" ]; do
	projectRoot=$(dirname "$projectRoot")
done
# godepview reads these via config/env so it collects the project's graph
# under the repo's build tags regardless of how it is launched.
export GODEPVIEW_ROOT="$projectRoot"
if [ -f "$projectRoot/tags" ]; then
	export GODEPVIEW_TAGS="$(tr -d '\n' < "$projectRoot/tags")"
fi
cd "$projectRoot"
"$here/main_go" --logFormat=console \
	--logLevel=info \
       	--pprofHttpListenAddress "localhost:6060" \
       	--flightRecorder --flightRecorderOutputFile="$flightRecord" --flightRecorderFlushOnSignal=SIGTERM,SIGINT \
       	imzero2 demo --clientBinary "$clientDir/imzero2" \
                      --clientType "egui" \
                      --clientBackgroundColorRGBA 8f8f8fff \
                      --clientVsync $VSYNC \
		      --clientFullscreen off \
		      --clientInitialMainWindowWidth "$WINDOW_W" \
		      --clientInitialMainWindowHeight "$WINDOW_H" \
		      ${MAIN_FONT:+--mainFontTTF "$MAIN_FONT"} \
		      ${MONO_FONT:+--monoFontTTF "$MONO_FONT"} \
		      ${PHOSPHOR_FONT:+--phosphorFontTTF "$PHOSPHOR_FONT"} \
		      ${FALLBACK_FONT:+--fallbackFontTTF "$FALLBACK_FONT"} \
		      "$@"
