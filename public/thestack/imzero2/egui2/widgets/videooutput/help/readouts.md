---
type: reference
audience: end-user
status: draft
title: Reading the video-output panel
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Reading the video-output panel

The video-output panel controls the codec pipeline that streams this
session's framebuffer to a remote browser (ADR-0088). It appears only while
a remote viewer is connected — under the local desktop host there is nothing
to stream and the panel stays hidden. The status bar shows the active codec;
click it to open the panel.

The panel has two parts: a one-line stream summary and a per-codec table.
Every figure is reported by the headless host each frame, so the numbers
track the live stream rather than a snapshot.

## Stream summary

The first line names the geometry and cadence.

- **`1920×1080`** — the framebuffer resolution being encoded, in pixels. It
  follows the viewer's window: a browser resize re-sizes the stream
  (ADR-0024 resize/DPR), so this changes when the viewer does.
- **`@ 60 fps`** — the frame rate the encoder is paced to. This is a
  ceiling, not a measured rate; an idle session sends fewer frames (see
  *cadence*).
- **`continuous` / `reactive`** — the render cadence. *Continuous* renders
  and sends every frame up to the fps ceiling. *Reactive* renders only when
  something changes (input, animation), so a still screen sends almost
  nothing. The host switches between the two at runtime to spend bandwidth
  only when the picture moves.

The second line is the live telemetry.

- **`x.x Mbps`** — the wire bitrate: a short exponential moving average of
  the bytes actually sent, refreshed about four times a second. It rises
  with motion and the codec's bitrate and falls when the screen is still.
- **`N sent`** — frames encoded and sent to the viewer since it connected.
- **`M coalesced`** — frames dropped *before* the encoder because a newer
  frame arrived first. The host keeps only the latest frame in a one-slot
  mailbox so a slow encoder or a slow viewer never stalls the render loop
  (ADR-0024 SD9); the skipped frames are counted here. A steadily climbing
  count means the encoder or the link cannot keep up with the render rate.
- **`K behind`** — how many sent frames the viewer has not yet reported
  decoding: roughly its backlog. It is a count of frames, not a time, and is
  refreshed from the viewer's periodic decode ping, so it is approximate. A
  small, stable number is healthy; a growing one means the viewer is falling
  behind.

## Codec table

One row per codec the host can encode here. The active codec's name is
highlighted; click another to switch (a brief glitch while the pipeline
re-opens is expected). Encode and decode acceleration are reported
**separately** — the host and the browser accelerate independently, and
either can be hardware while the other is software.

- **Codec** — H.264, VP9, or AV1.
- **Encoder** — the ffmpeg encoder the host would use: a `*_vaapi` entry for
  hardware (e.g. `h264_vaapi`), or the software library otherwise
  (`libopenh264`, `libvpx-vp9`, `libsvtav1`).
- **Encode** — the *host* backend for this codec: `hardware` when a VAAPI
  encoder probed working on this machine, else `software` (CPU). It is
  probed, not assumed — a driver that advertises an encoder it cannot
  actually run is caught and reported as software.
- **Decode** — the *browser* standing for this codec, as the connected
  viewer reports it: `hardware` (decoded power-efficiently), `software`
  (supported and expected to play smoothly on the CPU), `software?`
  (supported, smoothness unknown), or `unsupported` (the browser cannot
  decode it). A codec is selectable only when both sides can handle it.
- **WebCodecs** — the representative WebCodecs codec string the viewer
  configures its decoder with (e.g. `avc1.42E01E`). The active H.264
  stream's exact profile may differ; the viewer derives the precise string
  from the bitstream.
- **Pixels** — the pixel format the pipeline encodes: `4:2:0 8-bit` chroma
  subsampling, the broadly-decodable baseline these codecs share.
