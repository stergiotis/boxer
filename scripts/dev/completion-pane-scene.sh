#!/bin/bash
# completion-pane-scene.sh — assert the completion pane's answer, headless.
#
# ADR-0190's verification plan asks for one thing no unit test can reach: that
# the caret model, the domain resolution and the pane's rows survive a real
# render, end to end from the buffer a person typed. The engine is table-tested
# and the precision oracle checks the offered sets against a live server; what
# neither sees is the render — a pane that resolves the right domain and then
# draws nothing looks identical to both.
#
# It runs through the HEADLESS host (ADR-0024) with no compositor anywhere, and
# `imzero2 drive` replays against the accessibility tree (ADR-0154). play is the
# subject, seeded through its own scripted-launch knobs: BOXER_PLAY_SQL puts the
# buffer in place and BOXER_PLAY_FOCUS_COMPLETION opens the tab, so the scene
# drives no input at all and asserts only what the pane rendered.
#
# What it CANNOT assert is the highlight. The match outline is a Frame stroke
# and the editor's tint is a styled section; neither reaches the accessibility
# tree, so both are pinned in Go instead (TestCompleteDrivingCases's match
# state, TestResolvedTokenSection's styled span).
#
# Usage:
#   scripts/dev/completion-pane-scene.sh
#   scripts/dev/completion-pane-scene.sh kinds     # one scene by name
#   COMPSCENE_DRY=1 scripts/dev/completion-pane-scene.sh    # resolve anchors only
#   COMPSCENE_BUILD=0 scripts/dev/completion-pane-scene.sh  # reuse rust/imzero2/ binaries
#
# Exit status is the assertion: a `wait` that never resolves fails the run.
set -uo pipefail

here=$(dirname "$(readlink -f "$BASH_SOURCE")")
root=$(cd "$here/../.." && pwd)

OUT="${COMPSCENE_OUT:-$root/tmp/completion-scene}"
SIZE="${COMPSCENE_SIZE:-1600x1000}"
TIMEOUT="${COMPSCENE_TIMEOUT:-90}"
SETTLE_MS="${COMPSCENE_SETTLE_MS:-250}"
BUILD="${COMPSCENE_BUILD:-1}"
BIN="${COMPSCENE_BIN:-$OUT/bin}"
DRY="${COMPSCENE_DRY:-}"
PORT="${COMPSCENE_PORT:-8797}"
export IMZERO2_HEADLESS_ENCODER_ARGS="${IMZERO2_HEADLESS_ENCODER_ARGS:--c:v libopenh264 -rc_mode off -bf 0 -g 100000}"

log() { printf '%s\n' "$*" >&2; }
die() { log "completion-pane-scene: $*"; exit 1; }

mkdir -p "$OUT/logs"

# --- binaries ----------------------------------------------------------------
# A private pair by default, for the reason the tree scene builds one: a Go host
# paired with a Rust client from a different egui2 codegen desyncs the FFFI wire
# mid-frame, and every assertion then fails for a reason that has nothing to do
# with the pane.
if [[ "$BUILD" == 1 ]]; then
	mkdir -p "$BIN"
	log "building a private host into $BIN (COMPSCENE_BUILD=0 to reuse rust/imzero2/)"
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
MAIN_GO="${COMPSCENE_MAIN_GO:-$BIN/main_go}"
CLIENT="${COMPSCENE_CLIENT:-$BIN/imzero2}"
[[ "$BUILD" == 1 ]] || { MAIN_GO="${COMPSCENE_MAIN_GO:-$root/rust/imzero2/main_go}"
                         CLIENT="${COMPSCENE_CLIENT:-$root/rust/imzero2/target/headless/release/imzero2}"; }
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

# --- the scenes --------------------------------------------------------------
# Each is a buffer and a trace. The caret sits at the END of a seeded buffer —
# an editor that was never focused reports (end, end) — which is exactly the
# state the driving cases are written in.
#
# Anchors are against role "label": egui puts a Label's text in the accessible
# VALUE and leaves the name empty, and the pane's cells are all labels. Two
# things about the anchors are load-bearing:
#
#   * `role` is always pinned, because egui emits a `text_run` child under some
#     labels carrying the same text — a bare value anchor resolves on one and
#     reports an ambiguity on the next.
#   * row anchors are EXACT (`value`), not `valueContains`. The domains are full
#     of superstrings — SysCpu inside SysCpuInfo, TotalBytes inside
#     SwapTotalBytes — and a substring anchor matches both, which is an
#     ambiguity rather than a hit.
scene_names=(kinds fields silence)

scene_sql_kinds="SELECT LW_COMPONENT('Sys"
scene_trace_kinds='
{"do":"note","text":"ADR-0190 M1 — the component-kind domain, rendered"}
{"do":"sleep","settleMs":2500,"comment":"play mounts a dock, restores a layout and runs its pass pipeline before the pane has anything to draw"}
{"do":"wait","valueContains":"component kind","role":"label","settleMs":600,"comment":"the heading names the position: which call, which argument, which domain"}
{"do":"wait","value":"SysMem","role":"label","comment":"a registered kind reached a row"}
{"do":"wait","value":"SysCpu","role":"label","comment":"and so did a second one — the pane shows the whole domain, not only the row the caret matches"}
{"do":"wait","value":"components","role":"label","nth":0,"comment":"the provenance column: SD1 asks every candidate to carry where it came from"}
'

