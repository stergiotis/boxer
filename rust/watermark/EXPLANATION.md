---
type: explanation
audience: watermark crate maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Implementation Plan: Tiled Luminance-Grid Watermark (Rust)

A task brief for an autonomous coding agent. Build a Rust library + CLI that
embeds **64 bits** into a frame as a **tiled luminance grid**, such that **every
464×432 px crop of the frame contains all 64 bits** and the payload survives
H.264 / VP9 / AV1 compression and a screenshot.

The plan is a **test-driven staircase**: each stage ends with a concrete
acceptance test you can run. Do not advance to the next stage until the current
stage's test passes. The codec — the riskiest dependency — does not appear until
Stage 8; everything before it is proven in-memory so failures are unambiguous.

---

## 0. Design constants (do not rederive these; they are the spec)

- **Payload:** 64 bits = 8 bytes.
- **Checksum:** **CRC-16/CCITT** over the 64-bit payload → **80 info bits**. The
  final clean/not-clean gate; 1/65536 missed-detection floor.
- **FEC:** **extended binary Golay [24,12,8]**, rate ½, **7 codewords** (80 info
  bits → ceil(80/12)=7 words → 7×24 = **168 coded bits = 168 data cells**),
  interleaved. Each word corrects ≤ 3 bit errors / detects ≤ 4.
- **Cell size:** **16×16 px** (conservative; survives moderate-bitrate codecs),
  sample only the **inner 8×8** (deblocking eats edges).
- **Grid:** **14 cols × 13 rows = 182 cells** = 168 data + **14 reference cells**.
- **Tile size:** `14·16 + 8 guard = 232` wide, `13·16 + 8 guard = 216` tall →
  **tile = 232×216 px**.
- **Guaranteed window:** `2 × tile = `**`464 × 432 px`** *(the requirement)*.
  - *Why:* an arbitrarily-placed window of extent `W` fully contains a tile of
    period `w` iff `W ≥ 2w`. Here `W = 2w` **exactly**.
  - **⚠ Design consequence:** at `W = 2w` the worst-case offset contains exactly
    **one** complete tile (favorable offsets / larger crops give 2–4). The
    guarantee therefore rests on **single-tile decode**: a *single* post-codec
    tile must decode on Golay FEC alone. Multi-tile soft-combining is an
    opportunistic bonus, **not** a guaranteed part of the budget. This is why
    cells are sized conservatively at 16×16.
- **Signal:** **luma only.** Never chroma. (Codecs subsample + quantize chroma hardest.)
- **Modulation:** add/subtract a luma delta **Δ** to each cell's mean. Default
  **Δ = 8** (0–255 scale); expose as a tunable. Apply Δ in the **same gamma domain
  you measure in** — pick sRGB-gamma space and be consistent end to end.
- **Reference cells:** the 14 spare cells per tile at known levels
  (black / mid / white) for per-crop brightness+gamma self-calibration.

**Cell budget (must hold):** 64 payload + 16 CRC = 80 info → 7 Golay words = 168
data cells; grid 14×13 = 182 cells → 168 data + 14 reference. 168 ≤ 182 ✔.
*Golay is fixed rate ½ — you cannot retune it; if you change the checksum width or
codeword count, resize the grid and re-derive the tile/window above.*

---

## 1. Crate selection (pin these; don't go shopping mid-build)

| Concern | Crate | Notes |
|---|---|---|
| Image I/O (PNG) | `image` | Encode/decode test frames. |
| Numerics | `ndarray` *(or* `Vec<f32>` *)* | Luma plane + cell math. |
| **FEC** | **none — implement Golay24 in-crate (~100 lines)** | Syndrome → coset-leader table lookup (4096-entry) or the standard arithmetic decoder. No GF(256), no Berlekamp–Massey, **no FEC dependency to pick wrong.** This dependency-freeness is the reason for choosing Golay over Reed–Solomon. |
| CRC | `crc` | CRC-16/CCITT (poly 0x1021). |
| FFT (tile locator) | `rustfft` | Detect tile period + phase from the luma spectrum. |
| Homography (optional, Stage 10) | `nalgebra` | Perspective rectify if screenshots can be skewed. |
| CLI | `clap` (derive) | `encode` / `decode` / `roundtrip` subcommands. |
| Codec round-trip | **external `ffmpeg` via `std::process::Command`** | Do **not** bind libav. Shell out, use temp files. |
| Test RNG | `rand` + fixed seeds | Determinism is mandatory for the test suite. |

Confirm `ffmpeg` is on PATH at Stage 8; if absent, tell the user to install it and
skip codec tests rather than failing the build.

---

## 2. Module layout

