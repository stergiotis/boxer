---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# ImZero2 remote access (headless host) — Explanation

This file explains how the remote-access pipeline of the ImZero2 renderer
hangs together: the data flow from the FFFI2 command stream to pixels in a
browser tab, the thread/process topology, and the invariants the pieces
rely on. The *decisions* behind this shape — why pixel streaming, why
WebSocket + WebCodecs, why protobuf — live in
[ADR-0024](../../doc/adr/0024-imzero2-remote-access-browser-viewer.md);
this file should stay valid even if individual components are rewritten.
An RDP delivery head over the same pipeline was designed and withdrawn
the same day ([ADR-0081](../../doc/adr/0081-imzero2-headless-rdp-egfx-head.md),
kept as reference analysis) — the browser path below is the delivery
channel.

## Background

ImZero2 is a Go-hosted GUI: the Go process owns application logic and
streams widget-level commands over FFFI2 (stdin/stdout, lockstep,
length-prefixed binary) to this Rust process, which interprets them
against a live egui pass. On the desktop, eframe + winit own the window,
the GPU surface, and frame pacing (vsync).

Remote access replaces the *window* — nothing else. The same interpreter
runs against the same egui context; only the host loop differs: frames go
to an offscreen texture, get read back, encoded as H.264, and carried to
a viewer; the viewer's input events travel back and are translated into
ordinary egui events. The Go side cannot tell which host is running.

## The pipeline

```
Go host (FFFI2 commands, stdin/stdout — unchanged)
  │
  ▼
render loop (main thread, fixed tick)
  egui::Context::run_ui → interpreter dispatch → tessellate
  → egui_wgpu render to offscreen BGRA texture
  → copy_texture_to_buffer (256-byte row alignment) → map → BGRA frame
  │
  ▼ FrameSink fan-out
  ├─ PNG dump            (verification)
  ├─ EncoderSink → file  (raw Annex-B, verification)
  └─ WsCarrier           (the live path)
       │ per-connection EncoderSink
       ▼
     ffmpeg subprocess: rawvideo BGRA on stdin → H.264 Annex-B on stdout
       │ drain thread: split into access units, envelope, frame
       ▼
     bounded channel → tokio task → WebSocket binary messages
       │
       ▼
     browser viewer page: NAL/envelope parse → WebCodecs VideoDecoder
       → canvas; DOM input → protobuf InputEvent → same WebSocket
       │
       ▼ (server side)
     event queue → InputTranslator → egui::RawInput.events (next tick)
```

Wire framing is one binary WebSocket with a one-byte type prefix:
`0x01` video chunk (server→client), `0x02` input event (client→server),
`0x03` session control (both directions: hello, viewport-resize report,
decode-progress ping). Payloads are protobuf, package `boxer.imzero2.v1`
(canonical contract: `proto/boxer/imzero2/v1/input.proto`).

## Thread and process topology

Five execution contexts, each with one job:

1. **Render thread** (process main): paces the frame loop at a fixed
   tick, drives the interpreter, renders, reads back, feeds sinks. It is
   the only context that touches egui or wgpu.
2. **ffmpeg subprocess**: encoding is invoked, not linked (the
   encoder-as-subprocess practice from ADR-0024). Its stdin is fed by the
   render thread; its stderr joins ours.
3. **Drain thread** (one per encoder instance): reads ffmpeg's stdout,
   splits the byte stream into access units, wraps each in the protobuf
   envelope, and pushes framed payloads into a bounded channel via a
   blocking send.
4. **Carrier thread**: a current-thread tokio runtime running the
   WebSocket accept/session loops and the single-page HTTP responder for
   the embedded viewer (served on port+1).
5. **Browser**: decodes and presents; captures input.

Cross-context communication is deliberately narrow: atomics for
connection state (connected flag + generation counter), one mutex-guarded
vector for inbound events, one bounded channel for outbound video.

## Backpressure: one chain, no drops after the encoder

A slow or stalled viewer must not cause unbounded buffering, and encoded
frames must never be discarded — with B-frames disabled every encoded
frame is a reference frame, so dropping one breaks decode until the next
IDR. The pipeline therefore propagates pressure *upstream* instead of
dropping *downstream*:

WebSocket send stalls → bounded channel fills → drain thread's blocking
send blocks → ffmpeg's stdout pipe fills → ffmpeg stops consuming stdin →
the render thread's frame write blocks → rendering slows to what the
viewer sustains.

Frames may be skipped *before* encoding (that is just a lower frame
rate), never after. ADR-0024 SD9 names a refinement — a ring that decouples
render cadence from encoder cadence so a slow consumer cannot slow the
FFFI2 loop itself — which is not built yet; until then render and encode
share one cadence and the chain above is the whole story.

## Stream mechanics

The encoder output is a raw H.264 Annex-B elementary stream; there is no
container at any point. Three properties make it transportable and
joinable:

- **Access-unit framing without slice parsing.** ffmpeg is told to insert
  Access Unit Delimiters (`h264_metadata=aud=insert`); the drain thread
  splits on AUD start codes, which requires no knowledge of slice
  headers. One WebSocket message carries exactly one displayable frame.
