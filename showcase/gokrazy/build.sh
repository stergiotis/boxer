#!/bin/bash
# Build the gokrazy appliance images for the imzero2 headless host.
#
# This is ADR-0205 M6, which inherits ADR-0128 M3: an appliance image that
# renders imzero2 with no GPU stack in it at all. The CPU rasterizer
# (`headless_soft`, ADR-0205) is what makes that possible — the host links only
# libc/libm/libgcc_s and the loader, so the whole non-Go runtime closure is
# ~4.8 MB of glibc rather than Mesa plus a Vulkan loader plus an ICD.
#
# Two variants, differing ONLY by whether a static ffmpeg is in the image:
#
#   boxer-soft         no ffmpeg. The encoder probe finds nothing to spawn and
#                      CodecLane::best() degrades to the ADR-0128 mesh
#                      draw-stream lane, which needs no encoder at all. The
#                      image demonstrates that fallback rather than asserting it.
#   boxer-soft-video   the same host binary plus the static, software-only
#                      ffmpeg the airgap lane already builds
#                      (scripts/dev/build-static-ffmpeg.sh), so the H.264 lane
#                      works. A fully static ffmpeg cannot dlopen a VAAPI
#                      driver, so every hardware lane fails and libopenh264 is
#                      the only candidate — which is exactly what codeclane.rs
#                      documents.
#
# Nothing here is committed as a generated artifact: `_stage/` (the files the
# packer copies into the image), `builddir/` (the module context gokrazy builds
# boxer in) and the `.img` outputs are all produced by this script.
#
# usage: build.sh [--variant mesh|video|both] [--run] [--no-rust-build]
set -euo pipefail

here=$(dirname "$(readlink -f "$BASH_SOURCE")")
repo=$(readlink -f "$here/../..")
instances="$here/instances"

variant=both
run_vm=0
rust_build=1
while [ $# -gt 0 ]; do
    case "$1" in
        --variant) variant="$2"; shift 2 ;;
        --run)     run_vm=1; shift ;;
        --no-rust-build) rust_build=0; shift ;;
        -h|--help) sed -n '2,30p' "$0"; exit 0 ;;
        *) echo "build.sh: unknown argument: $1" >&2; exit 2 ;;
    esac
done

# gokrazy builds each package in its own module under the instance's builddir/.
# A go.work ABOVE the checkout (Go walks up to find one) would put those modules
# in that workspace instead, and its module set does not provide gokrazy's own
# packages — the failure reads "no required module provides package
# github.com/gokrazy/gokrazy/cmd/dhcp". The builddir modules are self-contained
# by design, and the boxer one carries an explicit replace, so none of them
# wants a workspace.
export GOWORK=off

command -v gok >/dev/null 2>&1 || {
    echo "build.sh: gok not on PATH. Install it with:" >&2
    echo "  go install github.com/gokrazy/tools/cmd/gok@latest" >&2
    exit 1
}

# ---- the Rust host ----------------------------------------------------------
# headless_soft, not headless_wgpu: the point of the image is that it carries no
# Vulkan loader and no ICD. build_rust_headless_soft.sh keeps its own target dir.
rust_bin="$repo/rust/imzero2/target/headless-soft/release/imzero2"
if [ "$rust_build" = 1 ] || [ ! -x "$rust_bin" ]; then
    echo "build.sh: building the CPU-rasterizing host (--features headless_soft)" >&2
    "$repo/rust/imzero2/build_rust_headless_soft.sh"
fi
[ -x "$rust_bin" ] || { echo "build.sh: no host binary at $rust_bin" >&2; exit 1; }

# The image has no dynamic loader of its own, so whatever this binary asks for
# has to be staged next to it. Read that list from the binary rather than
# hardcoding it: a new dependency should break the build here, loudly, instead
# of at boot.
mapfile -t needed < <(ldd "$rust_bin" | awk '/=>/ {print $3} /ld-linux/ {print $1}' | grep '^/' | sort -u)
[ "${#needed[@]}" -gt 0 ] || { echo "build.sh: could not read the host's dynamic closure" >&2; exit 1; }

# ---- fonts ------------------------------------------------------------------
# gokrazy has no fontconfig, so the host cannot fc-match at run time the way
# hmi_headless.sh does; the files ship in the image and the paths are passed as
# flags. The CJK fallback is deliberately left out — it is ~20 MB for glyphs the
# widget demo never draws. A demo that needs them stages a third font here.
phosphor="$repo/rust/imzero2/assets/fonts/phosphor/Phosphor.ttf"
[ -f "$phosphor" ] || { echo "build.sh: missing $phosphor" >&2; exit 1; }
main_font="${MAIN_FONT:-}"
if [ -z "$main_font" ] && command -v fc-match >/dev/null 2>&1; then
    main_font=$(fc-match -f '%{file}' 'Noto Sans' 2>/dev/null || true)
fi
[ -n "$main_font" ] && [ -f "$main_font" ] || {
    echo "build.sh: no main font found; set MAIN_FONT=/path/to/font.ttf" >&2; exit 1; }

