---
type: adr
status: proposed
date: 2026-05-14
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0031: ImZero2 design system — color foundations (OKLCh semantic + scientific data-encoding, dark theme)

## Context

[ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) introduces the ImZero2 Design System (IDS) and reserves the color choice for a foundations sub-ADR (§SD4). The parent commits to:

- **Two disjoint palettes** — a *semantic* palette (info / success / warning / error / neutral / accent) and a *data-encoding* palette (qualitative / sequential / diverging). The disjointness prevents a chart color colliding with a status color (e.g., red as "error" vs. red as "high temperature").
- **A perceptually-uniform color space** for construction — the "CIE L\*C\*h\* family."
- **Original values** — no hex value or token name lifted from Spectrum / Material / Carbon / Tailwind / Fluent / Polaris / Bootstrap. Pre-cleared exceptions per [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD12: scientifically-published colormaps under CC0 / MIT (viridis family, Crameri's *Scientific colour maps*).

This sub-ADR makes the deferred choices concrete: which space variant for *constructed* tokens, which *scientific* palettes to adopt for data-encoding, dark-only scope, accessibility, the source-of-truth artefacts and build pipelines, and the binding from IDS tokens to `egui::Visuals` and `egui_plot`.

The use case sharpens the decision in four ways:

- **Data-intensive surface.** Most ImZero2 apps are tables, plots ([project_grafana_replacement] is the largest in-flight), heatmaps (regex_explorer), and metrics dashboards (imztop). The data-encoding palettes carry as much visual weight as the semantic palette — sometimes more.
- **Long viewing sessions.** Engineers watching panels for tens of minutes need low eye-strain; high-chroma colors fatigue quickly. Swiss-minimalist restraint ([ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) Context fourth force) lands here as *low-chroma* semantic colors, not because of taste alone but because of perceived fatigue.
- **Scientific defensibility.** The maintainer is a scientist. Custom sequential / diverging ramps — even ones constructed in OKLCh — reinvent inferior versions of published scientific results (viridis: van der Walt & Smith 2015; Crameri's *Scientific colour maps*: Crameri 2018). Adopting the published palettes is the *more* original choice — the value is in the scientific publication, not in the hand-rolled imitation.
- **Dark theme only for v1.** Per [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) Context. Light theme is deferred until a real daylight-office complaint surfaces. This dramatically simplifies the token count, the binding code, the theme-switching surface, and the M1 verification work.

The IDS performance invariant ([ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD13) constrains the implementation: color values are `egui::Color32` (sRGB) constants at runtime; OKLCh math happens at *design-time* (semantic palette generation) only, and the scientific palettes are pre-rendered 256-entry RGB lookup tables — neither touches the render path.

## Design space (QOC)

**Question.** Which color-space + palette-source combination best serves a dark-only, Swiss-minimalist, scientifically-defensible IDS for a data-intensive desktop UI under egui?

**Options.**

- **O1 — sRGB hand-picked, custom data-encoding.** Hex values by eye for both palettes. Zero color-science overhead.
- **O2 — CIE L\*C\*h\* for both palettes.** Parent ADR's stated method. Custom-construct semantic *and* data-encoding ramps in CIE LCh.
- **O3 — OKLCh for the semantic palette + direct adoption of scientific colormaps for data-encoding (chosen).** Use OKLCh's perceptual uniformity to construct restrained semantic tokens; adopt Crameri (batlow / vik / batlowS) + the viridis family for data-encoding without modification.
- **O4 — Inherited palette from a design system** (Tailwind / Material / Radix / Carbon). Rejected at intake per [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD12.
- **O5 — sRGB + per-display calibration.** Runtime device-profile math. Rejected on the performance invariant.
- **O6 — OKLCh for the semantic palette + custom-constructed OKLCh data-encoding ramps.** The previous draft's direction. Custom sequential/diverging from scratch.

**Criteria.**

- **C1 — Perceptual uniformity.** Do "equally distant" colors look equally distant?
- **C2 — Blue-hue handling.** CIE Lab rotates blues toward purple at high L; OKLab fixes this.
- **C3 — CVD-safe verifiability.** Palette pairs distinguishable under deuteranopia / protanopia / tritanopia.
- **C4 — Scientific defensibility (data-encoding).** Does the data-encoding choice survive scientific scrutiny? Can you cite a peer-reviewed publication for it?
- **C5 — Runtime cost.** Does any color math touch the render path?
- **C6 — IP cleanliness.** Risk of inadvertently reproducing a published palette.
- **C7 — Swiss-minimalist alignment.** Does the palette feel restrained — low chroma, type-driven hierarchy supported, no ornament-by-color?
- **C8 — Maintenance / iteration cost.** Effort to evolve the palettes over time.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative. (O4 excluded — IP-disqualified at intake.)

|    | O1 | O2 | O3 | O5 | O6 |
|----|----|----|----|----|----|
| C1 | −− | +  | ++ | −− | ++ |
| C2 | −  | −  | ++ | −  | ++ |
| C3 | −  | +  | ++ | −  | ++ |
| C4 | −− | −  | ++ | −− | −  |
| C5 | ++ | ++ | ++ | −− | ++ |
| C6 | ++ | ++ | ++ | ++ | ++ |
| C7 | −  | +  | ++ | −  | ++ |
| C8 | +  | −  | ++ | +  | −  |

O3 dominates without losing on any criterion. O6 (the previous draft) is the closest competitor and loses only on C4 and C8 — but those are decisive: hand-constructed ramps cannot match a peer-reviewed perceptually-uniform colormap, and rebuilding the wheel every time the palette evolves is wasted effort. O2 loses on the same C4 plus blue-hue rotation (C2). O1 and O5 fail the foundational criteria.

## Decision

We will use:

- **OKLCh** as the construction space for the *semantic* palette only (6 roles × 3 emphasis × dark theme = ~18 tokens). Swiss-restrained chroma: lower C than typical UI palettes (C ≈ 0.10 for default emphasis, vs. ~0.15 in typical bright-UI systems). Verified for WCAG AA contrast and CVD distinguishability (§SD5).
- **Crameri's Scientific colour maps** (Crameri 2018, MIT) as the *default* data-encoding family:
  - **Qualitative** — `batlowS` (10 categorical colors sampled from batlow's perceptually-uniform curve)
  - **Sequential** — `batlow` (default); `lapaz`, `oslo`, `lajolla` available as alternates
  - **Diverging** — `vik` (default); `roma`, `broc`, `cork` available as alternates
- **The viridis family** (van der Walt & Smith 2015, CC0) as opt-in alternates for sequential — `viridis`, `magma`, `plasma`, `inferno`, `cividis` — for users who prefer the matplotlib lineage or need cividis's explicit CVD-optimisation.
- **Dark theme only** for v1. Light theme is deferred. No theme-switching code, no parallel L-spine generation.
- WCAG AA mandatory; AAA aspirational. CVD verification gates the *semantic* palette in CI; the scientific palettes are pre-verified by their publications.

Semantic palette OKLCh coordinates live in `palette.toml`; a build script generates `Color32` constants (§SD8). Scientific palettes are vendored as 256-entry RGB lookup tables under MIT / CC0 with provenance metadata.

## Subsidiary design decisions

### SD1 — Color space for semantic palette: OKLCh

OKLCh (Ottosson 2020) is the cylindrical form of OKLab. We use it for the *semantic palette construction only* — the scientific palettes (§SD3) are adopted as sRGB lookup tables and do not pass through OKLCh.

Justification (vs. parent ADR's "CIE L\*C\*h\*"):

- **Blue-hue rotation.** CIE Lab's blue at high L drifts toward purple; OKLab keeps blue blue. Relevant because the semantic Info color is in the blue family.
- **Chroma scaling.** OKLab's `a/b` (and thus OKLCh's C) is closer to perceptual saturation across hues; same-C-different-h tokens feel equally loud.

OKLCh sits within the parent ADR's "CIE L\*C\*h\* family" intent — same cylindrical L/C/h structure, refined math. The substitution is captured in this sub-ADR rather than requiring a parent-ADR amendment.

### SD2 — Semantic palette: 6 roles × 3 emphasis (dark theme only)

Six roles, three emphasis levels each. **Swiss-restrained chroma** throughout — C values picked at the low end of the "still feels colored" range so the screenshots read as type-and-grid Swiss rather than tinted-everything modern.

| Role     | Hue (deg) | Use                                                                       |
|----------|-----------|---------------------------------------------------------------------------|
| info     | 240       | Informational, links, neutral attention                                   |
| success  | 145       | Positive confirmation, healthy state                                      |
| warning  | 80        | Caution, attention needed                                                 |
| error    | 25        | Destructive, error, failed state                                          |
| neutral  | —         | Surfaces, borders, body text (C = 0)                                      |
| accent   | 295       | Branded highlights, selection, focus rings (final hue in §Status Q1)      |

Emphasis levels, dark theme only:

- **subtle** — background tints (e.g., a warning-tinted row); L ≈ 0.20, C ≈ 0.03
- **default** — standard foreground (e.g., warning badge fill); L ≈ 0.68, C ≈ 0.10
- **strong** — emphasised states, hover / active; L ≈ 0.82, C ≈ 0.12

The default-emphasis C ≈ 0.10 is the Swiss-restraint dial. Typical bright-UI systems sit at C ≈ 0.15–0.20; IDS deliberately undersaturates. This is a *deliberate aesthetic restraint*, not a perceptual constraint. If real-world testing in M1 finds the palette feels muted to the point of weakness, C can lift to 0.12 (Q9).

The neutral role uses C = 0 across all emphasis levels plus additional L points for surface elevation (§SD4).

### SD3 — Data-encoding palette: scientifically-published colormaps

Direct adoption of peer-reviewed, perceptually-uniform, CVD-safe, public-domain / MIT-licensed colormaps. No construction; no perturbation; no IDS-specific tuning — we ship the published values verbatim, with attribution.

**Qualitative — `batlowS` (default; 10 colors).** Crameri's `batlowS` is a categorical sampling of the `batlow` sequential curve. Each color sits at a distinct L *and* hue, which means the palette degrades gracefully to grayscale (still ordered) and survives all three CVD transforms by construction. Ten colors is the published cardinality; beyond ten we fall back to repetition (Tier 3 escalation if a real consumer needs more).

**Sequential — `batlow` (default).** Crameri's flagship. Perceptually uniform; lightness-monotonic; CVD-safe; converts to grayscale with monotonic luminance. 256-entry LUT, vendored. Alternates available as opt-in palettes:

- `lapaz` — blue-to-yellow, scientific
- `oslo` — pure-blue sequential
- `lajolla` — orange-warm sequential
- `viridis` — van der Walt & Smith's matplotlib flagship (CC0); included for users familiar with the matplotlib lineage
- `magma` — viridis family, warmer
- `plasma` — viridis family, purple-to-yellow
- `inferno` — viridis family, dark-to-fire
- `cividis` — Nuñez et al. 2018 (CC0); explicitly CVD-optimised, useful when CVD is a stated priority

The default is `batlow`; the alternates are reached for when a specific data domain wants a domain-specific palette (e.g., topography conventions for `lapaz`, temperature for `lajolla`).

**Diverging — `vik` (default).** Crameri's flagship diverging. Perceptually uniform; symmetric around a neutral midpoint (light grey L ≈ 0.55, hue-neutral); CVD-safe. 256-entry LUT, vendored. Alternates:

- `roma` — diverging with different terminal hues
- `broc` — green-purple diverging
- `cork` — teal-pink diverging

The default is `vik`; alternates reached for when a chart's semantic meaning aligns with a specific domain palette.

**License and attribution.** Crameri palettes are MIT-licensed; viridis/cividis are CC0. Both require citation in scientific publications; IDS treats `INSPIRATIONS.md` plus a comment in the LUT file as the equivalent. Per [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD12 (updated), scientific palettes are pre-cleared for IP — adopting them is the more-original choice.

**Vendoring details.** Each palette ships as a 256-entry `[(u8, u8, u8); 256]` static array in Rust (parallel `[256][3]uint8` in Go). One file per palette under `rust/imzero2/imzero2_egui/src/style/data_encoding/`. Each file carries a header comment with the source DOI, version, and SHA-256 of the upstream `.txt`. Total size cost across all bundled palettes: ~50 KB Rust + ~50 KB Go.

A small conversion script `scripts/ci/designcolors/vendor/main.go` reads upstream `.txt` files (one RGB triple per line; Crameri's published format; viridis matplotlib export format) and emits the Rust + Go source files deterministically. CI re-runs the script and verifies byte-equality against the committed files.

### SD4 — Dark theme reference points

Ten neutral L points define the dark theme spine:

| Token              | Approx L | Use                                       |
|--------------------|----------|-------------------------------------------|
| `bg.extreme`       | 0.06     | Modal scrim base                          |
| `bg.panel`         | 0.13     | Default panel fill                        |
| `bg.faint`         | 0.17     | Alternating rows, faint surfaces          |
| `bg.surface`       | 0.20     | Raised cards over `bg.panel`              |
| `border.faint`     | 0.26     | Subtle dividers                           |
| `border.default`   | 0.32     | Standard borders, window strokes          |
| `text.disabled`    | 0.50     | Disabled text                             |
| `text.secondary`   | 0.72     | Caption, secondary labels                 |
| `text.primary`     | 0.90     | Body text                                 |
| `text.extreme`     | 0.98     | Display, strong emphasis                  |

C ≈ 0.005 with h = 240 — a barely-perceptible cool tint, not pure grey. The tint is small enough to read as neutral but cool enough to feel intentional (Swiss-aligned). Pure-grey neutrals read as "computer default"; the faint tint reads as "designed."

### SD5 — Accessibility: WCAG contrast + CVD verification

**Contrast (WCAG 2.1).**

- **AA mandatory** for every (foreground, background) pair shipped in the token module:
  - Body text on body background: ≥ 4.5 : 1
  - Large text (≥ 18 pt or ≥ 14 pt bold): ≥ 3 : 1
  - UI components (icons, focus indicators, semantic borders): ≥ 3 : 1
- **AAA aspirational** (target 7 : 1 / 4.5 : 1 where aesthetics permit).

Automated check (§SD9): a tool reads `palette.toml`, enumerates every (fg, bg) declared in `pairs.toml`, computes WCAG contrast, fails CI on AA violation, warns on AAA miss.

**CVD verification (semantic palette only).** Three CVD types verified via Brettel-Viénot-Mollon simulation:

- **deuteranopia** (~5% of males) — green cone missing
- **protanopia** (~1%) — red cone missing
- **tritanopia** (rare) — blue cone missing

Every pair of semantic tokens at the same emphasis level must have ΔE > 15 in CVD-transformed OKLab. A failing pair has its higher-index hue perturbed in OKLCh until the pair separates; the perturbation is committed to `palette.toml`.

**Data-encoding CVD verification.** Pre-validated by publication. Crameri palettes are explicitly CVD-tested in the source paper; viridis family is similarly validated. We trust the publication — no in-house re-verification — and document the dependency by SHA-pinning the upstream LUT.

**Color is never the sole encoding channel.** Plot series additionally encode via line style (solid / dashed / dotted), marker shape, and label. Tables encode via column position and text. Status badges encode via icon + text. This rule lives in `patterns/status-and-legends.md` and is verified by Tier 2 rubric V2.

### SD6 — Binding to `egui::Visuals`

The token module's `apply(visuals, density)` function writes IDS colors into `egui::Visuals`. No `theme` parameter — dark only. Abbreviated mapping:

| IDS token                          | `egui::Visuals` field                                   |
|------------------------------------|---------------------------------------------------------|
| `bg.panel`                         | `panel_fill`                                            |
| `bg.faint`                         | `faint_bg_color`                                        |
| `bg.extreme`                       | `extreme_bg_color`                                      |
| `bg.surface`                       | `window_fill`                                           |
| `border.default`                   | `window_stroke.color`                                   |
| `text.primary`                     | `override_text_color` (when set)                        |
| `semantic.info.default`            | `hyperlink_color`                                       |
| `semantic.warning.default`         | `warn_fg_color`                                         |
| `semantic.error.default`           | `error_fg_color`                                        |
| `semantic.accent.default`          | `selection.bg_fill`                                     |
| `neutral.*` per state              | `widgets.{noninteractive, inactive, hovered, active, open}.{bg_fill, fg_stroke, ...}` |

The full mapping is M1's deliverable in `policy/color-egui-binding.md`. Apps that need to deviate (e.g., a custom dialog wants a non-standard `panel_fill`) escalate via Tier 3.

### SD7 — `egui_plot` integration

`egui_plot` accepts colors via `Line::color(Color32)`, `Points::color`, `BarChart::color`, etc. The IDS token module exposes:

```rust
pub fn qualitative_cycle(idx: usize) -> Color32;
pub fn sequential(palette: SequentialE, t: f32)  -> Color32;  // t ∈ [0, 1]
pub fn diverging (palette: DivergingE,  t: f32)  -> Color32;  // t ∈ [-1, 1]

pub enum SequentialE { Batlow, Lapaz, Oslo, Lajolla, Viridis, Magma, Plasma, Inferno, Cividis }
pub enum DivergingE  { Vik, Roma, Broc, Cork }
```

`qualitative_cycle` reads from the batlowS LUT (`idx % 10`). `sequential` / `diverging` index into the chosen palette's 256-entry LUT — a single lookup, no allocation, no branching beyond the enum match. All three are `#[inline]`, deterministic, and safe to call per-frame.

The companion patterns doc `patterns/plots.md` documents *when* to use which palette: qualitative for categorical series, sequential for ordered magnitude, diverging for signed deviation. Specific Crameri/viridis selections are domain-driven (topography → lapaz; temperature → lajolla; default → batlow).

### SD8 — Source of truth and build pipelines (two flows)

**Flow 1 — Semantic palette (constructed in OKLCh).**

- **Source**: `rust/imzero2/imzero2_egui/assets/colors/palette.toml` — OKLCh coordinates for every semantic token.
- **Generator**: `scripts/ci/designcolors/gen/main.go` — reads `palette.toml`, performs OKLCh→sRGB gamut mapping (clip-to-gamut via chroma reduction; never lightness reduction, to preserve contrast), runs §SD5 contrast + CVD checks, writes:
  - `rust/imzero2/imzero2_egui/src/style/tokens/palette_generated.rs`
  - `public/thestack/imzero2/egui2/styletokens/palette_generated.go`
  - `doc/design-system/foundations/color.md` (semantic-palette section)
- **Build artefacts committed**; CI verifies byte-equality on regen.

**Flow 2 — Scientific palettes (vendored LUTs).**

- **Sources**: Crameri palettes from Zenodo (DOI 10.5281/zenodo.1243862) and viridis family from matplotlib (CC0). Vendored as upstream `.txt` files under `rust/imzero2/imzero2_egui/assets/colors/scientific/`.
- **Converter**: `scripts/ci/designcolors/vendor/main.go` — reads `.txt`, emits `data_encoding/<palette>.rs` (Rust) and `data_encoding/<palette>.go` (Go) with provenance comments (DOI, version, upstream SHA).
- **Build artefacts committed**; CI verifies byte-equality on regen and verifies upstream SHA matches.

The two flows share `scripts/ci/designcolors/` infrastructure but are independent — semantic palette changes do not re-vendor scientific palettes and vice versa.

### SD9 — Tooling: contrast verifier, CVD simulator, palette explorer

Three sub-tools under `scripts/ci/designcolors/`:

- **`contrast/`** — pair-wise WCAG contrast checker for semantic palette pairs. Reads `palette.toml` and `pairs.toml`; fails CI on AA violation; warns on AAA miss.
- **`cvd/`** — Brettel-Viénot-Mollon simulator for the semantic palette. Verifies pairwise ΔE > 15 across same-emphasis pairs. Fails CI on violation. Skipped for scientific palettes (pre-validated by publication).
- **`explorer/`** — local HTTP server that renders the palette as swatches (semantic plus all scientific) side-by-side normal + CVD-transformed, with contrast ratios and a `egui_plot` test panel. Used by the maintainer to inspect changes before committing. Not run in CI.

All three are written in Go (consistent with [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD8). Zero runtime presence in apps.

### SD10 — IP boundary

Per [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD12:

- **Semantic palette** — verbatim hex search against Spectrum / Material / Carbon / Tailwind / Radix / Fluent / Polaris / Bootstrap. Collision *and* role match triggers ΔL = 0.02 nudge and re-derivation. Token names checked against famous ladders (no `100/200/300`, no `primary/secondary/tertiary`). Verified original.
- **Data-encoding palettes** — pre-cleared. Crameri (MIT) and viridis family (CC0) are scientifically-published, publicly-licensed, and *expected* to be reused verbatim by downstream consumers. Adopting them is the more-original choice (vs. constructing inferior imitations). Each vendored LUT carries provenance: DOI, version, upstream SHA, citation string. `INSPIRATIONS.md` records the broader citation.

The boundary-check log is committed to `doc/design-system/foundations/ip-boundary-check.md` and updated on every semantic-palette change.

### SD11 — Phasing

- **M0 — Generate semantic palette + vendor scientific palettes.** Define `palette.toml`; run generator; run contrast + CVD verifiers; run IP boundary search. Vendor `batlow`, `vik`, `batlowS`, `viridis`, `cividis` LUTs with provenance. Output: committed `palette.toml`, generated tokens, vendored LUTs, `ip-boundary-check.md`, `color.md`. Plan ~1 day plus iteration.
- **M1 — `egui::Visuals` binding + pilot app.** Implement `apply(visuals, density)` per §SD6. Wire pilot app (carousel demo) to use the tokens. Capture screenshots for [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) M4 Tier 2 calibration.
- **M2 — `egui_plot` integration.** Implement `qualitative_cycle` / `sequential` / `diverging` per §SD7. Migrate one plot consumer (a Grafana-replacement panel or the imztop CPU/memory plots) to the data-encoding palette. Document patterns in `patterns/plots.md`.
- **M3 — Fleet backfill.** Apply tokens to existing apps (regex_explorer, snarl, time-range picker, imztop, dock demo, walkers). One app per follow-on PR.

Each milestone is independently shippable. No theme-switching milestone — dark only.

## Alternatives

- **O1 — sRGB hand-picked, custom data-encoding.** Rejected on C1 / C3 / C4: hand-picked sRGB cannot achieve perceptual uniformity, CVD safety, or scientific defensibility.
- **O2 — CIE L\*C\*h\* for both palettes.** Rejected for blue-hue rotation (C2) and especially for C4 — custom diverging ramps in CIE LCh reinvent inferior versions of vik / RdBu without scientific publication backing.
- **O4 — Inherited design palette.** Rejected at intake per [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD12.
- **O5 — sRGB + per-display calibration.** Rejected on the performance invariant.
- **O6 — OKLCh for both palettes with custom-constructed sequential / diverging.** This was the *previous* draft's direction. Rejected here on C4 and C8: hand-constructed perceptually-uniform ramps in OKLCh are inferior in scientific defensibility and require ongoing iteration that adopted publications do not. Crameri's batlow and the viridis family are the rigorously-validated artefacts; we adopt them rather than imitate.
- **HSL / HSV hand-tuning.** Same L in HSL is wildly different perceived brightness across hues; rejected on C1.
- **Per-app overridable palette tokens.** Forks the palette across apps; defeats cohesion. Tier 3 escalation only.
- **WCAG AAA mandatory.** Constrains the design space heavily; rejected as mandatory, kept as aspirational.
- **Light theme support at M1.** Rejected: dark only for v1 per [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) Context fourth force. Reconsider if a real daylight-office complaint accumulates; the framework supports adding a light spine without changing scientific-palette adoption.
- **Custom Swiss-themed colormaps (e.g., a black-red-white Swiss-flag-inspired diverging).** Considered as a stylistic statement. Rejected: would fail scientific defensibility (C4) and CVD safety (C3) without re-validation; the Swiss aesthetic is already carried by the restrained semantic chroma (SD2) and the grid / type direction in ADR-0030.
- **Material 3-style derivation from one "primary" hue.** Collapses the semantic/data-encoding disjointness; rejected.

## Consequences

### Positive

- **Scientifically defensible data-encoding.** No critique possible — we use peer-reviewed, perceptually-uniform, CVD-validated colormaps by their authors' names. Citations are honest because we use the verbatim published values.
- **Swiss-restrained semantic palette.** Low-chroma defaults (C ≈ 0.10) keep the screenshots minimal; hierarchy comes from type weight (ADR-0030) and spacing, not from color shouting.
- **Dark-only simplifies dramatically.** No theme switching, no parallel L spine, no system-preference detection, no theme-change bus event. M1 ships faster.
- **Less work than custom construction.** Vendoring three Crameri LUTs + the viridis family is a one-day effort vs. weeks of iterating custom ramps.
- **egui-native integration intact.** Tokens map to `Visuals` fields; `egui_plot` consumes the data-encoding functions; no widget forks.
- **Performance invariant intact.** All OKLCh math is design-time; scientific LUTs are static arrays; runtime is `Color32` constants and a single index per ramp lookup.
- **IP exposure low and audited.** Semantic palette gets the §SD10 verbatim search; scientific palettes are pre-cleared by their publication terms. Both flows produce reproducible logs.

### Negative

- **No light theme.** Daylight-office users will be uncomfortable until a light theme lands. The framework supports adding it later — the semantic palette generator can produce a light spine; the scientific palettes work in both themes by virtue of being absolute colors. Mitigation: monitor for real complaints; revisit as a Tier 3 ADR if they accumulate.
- **OKLCh→sRGB gamut clipping on the semantic palette is non-trivial.** Some L/C/h triples sit outside the sRGB gamut; chroma reduction is the standard fix but shifts perceived character. M0 surfaces which target points need adjustment.
- **Crameri LUTs are 256-entry; vendored bytes drift if Crameri publishes a new version.** Mitigation: SHA-pin to a specific Crameri release version; treat upstream updates as Tier 3 changes.
- **AAA aspirational means AAA is not enforced.** Some text/bg pairs may sit at AA-only indefinitely. Acceptable for v1.
- **Palette changes are public-stability surface.** Re-keying a semantic token or swapping a default scientific palette is a fleet sweep. Mitigation: Tier 3 ADR for every change.

### Neutral

- **`palette.toml` and the vendored LUTs are public artefacts.** Reviewers see exact OKLCh coordinates and exact upstream RGB tables.
- **Generated `palette_generated.{rs,go}`, `data_encoding/*.{rs,go}`, and `color.md` are checked in.** CI re-runs both flows and verifies byte-equality.
- **The `accent` role is one hue, fleet-wide.** Apps that want their own accent escalate via Tier 3.

## Status

Proposed — awaiting review by @spx. M0 (semantic palette generation + scientific palette vendoring + IP check) can begin immediately upon acceptance.

Open questions:

1. **Accent hue final pick.** Proposed h = 295 (violet). Alternatives: h = 180 (teal, calmer), h = 340 (pink, energetic), h = 20 (orange-red — collides with `error`, no). Could also consider Swiss-flag red as a *deliberate cultural reference* — but it collides with `error`. Defer to M0 swatch comparison.
2. **Default sequential palette pick.** Proposed `batlow` (Crameri default, Swiss-authored, perceptually-uniform-and-CVD-safe). Alternative defaults: `viridis` (more widely recognised in matplotlib world). Likely keep `batlow` for the Swiss alignment; revisit if real-world reading suggests viridis is more legible in the imztop-style use case.
3. **How many Crameri palettes to bundle.** Minimum proposed: `batlow`, `vik`, `batlowS`. Plus viridis family. Lapaz / oslo / lajolla / roma / broc / cork are opt-in but vendoring all of Crameri's library (~50 palettes) is bytes we don't need. Pick a curated subset (~10) in M0.
4. **OKLab implementation pinning.** Rust: `palette` crate (mature) vs. a small in-repo port. Go: small embedded port under `scripts/ci/designcolors/gen/oklab/`. Choice in M0.
5. **Diverging midpoint policy.** Crameri's `vik` has a fixed light-grey midpoint built into the LUT — we adopt it as-is. No theme-adjusted midpoint (we're dark-only and the published midpoint reads as neutral on a dark background).
6. **Per-display calibration.** Out of scope for v1.
7. **Plot legend swatch handling.** Should legend swatches use the exact qualitative color or a slightly desaturated version for legibility against the dark legend background? Defer to M2.
8. **Snarl node-class colors.** Should snarl ([ADR-0021](./0021-imzero2-snarl-node-editor-binding.md)) consume `batlowS` for node-class tints, or get its own palette slot? Likely `batlowS` for v1; revisit if collisions with plot series in mixed views become an issue.
9. **Semantic-palette C value.** Proposed default-emphasis C ≈ 0.10 (Swiss-restrained). If M1 testing finds it feels muted, lift to 0.12. Concrete trigger: a Tier 2 rubric V1 (hierarchy) finding tracing back to insufficient color weight, on more than one app's screenshots.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only; supersession is recorded, not deleted.

## References

- [ADR-0029 — ImZero2 design system: foundations, data-intensive patterns, policy-as-code](./0029-imzero2-design-system-and-policy-as-code.md) — parent ADR; this is the §SD4 color-foundations sub-decision.
- [ADR-0030 — ImZero2 design system: typography](./0030-imzero2-design-system-typography.md) — sibling foundations ADR; §SD8 here parallels §SD9 there (source-of-truth + generator + committed artefacts pattern).
- [ADR-0026 — App runtime and capability subjects](./0026-app-runtime-and-capability-subjects.md) — runtime bus carries no theme-switching event in v1 (dark only).
- [ADR-0021 — ImZero2 snarl node editor binding](./0021-imzero2-snarl-node-editor-binding.md) — candidate consumer of `batlowS` (§Status Q8).
- Crameri, F. (2018). *Scientific colour maps* (Version 8.0.1) [Software]. Zenodo. [https://doi.org/10.5281/zenodo.1243862](https://doi.org/10.5281/zenodo.1243862) — MIT-licensed; source for `batlow`, `vik`, `batlowS`, and alternates (§SD3).
- van der Walt, S. & Smith, N. (2015). *Default colors for matplotlib (the viridis family).* [https://bids.github.io/colormap/](https://bids.github.io/colormap/) — CC0; source for `viridis`, `magma`, `plasma`, `inferno`.
- Nuñez, J. R., Anderton, C. R., & Renslow, R. S. (2018). *Optimizing colormaps with consideration for color vision deficiency to enable accurate interpretation of scientific data.* PLOS ONE 13(7): e0199239. — CC0; source for `cividis`.
- Björn Ottosson, *A perceptual color space for image processing* (2020). [https://bottosson.github.io/posts/oklab/](https://bottosson.github.io/posts/oklab/) — OKLab / OKLCh primary reference for §SD1.
- Brettel, H., Viénot, F., & Mollon, J. D. (1997). *Computerized simulation of color appearance for dichromats.* JOSA A 14(10), 2647–2655. — CVD simulation method used in §SD5 / §SD9.
- WCAG 2.1 success criteria 1.4.3 (Contrast Minimum, AA) and 1.4.6 (Enhanced Contrast, AAA). [https://www.w3.org/TR/WCAG21/](https://www.w3.org/TR/WCAG21/).
- [`egui::Visuals`](https://docs.rs/egui/latest/egui/style/struct.Visuals.html) — target API for §SD6.
- [`egui_plot`](https://docs.rs/egui_plot) — consumer of §SD7.
