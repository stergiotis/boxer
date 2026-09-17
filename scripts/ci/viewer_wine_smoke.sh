#!/bin/bash
# ADR-0243: exercise the packaged Windows binary with all supported codecs.
# A configured Wine installation and an X11/Wayland display are prerequisites.
set -euo pipefail
repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
crate="$repo/rust/imzero2-viewer"
exe=${1:-"$crate/target/windows-portable/imzero2-viewer.exe"}
wine=${WINE:-wine}
for tool in "$wine" ffmpeg python3 cargo timeout; do command -v "$tool" >/dev/null || { printf 'missing prerequisite: %s\n' "$tool" >&2; exit 1; }; done
[[ -f "$exe" ]] || { printf 'missing Windows executable: %s\n' "$exe" >&2; exit 1; }
cd "$crate"
source "$repo/scripts/dev/rust-repro-env.sh"
cargo build --locked --example fixture_server
out="$crate/target/wine-smoke"
mkdir -p "$out"
ffmpeg -v error -f lavfi -i testsrc2=size=320x180:rate=20 -frames:v 1 -pix_fmt yuv420p -c:v libx264 -preset ultrafast -tune zerolatency -f h264 -y "$out/h264.au"
ffmpeg -v error -f lavfi -i testsrc2=size=320x180:rate=20 -frames:v 1 -pix_fmt yuv420p -c:v libvpx-vp9 -deadline realtime -f ivf -y "$out/vp9.ivf"
ffmpeg -v error -f lavfi -i testsrc2=size=320x180:rate=20 -frames:v 1 -pix_fmt yuv420p -c:v libsvtav1 -preset 12 -f ivf -y "$out/av1.ivf"
python3 - "$out" <<'PY'
from pathlib import Path
import sys
for codec in ('vp9','av1'):
    p=Path(sys.argv[1])/f'{codec}.ivf'
    b=p.read_bytes()
    assert b[:4]==b'DKIF' and len(b)>=44
    n=int.from_bytes(b[32:36],'little')
    assert 0<n<=len(b)-44
    p.with_suffix('.au').write_bytes(b[44:44+n])
PY
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
    python3 - "$out/$codec.bmp" <<'PY'
from pathlib import Path
import sys
b=Path(sys.argv[1]).read_bytes()
assert b[:2]==b'BM' and len(b)>54
pixels=b[int.from_bytes(b[10:14],'little'):]
assert len(set(pixels))>32, 'capture is blank or nearly uniform'
PY
    printf '%s: decoded capture passed\n' "$codec"
    port=$((port+1))
done
