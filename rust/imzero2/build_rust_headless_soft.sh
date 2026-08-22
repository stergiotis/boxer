#!/bin/bash
# Build the GPU-LESS pixel host: the same headless carrier + FFFI2 interpreter
# as build_rust_headless.sh, but with the frame rasterized on the CPU by the
# vendored egui_software_backend (vendor/egui_software_backend/VENDORING.md)
# instead of by wgpu against an offscreen texture.
#
# What that buys, and why it is a separate binary rather than a runtime flag:
# `cargo tree --features headless_soft` carries no wgpu, so the runtime image
# needs no Vulkan loader and no ICD (not even lavapipe) — which is the whole
# point, and a claim only a build without the dependency can make. Everything
# downstream of a frame is unchanged: PNG dump, ADR-0154 capture requests, the
# ffmpeg encoder lane and the carrier all take the same tightly-packed BGRA.
#
# Separate --target-dir, same reasoning as the other host scripts: flipping
# between feature sets otherwise thrashes one incremental cache. Binary lands
# at target/headless-soft/release/imzero2.
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
cargo build --release --no-default-features --features headless_soft,fast_alloc --target-dir target/headless-soft
