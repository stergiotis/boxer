#!/bin/bash
# Build the FULL headless render host (ADR-0024 SD1): no eframe/winit, the
# hand-rolled egui_wgpu offscreen loop with every video codec + PNG dump +
# H264_OUT. Since ADR-0128 M3 the wgpu renderer lives behind `headless_wgpu`
# (which pulls in `headless`), so this is the feature to build for the dev
# host that hmi_headless.sh runs. The lean mesh-only appliance host is
# build_rust_headless_mesh.sh (`--features headless`, no wgpu/ffmpeg).
# Separate --target-dir so flipping between the desktop and headless feature
# sets doesn't thrash one incremental cache. Binary lands at
# target/headless/release/imzero2.
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
cargo build --release --no-default-features --features headless_wgpu --target-dir target/headless
