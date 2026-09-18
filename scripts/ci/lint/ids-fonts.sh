#!/bin/bash
# The per-directory SHA256SUMS over the committed IDS font binaries.
#
# One lint step. Runnable on its own; scripts/ci/lint.sh runs it with the
# others and documents the exit-status contract every step obeys — here a
# non-zero exit fails the gate.

set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../../.."

# IDS font binary hash pinning (ADR-0034 §SD5). Each per-directory
# SHA256SUMS verifies the committed .ttf bytes; drift fails the build
# with a structured error naming the file and expected vs observed SHA.
ids_fonts_dir="rust/imzero2/assets/fonts"
ids_fonts_ok=1
if [ -d "$ids_fonts_dir" ]; then
    for d in "$ids_fonts_dir"/*/; do
        if [ -f "$d/SHA256SUMS" ]; then
            (cd "$d" && sha256sum -c --quiet SHA256SUMS) || ids_fonts_ok=0
        fi
    done
fi
if [ "$ids_fonts_ok" -eq 0 ]; then
    exit 1
fi
echo "passed"
