#!/bin/bash
# ADR-0263: build the BROWSER host — the shared FFFI2 interpreter and egui
# tessellating into the ADR-0128 mesh wire, as a wasm32 cdylib with a plain
# C ABI (browser/ crate over src/imzero2/browser.rs) for the JS worker. No
# wasm-bindgen: the module's only import is `env.now_ms`, and the two
# getrandom majors egui_graphs needs are pointed at the crate's own generator
# (see Cargo.toml), 0.3's by the cfg below. That cfg cannot live in a cargo
# config file: rust-repro-env.sh exports RUSTFLAGS, and an environment
# RUSTFLAGS replaces every `rustflags` a config would set, so this script is
# the one place that composes both.
#
# Separate --target-dir so it doesn't thrash the native hosts' caches.
# Module lands at target/browser/wasm32-unknown-unknown/release/imzero2_browser.wasm.
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
# shellcheck source=/dev/null
source "$here/../../scripts/dev/rust-repro-env.sh"
export RUSTFLAGS="${RUSTFLAGS:+$RUSTFLAGS }--cfg getrandom_backend=\"custom\""
cargo build --release --locked -p imzero2_browser --target wasm32-unknown-unknown --target-dir target/browser
