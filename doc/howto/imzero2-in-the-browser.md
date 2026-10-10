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

1. **Build the bundle.** The script runs the tab binary's `bundle`
   subcommand, which builds the Go tab host as a wasip1 reactor
   (`public/thestack/cmd/imzero2tab`) and the Rust browser host as a wasm32
   cdylib (`rust/imzero2/browser`), and writes the fonts, the worker, the
   WASI shim and the viewer page beside them. Fonts follow
   `rust/imzero2/font-resolve.sh`; set `MAIN_FONT` and friends to pin faces,
   or pass `--fonts <dir>` to `bundle`. A slot with no file is left to egui's
   default face. Inside boxer the Rust host is built from source, which
   needs cargo and the `wasm32-unknown-unknown` target; `bundle --host
   <file>` takes a prebuilt one instead. From a module that depends on boxer,
   `bundle` fetches the host published for its pin by the digest in
   `browserhost.sum` (`--hostFrom` chooses; `BOXER_TAB_HOST_URL` points at a
   mirror).

   After changing the browser host's sources or regenerating the bindings,
   refresh the recorded digest, or the `tab-host` workflow and a tabhost test
   fail:

   ```bash
   go run ./public/thestack/cmd/imzero2tab hostdigest --write
   ```

   ```bash
   scripts/dev/build_tab_bundle.sh /tmp/tab
   ```

   The worker refuses a Go module and a browser host generated from
   different versions of the bindings, and says so in the page's status
   line; rebuild both from one tree.

2. **Serve it.** The same binary, built natively, serves the directory and
   proxies `/ch/` to ClickHouse, so the data plane stays same-origin;
   `--chURL` points it elsewhere and `--listen` moves it. It is a
   development server: no auth, no TLS. The page, worker and shim are
   embedded in the binary and served from there when the directory lacks
   them; a file in the directory wins, which is how a harness brings its
   own page.

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
   links in: play, mdedit, taskdemo, fibscope and the committed SQL applets,
   each under `github.com/stergiotis/boxer/apps/sqlapplet/<slug>`), `env=NAME=value`
   (repeatable; `CLICKHOUSE_URL` defaults to the page's `/ch/`), `arg=`
   for further module flags, `stage=WxH` for the initial viewport,
   `<slot>Font=<url>` for a font, `cadence=continuous` to tick at `fps`
   instead of on demand, and `log=1` to forward the module's stderr to the
   server's output. play's own seed variables (`BOXER_PLAY_SQL`,
   `BOXER_PLAY_AUTORUN`, the `BOXER_PLAY_FOCUS_*` family,
   [doc/env-vars.md](../env-vars.md)) go through `env=`.

   A remote server works the same way: `--chURL` points `/ch/` at it and
   `env=CLICKHOUSE_USER=<user>` names the user. Checked on 2026-10-03 against
   ClickHouse's public ADS-B instance (`--chURL` its HTTPS endpoint,
   `env=CLICKHOUSE_USER=website`): play ran in the tab, learned that the user
   is read-only ([ADR-0181](../adr/0181-leeway-dql-authoring-surface.md)
   Update 2026-10-01) and drew the Map's world raster. That endpoint refused a
   connection now and then; a refused run shows its error in the pane and the
   last good raster stays.

4. **Or open an applet document the server holds.** Put the document — the
   [ADR-0132](../adr/0132-sqlapplet-sql-defined-applets.md) shape, its base
   name the slug — anywhere the page's server serves, for `serve` inside the
   bundle directory, and name its path with `env=`, relative to the page or
   absolute on its origin:

   ```
   http://127.0.0.1:8765/index.html?worker=worker.mjs%3Fenv%3DBOXER_SQLAPPLET_TAB_DOC%253Dapplets%252Fmy-applet.md
   ```

   The tab fetches it at start and mounts it in place of `app=`. A path that
   leaves the page's origin, a document whose buffer is not read-class, and
   one whose slug a committed applet holds are refused, and the reason is
   drawn in the tab
   ([ADR-0299](../adr/0299-sql-applets-a-tab-loads-from-its-origin.md)). An `endpoint: introspection` applet and one that declares
   `datasets:` do not run in a tab. A document whose rows are literals —
   `boxer code analysis sccapplet` writes one — runs with no database when
   `CLICKHOUSE_URL` names the in-process endpoint,
   `http://keelson.invalid/query`.

## Verification

The page's status line reads `connected — WxH @ppp P — N frames painted
(mesh lane)` and `cadence: reactive`; the frame count grows while
something animates and stops when nothing does. With play open, a query
returns rows into the Table pane; with `log=1` in the worker's query the
module's own log lines appear in the server's output. `P` is the page's devicePixelRatio, which reaches egui's pixels per point.
A headless capture follows the same URL; `CDP_SCALE=2` emulates a 2× display
and `CDP_AFTER_CLICK_MS` lengthens the wait after the optional click:

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
- **The HTTP egress service** (ADR-0262). Map basemap tiles are fetched by
  the tab itself instead, straight from the tile server (OpenStreetMap
  unless `BOXER_MAP_TILE_URL` names another), and the map panes start with
  the basemap on — so a tab with a map reaches that server from every
  viewer's browser (ADR-0262 Update 2026-10-03).
- **Live query progress.** The transport that reads ClickHouse's progress
  headers as they stream speaks HTTP over a raw socket; under wasm the
  stock client is used and a run reports when it completes.
- **A non-blocking request on the render goroutine.** Requests from query
  lanes, tile loaders and other goroutines go out asynchronously and the tab
  keeps painting; one made on the goroutine running `setup` or a frame is
  performed synchronously and stops the tab until it returns (ADR-0263
  Update 2026-10-03).
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
