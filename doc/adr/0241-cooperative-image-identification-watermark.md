---
type: adr
status: accepted
date: 2026-09-17
reviewed-by: "repository maintainer (implementation-plan approval)"
reviewed-date: 2026-09-17
---

# ADR-0241: Cooperative image identification watermark

## Context

The watermark identifies an image or session for cooperating producers and
readers. It is not evidence of authorship or protection against deliberate
replacement. A complete tile in a crop establishes geometric containment, not
successful recovery after arbitrary image processing.

Adding a fixed luminance delta does not establish a signal on arbitrary content:
clipping removes positive excursions on white, and text or texture can dominate
the difference between an inner block and its surrounding ring. Recovery takes
priority over preserving appearance for this use case.

## Decision

Construct a target inner-minus-ring luminance contrast rather than adding a
fixed offset to the source. Move the ring mean away from the luminance endpoints
when necessary, then adjust the inner mean relative to that reference. A convex
move toward black or white reaches the target without clipping; it can visibly
alter text and texture. Delta denotes target contrast, not a pixel-change budget.

Keep the payload, Golay code, interleaver, reference positions and tile geometry.
Retain color through a common luminance extraction/reconstruction convention.
Non-opaque images require compositing before encoding or decoding: an unknown
background is outside the channel contract.

Validate external inputs and reject explicitly uncorrectable codewords, invalid
padding and failed checksums. These checks do not authenticate a payload or
exclude all false detections. Measure single-tile recovery separately from
multi-tile recovery. Qualify codec behavior against named fixtures and settings,
not against a universal crop-recovery claim.

## Surfaces

| Surface | Change |
| --- | --- |
| Watermark cell encoding | Establish target contrast with luminance headroom |
| Rust construction API | Checked dimensions, storage and contrast; immutable geometry |
| PNG CLI | Retain color; require opaque/composited input |
| Decode result | Distinguish unusable evidence, FEC, padding and CRC failures |
| Codec validation | Explicit dependency-required test lane |

## Alternatives

- **Keep additive modulation.** It retains appearance better but does not resolve
  the white and text failures.
- **Bound distortion and reject difficult content.** Suitable for a visibility-first
  contract, but recovery takes priority here.
- **Change the tile format or adopt a keyed watermark.** Neither is necessary for
  cooperative identification; a new layout would impose an avoidable migration.

## Consequences

Local detail and color saturation can change substantially. Absolute reference
blocks remain visible. A positive clean-image contrast does not guarantee
survival through nonlinear transforms, resizing or lossy compression. The
supported path is axis-aligned and unscaled; perspective, rotation, adversarial
removal and live-stream integration remain outside this decision.

Generated pathological fixtures complement the smooth synthetic fixtures.
Distortion measurements describe pixel changes, not perceptual invisibility.
Old quality-limit measurements do not automatically apply to the new encoder.

## Migration

The bit layout is unchanged; existing decoders can sample newly encoded cells.
Stricter decoders may reject damaged images previously accepted by checksum alone.
Rust callers move to checked constructors and accessors; the crate is unpublished
and its in-repository callers move together. Re-encode images whose additive
watermarks were lost to clipping or host content.

## Verification plan

The default Rust test lane checks input contracts, signal construction,
pathological-content recovery, PNG/color round trips and CLI reporting. A
dedicated codec lane requires ffmpeg and the declared encoders, exercising
single-frame crops and a small moving sequence. No test result substitutes for
qualification on a consumer's capture path.

## Status

Accepted — the cooperative-identification contract and recovery-over-appearance
trade-off were explicitly selected during review of this change.

## References

- [Watermark](../../rust/watermark/README.md)
- [Watermark explanation](../../rust/watermark/EXPLANATION.md)
