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
# Required post-processors — the committed blob is their output, not cargo's:
#   - `wasm-strip` from wabt  (dnf install wabt)
#   - `wasm-opt -Oz` from binaryen  (dnf install binaryen)
# They were optional until it became clear what skipping them costs: the blob
# they are skipped for is ~23% larger and byte-different, so writing it over
# the committed artifact hands the drift to scripts/ci/h3_wasm_parity.sh on
# every machine that HAS them. Missing either is now an error.
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
# rustup picks a toolchain from the CURRENT DIRECTORY, and this script builds
# from the repo root with --manifest-path, so rust/h3bridge/rust-toolchain.toml
# would be ignored — the pin would silently do nothing. Name the channel on the
# command line instead. cd-ing into the crate would also work but is NOT
# equivalent: cargo then passes rustc a relative source path instead of an
# absolute one, which changes the bytes this artifact is byte-compared against.
tc=""
if command -v rustup >/dev/null 2>&1; then
    ch=$(sed -n 's/^channel[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' "$crate_dir/rust-toolchain.toml" | head -1)
    if [ -n "$ch" ]; then
        rustup toolchain list 2>/dev/null | grep -q "^$ch" \
            || { echo "ERROR: $crate_dir pins rustc $ch, which is not installed." >&2
                 echo "  rustup toolchain install $ch --target wasm32-unknown-unknown" >&2
                 exit 1; }
        tc="+$ch"
    fi
fi

# shellcheck disable=SC2086 # $tc is one optional word
sysroot=$(rustc $tc --print sysroot 2>/dev/null || true)
if [ -z "$sysroot" ] || [ ! -d "$sysroot/lib/rustlib/wasm32-unknown-unknown" ]; then
    echo "ERROR: wasm32-unknown-unknown stdlib not installed." >&2
    echo "  Under rustup:  rustup target add wasm32-unknown-unknown" >&2
    echo "  Under Fedora:  sudo dnf install rust-std-static-wasm32-unknown-unknown" >&2
    exit 1
fi

missing_pp=()
command -v wasm-strip >/dev/null 2>&1 || missing_pp+=("wasm-strip  (dnf install wabt)")
command -v wasm-opt   >/dev/null 2>&1 || missing_pp+=("wasm-opt    (dnf install binaryen)")
if [ ${#missing_pp[@]} -gt 0 ]; then
    echo "ERROR: post-processor(s) not installed; the artifact would not match CI's:" >&2
    printf '  %s\n' "${missing_pp[@]}" >&2
    exit 1
fi

# Fixed seed for const-random (transitively used by h3o → ahash → const-random)
# so the release wasm is byte-reproducible across machines.
export CONST_RANDOM_SEED="boxer-h3-fixed-seed"

# Remap absolute source paths out of the panic strings baked into the artifact:
# registry sources land under /cargo, checkout sources under /build. Without
# this the blob carries the building user's $CARGO_HOME and checkout path, and
# byte-parity across machines is impossible. The flags live in one file so this
# script and scripts/ci/h3_wasm_parity.sh cannot drift apart.
# shellcheck source=/dev/null
source "$here/rust-repro-env.sh"

echo "=== cargo $tc build --release --target wasm32-unknown-unknown ==="
# shellcheck disable=SC2086 # $tc is one optional word
cargo $tc build \
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

echo "=== wasm-strip ==="
wasm-strip "$tmp"

echo "=== wasm-opt -Oz --strip-debug --strip-producers ==="
# rustc (≥1.82) emits bulk-memory and nontrapping float-to-int by default
# and the wazero runtime accepts both; wasm-opt must be told to allow them.
wasm-opt -Oz --enable-bulk-memory --enable-nontrapping-float-to-int \
    --strip-debug --strip-producers "$tmp" -o "$tmp.opt"
mv "$tmp.opt" "$tmp"

mkdir -p "$(dirname "$dst")"
cp "$tmp" "$dst"
echo "=== wrote $dst ($(wc -c < "$dst") bytes) ==="

echo "=== cargo run --bin emit_golden ==="
# shellcheck disable=SC2086 # $tc is one optional word
cargo $tc run \
    --locked \
    --manifest-path "$crate_dir/Cargo.toml" \
    --bin emit_golden

echo "=== build_h3_wasm: done ==="
