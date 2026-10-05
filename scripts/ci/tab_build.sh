#!/bin/bash
# Check that the browser tab's two modules still build (ADR-0263): the Go tab
# host as a wasip1 c-shared module, and the Rust browser host for
# wasm32-unknown-unknown. Neither is exercised by any other lane — the Go
# tree builds natively, and rust_imzero2_check.sh checks the `browser`
# feature natively — so a break in the wasm-only corners (a syscall a
# build tag missed, an import the wasm32 target lacks) would otherwise
# surface only when someone runs scripts/dev/build_tab_bundle.sh.
#
# The Go half always runs (Go is mandatory). The Rust half is a `cargo
# check`, not a build, and skips like h3_wasm_parity.sh when cargo or the
# wasm32-unknown-unknown target is missing, so a contributor without that
# target still sees a green lint while CI enforces it.
set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
# shellcheck source=/dev/null
source scripts/dev/go-build-env.sh
tags=$(cat ./tags)
out=$(mktemp -d "${TMPDIR:-/tmp}/tab_build.XXXXXX")
trap 'rm -rf "$out"' EXIT

# shellcheck disable=SC2086 # deliberate word splitting of the flag list
GOOS=wasip1 GOARCH=wasm go build $BOXER_GO_FLAGS -buildmode=c-shared -tags "$tags" -o "$out/imzero2tab.wasm" ./public/thestack/cmd/imzero2tab
echo "tab_build: imzero2tab.wasm builds ($(stat -c %s "$out/imzero2tab.wasm") bytes)"

if ! command -v cargo >/dev/null 2>&1; then
    echo "tab_build: rust half skipped (cargo not installed)"
    exit 0
fi
sysroot=$(rustc --print sysroot 2>/dev/null || true)
if [ -z "$sysroot" ] || [ ! -d "$sysroot/lib/rustlib/wasm32-unknown-unknown" ]; then
    echo "tab_build: rust half skipped (wasm32-unknown-unknown target not installed)"
    exit 0
fi
# The same flags build_rust_browser.sh composes: the repro environment's
# RUSTFLAGS. A check, not a build: codegen for the whole egui graph is the
# bundle script's business.
(
    cd rust/imzero2
    # shellcheck source=/dev/null
    source ../../scripts/dev/rust-repro-env.sh
    cargo check --locked --quiet -p imzero2_browser --target wasm32-unknown-unknown --target-dir target/browser
)
echo "tab_build: imzero2_browser checks for wasm32-unknown-unknown"
