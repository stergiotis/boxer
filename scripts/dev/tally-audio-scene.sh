#!/bin/bash
# tally-audio-scene.sh — drive tally's audio preview headless against the local store.
#
# ADR-0200's audio preview over ADR-0208's player: select a recording in a
# snapshot and get a waveform and a transport. The two staging shapes are the
# point of the scene, so it asserts both — a compressed file goes to ffmpeg
# through an inherited descriptor onto anonymous memory, a WAV is sealed into
# the ad-hoc dataset store and read back in-process — and then that browsing
# away releases whichever was open.
#
# It needs a reachable ClickHouse holding a mount whose newest snapshot has
# the recordings the trace names; `--fixture` makes one. The readouts are what
# is asserted: the waveform itself is painter output, and the headless client
# cannot capture it (the same gap scripts/dev/waveform-scene.sh has).
#
# Usage:
#   scripts/dev/tally-audio-scene.sh --fixture   # build the mount, then drive
#   scripts/dev/tally-audio-scene.sh             # drive against an existing one
#
# Knobs: TALLYAUDIO_OUT, TALLYAUDIO_SIZE, TALLYAUDIO_TIMEOUT, TALLYAUDIO_BUILD=0,
# TALLYAUDIO_BIN, TALLYAUDIO_PORT, TALLYAUDIO_MOUNT, TALLYAUDIO_MOUNT_ID.
set -uo pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
root=$(cd "$here/../.." && pwd)
OUT="${TALLYAUDIO_OUT:-$root/tmp/tally-audio-scene}"
SIZE="${TALLYAUDIO_SIZE:-1400x1000}"
TIMEOUT="${TALLYAUDIO_TIMEOUT:-180}"
BUILD="${TALLYAUDIO_BUILD:-1}"
BIN="${TALLYAUDIO_BIN:-$OUT/bin}"
PORT="${TALLYAUDIO_PORT:-8799}"
MOUNT="${TALLYAUDIO_MOUNT:-tally-audio}"
MOUNT_ID="${TALLYAUDIO_MOUNT_ID:-0x3BFE363BCF148011}"
FIXTURE=0
[[ "${1:-}" == "--fixture" ]] && FIXTURE=1
export IMZERO2_HEADLESS_ENCODER_ARGS="${IMZERO2_HEADLESS_ENCODER_ARGS:--c:v libopenh264 -rc_mode off -bf 0 -g 100000}"
log() { printf '%s\n' "$*" >&2; }
die() { log "tally-audio-scene: $*"; exit 1; }
mkdir -p "$OUT/logs" "$BIN"

if ((FIXTURE)); then
	command -v ffmpeg >/dev/null || die "--fixture needs ffmpeg to synthesise the recordings"
	tree="$OUT/tree"
	mkdir -p "$tree"
	log "synthesising three recordings of one signal into $tree"
	ffmpeg -v error -y -f lavfi -i "sine=frequency=440:duration=18:sample_rate=48000" \
		-af "aformat=channel_layouts=stereo,tremolo=f=0.6:d=0.9" \
		-c:a pcm_s16le "$tree/interview-take-1.wav" || die "ffmpeg failed"
	ffmpeg -v error -y -i "$tree/interview-take-1.wav" -c:a flac "$tree/interview-take-2.flac" || die "ffmpeg failed"
	ffmpeg -v error -y -i "$tree/interview-take-1.wav" -c:a libmp3lame -q:a 4 "$tree/room-tone.mp3" || die "ffmpeg failed"
	printf 'Two takes and a room tone.\n' >"$tree/notes.md"
	log "building the CLI and snapshotting them as mount $MOUNT_ID ($MOUNT)"
	( cd "$root" && go build -tags "$(tr -d '\n' <./tags)" -o "$BIN/boxer" ./public/app ) || die "go build failed"
	"$BIN/boxer" --logFormat=console --logLevel=warn fs snapshot \
		--mount "$MOUNT_ID" --name "$MOUNT" --ttl-days 7 "$tree" || die "snapshot failed"
fi

if [[ "$BUILD" == 1 ]]; then
	log "building a private host into $BIN (TALLYAUDIO_BUILD=0 to reuse)"
	( cd "$root" && CGO_ENABLED=0 go build -tags "$(tr -d '\n' <./tags),binary_log" \
		-o "$BIN/main_go" ./public/thestack/cmd/imzero2/ ) || die "go build failed"
	[[ -x "$root/rust/imzero2/target/headless/release/imzero2" ]] ||
		die "no headless client — run rust/imzero2/build_rust_headless.sh"
	cp -fp "$root/rust/imzero2/target/headless/release/imzero2" "$BIN/imzero2" ||
		die "cannot copy the headless client"
