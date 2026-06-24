#!/bin/bash
# Build, clippy, and test the rust/watermark crate (tiled luminance-grid
# watermark). Standalone — deliberately NOT wired into scripts/ci/lint.sh,
# because the codec round-trip tests shell out to ffmpeg and the noise sweep is
# slow; run this explicitly in CI or before touching the crate.
#
# Graceful skip when cargo (or the pinned toolchain) is absent: prints a
# 'skipped' line and exits 0, so contributors without the Rust toolchain still
# see green. The Stage 8/9 codec tests self-skip when ffmpeg is not on PATH.

set -e
set -o pipefail

here=$(dirname "$(readlink -f "$BASH_SOURCE")")
crate_dir="$here/../../rust/watermark"
cd "$crate_dir"

if ! command -v cargo >/dev/null 2>&1; then
    echo "watermark_test: skipped (cargo not installed)"
    exit 0
fi

# The crate pins toolchain 1.92 via rust-toolchain.toml; if it is not installed
# and cannot be resolved, skip rather than fail a toolchain-less environment.
if ! cargo --version >/dev/null 2>&1; then
    echo "watermark_test: skipped (pinned toolchain unavailable)"
    exit 0
fi

if ! command -v ffmpeg >/dev/null 2>&1; then
    echo "watermark_test: note: ffmpeg not on PATH — Stage 8/9 codec tests will self-skip"
fi

echo "watermark_test: clippy"
cargo clippy --all-targets -- -D warnings

echo "watermark_test: test (release)"
cargo test --release

echo "watermark_test: ok"
