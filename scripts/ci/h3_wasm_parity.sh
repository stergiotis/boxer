#!/bin/bash
# Rebuild h3.wasm in a clean temporary target dir and byte-compare against
# the committed artifact. Drift exits non-zero; CI wires this into
# scripts/ci/lint.sh.
#
# Graceful skip when cargo or the wasm32-unknown-unknown target is absent:
# prints a 'skipped' line and exits 0. The intent is that contributors who
# have not installed the Rust toolchain still see a green lint, while CI
# (which has the toolchain) enforces the invariant.

set -e
set -o pipefail

here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."

crate_dir="rust/h3bridge"
committed="public/science/geo/h3/internal/h3o_wasm/h3.wasm"

if ! command -v cargo >/dev/null 2>&1; then
    echo "h3_wasm_parity: skipped (cargo not installed)"
    exit 0
fi

sysroot=$(rustc --print sysroot 2>/dev/null || true)
if [ -z "$sysroot" ] || [ ! -d "$sysroot/lib/rustlib/wasm32-unknown-unknown" ]; then
    echo "h3_wasm_parity: skipped (wasm32-unknown-unknown target not installed)"
    exit 0
fi

if [ ! -f "$committed" ]; then
    echo "h3_wasm_parity: ERROR: committed artifact not found at $committed" >&2
    exit 1
fi

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

# See scripts/dev/build_h3_wasm.sh for why this seed is pinned.
export CONST_RANDOM_SEED="boxer-h3-fixed-seed"

cargo build \
    --release \
    --locked \
    --target wasm32-unknown-unknown \
    --manifest-path "$crate_dir/Cargo.toml" \
    --target-dir "$tmpdir" >/dev/null 2>&1

built="$tmpdir/wasm32-unknown-unknown/release/h3bridge.wasm"
if [ ! -f "$built" ]; then
    echo "h3_wasm_parity: ERROR: build produced no artifact at $built" >&2
    exit 1
fi

if command -v wasm-strip >/dev/null 2>&1; then
    wasm-strip "$built"
fi
if command -v wasm-opt >/dev/null 2>&1; then
    wasm-opt -Oz --strip-debug --strip-producers "$built" -o "$built.opt"
    mv "$built.opt" "$built"
fi

new_hash=$(sha256sum "$built" | awk '{print $1}')
cur_hash=$(sha256sum "$committed" | awk '{print $1}')

if [ "$new_hash" = "$cur_hash" ]; then
    echo "h3_wasm_parity: ok ($new_hash)"
    exit 0
fi

echo "h3_wasm_parity: DRIFT" >&2
echo "  committed: $cur_hash  ($(wc -c < "$committed") bytes)" >&2
echo "  rebuilt:   $new_hash  ($(wc -c < "$built") bytes)" >&2
echo "  fix: run scripts/dev/build_h3_wasm.sh and commit the result" >&2
if command -v wasm-objdump >/dev/null 2>&1; then
    echo "--- committed sections ---" >&2
    wasm-objdump -h "$committed" >&2 || true
    echo "--- rebuilt sections ---" >&2
    wasm-objdump -h "$built" >&2 || true
fi
exit 1
