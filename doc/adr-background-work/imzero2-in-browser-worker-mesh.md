---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-09-24 from a design
> dialogue. Nothing here is a decision. Claims about this repository were
> checked against the working tree on the compile date; the measurement it
> points at is a first run of a new trial on one machine.

# imzero2 in a browser tab: both wasm modules in one worker, the mesh to the existing painter

## 1 Question

Can imzero2 be recomposed to run in a browser by compiling the egui side to
wasm, letting it produce the tessellated mesh in the tab, and reusing the
existing web viewer to paint it — with the Go application layer either beside
it in the tab or left on a server?

## 2 What the tree already says

The question has been decided twice, in opposite halves.

- **The Go host stays on a server, only egui moves.** Rejected as
  [ADR-0024](../adr/0024-imzero2-remote-access-browser-viewer.md) O5 and
  again in [ADR-0128](../adr/0128-imzero2-mesh-draw-stream-codec-lane.md)'s
  alternatives. The reason is the protocol, not the renderer: FFFI2 is a
  lock-step pipe, `StateManager.Sync` issues its fetchers as sequential
  blocking round trips (24 call sites in `egui2_statemanagement.go` on the
  compile date), and the command stream carries no dedup, so every frame
  crosses in full. Across a network that is tens of round trips per frame.
- **Both halves in the tab.** Accepted as
  [ADR-0077](../adr/0077-keelson-browser-wasm-execution.md): Go and the Rust
  renderer as sibling wasm modules on the page's main thread, joined by a
  synchronous "sandwich" bridge (SD1), `GOOS=wasip1` as the working
  hypothesis (O2). Its worker option (O3) was killed because Go in a worker
  with the renderer on the main thread needs `SharedArrayBuffer`, hence
  COOP/COEP headers the target fleet cannot rely on. None of it is built;
  its gating measurement, the Phase-0 spike (SD2), had not been run.

What the tree has for the display side: the mesh lane (ADR-0128) serialises
egui's `ClippedPrimitive` meshes and texture deltas to a hand-written WebGL2
painter inside the single-file viewer page (`viewer/index.html` in
`rust/imzero2`). The painter consumes only the mesh byte format, a session
hello and a canvas; it needs nothing from the carrier's roster, auth or
encoder services.

## 3 The recomposition worth recording

Put **both** wasm modules in **one** worker and keep ADR-0077's synchronous
bridge inside it. The worker posts each frame's mesh to the main thread as a
transferable buffer in the ADR-0128 wire format, and the main thread keeps
only what today's viewer already is: the painter and DOM input, sending input
events back over the same port.

```
 main thread (the existing viewer page, transport swapped)
 ┌──────────────────────────────────────────────────────────────┐
 │  DOM input ──▶ InputEvent (protobuf) ──▶ port.postMessage    │
 │  WebGL2 painter ◀── mesh frame / texture delta (transferable) │
 └───────────────▲───────────────────────────────┬──────────────┘
                 │ ADR-0128 mesh bytes            │ input bytes
 ┌───────────────┴───────────────────────────────▼──────────────┐
 │ worker                                                        │
 │   Go module (keelson, apps, bindings)                         │
 │      │ FFFI2 frame, fd 1 ──▶ consume()  ┐ synchronous, same   │
 │      ◀── fetch replies, fd 0 ◀──────────┘ thread, no SAB      │
 │   Rust module: interpreter → egui pass → tessellate → serialise│
 │   ClickHouse: fetch() to a same-origin proxy                  │
 └───────────────────────────────────────────────────────────────┘
```

What this changes against ADR-0077 SD1:

- No `SharedArrayBuffer`, no COOP/COEP: nothing blocks across threads, since
  the two modules that must alternate synchronously share one thread.
- The main thread never runs a Go frame. A slow frame lowers the frame rate
  instead of delaying input handling, which softens SD2's acceptance gate.
