#!/bin/bash
# Measure encode throughput of the imzero2 software lanes on this host, through
# the real encoder path: BGRA frames -> -f rawvideo pipe:0 -> lane args -> NUT.
#
# WHY: on a host with no usable hardware encoder — which includes every server
# CPU without an iGPU, and every fully static ffmpeg (it cannot dlopen a VAAPI
# or NVENC driver) — the software lanes are the whole story, and "which codec
# should this deployment use" becomes a throughput question. The lanes differ by
# an order of magnitude: libaom 4:4:4 can sit below realtime at 1080p on
# hardware where libopenh264 has 7x headroom at 720p.
#
# Companion to verify-ffmpeg-lanes.sh, which answers "does it work" rather than
# "is it fast enough". Lane arguments are transcribed from CodecLane::software();
# keep them in step when that changes.
#
# READ THE NUMBERS WITH CARE:
#   * This is throughput, not latency. It says whether a lane can keep up, not
#     how interactive it feels.
#   * Reactive cadence (ADR-0062) means the encoder only sees CHANGED frames, so
#     sustained load in a real session is well below a full-rate measurement.
#   * Content matters more than anything else here; see --content. A GUI is
#     mostly static, so `synthetic` is a conservative floor and `flat` is an
#     optimistic ceiling. Neither is your workload; the pair brackets it.
#
# usage: bench-ffmpeg-lanes.sh [options] [/path/to/ffmpeg]
set -u

ff=${IMZERO2_FFMPEG_BIN:-ffmpeg}
sizes="1280x720,1920x1080"
frames=150
target_fps=60
content=auto
generator=""
generator_explicit=0

usage() {
    cat <<EOF
usage: $(basename "$0") [options] [/path/to/ffmpeg]

  --sizes A,B      resolutions to test (default: $sizes)
  --frames N       frames per measurement (default: $frames)
  --target-fps N   realtime reference for the multiplier (default: $target_fps)
  --content MODE   synthetic | flat | noise | auto   (default: auto)
                     synthetic  moving detail from testsrc2 -- needs a
                                full-featured generator ffmpeg; conservative
                     flat       solid black (zeroed BGRA); trivially
                                compressible, so an optimistic ceiling. Needs
                                no ffmpeg to generate, so it always works
                     noise      /dev/urandom; incompressible, a pessimistic
                                floor no real GUI will ever reach
                     auto       synthetic when a generator is available, else
                                flat with a warning
  --generator PATH full-featured ffmpeg used to synthesize source frames.
                   The binary under test usually CANNOT do this: a trimmed
                   build has no testsrc2 filter.
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
    --sizes) sizes="$2"; shift 2 ;;
    --frames) frames="$2"; shift 2 ;;
    --target-fps) target_fps="$2"; shift 2 ;;
    --content) content="$2"; shift 2 ;;
    --generator) generator="$2"; generator_explicit=1; shift 2 ;;
    -h | --help) usage; exit 0 ;;
    -*) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
    *) ff="$1"; shift ;;
    esac
done

command -v "$ff" >/dev/null 2>&1 || [ -x "$ff" ] || {
    echo "error: no such ffmpeg: $ff" >&2
    exit 2
}

# A generator needs the lavfi input device, the testsrc2 source and a rawvideo
# muxer. The binary under test is deliberately trimmed and generally has none of
# testsrc2, so default to a system ffmpeg rather than to itself.
have_generator() {
    local g=$1
    [ -n "$g" ] || return 1
    command -v "$g" >/dev/null 2>&1 || [ -x "$g" ] || return 1
    "$g" -hide_banner -loglevel error -f lavfi -i "testsrc2=size=64x64:rate=1" \
        -frames:v 1 -pix_fmt bgra -f rawvideo - >/dev/null 2>&1
}

if [ "$content" = auto ] || [ "$content" = synthetic ]; then
    if [ "$generator_explicit" = 1 ]; then
        # An explicit --generator is honoured strictly: silently substituting a
        # different binary would report timings for content the caller did not
        # ask for, and hide a typo'd path entirely.
        have_generator "$generator" ||
            { echo "error: --generator $generator cannot produce testsrc2 rawvideo" >&2; exit 2; }
    else
        for cand in ffmpeg /usr/bin/ffmpeg; do
            if have_generator "$cand"; then generator=$cand; break; fi
            generator=""
        done
    fi
    if [ -n "$generator" ]; then
        content=synthetic
    elif [ "$content" = synthetic ]; then
        echo "error: --content synthetic needs a generator ffmpeg with testsrc2 (see --generator)" >&2
        exit 2
    else
        content=flat
        echo "note: no generator ffmpeg with testsrc2 found -- falling back to flat content."
        echo "      Flat frames are trivially compressible, so these figures are an"
        echo "      OPTIMISTIC CEILING, not a realistic workload."
        echo
    fi
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# name | encoder args | bsf ("" = none)
lanes=(
    "h264|-c:v libopenh264 -rc_mode off -bf 0 -g 120 -pix_fmt yuv420p|dump_extra=freq=keyframe"
    "vp9|-c:v libvpx-vp9 -deadline realtime -cpu-used 8 -b:v 6M -g 120 -pix_fmt yuv420p|"
    "av1|-c:v libsvtav1 -preset 8 -g 120 -pix_fmt yuv420p|"
    "av1-444|-c:v libaom-av1 -usage realtime -cpu-used 8 -g 120 -pix_fmt yuv444p|"
)

