#!/bin/bash
# waveform-scene.sh — assert the waveform player's pointer and transport paths, headless.
#
# ADR-0208's verification plan asks for a headless scene that seeks by click,
# pans by drag and plays on the null sink, asserting the readout labels. The
# interesting failures are the silent ones every painter widget is exposed to:
# a sense region that does not win the hit test, a drag summed from deltas
# that lands short, a click whose id never reads back. A capture would look
# right through all of them; the readouts do not.
#
# The canvas is painter-only and has no accessibility node, so the scene runs
# in two phases: it expands the demo and dumps the tree, locates the button
# row above the canvas and the readout line below it, and aims the pointer at
# the midpoint between them — inside the 220 px canvas whatever the fonts do.
#
# Usage:
#   scripts/dev/waveform-scene.sh
#   WFSCENE_BUILD=0 scripts/dev/waveform-scene.sh   # reuse rust/imzero2/ binaries
#
# Exit status is the assertion: a `wait` that never resolves fails the run.
set -uo pipefail

here=$(dirname "$(readlink -f "$BASH_SOURCE")")
root=$(cd "$here/../.." && pwd)

OUT="${WFSCENE_OUT:-$root/tmp/waveform-scene}"
SIZE="${WFSCENE_SIZE:-1400x1000}"
TIMEOUT="${WFSCENE_TIMEOUT:-90}"
SETTLE_MS="${WFSCENE_SETTLE_MS:-250}"
BUILD="${WFSCENE_BUILD:-1}"
BIN="${WFSCENE_BIN:-$OUT/bin}"
PORT="${WFSCENE_PORT:-8796}"
export IMZERO2_HEADLESS_ENCODER_ARGS="${IMZERO2_HEADLESS_ENCODER_ARGS:--c:v libopenh264 -rc_mode off -bf 0 -g 100000}"

log() { printf '%s\n' "$*" >&2; }
die() { log "waveform-scene: $*"; exit 1; }

mkdir -p "$OUT/logs"

# --- binaries (a private pair, for the reason the tree scene builds one) ------
if [[ "$BUILD" == 1 ]]; then
	mkdir -p "$BIN"
	log "building a private host into $BIN (WFSCENE_BUILD=0 to reuse rust/imzero2/)"
	( cd "$root" && CGO_ENABLED=0 go build -tags "$(tr -d '\n' < ./tags),binary_log" \
		-o "$BIN/main_go" ./public/thestack/cmd/imzero2/ ) || die "go build failed"
	# The capture step needs a raster-capable client: the CPU rasterizer
	# (ADR-0205, build_rust_headless_soft.sh) or the wgpu build. A mesh-only
	# appliance build passes every assertion and then fails the capture.
	src_client=""
	for cand in "$root/rust/imzero2/target/headless-soft/release/imzero2" \
	            "$root/rust/imzero2/target/headless/release/imzero2"; do
		[[ -x "$cand" ]] && { src_client=$cand; break; }
	done
	[[ -n "$src_client" ]] ||
		die "no headless client — run rust/imzero2/build_rust_headless_soft.sh"
	cp -fp "$src_client" "$BIN/imzero2" || die "cannot copy the headless client"
	for gen in "$root/rust/imzero2/src/imzero2/enums_out.rs" \
	           "$root/rust/imzero2/src/imzero2/interpreter.rs"; do
		if [[ "$gen" -nt "$BIN/imzero2" ]]; then
			die "$(basename "$gen") is newer than the headless client — rebuild it with rust/imzero2/build_rust_headless.sh"
		fi
	done
fi
MAIN_GO="${WFSCENE_MAIN_GO:-$BIN/main_go}"
CLIENT="${WFSCENE_CLIENT:-$BIN/imzero2}"
[[ "$BUILD" == 1 ]] || { MAIN_GO="${WFSCENE_MAIN_GO:-$root/rust/imzero2/main_go}"
                         CLIENT="${WFSCENE_CLIENT:-$root/rust/imzero2/target/headless-soft/release/imzero2}"; }
[[ -x "$MAIN_GO" ]] || die "no Go host at $MAIN_GO"
[[ -x "$CLIENT" ]] || die "no Rust client at $CLIENT"

# --- fonts (mirrors rust/imzero2/hmi.sh) -------------------------------------
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

