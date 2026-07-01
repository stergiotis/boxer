---
type: how-to
audience: engineer with a specific task
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to serve imzero2 views as SVG over HTTP

`apps/svgserver` renders an imzero2 (egui) view and returns it as an SVG
document over HTTP. It drives the GPU-less `headless_svg` imzero2 client — a
third host beside the eframe desktop and the wgpu pixel-streaming headless host
of [ADR-0024](../adr/0024-imzero2-remote-access-browser-viewer.md). The client
renders each request's window and exports it with `ExportSvgWindow`, reading
egui's pre-tessellation shapes, so no GPU is involved. This is a prototype — read
[Tradeoffs](#tradeoffs) before leaning on it.

## Build

Two binaries: the Rust client and the Go server.

```sh
# 1. the GPU-less SVG client → rust/imzero2/target/headless_svg/release/imzero2
bash rust/imzero2/build_rust_headless_svg.sh

# 2. the HTTP server
go build -tags="$(cat ./tags)" -o /tmp/svgserver ./apps/svgserver/
```

## Run

Point the server at the client binary. Fonts are optional but recommended: with
them, exported SVGs embed a subset of each used face and are self-contained;
without them the SVG references generic family names and depends on the viewer's
installed fonts.

```sh
/tmp/svgserver \
  -addr :8087 \
  -clientBinary rust/imzero2/target/headless_svg/release/imzero2 \
  -mainFontTTF rust/imzero2/assets/fonts/iosevka-aile/IosevkaAile-Regular.ttf \
  -monoFontTTF rust/imzero2/assets/fonts/ids-mono/IDSMono-Regular.ttf \
  -phosphorFontTTF rust/imzero2/assets/fonts/phosphor/Phosphor.ttf
```

Open `http://localhost:8087/` for a demo page, or request an SVG directly:

```sh
curl 'http://localhost:8087/svg?title=Report&body=alpha%3A%201%0Abeta%3A%202' > report.svg
```

## Request API

`GET /svg`, with query parameters:

| Param   | Default              | Meaning |
|---------|----------------------|---------|
| `title` | `imzero2 SVG report` | Window title and heading. |
| `body`  | a demo payload       | Report body; a newline (URL-encoded `%0A`) starts a new line. |
| `mode`  | `content`            | `content` crops window chrome to the body; `faithful` keeps the whole window frame. |
| `bg`    | `1e1e1eff`           | Background as `RRGGBBAA` hex, or `transparent` to omit the background rect (the host page shows through). |
| `embed` | `true`               | Embed subsetted fonts (`true`) or reference generic family names (`false`). |

`GET /` serves a small HTML page that embeds a sample render.

To change what a request draws, edit `renderReport` in
`apps/svgserver/svgserver.go`: swap the demo labels for whatever imzero2 widgets
you need (tables, plots, cards). Everything the widget paints is exported.

## Tradeoffs

- **One request at a time.** A single render loop serialises all requests;
  throughput is bounded by the per-frame round-trip. Fine for dashboards,
  reports, and low request rates — not a high-concurrency service. Scaling today
  means running several instances behind a load balancer; genuine per-request
  concurrency would need per-request egui contexts, which this prototype does
  not do.
- **A few frames of latency.** The client writes the SVG to a temp file that the
  server polls, after a short settle so the window's auto-size is final. That is
  a handful of round-trips — small, but not zero.
- **SVG only, no pixels.** Having no GPU is what makes this host containerisable,
  but it also cannot produce PNG/framebuffer output. For rasters, use the wgpu
  headless host of [ADR-0024](../adr/0024-imzero2-remote-access-browser-viewer.md)
  instead.
- **Output size scales with text.** The exporter emits one `<text>` element per
  glyph, so text-heavy views produce many elements. Embedding fonts adds a
  one-time ~30–80 KB per used face but makes the SVG self-contained and
  pixel-faithful; without embedding, glyph positioning depends on the viewer
  having a matching font.
- **Not everything translates.** Vector content — text, shapes, plots, tables —
  maps cleanly. Textured images embed only when their pixels are in the texture
  cache; custom wgpu paint callbacks become placeholder rectangles; walkers map
  tiles are not captured. Check the output for anything painted through those
  paths.
- **Background versus theme.** egui's default theme is light text on a dark
  surface. `bg=transparent` suits embedding in a dark-themed page, but on a white
  page the text is invisible — use an opaque `bg` for standalone viewing.
- **`content` mode crops tight.** ContentOnly can clip a pixel or two off the
  left (a viewBox margin approximation in `svgexport.rs`); use `mode=faithful` if
  that matters.
- **Prototype, not hardened.** No authentication, TLS, request-size limits, or
  response caching. Put it behind something that provides those before exposing
  it beyond localhost.
