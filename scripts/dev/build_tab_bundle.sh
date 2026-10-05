#!/bin/bash
# build_tab_bundle.sh — assemble a servable directory that runs imzero2 in a
# browser tab (ADR-0263): the Rust browser host and the Go tab host as wasm
# modules, the worker and WASI shim that join them, the viewer page that
# paints, and the fonts the host loads as bytes.
#
#   scripts/dev/build_tab_bundle.sh <out-dir>
#   go run ./public/thestack/cmd/imzero2tab serve --dir <out-dir>   # then open
#   http://127.0.0.1:8765/index.html?worker=worker.mjs%3Fapp%3Dgithub.com%252Fstergiotis%252Fboxer%252Fapps%252Fplay
#
# The work is the tab binary's own `bundle` subcommand (ADR-0278 SD4,
# proposed), which a module consuming boxer runs the same way on its own tab
# binary; this script only names boxer's. --withAssets writes the page,
# worker and shim too, so the directory serves from any static host, not only
# from `imzero2tab serve`, which embeds them.
#
# `imzero2tab serve` proxies /ch/ to a ClickHouse HTTP endpoint (--chURL,
# default http://127.0.0.1:8123/); the worker hands the module CLICKHOUSE_URL
# for it.
# Fonts follow rust/imzero2/font-resolve.sh (MAIN_FONT, MONO_FONT,
# PHOSPHOR_FONT, FALLBACK_FONT override); a slot with no file is left to
# egui's default face. See doc/howto/imzero2-in-the-browser.md.
set -euo pipefail
out=${1:?usage: build_tab_bundle.sh <out-dir>}
here=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")
root=$(cd "$here/../.." && pwd)
mkdir -p "$out"
out=$(cd "$out" && pwd)
cd "$root"
go run ./public/thestack/cmd/imzero2tab bundle --out "$out" --withAssets
du -sh "$out" | cut -f1 >&2
