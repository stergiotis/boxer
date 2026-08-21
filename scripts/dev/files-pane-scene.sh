#!/bin/bash
# files-pane-scene.sh — assert the Files pane's tree, headless.
#
# ADR-0200's play panel interns a result into a file system and hands it to the
# browser widget. The interning is table-tested and the widget has its own scene
# (fsbrowser-widget-scene.sh); what neither sees is the join — a panel that
# builds a correct tree and then draws nothing, or draws it against the wrong
# ids, looks identical to both.
#
# It runs through the HEADLESS host (ADR-0024) with no compositor anywhere, and
# `imzero2 drive` replays against the accessibility tree (ADR-0154). play is the
# subject, seeded through its scripted-launch knobs: BOXER_PLAY_SQL puts the
# query in the buffer, BOXER_PLAY_AUTORUN runs it on mount and
# BOXER_PLAY_FOCUS_FILES opens the tab.
#
# UNLIKE ITS SIBLING SCENES THIS ONE NEEDS A SERVER. A result panel has nothing
# to draw without a result, and play's results come from ClickHouse. The
# `synthetic` scene needs no TABLES — its rows are literals, so any reachable
# server runs it — while `lading` reads whatever mounts the store holds and is
# skipped when it holds none.
#
# Usage:
#   scripts/dev/files-pane-scene.sh
#   scripts/dev/files-pane-scene.sh synthetic     # one scene by name
#   FILESCENE_DRY=1 scripts/dev/files-pane-scene.sh    # resolve anchors only
#   FILESCENE_BUILD=0 scripts/dev/files-pane-scene.sh  # reuse rust/imzero2/ binaries
#
# Exit status is the assertion: a `wait` that never resolves fails the run.
set -uo pipefail

here=$(dirname "$(readlink -f "$BASH_SOURCE")")
root=$(cd "$here/../.." && pwd)

OUT="${FILESCENE_OUT:-$root/tmp/files-scene}"
SIZE="${FILESCENE_SIZE:-1600x1000}"
TIMEOUT="${FILESCENE_TIMEOUT:-90}"
SETTLE_MS="${FILESCENE_SETTLE_MS:-250}"
BUILD="${FILESCENE_BUILD:-1}"
BIN="${FILESCENE_BIN:-$OUT/bin}"
DRY="${FILESCENE_DRY:-}"
PORT="${FILESCENE_PORT:-8798}"
CH_URL="${CLICKHOUSE_URL:-http://localhost:8123/}"
export IMZERO2_HEADLESS_ENCODER_ARGS="${IMZERO2_HEADLESS_ENCODER_ARGS:--c:v libopenh264 -rc_mode off -bf 0 -g 100000}"

log() { printf '%s\n' "$*" >&2; }
die() { log "files-pane-scene: $*"; exit 1; }

mkdir -p "$OUT/logs"

# --- the server --------------------------------------------------------------
# Skip rather than fail: this is a dev scene, and a contributor without a server
# should not read a red run as a broken pane.
if ! curl -fsS --max-time 5 "$CH_URL" --data-binary 'SELECT 1' >/dev/null 2>&1; then
	log "files-pane-scene: no ClickHouse at $CH_URL — skipping (set CLICKHOUSE_URL)"
	exit 0
fi

# --- binaries ----------------------------------------------------------------
# A private pair by default, for the reason the widget scene builds one: a Go
# host paired with a Rust client from a different egui2 codegen desyncs the FFFI
# wire mid-frame, and every assertion then fails for an unrelated reason.
if [[ "$BUILD" == 1 ]]; then
	mkdir -p "$BIN"
	log "building a private host into $BIN (FILESCENE_BUILD=0 to reuse rust/imzero2/)"
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
MAIN_GO="${FILESCENE_MAIN_GO:-$BIN/main_go}"
CLIENT="${FILESCENE_CLIENT:-$BIN/imzero2}"
[[ "$BUILD" == 1 ]] || { MAIN_GO="${FILESCENE_MAIN_GO:-$root/rust/imzero2/main_go}"
                         CLIENT="${FILESCENE_CLIENT:-$root/rust/imzero2/target/headless/release/imzero2}"; }
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
# `synthetic` is a fixture of literal rows, so its names are known and can be
# clicked; `lading` is the case the panel exists for and asserts only what is
# true of any store — the chrome, the status line, and a capture to look at.
scene_names=(synthetic lading)

