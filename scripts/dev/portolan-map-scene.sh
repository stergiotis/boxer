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

# --- the camera reader and its arithmetic -----------------------------------
# The demo's readout lines are the instrument: "centre LAT, LON   zoom Z …",
# "canvas at X,Y · W × H px", "tiles: … E errors … loading B", "… re-ships R
# …". `read` turns the accessibility tree into one JSON line; the other
# subcommands compare two such readings against what a gesture must have
# done, in web-mercator pixels at the zoom of the reading.
cam="$OUT/logs/cam.py"
cat >"$cam" <<'PY'
import json, math, re, sys

def read(path):
    out = {}
    for line in open(path):
        line = line.strip()
        if not line.startswith("{"):
            continue
        try:
            d = json.loads(line)
        except ValueError:
            continue
        if d.get("role") != "label":
            continue
        v = str(d.get("value", ""))
        m = re.match(r"centre (-?[\d.]+), (-?[\d.]+)\s+zoom ([\d.]+)", v)
        if m:
            out["lat"], out["lon"], out["zoom"] = float(m[1]), float(m[2]), float(m[3])
        m = re.match(r"canvas at (-?[\d.]+),(-?[\d.]+) · (\d+) × (\d+) px", v)
        if m:
            out["ox"], out["oy"], out["w"], out["h"] = (float(m[1]), float(m[2]), int(m[3]), int(m[4]))
        m = re.search(r"(\d+) requested · (\d+) loaded · (\d+) errors .* loading (\w+)", v)
        if m:
            out["requested"], out["loaded"], out["errors"], out["loading"] = int(m[1]), int(m[2]), int(m[3]), m[4]
        m = re.search(r"re-ships (\d+)", v)
        if m:
            out["reships"] = int(m[1])
    return out

def px(lat, lon, z):
    s = 256 * 2 ** z
    x = (lon + 180) / 360 * s
    y = (1 - math.log(math.tan(math.radians(lat)) + 1 / math.cos(math.radians(lat))) / math.pi) / 2 * s
    return x, y

def dist_px(a, b, z):
    ax, ay = px(a["lat"], a["lon"], z)
    bx, by = px(b["lat"], b["lon"], z)
    return bx - ax, by - ay

def need(r, *keys):
    for k in keys:
        if k not in r:
            sys.exit("readout lacks %r in %s" % (k, r))

cmd = sys.argv[1]
if cmd == "read":
    r = read(sys.argv[2])
    need(r, "lat", "lon", "zoom", "ox", "oy", "w", "h", "loading", "errors", "reships")
    print(json.dumps(r))
    sys.exit(0)
a = json.load(open(sys.argv[2]))
b = json.load(open(sys.argv[3]))
if cmd == "baseline":
    ok = abs(a["lat"] - 51.0992) < 1e-4 and abs(a["lon"] - 17.0366) < 1e-4 and abs(a["zoom"] - 12) < 0.006
    ok = ok and a["w"] == 720 and a["h"] == 460 and a["loading"] == "false" and a["errors"] == 0
    print("baseline: centre %.5f,%.5f zoom %.2f canvas %dx%d at %.0f,%.0f errors %d" % (a["lat"], a["lon"], a["zoom"], a["w"], a["h"], a["ox"], a["oy"], a["errors"]))
    sys.exit(0 if ok else 1)
if cmd == "drag":
    dx, dy, tol = float(sys.argv[4]), float(sys.argv[5]), float(sys.argv[6])
    mx, my = dist_px(a, b, a["zoom"])
    # dragging the map by (+dx,+dy) moves the centre by (-dx,-dy) pixels
    ex, ey = mx + dx, my + dy
    print("drag: centre moved %.2f,%.2f px (expected %.0f,%.0f ± %.0f; inertia on a slow drag is a pixel or two)" % (mx, my, -dx, -dy, tol))
    sys.exit(0 if abs(ex) <= tol and abs(ey) <= tol and abs(a["zoom"] - b["zoom"]) < 0.006 else 1)
if cmd == "wheel":
    lo, hi, tol = float(sys.argv[4]), float(sys.argv[5]), float(sys.argv[6])
    dz = b["zoom"] - a["zoom"]
    mx, my = dist_px(a, b, a["zoom"])
    print("wheel: zoom %+.2f (expected %.2f..%.2f), centre moved %.2f,%.2f px (within %.0f: the notch is about the centre)" % (dz, lo, hi, mx, my, tol))
    sys.exit(0 if lo <= dz <= hi and abs(mx) <= tol and abs(my) <= tol else 1)
if cmd == "dblclick":
    tol = float(sys.argv[4])
    dz = b["zoom"] - a["zoom"]
    mx, my = dist_px(a, b, a["zoom"])
    print("double click: zoom %+.2f (expected +1.00), centre moved %.2f,%.2f px (within %.0f: anchored at the centre)" % (dz, mx, my, tol))
    sys.exit(0 if abs(dz - 1) < 0.011 and abs(mx) <= tol and abs(my) <= tol else 1)
if cmd == "key":
    dx, tol = float(sys.argv[4]), float(sys.argv[5])
    mx, my = dist_px(a, b, a["zoom"])
    print("ArrowRight: centre moved %.2f,%.2f px (expected %.0f,0 within %.1f)" % (mx, my, dx, tol))
    sys.exit(0 if abs(mx - dx) <= tol and abs(my) <= tol and abs(a["zoom"] - b["zoom"]) < 0.006 else 1)
