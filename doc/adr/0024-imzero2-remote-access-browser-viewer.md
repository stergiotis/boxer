---
type: adr
status: proposed
date: 2026-05-10
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0024: ImZero2 remote access via headless render + ffmpeg + browser viewer

## Context

ImZero2 today is a desktop binary: `rust/imzero2/hmi.sh` builds and launches the Rust process under `eframe + winit + wgpu`, with the Go side spawning it as a child over stdin/stdout (FFFI2). The Grafana-replacement scope (memory `project_grafana_replacement`) requires the same UI to reach users on machines without a native build — i.e. a browser tab — and ultimately to be deployable as a Kubernetes service. Neither shape is achievable on the existing eframe-driven path.

ImZero1 (`~/repo/imzero_client_cpp`, user-authored prior art; memory `project_imzero1_video_prior_art`) demonstrated a working end-to-end remote-rendering pipeline: patched-ImGui hooks emitted FlatBuffers vector draw commands, a server-side Skia renderer rasterised those commands into a BGRA buffer, ffmpeg (`h264_vaapi -bf 0 -qp:v 26`, NUT container, `skia/video_local_h264.sh` driver) encoded the buffer through a named pipe, a libmpv-embedding desktop client (`video_player/sdl3_mpv/`, `imzero_video_play`) decoded and presented the stream, and SDL3-captured input flowed back as FlatBuffers `UserInteractionFB` events through a second named pipe. The architecture worked. Its brittleness — confirmed by the user — was in the patched-ImGui fork and the Skia-rasteriser middle layer, neither of which exists in ImZero2 because egui owns its own renderer.

The streaming **tail** of ImZero1 (BGRA buffer → ffmpeg → encoded video → wire format → input events back) is the part that survives intact and can be brought forward. The **middle** (vector commands + Skia rasteriser + patched ImGui) is replaced by ImZero2's existing wgpu-driven egui pipeline, which produces the same kind of pixel buffer at the same point in the data flow. This ADR is therefore not a new architecture — it is an acknowledgement of ImZero1's design and a recipe for porting its working tail onto ImZero2's healthier middle, while pivoting the client end from a libmpv desktop binary to a browser tab and the input wire format from FlatBuffers to protobuf.

Forces this ADR must respect:

- **Browser-first delivery.** The first remote-access user-facing target is a browser tab, not a native client. K8s-readiness (a follow-up phase) follows naturally from a browser-native architecture and is not part of v1.
- **FFFI2 protocol unchanged.** The Go ↔ Rust internal protocol is mature; remote access is a tap on Rust's render output, not a re-architecture of the command stream.
- **Continuous-redraw model preserved at v1** (memory `project_imzero2_continuous_rendering`). Reactive repaint is desirable for encoder cost reduction but is a separate ADR; v1 carries the existing model forward without modification.
- **eframe is load-bearing in interactive mode.** The desktop `hmi.sh` path will continue to use eframe + winit; only the headless mode needs to drop them. Two binary targets, gated by Cargo features.
- **License posture for clean redistribution.** Encoder via ffmpeg subprocess (LGPL/GPL build choice deferred to runtime), not linkage. GPL-licensed media stacks (Sunshine, KasmVNC) are not adopted.
- **Single-session at v1.** Multi-tenancy, authentication, and K8s packaging are deferred to a follow-up ADR. v1 is one binary instance, one user, localhost-WebSocket transport.

## Design space (QOC)

**Question.** How should ImZero2 expose a remote-access deployment shape that delivers an interactive UI to a browser tab while preserving the existing Go ↔ Rust ↔ egui rendering path and respecting the long-term path to K8s deployment?

**Options.**

