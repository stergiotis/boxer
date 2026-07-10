---
type: reference
audience: imzero2 developer / operator
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not yet verified against the current documentation standard. Do not cite as authoritative.

# imzero2 (Rust renderer)

The Rust half of ImZero2: an egui-based renderer driven by the Go host
over FFFI2 (widget-level commands on stdin/stdout). It builds as two
hosts from one source tree:

- **Desktop** (default; Cargo feature `desktop`): eframe + winit window,
  launched by the Go side. Entry: `./hmi.sh`.
- **Headless remote access** (Cargo feature `headless`,
  [ADR-0024](../../doc/adr/0024-imzero2-remote-access-browser-viewer.md)):
  no window system — renders offscreen, encodes H.264 via an ffmpeg
  subprocess, and serves a browser viewer over one WebSocket. Entry:
  `./hmi_headless.sh`. Architecture notes: [EXPLANATION.md](./EXPLANATION.md).

## Maturity

The desktop host is the daily-driven path. The headless host is a first
cut of ADR-0024 v1 (2026-06-12), verified end-to-end against the demo
carousel. The viewer's viewport and pixel scale (devicePixelRatio ×
zoom) are applied live (the canvas follows the browser window and HiDPI
clients get native-resolution pixels), render cadence is reactive or
continuous and switchable at runtime, and the Rust wire types are
generated from the .proto. Named gaps: **no authentication** (network
reachability is the access control; keep it on localhost or a trusted
network), single session, and the browser viewer's protobuf codec is
hand-written (by choice — see the wire-types note below). The full
shipped/deviation list is in ADR-0024's 2026-06-12 Updates entry.

## Remote access — quick start

```sh
./hmi_headless.sh                       # builds Rust (headless) + Go, serves the widgets demo
# open http://127.0.0.1:8089/ in a WebCodecs-capable browser
# (Chromium-family, Safari 16.4+, Firefox 130+)
./hmi_headless.sh --launch play         # any demo selector (see `main_go imzero2 demo --list`)
```

Both listeners (8089 and 8090) serve the viewer page *and* accept the
WebSocket upgrade — requests are dispatched by sniffing the upgrade
header, and the page connects back to its own origin. One port is
therefore enough to forward or proxy.

Touch clients (iPad etc.): the viewer translates gestures — one finger
acts as the pointer (tap = click, drag = pointer drag, which scrolls in
egui scroll areas via drag-to-scroll), two fingers scroll
trackpad-style and pinch to zoom. Three and more fingers are ignored;
there is no long-press right-click yet.

### Viewing from another host

The recommended path is an SSH tunnel from the viewing machine — it
needs no firewall changes, keeps the localhost bind (no unauthenticated
exposure), and `http://127.0.0.1` is a secure context:

```sh
ssh -N -L 8089:127.0.0.1:8089 user@server
# then open http://127.0.0.1:8089/ on the viewing machine
```

Direct LAN binding works (`IMZERO2_HEADLESS_LISTEN=0.0.0.0:8089`, open
the port in the firewall) but runs into two things: there is
**no authentication** — anyone who can reach the port gets full
keyboard/mouse control — and browsers expose **WebCodecs only in secure
contexts**: `http://127.0.0.1` qualifies, a plain-HTTP LAN origin does
not, so the viewer will report "WebCodecs is not available". On desktop
browsers there are test overrides (Chromium
`--unsafely-treat-insecure-origin-as-secure=http://<lan-ip>:8089`,
Firefox `dom.securecontext.allowlist`); **iOS/iPadOS Safari has no such
override** — plain-HTTP LAN viewing cannot work there. iPad-capable
paths, by what you have available:

- an SSH app with port forwarding (Blink Shell, Termius): forward
  `8089 → 127.0.0.1:8089`, browse `http://127.0.0.1:8089/`;
- Tailscale on both machines: `tailscale serve --bg 8089` gives
  `https://<machine>.<tailnet>.ts.net/` with a real certificate — the
  same-origin page makes the single mapping carry the WebSocket too;
