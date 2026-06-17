---
type: explanation
audience: contributor or operator
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-06-17
---

# showcase — runtime architecture

How the running demonstrator is wired: the processes, who spawns whom, and the
transport on every edge from a rendered frame to a browser tab and back.

This is the **coarse-grained** picture. It does not cover how the box is built or
deployed — see [`README.md`](README.md) for the transport/demonstrator split and
the two delivery paths, [`DEPLOY.md`](DEPLOY.md) for the off-box container path,
and [`onbox/ONBOX.md`](onbox/ONBOX.md) for the on-box self-updating path. The
*decisions* behind the shapes below live in their ADRs, cited inline; this
document explains the structure, not the choices.

## What is actually in the box

Of the parts below, only **two run in the deployed box**: Caddy and the imzero2
container. Inside that container, one process tree (`main_go` → `imzero2` →
`ffmpeg`) does the work. `clickhouse-local` is **optional** — absent from the
default image, spawned only by demos that need it. A `clickhouse` **server** is
**external** — a handful of demos dial out to one; the box never runs it.

The framing invariant (ADR-0024): `imzero2` is the **transport** — offscreen
egui render → encoded video → WebSocket — and `showcase` is *what* is
transported. That separation is why the deploy machinery lives here at the repo
root and treats imzero2 as a dependency.

## Process inventory

| Process | Language | Role | Spawned by | In default box? |
|---|---|---|---|---|
| `caddy` | — | TLS termination + password gate + reverse proxy; the only published port | container runtime | yes |
| `main_go` | Go | The keelson "brain": app runtime (ADR-0026), emits egui2 UI opcodes, hosts the in-process bus | `tini` (PID 1) | yes |
| `imzero2` | Rust | The carrier: offscreen wgpu render, WebSocket server, NUT demux, serves the viewer page | `main_go` | yes |
| `ffmpeg` | — | Encodes raw frames → H.264 / VP9 / AV1 | `imzero2` (Rust) | yes |
| `clickhouse-local` | — | Embedded one-shot/pooled query engine: `--launch` resolver + leeway readback demos | `main_go` | **no** (added only in the `+CH` image variant) |
| browser | — | WebCodecs viewer: decodes video to a canvas, sends input back | the viewer | n/a (the client) |
| `clickhouse` server | — | Long-running SQL server over HTTP `:8123` | **external** | **no** (off-box) |

## Data-flow topology

```
  ┌─────────┐         ┌─────────┐              ┌──────────────────┐          ┌───────────────┐
  │ browser │◀══ WSS ═▶│  caddy  │◀═ WS/HTTP ══▶│  imzero2 (Rust)  │◀═ stdio ═▶│ main_go (Go)  │
  │ viewer  │   :443   │ TLS+auth│    :8089     │ render + carrier │  FFFI2   │  app runtime  │
  └─────────┘         └─────────┘   internal   └────────┬─────────┘ opcodes  └───────┬───────┘
   WebCodecs          the only                          │ ▲                          │ exec / bus
   decode→canvas      published surface   raw BGRA stdin │ │ NUT stdout               ▼
   input→ws                                         ┌────▼─┴───┐              ┌─────────────────┐
                                                    │  ffmpeg  │              │ clickhouse-local│
                                                    │  encode  │              │   (optional)    │
                                                    └──────────┘              └─────────────────┘
                                                                       clickhouse server :8123
                                                                       (external) ──▶ play, hn_explorer
```

## Process tree

`tini` (compose `init: true`) is PID 1 so the whole tree is reaped cleanly on
exit; this is why the compose file calls it the "Go→Rust→ffmpeg process tree".

```
caddy                          (container 1 — the only public surface)

imzero2 container:
  tini                         (PID 1, reaps the tree)
   └─ main_go .................. Go driver: app runtime, emits egui2 opcodes
       ├─ imzero2 ............... Rust client: offscreen render + ws carrier + NUT mux
       │    └─ ffmpeg .......... encodes raw BGRA → H.264/VP9/AV1   (child of Rust)
       └─ clickhouse-local ..... spawned on demand; absent by default
```