drive() { # drive <trace> [extra args]
	local trace=$1; shift
	timeout "$TIMEOUT" "$MAIN_GO" --logFormat=console --logLevel=info \
		imzero2 drive --url "ws://127.0.0.1:$PORT/" ${trace:+--trace "$trace"} --settle "$SETTLE_MS" "$@"
}

# --- phase A: reach the demo -------------------------------------------------
traceA="$OUT/logs/a-expand.jsonl"
cat >"$traceA" <<'EOF'
{"do":"note","text":"ADR-0208 M2 — the waveform player, asserted through its readout labels"}
{"do":"wait","role":"text_input","comment":"the gallery has mounted"}
{"do":"focus","role":"text_input"}
{"do":"type","role":"text_input","text":"waveform","comment":"narrow the gallery to the demo"}
{"do":"wait","contains":"waveform player","settleMs":400}
{"do":"click","contains":"waveform player","comment":"expand the demo's section"}
{"do":"wait","name":"10 ms/px","settleMs":600,"comment":"the demo body is up"}
{"do":"scroll_into_view","name":"10 ms/px","settleMs":600}
{"do":"wait","value":"position: 0:00.000 · paused","role":"label"}
EOF

# --- a real recording, when ffmpeg is here -----------------------------------
# Phase C opens a FLAC through the demo's file field: the sniffing decoder,
# ffmpeg via extbin, the background build and the widget, end to end. The
# peaks cache is a per-run directory so the readout says "built", not "from
# cache". Skipped, not failed, without ffmpeg.
FIXTURE=""
if command -v ffmpeg >/dev/null 2>&1; then
	FIXTURE="$OUT/tone.flac"
	ffmpeg -y -v error -f lavfi -i "sine=frequency=440:duration=30" -ac 2 -ar 48000 -c:a flac "$FIXTURE" ||
		{ log "ffmpeg could not write the fixture; skipping phase C"; FIXTURE=""; }
fi
rm -rf "$OUT/peaks-cache"