# ---- the static ffmpeg ------------------------------------------------------
# Reuse the airgap lane's build rather than growing a second one: it is already
# static and software-only, carrying exactly the components the headless encoder
# invokes. scripts/dev/build-static-ffmpeg.sh produces it.
static_ffmpeg="${IMZERO2_STATIC_FFMPEG:-$repo/.airgap-ffmpeg-src/ffmpeg-7.1.1/ffmpeg}"

# gokrazy needs the image size up front when writing to a file rather than a
# device. Two root partitions carry a copy each of the kernel and modules, the
# Go host, the ~39 MB Rust host and (in the video variant) the 22 MB ffmpeg;
# 2 GiB leaves room for those plus a usable /perm.
storage_bytes="${IMZERO2_APPLIANCE_BYTES:-2147483648}"

stage_one() {  # <instance> <with-ffmpeg 0|1>
    local inst="$1" with_ffmpeg="$2"
    local dir="$instances/$inst" stage="$instances/$inst/_stage"

    rm -rf "$stage"; mkdir -p "$stage/lib"
    install -m 0755 "$rust_bin" "$stage/imzero2-client"
    install -m 0644 "$phosphor" "$stage/Phosphor.ttf"
    # Copied to a plain name: the packaged Noto is a variable font whose
    # filename carries brackets ("NotoSans[wght].ttf"), which are awkward in
    # both JSON and shell.
    install -m 0644 "$main_font" "$stage/MainFont.ttf"
    local so
    for so in "${needed[@]}"; do install -m 0755 "$so" "$stage/lib/$(basename "$so")"; done

    if [ "$with_ffmpeg" = 1 ]; then
        [ -x "$static_ffmpeg" ] || {
            echo "build.sh: no static ffmpeg at $static_ffmpeg" >&2
            echo "build.sh: build one with scripts/dev/build-static-ffmpeg.sh, or set IMZERO2_STATIC_FFMPEG" >&2
            exit 1; }
        if ldd "$static_ffmpeg" >/dev/null 2>&1; then
            echo "build.sh: $static_ffmpeg is dynamically linked — the image has no loader for it" >&2
            exit 1
        fi
        install -m 0755 "$static_ffmpeg" "$stage/ffmpeg"
    fi

    # The module context gokrazy builds boxer in. packer.BuildDir walks up from
    # builddir/<import path> looking for a go.mod, so putting one at the module
    # root of that tree makes every boxer package build from THIS checkout
    # rather than from whatever is published — the mechanism gokrazy documents
    # for replace directives. The path is relative so no absolute path from the
    # build host ends up in the tree.
    local bd="$dir/builddir/github.com/stergiotis/boxer"
    mkdir -p "$bd"
    cat > "$bd/go.mod" <<EOF
module gokrazy.appliance/boxer

go 1.27

require github.com/stergiotis/boxer v0.0.0

replace github.com/stergiotis/boxer => ../../../../../../../..
EOF
}

build_one() {  # <instance> <with-ffmpeg 0|1>
    local inst="$1" with_ffmpeg="$2"
    stage_one "$inst" "$with_ffmpeg"
    local img="$instances/$inst/$inst.img"
    echo "build.sh: packing $inst -> $img" >&2
    ( cd "$instances/$inst" && gok --parent_dir "$instances" -i "$inst" \
        overwrite --full "$img" --target_storage_bytes "$storage_bytes" )
    echo "build.sh: $inst image: $(du -h "$img" | cut -f1)" >&2
}

case "$variant" in
    mesh)  build_one boxer-soft 0 ;;
    video) build_one boxer-soft-video 1 ;;
    both)  build_one boxer-soft 0; build_one boxer-soft-video 1 ;;
    *) echo "build.sh: --variant must be mesh, video or both" >&2; exit 2 ;;
esac

if [ "$run_vm" = 1 ]; then
    command -v qemu-system-x86_64 >/dev/null 2>&1 || {
        echo "build.sh: --run needs QEMU (dnf install qemu-system-x86)" >&2; exit 1; }
    case "$variant" in video) inst=boxer-soft-video ;; *) inst=boxer-soft ;; esac
    # gok's default -netdev forwards only 80 (the gokrazy web UI) and 22, so the
    # carrier would be unreachable. Override it to add the carrier port and the
    # viewer page beside it (IMZERO2_HEADLESS_LISTEN + 1). Note that `vm run`
    # packs its own ephemeral image rather than booting the .img above; that one
    # is the artifact you would write to a disk.
    echo "build.sh: booting $inst; viewer page on http://localhost:8090/" >&2
    ( cd "$instances/$inst" && gok --parent_dir "$instances" -i "$inst" vm run \
        --arch amd64 --target_storage_bytes "$storage_bytes" \
        --netdev "user,id=net0,hostfwd=tcp::8080-:80,hostfwd=tcp::8022-:22,hostfwd=tcp::8089-:8089,hostfwd=tcp::8090-:8090" )
fi
