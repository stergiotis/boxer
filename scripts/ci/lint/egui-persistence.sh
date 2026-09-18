#!/bin/bash
# Refuses eframe's `persistence` feature in rust/imzero2/Cargo.toml.
#
# One lint step. Runnable on its own; scripts/ci/lint.sh runs it with the
# others and documents the exit-status contract every step obeys — here a
# non-zero exit fails the gate.

set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../../.."

# ADR-0148 (Update 2026-07-30): runtime and app state lives in boxer.facts
# and nowhere else. eframe's `persistence` feature enables
# `egui/persistence`, which serializes egui's IdTypeMap to a file under the
# home directory — and egui_table's TableState stores itself with
# insert_persisted while carrying col_widths. Enabling it would put column
# widths on disk AND make that copy authoritative, since TableState
# overrides the Go-supplied width on every frame after first show.
#
# A hard gate rather than a warning: the failure is silent at runtime (the
# app works, it just reads its widths from the wrong place), so review is
# the only other thing standing between a one-character edit and a second
# store. Matches the feature only when uncommented.
if grep -nE '^[[:space:]]*"persistence"' rust/imzero2/Cargo.toml; then
    echo "eframe 'persistence' is enabled in rust/imzero2/Cargo.toml — prohibited by ADR-0148."
    echo "egui memory would be serialized to disk, creating a second store for state that belongs in boxer.facts."
    exit 1
fi
echo "passed"
