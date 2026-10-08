#!/bin/bash
# Production build of one imzero2 host under the `dist` cargo profile (LTO,
# one codegen unit, stripped; see [profile.dist] in Cargo.toml). The
# development builds are the build_rust*.sh scripts beside this one; they stay
# on `release` and are what hmi.sh and the scene runner pick up.
#
# Usage: ./build_rust_dist.sh [desktop|headless|headless_mesh|headless_soft|headless_svg]
#
# Each host takes the same feature set and target directory as its development
# script, minus the dev tooling: `desktop` drops `puffin` and the default
# `inspection` (egui_mcp). The headless hosts never carried either. The binary
# lands at target/<host dir>/dist/imzero2 — beside, not over, the release one.
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
host="${1:-desktop}"
case "$host" in
    desktop)       features="desktop";        target_dir="target" ;;
    headless)      features="headless_wgpu";  target_dir="target/headless" ;;
    headless_mesh) features="headless";       target_dir="target/headless_mesh" ;;
    headless_soft) features="headless_soft";  target_dir="target/headless-soft" ;;
    headless_svg)  features="headless_svg";   target_dir="target/headless_svg" ;;
    *) echo "build_rust_dist.sh: unknown host '$host'" >&2; exit 2 ;;
esac
# Byte-reproducible output: path remapping, and --locked so the graph is the
# committed one (ADR-0215).
# shellcheck source=/dev/null
source "$here/../../scripts/dev/rust-repro-env.sh"
cargo build --profile dist --locked --no-default-features --features "$features" --target-dir "$target_dir"
