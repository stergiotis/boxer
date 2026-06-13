#!/usr/bin/env bash
# Stage the prebuilt host binaries + assets, then build the runtime image.
# Prereqs (run once in rust/imzero2): ./build_rust_headless.sh && ./build_go.sh
# Engine override: ENGINE=docker ./build.sh   (default: podman)
set -euo pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
imz="$(dirname "$here")"   # rust/imzero2

[ -x "$imz/main_go" ] || { echo "missing $imz/main_go — run (cd $imz && ./build_go.sh)"; exit 1; }
[ -x "$imz/target/headless/release/imzero2" ] || { echo "missing headless client — run (cd $imz && ./build_rust_headless.sh)"; exit 1; }

rm -rf _stage && mkdir -p _stage
cp "$imz/main_go" _stage/main_go
cp "$imz/target/headless/release/imzero2" _stage/imzero2
cp -r "$imz/assets" _stage/assets

"${ENGINE:-podman}" build -t imzero2-demo:latest -f Dockerfile .
echo "OK: built imzero2-demo:latest"