`main_go` is built from
[`github.com/stergiotis/boxer/public/thestack/cmd/imzero2`](../public/thestack/cmd/imzero2);
the entrypoint runs its `imzero2 demo` subcommand, which spawns the Rust client
named by `--clientBinary` (see [`entrypoint.sh`](entrypoint.sh) and the spawn in
[`github.com/stergiotis/boxer/public/thestack/imzero2/application`](../public/thestack/imzero2/application)).
The baked Rust binary is a **headless-only build** (`build_rust_headless.sh`), so
it always renders offscreen and streams rather than opening a window.

## The edges

| Edge | Transport | Payload / direction |
|---|---|---|
| browser ⇄ caddy | **HTTPS / WSS** on `:443` | Caddy adds the TLS (Let's Encrypt) and HTTP basic-auth the v1 carrier does not ship (ADR-0082). See [`Caddyfile`](Caddyfile). |
| caddy ⇄ imzero2 | **HTTP / WS** → `imzero2:8089` | internal network, never published; `flush_interval -1` so the long-lived video socket streams unbuffered. The carrier multiplexes the viewer page and the WebSocket on one port (HTTP-vs-WS sniffed). |
| imzero2 ⇄ main_go | **stdio pipes**, length-prefixed FFFI2 opcode frames | Go→Rust: egui2 UI opcodes. Rust→Go: input events + responses. The **same channel serves windowed and headless modes** — headless only adds the offscreen-render + encode tail. |
| imzero2 → ffmpeg | **stdin pipe**, raw `bgra` (`-f rawvideo -pix_fmt bgra …`) | a depth-1 latest-wins mailbox feeds it, so the render thread never blocks on a slow encoder (ADR-0024, SD9). |
| ffmpeg → imzero2 | **stdout pipe**, NUT container (`-f nut`) | Rust demuxes NUT, re-wraps each coded frame as a `VideoChunk` and pushes it onto the WebSocket channel. |
| main_go → clickhouse-local | **exec** + Arrow IPC on stdin / TSV on stdout, or a **pooled worker over the in-process bus** | one-shot for `--launch`; warm pool (ADR-0028) for readback demos. |
| main_go → clickhouse server | **HTTP** `:8123` | external cluster, not on the box. |

## Flow: pixels out

1. `main_go` emits egui2 opcodes; the Rust carrier interprets them and renders
   egui to a **wgpu offscreen texture** (BGRA, software Mesa lavapipe/llvmpipe in
   the container).
2. The frame is read back to CPU and submitted to a **depth-1 mailbox**
   (latest-wins). The render loop never blocks here — a stale frame is dropped,
   not queued (ADR-0024, SD9).
3. A feeder thread writes the latest BGRA frame to **ffmpeg's stdin**; ffmpeg
   encodes with the active codec lane and muxes to **NUT** on stdout.
4. A drain thread runs a **NUT demuxer**, re-frames each coded picture as a
   `VideoChunk` (type-byte `0x01`), and pushes it to the WebSocket.
5. Caddy proxies the socket to the browser; the **WebCodecs `VideoDecoder`**
   decodes each chunk and draws the `VideoFrame` to a canvas.

The render, encode, and serve stages live in the Rust carrier
([`rust/imzero2/src/imzero2/headless.rs`](../rust/imzero2/src/imzero2/headless.rs),
[`encoderpipe.rs`](../rust/imzero2/src/imzero2/encoderpipe.rs),
[`nutreader.rs`](../rust/imzero2/src/imzero2/nutreader.rs),
[`wscarrier.rs`](../rust/imzero2/src/imzero2/wscarrier.rs)); the viewer is a
single embedded page,
[`viewer/index.html`](../rust/imzero2/src/imzero2/viewer/index.html).

## Flow: input + control back (same socket)

- **Input** (`0x02`): the browser sends mouse / keyboard / touch as `InputEvent`
  frames. The carrier drains them each render tick and
  [`inputmap.rs`](../rust/imzero2/src/imzero2/inputmap.rs) translates them to
  `egui::Event`s, which flow back to `main_go` over the same opcode channel — so
  app logic sees browser input identically to a local window.
- **Control** (`0x03`): the browser probes its own decode capability
  (`VideoDecoder.isConfigSupported`, `navigator.mediaCapabilities`) and reports
  it host-ward. `main_go` uses that to pick an encoder lane; a runtime codec
  switch comes back as an opcode, and the carrier reaps and respawns ffmpeg on a
  fresh lane with a new keyframe (ADR-0088). Lane selection — hardware VAAPI vs
  software libx264/libopenh264/libvpx/SVT-AV1 — is in
  [`codeclane.rs`](../rust/imzero2/src/imzero2/codeclane.rs).

## Durable properties

These hold regardless of how the code is refactored, and are the reason for the
shapes above:

- **The render loop is never blocked by the network or the encoder.** A
  latest-wins mailbox sits between rendering and encoding; under congestion
  frames coalesce *before* encode, so a slow viewer degrades frame rate, not
  responsiveness (ADR-0024, SD9).
- **Pixels never cross the Go boundary.** ffmpeg is a child of the Rust renderer
  and reads frames in-process; the Go↔Rust channel carries only UI opcodes and
  input. The pixel path and the control path are physically separate streams.
- **One opcode channel serves both modes.** Windowed and headless differ only in
  the render *sink*; the Go application is unaware which one is attached.
- **Caddy is the entire security boundary.** The v1 carrier has no auth or TLS
  (ADR-0082 is proposed, not built); the app port is never published. Removing
  Caddy exposes an unauthenticated carrier — do not publish `:8089`/`:8090`.
- **One viewer at a time.** v1 is single-session by construction; a second
  connection is rejected.

## The two ClickHouse roles

`clickhouse-local` (embedded, transient) and a `clickhouse` server
(long-running, networked) are unrelated, and **neither is mandatory**.

- **`clickhouse-local` — the `--launch` resolver.** The app registry is itself
  queryable: `main_go` serializes registered app manifests to Arrow IPC and runs
  `SELECT id FROM … WHERE …` through `clickhouse-local` to resolve which demo a
  `--launch` expression names (see
  [`github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/carousel`](../public/thestack/imzero2/egui2/demo/carousel)).
  Because this is a hard dependency that adds ~2 GB, the **default image omits it
  and boots the in-browser carousel**, which needs no SQL. The `+CH` image
  variant bakes it in for `LAUNCH=<demo>` (see [`demo.env.example`](demo.env.example)).
- **`clickhouse-local` — readback demos.** Demos such as `regex_explorer` and
  the time-range picker evaluate SQL through a **warm pool** of workers
  ([`github.com/stergiotis/boxer/public/keelson/data/chlocalpool`](../public/keelson/data/chlocalpool)
  /
  [`github.com/stergiotis/boxer/public/keelson/data/chlocalbroker`](../public/keelson/data/chlocalbroker),
  ADR-0028) reached over the in-process bus, not a per-query spawn.
- **`clickhouse` server (HTTP `:8123`) — external.** Only `play` (a SQL
  playground, default `localhost`) and `hn_explorer` (a separate public cluster
  via `HN_EXPLORER_CLICKHOUSE_URL`) dial a server, through
  [`github.com/stergiotis/boxer/public/keelson/data/chclient`](../public/keelson/data/chclient).
  The showcase box does not run one.

## Where the decisions live

| Area | ADR |
|---|---|
| Remote access — browser viewer, pixel streaming, SD9 pacing | [ADR-0024](../doc/adr/0024-imzero2-remote-access-browser-viewer.md) |
| Runtime codec pipeline + viewer decode capabilities | [ADR-0088](../doc/adr/0088-imzero2-runtime-codec-pipeline-and-viewer-capabilities.md) |
| Remote session auth + TLS (the Caddy boundary; proposed) | [ADR-0082](../doc/adr/0082-imzero2-remote-session-auth-tls.md) |
| Pull-build-atomic deploy (the two delivery paths) | [ADR-0085](../doc/adr/0085-imzero2-demo-pull-build-atomic-deploy.md) |
| App runtime + capability subjects (the registry) | [ADR-0026](../doc/adr/0026-app-runtime-and-capability-subjects.md) |
| Low-latency clickhouse-local SQL capability | [ADR-0028](../doc/adr/0028-chlocal-low-latency-sql-cap.md) |
