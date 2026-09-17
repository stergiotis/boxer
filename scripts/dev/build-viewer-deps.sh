#!/bin/bash
# ADR-0243 §SD5: Windows decoder libraries, built from pinned source on Linux.
set -euo pipefail
repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
root="$repo/rust/imzero2-viewer/target"
sources="$root/sources"
prefix="$root/windows-deps"
fetch=0
preflight=0
jobs=4
for arg in "$@"; do
    case "$arg" in
        --fetch) fetch=1 ;;
        --preflight-only) preflight=1 ;;
        *) printf 'usage: %s [--fetch] [--preflight-only]\n' "$0" >&2; exit 2 ;;
    esac
done
cc=${CC_x86_64_pc_windows_gnu:-x86_64-w64-mingw32-gcc}
missing=0
for tool in "$cc" x86_64-w64-mingw32-ar x86_64-w64-mingw32-strip make meson ninja nasm pkg-config tar sha256sum; do
    if ! command -v "$tool" >/dev/null; then printf 'missing prerequisite: %s\n' "$tool" >&2; missing=1; fi
done
[[ "$missing" == 0 ]] || exit 1
[[ "$preflight" == 0 ]] || exit 0
mkdir -p "$sources" "$prefix" "$root/deps-build"
fetch_source() {
    local file=$1 url=$2 checksum=$3
    if [[ ! -f "$sources/$file" && "$fetch" == 1 ]]; then curl --fail --location --retry 2 "$url" -o "$sources/$file"; fi
    [[ -f "$sources/$file" ]] || { printf 'missing source %s (use --fetch)\n' "$file" >&2; exit 1; }
    printf '%s  %s\n' "$checksum" "$sources/$file" | sha256sum --check
}
fetch_source ffmpeg-8.1.tar.xz https://ffmpeg.org/releases/ffmpeg-8.1.tar.xz b072aed6871998cce9b36e7774033105ca29e33632be5b6347f3206898e0756a
fetch_source dav1d-1.5.3.tar.xz https://downloads.videolan.org/pub/videolan/dav1d/1.5.3/dav1d-1.5.3.tar.xz 732010aa5ef461fa93355ed2c6c5fedb48ddc4b74e697eaabe8907eaeb943011
for src in ffmpeg-8.1 dav1d-1.5.3; do
    [[ -d "$sources/$src" ]] || tar -xf "$sources/$src.tar.xz" -C "$sources"
done
# Kept with the build output so the exact cross configuration can be distributed.
cat > "$root/deps-build/mingw.ini" <<EOF
[binaries]
c = '$cc'
ar = 'x86_64-w64-mingw32-ar'
strip = 'x86_64-w64-mingw32-strip'
windres = 'x86_64-w64-mingw32-windres'
pkg-config = 'pkg-config'
[host_machine]
system = 'windows'
cpu_family = 'x86_64'
cpu = 'x86_64'
endian = 'little'
[properties]
needs_exe_wrapper = true
EOF
export SOURCE_DATE_EPOCH=1789603200
export CFLAGS="${CFLAGS:-} -ffile-prefix-map=$root=/build"
if [[ ! -f "$root/deps-build/dav1d/build.ninja" ]]; then
    meson setup "$root/deps-build/dav1d" "$sources/dav1d-1.5.3" --cross-file "$root/deps-build/mingw.ini" \
        --prefix "$prefix" --libdir lib --buildtype release --default-library shared \
        -Denable_tools=false -Denable_tests=false
fi
meson compile -C "$root/deps-build/dav1d" -j "$jobs"
meson install -C "$root/deps-build/dav1d"
export PKG_CONFIG_LIBDIR="$prefix/lib/pkgconfig"
export PKG_CONFIG_PATH="$PKG_CONFIG_LIBDIR"
cd "$root/deps-build"
mkdir -p ffmpeg
cd ffmpeg
if [[ ! -f config.h ]] || ! grep -q '^CONFIG_AV1_D3D11VA2_HWACCEL=yes' ffbuild/config.mak; then
    "$sources/ffmpeg-8.1/configure" --prefix="$prefix" --target-os=mingw32 --arch=x86_64 --enable-cross-compile \
        --cross-prefix=x86_64-w64-mingw32- --cc="$cc" --pkg-config=pkg-config \
        --disable-everything --disable-autodetect --disable-programs --disable-doc --disable-debug \
        --disable-avformat --disable-avfilter --disable-avdevice --disable-swresample --disable-swscale \
        --enable-avcodec --enable-avutil --enable-shared --disable-static --enable-libdav1d \
        --enable-decoder=h264,vp9,av1,libdav1d --enable-parser=h264,vp9,av1 \
        --enable-d3d11va --enable-hwaccel=h264_d3d11va,h264_d3d11va2,vp9_d3d11va,vp9_d3d11va2,av1_d3d11va,av1_d3d11va2 \
        --extra-cflags="$CFLAGS" --extra-ldflags=-Wl,--no-insert-timestamp
fi
make -j "$jobs"
make install
mkdir -p "$prefix/licenses" "$prefix/sources"
cp "$sources/ffmpeg-8.1/COPYING.LGPLv2.1" "$prefix/licenses/FFmpeg-LGPL-2.1.txt"
cp "$sources/dav1d-1.5.3/COPYING" "$prefix/licenses/dav1d.txt"
cp "$sources/ffmpeg-8.1.tar.xz" "$sources/dav1d-1.5.3.tar.xz" "$prefix/sources/"
cp "$repo/scripts/dev/build-viewer-deps.sh" "$prefix/sources/"
printf 'Windows libraries staged at %s\n' "$prefix"
