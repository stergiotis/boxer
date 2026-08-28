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
# ARCHITECTURE: x86_64 and aarch64 are both supported and both build the same
# component set. The difference is the assembler, and it is entirely in the
# preflight -- see the "target architecture" block there for why nasm is an
# x86-only prerequisite and GNU as takes its place on aarch64.
#
# Build-host prerequisites (system packages, all of them ordinary toolchain):
#   cc/c++, make, cmake, perl, pkg-config, tar, GNU diff, an assembler for this
#   CPU (x86_64: nasm plus the `which` BINARY; aarch64: GNU as), a static libc,
#   and -- for the default h264 build -- a static libstdc++. You do not have to
#   work from this list: --preflight-only checks every one of them and prints
#   those that are missing TOGETHER, each with the package that supplies it on
#   the host it is run on.
#
#   Four of them are easy to miss because a modern distro may ship none of them,
#   and each fails in a way that names the wrong culprit:
#     * busybox diff lacks options libvpx's configure uses, which then fails
#       with a bare "Configuration failed" that names no cause;
#     * on x86 libvpx probes for its assembler with `which nasm` rather than the
#       shell builtin -- on Fedora 42, which drops the `which` package and puts
#       nasm in /usr/sbin, an otherwise complete toolchain fails with "Neither
#       yasm nor nasm have been found" while nasm is plainly installed;
#     * without a static libc nothing links at all (glibc-static on RPM distros;
#       musl hosts have one already, and give the smaller binary);
#     * openh264 is C++ and its pkg-config file carries -lstdc++, so the static
#       link needs libstdc++.a. Without it ffmpeg's configure reports "openh264
#       >= 1.3.0 not found using pkg-config" -- which reads as a missing
#       openh264, though openh264 built and installed perfectly well.
#       --without-h264 drops that requirement along with the encoder.
set -e
set -o pipefail

here=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")

FFMPEG_VER=7.1.1
# >= 3.13 is REQUIRED on a host with nasm 3.x. Up to 3.12.1, libaom's test_nasm
# looked for the literal "-Ox" in `nasm -hf` output; nasm 3.0 moved that text to
# `nasm -hO`, so the probe reports "Unsupported nasm: multipass optimization not
# supported" and stops the build -- on a nasm that supports -Ox perfectly well.
# Newer libaom probes -hO for the flag and -hf only for object formats.
AOM_VER=3.14.1
SVTAV1_VER=2.3.0
VPX_VER=1.15.0
OPENH264_VER=2.6.0

src_dir="$PWD/_ffmpeg-src"
prefix=""
out=""
jobs=$(nproc 2>/dev/null || echo 4)
fetch=0
fetch_only=0
with_h264=1
preflight_only=0

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
  --preflight-only
                  check the build-host prerequisites and exit, building nothing.
                  For a caller that runs this build LATE in a longer flow: gate
                  on it up front instead of discovering a missing cmake at the end.
  --fetch-only    download the source tarballs into --src-dir and exit, building
                  nothing. Implies --fetch. For staging sources to be compiled
                  somewhere else -- an airgap bundle carries them so the binary can
                  be built later on a host of the TARGET's architecture, which is
                  how a bundle for a foreign arch gets an ffmpeg at all. Skips the
                  build-host preflight, since nothing is compiled here.
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
    --src-dir) src_dir=$(readlink -f "$2"); shift 2 ;;
    --prefix) prefix=$(readlink -f "$2"); shift 2 ;;
    # Absolute, because the install below runs from inside the ffmpeg source
    # tree: a relative --out would silently land there instead of the caller's
    # cwd. readlink -f resolves a path whose final component does not exist yet.
    --out) out=$(readlink -f "$2"); shift 2 ;;
    --jobs) jobs="$2"; shift 2 ;;
    --fetch) fetch=1; shift ;;
    --without-h264) with_h264=0; shift ;;
    --preflight-only) preflight_only=1; shift ;;
    --fetch-only) fetch_only=1; fetch=1; shift ;;
    -h | --help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
    esac
done

[ -n "$prefix" ] || prefix="$src_dir/_prefix"
[ -n "$out" ] || out="$PWD/ffmpeg-static"
log_dir="$src_dir/_logs"
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