fi
MAIN_GO="$BIN/main_go"
CLIENT="$BIN/imzero2"
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
trace="$OUT/logs/tally-audio-scene.jsonl"
cat >"$trace" <<EOF
{"do":"note","text":"tally's audio preview — a recording read out of a lading snapshot"}
{"do":"wait","value":"Mounts","role":"label","settleMs":500}
{"do":"wait","contains":"$MOUNT  ·","role":"button","settleMs":1000}
{"do":"click","name":"A","role":"button","settleMs":200}
{"do":"click","contains":"$MOUNT  ·","role":"button","settleMs":800}
{"do":"wait","name":"Follow latest","settleMs":600}
{"do":"note","text":"--- a flac: staged into anonymous memory, probed and decoded by ffmpeg over inherited descriptors ---"}
{"do":"click","value":"interview-take-2.flac","role":"label","pointer":true,"nth":0,"settleMs":600}
{"do":"wait","valueContains":"interview-take-2.flac  ·","role":"label","settleMs":10000,"comment":"the preview header; staging reads the whole recording out of the store"}
{"do":"wait","contains":"Play","role":"button","settleMs":8000,"comment":"the transport is up, so the track opened"}
{"do":"wait","valueContains":"2 ch · 48000 Hz","role":"label","settleMs":8000,"comment":"ffprobe agreed with the recording"}
{"do":"wait","valueContains":"· ffmpeg","role":"label","settleMs":2000,"comment":"and it took the external decoder"}
{"do":"note","text":"--- play: the playhead moves, through the device or the silent clock ---"}
{"do":"click","contains":"Play","role":"button","settleMs":1500}
{"do":"wait","valueContains":"· playing ·","role":"label","settleMs":3000}
{"do":"click","contains":"Pause","role":"button","settleMs":500}
{"do":"wait","valueContains":"· paused ·","role":"label","settleMs":3000}
{"do":"note","text":"--- the WAV: sealed into the ad-hoc dataset store, decoded in-process ---"}
{"do":"click","value":"interview-take-1.wav","role":"label","pointer":true,"nth":0,"settleMs":800}
{"do":"wait","valueContains":"interview-take-1.wav  ·","role":"label","settleMs":10000}
{"do":"wait","valueContains":"· wav","role":"label","settleMs":8000,"comment":"the native reader over the sealed staging file"}
{"do":"wait","valueContains":"2 ch · 48000 Hz","role":"label","settleMs":4000}
{"do":"click","name":"Fit","role":"button","settleMs":400}
{"do":"note","text":"--- browsing away releases the recording: the lane owns it ---"}
{"do":"click","value":"notes.md","role":"label","pointer":true,"nth":0,"settleMs":1200}
{"do":"wait","valueContains":"notes.md  ·","role":"label","settleMs":4000}
EOF
staging="$OUT/adhoc"
rm -rf "$staging"
log "launching tally headless on 127.0.0.1:$PORT"
env -u DISPLAY -u WAYLAND_DISPLAY \
	IMZERO2_HEADLESS_LISTEN="127.0.0.1:$PORT" \
	IMZERO2_HEADLESS_DUMP_DIR="$OUT" \
	IMZERO2_HEADLESS_DUMP_EVERY=1000000 \
	IMZERO2_HEADLESS_FPS=30 \
	IMZERO2_SCREENSHOT_SIZE="$SIZE" \
	BOXER_COMPONENT=tally-audio-scene \
	BOXER_ADHOC_DIR="$staging" \
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
	imzero2 drive --url "ws://127.0.0.1:$PORT/" --trace "$trace" --settle 300 \
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
# Whatever staging wrote must be gone: a released recording takes its sealed
# file with it, and an anonymous one never had a name here at all.
left=$(find "$staging" -type f 2>/dev/null | wc -l)
if ((left != 0)); then
	log "FAIL — $left staged file(s) left under $staging"
	find "$staging" -type f >&2
	rc=1
fi
if ((rc == 0)); then
	log "PASS — both staging shapes opened, played and were released"
else
	log "FAIL — see $OUT/logs/drive.log and $OUT/logs/host.log"
fi
exit "$rc"
