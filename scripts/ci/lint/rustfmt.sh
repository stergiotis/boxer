#!/bin/bash
# rustfmt drift across every crate under ./rust, each checked with its own
# pinned toolchain.
#
# One lint step. Runnable on its own; scripts/ci/lint.sh runs it with the
# others and documents the exit-status contract every step obeys — here a
# non-zero exit fails the gate.

set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../../.."

# Verifies every crate under ./rust is formatted with its OWN pinned rustfmt:
# scripts/dev/fmt_rust.sh --check runs `cargo fmt --all --check` inside each crate
# so the rustup proxy resolves the pin (imzero2 -> 1.96 / rustfmt 1.9.0,
# watermark -> 1.92 / rustfmt 1.8.0, h3bridge -> stable). Drift fails the build;
# fix with `scripts/dev/fmt_rust.sh`. Like h3_wasm_parity it skips gracefully when
# cargo or a pinned toolchain is absent, so local lint stays green for contributors
# not touching Rust and CI is the enforcer; h3bridge's stable pin shares that
# step's assumption that CI's stable matches the committed formatting. The
# `if out=$(...)` capture is required under `set -e` since --check exits non-zero
# on drift.
if out=$(./scripts/dev/fmt_rust.sh --check 2>&1); then
    # fmt_rust.sh is verbose even when clean (per-crate headers + rustfmt.toml
    # unstable-option warnings), so keep the step concise on success like its
    # siblings and surface the full output only on drift.
    echo "passed"
    exit 0
fi
echo "$out"
exit 1
