---
type: reference
audience: watermark crate user / developer
status: draft
---

> **Status: draft — pre-human-review.** Recovery depends on content and the capture channel; the geometric crop bound is not a universal decoding guarantee.

# watermark

An experimental Rust library and CLI for **cooperative image/session
identification**. It embeds an eight-byte identifier into repeated luminance cells
and decodes without the original image. It does not authenticate the identifier
or resist deliberate replacement.

A 464×432 unscaled, axis-aligned crop contains a complete 232×216 tile at any
phase. Recovery also needs usable reference cells and enough surviving data
contrast. Smaller images can decode when they contain an aligned tile.

[EXPLANATION.md](./EXPLANATION.md) describes the geometry and signal model;
[ADR-0241](../../doc/adr/0241-cooperative-image-identification-watermark.md)
records the recovery-over-appearance contract. There is no live-stream integration.

## Signal and appearance

The encoder establishes a target inner-versus-ring contrast rather than blindly
adding a delta. It adjusts saturated rings to leave headroom and changes inner
pixels relative to the measured ring. This can substantially change text, texture
and saturated colors. Reference blocks are absolute dark/mid/bright levels and
can be visible even at low contrast settings.

`--delta` is target luminance contrast, **not a per-pixel distortion budget**.
The default is 8; finite values in 2..127 are accepted. Acceptance is not codec
qualification. The pathological codec acceptance test uses delta 16: at delta 8,
a one-pixel checkerboard can lose minimum-crop recovery after H.264 compression.
The default remains 8 for compatibility; use `sweep` to choose contrast for the
intended content and codec. `roundtrip` and `sweep` report mean and maximum absolute luminance
change, not a perceptual-invisibility score.

Encoding retains color for RGB/RGBA PNGs. Inputs must be opaque; composite partial
alpha against the intended background first. Output is 8-bit, with large changes
potentially reducing color saturation. Decode uses the same gamma-domain
luminance convention as encode.

## CLI

```sh
watermark encode --input base.png --output marked.png --payload deadbeef12345678
watermark decode --input marked.png
watermark roundtrip --input base.png --codec all --payload deadbeef12345678
watermark sweep --input base.png --seed 1
```

Without `--input`, encoding uses a smooth synthetic base of `--size` (default
1280x720). Without `--payload`, `encode` generates a random identifier.

`roundtrip` distinguishes first-tile BER, first-tile recovery and whole-frame
recovery. `sweep` shares a seeded payload across contrast/codec arms so changes
are not confounded by independently drawn payloads. Both are experiments at
particular settings, not certification of the crop bound.

## Library

Use `TileSpec::new` for checked contrast, `LumaFrame::from_luma` for checked
storage, and `encode_frame` / `decode_frame` at the crate root. Construction and
encoding return `Result`; geometry is immutable. Fixed-length pixel mutation is
available, and entry points reject non-finite or out-of-range samples after
mutation. `decode_report` exposes tile counts, correction information and the
raw location score; that score is not a probability.

## Build and tests

The crate pins its own Rust toolchain in `rust-toolchain.toml`, independently of
the other Rust crates. From the crate directory:

```sh
source ../../scripts/dev/rust-repro-env.sh
cargo test --locked
cargo clippy --locked --all-targets -- -D warnings
cargo fmt --check
```

Fast tests cover FEC, layout, pathological content, PNG/color I/O and CLI input.
Codec integration tests are ignored by default, rather than conditionally running
on a developer's PATH. Run the full required lane with:

```sh
../../scripts/ci/watermark_test.sh
```

That lane requires ffmpeg with libx264, libvpx-vp9 and libsvtav1. Missing required
dependencies are failures. A dedicated workflow runs it on manual dispatch and
release tags, following the repository's CI trigger policy.

The longer quality sweep remains explicitly opt-in:

```sh
cargo test --locked --release --test s8_codec codec_quality_sweep -- --ignored --nocapture
```

Earlier codec-quality figures measured the additive encoder on smooth synthetic
content. They are not carried forward as limits of the recovery-first encoder.
Scale, rotation, perspective and arbitrary screenshot processing remain outside
the qualified path. Consumer-specific imagery and codec settings need their own
measurement.
