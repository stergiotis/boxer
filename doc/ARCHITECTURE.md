---
type: explanation
audience: contributors and integrators deciding how to run boxer and where its data lives
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-23
---

> This page decides nothing; where it and an ADR disagree, the ADR is the
> record. It flattens the journey the ADRs carry in their dated Updates, and
> every number on it is quoted from the ADR or trial named beside it, with
> that record's date — none was re-measured for this page.

# Boxer architecture — how it runs, and where its data lives

Boxer is a data-engineering toolkit and app stack over ClickHouse, written in
Go with a Rust-rendered UI ([README](../README.md),
[why-boxer](./explanation/why-boxer.md)). This page draws the two questions a
deployment answers before anything else: **how the UI reaches a person** — the
operation modes, from a desktop window to a 109 MB appliance image — and
**where data lives** — the two ClickHouse engines, the `boxer.*` tables,
ad-hoc datasets, the filesystem snapshot store and its rclone seam. Each
section names the decision records that carry the *why*; the page only
carries the shape they add up to, which no single ADR states.

**In two sentences.** Boxer runs data apps from one Go host process — a Rust
client beside it only renders, helpers (`clickhouse-local` workers, `ffmpeg`,
`rclone`) ride pipes, and a ClickHouse server is the only durable store. The
same app runs on a desktop, in a browser, or from a 109 MB appliance image,
and everything durable — app state, metrics, query runs, even file trees —
lands in one queryable table shape. These two sentences are what the
[positioning statement](./explanation/positioning-statement.md) took from
this page; §5 says where each clause comes from, and what the architecture
alone cannot supply.

Three facts make the drawings possible at all, and recur throughout:

- **One app source, every host.** A keelson app is four methods —
  `Manifest` / `Mount` / `Frame` / `Unmount` — and "placement is the host's
  call, not the app's … the same app source runs unchanged across hosts"
  ([ADR-0026 §SD8](./adr/0026-app-runtime-and-capability-subjects.md)).
  Nothing in §2 changes an app.