```
src/
  lib.rs        // public API: encode_frame(), decode_crop()
  payload.rs    // 64-bit payload <-> bytes, CRC-16 append/check
  fec.rs        // Golay24 encode/decode (in-crate) + interleave/de-interleave
  layout.rs     // TileSpec geometry, cell coords, the W>=2w constraint check, reference-cell placement
  render.rs     // coded bits -> 14x13 luma cells -> tile across frame
  calibrate.rs  // read reference cells, fit brightness/gamma normalization
  locate.rs     // FFT period+phase detection; enumerate complete tiles in a crop
  sample.rs     // per-cell soft value (inner 8x8 mean minus local DC); combine across any complete tiles present
  decode.rs     // locate -> sample -> combine -> calibrate -> threshold -> de-interleave -> Golay-decode -> CRC
  cli.rs / main.rs
tests/
  s1_payload.rs  s2_fec.rs  s3_layout.rs  s5_roundtrip_clean.rs
  s6_noise.rs    s7_locate.rs  s8_codec.rs  s9_crop.rs
```

Core types:

```rust
pub struct Payload(pub [u8; 8]);                 // 64 bits

pub struct TileSpec {
    pub tile_w: u32, pub tile_h: u32,            // 232, 216
    pub cell_px: u32, pub inner_px: u32,         // 16, 8
    pub cols: u32, pub rows: u32,                // 14, 13
    pub delta: f32,                              // luma Δ, default 8.0
    pub reference: Vec<(u32, u32, RefLevel)>,    // (col,row,level), 14 of them
}
pub enum RefLevel { Black, Mid, White }

pub struct LumaFrame { pub w: u32, pub h: u32, pub y: Vec<f32> } // sRGB-gamma luma 0..255

pub struct Locate { pub period_x: f32, pub period_y: f32, pub phase_x: f32, pub phase_y: f32 }
```

---

## 3. Stages

### Stage 1 — Payload + CRC
- **Goal:** `Payload <-> [u8;8]`; append/verify CRC-16/CCITT → 80-bit info word ↔ bits.
- **Accept:** property test: random payload → encode → corrupt one bit → CRC detects it;
  uncorrupted → CRC passes. Round-trip is bit-exact.

### Stage 2 — FEC layer (Golay24)
- **Goal:** in-crate Golay [24,12,8] encode/decode; pack the 80-bit info word into
  7 codewords (4 pad bits in the last); interleave the 168 coded bits; de-interleave + decode.
- **Accept:** for each codeword, inject **≤ 3 random bit errors** → decode recovers exactly;
  inject 4 → decode *detects* failure or CRC catches it downstream (**never** a silent wrong
  answer). Run 1000 seeded trials across all 7 words. Verify the interleaver spreads each
  codeword's 24 bits across the grid (no two bits of one word in adjacent cells).

### Stage 3 — Tile layout / geometry
- **Goal:** `TileSpec` → cell pixel rectangles, 14 reference-cell placements, and a
  pure function `guaranteed_containment(window_w, window_h, spec) -> bool` implementing `W ≥ 2w`.
- **Accept:** unit tests: tile 232×216 with window 464×432 → `true`; 465×433 → `true`;
  463×431 → `false`. Cell rectangles tile the 224×208 active area with no overlap and an
  8 px guard border. Add a test asserting the worst-case offset yields exactly **one**
  complete tile in a 464×432 window (the single-tile guarantee).

### Stage 4 — Renderer
- **Goal:** map 168 coded bits → data cells (bit 1 → +Δ, bit 0 → −Δ on inner 8×8;
  14 reference cells forced to their levels) → tile across an arbitrary base `LumaFrame`
  → write PNG.
- **Accept:** visual check (emit a PNG to `target/`), plus: the rendered frame is exactly
  periodic with period 232×216; reference cells read back at their nominal levels.

### Stage 5 — In-memory decoder (no codec, no crop)
- **Goal:** full decode chain on the *same* clean array: sample inner 8×8 means, subtract
  local DC, threshold, de-interleave, Golay-decode, CRC.
- **Accept:** **encode → decode is bit-exact** for 1000 random payloads on a flat mid-gray
  frame *and* on a natural-image base frame. Golden test; if it fails nothing downstream works.

### Stage 6 — Perturbation robustness (still no codec)
- **Goal:** prove calibration + soft thresholding before the real codec.
- **Steps:** add Gaussian luma noise (σ sweep), apply global brightness offset and a gamma
  shift, then decode using only the reference cells to recalibrate.
