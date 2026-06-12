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
cut of ADR-0024 v1 (2026-06-12): verified end-to-end against the demo
carousel, with named gaps — **no authentication** (network reachability
is the access control; keep it on localhost or a trusted network),
single session, render and encode share one cadence, and the protobuf
codecs on both ends are hand-written mirrors of the .proto pending
codegen. The viewer's viewport and pixel scale (devicePixelRatio × zoom)
are applied live: the canvas follows the browser window and HiDPI
clients get native-resolution pixels. The full shipped/deviation list is
in ADR-0024's 2026-06-12 Updates entry.

## Remote access — quick start

```sh
./hmi_headless.sh                       # builds Rust (headless) + Go, serves the widgets demo
# open http://127.0.0.1:8090/ in a WebCodecs-capable browser
# (Chromium-family, Safari 16.4+, Firefox 130+)
./hmi_headless.sh --launch play         # any demo selector (see `main_go imzero2 demo --list`)
```

### Viewing from another host

The recommended path is an SSH tunnel from the viewing machine — it
needs no firewall changes, keeps the localhost bind (no unauthenticated
exposure), and `http://127.0.0.1` is a secure context:

```sh
ssh -N -L 8089:127.0.0.1:8089 -L 8090:127.0.0.1:8090 user@server
# then open http://127.0.0.1:8090/ on the viewing machine
```

Direct LAN binding works (`IMZERO2_HEADLESS_LISTEN=0.0.0.0:8089`, open
ports 8089–8090 in the firewall) but runs into two things: there is
**no authentication** — anyone who can reach the port gets full
keyboard/mouse control — and browsers expose **WebCodecs only in secure
contexts**: `http://127.0.0.1` qualifies, a plain-HTTP LAN origin does
not, so the viewer will report "WebCodecs is not available". Workaround
for trusted-LAN testing: Chromium's
`--unsafely-treat-insecure-origin-as-secure=http://<lan-ip>:8090` flag
(Firefox: `dom.securecontext.allowlist` in `about:config`). The real
fix — TLS + auth — is the named ADR-0024 follow-up.

The encoder defaults to VAAPI (`h264_vaapi -bf 0 -qp:v 26`, ADR-0024
SD3). Stock Fedora mesa ships VAAPI H.264 *encode* disabled; on such
boxes use the software fallback:

```sh
IMZERO2_HEADLESS_ENCODER_ARGS="-c:v libopenh264 -b:v 4M -bf 0 -g 60" ./hmi_headless.sh
```

### Configuration (environment variables)

The Go launcher passes its environment through to the Rust client, so
these need no flag plumbing (`IMZERO2_RENDER_CADENCE` precedent):

| Variable | Default | Effect |
|---|---|---|
| `IMZERO2_HEADLESS_LISTEN` | unset | WebSocket bind address (e.g. `127.0.0.1:8089`); viewer page on port+1. Unset = no remote access. `hmi_headless.sh` sets `127.0.0.1:8089`. |
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

### Probing without a browser

`imzero2_ws_probe` (built with the headless feature) connects like a
viewer, records the H.264 stream to a file, and can inject a click:

```sh
target/headless/release/imzero2_ws_probe ws://127.0.0.1:8089/ /tmp/probe.h264 150 96 202 30
ffprobe -f h264 /tmp/probe.h264
```

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
| `src/imzero2/inputproto.rs`, `inputmap.rs` | Wire mirror of the proto + egui translation |
| `../../proto/boxer/imzero2/v1/input.proto` | Canonical wire contract |

## Related decisions

- [ADR-0024](../../doc/adr/0024-imzero2-remote-access-browser-viewer.md) — remote access via pixel streaming (accepted; v1 record in Updates).
- [ADR-0081](../../doc/adr/0081-imzero2-headless-rdp-egfx-head.md) — RDP head on the same foundation (withdrawn 2026-06-12; kept as reference analysis).
- [ADR-0013](../../doc/adr/0013-imzero2-stateful-widget-contract.md) — stateful widget contract.
- [ADR-0062](../../doc/adr/0062-imzero2-render-cadence.md) — render cadence (reactive mode is named follow-up for the headless host).
