#!/bin/bash
# Drive an ffmpeg binary with the EXACT argv the imzero2 headless encoder emits,
# so "it built" is never mistaken for "it runs the pipeline".
#
# Checking `ffmpeg -encoders` is not enough: a build can list every encoder and
# still fail at run time on a missing input device, muxer, filter or bitstream
# filter. Each of those produces a different error and only shows up when the
# real argv runs. This replays all three shapes, per codec lane:
#
#   probe   codeclane.rs  probe_lane()      -f lavfi -i color=... -> -f null -
#   encode  encoderpipe.rs EncoderSink::spawn  BGRA rawvideo pipe:0 -> -f nut pipe:1
#   dump    encoderpipe.rs EncoderTarget::File -> -f h264 (IMZERO2_HEADLESS_H264_OUT)
#
# Lane arguments are transcribed from CodecLane::software(); GOP is
# PERIODIC_IDR_GOP. Keep them in step when that changes.
#
# usage: verify-ffmpeg-lanes.sh [/path/to/ffmpeg]     (default: ffmpeg on PATH,
#        or $IMZERO2_FFMPEG_BIN when set)
set -o pipefail

ff=${1:-${IMZERO2_FFMPEG_BIN:-ffmpeg}}
command -v "$ff" >/dev/null 2>&1 || [ -x "$ff" ] || {
    echo "error: no such ffmpeg: $ff" >&2
    exit 2
}

W=320
H=240
FPS=30
FRAMES=10
GOP=120
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
pass=0
fail=0

# name | encoder args | bsf ("" = none)
lanes=(
    "av1|-c:v libsvtav1 -preset 8 -g $GOP -pix_fmt yuv420p|"
    "av1-444|-c:v libaom-av1 -usage realtime -cpu-used 8 -g $GOP -pix_fmt yuv444p|"
    "vp9|-c:v libvpx-vp9 -deadline realtime -cpu-used 8 -b:v 6M -g $GOP -pix_fmt yuv420p|"
    "h264|-c:v libopenh264 -rc_mode off -bf 0 -g $GOP -pix_fmt yuv420p|dump_extra=freq=keyframe"
)

report() { # <lane> <shape> <rc> <detail>
    if [ "$3" = 0 ]; then
        pass=$((pass + 1))
        printf '  PASS  %-8s %-7s %s\n' "$1" "$2" "$4"
    else
        fail=$((fail + 1))
        printf '  FAIL  %-8s %-7s %s\n' "$1" "$2" "$4"
    fi
}

echo "== $ff"
"$ff" -hide_banner -version 2>/dev/null | head -1
printf '  %s\n\n' "$(file -b "$(command -v "$ff" || echo "$ff")" 2>/dev/null | cut -c1-64)"

echo "-- probe_lane (codeclane.rs)"
for spec in "${lanes[@]}"; do
    IFS='|' read -r name args bsf <<<"$spec"
    bsfarg=()
    [ -n "$bsf" ] && bsfarg=(-bsf:v "$bsf")
    # shellcheck disable=SC2086
    err=$("$ff" -hide_banner -loglevel error \
        -f lavfi -i "color=c=black:s=256x256:r=30" -frames:v 2 \
        $args "${bsfarg[@]}" -f null - 2>&1)
    report "$name" probe $? "$(echo "$err" | grep -v '^Svt\[' | head -1 | cut -c1-58)"
done

echo
echo "-- encoder spawn (encoderpipe.rs) BGRA -> NUT"
for spec in "${lanes[@]}"; do
    IFS='|' read -r name args bsf <<<"$spec"
    bsfarg=()
    [ -n "$bsf" ] && bsfarg=(-bsf:v "$bsf")
    # shellcheck disable=SC2086
    dd if=/dev/urandom bs=$((W * H * 4)) count=$FRAMES 2>/dev/null |
        "$ff" -hide_banner -loglevel error \
            -f rawvideo -pix_fmt bgra -video_size ${W}x${H} -framerate $FPS -i pipe:0 \
            $args "${bsfarg[@]}" -flush_packets 1 -f nut pipe:1 \
            >"$tmp/$name.nut" 2>"$tmp/$name.err"
    rc=$?
    sz=$(stat -c %s "$tmp/$name.nut" 2>/dev/null || echo 0)
    # A NUT stream opens with the ID string "nut/multimedia container\0".
    magic=$(head -c 4 "$tmp/$name.nut" 2>/dev/null | od -An -c | tr -d ' \n')
    if [ "$rc" = 0 ] && [ "$sz" -gt 0 ] && [ "$magic" = "nut/" ]; then
        report "$name" encode 0 "$sz bytes, NUT id string ok"
    else
        report "$name" encode 1 "rc=$rc sz=$sz $(head -1 "$tmp/$name.err" | cut -c1-46)"
    fi
done

echo
echo "-- file dump target (EncoderTarget::File) Annex-B H.264"
dd if=/dev/urandom bs=$((W * H * 4)) count=$FRAMES 2>/dev/null |
    "$ff" -hide_banner -loglevel error \
        -f rawvideo -pix_fmt bgra -video_size ${W}x${H} -framerate $FPS -i pipe:0 \
        -c:v libopenh264 -rc_mode off -bf 0 -g $GOP -pix_fmt yuv420p \
        -bsf:v dump_extra=freq=keyframe -f h264 pipe:1 \
        >"$tmp/out.h264" 2>"$tmp/h264dump.err"
rc=$?
report h264 annexb $rc "$(stat -c %s "$tmp/out.h264" 2>/dev/null || echo 0) bytes"

echo
if [ "$fail" = 0 ]; then
    echo "all $pass checks passed"
    exit 0
fi
echo "$pass passed, $fail FAILED"
echo
echo "A failing h264 lane on an AV1+VP9-only build is expected: CodecLane::best"
echo "resolves H.264 to the encoderless mesh lane there. Any other failure means"
echo "this ffmpeg is missing a component the headless encoder needs."
exit 1
