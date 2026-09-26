#!/bin/bash
# ADR-0077 Phase 1: build the BROWSER host — the shared FFFI2 interpreter and
# egui tessellating into the ADR-0128 mesh wire, as a wasm32 module with a
# plain C ABI (src/imzero2/browser.rs) for the JS worker of the
# keelson-wasm-frame-cost trial. No wasm-bindgen: the module's only import is
# `env.now_ms`, and the two getrandom majors egui_graphs needs are pointed at
# the crate's own generator by the rustflag below (see Cargo.toml).
#
# Separate --target-dir so it doesn't thrash the native hosts' caches.
# Module lands at target/browser/wasm32-unknown-unknown/release/imzero2.wasm.
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
# shellcheck source=/dev/null
source "$here/../../scripts/dev/rust-repro-env.sh"
export RUSTFLAGS="${RUSTFLAGS:+$RUSTFLAGS }--cfg getrandom_backend=\"custom\""
cargo build --release --locked --lib --no-default-features --features browser --target wasm32-unknown-unknown --target-dir target/browser
