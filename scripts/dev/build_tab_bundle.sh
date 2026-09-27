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
mkdir -p "$out/fonts"
out=$(cd "$out" && pwd)

# shellcheck source=/dev/null
source "$here/go-build-env.sh"
tags=$(cat "$root/tags")
echo "bundle: the Go tab host (wasip1 reactor)" >&2
# -s -w: the symbol and DWARF sections are of no use in a browser and cost
# about five percent. The size that remains is the linked graph's code —
# play alone is a third of it — so the lever for a smaller tab is which
# apps a build links, not flags; wasm-opt -Oz, when binaryen is installed,
# takes a further slice off (the h3 lane documents installing it).
# shellcheck disable=SC2086 # deliberate word splitting of the flag list
(cd "$root" && GOOS=wasip1 GOARCH=wasm go build $BOXER_GO_FLAGS -buildmode=c-shared -ldflags="-s -w" -tags "$tags" \
	-o "$out/imzero2tab.wasm" ./public/thestack/cmd/imzero2tab)
if command -v wasm-opt >/dev/null 2>&1; then
	wasm-opt -Oz --enable-bulk-memory --enable-sign-ext --enable-mutable-globals --enable-nontrapping-float-to-int \
		"$out/imzero2tab.wasm" -o "$out/imzero2tab.opt.wasm" && mv "$out/imzero2tab.opt.wasm" "$out/imzero2tab.wasm" \
		|| echo "bundle: wasm-opt failed; the unoptimised module stands" >&2
fi
echo "bundle: the Rust browser host (wasm32 cdylib)" >&2
(cd "$root/rust/imzero2" && ./build_rust_browser.sh >/dev/null)
cp "$root/rust/imzero2/target/browser/wasm32-unknown-unknown/release/imzero2_browser.wasm" "$out/"
cp "$root/public/thestack/imzero2/browserhost/web/"{bridge.mjs,worker.mjs} "$out/"
cp "$root/rust/imzero2/src/imzero2/viewer/index.html" "$out/index.html"

# shellcheck source=/dev/null
source "$root/rust/imzero2/font-resolve.sh"
imzero2_resolve_fonts 2>/dev/null || true
for slot in main:"${MAIN_FONT:-}" mono:"${MONO_FONT:-}" phosphor:"${PHOSPHOR_FONT:-}" fallback:"${FALLBACK_FONT:-}"; do
	name=${slot%%:*}; file=${slot#*:}
	if [[ -n "$file" && -f "$file" ]]; then cp "$file" "$out/fonts/$name.ttf"; else echo "bundle: no $name font; egui's default face" >&2; fi
done
echo "bundle: $out" >&2
du -sh "$out" | cut -f1 >&2
