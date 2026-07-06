#!/bin/bash
# Format every Rust crate under ./rust with its OWN pinned toolchain.
#
# Each crate carries a rust-toolchain{,.toml} file (rust/imzero2 + rust/watermark
# pin 1.92 -> rustfmt 1.8.0; rust/h3bridge pins stable). Running `cargo fmt` from
# *inside* the crate directory makes the rustup proxy resolve that crate's pin,
# so the committed Rust stays byte-stable regardless of which rustfmt happens to
# be the machine's default toolchain.
#
# This exists because the egui2gen generator used to shell out to the bare
# `rustfmt` on PATH from the repo root — i.e. the DEFAULT toolchain, a newer
# rustfmt — which reformatted the generated Rust differently from the pin and
# produced spurious reformatting diffs. The generator now formats through the pin
# (see rustfmtFile in egui2_driver_driver.go); this script is the whole-tree
# counterpart for hand-written source and one-shot cleanups.
#
# Usage:
#   scripts/dev/fmt_rust.sh            # format in place
#   scripts/dev/fmt_rust.sh --check    # verify only; non-zero exit on drift (CI)
#
# Graceful skip when cargo is absent, matching scripts/ci/watermark_test.sh, so
# a toolchain-less checkout still goes green.
#
# NOTE: rust/h3bridge pins the rolling `stable` channel, so its formatting is
# only reproducible until the next stable release; the 1.92-pinned crates are
# fully reproducible.

set -e
set -o pipefail

here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."

fmt_args=()
mode="write"
if [ "${1:-}" = "--check" ]; then
    # cargo-fmt's own --check (not `-- --check`): it aggregates the exit code
    # across all workspace members, whereas the pass-through form drops drift in
    # non-final members and can spuriously report a dirty workspace as clean.
    fmt_args=(--check)
    mode="check"
elif [ -n "${1:-}" ]; then
    echo "usage: $0 [--check]" >&2
    exit 2
fi

if ! command -v cargo >/dev/null 2>&1; then
    echo "fmt_rust: skipped (cargo not installed)"
    exit 0
fi

# Top-level crate manifests only (depth 2): rust/<crate>/Cargo.toml. Workspace
# members such as rust/imzero2/imzero2_egui are covered by `cargo fmt --all` on
# their workspace root, so they are intentionally not visited on their own.
mapfile -t manifests < <(find rust -mindepth 2 -maxdepth 2 -name Cargo.toml | sort)
if [ "${#manifests[@]}" -eq 0 ]; then
    echo "fmt_rust: no crates found under ./rust" >&2
    exit 1
fi

rc=0
for manifest in "${manifests[@]}"; do
    crate=$(dirname "$manifest")
    echo "=== cargo fmt --all ($mode): $crate ==="
    # Subshell + cd so the rustup proxy resolves this crate's pinned toolchain.
    if ! ( cd "$crate" && cargo fmt --all "${fmt_args[@]}" ); then
        rc=1
        [ "$mode" = "check" ] && echo "fmt_rust: $crate is not formatted (run without --check to fix)" >&2
    fi
done

if [ "$rc" -eq 0 ]; then
    echo "fmt_rust: ok ($mode)"
fi
exit $rc