- **Self-describing key frames.** `dump_extra=freq=keyframe` repeats
  SPS/PPS in-band on every IDR, so a decoder can be configured from the
  stream alone — the viewer derives the WebCodecs codec string
  (`avc1.PPCCLL`) from the three bytes after the SPS NAL header and runs
  the decoder in Annex-B mode (no `description` in `configure`).
- **Join = IDR.** A decoder cannot enter a stream mid-GOP. Rather than
  requesting key frames mid-stream, the encoder's lifetime is tied to the
  connection: it spawns when a viewer connects and stops on disconnect,
  so every session begins with SPS/PPS + IDR by construction, and no
  encoding happens with nobody watching. Supervised encoder restarts
  (crash recovery) restart the stream the same way.
- **Resize = a fresh stream, announced first.** The viewer reports its
  viewport (CSS pixels) and pixel scale; the host clamps it, rebuilds the
  offscreen target at logical × scale physical pixels, and re-announces
  the session hello *through the same outbound channel as the video* —
  channel ordering then guarantees the viewer sees the new geometry
  (resize canvas, drop decoder) before the new stream's first IDR
  arrives, and the normal join-at-keyframe path does the rest. rawvideo
  dimensions are fixed per ffmpeg invocation, so a geometry change is an
  encoder restart by necessity, not by choice.

Frame dimensions are rounded up to even numbers at host start: 4:2:0
chroma subsampling requires them, and baking the constraint into the
texture keeps every downstream representation identical.

## Input translation

The viewer sends browser-shaped events (CSS-pixel coordinates already
converted to logical points using the geometry from the session hello);
the host's translator turns them into `egui::Event` values at the edge:
pointer events map one-to-one, browser `KeyboardEvent.key` names map via
`egui::Key::from_name` (which accepts the browser spellings), printable
characters additionally arrive as text events, and a modifier bitmask
mirroring `egui::Modifiers` keeps `RawInput::modifiers` coherent between
events. Everything past `RawInput` is indistinguishable from desktop
input — the interpreter and the widget contract (ADR-0013) are unaware
of remoting.

## Invariants

- **stdout is a data channel.** FFFI2 owns the Rust process's stdout;
  nothing may print to it. The same discipline repeats one level down:
  ffmpeg's stdout is the elementary stream, its diagnostics go to stderr.
- **Encoded frames are never dropped** (see backpressure above).
- **Every viewer session starts at an IDR** (encoder lifetime =
  connection lifetime).
- **Single egui pass per frame.** Multipass would re-run the UI closure,
  but the per-frame FFFI2 opcode stream has already been consumed by the
  first pass; the host pins `max_passes = 1` (shared init, both hosts).
- **Host-independent interpreter.** All context setup that affects
  interpretation (fonts, style overlay, the multipass pin, the SVG-export
  plugin) lives in one shared init path used by both hosts; the headless
  host adds nothing the desktop host doesn't have.
- **One session at a time** (v1): a second connection is rejected while
  one is active.

## Observability

Rendering headless means nothing is visible by default, so each hop has
an observation point: a PNG sink for raw frames, a file target for the
encoded stream (ffprobe-able), a probe client (`ws_probe`) that records
the wire and can inject input, and a decode-progress ping — the viewer
reports its decoded-frame count over the session-control channel and the
server logs it, which turns "is the browser actually decoding?" into a
server-side log line. The pings exist for verification and field
debugging, not for protocol correctness.

## Trade-offs

- Pixel streaming costs server-side encode per session and bandwidth
  proportional to motion; in exchange the client needs nothing but a
  WebCodecs-capable browser, and application latency characteristics are
  uniform across client devices. The alternatives (and when they would
  win) are recorded in ADR-0024's design space.
- Chroma is 4:2:0: fine chart linework pays a fidelity cost relative to
  RGB. A full-chroma upgrade has no mature WebCodecs-side path today
  (the withdrawn RDP head had AVC444 as its lane for this).
- A fixed render tick burns encode work on idle dashboards; reactive
  cadence (ADR-0062) on the headless host is named follow-up work.

## Further reading

- [ADR-0024](../../doc/adr/0024-imzero2-remote-access-browser-viewer.md) —
  the design space, decisions, and the v1 implementation record
  (Updates, 2026-06-12).
- [ADR-0081](../../doc/adr/0081-imzero2-headless-rdp-egfx-head.md) —
  the RDP head over the same foundation (withdrawn 2026-06-12; the
  design space and ecosystem facts remain the reference).
- [ADR-0013](../../doc/adr/0013-imzero2-stateful-widget-contract.md) —
  the widget contract remote input must respect.
- [ADR-0062](../../doc/adr/0062-imzero2-render-cadence.md) — render
  cadence; the reactive mode the headless host should eventually adopt.
- `proto/boxer/imzero2/v1/input.proto` — the wire contract.
- [MS-RDPEGFX](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-rdpegfx/da5c75f9-cd99-450c-98c4-014a496942b0)
  — the protocol the same elementary stream would ride under ADR-0081.
