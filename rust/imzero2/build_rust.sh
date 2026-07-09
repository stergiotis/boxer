#!/bin/bash
set -ev
# IMZERO2_BUILD_FEATURES appends extra (space-separated) cargo features to the
# release build. hmi.sh sets it to `inspection` when EGUI_INSPECTION is truthy
# (egui_mcp — doc/howto/egui-mcp.md); unset, the build is unchanged.
cargo build --release --features "puffin${IMZERO2_BUILD_FEATURES:+ $IMZERO2_BUILD_FEATURES}"