- a TLS proxy with a CA the device trusts (e.g. caddy + mkcert, root CA
  installed as an iOS profile): `reverse_proxy 127.0.0.1:8089` is the
  whole config; `wss` follows the page scheme automatically.

The real fix — built-in TLS + auth — is the named ADR-0024 follow-up.

The encoder defaults to VAAPI (`h264_vaapi -bf 0 -qp:v 26`, ADR-0024
SD3). Stock Fedora mesa ships VAAPI H.264 *encode* disabled; on such
boxes use the software fallback:

```sh
IMZERO2_HEADLESS_ENCODER_ARGS="-c:v libopenh264 -rc_mode off -bf 0 -g 100000" ./hmi_headless.sh
```

The `-g 100000` (effectively no periodic key frames) is deliberate, in
the VAAPI default too: every connection starts its own encoder at a key
frame, the viewer reconnects on decode errors, and periodic IDR refresh
re-quantizes the whole screen — which shows up as a color pulse on
static content every GOP (measured: RMSE 316 at each IDR vs 0 between
P-frames before the change; 0 throughout after).

### Configuration (environment variables)

The Go launcher passes its environment through to the Rust client, so
these need no flag plumbing (`IMZERO2_RENDER_CADENCE` precedent). They
are also catalogued in the boxer-wide env registry (ADR-0058) and
surface in [`doc/env-vars.md`](../../doc/env-vars.md):

| Variable | Default | Effect |
|---|---|---|
| `IMZERO2_HEADLESS_LISTEN` | unset | Carrier bind address (e.g. `127.0.0.1:8089`); this port and port+1 each serve both the viewer page and the WebSocket. Unset = no remote access. `hmi_headless.sh` sets `127.0.0.1:8089`. |
| `IMZERO2_HEADLESS_FPS` | `60` | Render tick in Hz (`hmi_headless.sh` sets 30). Paces the FFFI2 loop in place of vsync. |
| `IMZERO2_HEADLESS_ENCODER_ARGS` | VAAPI per SD3 | Whitespace-split ffmpeg args between rawvideo input and `-f h264` output. |
| `IMZERO2_HEADLESS_PIXELS_PER_POINT` | `1.0` | Initial (pre-connect) HiDPI scale; a connected viewer's reported viewport + scale take over. |
| `IMZERO2_HEADLESS_H264_OUT` | unset | Also write the raw Annex-B stream to this file (verification). |
| `IMZERO2_HEADLESS_DUMP_DIR` / `_DUMP_EVERY` | unset / `60` | PNG-dump every Nth frame (verification). |
| `IMZERO2_HEADLESS_MAX_FRAMES` | `0` | Stop after N frames (0 = unbounded; smoke tests). |
| `IMZERO2_HEADLESS` | unset | Only with a dual-feature build: `1`/`on` selects the headless host at runtime. |

When both binaries exist, the Go launcher selects via
`--clientBinary` (`target/release/imzero2` vs
`target/headless/release/imzero2`).

### Render cadence

`IMZERO2_RENDER_CADENCE=reactive` (shared with the desktop host and the
Go-side decorator) starts the host in reactive mode: a pass runs when
egui schedules a repaint, when wire input arrives, or at a 1 s idle
heartbeat — capped at the configured fps. Measured on the idle widgets
demo: 16 passes per 12 s reactive vs 407 continuous. The cadence can be
switched at runtime from the viewer (status-bar toggle, or
`?cadence=continuous|reactive` once at load) — host scope, survives
reconnects. Caveat: the Go decorator reads the variable at startup, so
a server *launched* continuous keeps requesting per-frame repaints and
a runtime switch to reactive only stops encoding, not rendering; launch
reactive for the full effect.

Independent of cadence, frames whose pixels are unchanged are never
encoded (blake3 dedup), and passes with no pixel consumer (no viewer,
no dump sink) skip rendering and readback entirely.

