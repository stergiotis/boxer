#!/bin/bash
# portolan-map-scene.sh — assert the portolan map's camera under synthetic
# gestures, headless.
#
# ADR-0204 M5's regression net. The map widget reads its input one frame
# behind the host from the painter lane's registers, and the failures worth a
# scene are silent: a drag that lands short (the per-frame-delta recipe lost
# 20 × 10 px of a 240 × 120 px drag, ADR-0204 §SD6), a wheel notch that zooms
# about the wrong point or loses part of itself, a double click that does not
# reach its level, arrows that do nothing because focus was surrendered at
# the click. A capture looks right in every one of those cases, so the scene
# asserts the CAMERA the demo reads back from the map — centre, zoom, the
# canvas rect, the tile pipeline — against what each gesture must do, with
# the tolerances the input path earns: a few pixels of inertia after a slow
# drag, the sigmoid's chunking of a wheel notch egui smooths over a dozen
# frames.
#
# Tiles come from scripts/dev/tile-stub-server.py, so the scene runs offline
# and the basemap machinery (BOXER_MAP_TILE_URL, the Go loader, paintImage)
# is under test as much as the map; a capture of the end state is written
# as a by-product.
#
# Usage:
#   scripts/dev/portolan-map-scene.sh
#   PORTOLANSCENE_BUILD=0 scripts/dev/portolan-map-scene.sh   # reuse rust/imzero2/ binaries
#   PORTOLANSCENE_CLIENT=… PORTOLANSCENE_MAIN_GO=… …           # an explicit pair
#
# The Rust client is the CPU-rasterised headless host by default
# (rust/imzero2/build_rust_headless_soft.sh; no GPU, no Vulkan loader), the
# wgpu one when only that is built. Exit status is the assertion.
set -uo pipefail

here=$(dirname "$(readlink -f "$BASH_SOURCE")")
root=$(cd "$here/../.." && pwd)

OUT="${PORTOLANSCENE_OUT:-$root/tmp/portolan-scene}"
SIZE="${PORTOLANSCENE_SIZE:-1100x800}"
TIMEOUT="${PORTOLANSCENE_TIMEOUT:-120}"
SETTLE_MS="${PORTOLANSCENE_SETTLE_MS:-250}"
BUILD="${PORTOLANSCENE_BUILD:-1}"
BIN="${PORTOLANSCENE_BIN:-$OUT/bin}"
PORT="${PORTOLANSCENE_PORT:-8797}"
export IMZERO2_HEADLESS_ENCODER_ARGS="${IMZERO2_HEADLESS_ENCODER_ARGS:--c:v libopenh264 -rc_mode off -bf 0 -g 100000}"