# --fetch-only compiles nothing, so the build toolchain is beside the point; only
# curl and tar matter, and a missing one fails loudly at the fetch itself.
if [ "$fetch_only" = 1 ]; then
    echo "  skipped (--fetch-only compiles nothing)"
fi

# Every missing prerequisite is reported AT ONCE, each named with the package
# that supplies it on this host. Failing on the first one costs the operator a
# round trip per package, and a distro may plausibly be missing four of these at
# a time -- a modern Fedora ships neither a static libc nor a static libstdc++,
# and nasm and cmake are not in any base install.
if [ "$fetch_only" = 0 ]; then
pkgmgr=""
for m in dnf apt-get apk pacman; do
    if command -v "$m" >/dev/null 2>&1; then pkgmgr=$m; break; fi
done
case "$pkgmgr" in
apt-get) pkg_install="sudo apt-get install" ;;
apk)     pkg_install="sudo apk add" ;;
pacman)  pkg_install="sudo pacman -S" ;;
dnf)     pkg_install="sudo dnf install" ;;
*)       pkg_install="" ;;
esac

# Pick the package for this host's family. Where a family ships a thing inside a
# broader package rather than its own -- Debian's libc.a lives in libc6-dev, its
# libstdc++.a comes with the g++ metapackage -- name what actually installs it.
pkg_for() { # <rpm> <deb> <apk> <arch>
    case "$pkgmgr" in
    dnf)     echo "$1" ;;
    apt-get) echo "$2" ;;
    apk)     echo "$3" ;;
    pacman)  echo "$4" ;;
    *)       echo "" ;;
    esac
}

# ---- target architecture ----------------------------------------------------
# Every codec here builds on both x86_64 and aarch64; only the ASSEMBLER differs,
# and getting that wrong is the difference between "aarch64 is unsupported" and
# "aarch64 works". nasm is an x86 assembler and nothing in this component set
# reaches for it on any other CPU:
#   * libaom gates its entire nasm/yasm search on AOM_TARGET_CPU being x86 or
#     x86_64 (cmake/aom_configure.cmake); on aarch64 it takes AOM_TARGET_CPU=arm64
#     and assembles .S through the C driver;
#   * SVT-AV1 gates the same search on HAVE_X86_PLATFORM, and on ARM builds its
#     Neon/SVE flavours with plain -march= flags (CMakeLists.txt);
#   * libvpx's `which nasm` probe sits inside its x86 branch
#     (build/make/configure.sh) -- which is also the only reason the `which`
#     BINARY is a prerequisite at all;
#   * ffmpeg only looks for an x86asmexe on x86.
# So demanding nasm unconditionally would refuse an aarch64 host that can build
# all four perfectly well -- and, through airgap_preflight_ffmpeg_build, would
# silently cost every aarch64 bundle its ffmpeg.
#
# What takes nasm's place there is GNU as: libvpx's arm64-linux-gcc target sets
# AS=${CROSS}as outright (configure.sh), and libaom and ffmpeg push their .S
# files through the C driver, which needs it too. binutils rides along with gcc,
# so this is nearly always satisfied -- but a clang-only host is not, and
# "as: command not found" partway into a libvpx build names the wrong thing.
#
# Only x86_64 and aarch64 are exercised. Anything else is not refused -- this
# script is usable outside the airgap flow, which does refuse them -- but it gets
# the aarch64 prerequisite set and a warning, because that is a guess: the codecs
# would fall back to their generic-C paths, and SVT-AV1 in particular wants
# -DCOMPILE_C_ONLY=ON there, which this script does not pass.
case "$(uname -m)" in
x86_64 | amd64)   target_cpu=x86 ;;
aarch64 | arm64)  target_cpu=arm64 ;;
*)                target_cpu=other ;;
esac
if [ "$target_cpu" = other ]; then
    echo "  WARNING: $(uname -m) is not an exercised target (x86_64/aarch64); proceeding anyway." >&2
fi

miss_what=() miss_pkg=() miss_why=()
need() { # <what> <package> <why>
    miss_what+=("$1")
    miss_pkg+=("$2")
    miss_why+=("$3")
}
lacks() { ! command -v "$1" >/dev/null 2>&1; }

