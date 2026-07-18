---
type: adr
status: proposed
date: 2026-07-18
---

# ADR-0128: imzero2 remote access — a mesh draw-stream codec lane beside video

## Context

[ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) chose pixel
streaming (O1) for remote access and recorded kill-reasons against its O5
("renderer-only in the browser, FFFI2 over WebSocket"): wasm bundle size, the
synchronous per-frame `Sync()` round-trips, and state replication.
[ADR-0077](./0077-keelson-browser-wasm-execution.md) re-examined that middle
form and reaffirmed pixel streaming for the remote problem. Both dialogues
predate a force that has since become concrete: the **appliance direction** —
running the demo box as a pseudo-unikernel-style image with a minimal,
statically-linked userland. Pixel streaming's server side is exactly the part
that resists this: software rasterization (mesa/llvmpipe under
`LIBGL_ALWAYS_SOFTWARE=1`) and an external `ffmpeg` encoder are a large,
dynamically-linked C closure, and they dominate the host's per-frame cost.
Measured on a desktop-class Zen 5 machine at 1280×800 @ 30 fps (the deployed
demo configuration): llvmpipe rasterization + readback costs 28.6 ms
(widgets) to 62 ms (imztop) of CPU per frame, and `libx264` encode a further
14–18 ms — versus ~3 ms for everything else the host does.

