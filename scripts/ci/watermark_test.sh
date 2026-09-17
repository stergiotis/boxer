#!/bin/bash
# Required watermark correctness and codec lanes (ADR-0241). Missing tools fail;
# use cargo test --locked directly for the dependency-free correctness lane.
set -euo pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
repo_root="$here/../.."
cd "$repo_root/rust/watermark"
source "$repo_root/scripts/dev/rust-repro-env.sh"
cargo --version
ffmpeg -version >/dev/null
encoders=$(ffmpeg -hide_banner -encoders 2>/dev/null)
for encoder in libx264 libvpx-vp9 libsvtav1; do
    if ! grep -q " $encoder " <<<"$encoders"; then
        printf 'watermark_test: missing encoder %s\n' "$encoder" >&2
        exit 1
    fi
done
cargo fmt --check
cargo clippy --locked --all-targets -- -D warnings
cargo test --locked
# Select acceptance tests exactly, leaving the optional quality sweep out.
cargo test --locked --release --test s8_codec codec_roundtrip_single_tile -- --ignored --exact
cargo test --locked --release --test s9_crop every_crop_recovers_payload -- --ignored --exact
cargo test --locked --release --test codec_pathological -- --ignored
