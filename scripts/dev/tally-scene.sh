#!/bin/bash
# tally-scene.sh — drive the tally app headless against the local store.
#
# ADR-0200 M2: open the app, pick a mount, enter a directory, select a file,
# and see its preview and its recorded attributes — asserted through the
# accessibility tree (ADR-0154), with two captures for a human to look at.
# It needs a reachable ClickHouse with a provisioned lading store holding a
# mount whose newest snapshot has the path the trace walks; the defaults
# match a `boxer fs snapshot --mount 0x3BFE363BCF148002 --name boxer-doc doc`
# taken from the repository root. Override the anchors with TALLYSCENE_MOUNT,
# TALLYSCENE_DIR and TALLYSCENE_FILE.
#
# Built on scripts/dev/fsbrowser-widget-scene.sh: same host, same driver.
set -uo pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
root=$(cd "$here/../.." && pwd)
OUT="${TALLYSCENE_OUT:-$root/tmp/tally-scene}"
SIZE="${TALLYSCENE_SIZE:-1400x1000}"
TIMEOUT="${TALLYSCENE_TIMEOUT:-150}"
SETTLE_MS="${TALLYSCENE_SETTLE_MS:-300}"
BUILD="${TALLYSCENE_BUILD:-1}"
BIN="${TALLYSCENE_BIN:-$OUT/bin}"
DRY="${TALLYSCENE_DRY:-}"
PORT="${TALLYSCENE_PORT:-8797}"
MOUNT="${TALLYSCENE_MOUNT:-boxer-doc}"
DIR="${TALLYSCENE_DIR:-adr}"
FILE="${TALLYSCENE_FILE:-0198-fs-snapshot-store.md}"
FILTER="${TALLYSCENE_FILTER:-0198}"
MOUNT2="${TALLYSCENE_MOUNT2:-lading-src}"
FINDPATTERN="${TALLYSCENE_FINDPATTERN:-0198}"
export IMZERO2_HEADLESS_ENCODER_ARGS="${IMZERO2_HEADLESS_ENCODER_ARGS:--c:v libopenh264 -rc_mode off -bf 0 -g 100000}"
log() { printf '%s\n' "$*" >&2; }
die() { log "tally-scene: $*"; exit 1; }
mkdir -p "$OUT/logs"
if [[ "$BUILD" == 1 ]]; then
	mkdir -p "$BIN"
	log "building a private host into $BIN (TALLYSCENE_BUILD=0 to reuse)"
	( cd "$root" && CGO_ENABLED=0 go build -tags "$(tr -d '\n' < ./tags),binary_log" \
		-o "$BIN/main_go" ./public/thestack/cmd/imzero2/ ) || die "go build failed"
	[[ -x "$root/rust/imzero2/target/headless/release/imzero2" ]] ||
		die "no headless client — run rust/imzero2/build_rust_headless.sh"
	cp -fp "$root/rust/imzero2/target/headless/release/imzero2" "$BIN/imzero2" ||
		die "cannot copy the headless client"
