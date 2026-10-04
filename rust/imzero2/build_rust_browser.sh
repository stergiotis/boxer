#!/bin/bash
# ADR-0263: build the BROWSER host — the shared FFFI2 interpreter and egui
# tessellating into the ADR-0128 mesh wire, as a wasm32 cdylib with a plain
# C ABI (browser/ crate over src/imzero2/browser.rs) for the JS worker. No
# wasm-bindgen: the module's only import is `env.now_ms`. Nothing in the
# wasm32 graph needs getrandom since egui_graphs left the tree; should a
# dependency bring it back, its backend cfg goes in RUSTFLAGS here, not in a
# cargo config file — rust-repro-env.sh exports RUSTFLAGS, and an environment
# RUSTFLAGS replaces every `rustflags` a config would set.
#
# Separate --target-dir so it doesn't thrash the native hosts' caches.
# Module lands at target/browser/wasm32-unknown-unknown/release/imzero2_browser.wasm,
# or under IMZERO2_BROWSER_TARGET_DIR when set: a consumer's `bundle` builds
# from boxer's module directory, which the Go module cache keeps read-only
# (ADR-0278 SD4, proposed).
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
# shellcheck source=/dev/null
source "$here/../../scripts/dev/rust-repro-env.sh"
cargo build --release --locked -p imzero2_browser --target wasm32-unknown-unknown --target-dir "${IMZERO2_BROWSER_TARGET_DIR:-target/browser}"
