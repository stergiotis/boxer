#!/bin/bash
set -ev
# IMZERO2_BUILD_FEATURES appends extra (space-separated) cargo features to the
# release build; unset, the build is the desktop default (which already carries
# `inspection` for egui_mcp — doc/howto/egui-mcp.md).
cargo build --release --features "puffin${IMZERO2_BUILD_FEATURES:+ $IMZERO2_BUILD_FEATURES}"
