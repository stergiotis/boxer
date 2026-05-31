---
type: adr
status: proposed
date: 2026-05-17
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0040: IDS palette (consolidated) — current OKLCh spine, semantic roles, and contracts

## Context

[ADR-0033](./0033-imzero2-design-system-palette-m0.md) (palette M0) and [ADR-0037](./0037-imzero2-design-system-palette-m1-refinement.md) (M1 spine refinement) describe the same palette through four overlapping layers: the 0033 body picked target OKLCh coordinates and `h=295` accent; a 0033 update (2026-05-16, M0b execution) bumped text + border + semantic L values to clear APCA and replaced WCAG with APCA + naive chroma-stepping with CSS Color 4 §13 GMA; a 0033 update (2026-05-17, accent refinement) moved accent to `h=270, C=0.08`; 0037 then lifted the spine from `L=0.13` to `L=0.16` and softened text; and a 0037 update (2026-05-17) widened the panel↔surface elevation step to `bg.surface=0.24`. A reader landing on any single document cannot reach the current truth.

Per the three-tier ADR policy in [boxer's DOCUMENTATION_STANDARD §1 ADR](https://github.com/stergiotis/boxer/blob/main/doc/DOCUMENTATION_STANDARD.md), this is Tier 3: an Updates chain that has accumulated more substance than the originating body, with the result no longer reachable from a single body read. The fix is to write the current state fresh in one ADR and mark the predecessors `superseded`.

This ADR is that consolidation. It captures **what the palette is now** plus the contracts (APCA, GMA, OKLab, spine-ladder rule, IP-boundary method) that constrain future moves. It does not re-litigate the design space — the QOC matrices, hue-comparison tables, and screenshot tours that informed each value live in ADR-0033 and ADR-0037 and are pointed to here.

## Decision

We will treat the palette state captured in §SD1–§SD7 below as the current authoritative IDS palette. ADR-0033 and ADR-0037 are marked `superseded by ADR-0040`; their bodies remain readable for historical context but are no longer load-bearing.

The change set vs. ADR-0033's original body is the union of all updates that ran across 0033 + 0037 between 2026-05-16 and 2026-05-17. No new design moves land in this ADR; values, contracts, and open questions are re-stated, not re-decided.

## Subsidiary design decisions

### SD1 — Neutral spine (10 tokens)

Dark theme only (v1). All tokens at `h = 240`, `C = 0.005` (a single faint blue cast over neutral grey). OKLCh L values:

| Token | L | sRGB | Use |
|---|---:|---|---|
| `bg.extreme` | 0.06 | near-black | Modal scrim base |
| `bg.panel` | 0.16 | `#0D0E10` | Default panel fill |
| `bg.faint` | 0.18 | — | Alternating rows |
| `bg.surface` | 0.24 | `#1D2021` | Raised cards; egui `Window` title-bar fill (`window_fill`) |
| `border.faint` | 0.48 | `#5B5E60` | Subtle dividers; bound to egui `widgets.noninteractive.bg_stroke` (indent vlines, faint separators) — see [ADR-0037 2026-05-17b amendment](./0037-imzero2-design-system-palette-m1-refinement.md) for the bump from L=0.26. Sits just below WCAG 3:1 by design (subtle-hint role); APCA Lc-15 floating gate is the load-bearing check |
| `border.default` | 0.58 | — | Standard borders (APCA Lc-30 floor on `bg.panel`) |
| `text.disabled` | 0.50 | — | Disabled text (APCA-exempt per ADR-0031 §SD5) |
| `text.secondary` | 0.74 | — | Caption / secondary text |
| `text.primary` | 0.93 | — | Body text (APCA Lc-90 with ~0.5 Lc headroom on `bg.surface`) |
| `text.extreme` | 0.98 | — | Display / strong emphasis |

**Spine ladder preservation rule (load-bearing).** The four background tokens must maintain `bg.extreme < bg.panel < bg.faint < bg.surface` in OKLCh L. Single-token bumps to any of the four are rejected at intake — they invert the ladder and break alternating-row affordance. Future spine adjustments must batch panel/faint/surface together.

### SD2 — Semantic palette (6 roles × 3 emphasis = 18 tokens)

| Role | h | subtle (L, C) | default (L, C) | strong (L, C) |
|---|---:|---|---|---|
| `info` | 240 | (0.20, 0.03) | (0.80, 0.12) | (0.90, 0.13) |
| `success` | 145 | (0.20, 0.03) | (0.80, 0.12) | (0.90, 0.13) |
| `warning` | 80 | (0.20, 0.03) | (0.80, 0.12) | (0.90, 0.13) |
| `error` | 25 | (0.20, 0.03) | (0.80, 0.12) | (0.90, 0.13) |
| `neutral` | — | (0.22, 0.00) | (0.80, 0.00) | (0.90, 0.00) |
| `accent` | 270 | (0.20, 0.03) | (0.80, 0.08) | (0.90, 0.13) |

Two asymmetries vs. the M0 plan worth flagging:

- **`accent.default` chroma is 0.08, not 0.12.** Breaks the all-roles-at-C=0.12 symmetry deliberately: accent reads as "supporting emphasis" rather than as a sixth status colour. Matches the register Linear, Notion, and macOS accent operate in. `accent.subtle` and `accent.strong` keep the standard C values, so the emphasis ladder reads `subtle (0.03) < default (0.08) < strong (0.13)` — default is intentionally closer to subtle than to strong.
- **`accent.hue` is 270 (blue-violet), not 295.** h=270 is the minimum hue that still reads as violet rather than blue at L=0.80; below ~h=275 the perceptual category collapses to blue regardless of L/C. h=270 also marginally improves CVD distinction from the warm status hues.
- **`neutral.subtle` L is 0.22, not 0.20.** Nudged up by 0.02 because the L=0.20 target produced sRGB `#161616`, which verbatim-collides with Carbon `gray-100` at the same role position (§SD5 IP boundary check).

### SD3 — Contrast contract: APCA primary, WCAG advisory

APCA-Lc (Myndex Beta 0.1.9 verbatim, in-tree at `scripts/ci/designcolors/gen/apca/`) is the **primary gate** for every (fg, bg) pair declared in `pairs.toml`. Three reasons APCA over WCAG 2.1:

- WCAG 2.1's `(L1 + 0.05) / (L2 + 0.05)` formula is known-flawed for dark themes — the +0.05 fudge factor lets dark-on-dark misread as passing.
- APCA's threshold is a function of `(size, weight)`, so `pairs.toml` carries explicit `font_pt` and `font_weight` per pair (e.g. "Body=13pt/400 → Lc≥90").
- APCA is proposed for WCAG 3.0 / Silver; it is the direction the contrast standard is moving in.

WCAG 2.1 stays as a **warn-only secondary check** so APCA/WCAG disagreement is visible during calibration and the legacy-compliance signal is retained.

Two pairs are explicitly graded but not gated:

- `disabled-on-panel` — intentional low-contrast per ADR-0031 §SD5.
- `secondary-on-panel` — Caption at 11pt regular is below APCA's body-text floor regardless of colour; resolution requires a Caption size or weight bump, deferred to a typography ADR.

### SD4 — Gamut mapping: CSS Color 4 §13 bisection

`scripts/ci/designcolors/gen/gma/` implements the CSS Color Module Level 4 §13 algorithm verbatim: bisect C, project via per-channel sRGB clipping, accept the largest C whose clipped projection is within ΔE_oklab 0.02 of the unclipped target. Result: provably-closest in-gamut sRGB triple by OKLab Euclidean distance. `palette.go` consumes `gma.MapToSrgbU8` in place of the original `oklab.OklchToSrgbU8`. **L is never reduced** — contrast guarantees preserved.

The original "chroma reduction in a loop" plan introduced quantization (step granularity) and hue drift near the gamut boundary (sRGB isn't a cylinder in OKLCh). Bisection eliminates both.

### SD5 — IP boundary check (verbatim sRGB + role-name match)

For every generated semantic-palette hex, the generator searches against 8 cached published-system palettes: Adobe Spectrum, Material 3, IBM Carbon, Tailwind CSS, Radix Colors, Fluent UI, Shopify Polaris, Bootstrap. Caches live under `scripts/ci/designcolors/ip-refs/<system>.json`; pinned by URL + retrieval date.

A collision is `(matching hex) AND (matching role position)`. Bare hex collisions are statistically inevitable across enough systems and are tolerated. Role+value collisions trigger a perturbation: nudge OKLCh source L by ΔL = ±0.02, re-derive sRGB, re-run contrast and CVD checks. Token *names* are also searched against the famous ladders (no `100/200/300`, no `primary/secondary/tertiary`, no `slate-500`, no `xs/sm/md/lg/xl`).

Current state: **0 (hex, role) collisions** across all 8 cached systems. The one historical collision (`neutral.subtle` `#161616` vs Carbon `gray-100`) was resolved by the L 0.20 → 0.22 nudge documented in §SD2.

### SD6 — OKLab implementation: in-repo Go + Rust port

Ottosson 2020 reference math vendored as ~80 lines of Go at `scripts/ci/designcolors/gen/oklab/oklab.go`, mirrored to Rust at `rust/imzero2/imzero2_egui/src/style/tokens/oklab.rs` via `./generate.sh`. The Rust file is generated from the Go source; CI verifies they match.

Test vectors from Ottosson 2020 + W3C CSS Color Module Level 4. Forward + inverse + gamma round-trip + OKLab↔OKLCh round-trip all pass.

Rejected alternatives — `palette` crate, `oklab` crate, `colorgrad` — fail the cross-language coverage requirement (Go-side generator + Rust-side runtime). The math is small enough to vendor in full; pinning an external crate adds an upgrade path we don't need to think about.

### SD7 — Scientific palette bundle (13 LUTs)

Vendored verbatim from upstream publications:

| Palette | Family | License | Source |
|---|---|---|---|
| `batlowS` | Crameri | MIT | cmcrameri upstream txt — default qualitative |
| `batlow` | Crameri | MIT | cmcrameri — default sequential |
| `vik` | Crameri | MIT | cmcrameri — default diverging |
| `lapaz` / `oslo` / `lajolla` | Crameri | MIT | cmcrameri — alternate sequential |
| `roma` / `broc` / `cork` | Crameri | MIT | cmcrameri — alternate diverging |
| `viridis` / `magma` / `plasma` / `inferno` | van der Walt & Smith | CC0 | `github.com/dim13/colormap` v1.1.0 |

**Cividis (Nuñez et al. 2018, CC0) is not in the v1 bundle.** Neither `cmcrameri` nor `dim13/colormap` ships it; vendoring from matplotlib's `_cm_listed.py` is ~10 minutes of work and lands in a follow-up. Flagged because cividis is the only sequential palette in the design space that was *optimised* for CVD-primary use; without it, the "CVD-explicit alternate" the ADR-0031 framework anticipates does not exist.

Crameri version pinning targets the latest stable Crameri release at the M0b execution date; SHA-pinned in the LUT file headers. Future bumps are re-runs of the generator.

## Alternatives

The original design-space analyses live in the predecessor ADRs and remain authoritative for the historical record:

- **Accent hue** (h=295 / 270 / 320 / 250 with info-shift) — ADR-0033 §Design space + ADR-0033 Update 2026-05-17.
- **OKLab implementation** (in-repo vs `palette` / `oklab` / `colorgrad`) — ADR-0033 §Design space.
- **Spine L target** (0.13 / 0.16 / 0.18 / 0.20) — ADR-0037 §Design space.
- **Contrast metric** (WCAG 2.1 vs APCA) — ADR-0033 Update 2026-05-16.
- **Gamut mapping** (chroma reduction vs CSS Color 4 §13 bisection) — ADR-0033 Update 2026-05-16.

Within the consolidated scope here, the relevant new alternative is **whether to consolidate at all**. Rejected: leave the 0033+0037+updates chain in place. The chain works for a reader who reads everything in order; it fails for the reader who lands on `0033-imzero2-design-system-palette-m0.md` from a code comment and sees a stale body. Tier 3 supersession is the documented path; this ADR exercises it.

## Consequences

### Positive

- **Single body holds the current palette.** A reader landing on ADR-0040 sees current OKLCh values, current contracts, and current open questions. No update-chain to traverse.
- **Predecessor ADRs gain a useful function as historical record.** With `status: superseded` and a top-of-body pointer to ADR-0040, 0033 and 0037 stop being treated as live and start being readable as design-history artifacts.
- **The supersession pattern is documented in practice.** Boxer's three-tier policy (Tier 3 = new superseding ADR) goes from prose to precedent; future palette refinements have a clear template to follow.

### Negative

- **One more ADR in the IDS series** (0029, 0031, 0033, 0037 → +0040). Net document count goes from 4 to 5 for the palette area; offset by 2 of those (0033, 0037) becoming non-load-bearing.
- **Cross-reference churn.** Other docs that link to ADR-0033 §SD3 or ADR-0037 §SD1 will continue to resolve, but the canonical reference is now ADR-0040. A sweep is optional; redirects are not needed (the superseded ADRs still render).

### Neutral

- **No code changes.** This ADR records the existing palette state; `palette.toml`, generated artefacts, and the verifier pipeline are untouched.
- **The supersession bar is intentionally low.** Per the three-tier policy, an Updates chain that reaches roughly three substantive entries on the same axis is the prompt to consolidate. Future palette refinements should reach for Tier 3 earlier than 0033 did.

## Status

Proposed — awaiting review by @spx.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`. See boxer's `DOCUMENTATION_STANDARD.md` §1 ADR for the edit-policy tiers (Tier 1 in-place / Tier 2 `## Updates` H3 / Tier 3 superseding ADR).

Open questions inherited from the predecessor ADRs:

1. **Crameri version pin.** v1 bundle targets the latest stable Crameri release at the M0b execution date. Re-asked at every Crameri bump.
2. **`palette.toml` `[meta] oklab_version` field semantics.** Currently `"ottosson-2020"` as a marker. If a future revision of OKLab (Ottosson 2024 or similar) ships and the math changes, a new ADR commits to the new math.
3. **Generator output formats beyond Rust / Go / markdown.** A Figma / Sketch / Adobe XD export is conceivable but out of scope (no design-tool integration planned).
4. **Line softening follow-on.** `border_default = 0.58` sits at APCA's Lc-30 ambient-UI floor; softening to ~0.48 requires either a Tier 3 ADR exempting `border-default-on-panel` from Lc-30, a re-classification of `border_default` as a non-ambient role, or per-pair tuning in `pairs.toml`. Defer until real-world feedback under L=0.16 + 0.24 spine still flags lines as too harsh.
5. **Per-app spine deviation case classes.** No app is currently asking for L=0.13 retention; the imztop / snarl / regex_explorer screens haven't been re-eyeballed under the current spine. If any visibly needs the darker canvas, a per-app exemption ADR is the right vehicle.
6. **Surface-elevation pair category.** `bg.surface` vs `bg.panel` is not in `pairs.toml` — APCA Lc is the wrong threshold for elevation perception *between* two backgrounds. Closing properly requires either a new pair category (`surface-elevation` with a ΔL threshold) or adding the pair as `advisory-only`. Defer.
7. **`bg.faint` re-evaluation.** With `bg.surface = 0.24` and `bg.faint = 0.18`, faint→surface gap is 0.06 (perceptible) but panel→faint stays at 0.02. If alternating-row affordance falls below the perceptibility floor in practice, a future ADR lifts `bg.faint` to ~0.20.
8. **`bg.extreme` review.** At L=0.06 the modal scrim is essentially pure black; scrim-vs-panel contrast is now ΔL=0.10. May read as too aggressive in modal contexts; re-eyeball + bump to ~0.08 if needed.
9. **CVD ΔE > 15 gate.** Currently advisory (68 same-emphasis pair advisories baseline); the Swiss-restrained C ≈ 0.12 default is below CVD safety for adjacent hues at the same L. Tier-2 rubric V2 ("color is never the sole encoding channel") is the load-bearing backstop today. Auto-perturbation is the path forward; not implemented.
10. **Cividis vendoring.** ~10 minutes of work to extract from matplotlib's `_cm_listed.py`; flagged as the missing CVD-explicit alternate in the v1 bundle.
11. **Per-package SSIM primitive.** Landed at `scripts/ci/designreview/ssim/` during M0b but not consumed by anything; available as the LLM pre-filter primitive for the §M4 Tier 2 design-review driver when it's built.

## References

- [ADR-0029 — design system + policy-as-code](./0029-imzero2-design-system-and-policy-as-code.md) — parent framework; §SD12 IP boundary check; §SD13 hard performance invariant (tokens apply once at startup).
- [ADR-0031 — color foundations](./0031-imzero2-design-system-color.md) — parent color ADR; §SD1 OKLCh; §SD2 semantic palette; §SD3 scientific palettes; §SD4 dark-theme neutral spine; §SD7 generator pipeline; §SD11 IP boundary.
- [ADR-0033 — palette M0](./0033-imzero2-design-system-palette-m0.md) — **superseded by this ADR**; historical record of the original design decisions + M0b execution + accent refinement.
- [ADR-0037 — palette M1 refinement](./0037-imzero2-design-system-palette-m1-refinement.md) — **superseded by this ADR**; historical record of the L=0.13→0.16 spine bump + text softening + bg.surface 0.20→0.24 amendment.
- tier3-human-review.md — Tier 3 process; T3-007 (accent hue) + T3-010 (OKLab impl) closed by the predecessor chain.
- `scripts/designcolors.sh` — regenerator entry point.
- `scripts/dev/hmi_screenshots.sh` — tour capture entry point.
- [APCA / SAPC-APCA](https://git.apcacontrast.com/) — Andrew Somers' contrast model; the primary gate per §SD3.
- [CSS Color Module Level 4 §13](https://www.w3.org/TR/css-color-4/#binsearch) — gamut-mapping bisection algorithm per §SD4.
- Crameri, F. (2018). *Scientific colour maps* (Version 8.0.1) — [Zenodo](https://doi.org/10.5281/zenodo.1243862).
- van der Walt, S. & Smith, N. (2015). *Default colors for matplotlib (the viridis family).* — [bids.github.io/colormap](https://bids.github.io/colormap/).
- Nuñez, J. R., Anderton, C. R., & Renslow, R. S. (2018). *Optimizing colormaps with consideration for color vision deficiency.* PLOS ONE 13(7): e0199239 — cividis CC0.
- Björn Ottosson (2020). *A perceptual color space for image processing.* — [bottosson.github.io/posts/oklab](https://bottosson.github.io/posts/oklab/).
- Brettel, H., Viénot, F., & Mollon, J. D. (1997). *Computerized simulation of color appearance for dichromats.* JOSA A 14(10), 2647–2655 — CVD method.
