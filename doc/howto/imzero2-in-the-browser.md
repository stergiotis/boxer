---
type: how-to
audience: engineer with a specific task
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to run an imzero2 app in a browser tab

Build the two wasm modules and the page around them into one directory,
serve it, and open an app in a tab. This covers the shape
[ADR-0263](../adr/0263-imzero2-browser-both-modules-in-one-worker-mesh-to-the-painter.md)
decided — both modules in one Web Worker, the mesh posted to the existing
viewer page — and what the tab does and does not have. It does not cover
serving a tab to other people: there is no auth, no session, and the dev
server here is a dev server.

## When to use this recipe

You want to see a keelson app run with no native process behind it: in a
browser, from a static directory, with ClickHouse reached through the
page's origin. The measurement side of the same shape — how much a frame
costs under wasm, and why — is the
[keelson-wasm-frame-cost](../trials/keelson-wasm-frame-cost/README.md)
trial, not this page.

## Prerequisites

- The Go toolchain go.mod names, and a Rust toolchain at least the
  `rust-version` in [rust/imzero2/Cargo.toml](../../rust/imzero2/Cargo.toml)
  with the `wasm32-unknown-unknown` target installed.
- A ClickHouse HTTP endpoint reachable from the machine that serves the
  page (default `http://127.0.0.1:8123/`), if the app queries.
- A browser with WebGL 2. Headless captures with Chromium need
  `--enable-unsafe-swiftshader` for it.

## Steps

1. **Build the bundle.** The script builds the Go tab host as a wasip1
   reactor (`public/thestack/cmd/imzero2tab`), the Rust browser host as a
   wasm32 cdylib (`rust/imzero2/browser`), and copies the worker, the WASI
   shim, the viewer page and the fonts beside them. Fonts
   follow `rust/imzero2/font-resolve.sh`; set `MAIN_FONT` and friends to
   pin faces. A slot with no file is left to egui's default face.

   ```bash
   scripts/dev/build_tab_bundle.sh /tmp/tab
   ```

2. **Serve it.** The same binary, built natively, serves the directory and
   proxies `/ch/` to ClickHouse, so the data plane stays same-origin;
   `--chURL` points it elsewhere and `--listen` moves it. It is a
   development server: no auth, no TLS.

   ```bash
   go run ./public/thestack/cmd/imzero2tab serve --dir /tmp/tab --listen 127.0.0.1:8765
   ```

3. **Open an app.** The page's `?worker=` parameter names the worker module
   and, inside it, the app and its environment. The inner query string is
   URL-encoded once.

   ```
   http://127.0.0.1:8765/index.html?worker=worker.mjs%3Fapp%3Dgithub.com%252Fstergiotis%252Fboxer%252Fapps%252Fplay
   ```

   The worker's parameters: `app=<id>` (a registered app the tab binary
   links in; play, mdedit, taskdemo and fibscope today), `env=NAME=value`
   (repeatable; `CLICKHOUSE_URL` defaults to the page's `/ch/`), `arg=`
   for further module flags, `stage=WxH` for the initial viewport,
   `<slot>Font=<url>` for a font, `cadence=continuous` to tick at `fps`
   instead of on demand, and `log=1` to forward the module's stderr to the
   server's output. play's own seed variables (`BOXER_PLAY_SQL`,
   `BOXER_PLAY_AUTORUN`, the `BOXER_PLAY_FOCUS_*` family,
   [doc/env-vars.md](../env-vars.md)) go through `env=`.

## Verification

The page's status line reads `connected — WxH @ppp 1 — N frames painted
(mesh lane)` and `cadence: reactive`; the frame count grows while
something animates and stops when nothing does. With play open, a query
returns rows into the Table pane; with `log=1` in the worker's query the
module's own log lines appear in the server's output. A headless capture follows the same URL:

```bash
node doc/trials/keelson-wasm-frame-cost/harness/cdp_shot.mjs \
  chromium --enable-unsafe-swiftshader -- "<url>" out.png 10000 1140x780
```

## Size

The Go module is the size of the graph it links, and Go's wasm code is
bulky. Measured on 2026-09-27 with symbols stripped (which is worth about
five percent):

| apps linked | module | brotli |
| --- | --- | --- |
| fibscope | 46 MB | 6.7 MB |
| + taskdemo | 47 MB | 6.8 MB |
| + mdedit | 63 MB | 8.4 MB |
| + play | 98 MB | 14 MB |

The Rust host is 8.5 MB (2.3 MB brotli). V8 compiles the 98 MB module in
about 0.35 s on a handheld (its baseline tier; optimisation follows in the
background) and caches the result; ADR-0077 accepts bundle size for the
LAN-served tier it targets. The lever for a smaller
tab is which apps a build links; a tab for one app should link that app.

## What the tab does not have

- **The runtime services.** Only the app's own bus client exists; a request
  to persist, appstate, fsbroker, adhocdata or the clipboard broker gets
  the bus's timeout. An app that needs one of them at mount does not
  mount.
- **Live query progress.** The transport that reads ClickHouse's progress
  headers as they stream speaks HTTP over a raw socket; under wasm the
  stock client is used and a run reports when it completes.
- **Sealed files and layered graph layouts**, which need `O_TMPFILE` and an
  embedded Graphviz runtime respectively; both fall back as their callers
  expect.
- **Any isolation credit.** A tab is a demo and per-user tier
  ([ADR-0087](../adr/0087-imzero2-client-compositor-compartmentalization.md)
  SD1), not an answer to the hostile-guest design.

## Related

- [ADR-0263](../adr/0263-imzero2-browser-both-modules-in-one-worker-mesh-to-the-painter.md) — the decision and its surfaces.
- [ADR-0077](../adr/0077-keelson-browser-wasm-execution.md) — the two-module plan this is a phase of.
- [keelson-wasm-frame-cost](../trials/keelson-wasm-frame-cost/README.md) — the measurements; quote figures from its §0.