scene_sql_fields="SELECT tupleElement(LW_COMPONENT('SysMem'), 'Tot"
scene_trace_fields='
{"do":"note","text":"ADR-0190 M1 — the field domain, decided by the sibling argument"}
{"do":"sleep","settleMs":2500,"comment":"as above"}
{"do":"wait","valueContains":"tuple element","role":"label","settleMs":600,"comment":"the heading says the domain came from the tuple, not from the clause"}
{"do":"wait","value":"TotalBytes","role":"label","comment":"a field of the kind named beside it"}
{"do":"wait","value":"DateTime64(9, \u0027UTC\u0027)","role":"label","comment":"the type column: the elements carry their own types, which is what tells two same-named fields apart"}
'

scene_sql_silence="SELECT LW_GET('"
scene_trace_silence='
{"do":"note","text":"ADR-0190 SD1 — a silence carries its reason, never an empty table"}
{"do":"sleep","settleMs":2500,"comment":"as above"}
{"do":"wait","value":"LW_GET argument 1 — leeway section","role":"label","settleMs":600,"comment":"the position still resolves to a domain, named in the heading"}
{"do":"wait","value":"nothing here answers leeway section","role":"label","comment":"and the pane says why it cannot fill it, rather than showing an empty table"}
'

run_scene() {
	local name="$1"
	local sql trace
	sql="$(eval "printf '%s' \"\$scene_sql_$name\"")"
	trace="$OUT/logs/$name.jsonl"
	eval "printf '%s' \"\$scene_trace_$name\"" | grep -v '^$' >"$trace"

	log "--- scene $name ---"
	env -u DISPLAY -u WAYLAND_DISPLAY \
		IMZERO2_HEADLESS_LISTEN="127.0.0.1:$PORT" \
		IMZERO2_HEADLESS_DUMP_DIR="$OUT" \
		IMZERO2_HEADLESS_DUMP_EVERY=1000000 \
		IMZERO2_HEADLESS_FPS=30 \
		IMZERO2_SCREENSHOT_SIZE="$SIZE" \
		BOXER_COMPONENT=completion-pane-scene \
		BOXER_PLAY_WINDOW_SIZE="${W}x${H}" \
		BOXER_PLAY_SQL="$sql" \
		BOXER_PLAY_FOCUS_COMPLETION=1 \
		"$MAIN_GO" --logFormat=console --logLevel=warn \
			imzero2 demo \
			--clientBinary "$CLIENT" \
			--clientInitialMainWindowWidth "$W" \
			--clientInitialMainWindowHeight "$H" \
			${MAIN_FONT:+--mainFontTTF "$MAIN_FONT"} \
			${MONO_FONT:+--monoFontTTF "$MONO_FONT"} \
			${PHOSPHOR_FONT:+--phosphorFontTTF "$PHOSPHOR_FONT"} \
			${FALLBACK_FONT:+--fallbackFontTTF "$FALLBACK_FONT"} \
			--launch play \
		>"$OUT/logs/$name.log" 2>&1 &
	local app_pid=$!

	for _ in $(seq 1 $((TIMEOUT * 4))); do
		(exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null && { exec 3<&-; break; }
		kill -0 "$app_pid" 2>/dev/null || break
		sleep 0.25
	done

	# The drive log is its own file: the host's is a wall of Rust frame logs,
	# and a failed anchor is one line inside it.
	timeout "$TIMEOUT" "$MAIN_GO" --logFormat=console --logLevel=info \
		imzero2 drive --url "ws://127.0.0.1:$PORT/" --trace "$trace" \
		--settle "$SETTLE_MS" ${DRY:+--dryRun} \
		>"$OUT/logs/$name.drive.log" 2>&1
	local rc=$?

	# Kill THIS run only, by pid. The Rust client is a child of the Go host and
	# owns the carrier socket, so it is reaped explicitly and waited out — a
	# re-run cannot bind the port until it is gone.
	local kids
	kids=$(pgrep -P "$app_pid" 2>/dev/null)
	kill "$app_pid" $kids 2>/dev/null
	wait "$app_pid" 2>/dev/null
	for _ in $(seq 1 40); do
		(exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null || break
		exec 3<&-
		sleep 0.25
	done

	if ((rc == 0)); then
		log "  PASS $name"
	else
		log "  FAIL $name — see $OUT/logs/$name.drive.log (and $name.log for the host)"
	fi
	return "$rc"
}

wanted=("$@")
failed=0
for name in "${scene_names[@]}"; do
	if ((${#wanted[@]})); then
		local_match=0
		for w in "${wanted[@]}"; do [[ "$name" == *"$w"* ]] && local_match=1; done
		((local_match)) || continue
	fi
	run_scene "$name" || failed=1
done

if ((failed == 0)); then
	log "PASS — the pane rendered the domain, the candidates and the reason"
else
	log "FAIL — see $OUT/logs/"
fi
exit "$failed"
