#!/bin/bash
#set -ev
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
clientDir="$here/target/release/"
VSYNC="${VSYNC:-on}"
# Font selection lives in font-resolve.sh, sourced rather than repeated: the
# launchers here and the ones in consuming repositories (shadow-boxer, sailing,
# which reach this file through their boxer pin) then cannot drift apart. Set
# MAIN_FONT / MONO_FONT / PHOSPHOR_FONT / FALLBACK_FONT beforehand to pin a
# face; hmi-fonts-pragmatapro.sh is exactly that, scoped to a licensed install.
. "$here/font-resolve.sh"
imzero2_resolve_fonts

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

# Rebuild policy: compile only when launched interactively. The edit/run loop
# wants a fresh binary, but a non-interactive launcher (a systemd unit, the
# ansible deploy timer, CI) should run the already-built artifact rather than
# invoke the toolchain on the box. The probe is stdin, so piping the output —
# `./hmi.sh | tee run.log` — still counts as interactive. HMI_BUILD=1/0 forces
# the decision; a missing binary rebuilds regardless, so a launcher never
# starts nothing.
go_bin="$here/main_go"
rust_bin="$here/target/release/imzero2"
if [[ "$HMI_BUILD" == 0 ]]; then
	do_build=0
elif [[ "$HMI_BUILD" == 1 || -t 0 ]]; then
	do_build=1
elif [[ ! -x "$go_bin" || ! -x "$rust_bin" ]]; then
	echo "hmi.sh: pre-built binary missing — rebuilding despite non-interactive launch" >&2
	do_build=1
else
	echo "hmi.sh: non-interactive launch — skipping rebuild (HMI_BUILD=1 to force)" >&2
	do_build=0
fi
# egui_mcp (doc/howto/egui-mcp.md): the `inspection` cargo feature now ships in
# the desktop default build, so there is nothing to toggle or rebuild here — a
# truthy EGUI_INSPECTION is simply exported so the launched client inherits it
# (the Go launcher passes its environment through). eframe then binds the
# inspection port (127.0.0.1:5719 by default) — unauthenticated remote control
# of the app, so keep it to trusted local sessions. Falsy set mirrors eframe's
# own (unset/empty/0/false) and leaves the port closed; anything else (1, true,
# host:port) opens it.
case "${EGUI_INSPECTION,,}" in
	""|0|false) : ;;
	*)
		export EGUI_INSPECTION
		echo "hmi.sh: egui_mcp inspection ON (EGUI_INSPECTION=$EGUI_INSPECTION)" >&2
		;;
esac
if [[ "$do_build" == 1 ]]; then
	./build_rust.sh || exit 1
	./build_go.sh || exit 1
fi
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
# regex_explorer uses `clickhouse local` via subprocess — no server
# needed. Set BOXER_CLICKHOUSE_LOCAL to override the binary path;
# default resolves "clickhouse" through $PATH.

# Root-viewport background: deliberately not set, so the client clears to
# the active theme's panel fill and the uncovered root is indistinguishable
# from the panels drawn over it. Pass
# `--clientBackgroundColorRGBA RRGGBB[AA]` to override; an alpha below ff
# asks the windowing system for a transparent window, which is the only way
# to get a see-through root on purpose. This used to read `8f8f8fff` — a
# skia-client leftover that never reached the egui client, and a light grey
# that would now clash with the dark theme.

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
# ADR-0126 topology mark: the launcher is this run's supervisor, so it injects
# the carrier's component identity (environment is exec-frozen — a bare
# un-launched run shows none). Respect an already-set value.
export BOXER_COMPONENT="${BOXER_COMPONENT:-imzero2-demo}"
"$here/main_go" --logFormat=console \
	--logLevel=info \
       	--pprofHttpListenAddress "localhost:6060" \
       	--flightRecorder --flightRecorderOutputFile="$flightRecord" --flightRecorderFlushOnSignal=SIGTERM,SIGINT \
       	imzero2 demo --clientBinary "$clientDir/imzero2" \
                      --clientType "egui" \
                      --clientVsync $VSYNC \
		      --clientFullscreen off \
		      --clientInitialMainWindowWidth "$WINDOW_W" \
		      --clientInitialMainWindowHeight "$WINDOW_H" \
		      "${IMZERO2_FONT_ARGS[@]}" \
		      "$@"
