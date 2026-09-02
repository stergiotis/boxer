#!/bin/bash
# Rebuild h3.wasm in a clean temporary target dir and byte-compare against
# the committed artifact. Drift exits non-zero; CI wires this into
# scripts/ci/lint.sh.
#
# Graceful skip when any part of the build pipeline is absent — cargo, the
# wasm32-unknown-unknown target, or the wasm-strip / wasm-opt post-processors:
# prints a 'skipped' line and exits 0. The intent is that contributors who
# have not installed the toolchain still see a green lint, while CI (which has
# all of it) enforces the invariant.
#
# The post-processors are part of that pipeline, not an optional extra. The
# committed blob is the OUTPUT of `wasm-strip` + `wasm-opt -Oz`, so a machine
# missing them rebuilds something that cannot match it and never could: -Oz
# alone accounts for ~90 KB and roughly half the function-section entries.
# Before this skip existed the step reported that as DRIFT and told the reader
# to "run build_h3_wasm.sh and commit the result" — which would have replaced
# the optimised artifact with an unoptimised one and moved the drift to every
# machine that DOES have binaryen.

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

# The pinned channel, named on the command line for the same reason
# scripts/dev/build_h3_wasm.sh does it: rustup reads rust-toolchain.toml from the
# CWD, and both scripts build from the repo root with --manifest-path. A pin the
# build script honours and this one ignores would compare two compilers' output.
tc=""
if command -v rustup >/dev/null 2>&1; then
    ch=$(sed -n 's/^channel[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' "$crate_dir/rust-toolchain.toml" | head -1)
    if [ -n "$ch" ]; then
        if ! rustup toolchain list 2>/dev/null | grep -q "^$ch"; then
            echo "h3_wasm_parity: skipped (pinned toolchain $ch not installed)"
            exit 0
        fi
        tc="+$ch"
    fi
fi

# shellcheck disable=SC2086 # $tc is one optional word
sysroot=$(rustc $tc --print sysroot 2>/dev/null || true)
if [ -z "$sysroot" ] || [ ! -d "$sysroot/lib/rustlib/wasm32-unknown-unknown" ]; then
    echo "h3_wasm_parity: skipped (wasm32-unknown-unknown target not installed${ch:+ for $ch})"
    exit 0
fi

missing_pp=()
command -v wasm-strip >/dev/null 2>&1 || missing_pp+=("wasm-strip (wabt)")
command -v wasm-opt   >/dev/null 2>&1 || missing_pp+=("wasm-opt (binaryen)")
if [ ${#missing_pp[@]} -gt 0 ]; then
    echo "h3_wasm_parity: skipped (post-processor(s) not installed: ${missing_pp[*]})"
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

# Path remap, from the same file scripts/dev/build_h3_wasm.sh sources — parity
# would drift on every machine whose $CARGO_HOME or checkout path differs from
# the builder's, and two copies of these flags would drift on their own.
# shellcheck source=/dev/null
source "$here/../dev/rust-repro-env.sh"

# shellcheck disable=SC2086 # $tc is one optional word
cargo $tc build \
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

# Both are present — the skip above returned otherwise. Feature flags must
# match scripts/dev/build_h3_wasm.sh.
wasm-strip "$built"
wasm-opt -Oz --enable-bulk-memory --enable-nontrapping-float-to-int \
    --strip-debug --strip-producers "$built" -o "$built.opt"
mv "$built.opt" "$built"

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
