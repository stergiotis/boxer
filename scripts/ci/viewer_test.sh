#!/bin/bash
# ADR-0243: dependency-light correctness lane; target runs are separate.
set -euo pipefail
repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$repo/rust/imzero2-viewer"
source "$repo/scripts/dev/rust-repro-env.sh"
cargo fmt --check
cargo clippy --locked --all-targets -- -D warnings
cargo test --locked
