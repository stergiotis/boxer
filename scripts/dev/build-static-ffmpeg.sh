#!/bin/bash
# Build a fully static, software-only ffmpeg carrying exactly the components the
# imzero2 headless encoder invokes — and nothing else.
#
# WHY: the headless pixel-streaming head shells out to `ffmpeg` (ADR-0024 SD3,
# ADR-0088 SD4). A distro ffmpeg drags in ~290 shared objects, which is a poor
# fit for a size-constrained or network-isolated deployment. This builds one
# self-contained binary (~18-21 MiB) from pinned source tarballs, with no
# runtime library dependencies at all. Point the host at it with
# IMZERO2_FFMPEG_BIN rather than shadowing the system ffmpeg on PATH.
#
# NO HARDWARE ENCODE, BY CONSTRUCTION: libva loads its driver with dlopen, and a
# statically linked binary cannot dlopen. Every VAAPI lane will therefore probe
# as NotBuilt and CodecLane::best falls back to the software lane — that path is
# supervised and tested, so this is a supported configuration, not a surprise.
# If you need hardware encode, you need a dynamically linked ffmpeg instead.
#
# OFFLINE: --fetch downloads the five tarballs; without it they must already be
# in --src-dir, so a target with no network can build from a staged source set.
# Nothing else is downloaded. Verify a result with scripts/dev/verify-ffmpeg-lanes.sh.
#
# Build-host prerequisites (system packages, all of them ordinary toolchain):
#   cc/c++, make, cmake, nasm, perl, GNU diff, the `which` BINARY, and a static
#   libc (glibc-static on RPM distros; musl hosts have it already and give the
#   smaller binary). The last two are easy to miss because a modern distro may
#   ship neither: busybox diff lacks options libvpx's configure uses, and
#   libvpx probes for its assembler with `which nasm` rather than the shell
#   builtin -- on Fedora 42, which drops the `which` package and puts nasm in
#   /usr/sbin, an otherwise complete toolchain fails with "Neither yasm nor
#   nasm have been found" while nasm is plainly installed.
#
#   The default build also needs a static C++ runtime (libstdc++-static on RPM
#   distros): openh264 is C++, its pkg-config file carries -lstdc++, and the
#   static link therefore needs libstdc++.a. Without it ffmpeg's configure
#   reports "openh264 >= 1.3.0 not found using pkg-config" -- which reads as a
#   missing openh264, though openh264 built and installed perfectly well.
#   --without-h264 drops that requirement along with the encoder.
set -e
set -o pipefail

here=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")

FFMPEG_VER=7.1.1
AOM_VER=3.12.1
SVTAV1_VER=2.3.0
VPX_VER=1.15.0
OPENH264_VER=2.6.0

src_dir="$PWD/_ffmpeg-src"
prefix=""
out=""
jobs=$(nproc 2>/dev/null || echo 4)
fetch=0
with_h264=1

usage() {
    cat <<EOF
usage: $(basename "$0") [options]

  --src-dir DIR   source tarballs live here (default: ./_ffmpeg-src)
  --prefix DIR    codec libraries are staged here (default: <src-dir>/_prefix)
  --out FILE      output binary (default: ./ffmpeg-static)
  --fetch         download the source tarballs (needs network)
  --without-h264  drop libopenh264: AV1 + VP9 only, royalty-free, ~1 MiB smaller.
                  NOTE the default IMZERO2_HEADLESS_CODEC is h264, which will
                  then resolve to the encoderless mesh lane -- set the codec
                  explicitly (av1 is the portable choice) if you use this.
  --jobs N        parallel build jobs (default: $jobs)
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
    --src-dir) src_dir=$(readlink -f "$2"); shift 2 ;;
    --prefix) prefix=$(readlink -f "$2"); shift 2 ;;
    --out) out="$2"; shift 2 ;;
    --jobs) jobs="$2"; shift 2 ;;
    --fetch) fetch=1; shift ;;
    --without-h264) with_h264=0; shift ;;
    -h | --help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
    esac
done

[ -n "$prefix" ] || prefix="$src_dir/_prefix"
[ -n "$out" ] || out="$PWD/ffmpeg-static"
log_dir="$src_dir/_logs"
mkdir -p "$src_dir" "$prefix" "$log_dir"
export PKG_CONFIG_PATH="$prefix/lib/pkgconfig"

step() { echo "== $*"; }
die() { echo "error: $*" >&2; exit 1; }

# Run a build step, keeping its log; a failure prints the tail rather than
# leaving the caller to guess which of five builds died.
run() {
    local name=$1
    shift
    if ! "$@" >>"$log_dir/$name.log" 2>&1; then
        echo "FAILED: $name (see $log_dir/$name.log)" >&2
        tail -30 "$log_dir/$name.log" >&2
        exit 1
    fi
}
have() { [ -f "$prefix/lib/$1" ]; }

