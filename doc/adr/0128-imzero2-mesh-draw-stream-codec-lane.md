---
type: adr
status: proposed
date: 2026-07-18
---

# ADR-0128: imzero2 remote access — a mesh draw-stream codec lane beside video

## Context

[ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) chose pixel
streaming (O1) and killed its O5 — "renderer in the browser, FFFI2 over
WebSocket" — on wasm bundle size, per-frame `Sync()` round-trips, and state
replication; [ADR-0077](./0077-keelson-browser-wasm-execution.md) reaffirmed
both dispositions. Neither dialogue weighed a force that has since become
concrete: the **appliance direction** — a pseudo-unikernel-style image with a
minimal, statically-linked userland. Pixel streaming's server side is exactly
what resists it: mesa/llvmpipe software rasterization plus an external
`ffmpeg` are a large dynamically-linked C closure, and they dominate host
cost — measured at 1280×800 @ 30 fps (the deployed demo configuration, on a
desktop-class Zen 5 machine): 28.6–62 ms CPU/frame raster + readback and
14–18 ms `libx264`, against ~3 ms for everything else the host does.

A third form neither ADR evaluated: keep keelson, the FFFI2 lockstep, and
the egui context server-side unchanged, and stream the **tessellated
meshes** (`ClippedPrimitive`) plus texture deltas to a browser WebGL2
painter. FFFI2 never touches the network, so the O5 kill-reasons do not
apply; layout and hit-testing stay server-authoritative, so viewers remain
mirrors; the server sheds rasterization and encoding entirely. Text needs
nothing browser-side: egui's CPU-built font atlas ships as a texture — the
atlas *is* the text.

A working-tree spike (env-gated probe + loopback serve in the headless host,
`IMZERO2_HEADLESS_MESH_*`; productization is M1) measured the open
questions.

## Spike evidence (2026-07-17/18)

Wire format: per mesh — clip `u16×4` (1/8 px), texture id `u32`, vertices
n×(pos `u16×2` @ 1/8 px, uv `u16norm×2`, rgba `u32`; 12 B/vertex), `u16`
indices (no demo mesh nears 64 Ki vertices). Dedup = per-mesh content hash
against the previous frame. 900-frame runs, 1280×800 @ 30 fps:

| scenario | full frame | full @30 fps | deduped @30 fps | changed meshes/frame |
| --- | --- | --- | --- | --- |
| app launcher | 54 KiB | 13.4 Mbit/s | 0.99 Mbit/s | 0.6 of 7 |
| widgets gallery | 33 KiB | 8.1 Mbit/s | 1.00 Mbit/s | 0.6 of 8 |
| imztop treemap (animated) | 138 KiB | 33.9 Mbit/s | 1.83 Mbit/s | 1.5 of 79 |

- Steady state on the live connection: **85 B/frame** (hash list, no bodies)
  ≈ 20 kbit/s; 18–44 % of frames change nothing.
- Bootstrap: one 1 MiB atlas + one 33.7 KiB all-bodies frame — the keyframe
  analogue. Atlas growth after: ~31 KiB over 30 s of the most text-hungry
  scenario.
- zstd −19 shrinks a full frame 3.2×; whole-frame retransmission is
  ~2.6 Mbit/s.
- Lane server cost: tessellate + serialize + hash 0.13–0.46 ms/frame; whole
  host 1.0–2.4 ms/frame with rasterization skipped (the `need_pixels` skip
  path is already the no-raster host shape).
- `Primitive::Callback`: **zero occurrences** across launcher, widgets, and
  imztop — measured, not assumed.
- Fidelity: a from-scratch rasterizer consuming only the dumped wire bytes
  matched the host's own PNG of the same frame (2 % mean absolute diff, all
  at AA edges); a ~120-line WebGL2 painter rendered it live.
- Interactive loopback session: scrolling acceptable; multiple tabs mirror
  the session, bodies uploading once per connection. Initial text softness
  had two causes, one since **verified fixed**: tessellating at the viewer's
  density (`pixels_per_point = 2`) restored crisp text at **zero wire cost**
  (the all-bodies frame stayed 33.7 KiB — vertex counts are
  density-invariant; only coordinate values and the atlas scale) and
  requires input to cross in *points* (the viewer divides by the frame's
  ppp). The residual dullness is the spike shader approximating egui's
  gamma-space blending; exact `egui_glow` semantics are the M2 contract.