# One row per entry, in the shape `fs()` projects: paths, a size, a directory
# flag. `src` and `src/api` are named by no row — they are the directories the
# interning has to synthesise.
scene_sql_synthetic="SELECT e.1 AS path, e.2 AS size, e.3 AS is_dir FROM (SELECT arrayJoin([('src/api/handler.go', 4096, 0), ('src/api/router.go', 2048, 0), ('src/main.go', 1024, 0), ('README.md', 512, 0)]) AS e)"
scene_trace_synthetic='
{"do":"note","text":"ADR-0200 M7 — a result with a path column, browsed"}
{"do":"sleep","settleMs":3500,"comment":"play mounts a dock, restores a layout, runs the pipeline and then the query"}
{"do":"wait","valueContains":"directories","role":"label","settleMs":600,"comment":"the status line: the interning happened and says what it made"}
{"do":"wait","value":"src","role":"label","comment":"a directory NO row named — synthesised because rows nest under it"}
{"do":"wait","value":"README.md","role":"label","comment":"and a row that is a leaf of the root"}
{"do":"capture","text":"files-pane-list","comment":"list mode: the synthesised tree beside the query|s own columns"}
{"do":"click","value":"src","role":"label","pointer":true,"comment":"select the synthesised directory"}
{"do":"key","text":"Enter","comment":"Enter enters a directory rather than reporting it"}
{"do":"wait","value":"api","role":"label","settleMs":400,"comment":"one level down, the second synthesised directory"}
{"do":"wait","value":"main.go","role":"label","comment":"and the file beside it"}
{"do":"note","text":"--- a row-backed entry publishes the row cursor, and Detail follows it ---"}
{"do":"click","value":"main.go","role":"label","pointer":true}
{"do":"wait","value":"src/main.go","role":"label","settleMs":800,"comment":"Detail is showing the ROW behind the entry — the selection signal crossed the panels"}
{"do":"wait","valueContains":"row 3 / 4","role":"label","comment":"and it is the right row: main.go is the third of the four the query returned"}
{"do":"note","text":"--- the outline draws the same tree ---"}
{"do":"click","contains":"Outline","role":"button"}
{"do":"wait","value":"main.go","role":"label","settleMs":600}
{"do":"capture","text":"files-pane-outline","comment":"outline mode over the same interned tree"}
'

# The case the panel exists for: the store|s own projection, every visible
# mount|s newest snapshot. Nothing here is asserted by name — the anchors are
# the pane|s own chrome — because what the store holds is the operator|s.
scene_sql_lading="SELECT path, size, mtime, is_dir, content_hash, text FROM fs('*') ORDER BY path LIMIT 500"
scene_trace_lading='
{"do":"note","text":"ADR-0200 M7 — a lading snapshot browsed inside play"}
{"do":"sleep","settleMs":4000,"comment":"as above, plus the macro expansion and a read of the store"}
{"do":"wait","valueContains":"directories","role":"label","settleMs":800,"comment":"the status line, so the interning ran over real entries"}
{"do":"wait","contains":"Hidden names","settleMs":400,"comment":"the pane|s own strip"}
{"do":"capture","text":"files-pane-lading","comment":"a snapshot, browsed from a query"}
'

run_scene() {
	local name="$1"
	local sql trace
	sql="$(eval "printf '%s' \"\$scene_sql_$name\"")"
	trace="$OUT/logs/$name.jsonl"
	# The traces are single-quoted shell strings, so an apostrophe inside a
	# comment is written `|` and restored here rather than escaped six ways.
	eval "printf '%s' \"\$scene_trace_$name\"" | tr '|' "'" | grep -v '^$' >"$trace"

	log "--- scene $name ---"
	env -u DISPLAY -u WAYLAND_DISPLAY \
		IMZERO2_HEADLESS_LISTEN="127.0.0.1:$PORT" \
		IMZERO2_HEADLESS_DUMP_DIR="$OUT" \
		IMZERO2_HEADLESS_DUMP_EVERY=1000000 \
		IMZERO2_HEADLESS_FPS=30 \
		IMZERO2_SCREENSHOT_SIZE="$SIZE" \
		CLICKHOUSE_URL="$CH_URL" \
		BOXER_COMPONENT=files-pane-scene \
		BOXER_PLAY_WINDOW_SIZE="${W}x${H}" \
		BOXER_PLAY_SQL="$sql" \
		BOXER_PLAY_AUTORUN=1 \
		BOXER_PLAY_FOCUS_FILES=1 \
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

	timeout "$TIMEOUT" "$MAIN_GO" --logFormat=console --logLevel=info \
		imzero2 drive --url "ws://127.0.0.1:$PORT/" --trace "$trace" \
		--settle "$SETTLE_MS" ${DRY:+--dryRun} \
		>"$OUT/logs/$name.drive.log" 2>&1
	local rc=$?

	# Kill THIS run only, by pid — the box routinely runs concurrent sessions.
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

# The lading scene needs mounts; without them fs('*') is an empty result and the
# pane would be asserting against a tree with nothing in it.
lading_has_mounts() {
	local n
	n=$(curl -fsS --max-time 5 "$CH_URL" --data-binary \
		'SELECT count() FROM boxer.fssnap' 2>/dev/null) || return 1
	[[ -n "$n" && "$n" != 0 ]]
}

want=("${scene_names[@]}")
(($# > 0)) && want=("$@")

rc=0
for name in "${want[@]}"; do
	if [[ "$name" == lading ]] && ! lading_has_mounts; then
		log "--- scene lading --- SKIP (the store holds no snapshot)"
		continue
	fi
	run_scene "$name" || rc=1
done

if ((rc == 0)); then
	log "PASS — captures under $OUT"
fi
exit "$rc"