The encoder runs on its own thread fed by a depth-1 latest-wins mailbox
(ADR-0024 SD9), so the render/FFFI2 loop never blocks on the encoder or
a slow viewer: under wire congestion the feeder thread blocks on
ffmpeg's stdin while the render loop keeps producing and the mailbox
coalesces to the freshest frame, dropping stale frames *before* the
encoder. Encoded frames are never dropped (every frame is a reference
with `-bf 0`).

### Probing without a browser

`imzero2_ws_probe` (built with the headless feature) connects like a
viewer, records the H.264 stream to a file, and can inject a click:

```sh
target/headless/release/imzero2_ws_probe ws://127.0.0.1:8089/ /tmp/probe.h264 150 96 202 30
ffprobe -f h264 /tmp/probe.h264
```

It can also inject a `resize <lw> <lh> <scale> <after_au>` and a
`cadence <0|1> <after_au>` (verifying the resize and SetCadence wire
paths end to end), in any combination after the click triple.

### Wire types

`proto/boxer/imzero2/v1/input.proto` is the canonical contract. The Rust
types are generated from it at build time (`build.rs`: protox +
prost-build, pure Rust — no system `protoc`; active only under the
`headless` feature), so the server side cannot drift from the schema.
The browser viewer hand-encodes/decodes the messages it uses with a
small inline codec, kept deliberately so the viewer stays a single
self-contained HTML file with no bundling step; that codec must be
updated by hand when the schema changes (a protobuf-es step is the
escape hatch if it ever outgrows hand-maintenance).

## Driving imzero2 from an agent (egui_mcp)

The desktop host can be driven by an MCP agent — reading the widget tree and
injecting clicks/keys/screenshots — through egui's upstream `egui_inspection`
protocol. The `inspection` feature is part of the desktop default build, so you
just set `EGUI_INSPECTION` (e.g. `EGUI_INSPECTION=1 ./hmi.sh`); eframe then binds
a loopback inspection port that the separately-installed
[egui-mcp](https://github.com/rerun-io/kittest_inspector) server connects to. The
port is unauthenticated remote control, so it stays closed until `EGUI_INSPECTION`
opens it (loopback only), and the headless remote-access build excludes the
feature entirely. Full steps and the security note:
[doc/howto/egui-mcp.md](../../doc/howto/egui-mcp.md).

## Layout

| Path | Contents |
|---|---|
| `src/imzero2/interpreter.rs` | FFFI2 interpreter (shared by both hosts) |
| `src/imzero2/app.rs`, `entry.rs` | Desktop host (eframe) |
| `src/imzero2/apphost.rs` | Host-independent init (fonts, style, single-pass pin) |
| `src/imzero2/headless.rs` | Headless host loop (offscreen render + readback) |
| `src/imzero2/encoderpipe.rs` | ffmpeg subprocess + access-unit framing |
| `src/imzero2/wscarrier.rs` | WebSocket carrier + embedded viewer serving |
| `src/imzero2/viewer/index.html` | Browser viewer (WebCodecs + input capture) |
| `src/imzero2/inputproto.rs`, `inputmap.rs` | Generated wire types (`include!` of the build.rs codegen) + egui translation |
| `build.rs` | protox + prost-build codegen of the wire types (headless feature only) |
| `../../proto/boxer/imzero2/v1/input.proto` | Canonical wire contract (Rust generated from it; viewer hand-codes it) |

## Related decisions

- [ADR-0024](../../doc/adr/0024-imzero2-remote-access-browser-viewer.md) — remote access via pixel streaming (accepted; v1 record in Updates).
- [ADR-0081](../../doc/adr/0081-imzero2-headless-rdp-egfx-head.md) — RDP head on the same foundation (withdrawn 2026-06-12; kept as reference analysis).
- [ADR-0013](../../doc/adr/0013-imzero2-stateful-widget-contract.md) — stateful widget contract.
- [ADR-0062](../../doc/adr/0062-imzero2-render-cadence.md) — render cadence (reactive mode is named follow-up for the headless host).