- Lifecycle defect found: the spike's serve thread keeps the client process
  alive past FFFI2 shutdown (an orphaned port-holder). M1 ties the lane to
  carrier lifecycle.

## Decision

Add a **draw-stream codec lane** to the remote-access carrier: tessellated
meshes + texture deltas over the existing WebSocket session, negotiated per
viewer through the
[ADR-0088](./0088-imzero2-runtime-codec-pipeline-and-viewer-capabilities.md)
capability handshake, coexisting with the H.264/VP9/AV1 lanes. Keelson,
FFFI2, layout, and hit-testing stay server-side; session, roster, auth, and
input machinery are unchanged.

### SD1 — Wire format: quantized meshes, content-addressed bodies

The spike format is v1: per frame, an ordered list of per-mesh content
hashes plus the bodies this connection has not seen. Bodies are immutable
once named, so client-side they become static GPU buffers. A joiner
bootstraps from the current texture store plus one all-bodies frame — no
keyframe scheduling, no GOP.

### SD2 — Texture plane: `TexturesDelta` verbatim

Whole/partial texture messages map 1:1 onto `texSubImage2D`. The atlas ships
once and grows incrementally; image uploads ship once and are referenced by
id — video re-encodes them every frame.

### SD3 — Lane negotiation and fallback

The lane registers in the ADR-0088 handshake and runtime-switch seam.
Viewers without WebGL2, and content the lane cannot carry, fall back to a
video lane. `Primitive::Callback` is the sentinel: none exist today
(measured); one appearing at runtime forces video rather than dropping
content, and is a startup error in an appliance build that compiles only
this lane.

### SD4 — Viewer painter: small, exact, density-aware

One shader, per-mesh scissor, premultiplied blend, static buffers. Two
things are contractual, both spike-learned: the painter replicates the host
renderer's semantics exactly — `egui_wgpu`'s `fs_main_gamma_framebuffer`
with `dithering: false`, i.e. the pure gamma pipeline (see Status for the
measured proof) — and the host tessellates at the active viewer's
devicePixelRatio × zoom via the existing ViewportResize handling (verified
crisp at zero wire cost), with input crossing in points. Golden images
never compare across lanes or backends.

### SD5 — Session semantics unchanged

Input, clipboard, roster
([ADR-0086](./0086-imzero2-active-passive-viewers-and-roster.md)), takeover,
and the auth/TLS boundary
([ADR-0082](./0082-imzero2-remote-session-auth-tls.md)) are untouched: the
lane replaces the pixel payload, not the session. The spike injected input
at the same host seam the carrier uses.

### SD6 — The appliance host shape

With only this lane compiled, the host needs neither wgpu nor an encoder:
per-frame work is `ctx.run` + tessellate + serialize (≈ 0.5 ms measured). A
statically-linked host with no GL, no mesa, no ffmpeg becomes feasible — the
enabling step for the appliance target, without writing a CPU rasterizer.

### SD7 — Deferrals, recorded

- **Mesh-granularity splitting** — egui batches a layer into few meshes, so
  one changed glyph re-sends its ~4–7 KiB batch (the measured ~1 Mbit/s
  animated floor). Deferred until a deployment shows the floor matters.
- **Frame compression** (zstd, 3.2× measured) and **WebTransport** — wire
  optimizations, deferred.
- **Multi-DPR re-tessellation** — passive viewers scale, as under video.
- **IME, touch, clipboard beyond ADR-0082 SD6** — session-layer concerns,
  nothing lane-specific.

### Milestones

- **M1** — lane in the carrier behind ADR-0088 negotiation, tied to carrier
  lifecycle (no serve outliving FFFI2 shutdown); probe retired into it.
- **M2** — painter to product grade: gamma parity, DPR adoption, reconnect.
- **M3** — appliance host feature (mesh lane only; no wgpu/ffmpeg deps).
- **M4** — runtime fallback policy (callback sentinel, bandwidth guard).

## Alternatives

- **Pixel streaming only (status quo).** Twice reaffirmed, but under
  premises without the appliance force; its server side is the appliance's
  whole C-closure problem, and it re-encodes static screens forever. Not
  killed — it remains the fallback lane — but demoted from *only lane* to
  *sibling lane*.
- **Widget-level remote shapes** (ADR-0024 O5; ADR-0077 "remote shapes").
  Moves the FFFI2 lockstep onto the network (per-frame RTT, ~250–330
  KB/frame) and the egui context into each tab. Disposition unchanged:
  excluded.