step "preflight"
for t in cc make cmake nasm perl; do
    command -v "$t" >/dev/null 2>&1 || die "$t not on PATH (see the header for prerequisites)"
done
# libvpx's configure needs GNU diff; busybox diff lacks the options it uses and
# fails with a bare "Configuration failed" that names no cause.
diff --help 2>&1 | grep -q -- '--unified' || die "GNU diff required (busybox diff is not enough)"
# ...and it locates its assembler with `which nasm`, not the shell builtin. Test
# the binary the way libvpx will, or the build dies much later claiming nasm is
# absent when it is merely unreachable that way.
command -v which >/dev/null 2>&1 ||
    die "the 'which' binary is required (libvpx's configure uses it to find nasm)"
which nasm >/dev/null 2>&1 ||
    die "'which nasm' fails though nasm is on PATH -- libvpx will not find it (nasm may be in /usr/sbin; add it to PATH)"
echo 'int main(void){return 0;}' >"$src_dir/.cc-probe.c"
"${CC:-cc}" -static -o "$src_dir/.cc-probe" "$src_dir/.cc-probe.c" 2>/dev/null ||
    die "this toolchain cannot link -static (install glibc-static or build on a musl host)"
rm -f "$src_dir/.cc-probe" "$src_dir/.cc-probe.c"
if [ "$with_h264" = 1 ]; then
    # Probe the static C++ link the way ffmpeg's configure will: openh264 is
    # C++ and its .pc carries -lstdc++, so a missing libstdc++.a surfaces much
    # later as "openh264 >= 1.3.0 not found using pkg-config", blaming the
    # wrong component entirely.
    echo 'int main(void){return 0;}' >"$src_dir/.cxx-probe.cc"
    "${CXX:-c++}" -static -o "$src_dir/.cxx-probe" "$src_dir/.cxx-probe.cc" 2>/dev/null ||
        die "this toolchain cannot link C++ -static (install libstdc++-static; needed by openh264, or pass --without-h264)"
    rm -f "$src_dir/.cxx-probe" "$src_dir/.cxx-probe.cc"
fi

fetch_one() { # <url> <file>
    [ -f "$src_dir/$2" ] && return 0
    [ "$fetch" = 1 ] || die "missing $src_dir/$2 (pass --fetch, or stage the tarballs first)"
    step "fetch $2"
    curl -sSL --retry 3 -o "$src_dir/$2" "$1"
}