- The Rust module needs no GPU dependency at all: it is the lean mesh host of
  ADR-0128 M3 (`ctx.run` + `ctx.tessellate` + serialise) minus the carrier.
- One painter and one wire format serve both the server-tessellated
  appliance mode and the in-tab mode. That sharing is the reason to keep the
  mesh seam in-process; without it, `OffscreenCanvas` and egui's own GL
  painter would be the simpler choice.

## 4 What does not change

- **The prerequisites are ADR-0077's.** Fetch coalescing (SD3) and a frame
  envelope with push-style consumption in the interpreter's pull loop
  (`begin_consume_message`) are needed for any browser shape, since a worker
  cannot block on a port any more than on a socket.
- **The server-Go variant stays rejected** until those two land and a
  measurement compares FFFI2 bytes against mesh bytes on the dense-geometry
  scene, where the command stream would be far smaller than the 72-byte
  quads the mesh lane ships (see the multi-tenant
  [design-space page](./imzero2-multi-tenant-display-design-space.md) §6).
- **A tab earns no isolation credit** ([ADR-0087](../adr/0087-imzero2-client-compositor-compartmentalization.md)
  SD1), capslock does not compile for wasm, and the sensitivity wall's
  loopback rule ([ADR-0254](../adr/0254-model-inference-as-a-keelson-capability.md))
  has no meaning in a tab. The browser tier is a demo and per-user tier, not
  an answer to the hostile-guest design.

Rust-side portability was surveyed on the compile date: no `wasm32` cfg
exists in `rust/imzero2`, the Trunk configuration is a template leftover, and
the gates are the ones ADR-0077 SD4/SD5 list (fonts read from disk paths,
`std::time::Instant`, file writes for PNG and SVG, process exit, the jiff and
getrandom feature switches). The `headless_svg` host, a bare context plus the
interpreter with no clock, is the natural starting point.

## 5 The measurement this page led to, and the demonstrator

The shape of §3 exists as the trial's M2 (2026-09-26): both wasm modules in
one worker, the shim as the bridge, and the existing viewer page painting
the worker's mesh through a `?worker=` mode of about thirty lines. Whole
gallery frames on a handheld take 5 to 6 ms in Chromium and Firefox on
wasip1. With the Go module built as a wasip1 reactor the worker owns the
cadence and yields between frames, and the page's input reaches egui
through the carrier's own translator: fibscope, a real app, runs and takes
clicks in the tab at about 1.5 ms a frame. What the demonstrator still
lacks is fonts as bytes, the session-control channel (resize, cursor,
clipboard), a reactive cadence, and the wasm-bindgen glue a few
dependencies expect.


The Phase-0 spike now exists as the
[keelson-wasm-frame-cost](../trials/keelson-wasm-frame-cost/README.md)
trial: the unmodified Go application layer runs against a stub peer that
interprets nothing, natively and under both wasm targets, in Node and in
headless browsers, with the bridge of §3 as the fd 0/1 shim. Quote its
figures only from that page's §0.

## 6 Next steps, in order

1. Read the trial's §0 and decide whether ADR-0077's gate holds for the frame
   shapes that matter. The trial found that for synthetic frames the cost is
   set by messages per frame, and that a real play frame is about 180
   messages, so what the 4× wasm ratio multiplies there is Go's own
   per-frame work — decoding, formatting, the deferred body — at 3 to 8 ms
   natively on a handheld.
2. Land fetch coalescing (ADR-0077 SD3) natively. Deferred flushing, the
   larger of the two transport levers by the trial's numbers, is the
   channel's default since 2026-09-25.
3. Add the frame envelope and push consumption to the interpreter.
4. Record §3 as a new ADR referencing ADR-0077, since it amends SD1 and
   partly reverses the O3 kill reason; an accepted ADR only takes dated
   updates. Done as
   [ADR-0263](../adr/0263-imzero2-browser-both-modules-in-one-worker-mesh-to-the-painter.md),
   proposed 2026-09-26.
