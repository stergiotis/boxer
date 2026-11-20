#!/bin/bash
# The egui2 IDL against its committed generated code, on both sides of the
# FFFI2 boundary: scripts/dev/boxer-idl-check.sh --force, run with a state
# directory of its own so it never shares a stamp with a launch script.
#
# One lint step. Runnable on its own; scripts/ci/lint.sh runs it with the
# others and documents the exit-status contract every step obeys — here a
# non-zero exit fails the gate.
#
# The check regenerates the Rust side, and the generator formats what it
# splices with rustfmt under rust/imzero2's pinned toolchain; without that
# toolchain the comparison would be meaningless. Like rust_imzero2_check.sh,
# the step then prints a 'skipped' line and exits 0 — a missing toolchain is
# not a finding about the repository, and the lint workflow installs it.
set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
root="$here/../../.."

if ! command -v cargo >/dev/null 2>&1; then
    echo "skipped: cargo not on PATH"
    exit 0
fi
if ! (cd "$root/rust/imzero2" && rustfmt --version >/dev/null 2>&1); then
    echo "skipped: rustfmt for rust/imzero2's pinned toolchain unavailable"
    exit 0
fi
GOWORK=off "$root/scripts/dev/boxer-idl-check.sh" --force --state lint-idl-check
echo "passed"
