#!/bin/bash
# tree-widget-scene.sh — assert the tree widget's pointer interaction, headless.
#
# ADR-0176's verification plan asks for one thing no unit test can reach: that
# clicking a row selects it and clicking a disclosure control folds it, in a
# real render. The interesting failure is SD6's — a doubled `row_ui` replay
# emits every row's widget ids twice, which does not break rendering or
# clicking, only READ-BACK, silently and newest-wins. So the scene asserts the
# SELECTION, not the picture: a capture would still look correct.
#
# It runs through the HEADLESS host (ADR-0024) with no compositor anywhere, and
# `imzero2 drive` replays the gestures against the accessibility tree
# (ADR-0154). The gallery's "tree outline" demo (ADR-0176 M4) is the subject;
# its readout lines are what the assertions watch.
#
# Usage:
#   scripts/dev/tree-widget-scene.sh
#   TREESCENE_DRY=1 scripts/dev/tree-widget-scene.sh   # resolve anchors only
#   TREESCENE_BUILD=0 scripts/dev/tree-widget-scene.sh # reuse rust/imzero2/ binaries
#
# Exit status is the assertion: a `wait` that never resolves fails the run, so
# a broken selection path exits non-zero rather than producing a wrong picture.
set -uo pipefail

here=$(dirname "$(readlink -f "$BASH_SOURCE")")
root=$(cd "$here/../.." && pwd)

OUT="${TREESCENE_OUT:-$root/tmp/tree-scene}"
SIZE="${TREESCENE_SIZE:-1400x1000}"
TIMEOUT="${TREESCENE_TIMEOUT:-90}"
SETTLE_MS="${TREESCENE_SETTLE_MS:-250}"
BUILD="${TREESCENE_BUILD:-1}"
BIN="${TREESCENE_BIN:-$OUT/bin}"
DRY="${TREESCENE_DRY:-}"
PORT="${TREESCENE_PORT:-8795}"
# The carrier spawns a video encoder even though nothing here decodes a frame;
# the software one needs no VAAPI driver on the box.
export IMZERO2_HEADLESS_ENCODER_ARGS="${IMZERO2_HEADLESS_ENCODER_ARGS:--c:v libopenh264 -rc_mode off -bf 0 -g 100000}"

log() { printf '%s\n' "$*" >&2; }
die() { log "tree-widget-scene: $*"; exit 1; }

mkdir -p "$OUT/logs"

# --- binaries ----------------------------------------------------------------
# A private pair by default, for the reason play's tour builds one: a Go host
# paired with a Rust client from a different egui2 codegen desyncs the FFFI
# wire mid-frame, and every assertion then fails for a reason that has nothing
# to do with the widget.
if [[ "$BUILD" == 1 ]]; then
	mkdir -p "$BIN"
	log "building a private host into $BIN (TREESCENE_BUILD=0 to reuse rust/imzero2/)"
	( cd "$root" && CGO_ENABLED=0 go build -tags "$(tr -d '\n' < ./tags),binary_log" \
		-o "$BIN/main_go" ./public/thestack/cmd/imzero2/ ) || die "go build failed"
	[[ -x "$root/rust/imzero2/target/headless/release/imzero2" ]] ||
		die "no headless client — run rust/imzero2/build_rust_headless.sh"
	# -p preserves the mtime the staleness guard below reads.
	cp -fp "$root/rust/imzero2/target/headless/release/imzero2" "$BIN/imzero2" ||
		die "cannot copy the headless client"
	for gen in "$root/rust/imzero2/src/imzero2/enums_out.rs" \
	           "$root/rust/imzero2/src/imzero2/interpreter.rs"; do
		if [[ "$gen" -nt "$BIN/imzero2" ]]; then
			die "$(basename "$gen") is newer than the headless client — rebuild it with rust/imzero2/build_rust_headless.sh"
		fi
	done
fi
MAIN_GO="${TREESCENE_MAIN_GO:-$BIN/main_go}"
CLIENT="${TREESCENE_CLIENT:-$BIN/imzero2}"
[[ "$BUILD" == 1 ]] || { MAIN_GO="${TREESCENE_MAIN_GO:-$root/rust/imzero2/main_go}"
                         CLIENT="${TREESCENE_CLIENT:-$root/rust/imzero2/target/headless/release/imzero2}"; }
[[ -x "$MAIN_GO" ]] || die "no Go host at $MAIN_GO"
[[ -x "$CLIENT" ]] || die "no Rust client at $CLIENT"

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

