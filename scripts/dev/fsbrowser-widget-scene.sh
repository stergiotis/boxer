#!/bin/bash
# fsbrowser-widget-scene.sh — assert the file browser widget's interaction, headless.
#
# ADR-0200's verification plan asks for a headless scene over the browser
# widget (ADR-0154): that a row click selects, that Enter on a selected
# directory enters it, that Backspace goes back up, and that the outline mode
# renders. The scene asserts the demo's READOUT lines, not the picture — a
# wrong read-back would still look right — and leaves two captures behind for
# a human to look at.
#
# Built on scripts/dev/tree-widget-scene.sh: same headless host, same driver,
# same gallery; only the trace differs.
#
# Usage:
#   scripts/dev/fsbrowser-widget-scene.sh
#   FSSCENE_DRY=1 scripts/dev/fsbrowser-widget-scene.sh    # resolve anchors only
#   FSSCENE_BUILD=0 scripts/dev/fsbrowser-widget-scene.sh  # reuse rust/imzero2/ binaries
set -uo pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
root=$(cd "$here/../.." && pwd)
OUT="${FSSCENE_OUT:-$root/tmp/fsbrowser-scene}"
SIZE="${FSSCENE_SIZE:-1400x1000}"
TIMEOUT="${FSSCENE_TIMEOUT:-120}"
SETTLE_MS="${FSSCENE_SETTLE_MS:-250}"
BUILD="${FSSCENE_BUILD:-1}"
BIN="${FSSCENE_BIN:-$OUT/bin}"
DRY="${FSSCENE_DRY:-}"
PORT="${FSSCENE_PORT:-8796}"
export IMZERO2_HEADLESS_ENCODER_ARGS="${IMZERO2_HEADLESS_ENCODER_ARGS:--c:v libopenh264 -rc_mode off -bf 0 -g 100000}"
log() { printf '%s\n' "$*" >&2; }
die() { log "fsbrowser-widget-scene: $*"; exit 1; }
mkdir -p "$OUT/logs"
if [[ "$BUILD" == 1 ]]; then
	mkdir -p "$BIN"
	log "building a private host into $BIN (FSSCENE_BUILD=0 to reuse rust/imzero2/)"
	( cd "$root" && CGO_ENABLED=0 go build -tags "$(tr -d '\n' < ./tags),binary_log" \
		-o "$BIN/main_go" ./public/thestack/cmd/imzero2/ ) || die "go build failed"
	[[ -x "$root/rust/imzero2/target/headless/release/imzero2" ]] ||
		die "no headless client — run rust/imzero2/build_rust_headless.sh"
	cp -fp "$root/rust/imzero2/target/headless/release/imzero2" "$BIN/imzero2" ||
		die "cannot copy the headless client"
	for gen in "$root/rust/imzero2/src/imzero2/enums_out.rs" \
	           "$root/rust/imzero2/src/imzero2/interpreter.rs"; do
		if [[ "$gen" -nt "$BIN/imzero2" ]]; then
			die "$(basename "$gen") is newer than the headless client — rebuild it with rust/imzero2/build_rust_headless.sh"
		fi
	done
fi
MAIN_GO="${FSSCENE_MAIN_GO:-$BIN/main_go}"
CLIENT="${FSSCENE_CLIENT:-$BIN/imzero2}"
[[ "$BUILD" == 1 ]] || { MAIN_GO="${FSSCENE_MAIN_GO:-$root/rust/imzero2/main_go}"
                         CLIENT="${FSSCENE_CLIENT:-$root/rust/imzero2/target/headless/release/imzero2}"; }
