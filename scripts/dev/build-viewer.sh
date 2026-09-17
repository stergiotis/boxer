#!/bin/bash
# ADR-0243 §SD5: Linux-to-Windows viewer build and portable directory.
set -euo pipefail
repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
crate="$repo/rust/imzero2-viewer"
export FFMPEG_DIR="${FFMPEG_DIR:-$crate/target/windows-deps}"
cc=${CC_x86_64_pc_windows_gnu:-x86_64-w64-mingw32-gcc}
export CC_x86_64_pc_windows_gnu="$cc"
export CARGO_TARGET_X86_64_PC_WINDOWS_GNU_LINKER="$cc"
missing=0
for tool in cargo go "$cc" x86_64-w64-mingw32-objdump; do
    if ! command -v "$tool" >/dev/null; then printf 'missing prerequisite: %s\n' "$tool" >&2; missing=1; fi
done
for file in "$FFMPEG_DIR/include/libavcodec/avcodec.h" "$FFMPEG_DIR/lib/libavcodec.dll.a"; do
    if [[ ! -f "$file" ]]; then printf 'missing Windows FFmpeg development file: %s\n' "$file" >&2; missing=1; fi
done
[[ "$missing" == 0 ]] || exit 1
cd "$crate"
source "$repo/scripts/dev/rust-repro-env.sh"
# bindgen must see the target headers, not the build-host libc headers.
mingw_root_include=$($cc -print-file-name=../../../../x86_64-w64-mingw32/include)
export BINDGEN_EXTRA_CLANG_ARGS_x86_64_pc_windows_gnu="${BINDGEN_EXTRA_CLANG_ARGS_x86_64_pc_windows_gnu:-} --target=x86_64-w64-windows-gnu -I$mingw_root_include"
cargo build --release --locked --target x86_64-pc-windows-gnu
out="$crate/target/windows-portable"
mkdir -p "$out"
cp target/x86_64-pc-windows-gnu/release/imzero2-viewer.exe "$out/"
cp "$FFMPEG_DIR"/bin/*.dll "$out/"
# MinGW's thread runtime is required by some builds of dav1d/ring.
for dll in libwinpthread-1.dll libgcc_s_seh-1.dll; do
    path=$($cc -print-file-name="$dll")
    if [[ -f "$path" ]]; then cp "$path" "$out/"; fi
done
cp -R "$FFMPEG_DIR/licenses" "$FFMPEG_DIR/sources" "$out/"
cp README.md "$out/README.md"
# Preserve license files from the exact locked Rust dependency sources.
cargo metadata --locked --format-version 1 > "$crate/target/dependency-metadata.json"
(cd "$repo" && ./boxer.sh gov cargo-licenses \
    --metadata "$crate/target/dependency-metadata.json" \
    --out "$out/licenses/rust")
# A packager may supply toolchain-specific runtime notices without hardcoding a distro.
if [[ -n "${MINGW_LICENSE_DIR:-}" ]]; then cp -R "$MINGW_LICENSE_DIR" "$out/licenses/mingw"; fi
for file in "$out"/*.exe "$out"/*.dll; do
    x86_64-w64-mingw32-objdump -p "$file" | grep 'DLL Name:'
done > "$out/dependencies.txt"
printf 'Built %s\n' "$out/imzero2-viewer.exe"