lacks cc         && need cc         "$(pkg_for gcc build-essential build-base gcc)"         "nothing here compiles without it"
lacks c++        && need c++        "$(pkg_for gcc-c++ build-essential build-base gcc)"     "libaom's cmake project declares CXX"
lacks make       && need make       "$(pkg_for make build-essential make make)"             "libvpx, openh264 and ffmpeg build with it"
lacks cmake      && need cmake      "$(pkg_for cmake cmake cmake cmake)"                    "libaom and SVT-AV1 are cmake projects"
if [ "$target_cpu" = x86 ]; then
    lacks nasm   && need nasm       "$(pkg_for nasm nasm nasm nasm)"                        "libaom, libvpx and ffmpeg assemble their hot paths"
else
    lacks as     && need as         "$(pkg_for binutils binutils binutils binutils)"        "libvpx sets AS=as on aarch64; .S files need it either way"
fi
lacks perl       && need perl       "$(pkg_for perl perl perl perl)"                        "ffmpeg's configure and openh264's makefiles use it"
lacks tar        && need tar        "$(pkg_for tar tar tar tar)"                            "the pinned sources are tarballs"
lacks pkg-config && need pkg-config "$(pkg_for pkgconf-pkg-config pkg-config pkgconf pkgconf)" \
    "ffmpeg's configure locates the codec libraries with it"
if [ "$fetch" = 1 ]; then
    lacks curl && need curl "$(pkg_for curl curl curl curl)" "--fetch downloads the tarballs with it"
fi

# libvpx's configure needs GNU diff; busybox diff lacks the options it uses and
# fails with a bare "Configuration failed" that names no cause.
if ! diff --help 2>&1 | grep -q -- '--unified'; then
    need "GNU diff" "$(pkg_for diffutils diffutils diffutils diffutils)" \
        "libvpx's configure uses options busybox diff lacks"
fi
# ...and on x86 it locates its assembler with `which nasm`, not the shell
# builtin. Test the binary the way libvpx will, or the build dies much later
# claiming nasm is absent when it is merely unreachable that way. Both checks are
# x86-only: that `which` call is in libvpx's x86 branch, so on aarch64 a host
# without the `which` binary builds fine and must not be told otherwise.
if [ "$target_cpu" = x86 ]; then
    if lacks which; then
        need "the 'which' binary" "$(pkg_for which debianutils which which)" \
            "libvpx's configure finds nasm with it, not the shell builtin"
    elif ! lacks nasm && ! which nasm >/dev/null 2>&1; then
        need "nasm reachable by 'which'" "" \
            "nasm is installed but 'which nasm' fails (often /usr/sbin; add it to PATH)"
    fi
fi

probe_dir=$(mktemp -d)
trap 'rm -rf -- "$probe_dir"' EXIT
if ! lacks "${CC:-cc}"; then
    echo 'int main(void){return 0;}' >"$probe_dir/probe.c"
    if ! "${CC:-cc}" -static -o "$probe_dir/probe" "$probe_dir/probe.c" 2>/dev/null; then
        need "a static libc" "$(pkg_for glibc-static libc6-dev musl-dev glibc)" \
            "cc -static cannot link (a musl host already has one)"
    fi
fi
# Probe the static C++ link the way ffmpeg's configure will: openh264 is C++ and
# its .pc carries -lstdc++, so a missing libstdc++.a surfaces much later as
# "openh264 >= 1.3.0 not found using pkg-config", blaming the wrong component
# entirely.
if [ "$with_h264" = 1 ] && ! lacks "${CXX:-c++}"; then
    echo 'int main(void){return 0;}' >"$probe_dir/probe.cc"
    if ! "${CXX:-c++}" -static -o "$probe_dir/probecc" "$probe_dir/probe.cc" 2>/dev/null; then
        need "a static libstdc++" "$(pkg_for libstdc++-static g++ g++ gcc)" \
            "c++ -static cannot link; openh264 is C++ (or pass --without-h264)"
    fi
fi