fi
MAIN_GO="${TALLYSCENE_MAIN_GO:-$BIN/main_go}"
CLIENT="${TALLYSCENE_CLIENT:-$BIN/imzero2}"
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
trace="$OUT/logs/tally-scene.jsonl"
cat >"$trace" <<EOF
{"do":"note","text":"ADR-0200 M2 — tally against the local store"}
{"do":"wait","value":"Mounts","role":"label","settleMs":500,"comment":"the window is up; the mount list is a lane and may still be loading"}
{"do":"wait","contains":"$MOUNT  ·","role":"button","settleMs":400,"comment":"the mount list arrived (the count suffix tells the mount button from the breadcrumb root)"}
{"do":"click","contains":"$MOUNT  ·","role":"button"}
{"do":"wait","name":"Follow latest","settleMs":600,"comment":"the mount is selected: its snapshots and the follow toggle are up"}
{"do":"note","text":"--- a known starting point: the app restores the last workingset on a plain open (ADR-0148), so pane A may be anywhere; target A, switch its mount away and back, which resets it to the root following latest ---"}
{"do":"click","name":"A","role":"button","settleMs":200}
{"do":"click","contains":"$MOUNT2  ·","role":"button","settleMs":400}
{"do":"click","contains":"$MOUNT  ·","role":"button","settleMs":600}
{"do":"wait","name":"Follow latest","settleMs":400}
{"do":"note","text":"--- enter a directory: select by pointer, then Enter ---"}
{"do":"click","value":"$DIR","role":"label","pointer":true,"nth":0,"settleMs":300,"comment":"nth 0: pane A comes first in the tree; pane B shows the same directory"}
{"do":"key","text":"Enter"}
{"do":"wait","name":"$DIR","role":"button","nth":0,"settleMs":600,"comment":"the breadcrumb grew a segment: we are inside"}
{"do":"note","text":"--- narrow the listing with the quick filter, then select the file: preview and info follow ---"}
{"do":"focus","role":"text_input","nth":0,"comment":"pane A's quick filter is the first text input in the window (pane B has its own)"}
{"do":"type","role":"text_input","nth":0,"text":"$FILTER","settleMs":300}
{"do":"click","value":"$FILE","role":"label","pointer":true,"nth":0,"settleMs":300}
{"do":"wait","valueContains":"$FILE  ·","role":"label","settleMs":1500,"comment":"the preview header names the file and its size"}
{"do":"capture","text":"tally-preview","comment":"pane A inside $DIR with $FILE selected, its preview below"}
{"do":"click","x":465,"y":523,"settleMs":400,"comment":"the Info tab of the bottom leaf — egui_dock tabs are not in the accessibility tree, so the tab strip is hit by position; the coordinates are for the default 1400x1000 window and the layout as committed (Preview 388, Info 465, History 540, Diff 613 at y 523)"}
{"do":"wait","value":"content_hash","role":"label","settleMs":1500,"comment":"the Info grid carries the recorded BLAKE3 hash"}
{"do":"capture","text":"tally-info","comment":"the Info tab: the entry's attributes from fs()"}
{"do":"note","text":"--- History: the selected path across every snapshot of the mount ---"}
{"do":"click","x":540,"y":523,"settleMs":400}
{"do":"wait","valueContains":" across ","role":"label","settleMs":1500,"comment":"the history header: N snapshot(s) carry the path"}
{"do":"capture","text":"tally-history","comment":"the History tab: timeline flags and the versions table"}
{"do":"note","text":"--- Diff: point pane B at the other mount and compare pane A's directory against it ---"}
{"do":"click","name":"B","role":"button","settleMs":300,"comment":"the Mounts clicks now address pane B"}
{"do":"click","contains":"$MOUNT2  ·","role":"button","settleMs":600}
{"do":"click","x":613,"y":523,"settleMs":400}
{"do":"wait","valueContains":"added · ","role":"label","settleMs":2500,"comment":"the diff summary: counts of added / removed / modified"}
{"do":"capture","text":"tally-diff","comment":"the Diff tab: pane A's directory against pane B's snapshot, coloured by change"}
{"do":"note","text":"--- Find: a name search under pane A's directory ---"}
{"do":"click","x":675,"y":523,"settleMs":400}
{"do":"click","name":"A","role":"button","settleMs":300,"comment":"search in pane A's directory, not B's"}
{"do":"focus","role":"text_input","nth":3,"comment":"the find pattern box. Measured, not reasoned: with the Find tab up the accessibility tree lists the bottom leaf's inputs first and in reverse (needle, min, ext, pattern), then the pane filters"}
{"do":"type","role":"text_input","nth":3,"text":"$FINDPATTERN","settleMs":200}
{"do":"click","name":"Search","role":"button","settleMs":300}
{"do":"wait","value":"1 result(s) in this directory","role":"label","settleMs":1500,"comment":"the pattern matched exactly the one ADR"}
{"do":"capture","text":"tally-find","comment":"the Find tab: results of the name search"}
{"do":"note","text":"--- Du: directory totals and the treemap ---"}
{"do":"click","x":740,"y":523,"settleMs":400}
{"do":"wait","valueContains":"Disk usage","role":"label","settleMs":2500}
{"do":"capture","text":"tally-du","comment":"the Du tab: the one-pass du table and the file treemap"}
{"do":"note","text":"--- Problems: unreadable entries, and the audit on demand ---"}
{"do":"click","x":815,"y":523,"settleMs":400}
{"do":"wait","valueContains":"unreadable entries","role":"label","settleMs":1500}
{"do":"capture","text":"tally-problems","comment":"the Problems tab"}
EOF
log "launching tally headless on 127.0.0.1:$PORT"
env -u DISPLAY -u WAYLAND_DISPLAY \
	IMZERO2_HEADLESS_LISTEN="127.0.0.1:$PORT" \
	IMZERO2_HEADLESS_DUMP_DIR="$OUT" \
	IMZERO2_HEADLESS_DUMP_EVERY=1000000 \
	IMZERO2_HEADLESS_FPS=30 \
	IMZERO2_SCREENSHOT_SIZE="$SIZE" \
	BOXER_COMPONENT=tally-scene \
	"$MAIN_GO" --logFormat=console --logLevel=warn \
		imzero2 demo \
		--clientBinary "$CLIENT" \
		--clientInitialMainWindowWidth "$W" \
		--clientInitialMainWindowHeight "$H" \
		${MAIN_FONT:+--mainFontTTF "$MAIN_FONT"} \
		${MONO_FONT:+--monoFontTTF "$MONO_FONT"} \
		${PHOSPHOR_FONT:+--phosphorFontTTF "$PHOSPHOR_FONT"} \
		${FALLBACK_FONT:+--fallbackFontTTF "$FALLBACK_FONT"} \
		--launch tally \
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
	log "PASS — mount, directory, file, preview, info, history, diff, find, du and problems all reached the app"
	log "       captures under $OUT/: tally-preview, tally-info, tally-history, tally-diff, tally-find, tally-du, tally-problems (.png)"
else
	log "FAIL — see $OUT/logs/drive.log and $OUT/logs/host.log"
fi
exit "$rc"
