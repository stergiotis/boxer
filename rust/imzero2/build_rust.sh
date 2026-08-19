#!/bin/bash
set -ev
# IMZERO2_BUILD_FEATURES appends extra (space-separated) cargo features to the
# release build; unset, the build is the desktop default plus `puffin`.
#
# Two dev capabilities therefore ship in every build here, and BOTH stay inert
# until their runtime variable asks for them — the variable, not the compile
# flag, is the switch in each case:
#   `inspection` (desktop default) — egui_mcp, gated by EGUI_INSPECTION
#                                    (doc/howto/egui-mcp.md)
#   `puffin`     (added below)     — profiler + loopback server on :8585,
#                                    gated by IMZERO2_PUFFIN (ADR-0195)
cargo build --release --features "puffin${IMZERO2_BUILD_FEATURES:+ $IMZERO2_BUILD_FEATURES}"