# --- the trace ---------------------------------------------------------------
# Every assertion is a `wait`, which polls until its anchor resolves and fails
# the run when it never does.
#
# Two anchor kinds are in play, and the difference is the point:
#
#   * `name` is the accessible NAME, which only interactive widgets carry —
#     the demo's buttons and the tree's own disclosure controls.
#   * `value` / `valueContains` reach the accessible VALUE, which is where egui
#     puts a Label's text while leaving the name empty. The readout lines and
#     the row labels are only findable this way, and each is pinned to
#     role "label": egui emits a `text_run` child under SOME of its labels
#     carrying the same text, so a bare value anchor resolves on one and
#     reports an ambiguity on the next.
#
# And one click is `pointer`: a row's click sense sits BEHIND its cells so the
# disclosure button can win its own rect (SD7), which leaves the row's label an
# ordinary non-interactive node — findable, but deaf to an AccessKit action.
# Aiming a synthetic pointer at where that label was drawn hits the row region
# behind it, which is exactly what a human click does.
trace="$OUT/logs/tree-scene.jsonl"
cat >"$trace" <<'EOF'
{"do":"note","text":"ADR-0176 M4 — the tree widget's pointer interaction, asserted through the accessibility tree"}
{"do":"wait","role":"text_input","comment":"the gallery has mounted; its filter box is the only text input while every demo is folded"}
{"do":"focus","role":"text_input"}
{"do":"type","role":"text_input","text":"tree","comment":"narrow the gallery so the demo lands near the top of the scroll"}
{"do":"wait","contains":"tree outline","settleMs":400}
{"do":"click","contains":"tree outline","comment":"expand the demo's section"}
{"do":"wait","name":"collapse all","settleMs":400,"comment":"the demo body is up"}
{"do":"scroll_into_view","name":"collapse all","settleMs":600,"comment":"the rows must be ON SCREEN, not merely laid out: the etable emits only its visible range, and a pointer press is a position"}

{"do":"note","text":"--- baseline: fold everything, so exactly one row and one disclosure control remain ---"}
{"do":"click","name":"collapse all"}
{"do":"wait","valueContains":", 1 rows on screen","role":"label","comment":"CollapseAll left only the root"}

{"do":"note","text":"--- click-to-expand: the disclosure control, not the row ---"}
{"do":"click","name":"▶","comment":"the only collapsed control on screen is the root's"}
{"do":"wait","valueContains":", 4 rows on screen","role":"label","comment":"Animalia opened over its three phyla"}

{"do":"note","text":"--- click-to-select: the row, past its label, by pointer ---"}
{"do":"click","value":"Chordata","role":"label","pointer":true}
{"do":"wait","value":"selected: [Chordata]","role":"label","comment":"the click reached the row's sense region behind the label"}

{"do":"note","text":"--- the row click did NOT toggle: a row that names something selects it ---"}
{"do":"wait","valueContains":", 4 rows on screen","role":"label","comment":"still four rows, so selecting did not fold or unfold"}
{"do":"capture","text":"tree-outline","comment":"the selected row, whose outline must be closed on all four sides — the M4 defect was a missing bottom edge"}

{"do":"note","text":"--- and the state is Go's: reveal opens a buried node's ancestors and selects it ---"}
{"do":"click","name":"reveal Danaus plexippus"}
{"do":"wait","value":"selected: [Danaus plexippus]","role":"label","comment":"four ranks and a phylum away from anything that was open"}
EOF

# --- run ---------------------------------------------------------------------
log "launching the widget gallery headless on 127.0.0.1:$PORT"
env -u DISPLAY -u WAYLAND_DISPLAY \
	IMZERO2_HEADLESS_LISTEN="127.0.0.1:$PORT" \
	IMZERO2_HEADLESS_DUMP_DIR="$OUT" \
	IMZERO2_HEADLESS_DUMP_EVERY=1000000 \
	IMZERO2_HEADLESS_FPS=30 \
	IMZERO2_SCREENSHOT_SIZE="$SIZE" \
	BOXER_COMPONENT=tree-widget-scene \
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

# Wait for the carrier to accept connections: the host has a Rust client to
# spawn before it listens.
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

# Kill THIS run only, by pid: a pattern kill over the shared binary path would
# take out a concurrent session's client. The Rust client is a child of the Go
# host, so reap it explicitly.
kids=$(pgrep -P "$app_pid" 2>/dev/null)
kill "$app_pid" $kids 2>/dev/null
wait "$app_pid" 2>/dev/null
# The Rust client owns the carrier socket and outlives the Go host it is a
# child of; a re-run cannot bind until it is gone.
for _ in $(seq 1 40); do
	(exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null || break
	exec 3<&-
	sleep 0.25
done

if ((rc == 0)); then
	log "PASS — click-to-select and click-to-expand both reached the widget"
	log "       capture: $OUT/tree-outline.png — check the selected row's outline is"
	log "       closed on all four sides; a missing bottom edge is the M4 defect"
else
	log "FAIL — see $OUT/logs/drive.log and $OUT/logs/host.log"
fi
exit "$rc"