- **The Rust side renders and nothing else.** The Go process owns the apps,
  the bus, the data paths and — increasingly — the network; the Rust client
  interprets a frame stream and presents it. The Rust-side I/O that remains
  is the remote carrier's WebSocket and the encoder pipe; basemap tiles, the
  last HTTP client on that side, moved to Go on 2026-08-22
  ([§1.2](#12-where-network-io-lives)).
- **Two ClickHouse engines, no shared state.** One-shot `clickhouse-local`
  workers serve interactive and introspective queries; a ClickHouse server
  holds everything durable ([§3.1](#31-two-engines-no-shared-state)).

## 1. The stack at a glance

```text
                    a seat · a browser · an agent · an rclone client
                      ▲ window      ▲ WebSocket: video | meshes | a11y tree     ▲ SFTP over a pipe
                      │             │                                           │
 ┌────────────────────┴─────────────┴───────────────────────────────┐           │
 │ Rust process — the imzero2 client (egui)                         │           │
 │   FFFI2 interpreter ─▶ egui pass ─▶ one host loop per build:     │           │
 │   desktop window | headless carrier (+ wgpu or CPU rasterizer,   │           │
 │   + ffmpeg subprocess, or the mesh lane) | SVG export            │           │
 └────────────────────────────────▲─────────────────────────────────┘           │
                                  │ FFFI2: opcode stream down, Sync readback up │
                                  │ (stdin/stdout pipes, lock-step per frame)   │
 ┌────────────────────────────────┴─────────────────────────────────────────────┴─────────────────┐
 │ Go process — the keelson host                                                                  │
 │   apps/ (play · sqlapplet · tally · imztop · …) ─── egui2 bindings ─── windowhost              │
 │   bus (inprocbus, or natsbus) ── a capability is a subject filter ── brokers: fs · clipboard · │
 │      persist · ad-hoc datasets · tasks · launch                                                │
 │   facts store ──▶ boxer.facts     introspection: keelson('…') tables, /table and /query (HTTP) │
 │   chlocal broker + pool ──▶ clickhouse-local workers (one-shot, over pipes)                    │
 │   query-engine adapters ──▶ ClickHouse server (HTTP, Arrow)      lading ◀──▶ rclone (pipes)    │
 └──────────────┬──────────────────────────────────────────────┬──────────────────────────────────┘
                ▼ HTTP :8123 — Arrow IPC in, ArrowStream out   ▼ spawn; SQL on stdin, bytes on stdout
      ClickHouse server: the boxer.* tables          clickhouse-local workers: no persistence
```

Four kinds of process, and the boundaries between them:

| Boundary | Carries | Record |
| --- | --- | --- |
| Go ⇄ Rust, **FFFI2** over the child's stdin/stdout | one opcode stream per frame down, `Sync()` readback up; lock-step, one Go frame per Rust pass | [ADR-0024](./adr/0024-imzero2-remote-access-browser-viewer.md) §Context, [ADR-0062](./adr/0062-imzero2-render-cadence.md) |
| Go ⇄ ClickHouse server, **HTTP** | Arrow IPC bulk writes, `FORMAT ArrowStream` reads; one statement per request, no `FORMAT` of its own | [ADR-0089](./adr/0089-rowdml-serialization-clickhouse-native-ingestion.md), [ADR-0184 §SD2](./adr/0184-sysmetrics-persistence-tee.md) |
| Go ⇄ `clickhouse-local`, Rust ⇄ `ffmpeg`, boxer ⇄ `rclone`: **pipes** | SQL in / bytes out; BGRA frames in / an elementary stream out; SFTP in either direction | [ADR-0028](./adr/0028-chlocal-low-latency-sql-cap.md), [ADR-0088](./adr/0088-imzero2-runtime-codec-pipeline-and-viewer-capabilities.md), [ADR-0198](./adr/0198-fs-snapshot-store.md) |
| Rust ⇄ browser, **one WebSocket** | `0x01` video chunk, `0x02` protobuf input, `0x03` session control, `0x04` mesh frame; the viewer page on port + 1 | [ADR-0024 §SD6](./adr/0024-imzero2-remote-access-browser-viewer.md), [ADR-0128](./adr/0128-imzero2-mesh-draw-stream-codec-lane.md) (proposed) |

The keelson host is one Go binary — for the GUI stack,
[`public/thestack/cmd/imzero2`](../public/thestack/cmd/imzero2) (`imzero2 demo
--launch <app>`), whose carousel constructs the bus, the brokers, the facts
store, the introspection host and the window host; the `boxer` CLI at
[`public/app`](../public/app) carries the daemons and tools. The spine itself
lives under [`public/keelson`](../public/keelson) — `runtime/` (apps, bus,
brokers, facts, introspection), `data/` (ClickHouse clients, the chlocal pool,
the SQL pass registry), `security/` (the capslock gate); the apps under
[`apps/`](../apps) are hosted, never imported by the spine
([ADR-0035](./adr/0035-keelson-namespace-introduction.md)).

### 1.1 What a mode is made of

Every operation mode in §2 is the same Go process with a different **Rust
host loop**, selected at build time by Cargo feature and at run time by two
environment variables. The Go side does not know which one it got.

| Build (`rust/imzero2`) | Host loop | Who rasterizes | What it needs on the box | Build script |
| --- | --- | --- | --- | --- |
| `desktop` (default, with `inspection`, `fast_alloc`) | eframe + winit window | GPU via wgpu | a display server, a GPU or GL/Vulkan driver | [`build_rust.sh`](../rust/imzero2/build_rust.sh) |
| `headless_wgpu` | WebSocket carrier | GPU via wgpu, offscreen texture + readback | Vulkan loader + ICD (a real GPU, or Mesa's lavapipe); `ffmpeg` for a video lane | [`build_rust_headless.sh`](../rust/imzero2/build_rust_headless.sh) |
| `headless_soft` | WebSocket carrier | CPU, `egui_software_backend` on a rayon pool | four shared libraries; `ffmpeg` for a video lane | [`build_rust_headless_soft.sh`](../rust/imzero2/build_rust_headless_soft.sh) |
| `headless` | WebSocket carrier, mesh lane only | nobody on the box — the browser's WebGL2 painter | nothing beyond the binary | [`build_rust_headless_mesh.sh`](../rust/imzero2/build_rust_headless_mesh.sh) |
| `headless_svg` | SVG export over HTTP | nobody — shapes become `<svg>` | nothing | [`build_rust_headless_svg.sh`](../rust/imzero2/build_rust_headless_svg.sh) |

A build carrying both raster hosts resolves to wgpu
([ADR-0205 §SD1](./adr/0205-imzero2-cpu-rasterized-pixel-host.md)); a dual
`desktop`+`headless` build picks the headless loop when `IMZERO2_HEADLESS=1`
(ADR-0024 §SD1). On a carrier host the codec lane is
`IMZERO2_HEADLESS_CODEC` — `h264` · `vp9` · `av1` · `av1-444` · `mesh` — and,
absent a setting, `CodecLane::best` probe-encodes a few real frames per
candidate and falls back to the mesh lane when no encoder works
(ADR-0088 §SD5, ADR-0128 M1). That fallback is what the ffmpeg pair of appliance images in
§2.5–2.6 demonstrates rather than configures. The dev launcher
[`rust/imzero2/hmi_headless.sh`](../rust/imzero2/hmi_headless.sh) picks the
build from the same knobs — `IMZERO2_HEADLESS_CODEC=mesh` for the lean host,
`HMI_RASTER=soft` for the CPU rasterizer, the GPU host otherwise. The runtime
variables are registered in [doc/env-vars.md](./env-vars.md)
([ADR-0009](./adr/0009-environment-variable-registry.md)).

### 1.2 Where network I/O lives

The Go process owns egress policy — proxy, certificate trust, timeouts, the
capability gate of [ADR-0026 §SD10](./adr/0026-app-runtime-and-capability-subjects.md),
external binaries behind the [`public/extbin`](../public/extbin) chokepoint of
[ADR-0118](./adr/0118-extbin-external-process-chokepoint.md). Two things
still reach outside the box from the Rust client:

1. the carrier WebSocket of the headless hosts (`tokio`, kept);
2. the `ffmpeg` subprocess, resolved Rust-side through `IMZERO2_FFMPEG_BIN`
   — outside the extbin registry, which is a Go-side audit surface
   (ADR-0118 §Scope: resolution is centralised, spawning is not).

A third did until 2026-08-22: basemap tiles. The `walkers` map widget fetched
them in Rust through `reqwest`, and with it `rustls`, `ring`, `hyper`, `tokio`
and an HTTP cache — 40 crates, 24 owners, 1,057,461 bytes of machine code for
a widget of 13,826 bytes (2026-08-22,
[ADR-0203 §Context](./adr/0203-map-widget-without-the-http-stack.md),
proposed). [ADR-0204](./adr/0204-leaflet-map-core-port.md) (proposed) ports
Leaflet's map core to Go on the painter lane instead:
[`public/thestack/imzero2/egui2/widgets/portolan`](../public/thestack/imzero2/egui2/widgets/portolan)
fetches with `net/http`, caches compressed bytes, and ships decoded tiles
through the existing `paintImage` primitive — no new IDL. Its M0–M4 landed
2026-08-22: play and terrainscope run on it, and M4 removed the walkers
binding — `walkers`, `reqwest` and `h3o` left the manifest, distinct crates
went 435 → 313 for the desktop build and 321 → 166 for `headless_soft`, and
`reqwest`, `rustls`, `ring`, `hyper` appear in neither tree (figures re-taken
2026-08-23, ADR-0204 M4). [ADR-0056](./adr/0056-walkers-map-h3-binding.md)'s
binding is gone — its 2026-08-22 Update records the supersession — and
[ADR-0165](./adr/0165-imzero2-tile-transport-over-fffi2.md) (proposed) is
folded into the port.

Why this matters beyond tidiness: `ring` and `libmimalloc-sys` were the only
two crates in the headless closure that compile C, and exactly the two that
stopped a static musl build (340 of 348 crates checked clean without a musl C
compiler, 2026-08-22, ADR-0203 §Context). `mimalloc` sits behind a `fast_alloc`
feature ([ADR-0206 §SD4](./adr/0206-gokrazy-appliance-image.md), proposed) and
`ring` left with the map port, so `cargo check --target
x86_64-unknown-linux-musl --no-default-features --features headless_soft` now
passes with `fast_alloc` off (ADR-0204 M4, 2026-08-23): no C toolchain stands
between the headless host and a static musl appliance. The images in §2.4
still ship a glibc closure beside the Rust host; a musl-static image is
ADR-0206 M4, not yet built.

## 2. Operation modes

Three axes place every mode: **who rasterizes** (a GPU, the CPU, the viewer's
browser, or nobody), **what leaves the box** (nothing, pixels as video,
tessellated meshes, SVG, an accessibility tree), and **what is underneath**
(a desktop seat, a server OS, a gokrazy appliance image). The five modes the
project runs today sit on that grid as follows; §2.7 lists the
non-interactive lanes beside them and §2.8 compares.

```text
                    rasterized on the box by …          nothing on the box rasterizes
                    ───────────────────────────         ─────────────────────────────
   what leaves      GPU (wgpu)        CPU (software)    meshes → browser WebGL2
   the box
   ────────────────────────────────────────────────────────────────────────────────
   nothing          §2.1 desktop      —                 —
   video/WebSocket  §2.2 headless     §2.3 headless     —
                    (server OS)       soft (server OS)
                                      §2.6 appliance
                                      pixel stream
   meshes/WebSocket —                 —                 §2.5 appliance mesh
   SVG, a11y tree   §2.7 headless_svg · the carrier test driver (either raster host)
```

### 2.1 Desktop

```text
   a seat: display server (Wayland or X11) + a GPU
   ┌───────────────────────┐   FFFI2, pipes     ┌────────────────────────────────────┐
   │ Go — keelson host     │ ─────────────────▶ │ Rust — imzero2 client              │
   │ apps · bus · brokers  │ ◀───────────────── │ eframe + winit + wgpu → the window │
   │ facts · chlocal · …   │   Sync readback    │ egui_mcp on 127.0.0.1:5719 (opt-in)│
   └───────────┬───────────┘                    └────────────────────────────────────┘
               │ HTTP                                        ▲ input from the compositor
               ▼
   ClickHouse server · clickhouse-local workers
```

The baseline ADR-0024 describes before it adds anything: the Go host spawns
the Rust client, FFFI2 rides the child's pipes, the client presents on a real
window. Nothing leaves the machine.

- **For:** lowest input-to-photon path (no encode, no decode, no network);
  the full GPU; the inspection seam — `EGUI_INSPECTION=1` attaches an agent
  to the live widget tree ([doc/howto/egui-mcp.md](./howto/egui-mcp.md));
  one person, one seat, no auth question.
- **Against:** needs a seat — a compositor and a GPU driver stack, the
  dynamically linked C closure that [ADR-0128 §Context](./adr/0128-imzero2-mesh-draw-stream-codec-lane.md)
  names as the appliance's problem and the lanes below exist to shed; the
  continuous render cadence keeps drawing when the window is occluded —
  96.8 % of a core at 1 fps (2026-08-18,
  [ADR-0062 Updates](./adr/0062-imzero2-render-cadence.md)), an upstream
  egui fix brings it to 0.1 %; `IMZERO2_RENDER_CADENCE=reactive` is the
  mitigation a widget that mutates state without requesting a repaint pays
  for.
- **Status:** shipped; launch via [`rust/imzero2/hmi.sh`](../rust/imzero2/hmi.sh).

### 2.2 Headless, GPU-rasterized — the remote-access lane

```text
   a server OS (systemd), a GPU or Mesa lavapipe, ffmpeg on PATH or pinned
   ┌────────────────┐ FFFI2 ┌─────────────────────────────────────────────────────────────┐
   │ Go host        │ ◀───▶ │ Rust headless host (no eframe, no winit)                    │
   │ apps · keelson │       │  egui pass ─▶ wgpu offscreen texture ─▶ readback BGRA       │
   └────────────────┘       │  ─▶ frame mailbox (depth 1, latest wins) ─▶ encoder feeder  │
                            │  ─▶ ffmpeg subprocess ─▶ NUT demux ─▶ WsCarrier             │
                            └─────────────────────▲───────────────────────────────────────┘
                        one WebSocket: 0x01 video · 0x02 input · 0x03 session · 0x04 mesh
                                                  │
                         ┌────────────────────────┴──────────────────────────┐
                         │ browser: single-file viewer page (port + 1)       │
                         │ WebCodecs VideoDecoder → canvas; DOM input → proto│
                         │ one active viewer, N passive (roster server-side) │
                         └───────────────────────────────────────────────────┘
```

[ADR-0024](./adr/0024-imzero2-remote-access-browser-viewer.md) (2026-05-10,
accepted) puts the same Go ⇄ Rust pair on a server with no window system: the
Rust host runs `egui::Context::run_ui` and `egui_wgpu` against an offscreen
texture, reads the frame back, and hands it through a depth-one mailbox to an
external `ffmpeg` (invoked, never linked). The codec is chosen at run time from
what the host can encode and the browser can decode — the browser reports
WebCodecs capabilities, the host probe-encodes, Go renders the choice as a
widget ([ADR-0088](./adr/0088-imzero2-runtime-codec-pipeline-and-viewer-capabilities.md));
lanes are H.264 (`h264_vaapi` or `libopenh264`), VP9, AV1 and AV1-444. Viewers
are one active and N passive with a server-authoritative roster
([ADR-0086](./adr/0086-imzero2-active-passive-viewers-and-roster.md)); the
on-box deployment is a pull-build-and-atomic-deploy timer
([ADR-0085](./adr/0085-imzero2-demo-pull-build-atomic-deploy.md)) and the
hardware-encode recipe is
[doc/howto/amd-hardware-video-encoding.md](./howto/amd-hardware-video-encoding.md).

- **For:** server-resident apps reachable from any WebCodecs browser; several
  viewers on one frame stream; hardware encode where VAAPI works; the wgpu
  render is the fidelity reference every other raster path is compared to.
- **Against:** it wants a GPU — on lavapipe the rasterizer costs 102.6 ms of
  CPU per frame at 1920×1200 against 1.32 ms on an integrated GPU
  (2026-08-22, [egui-software-backend survey §14.1](./adr-background-work/egui-software-backend-survey.md));
  Mesa plus `ffmpeg` is the C closure ADR-0128 calls the appliance's problem;
  encode and decode add about two frames of latency; software H.264 encode is
  in the 14–18 ms per frame class at 1280×800 @ 30 fps (2026-07-18,
  [ADR-0128 §Context](./adr/0128-imzero2-mesh-draw-stream-codec-lane.md));
  **authentication and TLS are decided but unbuilt** —
  [ADR-0082](./adr/0082-imzero2-remote-session-auth-tls.md) is accepted, the
  carrier today has neither, and the non-loopback refusal lives in a shell
  script ([ADR-0206 §SD5](./adr/0206-gokrazy-appliance-image.md), proposed),
  so a reverse proxy supplies both.
- **Status:** shipped; [`rust/imzero2/hmi_headless.sh`](../rust/imzero2/hmi_headless.sh),
  recipe in [doc/howto/launch-apps-non-interactively.md](./howto/launch-apps-non-interactively.md).

### 2.3 Headless, CPU-rasterized — `headless_soft`

The same carrier, the same `ffmpeg`, one function swapped: `render_and_readback`
tessellates and hands the pass to the vendored
[`rust/imzero2/vendor/egui_software_backend`](../rust/imzero2/vendor/egui_software_backend),
which rasterizes into the frame buffer on a rayon pool — four workers by
default, capped at half the hardware threads
([ADR-0205](./adr/0205-imzero2-cpu-rasterized-pixel-host.md), accepted
2026-08-22). No GPU, no Vulkan loader, no ICD, no Mesa: `ldd` on the binary
lists four files, about 4.8 MB.

```text
   Rust headless host          render_and_readback()
   ┌───────────────────────────────────────────────────────────────────────┐
   │ egui pass ─▶ tessellate ─▶ ┌ wgpu offscreen + readback ┐ ─▶ BGRA frame│
   │                            └ or: CPU rasterizer, 4 thr ┘              │
   │ downstream never learns which: mailbox ─▶ ffmpeg | mesh | PNG capture │
   └───────────────────────────────────────────────────────────────────────┘
```

- **For:** pixels with no GPU stack — the first pixel-producing host an
  appliance could carry; 1.22 ms CPU per frame at 1280×800 (ADR-0205
  §Context, 2026-08-22), p50 0.25 ms against wgpu's 1.34 ms; 98.3 % of pixels
  identical to the wgpu render across 92 gallery images; the binary shrinks
  45.6 → 39.0 MB; CPU placement is advised in the log (`taskset` line, L3
  note).
- **Against:** memory — 228 MiB client RSS against wgpu's 118 MiB, about
  15 MiB per worker; the tail — p99 6.2 ms against 3.0 ms; a sixth crate on
  the egui version ring, vendored with a local delta; a cache keyed on texture
  *id*, so an in-place texture mutation under an unchanged mesh could persist
  stale pixels (unobserved, unguarded); the `ffmpeg` half of the C closure
  is untouched (all ADR-0205 §Consequences, 2026-08-22).
- **Status:** accepted after implementation; M0–M5 done; the musl half of M6
  open. `HMI_RASTER=soft ./hmi_headless.sh` builds and runs it
  ([`build_rust_headless_soft.sh`](../rust/imzero2/build_rust_headless_soft.sh),
  `--clientBinary target/headless-soft/release/imzero2`).

### 2.4 What the appliance adds

[ADR-0206](./adr/0206-gokrazy-appliance-image.md) (proposed 2026-08-22)
packages §2.3 as bootable x86-64 images with gokrazy — "a Go-first appliance,
non-Go binaries via `ExtraFilePaths`" — under
[showcase/gokrazy](../showcase/gokrazy/README.md): a pair that isolates
`ffmpeg`, and a third that carries ClickHouse (§SD6). The Go host, built
`CGO_ENABLED=0`, is the gokrazy package and the parent of the process tree; the
Rust host is one of its files, with its glibc closure read off the binary by
`ldd` and shipped under `/lib64`; fonts pass as flags because gokrazy has no
fontconfig. `GOWORK=off` is load-bearing and invisible (§SD3). The two images
carry byte-identical Go and Rust hosts and differ by exactly one file — a
static `ffmpeg` — so the pair isolates that variable: nothing in either image
selects a codec (§SD1). The third, `boxer-soft-play`, is the video image plus
`clickhouse-local` and `--launch play`.

```text
   boxer-soft.img  (109 MB)                 boxer-soft-video.img  (119 MB)
   ┌──────────────────────────────────┐     ┌──────────────────────────────────┐
   │ gokrazy kernel + init (A/B roots)│     │ gokrazy kernel + init (A/B roots)│
   │ main_go  — the Go host, CGO=0    │     │ main_go  — the Go host, CGO=0    │
   │ imzero2-client — headless_soft   │     │ imzero2-client — headless_soft   │
   │ /lib64: ld-linux libc libm libgcc│     │ /lib64: ld-linux libc libm libgcc│
   │ MainFont.ttf · Phosphor.ttf      │     │ MainFont.ttf · Phosphor.ttf      │
   │ (no ffmpeg) ─▶ codec falls to    │     │ /usr/bin/ffmpeg, static 7.1.1    │
   │               the mesh lane      │     │   ─▶ H.264 via libopenh264       │
   └──────────────────────────────────┘     └──────────────────────────────────┘
        both: IMZERO2_HEADLESS_LISTEN=0.0.0.0:8089, 30 fps, --launch widgets

   boxer-soft-play.img  (380 MB) = boxer-soft-video + /usr/bin/clickhouse on the
                                   A/B roots · CLICKHOUSE_URL → a server outside the image · --launch play
```

The pair booted under QEMU on 2026-08-22 (§Verification: the video image
answered 30 access units and no mesh frames, the other 0 and 30); the images
are not booted by CI, there is no size or per-frame assertion, and the CJK
font fallback (~20 MB) is dropped. The pair carries no ClickHouse and comes up
`facts:mem` / `persist:mem`. The third image answers the question
[ADR-0134 §SD8](./adr/0134-adhoc-datasets.md) had deferred to this probe:
`clickhouse-local` rides the A/B roots, not `/perm` — an update swaps the
engine and the app that expects it together, the root is read-only and
verified, and gokrazy leaves `/perm` unformatted in a file image (§SD6). It
is not what `play` queries: play talks HTTP to a ClickHouse *server*, so the
image points it at one outside itself with `CLICKHOUSE_URL` (under QEMU, the
host), while `clickhouse-local` serves the ad-hoc dataset path and the
sqlapplet books. Verified end to end — `SELECT * FROM boxer.facts` from the
appliance returned 95,826 rows in 591 ms (M3, 2026-08-22); the glibc closure
widened from four files to seven on its own, which is why `build.sh` reads it
from the binaries rather than listing it. A self-contained appliance would
need a ClickHouse server in the image — a different decision, not taken. All
three bind `0.0.0.0:8089` with no authentication and no TLS, which §SD5
records as acceptable **only** under QEMU user-mode networking; real hardware
needs ADR-0082 or an authenticating proxy first. The images do not supersede ADR-P-0001 — a deployment-substrate
decision recorded outside this repository — whose NixOS flake runs the
*showcase box* with ClickHouse and a reverse proxy at a measured 1.8–2.8 GiB
(ADR-0206 §Alternatives, quoting ADR-P-0001 Phase 1); this is the *appliance*
line.

Static linking, as it stands (2026-08-22): the Go host is static; the Rust
host is a dynamically linked glibc PIE carrying its four libraries; `ffmpeg` is
fully static, built in-tree from five pinned tarballs by
[`scripts/dev/build-static-ffmpeg.sh`](../scripts/dev/build-static-ffmpeg.sh)
for the airgapped bundle ([ADR-0095](./adr/0095-airgapped-build-bundle.md),
[doc/howto/airgapped-build.md](./howto/airgapped-build.md)) and reused here — a
static binary cannot `dlopen` a VA driver, so every hardware lane probes as
absent and `libopenh264` is the only candidate left, a supported configuration
rather than a surprise. A musl-static Rust host is ADR-0206 M4; the blocker
§1.2 names fell on 2026-08-22, and no musl image has been built yet.

### 2.5 Headless appliance, mesh lane — `boxer-soft`

```text
   appliance                                                        browser
   ┌───────────────────────────────────────────┐   0x04 on the    ┌──────────────────────────┐
   │ Go host ─FFFI2─▶ Rust host: egui pass ─▶  │   WebSocket      │ WebGL2 painter           │
   │   tessellate ─▶ ClippedPrimitive meshes + │ ───────────────▶ │ content-addressed mesh   │
   │   TexturesDelta ─▶ hash bodies, send only │                  │ bodies as static buffers;│
   │   what changed; the font atlas is a       │ ◀─────────────── │ atlas texture = the text │
   │   texture — "the atlas is the text"       │  0x02 input      │ rasterizes at viewer DPR │
   └───────────────────────────────────────────┘                  └──────────────────────────┘
   idle 85 B/frame ≈ 20 kbit/s · animated ≈ 1–2 Mbit/s deduped · bootstrap 1 MiB atlas + 34 KiB
```

[ADR-0128](./adr/0128-imzero2-mesh-draw-stream-codec-lane.md) (proposed
2026-07-18; M1–M2 landed the same day, M3 split the features) streams egui's
tessellated output — `ClippedPrimitive` meshes and `TexturesDelta`, content-
addressed and deduplicated per connection — to a WebGL2 painter in the same
single-file viewer. Nothing rasterizes on the box and nothing encodes; the host
tessellates, hashes and serializes in about 0.5 ms per frame. The `boxer-soft`
image reaches the lane by having no `ffmpeg` to probe.

- **For:** no GPU, no Mesa, no `ffmpeg` — the per-frame C dependency surface
  is gone and the host's per-frame CPU with it (~95 %, ADR-0128 §Positive);
  idle sessions cost ~20 kbit/s; text is atlas-crisp at the viewer's density
  at zero wire cost; encode and decode leave the input-to-photon path
  (measurements 2026-07-17/18, 1280×800 @ 30 fps, ADR-0128 §Context and M1).
- **Against:** bandwidth is bursty, not constant — full invalidation is
  8–34 Mbit/s raw, ÷3.2 compressed, so "fine on LAN; WAN exposure waits on the
  M4 guard"; a second viewer painter and a versioned wire format to keep; not
  bit-identical to the wgpu path (0.009 % mean absolute difference against it,
  ADR-0128 M2); a `Primitive::Callback` anywhere forces video and is a startup
  error in a mesh-only build; the ADR is still *proposed* although the lane is
  in the shipped env registry; everything in §2.4 about auth and CI applies.
- **Status:** code shipped (`IMZERO2_HEADLESS_CODEC=mesh`,
  [`meshlane.rs`](../rust/imzero2/src/imzero2/meshlane.rs)); image built and
  booted 2026-08-22 (ADR-0206 M0–M2); M4 bandwidth guard not landed.

### 2.6 Headless appliance, pixel stream — `boxer-soft-video`

```text
   appliance                                                              browser
   ┌──────────────────────────────────────────────────────────┐  0x01    ┌────────────────────┐
   │ Go host ─FFFI2─▶ Rust host: egui ─▶ CPU rasterizer ─▶    │ ───────▶ │ WebCodecs decoder  │
   │   BGRA ─▶ mailbox ─▶ ffmpeg (static, libopenh264) ─▶ NUT │          │ → canvas           │
   │   demux ─▶ H.264 access units ─▶ WsCarrier               │ ◀─────── │ input → protobuf   │
   └──────────────────────────────────────────────────────────┘  0x02    └────────────────────┘
   no VA driver can be dlopen'ed from a static binary ⇒ software encode only, by construction
```

The same image plus the 22 MB static `ffmpeg`: the CPU rasterizer of §2.3
feeds the encoder pipeline of §2.2, and the carrier advertises H.264.

- **For:** any WebCodecs browser, WebGL2 or not; the pixels are the host's
  own, so what the viewer sees is what a capture would save; bandwidth is
  rate-controlled by the encoder rather than by scene change; passive viewers
  ride the one stream cheaply ([ADR-0086 §SD6](./adr/0086-imzero2-active-passive-viewers-and-roster.md)).
- **Against:** software encode CPU on every frame (the 14–18 ms/frame class
  of §2.2 — `libx264` measured, `libopenh264` ships here); encode + decode
  latency; the static `ffmpeg` is a second supply chain of five tarballs
  whose digests the build script prints but does not pin; WebCodecs-only
  browsers; the §2.4 caveats.
- **Status:** image built and booted 2026-08-22 (ADR-0206, proposed); 1.1 ms
  per frame in Rust at 1920×984 measured in-frame on the appliance.

### 2.7 Lanes that are not a person's screen

- **SVG over HTTP** — `headless_svg` plus [`apps/svgserver`](../apps/svgserver):
  `GET /svg?…` renders a widget body to an `<svg>` from egui's pre-tessellation
  shapes. No pixels, one request at a time, prototype
  ([doc/howto/imzero2-svg-over-http.md](./howto/imzero2-svg-over-http.md)).
- **The carrier as a test driver** — [ADR-0154](./adr/0154-headless-carrier-tree-and-driver.md)
  adds an accessibility-tree export and coordinate-free actuation on the
  `0x03` session channel of any headless host; `imzero2 drive`
  ([`public/thestack/imzero2/carrierclient`](../public/thestack/imzero2/carrierclient)) runs a trace
  of `click` / `type` / `drag` / `capture` steps against it with no compositor.
  The screenshot tour and the headless scene scripts ride this lane; the same
  widget resolves to the same node id here and through `egui-mcp` on the
  desktop.
- **Keelson in the browser** — [ADR-0077](./adr/0077-keelson-browser-wasm-execution.md)
  (accepted 2026-06-21) decides a dual-module wasm execution with an in-page
  FFFI2 bridge, complementary to pixel streaming. As of 2026-08-22 no wasm
  target, build script or demo exists in the tree; its Phase 0 is the gate.
- **Withdrawn:** an in-process RDP/EGFX head
  ([ADR-0081](./adr/0081-imzero2-headless-rdp-egfx-head.md), withdrawn the
  day it was proposed — the browser path covered the need).

### 2.8 The modes, side by side

| | §2.1 desktop | §2.2 headless wgpu | §2.3 headless soft | §2.5 appliance mesh | §2.6 appliance video |
| --- | --- | --- | --- | --- | --- |
| rasterized by | GPU, on-box | GPU or lavapipe, on-box | CPU, on-box | the browser (WebGL2) | CPU, on-box |
| leaves the box | nothing | video + input | video + input | meshes + input | video + input |
| viewer needs | a seat | WebCodecs | WebCodecs | WebGL2 | WebCodecs |
| box needs | compositor, GPU stack | Vulkan + ICD, `ffmpeg` | 4 shared libs, `ffmpeg` | the image | the image (+22 MB) |
| host cost / frame, before encode (dated) | — | 1.32 ms on a GPU · 102.6 ms on lavapipe (2026-08-22) | 1.22–1.58 ms (2026-08-22) | ≈0.5 ms, nothing to encode (2026-07-18) | 1.1 ms (2026-08-22) |
| wire, idle → busy | — | constant, encoder-paced | constant, encoder-paced | 20 kbit/s → 1–2 Mbit/s, bursts to 34 Mbit/s | constant, encoder-paced |
| several viewers | no | 1 active + N passive | same | same carrier | same |
| auth / TLS | n/a | decided (ADR-0082), unbuilt | same | same, QEMU-only posture | same |
| decision record | ADR-0024 baseline | ADR-0024 / 0088 / 0086 accepted | ADR-0205 accepted | ADR-0128 + 0206 proposed | ADR-0206 proposed |

The choice follows the axes: a seat → desktop; a server with a GPU and a
VAAPI driver → headless wgpu; a server without one, or an appliance → the CPU
rasterizer, and then mesh for a LAN appliance with the lowest host cost, video
where the viewer cannot do WebGL2 or the link needs a constant rate.

## 3. Data architecture

### 3.1 Two engines, no shared state

```text
 ┌─────────────────────────────── the keelson host ────────────────────────────────┐
 │                                                                                 │
 │  ch.local.exec.<pool>   chlocal broker ──▶ pool: pre-spawned clickhouse-local   │
 │  (a bus capability)       cache (blake3 of SQL‖FORMAT‖settings, opt-in)         │
 │                                     │ SQL+FORMAT on stdin, close; drain stdout  │
 │  introspection /query ──(chhttp)────┤  one-shot workers: no listener, no reuse, │
 │  keelson('x') tables ── temp tables ┤   no persistence, no session state        │
 │  ad-hoc datasets ── url('/table/…') ┘   --max_memory_usage per worker           │
 │                                                                                 │
 │  chclient (HTTP :8123) ── Arrow IPC in · FORMAT ArrowStream out ── storeexec    │
 │     ▲ facts store · generated record stores · play (pinned base) · lading       │
 └─────┼───────────────────────────────────────────────────────────────────────────┘
       ▼
   ClickHouse server — everything durable: boxer.facts · boxer.persiststate ·
   boxer.fsmeta / fsdata / fssnap · boxer.resultsets + pin_* · boxer.mv_queryruns · boxer.tables_*
```

**chlocal** ([ADR-0028](./adr/0028-chlocal-low-latency-sql-cap.md), 2026-05-14,
accepted) keeps a small pool of `clickhouse-local` processes already past
their start-up, blocked on stdin; a request writes `<SQL> FORMAT <fmt>;`,
closes the pipe, drains stdout, and the worker exits — no reuse, no listener,
no shared state with the server. The M0 spike (2026-05-14, ClickHouse 26.3)
measured a warm worker at 7.8 ms p50 against 41.3 ms for a cold
`clickhouse-local --query`, and the pool's rationale is exactly that number:
"optimize for latency, not throughput" for an interactive UI. Defaults:
two idle workers, eight concurrent, 1 GiB per worker, a 64 MiB in-memory
result ceiling, a 60 s result cache. What it cannot do is as important as what
it does: nothing persists, bursts past the warm pool pay a cold spawn, and a
high-QPS caller "should be using `ch.query.*` against a server".

**The server** is reached over HTTP by [`public/keelson/data/chclient`](../public/keelson/data/chclient)
(Arrow IPC bulk inserts; reads appended with `FORMAT ArrowStream` and decoded
as they arrive by [`public/keelson/data/storeexec`](../public/keelson/data/storeexec)); the HTTP
interface takes one statement per request and rejects a multi-statement body
outright, which is why every persistence milestone is gated on the live HTTP
lane rather than on `clickhouse-local` tests
([ADR-0105 Updates](./adr/0105-keelson-adopts-generated-record-stores.md),
2026-08-15). The same server-side HTTP dialect is re-implemented in
[`public/db/clickhouse/chhttp`](../public/db/clickhouse/chhttp)
([ADR-0133](./adr/0133-chhttp-server-dialect-and-param-binding.md)) so that
the in-process `/query` endpoint of §3.4 speaks ClickHouse to play unchanged.

| Use | Engine |
| --- | --- |
| play and sqlapplet queries | the server, by default; the in-process plane when the statement names only `keelson()` tables (§3.4) |
| `keelson('…')` introspection, ad-hoc datasets | chlocal, through the in-process plane |
| `boxer.facts` writes, persist state, sysmetrics tee, the fs snapshot store | the server (record stores and tests can aim at `clickhouse-local`) |
| scratch: `file()`, `url()`, `s3()`, `engine=Memory`, `system.*` | chlocal |

### 3.2 What is durable — the `boxer.*` tables

Everything durable sits in one database, `boxer` (renamed from `runtime`
2026-07-21; bus subjects and vocabulary keep the `runtime.*` prefix on purpose),
and nearly all of it in one *shape*: the leeway facts table.

| Table | Holds | Written by | Shape · engine | Record |
| --- | --- | --- | --- | --- |
| `boxer.facts` | the shared append-only fact trail: grants, audit, logs, runs and heartbeats, app lifecycle, launches, workingsets, column widths, query runs, the sysmetrics tee (13 kinds), the lading mount policy | hand-rolled [`public/keelson/runtime/factsstore/chstore`](../public/keelson/runtime/factsstore/chstore) (Arrow IPC), facts-bound generated stores, a refreshable MV | facts-shaped, 21 sections / 185 physical columns; `MergeTree ORDER BY ts`; no TTL unless the operator sets one | [ADR-0026 §SD6](./adr/0026-app-runtime-and-capability-subjects.md), [ADR-0184](./adr/0184-sysmetrics-persistence-tee.md), [ADR-0115](./adr/0115-query-observability-data-plane-strategy.md) |
| `boxer.persiststate` | app durable state: one row per `Set`, entity `<app>/<key>`, newest wins, a tombstone is a delete | the generated store [`public/keelson/runtime/persist/persiststore`](../public/keelson/runtime/persist/persiststore) | own `TableDesc` with a `u8` lifecycle, which is what lets the generator emit a state view; `ORDER BY (id, ts)` | [ADR-0105 §D3a](./adr/0105-keelson-adopts-generated-record-stores.md) |
| `boxer.fsmeta` / `boxer.fsdata` / `boxer.fssnap` | the filesystem snapshot store (§3.6) | generated stores under [`public/fs/lading`](../public/fs/lading) | facts-shaped on store-owned tables; `ORDER BY (mount, snapshot, path)`; `PARTITION BY` expiry day; `TTL` | [ADR-0198](./adr/0198-fs-snapshot-store.md) |
| `boxer.resultsets` + `boxer.pin_<fp>` | pinned query results: one metadata row, one content-addressed table per pin carrying the result's own Arrow schema | play | plain columns | [ADR-0115](./adr/0115-query-observability-data-plane-strategy.md) |
| `boxer.mv_queryruns` | the refreshable materialized view that pulls `queryrunsd`'s `/pull` into `boxer.facts` every 5 s — ClickHouse owns the insert | [`public/keelson/runtime/queryrunsvc`](../public/keelson/runtime/queryrunsvc) reconciles it at boot | MV over `url(…, 'ArrowStream')` | ADR-0115 |
| `boxer.tables_*` | the data catalog: every discovered table classified opaque / leeway, restoration payloads, pairwise compatibility — rebuilt whole per run | the catalog command | plain columns | [ADR-0170](./adr/0170-data-catalog-competence.md) |

Not tables, though they read like them: **`keelson('…')`** names are in-process
providers materialized to Arrow on demand — `env`, `apps`, `windows`,
`extbin`, `workingsets`, `subscriptions`, `tasks`, `tables`, `columns`, … —
served at `GET /table/<name>` as `ArrowStream` on a loopback-bound HTTP server,
or projected straight into a chlocal worker as temporary tables
([ADR-0094](./adr/0094-keelson-introspection-tables.md)). Nothing is stored.
**Ad-hoc datasets** (§3.5) are encrypted files, not rows. **chlocal** persists
nothing.

### 3.3 Why "facts-shaped", and what a generated record store is

A leeway table has a **backbone** of plain columns — in `boxer.facts`: `id`,
`naturalKey`, `ts`, `expiresAt` — and a **payload** of *sections*, one per
canonical value type (`symbol`, `bool`, `u8` … `i64Array`, `f64Array`,
`timeArray`, `foreignKey`, …), each expanding into a value lane plus
membership lanes. A row's content is not a schema: an attribute is a value in
a section tagged by registry-minted membership ids, a component is a flat Go
DTO whose `lw:` tags bind fields to `(section, membership)` slots, and the
encoding aspects are baked into the physical column names. The consequence
this repository keeps repeating is that **a new kind of durable fact is a DTO
plus a few vocabulary entries and never a schema change** — it inherits the
shared read access, the bus codecs, the `LW_*` query vocabulary and play's
surfaces by adopting the shape, and the only thing a table still owns is its
life cycle: key, partition, TTL, indexes
([doc/explanation/leeway-sql-read-surface.md](./explanation/leeway-sql-read-surface.md),
[the leeway-beginner skill](./skills/leeway-beginner/SKILL.md)).

A **generated record store** ([ADR-0100](./adr/0100-recordstore-generated-leeway-clickhouse-store.md))
is the generator that turns such a DTO set into DDL, an Arrow writer, a
`Begin/Add/Commit` API, read-back and a state view; keelson adopted it for
durable facts in [ADR-0105](./adr/0105-keelson-adopts-generated-record-stores.md).
Two bindings exist: a store with its own `TableDesc` and its own DDL
(`persiststate`, the three lading tables), or a *facts-bound* store that writes
into `boxer.facts` and is emitted `ExternallyProvisioned` because `chstore` is
that table's sole DDL author (the sysmetrics tee, the lading policy kind).
ADR-0198's design space names the trade the lading tables made: O3, "the facts
*shape* on store-owned *tables*", keeps lifecycle control and the leeway
tooling at once; O4, rows in `boxer.facts`, would have given up retention and
indexes to whoever writes most. The measured bill (2026-08-19,
[fs-snapshot-store M0 trial](./trials/fs-snapshot-store-m0/README.md)): about
2× the insert wall-clock of a bespoke table at 1 MiB blocks, and essentially
nothing on storage — the 184 columns beside the block data compress to 39 KiB
against 7.7 MiB of blocks.

Two deliberate asymmetries are worth knowing before reaching for either shape:
workingsets and column widths still live on `boxer.facts` as hand-written
`argMax` queries and are decided to move to `boxer.persiststate` (ADR-0105
Update 2026-08-15, not yet shipped); and `runtime.persist.*` remains wired but
is "no longer where new app state should be sent" — state that must survive
gets a modelled fact kind ([ADR-0026 Update 2026-07-30](./adr/0026-app-runtime-and-capability-subjects.md),
[ADR-0148](./adr/0148-app-workingsets.md)).

### 3.4 The query path

```text
 the buffer — play editor · sqlapplet document (first sql fence) · LW_* macros inside either
   │  canonicalize · lift SET param_* · alias → handle (ad-hoc) · pre-execute passes (StagePreExecute:
   │  LW_ID_* · LW_COMPONENT · LW_GET · LW_PLAIN/LW_TV · gloss() · fs()/fsdata()/fssnap() ·
   │  docsearch() · descriptiveStatistics() · column handles) · FORMAT ArrowStream
   ▼  the residual — what the engine will see; Preview shows the same bytes
 dispatch: one decision per run ──── Auto: keelsonResolver │ pinned: staticResolver
   │   confinement wall above every resolver: a statement naming a sealed handle is confined
   ├──▶ ClickHouse server (the pinned base)        chserver adapter over HTTP
   └──▶ the in-process plane 127.0.0.1:<port>/query   chhttp dialect → chlocal worker
   ▼         (keelson('x') → temp tables; ad-hoc → url('/table/<handle>') decrypted in-process)
 result frames: data · progress · exactly one terminal — no terminal means incomplete
   ▼
 lanes (main, Run-gated · observe · per-node bound · panel-authored) ──▶ panels = observers
   ▲                                                                        │
   └──────────── signals {name:Type}: selections, viewports, time extents ◀─┘
```

Authoring is one SQL text ([ADR-0097](./adr/0097-play-reactive-query-graph.md):
"one buffer is the artifact"); a sqlapplet is a markdown document whose
frontmatter is the manifest and whose first `sql` fence is that buffer
([ADR-0132](./adr/0132-sqlapplet-sql-defined-applets.md)). Rewrites that the
engine must see run through one registry,
[`public/keelson/data/passreg`](../public/keelson/data/passreg)
([ADR-0108](./adr/0108-keelson-sql-pass-registry.md)), applied by play's client
and by the in-process `/query` alike, and visible as `keelson('sql_passes')`.
The endpoint is a **decision, once per run**, with a class and a reason
([ADR-0141](./adr/0141-play-endpoint-dispatch-seam.md)): under the default
*Auto* preset a provably read-only statement naming only `keelson()` tables
routes to the in-process plane, anything else stays on the pinned base, a mix
is refused, and mutations are never auto-routed. Engines are adapters in three
roles — delivery, observation, control — of which only the server implements
the last two ([ADR-0144](./adr/0144-query-engine-adapters.md)); a result is a
sequence of frames with **exactly one terminal**, and a missing terminal is
read as incomplete rather than complete
([ADR-0142](./adr/0142-runstream-result-frame-contract.md)). Panels observe
nodes through typed channels; signals flow between them as ordinary query
parameters. The longer telling of this half is
[doc/explanation/play-architecture.md](./explanation/play-architecture.md).

What is decided but not built here: a streaming reply channel on the bus
([ADR-0143](./adr/0143-bus-streaming-reply-channel.md), accepted, no code) —
until it exists a chlocal result is drained whole into memory before it is
answered.

### 3.5 Ad-hoc datasets — ephemeral by cryptography

```text
 producer app ── adhoc.publish ──▶ adhocdata service ──▶ BOXER_ADHOC_DIR/<handle>
 (imzrt, play, a demo)             (runtime.adhoc)        AEAD-chunked Arrow IPC; a fresh key
                                       │                  per dataset, held only in RAM
                                       ├── catalog keelson('adhoc') · handle adhoc_<random>
                                       └── adhoc.event.published / .retracted (hints)
 applet doc:  datasets: [items]  ── alias → handle bound at mount (adhoc.resolve = truth)
 query:  SELECT … FROM keelson('items')
           ─▶ alias → handle ─▶ url('http://127.0.0.1:<p>/table/<handle>','ArrowStream','<structure>')
           ─▶ /table decrypts in-process by handle; plaintext rides loopback only ─▶ a chlocal worker
 retract:  leave (catalog, resolve) ─▶ notify ─▶ unload after one bus request timeout
```

[ADR-0134](./adr/0134-adhoc-datasets.md) (accepted 2026-07-20) answers how an
app hands a computed table to a SQL applet without creating durable state,
durable names, or plaintext at rest — and does so for the crash case too, which
cleanup-on-exit cannot: each dataset is an encrypted Arrow file whose key never
leaves process memory, so after a crash what remains is ciphertext without a
key. Buffers name a stable alias, never a handle; the binding is instance
state, applied like a tab binding, so the engine's classification still sees a
plain `keelson` read. The only engine that can see such a dataset is the
in-process plane; a confined run pinned anywhere else is refused above the
resolver with a reason, rather than failing at a server that does not know
`keelson()` ([ADR-0145](./adr/0145-sealed-app-data.md)). Withdrawal is
two-phase and bounded — leave, notify, unload after a grace of one bus request
timeout — because NATS core is at-most-once and "events are hints;
request/reply is truth" ([ADR-0188 §SD3](./adr/0188-app-instance-effect-tracking.md)).
Limits: 256 MiB per dataset, 1 GiB per store, 64 datasets; a bounded Arrow type
set at the publish gate. Data that should outlive the session is ingested
([ADR-0089](./adr/0089-rowdml-serialization-clickhouse-native-ingestion.md)) —
"an explicit act, not a default".

### 3.6 The filesystem in ClickHouse — the lading store

```text
 any fs.FS ── os.DirFS · embed.FS · zip · an rclone remote (§3.7) ── one fs.WalkDir per snapshot
   │ Snapshot(fsys, mount, policy)      mount = the caller's tagged id; never minted here
   ▼
 boxer.fsmeta   one row per node per snapshot     ┐ ORDER BY (id, ts, naturalKey) = (mount, snapshot, path)
 boxer.fsdata   one row per block (1 MiB, BLAKE3) ┼ PARTITION BY toYYYYMMDD(expiresAt) · TTL expiresAt
 boxer.fssnap   the snapshot index, via an MV     ┘ ttl_only_drop_parts = 1; retention in whole days
   │ the root row ('.') is written last, alone: a snapshot is complete exactly when it exists
   ▼
 reads: ladingadapter (io/fs) · SQL macros fs()/fsdata()/fssnap() · the SFTP head · apps/tally
```

[ADR-0198](./adr/0198-fs-snapshot-store.md) (accepted 2026-08-21) stores
`io/fs` trees as three facts-shaped tables. The backbone *is* the address:
`(mount, snapshot, path)`; a mount is the application's own tagged id
([ADR-0106](./adr/0106-identity-fibonacci-tags-build-tag-retirement.md)), so
one store serves many owners without a registry. Content policy is per mount —
stat only, blocks up to an inline threshold, or a reference with hash and size
— and text files are cut at the last newline before the block size with a
`line0` per block, so grep-shaped SQL is boundary-safe. Retention is
declarative and exact: every row of a partition expires at the same instant
because partitions are expiry *days*, so whole parts drop and the partition
count is the number of distinct expiry days, not of mounts — a sub-day
retention class is refused in code. There is no mutation, no garbage
collection and no shared block: purge is `DELETE … WHERE id = <mount>`; an
interrupted walk leaves unreachable rows that `TTL` removes. A snapshot at
`index_granularity = 1` makes one block one mark one compressed block, so a
point read of a 1 MiB block costs 1.00 MiB of compressed reads; the M0 trial's
other numbers are in §3.3. [`apps/tally`](../apps/tally)
([ADR-0200](./adr/0200-tally-lading-browser.md)) is the GUI over it — browse,
preview, diff, find, with time travel as `cd` between snapshots — and
[doc/howto/lading-snapshot-store.md](./howto/lading-snapshot-store.md) the
recipe.

### 3.7 rclone mounts — a pipe in either direction

```text
 egress — rclone is the client, boxer is an SFTP server on a pipe, read-only

   rclone mount --read-only ':sftp,ssh="boxer fs sftp-stdio --mount 0x…",shell_type=unix:/<mount>/latest' /mnt/x
      │ spawns the ssh= command instead of ssh; possession of the pipe is the authorization
      ▼
   boxer fs sftp-stdio ── pkg/sftp RequestServer ── ladingadapter ── ClickHouse
      tree: /<mount, 16 hex>/<snapshot, RFC3339-ish>/<path> · latest · every Filewrite/Filecmd = permission denied
   in front of it, rclone's own: serve s3 | webdav | nfs | http … · union (merged, not an overlay) · hasher · filters

 ingress — boxer is the client, rclone serves a remote on a pipe

   ladingremote.Serve(ctx, "s3:bucket/prefix") ── spawns: rclone serve sftp --stdio <remote> [--exclude …]
      │ wraps the pkg/sftp client as an fs.FS
      ▼
   ladingingest.Snapshot(fsys, mount, policy) ──▶ boxer.fsmeta / fsdata / fssnap
```

There is no socket, no port and no credential on either path: the pattern is
restic's `rclone serve restic --stdio`, analysed in
[doc/explanation/rclone-architecture-lessons.md](./explanation/rclone-architecture-lessons.md)
("a pipe is a transport; possession of the descriptor is the authorization").
That is also what keeps it inside the runtime's refusal to bind non-loopback
addresses before ADR-0082. On egress, visibility is `--mount <id>` or
`--all-mounts`, and an invisible mount is *absent*, not forbidden, so a client
cannot enumerate what it may not read; everything rclone knows how to serve —
S3, WebDAV, NFS, the web GUI, a writable `union` layer — sits in front of one
read-only head ([`public/fs/lading/ladingsftp`](../public/fs/lading/ladingsftp),
[`public/app/commands/ladingfs`](../public/app/commands/ladingfs)). On ingress,
[`public/fs/lading/ladingremote`](../public/fs/lading/ladingremote) runs `rclone serve sftp
--stdio` and the walker snapshots the result like any other `fs.FS`; rclone's
filter language runs on the serving side, so what is excluded never reaches
this process. Measured limits (2026-08-20, ADR-0198 M5/M6): no file modes cross
in either direction, mtimes are whole seconds, symlinks need `--links` on
ingress, and `latest` presents to rclone as a directory. `rclone` is declared
through extbin (`BOXER_RCLONE`); a native S3 head is M7, deferred — tier 1 is
`rclone serve s3` over the pipe.

### 3.8 The data paths, side by side

| | persisted facts (`boxer.facts`, record stores) | ad-hoc datasets | the lading store | chlocal scratch |
| --- | --- | --- | --- | --- |
| lives | the server, as rows | an encrypted file + a key in RAM | the server, as rows | a worker's memory, until it exits |
| outlives | the process, the operator's retention | the session — crash included, by construction | its retention class, in whole days | the request |
| named by | kind + membership ids; `keelson('workingsets')` & co. | an alias bound at mount, `adhoc_<random>` underneath | `(mount, snapshot, path)`; `/<mount>/<snapshot>/<path>` over SFTP | nothing |
| visible to | any engine on the server | the in-process plane only | any engine on the server; rclone | the one worker |
| costs | wide rows: insert/merge is the number to watch at fleet scale; `boxer.facts` has no TTL by default | 256 MiB / 1 GiB / 64 datasets; a bounded type set; pasteable-modulo-grant | undeduplicated snapshots; a full walk per snapshot; ms-latency adapter calls | a 64 MiB buffered result ceiling; cold spawn past the warm pool |
| record | ADR-0026 §SD6, 0100, 0105, 0184 | ADR-0134, 0145, 0188 | ADR-0198, 0200 | ADR-0028 |

## 4. Modes × data — what each box can reach

The data architecture is the Go process's, so it travels with the host
unchanged; what differs per mode is what the box has next to it.

| | desktop / headless on a server OS | the appliance images (2026-08-22) |
| --- | --- | --- |
| ClickHouse server | whatever the deployment provides; the showcase box runs one beside the host | the ffmpeg pair: none, `facts:mem` / `persist:mem`; `boxer-soft-play`: `CLICKHOUSE_URL` names a server outside the image (under QEMU, the host) |
| `clickhouse local` | the `clickhouse` binary on PATH or at `BOXER_CLICKHOUSE_LOCAL` (which names the multi-call binary, not the `clickhouse-local` symlink); needed by `/query` and by `--launch "<SQL WHERE>"` | not in the ffmpeg pair (a bare `--launch <alias>` resolves without it); in `boxer-soft-play` on the A/B roots (ADR-0206 §SD6) |
| ad-hoc datasets | `BOXER_ADHOC_DIR`, default under the user cache dir | the images set `XDG_CACHE_HOME=/tmp/.cache`, a tmpfs with no swap (ADR-0134 §SD8 names `/perm` for a persistent placement) |
| the lading store, rclone | the server, `rclone` via extbin | no `rclone` in the images; the store's tables only where `CLICKHOUSE_URL` names a server that has them |
| the bus | in-process; NATS for the sysmetrics plane (`natsbus`, a bridge — the UI-coupled brokers stay on `inprocbus`) | in-process |

## 5. What it adds up to — the architecture's clauses in the positioning statement

The [positioning statement](./explanation/positioning-statement.md) is
Moore's template — *for* a target *who* has a need, *the product* is a
category *that* delivers a benefit; *unlike* the alternative, it differs in
one way — cut from the premises. Since 2026-08-23 it also carries what this
page evidences: the category clause names the processes (one Go host process,
a Rust client that only renders, helpers on pipes, ClickHouse as the one place
anything durable lives), and the benefit clause carries "that app runs
unchanged on a desktop, in a browser, or from a roughly 110 MB appliance
image" and "every durable fact … lands in one queryable table shape".

Where those clauses come from on this page: the processes — §1 (four process
kinds and the boundaries between them, "the Rust side renders and nothing
else", "two engines, no shared state"); the hosts — §1's first fact and
§2.1–2.6; the one shape — §3.2–3.3 and §3.6–3.7. Filled from this page
alone, the template would read differently, and the difference is the useful
check: the segment would shrink to an operational need (run apps on whatever
box you control) with no trace of sovereignty or ownership; the category
would narrow to an application runtime, dropping the toolkit and the
problem-oriented languages that are the compounding mechanism; and the foil
would be an inference, because this page contrasts only internal alternatives
— GPU, CPU or browser rasterization; chlocal or the server; the facts shape or
bespoke tables; the NixOS box or the appliance. Those three slots come from
[why-boxer](./explanation/why-boxer.md)'s premises, which is why the merged
statement keeps the premise frame and takes only the picturable clauses from
here.

## 6. Where this page stops

The records, by area, with their state on 2026-08-23 — cite them, not this
page:

| Area | Accepted | Proposed / open |
| --- | --- | --- |
| app runtime, bus, capabilities | [0026](./adr/0026-app-runtime-and-capability-subjects.md), [0035](./adr/0035-keelson-namespace-introduction.md), [0036](./adr/0036-runtime-buscodec.md), [0038](./adr/0038-keelson-background-task-primitive.md), [0135](./adr/0135-app-launch-requests.md), [0148](./adr/0148-app-workingsets.md), [0155](./adr/0155-app-embed-seam.md), [0158](./adr/0158-app-classification-topics-keywords-kind.md), [0188](./adr/0188-app-instance-effect-tracking.md) | [0143](./adr/0143-bus-streaming-reply-channel.md) (accepted, unbuilt), [0185](./adr/0185-durable-app-state-manager.md), [0191](./adr/0191-runtime-instance-attribution.md) |
| render hosts and remote access | [0024](./adr/0024-imzero2-remote-access-browser-viewer.md), [0062](./adr/0062-imzero2-render-cadence.md), [0082](./adr/0082-imzero2-remote-session-auth-tls.md) (unbuilt), [0085](./adr/0085-imzero2-demo-pull-build-atomic-deploy.md), [0086](./adr/0086-imzero2-active-passive-viewers-and-roster.md), [0087](./adr/0087-imzero2-client-compositor-compartmentalization.md) (a posture), [0088](./adr/0088-imzero2-runtime-codec-pipeline-and-viewer-capabilities.md), [0154](./adr/0154-headless-carrier-tree-and-driver.md), [0205](./adr/0205-imzero2-cpu-rasterized-pixel-host.md) | [0128](./adr/0128-imzero2-mesh-draw-stream-codec-lane.md), [0206](./adr/0206-gokrazy-appliance-image.md); [0077](./adr/0077-keelson-browser-wasm-execution.md) (accepted, unbuilt); [0081](./adr/0081-imzero2-headless-rdp-egfx-head.md) withdrawn |
| the Rust side's network I/O, static builds | [0095](./adr/0095-airgapped-build-bundle.md), [0118](./adr/0118-extbin-external-process-chokepoint.md), [0056](./adr/0056-walkers-map-h3-binding.md) (binding removed 2026-08-22; superseded by 0204 per its Update) | [0165](./adr/0165-imzero2-tile-transport-over-fffi2.md), [0203](./adr/0203-map-widget-without-the-http-stack.md), [0204](./adr/0204-leaflet-map-core-port.md) |
| engines and the query path | [0028](./adr/0028-chlocal-low-latency-sql-cap.md), [0094](./adr/0094-keelson-introspection-tables.md), [0097](./adr/0097-play-reactive-query-graph.md), [0108](./adr/0108-keelson-sql-pass-registry.md), [0115](./adr/0115-query-observability-data-plane-strategy.md), [0132](./adr/0132-sqlapplet-sql-defined-applets.md), [0133](./adr/0133-chhttp-server-dialect-and-param-binding.md), [0141](./adr/0141-play-endpoint-dispatch-seam.md), [0142](./adr/0142-runstream-result-frame-contract.md), [0144](./adr/0144-query-engine-adapters.md) | — |
| persistence | [0089](./adr/0089-rowdml-serialization-clickhouse-native-ingestion.md), [0100](./adr/0100-recordstore-generated-leeway-clickhouse-store.md), [0105](./adr/0105-keelson-adopts-generated-record-stores.md), [0134](./adr/0134-adhoc-datasets.md), [0145](./adr/0145-sealed-app-data.md), [0170](./adr/0170-data-catalog-competence.md), [0184](./adr/0184-sysmetrics-persistence-tee.md), [0198](./adr/0198-fs-snapshot-store.md), [0200](./adr/0200-tally-lading-browser.md) | — |

Companion explanations: [why-boxer](./explanation/why-boxer.md) for the
premises these shapes enact, [play-architecture](./explanation/play-architecture.md)
for the query graph, [facts-bound record stores](./explanation/facts-bound-record-stores.md)
for what a new kind must satisfy, [query-observability](./explanation/query-observability.md)
for the post-run plane, and the gokrazy [showcase README](../showcase/gokrazy/README.md)
for booting an image.
