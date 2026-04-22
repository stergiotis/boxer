#!/bin/bash
# Build the h3bridge Rust crate to wasm32-unknown-unknown and copy the
# artifact into public/science/geo/h3/internal/h3o_wasm/h3.wasm. Also
# regenerates golden testdata vectors on the host target.
#
# Requires:
#   - `cargo` (distro-packaged or rustup-managed).
#   - The wasm32-unknown-unknown target library. Under rustup:
#         rustup target add wasm32-unknown-unknown
#     Under Fedora's system cargo:
#         sudo dnf install rust-std-static-wasm32-unknown-unknown
#
# Optional post-processors (improve reproducibility of the committed blob):
#   - `wasm-strip` from wabt  (dnf install wabt)
#   - `wasm-opt -Oz` from binaryen  (dnf install binaryen)
#
# The script is idempotent: running it twice on a clean tree should produce
# byte-identical output.

set -e
set -o pipefail

here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."

crate_dir="rust/h3bridge"
dst="public/science/geo/h3/internal/h3o_wasm/h3.wasm"

if ! command -v cargo >/dev/null 2>&1; then
    echo "ERROR: cargo not found on PATH." >&2
    echo "  Install via 'rustup-init -y' or 'dnf install rust cargo'." >&2
    exit 1
fi

# Probe the target library by looking for the rustlib directory. Works for
# both rustup (~/.rustup/toolchains/*/lib/rustlib/wasm32-unknown-unknown)
# and Fedora (/usr/lib/rustlib/wasm32-unknown-unknown) installs, and does
# not need write access to invoke rustc with tempdirs.
sysroot=$(rustc --print sysroot 2>/dev/null || true)
if [ -z "$sysroot" ] || [ ! -d "$sysroot/lib/rustlib/wasm32-unknown-unknown" ]; then
    echo "ERROR: wasm32-unknown-unknown stdlib not installed." >&2
    echo "  Under rustup:  rustup target add wasm32-unknown-unknown" >&2
    echo "  Under Fedora:  sudo dnf install rust-std-static-wasm32-unknown-unknown" >&2
    exit 1
fi

# Fixed seed for const-random (transitively used by h3o → ahash → const-random)
# so the release wasm is byte-reproducible across machines.
export CONST_RANDOM_SEED="boxer-h3-fixed-seed"

echo "=== cargo build --release --target wasm32-unknown-unknown ==="
cargo build \
    --release \
    --locked \
    --target wasm32-unknown-unknown \
    --manifest-path "$crate_dir/Cargo.toml"

built="$crate_dir/target/wasm32-unknown-unknown/release/h3bridge.wasm"
if [ ! -f "$built" ]; then
    echo "ERROR: expected artifact not found: $built" >&2
    exit 1
fi

tmp=$(mktemp --suffix=.wasm)
trap 'rm -f "$tmp"' EXIT
cp "$built" "$tmp"

if command -v wasm-strip >/dev/null 2>&1; then
    echo "=== wasm-strip ==="
    wasm-strip "$tmp"
fi

if command -v wasm-opt >/dev/null 2>&1; then
    echo "=== wasm-opt -Oz --strip-debug --strip-producers ==="
    wasm-opt -Oz --strip-debug --strip-producers "$tmp" -o "$tmp.opt"
    mv "$tmp.opt" "$tmp"
fi

mkdir -p "$(dirname "$dst")"
cp "$tmp" "$dst"
echo "=== wrote $dst ($(wc -c < "$dst") bytes) ==="

echo "=== cargo run --bin emit_golden ==="
cargo run \
    --locked \
    --manifest-path "$crate_dir/Cargo.toml" \
    --bin emit_golden

echo "=== build_h3_wasm: done ==="
