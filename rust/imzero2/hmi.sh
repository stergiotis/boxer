#!/bin/bash
#set -ev
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
clientDir="$here/target/release/"
VSYNC="${VSYNC:-on}"
# Resolve a Noto font file via fontconfig so this works across distros
# instead of hardcoding one layout: Fedora ships them under
# google-noto-vf/, Arch under noto/ & noto-cjk/, Debian under
# truetype/noto/ — each with different directory and file names. fc-match
# is part of fontconfig and present on every mainstream desktop. It
# always returns *some* font though, so each lookup is guarded by the
# matched family name to reject a silent fallback to an unrelated font.
resolve_noto() {
    local family="$1" want="$2" line file fam
    command -v fc-match >/dev/null 2>&1 || return 0
    line=$(fc-match -f '%{file}\t%{family}\n' "$family" 2>/dev/null) || return 0
    file="${line%%$'\t'*}"; fam="${line#*$'\t'}"
    [[ "$fam" == *"$want"* && -f "$file" ]] && printf '%s' "$file"
}

# Proportional UI font: Noto Sans (base latin). Override MAIN_FONT to pin
# a specific .ttf; otherwise detect, falling back to the Fedora path only
# when fontconfig is unavailable.
MAIN_FONT="${MAIN_FONT:-$(resolve_noto 'Noto Sans' 'Noto Sans')}"
MAIN_FONT="${MAIN_FONT:-/usr/share/fonts/google-noto-vf/NotoSans[wght].ttf}"
# MONO_FONT is empty by default; the Rust loader then re-uses MAIN_FONT
# as the FontFamily::Monospace primary (preserves pre-split UX). Set it
# (e.g. via hmi-fonts-pragmatapro.sh) to scope a mono override.
MONO_FONT="${MONO_FONT:-}"
# ADR-0044 iconography: PHOSPHOR_FONT is the single icon font (Phosphor
# regular). Vendored from the `stergiotis/ids-fonts` v0.2.4 release at
# `assets/fonts/phosphor/`. No download fallback needed.
PHOSPHOR_FONT="${PHOSPHOR_FONT:-$here/assets/fonts/phosphor/Phosphor.ttf}"
# CJK fallback: query a language-qualified family ('... CJK JP') because
# the bare 'Noto Sans Mono CJK' is not a fontconfig family and silently
# falls back to plain Noto Sans (no CJK glyphs); the 'CJK' guard rejects
# that. Falls back to the Fedora path when fontconfig is unavailable.
FALLBACK_FONT="${FALLBACK_FONT:-$(resolve_noto 'Noto Sans Mono CJK JP' 'CJK')}"
FALLBACK_FONT="${FALLBACK_FONT:-/usr/share/fonts/google-noto-sans-mono-cjk-vf-fonts/NotoSansMonoCJK-VF.ttc}"

# Best-effort heads-up: a missing MAIN_FONT means the app silently falls
# back to egui's built-in font. The optional CJK FALLBACK_FONT may be
# absent. Detection above keeps these pointing at real files wherever
# Noto is installed; this only fires on a box that lacks it.
if [[ -n "$MAIN_FONT" && ! -f "$MAIN_FONT" ]]; then
    echo "hmi.sh: MAIN_FONT not found: $MAIN_FONT" >&2
    echo "  install Noto Sans (Fedora: google-noto-sans-vf-fonts, Arch: noto-fonts," >&2
    echo "  Debian/Ubuntu: fonts-noto-core) or set MAIN_FONT to an absolute .ttf." >&2
fi

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
