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
# cannot capture it.
#
# Usage:
#   scripts/dev/tally-audio-scene.sh --fixture   # build the mount, then drive
#   scripts/dev/tally-audio-scene.sh             # drive against an existing one
#
# The drive itself is apps/tally/scenes/tally-audio.scene.md, run by
# scripts/dev/scene.sh (ADR-0248). What stays here is what a scene document
# cannot hold: generating the fixture, which mutates the store, and the check
# on the staging directory afterwards, which is not in the accessibility tree.
#
# Knobs: TALLYAUDIO_OUT, TALLYAUDIO_BIN, TALLYAUDIO_MOUNT_ID.
set -uo pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
root=$(cd "$here/../.." && pwd)
OUT="${TALLYAUDIO_OUT:-$root/tmp/tally-audio-scene}"
BIN="${TALLYAUDIO_BIN:-$OUT/bin}"
MOUNT=tally-audio # the scene document anchors on this name
MOUNT_ID="${TALLYAUDIO_MOUNT_ID:-0x3BFE363BCF148011}"
FIXTURE=0
[[ "${1:-}" == "--fixture" ]] && FIXTURE=1
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

# The staging directory is this script's, so it can say afterwards that it is
# empty. The runner passes the ambient environment on to the host.
staging="$OUT/adhoc"
rm -rf "$staging"
export BOXER_ADHOC_DIR="$staging"

"$here/scene.sh" --out "$OUT" "$root/apps/tally/scenes/tally-audio.scene.md"
rc=$?
((rc == 0)) || { log "FAIL — see $OUT/index.md and $OUT/logs/"; exit "$rc"; }

# Whatever staging wrote must be gone: a released recording takes its sealed
# file with it, and the ffmpeg shape never had one.
left=$(find "$staging" -type f 2>/dev/null | wc -l)
if ((left > 0)); then
	log "FAIL — $left staged file(s) left under $staging"
	find "$staging" -type f >&2
	exit 1
fi
log "PASS — both staging shapes opened, played and were released"