log() { printf '%s\n' "$*" >&2; }
app_pid=""; stub_pid=""
cleanup() {
	# Kill THIS run only, by pid: a pattern kill over the shared binary path
	# would take out a concurrent session's client. The Rust client is a child
	# of the Go host, so reap it explicitly; the stub server is ours too.
	if [[ -n "$app_pid" ]]; then
		kids=$(pgrep -P "$app_pid" 2>/dev/null)
		kill "$app_pid" $kids 2>/dev/null
		wait "$app_pid" 2>/dev/null
		for _ in $(seq 1 40); do
			(exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null || break
			exec 3<&-
			sleep 0.25
		done
		app_pid=""
	fi
	[[ -n "$stub_pid" ]] && { kill "$stub_pid" 2>/dev/null; wait "$stub_pid" 2>/dev/null; stub_pid=""; }
}
die() { log "portolan-map-scene: FAIL — $*"; cleanup; exit 1; }
trap cleanup EXIT

mkdir -p "$OUT/logs"

# --- the camera oracle --------------------------------------------------------
# Reading the demo's readout lines, and checking a gesture against what Leaflet
# says it must do, is `boxer dev portolan-cam`. Built once: the scene reads and
# checks per gesture.
TOOL="${PORTOLANSCENE_TOOL:-$OUT/bin/boxer}"
if [[ -z "${PORTOLANSCENE_TOOL:-}" ]]; then
	mkdir -p "$(dirname "$TOOL")"
	( cd "$root" && CGO_ENABLED=0 go build -tags "$(tr -d '\n' < ./tags)" \
		-o "$TOOL" ./public/app ) || die "go build (boxer) failed"
fi

# --- binaries ----------------------------------------------------------------
# A private pair by default, for the reason the tree scene builds one: a Go
# host paired with a Rust client from a different egui2 codegen desyncs the
# FFFI wire mid-frame.
client_default() {
	local c
	for c in "$root/rust/imzero2/target/headless-soft/release/imzero2" \
	         "$root/rust/imzero2/target/headless/release/imzero2"; do
		[[ -x "$c" ]] && { printf '%s' "$c"; return; }
	done
}
if [[ "$BUILD" == 1 ]]; then
	mkdir -p "$BIN"
	log "building a private host into $BIN (PORTOLANSCENE_BUILD=0 to reuse rust/imzero2/)"
	( cd "$root" && CGO_ENABLED=0 go build -tags "$(tr -d '\n' < ./tags),binary_log" \
		-o "$BIN/main_go" ./public/thestack/cmd/imzero2/ ) || die "go build failed"
	src="${PORTOLANSCENE_CLIENT:-$(client_default)}"
	[[ -n "$src" && -x "$src" ]] || die "no headless client — run rust/imzero2/build_rust_headless_soft.sh"
	cp -fp "$src" "$BIN/imzero2" || die "cannot copy the headless client"
	for gen in "$root/rust/imzero2/src/imzero2/enums_out.rs" \
	           "$root/rust/imzero2/src/imzero2/interpreter.rs"; do
		if [[ "$gen" -nt "$BIN/imzero2" ]]; then
			die "$(basename "$gen") is newer than the headless client $src — rebuild it first"
		fi
	done
	MAIN_GO="$BIN/main_go"; CLIENT="$BIN/imzero2"
else
	MAIN_GO="${PORTOLANSCENE_MAIN_GO:-$root/rust/imzero2/main_go}"
	CLIENT="${PORTOLANSCENE_CLIENT:-$(client_default)}"
fi
[[ -x "$MAIN_GO" ]] || die "no Go host at $MAIN_GO"
[[ -n "$CLIENT" && -x "$CLIENT" ]] || die "no Rust client"

# --- fonts (mirrors rust/imzero2/hmi.sh) -------------------------------------
resolve_font() { # resolve_font <family> <substring the match must contain>
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

# --- the stub tile server ----------------------------------------------------
python3 "$here/tile-stub-server.py" 0 >"$OUT/logs/tiles.log" 2>&1 &
stub_pid=$!
for _ in $(seq 1 50); do
	grep -q '^ready ' "$OUT/logs/tiles.log" 2>/dev/null && break
	sleep 0.1
done
TILE_PORT=$(awk '/^ready /{print $2; exit}' "$OUT/logs/tiles.log")
[[ -n "$TILE_PORT" ]] || die "the tile stub did not start — see $OUT/logs/tiles.log"

drive() { # drive <trace file>
	timeout "$TIMEOUT" "$MAIN_GO" --logFormat=console --logLevel=info \
		imzero2 drive --url "ws://127.0.0.1:$PORT/" --trace "$1" --settle "$SETTLE_MS" \
		>>"$OUT/logs/drive.log" 2>&1
}
# r names a reading on disk; the demo's readout lines are the instrument, and
# `portolan-cam read` turns one accessibility-tree dump into one reading.
r() { printf '%s' "$OUT/logs/$1.json"; }
snapshot() { # snapshot <name>  → $OUT/logs/<name>.json
	# One JSON object per label on stdout; the driver's log goes to stderr.
	timeout 60 "$MAIN_GO" imzero2 drive --url "ws://127.0.0.1:$PORT/" \
		--dumpTree --treeFormat jsonl --treeRole label \
		>"$OUT/logs/$1.tree.jsonl" 2>>"$OUT/logs/drive.log"
	"$TOOL" dev portolan-cam read "$OUT/logs/$1.tree.jsonl" >"$(r "$1")" ||
		die "cannot read the camera ($1) — see $OUT/logs/$1.tree.jsonl"
}
check() { # check <verb> [flags…] <reading…>
	local msg
	if msg=$("$TOOL" dev portolan-cam "$@"); then
		log "  ok   $msg"
	else
		log "  FAIL $msg"
		die "$1 did not do what it must — see $OUT/logs/"
	fi
}
# canvas-relative → screen, from a snapshot's canvas rect
at() { "$TOOL" dev portolan-cam at --dx "$2" --dy "$3" "$(r "$1")"; }

# --- run ---------------------------------------------------------------------
log "launching the widget gallery headless on 127.0.0.1:$PORT (tiles from the stub on $TILE_PORT)"
env -u DISPLAY -u WAYLAND_DISPLAY \
	IMZERO2_HEADLESS_LISTEN="127.0.0.1:$PORT" \
	IMZERO2_HEADLESS_DUMP_DIR="$OUT" \
	IMZERO2_HEADLESS_DUMP_EVERY=1000000 \
	IMZERO2_HEADLESS_FPS=60 \
	IMZERO2_SCREENSHOT_SIZE="$SIZE" \
	BOXER_COMPONENT=portolan-map-scene \
	BOXER_MAP_TILE_URL="http://127.0.0.1:$TILE_PORT/{z}/{x}/{y}.png" \
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
	kill -0 "$app_pid" 2>/dev/null || die "the host exited before listening — see $OUT/logs/host.log"
	sleep 0.25
done

# 1. The demo up, every tile of the first view landed.
t="$OUT/logs/t1.jsonl"
cat >"$t" <<'TRACE'
{"do":"note","text":"ADR-0204 M5 — the portolan map's camera under synthetic gestures, asserted through the demo's readout"}
{"do":"wait","role":"text_input","comment":"the gallery has mounted"}
{"do":"focus","role":"text_input"}
{"do":"type","role":"text_input","text":"portolan","comment":"narrow the gallery to the one demo"}
{"do":"wait","contains":"portolan (slippy","settleMs":400}
{"do":"click","contains":"portolan (slippy","comment":"expand the demo's section"}
{"do":"wait","valueContains":"bounds","role":"label","settleMs":500,"comment":"the readout is up"}
{"do":"wait","valueContains":"loading false","role":"label","settleMs":800,"comment":"every tile of the first view landed"}
TRACE
drive "$t" || die "the demo did not come up — see $OUT/logs/drive.log and host.log"
snapshot s0
check baseline "$(r s0)"

# 2. A slow 240 × 120 px drag from the canvas centre: the centre moves by
#    exactly that, bar a few pixels of inertia at 200 px/s (a measured run
#    coasted 3; the recipe this guards against lost 20 × 10).
read -r cx cy < <(at s0 360 230)
t="$OUT/logs/t2.jsonl"
printf '%s\n' "{\"do\":\"drag\",\"x\":$cx,\"y\":$cy,\"toX\":$((cx + 240)),\"toY\":$((cy + 120)),\"steps\":24,\"durationMs\":1200,\"settleMs\":1500,\"comment\":\"the drag verb, ADR-0204 §SD10\"}" \
       '{"do":"wait","valueContains":"loading false","role":"label","settleMs":400}' >"$t"
drive "$t" || die "the drag step failed — see $OUT/logs/drive.log"
snapshot s1
check drag --dx 240 --dy 120 --tol 6 "$(r s0)" "$(r s1)"

# 3. One wheel notch at the canvas centre: a zoom of Leaflet's sigmoid,
#    chunked by egui's smoothing, about the centre.
read -r cx cy < <(at s1 360 230)
t="$OUT/logs/t3.jsonl"
printf '%s\n' "{\"do\":\"hover\",\"x\":$cx,\"y\":$cy,\"settleMs\":200}" \
       '{"do":"scroll","x":0,"y":60,"settleMs":1500,"comment":"one notch, 60 px: +0.6..0.8 levels through the sigmoid"}' >"$t"
drive "$t" || die "the wheel step failed"
snapshot s2
check wheel --lo 0.55 --hi 0.80 --tol 3 "$(r s1)" "$(r s2)"

# 4. A double click at the canvas centre: one level in, animated, anchored.
read -r cx cy < <(at s2 360 230)
t="$OUT/logs/t4.jsonl"
printf '%s\n' "{\"do\":\"click\",\"x\":$cx,\"y\":$cy,\"settleMs\":60}" \
       "{\"do\":\"click\",\"x\":$cx,\"y\":$cy,\"settleMs\":1500,\"comment\":\"two clicks within egui's double-click window\"}" >"$t"
drive "$t" || die "the double-click step failed"
snapshot s3
check dblclick --tol 3 "$(r s2)" "$(r s3)"

# 5. ArrowRight after the click left the map focused: 80 px, animated.
t="$OUT/logs/t5.jsonl"
printf '%s\n' '{"do":"key","text":"ArrowRight","settleMs":1200,"comment":"the map took focus on the click; Leaflet pans 80 px per arrow"}' >"$t"
drive "$t" || die "the key step failed"
snapshot s4
check key --dx 80 --tol 1.5 "$(r s3)" "$(r s4)"

# 6. The pipeline: no errors, no re-ships, and a capture of where we ended.
t="$OUT/logs/t6.jsonl"
printf '%s\n' '{"do":"wait","valueContains":"loading false","role":"label","settleMs":600}' \
       '{"do":"capture","text":"portolan-scene","comment":"the end state: stub tiles, overlays, the readout"}' >"$t"
drive "$t" || die "the capture step failed"
snapshot s5
check pipeline "$(r s5)"

cleanup
log "PASS — drag, wheel, double click and arrow key each moved the camera as Leaflet says"
log "       capture: $OUT/portolan-scene.png; readings: $OUT/logs/s*.json"
exit 0
