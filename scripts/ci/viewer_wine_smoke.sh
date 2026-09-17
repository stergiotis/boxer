#!/bin/bash
# ADR-0243: exercise the packaged Windows binary with all supported codecs.
# A configured Wine installation and an X11/Wayland display are prerequisites.
set -euo pipefail
repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
crate="$repo/rust/imzero2-viewer"
exe=${1:-"$crate/target/windows-portable/imzero2-viewer.exe"}
wine=${WINE:-wine}
for tool in "$wine" ffmpeg go cargo timeout; do command -v "$tool" >/dev/null || { printf 'missing prerequisite: %s\n' "$tool" >&2; exit 1; }; done
[[ -f "$exe" ]] || { printf 'missing Windows executable: %s\n' "$exe" >&2; exit 1; }
cd "$crate"
source "$repo/scripts/dev/rust-repro-env.sh"
cargo build --locked --example fixture_server
out="$crate/target/wine-smoke"
mkdir -p "$out"
# The container unwrapping and the capture check are `boxer dev viewer-fixture`,
# built once here: the capture check runs per codec, and a link per call would
# cost more than the wine runs it guards.
boxer="$out/boxer"
(
    cd "$repo"
    source scripts/dev/go-build-env.sh
    # shellcheck disable=SC2086 # deliberate word splitting of the flag list
    go build $BOXER_GO_FLAGS -tags "$BOXER_GO_TAGS" -o "$boxer" ./public/app
)
ffmpeg -v error -f lavfi -i testsrc2=size=320x180:rate=20 -frames:v 1 -pix_fmt yuv420p -c:v libx264 -preset ultrafast -tune zerolatency -f h264 -y "$out/h264.au"
ffmpeg -v error -f lavfi -i testsrc2=size=320x180:rate=20 -frames:v 1 -pix_fmt yuv420p -c:v libvpx-vp9 -deadline realtime -f ivf -y "$out/vp9.ivf"
ffmpeg -v error -f lavfi -i testsrc2=size=320x180:rate=20 -frames:v 1 -pix_fmt yuv420p -c:v libsvtav1 -preset 12 -f ivf -y "$out/av1.ivf"
# ffmpeg writes VP9 and AV1 into IVF; the fixture server ships a bare bitstream.
for codec in vp9 av1; do "$boxer" dev viewer-fixture ivf-extract "$out/$codec.ivf" "$out/$codec.au"; done
peer=''
cleanup() { [[ -z "$peer" ]] || kill "$peer" 2>/dev/null || true; }
trap cleanup EXIT
port=19431
for codec in h264 vp9 av1; do
    "$crate/target/debug/examples/fixture_server" "$port" "$codec" "$out/$codec.au" >"$out/$codec-peer.log" 2>&1 &
    peer=$!
    # Small bounded wait for the real listener, rather than racing the GUI startup.
    for attempt in {1..100}; do
        grep -q '^listening' "$out/$codec-peer.log" && break
        kill -0 "$peer" 2>/dev/null || { cat "$out/$codec-peer.log"; exit 1; }
        sleep 0.05
    done
    timeout 30s "$wine" "$exe" --url "ws://127.0.0.1:$port" --software \
        --exit-after-frames 10 --capture "Z:$out/$codec.bmp" --timeout 20 >"$out/$codec-viewer.log" 2>&1
    wait "$peer"
    peer=''
    # A viewer that decoded nothing still writes a valid BMP, of one flat
    # colour, and no exit status reports that — so the capture is inspected.
    "$boxer" dev viewer-fixture bmp-check "$out/$codec.bmp"
    printf '%s: decoded capture passed\n' "$codec"
    port=$((port+1))
done
