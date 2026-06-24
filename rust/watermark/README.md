---
type: reference
audience: watermark crate user / developer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not yet verified against the current documentation standard. Do not cite as authoritative.

# watermark

A Rust library + CLI that embeds a **64-bit payload** into a frame as a
periodically tiled grid of luma deltas, so that **every 464×432 px crop** of the
frame recovers the payload after H.264 / VP9 / AV1 compression and a screenshot.

The full design — geometry, the Golay FEC budget, and the single-tile decode
guarantee that the crop size rests on — is in [`EXPLANATION.md`](./EXPLANATION.md). This
README covers how to build, run, and what the measured limits are.

## Status

The test-driven staircase in `EXPLANATION.md` is implemented through Stage 9 (the
crop requirement) plus CLI/polish. Stage 10 (perspective/homography for skewed
screenshots) is deferred — decode assumes axis-aligned, unscaled crops. The
numbers below were measured on synthetic content with single-tile decode; treat
them as indicative, not a guarantee across all imagery.

## How it works (one paragraph)

`Payload(8 bytes)` → CRC-16 → 80-bit info word → extended Golay [24,12,8] FEC
(7 codewords, in-crate) → a 2D interleaver scatters the 168 coded bits across a
14×13 cell grid → each data cell adds `±Δ` to the luma of its inner 8×8 block,
leaving a 4-px ring as the local-DC reference → the tile (232×216, incl. an 8-px
guard) repeats across the frame. Decode reverses it: locate the tile grid
(reference-cell covariance search), calibrate brightness/gamma from the
reference cells, read `inner − ring` per cell, soft-combine across whatever
complete tiles the crop holds (one in the worst case), threshold, Golay-decode,
and verify the CRC. A failed CRC returns an error — never a guessed payload.

The Golay FEC is a direct in-crate port of this repo's IRIG-106 Appendix Q
implementation (`doc/golay24/*.c`, `public/fec/code/golay24`); the encode table
is checked byte-for-byte against the Go `Encoding` table in `tests/s2_fec.rs`.

## Build & test

The crate pins Rust **1.92** via `rust-toolchain.toml` (matching `rust/imzero2`).

```sh
cd rust/watermark
cargo test          # Stages 1–7 need no external deps
cargo clippy --all-targets -- -D warnings
```

Stages 8–9 shell out to **ffmpeg** (libx264 / libvpx-vp9 / libsvtav1). If
`ffmpeg` is not on `PATH`, those tests print a skip notice and pass. The
`scripts/ci/watermark_test.sh` helper runs clippy + the full suite, skipping
gracefully when cargo is absent.

The opt-in quality sweep (`cargo test --release -- --ignored
codec_quality_sweep`) prints the BER-vs-CRF table the limits below come from.

## CLI

```sh
# Embed (uses a synthetic base if --input is omitted)
watermark encode --input base.png --output wm.png --payload deadbeef12345678
watermark encode --output wm.png --size 1280x720          # random payload

# Recover (works on the full frame or any 464×432+ crop of it)
watermark decode --input wm.png                            # prints the hex payload

# Codec round-trip report (pre-Golay BER + recovery, per codec)
watermark roundtrip --input base.png --codec all

# Visibility/robustness trade-off across Δ
watermark sweep --input base.png
```

## Δ — the visibility knob

`Δ` is the luma swing applied to each data cell (0–255 scale). The default is
**Δ = 8**: subtle on natural content (the data grid is barely visible; the
reference cells are the more noticeable feature) yet leaving large margin after
compression. At the default CRFs below, single-tile decode shows **0 pre-Golay
bit errors** at Δ = 8 — i.e. Golay's error budget is untouched, so there is room
to lower Δ for less visibility if a use case needs it. Lower Δ trades margin for
invisibility; `watermark sweep` quantifies it for your content.

## Measured codec-quality limits

Single-tile decode, synthetic-natural base, 12 payloads per point. "Clean" means
0 pre-Golay BER and full recovery.

| Codec | Default CRF | Clean through | First failure |
|------:|:-----------:|:-------------:|:--------------|
| h264 (libx264)    | 23 | CRF 33 | CRF 36 (BER 0.18, 0/12) |
| vp9 (libvpx-vp9)  | 31 | CRF 50 | none observed up to 50 |
| av1 (libsvtav1)   | 30 | CRF 42 | CRF 48 degraded (7/12), CRF 52 (0/12) |

The defaults sit well inside the clean range. Over ≥500 random crop offsets per
codec at the default CRF (`tests/s9_crop.rs`), every 464×432 crop recovered the
payload CRC-clean, with the single-tile worst case covering ~98% of offsets.

## Module map

| File | Stage | Role |
|---|---|---|
| `payload.rs`   | 1 | payload ↔ bytes, CRC-16/CCITT |
| `fec.rs`       | 2 | Golay [24,12,8] encode/decode (ported in-crate) |
| `layout.rs`    | 3 | tile geometry, the 2D interleaver, reference-cell placement |
| `frame.rs`     | 4 | the luma plane + PNG I/O + synthetic bases |
| `render.rs`    | 4 | coded bits → tiled luma cells |
| `sample.rs`    | 5 | per-cell inner/ring means |
| `decode.rs`    | 5 | sample → soft-combine → Golay-decode → CRC |
| `calibrate.rs` | 6 | brightness/gamma transfer from reference cells |
| `locate.rs`    | 7 | tile-grid phase recovery |
| `codec.rs`     | 8 | ffmpeg round-trip harness |
| `cli.rs` / `main.rs` | 11 | `encode` / `decode` / `roundtrip` / `sweep` |
