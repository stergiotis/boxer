#!/bin/bash
# Source this before `cargo build` to make the artifact byte-reproducible across
# machines (ADR-0215). Exports RUSTFLAGS with the two --remap-path-prefix pairs
# that actually leak into a release binary:
#
#   $CARGO_HOME  -> /cargo   Registry sources. A release imzero2 carried 828
#                            strings naming the builder's $HOME this way — they
#                            are the file names in panic locations, which
#                            survive `debug = false`.
#   $PWD         -> /build   The crate itself, and everything cargo generates
#                            beneath it. This one also covers OUT_DIR: under
#                            `headless`, build.rs generates the protobuf wire
#                            types, and the generated file names its own target
#                            directory. Two builds differing only in
#                            --target-dir produced 56 157 differing bytes
#                            before this flag.
#
# Usage — cd into the crate first, then:
#
#     source "$boxer/scripts/dev/rust-repro-env.sh"
#
# Set RUST_REPRO_ROOT beforehand to remap something other than $PWD. Sourcing
# twice is a no-op rather than a doubled flag list.
#
# ONE CONSTRAINT, and every build script already satisfies it: the target
# directory must live INSIDE the remapped root. `--target-dir target/headless`
# (relative, in-crate) becomes `/build/target/headless/...` on every machine; an
# out-of-tree `--target-dir /tmp/x` is not covered by either prefix, and under
# `headless` — where build.rs generates the protobuf types and the generated
# file names its own OUT_DIR — that leaks straight into the binary. Measured:
# two builds differing only in an out-of-tree target dir are still 56 157 bytes
# apart; the same build into a relative target dir embeds
# `/build/target/<dir>/release/build/imzero2-<hash>/out/boxer.imzero2.v1.rs` and
# nothing machine-specific at all.
#
# Cargo's own `[profile.*] trim-paths` is the tidier form and would apply to a
# hand-typed `cargo build` too, but it is still nightly-only as of cargo 1.98
# ("feature `trim-paths` is required"). Recheck when the toolchain moves; this
# file is then the one place to change.
#
# Whatever this exports must match what scripts/ci/h3_wasm_parity.sh applies, or
# the parity gate rebuilds something no build script could have produced. Both
# now source this file, so they cannot drift.

if [ -z "${RUST_REPRO_ENV_APPLIED:-}" ]; then
    : "${RUST_REPRO_ROOT:=$PWD}"
    : "${CARGO_HOME:=$HOME/.cargo}"
    RUSTFLAGS="${RUSTFLAGS:+$RUSTFLAGS }--remap-path-prefix=$CARGO_HOME=/cargo --remap-path-prefix=$RUST_REPRO_ROOT=/build"
    export RUSTFLAGS
    RUST_REPRO_ENV_APPLIED=1
    export RUST_REPRO_ENV_APPLIED
fi