n=${#miss_what[@]}
if [ "$n" -gt 0 ]; then
    w=0 pw=0 pkgs=()
    for ((i = 0; i < n; i++)); do
        if [ ${#miss_what[i]} -gt "$w" ]; then w=${#miss_what[i]}; fi
        if [ ${#miss_pkg[i]} -gt "$pw" ]; then pw=${#miss_pkg[i]}; fi
        if [ -n "${miss_pkg[i]}" ]; then pkgs+=("${miss_pkg[i]}"); fi
    done
    {
        printf 'error: %d missing build-host prerequisite%s:\n\n' "$n" "$([ "$n" = 1 ] || echo s)"
        for ((i = 0; i < n; i++)); do
            printf '  %-*s  %-*s  %s\n' "$w" "${miss_what[i]}" "$pw" "${miss_pkg[i]}" "${miss_why[i]}"
        done
        echo
        if [ -n "$pkg_install" ] && [ ${#pkgs[@]} -gt 0 ]; then
            # de-duplicate preserving order: one package can supply several needs
            printf '  %s %s\n' "$pkg_install" \
                "$(printf '%s\n' "${pkgs[@]}" | awk '!seen[$0]++' | paste -sd' ')"
        else
            echo "  (no package manager recognised here -- install the above by hand)"
        fi
    } >&2
    exit 1
fi

if [ "$preflight_only" = 1 ]; then
    echo "  all prerequisites present ($(uname -m); assembler: $([ "$target_cpu" = x86 ] && echo nasm || echo 'GNU as'))"
    exit 0
fi
fi   # end: fetch_only == 0

mkdir -p "$src_dir" "$prefix" "$log_dir"

fetch_one() { # <url> <file>
    [ -f "$src_dir/$2" ] && return 0
    [ "$fetch" = 1 ] || die "missing $src_dir/$2 (pass --fetch, or stage the tarballs first)"
    step "fetch $2"
    # Download aside and rename only on success. A tarball truncated by a dropped
    # connection would otherwise satisfy the -f test above on the next run, which
    # never re-fetches it -- turning a transient network failure into a permanent
    # "tar: unexpected EOF" that no amount of re-running clears.
    curl -sSL --retry 3 -o "$src_dir/$2.part" "$1" || {
        rm -f "$src_dir/$2.part"
        die "could not download $2 from $1"
    }
    mv -f "$src_dir/$2.part" "$src_dir/$2"
}

step "sources"
fetch_one "https://ffmpeg.org/releases/ffmpeg-${FFMPEG_VER}.tar.xz" "ffmpeg-${FFMPEG_VER}.tar.xz"
fetch_one "https://storage.googleapis.com/aom-releases/libaom-${AOM_VER}.tar.gz" "libaom-${AOM_VER}.tar.gz"
fetch_one "https://gitlab.com/AOMediaCodec/SVT-AV1/-/archive/v${SVTAV1_VER}/SVT-AV1-v${SVTAV1_VER}.tar.gz" "SVT-AV1-v${SVTAV1_VER}.tar.gz"
fetch_one "https://github.com/webmproject/libvpx/archive/refs/tags/v${VPX_VER}.tar.gz" "libvpx-${VPX_VER}.tar.gz"
fetch_one "https://github.com/cisco/openh264/archive/refs/tags/v${OPENH264_VER}.tar.gz" "openh264-${OPENH264_VER}.tar.gz"
(cd "$src_dir" && sha256sum ./*.tar.*)

if [ "$fetch_only" = 1 ]; then
    step "done (--fetch-only): sources staged in $src_dir"
    exit 0
fi

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
    # CMAKE_POLICY_VERSION_MINIMUM is needed from CMake 4 on: SVT-AV1's vendored
    # third_party/cpuinfo still declares cmake_minimum_required below 3.5, and
    # CMake 4 removed compatibility with it ("Compatibility with CMake < 3.5 has
    # been removed"). This is CMake's own documented escape hatch, and it is
    # inert on older CMake -- an unused-variable warning, not an error -- so it
    # does not fork the build by toolchain version. Preferred over bumping the
    # pin: SVT-AV1 3.0 dropped deprecated API that ffmpeg 7.1.1's libsvtav1
    # wrapper still uses.
    run svt cmake "$src_dir/SVT-AV1-v${SVTAV1_VER}" \
        -DCMAKE_INSTALL_PREFIX="$prefix" -DCMAKE_INSTALL_LIBDIR=lib \
        -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF \
        -DBUILD_APPS=OFF -DBUILD_TESTING=OFF -DBUILD_DEC=OFF \
        -DCMAKE_POLICY_VERSION_MINIMUM=3.5
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