make_source() { # <size> <dest>
    local size=$1 dest=$2 w h src
    case "$content" in
    synthetic)
        "$generator" -hide_banner -loglevel error \
            -f lavfi -i "testsrc2=size=$size:rate=$target_fps" \
            -frames:v "$frames" -pix_fmt bgra -f rawvideo "$dest" || return 1
        ;;
    flat | noise)
        # Both synthesized without any ffmpeg, which matters on the target this
        # tool exists for: a trimmed build enables the rawvideo DEMUXER but not
        # the muxer, so the binary under test cannot write raw frames even
        # though it reads them. Zeroed BGRA is a solid black frame.
        w=${size%x*}; h=${size#*x}
        src=/dev/zero
        [ "$content" = noise ] && src=/dev/urandom
        dd if=$src of="$dest" bs=$((w * h * 4)) count="$frames" 2>/dev/null || return 1
        ;;
    *) echo "unknown --content: $content" >&2; exit 2 ;;
    esac
}

bench_one() { # <label> <size> <raw> <args...>
    local label=$1 size=$2 raw=$3
    shift 3
    local t0 t1 rc
    # Mux to NUT on stdout, exactly like EncoderTarget::Channel, and discard the
    # bytes. NEVER name /dev/null as the output FILE -- ffmpeg sees an existing
    # file and stops at an interactive overwrite prompt, so the measurement
    # times a prompt instead of an encode and reports an absurd frame rate.
    t0=$(date +%s.%N)
    "$ff" -nostdin -hide_banner -loglevel error \
        -f rawvideo -pix_fmt bgra -video_size "$size" -framerate "$target_fps" -i "$raw" \
        "$@" -f nut pipe:1 >/dev/null 2>"$work/err" </dev/null
    rc=$?
    t1=$(date +%s.%N)
    # SVT-AV1 prints an unconditional info banner to stderr, so "produced any
    # stderr" is not a failure signal.
    grep -v '^Svt\[' "$work/err" >"$work/err.real" 2>/dev/null || true
    if [ $rc -ne 0 ] || [ -s "$work/err.real" ]; then
        printf '  %-9s %-10s  FAILED: %s\n' "$label" "$size" \
            "$(head -1 "$work/err.real" | cut -c1-48)"
        return
    fi
    awk -v t0="$t0" -v t1="$t1" -v l="$label" -v s="$size" -v n="$frames" -v tf="$target_fps" \
        'BEGIN{
            d = t1 - t0
            if (d <= 0.01) { printf "  %-9s %-10s  SUSPECT: %.3fs -- too fast to be an encode\n", l, s, d; exit }
            printf "  %-9s %-10s %7.2fs %8.1f fps %7.1fx realtime@%d\n", l, s, d, n/d, (n/d)/tf, tf
        }'
}

echo "== $ff"
"$ff" -hide_banner -version 2>/dev/null | head -1
printf '   content=%s  frames=%s  threads=%s\n' "$content" "$frames" "$(nproc 2>/dev/null || echo '?')"
[ "$content" = synthetic ] && printf '   generator=%s\n' "$generator"
echo

IFS=',' read -ra size_list <<<"$sizes"
for size in "${size_list[@]}"; do
    raw="$work/src.bgra"
    make_source "$size" "$raw" || {
        echo "  $size: source generation FAILED"
        continue
    }
    printf -- '-- %s (%s raw)\n' "$size" "$(du -h "$raw" | cut -f1)"
    for spec in "${lanes[@]}"; do
        IFS='|' read -r name args bsf <<<"$spec"
        bsfarg=()
        [ -n "$bsf" ] && bsfarg=(-bsf:v "$bsf")
        # shellcheck disable=SC2086
        bench_one "$name" "$size" "$raw" $args "${bsfarg[@]}"
    done
    rm -f "$raw"
    echo
done

cat <<'EOF'
Throughput only -- not latency, and not a sustained-load figure (reactive
cadence means the encoder sees only changed frames). A lane below 1.0x cannot
keep up with full-rate content at that resolution on this host.
EOF
