#!/bin/bash
# ADR-0128 M3: build the LEAN mesh appliance host — the carrier + FFFI2
# interpreter + the mesh draw-stream lane (SD6) only, with NO wgpu and NO
# ffmpeg in the dependency tree. `cargo tree --features headless` carries no
# wgpu/naga/pollster, so the resulting binary needs no GPU/mesa/vulkan runtime
# and no ffmpeg: the host does `ctx.run` + `ctx.tessellate` + serialize and
# streams meshes to a WebGL2 viewer.
#
# The mesh wire is byte-identical to the full host (build_rust_headless.sh,
# `--features headless_wgpu`), so a stream verified there is exactly what this
# build serves. This is the deployment artifact; the full host is the dev one.
#
# Next milestones (deferred): a musl-static target for a self-contained binary,
# then a gokrazy QEMU boot probe (see ADR-0128 M3).
#
# Separate --target-dir so it doesn't thrash the full host's incremental cache.
# Binary lands at target/headless_mesh/release/imzero2.
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
cargo build --release --no-default-features --features headless --target-dir target/headless_mesh