step "sources"
fetch_one "https://ffmpeg.org/releases/ffmpeg-${FFMPEG_VER}.tar.xz" "ffmpeg-${FFMPEG_VER}.tar.xz"
fetch_one "https://storage.googleapis.com/aom-releases/libaom-${AOM_VER}.tar.gz" "libaom-${AOM_VER}.tar.gz"
fetch_one "https://gitlab.com/AOMediaCodec/SVT-AV1/-/archive/v${SVTAV1_VER}/SVT-AV1-v${SVTAV1_VER}.tar.gz" "SVT-AV1-v${SVTAV1_VER}.tar.gz"
fetch_one "https://github.com/webmproject/libvpx/archive/refs/tags/v${VPX_VER}.tar.gz" "libvpx-${VPX_VER}.tar.gz"
fetch_one "https://github.com/cisco/openh264/archive/refs/tags/v${OPENH264_VER}.tar.gz" "openh264-${OPENH264_VER}.tar.gz"
(cd "$src_dir" && sha256sum ./*.tar.*)

cd "$src_dir"
for t in *.tar.*; do
    d=${t%.tar.*}
    [ -d "$src_dir/$d" ] || tar xf "$t"
done

# CMAKE_INSTALL_LIBDIR=lib is load-bearing: GNUInstallDirs defaults it to lib64
# on RPM distros, and ffmpeg's --pkg-config-flags=--static probe then reports
# "aom >= 2.0.0 not found" even though the archive built perfectly well.

# libaom -- the AV1 4:4:4 lane (libsvtav1 is 4:2:0 only). Encoder only.
if have libaom.a; then step "libaom cached"; else
    step "build libaom ${AOM_VER}"
    mkdir -p "$src_dir/_b/aom" && cd "$src_dir/_b/aom"
    run aom cmake "$src_dir/libaom-${AOM_VER}" \
        -DCMAKE_INSTALL_PREFIX="$prefix" -DCMAKE_INSTALL_LIBDIR=lib \
        -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=0 \
        -DENABLE_TESTS=0 -DENABLE_EXAMPLES=0 -DENABLE_TOOLS=0 -DENABLE_DOCS=0 \
        -DCONFIG_AV1_DECODER=0
    run aom make -j"$jobs" install
fi

# SVT-AV1 -- the fast 4:2:0 AV1 lane. Encoder only.
if have libSvtAv1Enc.a; then step "SVT-AV1 cached"; else
    step "build SVT-AV1 ${SVTAV1_VER}"
    mkdir -p "$src_dir/_b/svt" && cd "$src_dir/_b/svt"
    run svt cmake "$src_dir/SVT-AV1-v${SVTAV1_VER}" \
        -DCMAKE_INSTALL_PREFIX="$prefix" -DCMAKE_INSTALL_LIBDIR=lib \
        -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF \
        -DBUILD_APPS=OFF -DBUILD_TESTING=OFF -DBUILD_DEC=OFF
    run svt make -j"$jobs" install
fi

# libvpx -- the VP9 lane. Encoder only.
if have libvpx.a; then step "libvpx cached"; else
    step "build libvpx ${VPX_VER}"
    mkdir -p "$src_dir/_b/vpx" && cd "$src_dir/_b/vpx"
    run vpx "$src_dir/libvpx-${VPX_VER}/configure" --prefix="$prefix" \
        --disable-shared --enable-static --disable-vp8 --disable-vp9-decoder \
        --enable-vp9-encoder --enable-pic --disable-examples --disable-tools \
        --disable-docs --disable-unit-tests
    run vpx make -j"$jobs" install
fi

if [ "$with_h264" = 1 ]; then
    if have libopenh264.a; then step "openh264 cached"; else
        step "build openh264 ${OPENH264_VER}"
        cd "$src_dir/openh264-${OPENH264_VER}"
        run oh264 make -j"$jobs" PREFIX="$prefix" install-static
    fi
fi

# ffmpeg. --disable-everything plus exactly the components codeclane.rs and
# encoderpipe.rs name:
#   encode : -f rawvideo -pix_fmt bgra pipe:0 -> -f nut (wire) | -f h264 (dump)
#   probe  : -f lavfi -i color=... -> -f null -        (codeclane.rs probe_lane)
#   bsf    : dump_extra=freq=keyframe                  (H.264 lanes)
#
# Three selections that are not obvious, each of which produced a binary that
# built cleanly and then failed at run time:
#   * `-f lavfi` is an input DEVICE, not a demuxer. --disable-avdevice silently
#     removes the entire lane-probe path, so every lane probes as unavailable.
#   * the bitstream filter is `dump_extradata` to configure and `dump_extra` on
#     the command line. --enable-bsf=dump_extra only WARNS, then dies at encoder
#     spawn with "Bitstream filter not found".
#   * no parsers: nothing here demuxes an elementary stream, and enabling the
#     h264 parser alone breaks the static link (h2645_sei.o references
#     ff_aom_uninit_film_grain_params, which only hevc_sei selects).
step "build ffmpeg ${FFMPEG_VER}"
cd "$src_dir/ffmpeg-${FFMPEG_VER}"
conf=(
    --disable-everything
    --disable-shared --enable-static
    --pkg-config-flags=--static
    --extra-ldflags=-static
    --extra-cflags="-I$prefix/include"
    --extra-ldflags="-L$prefix/lib"
    --disable-autodetect --disable-network --disable-doc --disable-debug
    --disable-postproc
    --disable-programs --enable-ffmpeg
    --enable-swscale --enable-avfilter --enable-swresample
    --enable-decoder=rawvideo,wrapped_avframe
    --enable-demuxer=rawvideo
    --enable-muxer=nut,h264,null
    --enable-filter=scale,format,null,copy,color
    --enable-avdevice --enable-indev=lavfi
    --enable-bsf=dump_extradata
    --enable-protocol=pipe,file
    --enable-libsvtav1 --enable-libaom --enable-libvpx
    --enable-encoder=libsvtav1,libaom_av1,libvpx_vp9
)
if [ "$with_h264" = 1 ]; then
    conf+=(--enable-libopenh264 --enable-encoder=libopenh264)
fi
run ffmpeg-configure ./configure "${conf[@]}"
run ffmpeg-make make -j"$jobs"

install -m 0755 ffmpeg "$out"
strip "$out" 2>/dev/null || true

step "done"
printf '  %s\n  %s bytes\n  %s\n' \
    "$out" "$(stat -c %s "$out")" "$(file -b "$out" | cut -c1-64)"
echo
echo "verify:  $here/verify-ffmpeg-lanes.sh $out"
echo "use:     IMZERO2_FFMPEG_BIN=$out"