[[ -x "$MAIN_GO" ]] || die "no Go host at $MAIN_GO"
[[ -x "$CLIENT" ]] || die "no Rust client at $CLIENT"
resolve_font() {
	local line file fam
	command -v fc-match >/dev/null 2>&1 || return 0
	line=$(fc-match -f '%{file}\t%{family}\n' "$1" 2>/dev/null) || return 0
	file="${line%%$'\t'*}"; fam="${line#*$'\t'}"
	[[ "$fam" == *"$2"* && -f "$file" ]] && printf '%s' "$file"
}
MAIN_FONT="${MAIN_FONT:-$(resolve_font 'Noto Sans' 'Noto Sans')}"
MONO_FONT="${MONO_FONT:-$(resolve_font 'DejaVu Sans Mono' 'DejaVu Sans Mono')}"
FALLBACK_FONT="${FALLBACK_FONT:-$(resolve_font 'Noto Sans Mono CJK JP' 'CJK')}"
PHOSPHOR_FONT="${PHOSPHOR_FONT:-$root/rust/imzero2/assets/fonts/phosphor/Phosphor.ttf}"
W=${SIZE%%[xX]*}; H=${SIZE##*[xX]}
trace="$OUT/logs/fsbrowser-scene.jsonl"
cat >"$trace" <<'EOF'
{"do":"note","text":"ADR-0200 M1 — the file browser widget's interaction, asserted through the accessibility tree"}
{"do":"wait","role":"text_input","comment":"the gallery has mounted"}
{"do":"focus","role":"text_input"}
{"do":"type","role":"text_input","text":"file browser","comment":"narrow the gallery to the one demo"}
{"do":"wait","contains":"file browser","settleMs":400}
{"do":"click","contains":"file browser","comment":"expand the demo's section"}
{"do":"wait","name":"clear selection","settleMs":400,"comment":"the demo body is up"}
{"do":"scroll_into_view","name":"clear selection","settleMs":600}
{"do":"note","text":"--- baseline: the root directory, nothing selected ---"}
{"do":"wait","value":"dir: .","role":"label"}
{"do":"wait","value":"selected: (nothing)","role":"label"}
{"do":"note","text":"--- click-to-select: the row, past its label, by pointer ---"}
{"do":"click","value":"internal","role":"label","pointer":true}
{"do":"wait","value":"selected: internal","role":"label","comment":"the click reached the row's sense region behind the label"}
{"do":"capture","text":"fsbrowser-list","comment":"list mode with one directory row selected"}
{"do":"note","text":"--- Enter enters the selected directory; the breadcrumb and the readout follow ---"}
{"do":"key","text":"Enter"}
{"do":"wait","value":"dir: internal","role":"label","settleMs":300}
{"do":"wait","value":"selected: (nothing)","role":"label","comment":"selection is per directory"}
{"do":"note","text":"--- Backspace goes up ---"}
{"do":"click","value":"store","role":"label","pointer":true,"comment":"take focus in the new listing first: focus is what the key capture keys on"}
{"do":"wait","value":"selected: internal/store","role":"label"}
{"do":"key","text":"Backspace"}
{"do":"wait","value":"dir: .","role":"label","settleMs":300}
{"do":"note","text":"--- the outline mode renders the same tree as an outline ---"}
{"do":"click","contains":"outline","role":"button"}
{"do":"wait","value":"last action: mode outline","role":"label","settleMs":400}
{"do":"capture","text":"fsbrowser-outline","comment":"outline mode: unread directories carry a disclosure control"}
EOF
log "launching the widget gallery headless on 127.0.0.1:$PORT"
env -u DISPLAY -u WAYLAND_DISPLAY \
	IMZERO2_HEADLESS_LISTEN="127.0.0.1:$PORT" \
	IMZERO2_HEADLESS_DUMP_DIR="$OUT" \
	IMZERO2_HEADLESS_DUMP_EVERY=1000000 \
	IMZERO2_HEADLESS_FPS=30 \
	IMZERO2_SCREENSHOT_SIZE="$SIZE" \
	BOXER_COMPONENT=fsbrowser-widget-scene \
	"$MAIN_GO" --logFormat=console --logLevel=warn \
		imzero2 demo \
		--clientBinary "$CLIENT" \
		--clientInitialMainWindowWidth "$W" \
		--clientInitialMainWindowHeight "$H" \
		${MAIN_FONT:+--mainFontTTF "$MAIN_FONT"} \
		${MONO_FONT:+--monoFontTTF "$MONO_FONT"} \
		${PHOSPHOR_FONT:+--phosphorFontTTF "$PHOSPHOR_FONT"} \
		${FALLBACK_FONT:+--fallbackFontTTF "$FALLBACK_FONT"} \
		--launch widgets \
	>"$OUT/logs/host.log" 2>&1 &
app_pid=$!
for _ in $(seq 1 $((TIMEOUT * 4))); do
	(exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null && { exec 3<&-; break; }
	kill -0 "$app_pid" 2>/dev/null || break
	sleep 0.25
done
timeout "$TIMEOUT" "$MAIN_GO" --logFormat=console --logLevel=info \
	imzero2 drive --url "ws://127.0.0.1:$PORT/" --trace "$trace" \
	--settle "$SETTLE_MS" ${DRY:+--dryRun} \
	2>&1 | tee "$OUT/logs/drive.log"
rc=${PIPESTATUS[0]}
kids=$(pgrep -P "$app_pid" 2>/dev/null)
kill "$app_pid" $kids 2>/dev/null
wait "$app_pid" 2>/dev/null
for _ in $(seq 1 40); do
	(exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null || break
	exec 3<&-
	sleep 0.25
done
if ((rc == 0)); then
	log "PASS — select, Enter, Backspace and the outline mode all reached the widget"
	log "       captures: $OUT/fsbrowser-list.png, $OUT/fsbrowser-outline.png"
else
	log "FAIL — see $OUT/logs/drive.log and $OUT/logs/host.log"
fi
exit "$rc"