- **Shape-level streaming** (pre-tessellation `ClippedShape`s). Needs fonts,
  shaping, and layout client-side — per-viewer divergence, wider
  serialization surface, and it forfeits the atlas-as-texture property.
  Killed.
- **Server-side CPU rasterizer + video.** Viable and universal, but keeps
  the encoder, costs an estimated 5–15 ms/frame where the lane costs 0.5,
  and ships lossy text. Retained only as the way to keep a video lane on an
  appliance image if ever required.
- **SVG streaming** (the svgexport walker exists). No delta story, DOM churn
  at 30 fps, fonts client-side. Killed; svgexport remains the export seam.

## Consequences

### Positive

- The appliance host loses its per-frame C dependency surface (mesa,
  ffmpeg) and ~95 % of its per-frame CPU; idle sessions cost ~20 kbit/s and
  no rasterization anywhere.
- Atlas-crisp text at the viewer's density, at no wire cost; images ship
  once; encode + decode (~2 frames) leave the input-to-photon path.

### Negative

- A second viewer painter beside the WebCodecs one; the wire format becomes
  a versioned compatibility surface.
- Pixels are not bit-identical to the wgpu/video path — goldens and the
  screenshot tour never compare across lanes.
- Bandwidth is bursty, not constant: full-invalidation worst case 8–34
  Mbit/s raw (÷3.2 compressed). Fine on LAN; WAN exposure waits on the M4
  guard.

### Neutral

- Video lanes, roster, auth, and input protocol untouched; the lane is
  additive within ADR-0088 negotiation.
- ADR-0024's O1 stands for what it decided; this ADR narrows it from "the
  remote answer" to "a remote lane" under a force it did not weigh.

## Status

Proposed. Spike evidence measured 2026-07-17/18 on the demo carousel at
1280×800 @ 30 fps; interactive loopback session exercised input, scrolling,
multi-viewer fan-out, and the viewer-density fix (verified crisp). M1 is the
acceptance gate — **landed 2026-07-18** (`3567db31`: `VideoCodec::Mesh`
through the ADR-0088 seams, the `meshlane` wire module, `0x04` broadcast in
the carrier with per-connection content-addressed dedup, the WebGL2 painter
in the embedded viewer; spike retired). Verified over the carrier protocol
end-to-end, with the H.264 lane regression-tested beside it. M1's recorded
cut: the lane is session-global (per-viewer mixing stays M4), and a
mesh ↔ video runtime switch reloads the viewer page (a canvas is bound to
its first context kind).

**M2 landed 2026-07-18.** Gamma parity closed by proof rather than shader
work: the headless reference renders `fs_main_gamma_framebuffer` with
`dithering: false` and non-sRGB textures — the pure gamma pipeline the
painter already implements — so the painter changes reduce to `egui_wgpu`'s
exact `ScissorRect` rounding and an opaque backbuffer. An exact-pipeline
software oracle fed from the live wire measures **0.009 % mean absolute
difference** against the host's own wgpu render of the same screen (0.2 %
of pixels beyond 2/255, all AA-edge rasterization minutiae). The spike-era
"dull" was entirely the viewer-density issue already fixed at M1's DPR
adoption. Reconnect reviewed: content-addressed bodies stay valid across
reconnects and the fresh-connection bootstrap covers the rest — no code
needed. Awaiting acceptance review.

## References

- [egui #1129 — epaint software renderer](https://github.com/emilk/egui/issues/1129)
  — upstream thread independently converging on mesh-level consumption.
- `egui_glow` painter — the gamma/blending semantics SD4 requires.

### Related ADRs

- [ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) — pixel
  streaming; the sibling lane and the O5 kill-reasons this lane routes
  around.
- [ADR-0077](./0077-keelson-browser-wasm-execution.md) — keelson-in-browser;
  the "remote shapes" disposition and SD3 fetch coalescing.
- [ADR-0088](./0088-imzero2-runtime-codec-pipeline-and-viewer-capabilities.md)
  — the codec negotiation this lane registers in.
- [ADR-0082](./0082-imzero2-remote-session-auth-tls.md),
  [ADR-0086](./0086-imzero2-active-passive-viewers-and-roster.md) — session
  boundary and roster, unchanged.
- [ADR-0062](./0062-imzero2-render-cadence.md) — reactive cadence; the
  idle-silence half of the bandwidth story.
