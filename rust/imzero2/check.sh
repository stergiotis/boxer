#!/usr/bin/env bash
# This scripts runs various CI-like checks in a convenient way.
set -eux

cargo check --quiet --workspace --all-targets
# `-p` per package, not `--all`: cargo-fmt reaches path dependencies, and
# vendor/egui_software_backend is third-party source we do not reformat (see
# its VENDORING.md). Add new first-party packages here when the workspace grows.
cargo fmt -p imzero2 -p imzero2_egui -- --check
cargo clippy --quiet --workspace --all-targets --all-features --  -D warnings -W clippy::all
cargo test --quiet --workspace --all-targets --all-features
cargo test --quiet --workspace --doc
