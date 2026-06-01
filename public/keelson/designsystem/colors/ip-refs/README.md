---
type: reference
audience: IDS color generator maintainers
status: draft
---

> **Status: draft — pre-human-review.** Cache is non-exhaustive; extend on real collisions.

# IP-boundary reference cache

Per [ADR-0033 §SD7](../../../../../doc/adr/0033-imzero2-design-system-palette-m0.md) and [ADR-0029 §SD12](../../../../../doc/adr/0029-imzero2-design-system-and-policy-as-code.md), the color generator searches every emitted IDS semantic-palette hex value against published design-system palettes. Each `<system>.json` file caches the published anchor hex values as a `{"<role-or-token>": "#RRGGBB"}` map.

The cache is **non-exhaustive on purpose** — it covers the most-recognisable anchors of each system (the 500-tier of Tailwind, the primary Material 3 hues, the Spectrum global colors). When a real collision surfaces in `ip-boundary-check.md`, the entry is extended with the surrounding shades.

## Cache update process

1. Visit the upstream system's published palette page (URL in each JSON's `_source` field).
2. Manually copy the named anchor hex values into the JSON.
3. Record retrieval date in `_retrieved` field.
4. Re-run the generator; new collisions surface in `ip-boundary-check.md`.

This is one-time vendor work, not a generator-time fetch — the network call would defeat reproducibility. JSON files are static and reviewed in PR.

## Collision policy

A hex match alone is not a violation; sRGB collisions are statistically inevitable across 8 systems. The collision **plus** a matching role triggers an `ΔL = 0.02` nudge to the OKLCh source and a re-derive (per ADR-0033 §SD7). The boundary log records the nudge.