if cmd == "pipeline":
    print("tiles: %d requested, %d loaded, %d errors, re-ships %d, loading %s" % (b["requested"], b["loaded"], b["errors"], b["reships"], b["loading"]))
    sys.exit(0 if b["errors"] == 0 and b["reships"] == 0 and b["loading"] == "false" and b["loaded"] >= 12 else 1)
sys.exit("unknown command " + cmd)
PY

drive() { # drive <trace file>
	timeout "$TIMEOUT" "$MAIN_GO" --logFormat=console --logLevel=info \
		imzero2 drive --url "ws://127.0.0.1:$PORT/" --trace "$1" --settle "$SETTLE_MS" \
		>>"$OUT/logs/drive.log" 2>&1
}
snapshot() { # snapshot <name>  → $OUT/logs/<name>.json
	# The node lines are log output, so they arrive on stderr.
	timeout 60 "$MAIN_GO" imzero2 drive --url "ws://127.0.0.1:$PORT/" --dumpTree \
		>"$OUT/logs/$1.tree.jsonl" 2>&1
	python3 "$cam" read "$OUT/logs/$1.tree.jsonl" >"$OUT/logs/$1.json" || die "cannot read the camera ($1) — see $OUT/logs/$1.tree.jsonl"
}
check() { # check <cmd> <a> <b> [args…]
	local cmd=$1 a=$2 b=$3; shift 3
	local msg
	if msg=$(python3 "$cam" "$cmd" "$OUT/logs/$a.json" "$OUT/logs/$b.json" "$@"); then
		log "  ok   $msg"
	else
		log "  FAIL $msg"
		die "$cmd did not do what it must — see $OUT/logs/"
	fi
}
# canvas-relative → screen, from a snapshot's canvas rect
at() { python3 -c "import json; r=json.load(open('$OUT/logs/$1.json')); print(int(r['ox']+$2), int(r['oy']+$3))"; }

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
check baseline s0 s0

# 2. A slow 240 × 120 px drag from the canvas centre: the centre moves by
#    exactly that, bar a few pixels of inertia at 200 px/s (a measured run
#    coasted 3; the recipe this guards against lost 20 × 10).
read -r cx cy < <(at s0 360 230)
t="$OUT/logs/t2.jsonl"
printf '%s\n' "{\"do\":\"drag\",\"x\":$cx,\"y\":$cy,\"toX\":$((cx + 240)),\"toY\":$((cy + 120)),\"steps\":24,\"durationMs\":1200,\"settleMs\":1500,\"comment\":\"the drag verb, ADR-0204 §SD10\"}" \
       '{"do":"wait","valueContains":"loading false","role":"label","settleMs":400}' >"$t"
drive "$t" || die "the drag step failed — see $OUT/logs/drive.log"
snapshot s1
check drag s0 s1 240 120 6

# 3. One wheel notch at the canvas centre: a zoom of Leaflet's sigmoid,
#    chunked by egui's smoothing, about the centre.
read -r cx cy < <(at s1 360 230)
t="$OUT/logs/t3.jsonl"
printf '%s\n' "{\"do\":\"hover\",\"x\":$cx,\"y\":$cy,\"settleMs\":200}" \
       '{"do":"scroll","x":0,"y":60,"settleMs":1500,"comment":"one notch, 60 px: +0.6..0.8 levels through the sigmoid"}' >"$t"
drive "$t" || die "the wheel step failed"
snapshot s2
check wheel s1 s2 0.55 0.80 3

# 4. A double click at the canvas centre: one level in, animated, anchored.
read -r cx cy < <(at s2 360 230)
t="$OUT/logs/t4.jsonl"
printf '%s\n' "{\"do\":\"click\",\"x\":$cx,\"y\":$cy,\"settleMs\":60}" \
       "{\"do\":\"click\",\"x\":$cx,\"y\":$cy,\"settleMs\":1500,\"comment\":\"two clicks within egui's double-click window\"}" >"$t"
drive "$t" || die "the double-click step failed"
snapshot s3
check dblclick s2 s3 3

# 5. ArrowRight after the click left the map focused: 80 px, animated.
t="$OUT/logs/t5.jsonl"
printf '%s\n' '{"do":"key","text":"ArrowRight","settleMs":1200,"comment":"the map took focus on the click; Leaflet pans 80 px per arrow"}' >"$t"
drive "$t" || die "the key step failed"
snapshot s4
check key s3 s4 80 1.5

# 6. The pipeline: no errors, no re-ships, and a capture of where we ended.
t="$OUT/logs/t6.jsonl"
printf '%s\n' '{"do":"wait","valueContains":"loading false","role":"label","settleMs":600}' \
       '{"do":"capture","text":"portolan-scene","comment":"the end state: stub tiles, overlays, the readout"}' >"$t"
drive "$t" || die "the capture step failed"
snapshot s5
check pipeline s5 s5

cleanup
log "PASS — drag, wheel, double click and arrow key each moved the camera as Leaflet says"
log "       capture: $OUT/portolan-scene.png; readings: $OUT/logs/s*.json"
exit 0
