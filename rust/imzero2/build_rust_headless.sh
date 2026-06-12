#!/bin/bash
# Build the headless render host (ADR-0024 SD1): no eframe/winit, hand-rolled
# egui_wgpu offscreen loop. Separate --target-dir so flipping between the
# desktop and headless feature sets doesn't thrash one incremental cache.
# Binary lands at target/headless/release/imzero2.
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
cargo build --release --no-default-features --features headless --target-dir target/headless