- **Accept:** BER-vs-σ curve recorded; payload fully recovers (post-Golay, CRC-clean) up to
  a documented σ **from a single tile** (don't average — prove the worst case). Brightness/gamma
  shifts within a stated range do not break decode.

### Stage 7 — FFT tile locator
- **Goal:** given a frame translated by an arbitrary offset, recover `period` and `phase`
  per axis from the luma spectrum (window input, remove DC, find peak nearest the known
  period), then enumerate pixel origins of all **complete** tiles in the region.
- **Accept:** for 200 random integer offsets, recovered phase matches truth within ±1 px and
  the set of complete-tile origins is correct — including the offsets where that set has size 1.

### Stage 8 — Codec round-trip harness ⚠️ first real test
- **Goal:** `roundtrip` subcommand: render → write PNG → shell to `ffmpeg` encoding to
  **h264 / vp9 / av1** at a given CRF/bitrate (`yuv420p`, resolution preserved) → decode the
  first output frame → report **BER before Golay** and **payload OK after**.
- **Notes:** one ffmpeg invocation per codec; temp dir; clean up. Verify output resolution ==
  input (a silent rescale invalidates cell sizes). Parameterize by codec × quality to sweep.
- **Accept:** at a chosen per-codec quality, payload recovers CRC-clean for all three codecs
  across 50 random payloads, decoding from a **single full-frame tile**. Record the quality
  threshold where each codec starts failing.

### Stage 9 — The crop constraint (the actual requirement) ⚠️
- **Goal:** after codec, crop to **464×432 at a random offset**, then decode *only that crop*.
  Exercises Stage 7's locator on degraded data and the single-tile worst case.
- **Accept:** **property test over ≥500 random crop offsets** (and all 3 codecs): every
  464×432 crop yields all 64 bits, CRC-clean — **including worst-case offsets that expose only
  one complete tile.** Definition-of-done test for the core requirement.

### Stage 10 — Perspective robustness (optional; only if screenshots can be skewed)
- **Goal:** apply a mild synthetic homography, add a light per-tile corner cue, detect it,
  rectify with `nalgebra`, then decode.
- **Accept:** decode survives rotations/skews up to a documented bound. Skip if screenshots
  are guaranteed axis-aligned and unscaled.

### Stage 11 — Tuning + CLI polish
- **Goal:** a `sweep` subcommand varying Δ, reporting (visibility vs single-tile post-Golay
  margin) across codecs/qualities. Document defaults. Finalize `encode`/`decode` + `--help`.
- **Accept:** `cargo test` green; `cargo clippy` clean; README documents chosen Δ and the
  codec-quality limits from Stages 8–9.

---

## 4. Test matrix (fill in as you go)

| Codec | Quality | Tiles in crop | Pre-Golay BER | Payload OK |
|---|---|---|---|---|
| h264 | CRF 23 | 1 (worst case) | … | … |
| vp9  | CRF 31 | 1 (worst case) | … | … |
| av1  | CRF 30 | 1 (worst case) | … | … |

The worst-case (single-tile) row is the one that must pass; multi-tile rows should be strictly easier.

---

## 5. Guardrails — failures the agent *will* hit if not warned

1. **Luma only.** Any signal in chroma dies in 4:2:0. Modulate Y, leave Cb/Cr untouched.
2. **Gamma consistency.** Apply Δ and measure in the same domain (sRGB-gamma). Mixing linear
   and gamma makes Δ vary across brightness and breaks reference-cell calibration.
3. **Sample the inner 8×8, never cell edges.** Deblocking/loop filters smear block boundaries.
4. **Soft-combine first, Golay-decode last.** Average soft per-cell luma across **whatever
   complete tiles the crop contains (1 in the worst case)**, threshold to bits *after*
   combining, then hard-decode Golay. The design must survive the **single-tile** case on
   Golay's 3-errors-per-word alone — extra tiles are a bonus, never a crutch.
5. **Interleave the 7 codewords.** Golay corrects only 3 errors/word and codec damage is
   bursty; scatter each word's 24 bits so a burst nicks a few bits from many words, not all
   24 of one.
6. **Size cells against the *received* resolution.** If the stream is downscaled before
   screenshot, a 16×16 source cell shrinks; detect/scale or the locator's period is wrong.
7. **Verify ffmpeg preserves resolution** (`yuv420p`, no scale filter). A silent rescale
   invalidates every geometry assumption.
8. **Determinism.** Every test seeds its RNG.
9. **Never report success on a CRC failure.** Golay can *mis-correct* (>3 errors → a valid
   but wrong codeword); CRC-16 is the only thing that catches that. A confident wrong decode
   is worse than a detected failure.

---

## 6. Definition of done

- Stage 9 passes: **any 464×432 crop, any of the 3 codecs at documented quality, recovers
  all 64 bits CRC-clean** across ≥500 random offsets, **including single-tile worst-case offsets.**
- Δ documented with a measured visibility/robustness trade-off.
- `cargo test` and `cargo clippy` clean; README states codec-quality limits.

---

## 7. Suggested build order (one sentence)

Scaffold → Stages 1–3 (pure logic, instant tests) → Stages 4–6 (render + in-memory decode +
perturbations, no external deps) → Stage 7 (locator) → **only now** Stage 8 (ffmpeg) →
Stage 9 (the requirement) → optional 10 → polish 11.
