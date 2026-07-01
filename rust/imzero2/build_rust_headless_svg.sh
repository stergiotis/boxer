#!/bin/bash
# Build the GPU-less, SVG-only host (headless_svg.rs): no eframe/winit, no
# wgpu/ffmpeg — the FFFI2 interpreter driven by ctx.run_ui, with the SVG-export
# plugin as the only pass consumer. Separate --target-dir so flipping between
# host feature sets doesn't thrash one incremental cache. Binary lands at
# target/headless_svg/release/imzero2.
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
cargo build --release --no-default-features --features headless_svg --target-dir target/headless_svg
