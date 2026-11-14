#!/bin/bash
# still_flip_check.sh — run scenes/still_flip.scene.md on one headless host and
# say whether the live frames show the second pattern after the swap.
#
# Usage:
#   doc/trials/image-upload-cost/still_flip_check.sh <out-dir> <host>     host: cpu | wgpu
#
# The scene makes the host dump every thirtieth live frame. The first pattern
# is a red ramp and the second a blue one, so the mean of (blue - red) over a
# frame rises by tens of levels when the canvas shows the second pattern. The
# script prints that mean per dump and for the scene's two captures, and
# compares them with the lowest dump, which is the plateau of the first
# pattern: once after the swap with nothing else changed, and once after the
# image has been moved by a point. A frame that shows the second pattern only
# after the nudge means the host held the new pixels and did not redraw them.
# Needs python3 with Pillow.
set -euo pipefail
here=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")
root=$(cd "$here/../../.." && pwd)
out=${1:?usage: still_flip_check.sh <out-dir> <host>}
host=${2:?host}
case "$host" in
cpu) client="$root/rust/imzero2/target/headless-soft/release/imzero2" ;;
wgpu) client="$root/rust/imzero2/target/headless/release/imzero2" ;;
*) echo "unknown host $host" >&2; exit 2 ;;
esac
mkdir -p "$out"
out=$(cd "$out" && pwd)
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
"$root/scripts/dev/scene.sh" --out "$work/scene" --timeout 5m --clientBinary "$client" \
	"$here/scenes/still_flip.scene.md" >"$work/stdout" 2>&1 || { tail -30 "$work/stdout" >&2; exit 1; }
python3 -I - "$work/scene" >"$out/still-flip-$host.txt" <<'PY'
import sys, pathlib
from PIL import Image, ImageStat
def level(f):
    r, g, b = ImageStat.Stat(Image.open(f).convert("RGB")).mean
    return b - r
root = pathlib.Path(sys.argv[1])
rows = [(f.name, level(f)) for f in sorted(root.rglob("frame_*.png"))]
print("# mean(blue - red) per dumped live frame, every thirtieth frame")
for name, d in rows:
    print(f"{name}\t{d:+.2f}")
caps = {}
for f in sorted(root.rglob("after-*.png")):
    caps[f.stem] = level(f)
    print(f"capture {f.name}\t{caps[f.stem]:+.2f}")
if len(rows) < 4 or "after-flip" not in caps:
    print("verdict: too few dumps or no capture to tell")
else:
    plateau = min(d for _, d in rows)
    shown = lambda d: "shows the second pattern" if d - plateau > 10 else "still shows the first pattern"
    print(f"first pattern's plateau: {plateau:+.2f}")
    print("verdict after the swap, nothing else changed: the frame " + shown(caps["after-flip"]))
    print("verdict after the nudge: the frame " + shown(rows[-1][1]))
PY
cat "$out/still-flip-$host.txt"