- **O1 — Headless render + ffmpeg + WebSocket + browser WebCodecs viewer + protobuf events (chosen).** Server replaces eframe with a hand-rolled `egui::Context` + `egui_wgpu::Renderer` loop, renders to an offscreen BGRA texture, reads back, pipes BGRA to ffmpeg (subprocess via named pipe; matching ImZero1 args), ships encoded H.264 over a WebSocket connection. Browser uses WebCodecs `VideoDecoder` to decode H.264 NAL units and renders to `<canvas>`; DOM events are protobuf-encoded back over the same WebSocket.
- **O2 — Reuse `imzero_video_play` (ImZero1's libmpv client) unchanged.** Match the ImZero1 wire format (NUT container + FlatBuffers `UserInteractionFB`); the existing C++ client connects as-is. Native-client target.
- **O3 — WebRTC peer connection with a reusable web client.** Server runs `webrtc-rs`, encodes via VAAPI/NVENC, ships via RTP over peer connection; browser connects via WHIP signaling. Production-grade reusable browser clients exist as open source (`epicgames/PixelStreamingInfrastructure` MIT, Selkies `gst-web` MPL-2.0, `m1k1o/neko` Apache-2.0), so v1 client-side work collapses from "build" to "adapt." Server side adds STUN/TURN, ICE, a WHIP signaling endpoint, and the `webrtc-rs` dependency footprint.
- **O4 — VNC server embedded in the Rust binary.** RFB protocol; CPU readback of the wgpu texture, emit Raw or Tight-encoded framebuffer updates. Browser via noVNC sidecar. Self-contained, no codec licensing.
- **O5 — Compile the Rust interpreter to wasm.** Browser hosts the full egui interpreter; FFFI2 protocol travels over WebSocket replacing stdin/stdout. Vector-command-equivalent path; client uses GPU.
- **O6 — `epaint::FullOutput` over WebSocket to a thin painter wasm client.** Server keeps Rust running, but only ships epaint shapes + texture deltas; browser-side wasm tessellator + WebGPU painter consumes them.

**Criteria.**

- **C1 — Browser-native client.** Hard requirement for v1: zero-install, runs in a browser tab.
- **C2 — Latency tolerable for dashboard interaction.** Chart pan/zoom should feel responsive at LAN-grade RTT.
- **C3 — Reuse of ImZero1 prior art.** Established choices (encoder, container, frame model, naming conventions) carry forward without re-validation.
- **C4 — Implementation cost to v1.** Engineer-weeks to a working end-to-end remote tab.
- **C5 — Frame-model compatibility.** Plays nicely with continuous-redraw at the server side and variable cadence at the client.
- **C6 — Forward path to K8s + multi-tenancy.** Architecture allows the follow-up ADR to add multi-session, authentication, and container packaging without an architectural rewrite.
- **C7 — License / dependency hygiene.** No GPL contamination on the Rust or browser side; all required deps permissive.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 (browser+ffmpeg+ws) | O2 (libmpv reuse) | O3 (WebRTC) | O4 (VNC) | O5 (wasm interpreter) | O6 (FullOutput wire) |
|----|------------------------|-------------------|-------------|----------|------------------------|----------------------|
| C1 | ++                     | −−                | ++          | + (via noVNC sidecar) | ++                | ++                   |
| C2 | +                      | ++                | ++          | −        | +                      | +                    |
| C3 | ++                     | ++                | +           | −        | −                      | −−                   |
| C4 | +                      | +                 | +           | +        | −                      | −−                   |
| C5 | +                      | +                 | +           | +        | −                      | −                    |
| C6 | +                      | −                 | +           | +        | +                      | +                    |
| C7 | ++                     | ++                | +           | + (rfb crate, MIT) | ++           | ++                   |

O1 is the chosen path on **stack-control** criteria (fewer dependencies, simpler signaling, debuggable with browser dev tools alone), not on raw cost. O3's effort, once reusable browser clients are factored in (Phase 4 collapses from "build viewer" to "adapt viewer"), is approximately comparable to O1; the choice between them is "DIY full stack with minimum dependencies" (O1) versus "stand on shoulders with WebRTC's complexity tax" (O3). We choose O1 for v1 because the stack-control benefit compounds over the project's lifetime and the WebRTC tax (signaling daemon, STUN/TURN, larger dependency footprint, entanglement of input schema with data-channel semantics) is real even when client work disappears. O3 remains the natural Phase N pivot if multi-tenancy, NAT traversal, or operator preference for the WebRTC ecosystem inverts the calculus. O2 is the natural native-client sibling — same architecture minus the browser — held in reserve as a Phase N option once the wire format stabilises; a libavcodec-based native client is a small additional layer on the same headless render. O4 fails on C2 — RFB encodings available in the Rust ecosystem (Raw, Hextile in `rfb`; no Tight/JPEG) are inadequate for chart fidelity at acceptable bandwidth, and KasmVNC's better encodings require Kasm-specific RFB extensions outside standard Rust crates. O5 is technically clean but greenfield-with-no-prior-art for ImZero2; wasm bundle size for the wgpu deps, uneven WebGPU support across browsers, and synchronous `Sync()` RTT exposure rule it out for v1. O6 is the most architecturally interesting (one server-side egui state, ship epaint shapes only) but has no public prior art and unbounded research scope.

## Decision

We will implement **option O1**: extend the ImZero2 Rust binary with a headless render mode that renders egui to an offscreen BGRA texture, encodes via an `ffmpeg` subprocess matching ImZero1's encoder configuration, ships the encoded stream over a single WebSocket connection to a browser viewer that decodes via the WebCodecs API, and accepts protobuf-serialised input events back over the same WebSocket.

The headless mode is gated by a Cargo feature; the existing interactive `hmi.sh` desktop mode remains unchanged. The two modes share the FFFI2 interpreter body verbatim — only the host loop differs.

### Subsidiary design decisions

- **SD1 — Two binary targets, one source tree.** Cargo feature `headless` selects the new render-to-texture host; default features keep `eframe + winit + wgpu` for `hmi.sh`. The interpreter (`App::logic` body) is identical between modes. Rationale: avoid forking the codebase; avoid pulling ffmpeg / WebSocket deps into the desktop build; avoid pulling winit into the headless build.
- **SD2 — Drop `eframe` in headless mode; drive `egui::Context` and `egui_wgpu::Renderer` directly.** The headless host owns wgpu Instance/Device/Queue creation (no Surface), creates an offscreen `wgpu::Texture` (`Bgra8UnormSrgb`, `RENDER_ATTACHMENT | COPY_SRC`), runs `Context::run` per frame with manually-built `RawInput`, tessellates and renders via `egui_wgpu::Renderer`, copies the colour attachment to a readback `wgpu::Buffer` (with the 256-byte `bytes_per_row` alignment), maps the buffer, and writes BGRA bytes to the encoder pipe. eframe's other responsibilities (HiDPI, multi-monitor, persistence, viewport lifecycle) are non-issues server-side and are not re-implemented.
- **SD3 — Encoder is `ffmpeg` as subprocess via named pipe; format is rawvideo BGRA, not BMP.** ImZero1 emitted BMP frames into `image2pipe`; this ADR replaces that with `-f rawvideo -pix_fmt bgra` to drop the per-frame BMP header. The remaining encoder arguments mirror ImZero1: `-c:v h264_vaapi -bf 0 -qp:v 26`. Subprocess is supervised; on crash the headless host logs and restarts. Zero-copy / vk_video integration is a future optimisation — v1 takes the CPU readback hit, same as ImZero1.
- **SD4 — Wire format is raw H.264 Annex-B over WebSocket binary frames; no MP4 / NUT container.** ffmpeg outputs `-f h264` (Annex-B byte stream); the headless host frames each NAL unit (or coalesced GOP segment) as a binary WebSocket message with a small protobuf envelope (frame index, timestamp, NAL-unit-type marker, SPS/PPS attached on key frames). Browser-side, a small JS splitter feeds NAL units to `VideoDecoder.decode(EncodedVideoChunk)`. Rationale: no remuxing overhead; no MP4 / NUT container complexity in the browser; WebCodecs operates directly on raw codec data once configured with SPS/PPS extracted from the first key frame.
- **SD5 — Browser decode is the WebCodecs API; no MSE, no `<video>` tag at v1.** WebCodecs `VideoDecoder` ships in current Chromium-family and Safari 16.4+; Firefox support is progressive. Decoded `VideoFrame` is drawn to a `<canvas>` via `drawImage(frame, 0, 0)` (2D context) or a small WebGPU pipeline for direct sampling. Rationale: WebCodecs has no demuxer cache, no jitter buffer, no presentation-queue heuristic — exactly the latency profile we want. An MSE / `<video>` fallback for older browsers is a future ADR if the audience demands it.
- **SD6 — Single WebSocket carries both video frames (server→client) and input events (client→server).** Binary messages with a one-byte type prefix: `0x01` for encoded video chunk, `0x02` for protobuf input event, `0x03` for session control (resize, ping, viewport metadata). Rationale: simpler signaling than two parallel sockets, no out-of-order coordination between channels, single auth boundary in the K8s follow-up. WebRTC DataChannel semantics are not needed at this volume.
- **SD7 — Input events serialised as protobuf; schema is new, does not reuse `UserInteractionFB`.** A `pebble2impl/v1/input.proto` schema covers `MouseMove`, `MouseButton`, `MouseWheel`, `KeyDown`, `KeyUp`, `TextInput` (Unicode characters / IME), `ViewportResize`, and `SessionControl`. Wrapped in a top-level `oneof` event type. Codegen via `prost` for Rust and `protobuf-es` for TypeScript. Rationale per the prior wire-format discussion: at input-event volume FlatBuffers' zero-copy advantage buys nothing; protobuf has better Rust and browser tooling and broader contributor familiarity. Migration from FlatBuffers is a one-time schema rewrite, not load-bearing on the architecture.
- **SD8 — Input events translate to `egui::RawInput` at the headless-host edge; the FFFI2 interpreter sees identical events as the desktop mode.** A small `protobuf → egui::Event` mapper lives in the headless host; key codes are mapped via a translation table; modifier state is tracked per-session. The FFFI2 interpreter (`App::logic`) does not learn that input is remote.
- **SD9 — Frame pacing decoupling: render at FFFI2 cadence (Go-driven, ~60 Hz today); encoder samples at 30 Hz via a small ring buffer.** The wgpu readback path runs every Go frame; the encoder feeder pulls the latest available BGRA buffer at its own clock. Rationale: decouples the existing render cadence from encoder cadence; preserves continuous-redraw (memory `project_imzero2_continuous_rendering`) without doubling work for the encoder; accommodates reactive-repaint when that lands without redesign.
- **SD10 — `imzero_video_play` (libmpv) client is not reused at v1.** The browser-first decision supersedes the native-client target. The libavcodec-based replacement discussed in conversation is held as a Phase N option; the wire format defined here (raw H.264 Annex-B + protobuf input over WebSocket) is intentionally browser-shaped, but a native client speaking the same protocol is feasible later as a small additional layer (libavcodec for decode + a SDL3-style host for input).
- **SD11 — Out of scope at v1.** Authentication; multi-tenancy (one binary instance per session at v1); K8s packaging (Dockerfile, Helm chart, GPU device plugin matrix); native client; audio path; clipboard sync; touch events (egui supports them but the protobuf schema starts mouse + keyboard only); reactive-repaint; encoder backend selection (NVENC, vk_video) — VAAPI is the assumed backend, matching ImZero1; software-encode fallback. Each is named so escape hatches are explicit and so the v1 surface stays small.

## Alternatives

- **O2 — Reuse `imzero_video_play` (libmpv-embedding C++ client).** Same architecture minus browser: server emits NUT-or-Annex-B over pipe-or-socket, FlatBuffers `UserInteractionFB` events flow back. Rejected at v1 because the user-facing target is browser-first; held as the natural Phase N native-client option once the wire format stabilises. The mpv-vs-libavcodec question identified in conversation (mpv's "playing a movie" defaults fight a remote-display use case) means the Phase N native client likely uses libavcodec directly rather than libmpv, but the wire format defined in this ADR works for both.
- **O3 — WebRTC + browser via WHIP signaling, reusing a published web client.** A serious alternative whose viability rests on a fact discovered late in the design conversation: production-grade browser clients for hardware-encoded video streams already exist as open source. Concrete reusable options:

  - **[`epicgames/PixelStreamingInfrastructure`](https://github.com/EpicGames/PixelStreamingInfrastructure)** (MIT) — Unreal Engine's reference web frontend; the canonical "browser tab decoding hardware-encoded H.264 with input return" implementation.
  - **[Selkies `gst-web`](https://github.com/selkies-project/selkies-gstreamer/tree/main/addons/gst-web)** (MPL-2.0) — the K8s-targeted equivalent; aligns with the Phase N container direction.
  - **[`m1k1o/neko`](https://github.com/m1k1o/neko)** (Apache-2.0) — multi-user "virtual browser" reference; another full-stack template.

  Server-side: replace `tokio-tungstenite` with `webrtc-rs`, add a WHIP-style HTTPS signaling endpoint, configure ICE/STUN (and TURN if NAT traversal matters). Browser-side: adapt the chosen reference client's input layer to our protobuf schema. The encoder configuration (`h264_vaapi -bf 0 -qp:v 26`) and the headless render path (SD2) carry across unchanged. Effort vs O1 is approximately a wash: ~1–2 weeks of "build TypeScript WebCodecs viewer" disappear, a comparable ~1–2 weeks of "integrate `webrtc-rs` + signaling + adapt reusable client" appears.

  Choosing O1 over O3 is therefore **not a cost decision; it is a stack-control decision.** Arguments for O1 over O3: (a) one fewer signaling daemon to operate; (b) no STUN/TURN dependency in the v1 deployment; (c) `tokio-tungstenite` is a smaller dependency footprint than `webrtc-rs`; (d) the WebSocket wire format is debuggable with browser dev tools alone, no SDP / ICE inspection needed; (e) the protobuf input event schema is independently designed and not entangled with WebRTC data-channel semantics; (f) external operators do not need a TURN deployment if ImZero2 lives on the same network as the browsers.

  O3 remains the natural Phase N evolution if any of these arguments inverts: multi-tenancy or NAT traversal becomes a hard requirement; the operations team prefers WebRTC's mature monitoring / SFU ecosystem (`mediasoup`, `pion`, `Janus`); an integrator wants to plug ImZero2 into existing WebRTC infrastructure. The wire-format definitions in this ADR (raw H.264 Annex-B; `pebble2impl/v1/input.proto` for input events) are intentionally transport-abstract — a WebRTC variant carries the same encoder configuration and the same input schema; only the framing changes.

  **VNC-family browser clients (KasmVNC, Apache Guacamole, noVNC) were evaluated as O3-adjacent alternatives and ruled out.** All three speak RFB or RFB-derived protocols whose codec ladders top out at JPEG/WebP/QOI; none support hardware-encoded H.264/H.265/AV1. Adopting them means giving up the codec fidelity decision (chart-quality at LAN bandwidth requires hardware video codecs, not image-delta protocols). KasmVNC additionally requires Kasm-specific RFB encoding extensions outside the standard Rust `rfb` crate; Guacamole additionally requires running `guacd` as an intermediate translation server.

  **HLS / DASH / `<video>`-tag clients (`hls.js`, `video.js`) were evaluated and ruled out.** Browser delivery is trivial (point a `<video>` element at a fragmented MP4 and let MSE handle it), but MSE's latency floor is ~1–3 seconds due to demuxer cache and presentation-queue heuristics — fine for one-way video playback, unworkable for an interactive UI. They also have no input-return channel.
- **O4 — Embedded VNC server.** Self-contained and license-clean (`rfb` crate, MIT/Apache); but the encodings available in pure-Rust crates are inadequate for chart fidelity at acceptable bandwidth, and lavapipe-on-CPU rendering (the K8s-without-GPU answer) compounds the cost. Discussed in design conversation; rejected on C2.
- **O5 — Compile Rust interpreter to wasm.** Browser hosts the full egui interpreter; FFFI2 over WebSocket replaces stdin/stdout. Technically feasible but: (a) wasm bundle size for ImZero2's wgpu deps is multi-MB; (b) WebGPU support is uneven across browsers; (c) the synchronous `Sync()` round-trip becomes RTT-blocking and degrades hover/scroll feel; (d) state replication makes reconnection ugly. Held as an "if pixel-streaming costs become unacceptable for some user class" escape hatch — the FFFI2 protocol is already the boundary that would carry across.
- **O6 — `epaint::FullOutput` over WebSocket to a thin painter wasm client.** Most architecturally interesting (one server-side egui state, ship epaint shapes only) but no public prior art and unbounded research scope. Held open as "the right answer if pixel streaming turns out wrong" but not pursued in this ADR.

## Consequences

### Positive

- **Browser deployment unlocks the Grafana-replacement scope.** Users open a tab; no install, no native build per platform. The dashboard target is browser-shaped and so is the v1 client.
- **ImZero1's proven encoder choices carry forward unchanged.** `h264_vaapi -bf 0 -qp:v 26`, ffmpeg-as-subprocess, named-pipe transport — all validated by the running ImZero1 prototype. No re-validation of encoder behaviour or codec parameters for v1.
- **The headless-render pattern is the same primitive needed for native-client (O2), WebRTC (O3), and any future K8s deployment.** Phase N work compounds on this ADR rather than replacing it.
- **No patched egui, no parallel renderer, no FlatBuffers vector-command middleware.** ImZero1's brittle middle layer is not reproduced. The new code surface is small (a headless host, an encoder spawner, a WebSocket carrier, a protobuf schema, a browser viewer) and most of it is mechanical.
- **License posture is clean.** ffmpeg is invoked as a subprocess (license tier deferred to runtime build); `prost` and `protobuf-es` are Apache-2.0 / MIT; `tokio-tungstenite` (anticipated WebSocket dep) is MIT; WebCodecs is a browser standard. No GPL-only stacks (Sunshine, KasmVNC) are adopted.

### Negative

- **eframe is dropped in the headless build.** The headless host hand-rolls the responsibilities eframe normally provides (Instance/Device/Queue creation, render-pass scheduling, frame lifecycle). This is bounded but new code, and it must continue to work as egui evolves. The desktop `hmi.sh` path keeps eframe to avoid widening the surface that has to track upstream egui changes.
- **WebCodecs availability constrains browser support.** Chromium-family is mature; Safari 16.4+ ships it; Firefox is progressing. Older browsers fall outside v1; an MSE / `<video>` fallback is a future ADR if a target audience demands older browser support.
- **Continuous-redraw cost still paid at the encoder.** Server renders 60 Hz, encoder samples 30 Hz; idle frames still redraw and still get encoded. Reactive-repaint will reduce this materially but is a separate ADR.
- **Single-session, no auth, no K8s at v1.** A binary instance per user is the operating model. v1 is local-deploy-only; multi-tenancy, authentication, and container packaging are deferred.
- **No native client at v1.** ImZero1 users with `imzero_video_play` muscle memory will not see a native-client option until Phase N.
- **No audio.** ImZero1 had no audio either; this ADR does not add it. If a future use case (e.g. alarm tones in dashboards) needs audio, it is a separate channel.
- **Protobuf schema versioning becomes a contract.** Once a deployed browser viewer speaks v1 of `pebble2impl/v1/input.proto`, the server must either support it indefinitely or coordinate viewer updates. Standard wire-protocol discipline applies.

### Neutral

- **Two binary targets (interactive + headless) is a moderate maintenance cost, not a heavy one.** The interpreter body is shared; only the host loop differs. Cargo features are a familiar mechanism for this in the ImZero2 stack (cf. `gpu_intel` / `gpu_nvml` / `gpu_rocm` in `observability/sysmetrics`).
- **The wire format is intentionally browser-shaped but transport-agnostic.** WebSocket today; WebRTC DataChannel, QUIC, or plain TCP are mechanically interchangeable below the protobuf framing.
- **Frame-pacing decoupling (SD9) is a small ring buffer, not an architectural change.** Accommodates reactive-repaint and variable-cadence rendering when those land.

### Derived practices

- **Cargo features for deployment-shape variance.** Headless vs interactive is the first real example in `rust/imzero2/`; future variants (a CLI-only test harness, a hypothetical embedded build, the eventual K8s container variant) follow the same pattern.
- **Wire-format ADRs document protobuf schema versioning policy explicitly.** This ADR establishes `pebble2impl/v1/...` as the namespace; future input-event additions are additive (new fields, new `oneof` variants) until a v2 is unavoidable.
- **Encoder-as-subprocess is the default integration pattern for media tools in this stack.** ffmpeg is invoked, not linked. The same shape applies if WebRTC adds GStreamer or a future audio path adds another encoder.

## Status

Proposed — awaiting review by @spx. Implementation phasing per the design conversation: Phase 1 (headless egui Context loop, drop eframe under feature gate, offscreen BGRA texture + readback) → Phase 2 (ffmpeg subprocess + rawvideo BGRA pipe + Annex-B output) → Phase 3 (protobuf schema, `prost` + `protobuf-es` codegen, input mapper at headless-host edge) → Phase 4 (browser viewer with WebCodecs decode + canvas render + DOM-event capture) → Phase 5 (end-to-end test, frame-pacing decoupling per SD9, encoder restart supervision). Estimated total: 4–6 weeks for production-quality v1.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only; supersession is recorded, not deleted.

## References

- ImZero1 prior-art prototype: `~/repo/imzero_client_cpp` (memory `project_imzero1_video_prior_art`); video pipeline driver `skia/video_local_h264.sh`; `UserInteractionFB` and `DrawList` schemas in `spec/ImZeroFB.fbs`; libmpv-embedding client at `video_player/sdl3_mpv/`.
- [ADR-0013 — ImZero2 stateful widget contract](./0013-imzero2-stateful-widget-contract.md) — input events from the protobuf channel must respect the gated `r10_push` rule when they translate to widget state changes.
- Memory `project_grafana_replacement` — browser-accessible UI is the dominant use case driving this ADR.
- Memory `project_imzero2_continuous_rendering` — frame model unchanged at v1; reactive-repaint deferred.
- Memory `project_imzero1_video_prior_art` — non-obvious context that informs SD3, SD4, SD7, SD10.
- [Sunshine (LizardByte/Sunshine)](https://github.com/LizardByte/Sunshine) — pixel-streaming reference; rejected on license (GPL-3) but architectural precedent.
- [`epicgames/PixelStreamingInfrastructure`](https://github.com/EpicGames/PixelStreamingInfrastructure) — MIT-licensed Unreal Engine web frontend; reusable web client for the O3 Phase N pivot.
- [Selkies-GStreamer `gst-web`](https://github.com/selkies-project/selkies-gstreamer/tree/main/addons/gst-web) — MPL-2.0 K8s-targeted reusable web client for the O3 Phase N pivot.
- [`m1k1o/neko`](https://github.com/m1k1o/neko) — Apache-2.0 multi-user virtual-browser web client; another reusable option for the O3 Phase N pivot.
- [Wolf (games-on-whales/wolf)](https://github.com/games-on-whales/wolf) — K8s-native containerised pixel-streaming precedent.
- [Unreal Engine Pixel Streaming docs](https://docs.unrealengine.com/en-US/PixelStreamingOverview/) — engine-internal-encoder + WebRTC architectural reference.
- [WebCodecs API specification](https://www.w3.org/TR/webcodecs/) — browser-side decoding primitive.
- [`prost`](https://crates.io/crates/prost) — Rust protobuf codegen.
- [`protobuf-es`](https://github.com/bufbuild/protobuf-es) — TypeScript protobuf codegen.
- [`tokio-tungstenite`](https://crates.io/crates/tokio-tungstenite) — anticipated WebSocket transport crate.
- ffmpeg `h264_vaapi` encoder — VAAPI-backed H.264 encode; same configuration as ImZero1.