# --- run ---------------------------------------------------------------------
log "launching the widget gallery headless on 127.0.0.1:$PORT"
env -u DISPLAY -u WAYLAND_DISPLAY \
	BOXER_AUDIO_PEAKS_CACHE_DIR="$OUT/peaks-cache" \
	IMZERO2_HEADLESS_LISTEN="127.0.0.1:$PORT" \
	IMZERO2_HEADLESS_DUMP_DIR="$OUT" \
	IMZERO2_HEADLESS_DUMP_EVERY=1000000 \
	IMZERO2_HEADLESS_FPS=30 \
	IMZERO2_SCREENSHOT_SIZE="$SIZE" \
	BOXER_COMPONENT=waveform-scene \
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
cleanup() {
	local kids
	kids=$(pgrep -P "$app_pid" 2>/dev/null)
	kill "$app_pid" $kids 2>/dev/null
	wait "$app_pid" 2>/dev/null
	for _ in $(seq 1 40); do
		(exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null || break
		exec 3<&-
		sleep 0.25
	done
}
trap cleanup EXIT

for _ in $(seq 1 $((TIMEOUT * 4))); do
	(exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null && { exec 3<&-; break; }
	kill -0 "$app_pid" 2>/dev/null || break
	sleep 0.25
done

drive "$traceA" 2>&1 | tee "$OUT/logs/drive-a.log"
rc=${PIPESTATUS[0]}
((rc == 0)) || { log "FAIL — could not reach the demo; see $OUT/logs/drive-a.log"; exit "$rc"; }

# --- locate the canvas from the tree -----------------------------------------
drive "" --dumpTree >"$OUT/logs/tree.txt" 2>&1 || die "tree dump failed"
node_xy() { # node_xy <grep pattern> → "cx cy"
	grep -F "$1" "$OUT/logs/tree.txt" | head -1 |
		sed -n 's/.* cx=\([0-9.]*\) cy=\([0-9.]*\) .*/\1 \2/p'
}
read -r bx by < <(node_xy 'name="10 ms/px"')
read -r px py < <(node_xy 'value="position: 0:00.000 · paused"')
[[ -n "${by:-}" && -n "${py:-}" ]] || die "could not locate the button row and the readout in the tree dump"
# The canvas is the 220 px strip between the button row and the first readout
# line; its midpoint is inside it regardless of the row heights around it.
CX=$(python3 -c "print(round(float('$bx')))")
CY=$(python3 -c "print(round((float('$by')+float('$py'))/2))")
log "canvas point: ($CX, $CY) — button row at y=$by, readout at y=$py"

# --- phase B: the assertions ---------------------------------------------------
# At 10 ms/px a 300 px drag is exactly 3.000 s; the pan is press-origin plus
# offset, so anything else means the drag path lost or doubled pointer moves.
traceB="$OUT/logs/b-assert.jsonl"
cat >"$traceB" <<EOF
{"do":"note","text":"--- a fixed zoom, so pixel offsets are known durations ---"}
{"do":"click","name":"10 ms/px"}
{"do":"wait","valueContains":"· 10.000 ms/px","role":"label"}

{"do":"note","text":"--- hover reaches the canvas: the per-canvas cursor row, one frame behind ---"}
{"do":"hover","x":$CX,"y":$CY,"settleMs":300}
{"do":"wait","valueContains":"hover: 0:0","role":"label","comment":"a time under the pointer, read back through R24"}

{"do":"note","text":"--- click seeks: the sense region wins the hit test and its click reads back ---"}
{"do":"click","x":$CX,"y":$CY,"settleMs":300}
{"do":"wait","valueContains":"clicked: 0:0","role":"label"}

{"do":"note","text":"--- drag pans by exactly the pointer offset ---"}
{"do":"click","name":" start","comment":"back to frame 0 so the view starts at 0:00.000"}
{"do":"wait","value":"position: 0:00.000 · paused","role":"label"}
{"do":"drag","x":$CX,"y":$CY,"toX":$((CX-300)),"toY":$CY,"steps":16,"durationMs":320,"settleMs":400}
{"do":"wait","valueContains":"view: 0:03.000 –","role":"label","comment":"300 px at 10 ms/px, from a view that started at 0"}

EOF

drive "$traceB" 2>&1 | tee "$OUT/logs/drive-b.log"
rc=${PIPESTATUS[0]}
((rc == 0)) || { log "FAIL — see $OUT/logs/drive-b.log and $OUT/logs/host.log"; exit "$rc"; }

# --- measure the time under the pointer, to land a press inside a region -----
# The demo's regions are the tone bursts: 0.35 s every 0.9 s. At 10 ms/px the
# hover readout under (CX, CY) says which time the pointer is over; the drag
# for the edit starts where the middle of the next burst is.
printf '%s\n' "{\"do\":\"hover\",\"x\":$CX,\"y\":$CY,\"settleMs\":300}" >"$OUT/logs/b-hover.jsonl"
drive "$OUT/logs/b-hover.jsonl" >"$OUT/logs/drive-b-hover.log" 2>&1 || die "hover for the measurement failed"
drive "" --dumpTree >"$OUT/logs/tree-b.txt" 2>&1 || die "tree dump failed"
hover_t=$(grep -F 'value="hover: ' "$OUT/logs/tree-b.txt" | head -1 | sed -n 's/.*value="hover: \([0-9]*\):\([0-9.]*\)".*/\1 \2/p')
[[ -n "$hover_t" ]] || die "no hover readout after the measurement hover"
read -r hm hs <<<"$hover_t"
EX=$(python3 -c "
t = $hm*60 + float('$hs')
import math
k = math.ceil(t / 0.9)
target = k*0.9 + 0.17
print(round($CX + (target - t) / 0.010))")
log "time under the pointer: $hm:$hs — pressing at x=$EX for a region"

traceB2="$OUT/logs/b2-assert.jsonl"
cat >"$traceB2" <<EOF
{"do":"note","text":"--- region editing (SD8): with the mode on, a drag on a region moves it and the host applies the edit ---"}
{"do":"click","name":"edit regions"}
{"do":"drag","x":$EX,"y":$CY,"toX":$((EX+200)),"toY":$CY,"steps":16,"durationMs":320,"settleMs":400}
{"do":"wait","valueContains":"edit: region","role":"label","comment":"an editable region under the press took the drag and reported the new bounds"}
{"do":"click","name":"edit regions","comment":"back to pan-by-drag"}

{"do":"note","text":"--- the wall-clock readout (SD9): the synthetic track has an epoch of 09:00:00 ---"}
{"do":"click","name":"wall clock"}
{"do":"wait","valueContains":"position: 09:00:0","role":"label"}
{"do":"click","name":"wall clock"}
{"do":"wait","valueContains":"position: 0:0","role":"label"}

{"do":"note","text":"--- transport on the null sink: play advances the position, pause holds it ---"}
{"do":"click","name":" play"}
{"do":"wait","valueContains":"· playing","role":"label"}
{"do":"sleep","settleMs":1000}
{"do":"click","name":"\ue39e pause"}
{"do":"wait","valueContains":"· paused","role":"label"}
{"do":"wait","valueContains":"position: 0:01.","role":"label","comment":"between one and two seconds elapsed on the clock"}
{"do":"capture","text":"waveform-player"}
EOF

drive "$traceB2" 2>&1 | tee "$OUT/logs/drive-b2.log"
rc=${PIPESTATUS[0]}
((rc == 0)) || { log "FAIL — see $OUT/logs/drive-b2.log and $OUT/logs/host.log"; exit "$rc"; }

# --- phase C: a file through the decoder -------------------------------------
# The demo's path field is not reachable through the accessibility tree, so
# the file goes in through BOXER_WAVEFORM_DEMO_FILE at mount: a second host,
# the same expansion steps, and the readouts say what happened.
if [[ -n "$FIXTURE" ]]; then
	cleanup
	trap - EXIT
	log "relaunching with BOXER_WAVEFORM_DEMO_FILE=$FIXTURE"
	env -u DISPLAY -u WAYLAND_DISPLAY \
		BOXER_AUDIO_PEAKS_CACHE_DIR="$OUT/peaks-cache" \
		BOXER_WAVEFORM_DEMO_FILE="$FIXTURE" \
		IMZERO2_HEADLESS_LISTEN="127.0.0.1:$PORT" \
		IMZERO2_HEADLESS_DUMP_DIR="$OUT" \
		IMZERO2_HEADLESS_DUMP_EVERY=1000000 \
		IMZERO2_HEADLESS_FPS=30 \
		IMZERO2_SCREENSHOT_SIZE="$SIZE" \
		BOXER_COMPONENT=waveform-scene-file \
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
		>"$OUT/logs/host-file.log" 2>&1 &
	app_pid=$!
	trap cleanup EXIT
	for _ in $(seq 1 $((TIMEOUT * 4))); do
		(exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null && { exec 3<&-; break; }
		kill -0 "$app_pid" 2>/dev/null || break
		sleep 0.25
	done
	traceC="$OUT/logs/c-file.jsonl"
	cat >"$traceC" <<'EOF'
{"do":"note","text":"--- a FLAC through decode → ffmpeg → background build → the widget ---"}
{"do":"wait","role":"text_input","comment":"the gallery has mounted"}
{"do":"focus","role":"text_input"}
{"do":"type","role":"text_input","text":"waveform"}
{"do":"wait","contains":"waveform player","settleMs":400}
{"do":"click","contains":"waveform player","comment":"mounting the demo opens the file named by the environment"}
{"do":"wait","name":"10 ms/px","settleMs":600}
{"do":"scroll_into_view","name":"10 ms/px","settleMs":600}
{"do":"wait","valueContains":"track: ffmpeg file","role":"label","comment":"the sniffing opener routed a FLAC to ffmpeg"}
{"do":"wait","valueContains":"peaks built","role":"label","comment":"the background build over the ffmpeg decoder completed"}
{"do":"wait","valueContains":"· task ","role":"label","comment":"the build was reported as a keelson task through the gallery host's bus (ADR-0038)"}
{"do":"wait","valueContains":"view: 0:00.000 – 0:30.000","role":"label","comment":"the declared length is the file's"}
{"do":"capture","text":"waveform-file"}
EOF
	drive "$traceC" 2>&1 | tee "$OUT/logs/drive-c.log"
	rc=${PIPESTATUS[0]}
	((rc == 0)) || { log "FAIL — the file path; see $OUT/logs/drive-c.log and $OUT/logs/host-file.log"; exit "$rc"; }
	log "PASS — a FLAC opened through ffmpeg, its peaks built in the background: $OUT/waveform-file.png"
else
	log "skipped phase C (no ffmpeg on PATH)"
fi

log "PASS — hover, click-to-seek, drag-to-pan, region editing, the wall-clock readout and the null-sink transport all reached the widget"
log "       capture: $OUT/waveform-player.png"
exit 0