There is a third form neither prior ADR evaluated: keep keelson, the FFFI2
lockstep, and the egui context server-side exactly as today, but stream the
**tessellated meshes** (egui's `ClippedPrimitive` output) plus the texture
deltas to the browser, and paint them there with a trivial WebGL2 painter.
The FFFI2 protocol never touches the network, so ADR-0024's O5 kill-reasons
do not apply; layout and hit-testing stay server-authoritative, so viewers
remain mirrors; and the server sheds rasterization and encoding entirely.
Text needs nothing browser-side because egui's CPU-built font atlas ships as
an ordinary texture — the atlas *is* the text.

A working-tree spike (an env-gated probe + loopback serve in the headless
host, `IMZERO2_HEADLESS_MESH_*`; productization is M1) measured the open
questions; the numbers are recorded below and are part of this decision's
evidence.

## Spike evidence (2026-07-17/18)

Wire format measured: per mesh — clip `u16×4` (1/8 px), texture id `u32`,
vertices n×(pos `u16×2` @ 1/8 px, uv `u16norm×2`, rgba `u32` — 12 B/vertex),
indices `u16` (no demo mesh exceeded 64 Ki vertices). Dedup = per-mesh
content hash against the previous frame. 900-frame runs, 1280×800 @ 30 fps:

| scenario | full frame | full @30 fps | deduped @30 fps | changed meshes/frame |
| --- | --- | --- | --- | --- |
| app launcher | 54 KiB | 13.4 Mbit/s | 0.99 Mbit/s | 0.6 of 7 |
| widgets gallery | 33 KiB | 8.1 Mbit/s | 1.00 Mbit/s | 0.6 of 8 |
| imztop treemap (animated) | 138 KiB | 33.9 Mbit/s | 1.83 Mbit/s | 1.5 of 79 |

- **Steady state on the live connection: 85 B/frame** (ordered hash list,
  zero bodies) ≈ 20 kbit/s; 18–44 % of frames change nothing.
- **Bootstrap**: one 1 MiB atlas (512×512) + one 33.7 KiB all-bodies frame —
  the keyframe analogue. Atlas growth afterwards: ~31 KiB over 30 s of the
  most text-hungry scenario.
- **Compression headroom**: zstd −19 shrinks a full frame 3.2× (33.6 →
  10.6 KB), so even whole-frame retransmission is ~2.6 Mbit/s.
- **Server cost of the lane**: tessellate + serialize + hash =
  0.13–0.46 ms/frame; total host CPU with rasterization skipped was
  1.0–2.4 ms/frame. The `need_pixels` skip path in the headless loop is
  already the no-raster host shape.
- **`Primitive::Callback`: zero occurrences** across launcher, widgets
  gallery, and imztop — measured, not assumed.
- **Fidelity**: a from-scratch software rasterizer consuming only the dumped
  wire bytes reproduced the host's own PNG of the same frame to visual
  identity (2 % mean absolute pixel difference, all at AA edges); a ~120-line
  browser WebGL2 painter (one shader, per-mesh scissor, premultiplied blend)
  rendered it live.
- **Interactive session** (loopback, input injected at the carrier's event
  seam): scrolling was subjectively fine; multiple tabs mirrored the session
  with content-addressed bodies uploading once per connection. Two observed
  defects, both with known causes: text renders slightly soft on a HiDPI
  display (the spike tessellates at `pixels_per_point = 1` and the browser
  upscales — the production lane must adopt the viewer's devicePixelRatio via
  the existing ViewportResize path, as the video lane already does), and
  slightly dull (the spike shader approximates egui's gamma-space blending;
  the production painter must replicate `egui_glow`'s shader semantics).

## Decision

Add a **draw-stream codec lane** to the remote-access carrier: tessellated
meshes + texture deltas over the existing WebSocket session, negotiated per
viewer through the [ADR-0088](./0088-imzero2-runtime-codec-pipeline-and-viewer-capabilities.md)
capability handshake, coexisting with the H.264/VP9/AV1 lanes. Keelson, the
FFFI2 lockstep, layout, and hit-testing stay server-side; the carrier's
session, roster, auth, and input machinery are unchanged.

### SD1 — Wire format: quantized meshes, content-addressed bodies

The spike format is adopted as the v1 lane payload: per-frame an ordered list
of per-mesh content hashes plus the bodies the connection has not yet been
sent; bodies are immutable once named (content-addressed), so client-side
they become static GPU buffers. Quantization: positions `u16` at 1/8 px in
viewer-pixel space, uvs `u16norm`, colors `u32` premultiplied sRGB, indices
`u16` with a `u32` escape. A joiner bootstraps from the current texture store
plus one all-bodies frame — no keyframe scheduling, no GOP.

### SD2 — Texture plane: `TexturesDelta` verbatim

egui's texture deltas map 1:1 onto whole/partial texture messages
(`texSubImage2D` client-side). The font atlas ships once and grows
incrementally; image-widget uploads ship once and are then referenced by id —
strictly better than video, which re-encodes them every frame.

### SD3 — Lane negotiation and fallback

The lane registers in the ADR-0088 capability handshake and runtime-switch
seam (`setVideoPipeline`). Viewers without WebGL2, and content the lane
cannot carry, fall back to a video lane. `Primitive::Callback` is the
sentinel: none exist today (measured), and a callback appearing at runtime
forces the session onto video rather than silently dropping content. In an
appliance build that compiles only the mesh lane, a callback is a startup
error, not a fallback.

### SD4 — Viewer painter: small, exact, DPR-aware

The painter stays deliberately small (one shader, scissor, premultiplied
blend, static buffers per body), but two things are contractual, both learned
from the spike: it replicates `egui_glow`'s gamma/blending semantics exactly,
and the host tessellates at the active viewer's devicePixelRatio × zoom via
the existing ViewportResize handling, so glyphs are atlas-native at the
viewer's density. Golden images never compare across lanes or backends.

### SD5 — Session semantics unchanged

Input, clipboard, the active/passive roster
([ADR-0086](./0086-imzero2-active-passive-viewers-and-roster.md)), takeover,
and the auth/TLS boundary ([ADR-0082](./0082-imzero2-remote-session-auth-tls.md))
are untouched: the lane replaces the *pixel* payload, not the session. The
spike validated input injection at the same host seam the carrier uses.

### SD6 — The appliance host shape

With only the mesh lane compiled, the headless host needs neither wgpu nor
an encoder: its per-frame work is `ctx.run` + tessellate + serialize
(≈ 0.5 ms measured). That makes a statically-linked host binary with no GL,
no mesa, and no ffmpeg feasible — the enabling step for the appliance/
pseudo-unikernel target, without writing a CPU rasterizer at all.

### SD7 — Deferrals, recorded

- **Mesh-granularity splitting.** egui batches a layer into few meshes, so
  one changed glyph re-sends its ~4–7 KiB batch — the measured ~1 Mbit/s
  animated floor. Splitting batches would shrink deltas; deferred until a
  real deployment shows the floor matters.
- **Frame compression** (zstd, measured 3.2×) and **WebTransport** — wire
  optimizations, deferred.
- **Multi-DPR re-tessellation** (per-viewer-class geometry) — passive viewers
  scale, as they do under video today.
- **IME**, clipboard beyond ADR-0082 SD6, and touch input — carried by the
  session layer when it grows them; nothing lane-specific.

### Milestones

- **M1** — lane in the carrier behind ADR-0088 negotiation; probe instrument
  retired into it.
- **M2** — painter page to product grade: gamma parity, DPR adoption,
  reconnect.
- **M3** — appliance host feature (mesh lane only, no wgpu/ffmpeg deps).
- **M4** — runtime fallback policy (callback sentinel, bandwidth guard).

## Alternatives

- **Pixel streaming only (status quo).** Reaffirmed twice under premises
  that did not include the appliance force; its server side (mesa + ffmpeg)
  is the appliance's whole C-closure problem, and it re-encodes static
  screens forever. Not killed — it remains the negotiated fallback for
  pathological content and non-WebGL2 viewers — but demoted from *only lane*
  to *sibling lane*.
- **Widget-level remote shapes** (ADR-0024 O5; ADR-0077 "remote shapes"):
  moves the FFFI2 lockstep onto the network (per-frame RTT; ~250–330
  KB/frame Go→Rust) and the egui context into each tab (per-viewer layout,
  session authority dissolves). Disposition unchanged: excluded for the
  remote problem.
- **Shape-level streaming** (pre-tessellation `ClippedShape`s): requires
  fonts, shaping, and layout client-side — per-viewer divergence and a much
  wider serialization surface. The mesh level's atlas-as-texture property is
  precisely what this loses. Killed.
- **Server-side CPU rasterizer + video** (pure-Rust raster host feeding the
  encoder): viable and universality-preserving, but it keeps the encoder
  dependency, costs an estimated 5–15 ms/frame where the lane costs 0.5, and
  ships lossy text. Retained only as the way to keep a video lane alive on
  an appliance image if that ever becomes a requirement.
- **SVG streaming** (the svgexport walker exists): no per-frame delta story,
  DOM churn at 30 fps, and text-as-SVG needs fonts client-side. Killed;
  svgexport remains the export/tooling seam.

## Consequences

### Positive

- The appliance host loses its entire per-frame C dependency surface
  (mesa, ffmpeg) and ~95 % of its per-frame CPU; idle sessions cost
  ~20 kbit/s and no rasterization anywhere.
- Text can be atlas-crisp at the viewer's native density; images ship once.
- Two frames of latency (encode + decode) disappear from the
  input-to-photon path.

### Negative

- A second viewer painter to keep correct (gamma, DPR, reconnect) beside the
  WebCodecs one; the wire format becomes a versioned compatibility surface.
- Pixel output is not bit-identical to the wgpu/video path — golden images
  and the screenshot tour must never compare across lanes.
- Bandwidth is bursty rather than constant: full-invalidation worst case
  measured 8–34 Mbit/s raw (÷3.2 compressed) versus video's steady rate —
  fine on LAN, needs the M4 guard before WAN exposure.

### Neutral

- The video lanes, roster, auth, and input protocol are untouched; the lane
  is additive within the ADR-0088 negotiation.
- ADR-0024's O1 decision stands for what it decided; this ADR narrows its
  scope from "the remote answer" to "a remote lane", under a force it did
  not weigh.

## Status

Proposed. Spike evidence measured 2026-07-17/18 on the demo carousel
(launcher, widgets, imztop) at 1280×800 @ 30 fps; interactive loopback
session exercised input, scrolling, and multi-viewer fan-out. M1 is the
acceptance gate.

## References

- [egui #1129 — epaint software renderer](https://github.com/emilk/egui/issues/1129)
  — upstream thread whose trajectory (tiny-skia PoC, missing vertex API,
  "render the triangles manually") independently converges on mesh-level
  consumption.
- `egui_glow` painter — the gamma/blending semantics the viewer painter must
  replicate.

### Related ADRs

- [ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) — pixel
  streaming; the sibling lane and the O5 kill-reasons this lane routes
  around.
- [ADR-0077](./0077-keelson-browser-wasm-execution.md) — keelson-in-browser;
  its "remote shapes" disposition and SD3 fetch coalescing.
- [ADR-0088](./0088-imzero2-runtime-codec-pipeline-and-viewer-capabilities.md)
  — the codec negotiation this lane registers in.
- [ADR-0082](./0082-imzero2-remote-session-auth-tls.md),
  [ADR-0086](./0086-imzero2-active-passive-viewers-and-roster.md) — session
  boundary and roster, unchanged.
- [ADR-0062](./0062-imzero2-render-cadence.md) — reactive cadence; the
  idle-silence half of the bandwidth story.
