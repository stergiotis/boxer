---
type: adr
status: superseded
date: 2026-05-14
superseded-by: ADR-0040
---

> **Superseded by [ADR-0040](./0040-imzero2-design-system-palette-consolidated.md)** (2026-05-17). The current authoritative palette state — OKLCh values, contrast contract, gamut-mapping algorithm, scientific bundle — lives in ADR-0040. This document remains as historical record of the M0 design decisions, the M0b execution amendment (APCA + GMA upgrade), and the 2026-05-17 accent refinement (hue 295 → 270, default chroma 0.12 → 0.08).

# ADR-0033: IDS palette M0 — OKLab impl, accent hue, semantic palette generation

## Context

[ADR-0031 §SD13 M0](./0031-imzero2-design-system-color.md) names three deliverables to unblock the IDS color foundations:

- `palette.toml` — OKLCh source of truth for the semantic palette
- The generator pipeline that turns `palette.toml` into `palette_generated.{rs,go}` + `color.md` + the IP boundary log
- Verifier passes — WCAG AA + CVD ΔE for the semantic palette; provenance + SHA pinning for the vendored scientific palettes

This ADR is the Tier 3 follow-on that captures the *design-time decisions* the M0 work depends on: which OKLab implementation to use, which accent hue to pin, which concrete OKLCh coordinates the semantic palette commits to, and which Crameri / viridis palettes are bundled. Per tier3-human-review.md §Pending decisions, this ADR resolves:

- **T3-007** — Accent hue final pick (proposed `h = 295` violet; alternates considered)
- **T3-010** — OKLab implementation pinning (`palette` crate vs in-repo port)

It also commits the semantic palette's OKLCh target coordinates and the scientific palette bundle subset for v1.

This ADR follows the **ADR-0028 M0 spike pattern**: the design decisions land in the proposed ADR; the *execution results* (concrete sRGB hex values from running the generator, IP boundary-check log, contrast / CVD verifier reports) land as an Amendment dated when the generator runs. The two stages keep the design contract reviewable independently of the execution outputs.

## Design space (QOC)

**Question 1.** Which accent hue best serves the IDS aesthetic intent (Swiss-minimalist; distinct from semantic; CVD-safe; not collision-prone with the existing semantic hues)?

**Options.**

- **O1a — `h = 295` violet (chosen).** Distinct from all four semantic-status hues (info 240, success 145, warning 80, error 25); pairs cleanly with cool-leaning neutral tint; rare-in-the-wild for desktop UI accents.
- **O1b — `h = 180` teal.** Calmer; closer to info 240 (Δh = 60°, perceptually distinct but in the same blue-green family); risks reading as "another info."
- **O1c — `h = 340` pink.** Energetic, brand-forward; closer to error 25 (Δh = 45°, perceptually adjacent); risks status-collision.
- **O1d — `h = 20` orange-red.** Rejected at intake — Δh = 5° from error 25; effectively collides.
- **O1e — Swiss-flag red** (h ≈ 0, high C). Rejected at intake — collides with error and overweights a cultural reference at the expense of clarity.

**Question 2.** Which OKLab implementation should the generator depend on?

**Options.**

- **O2a — In-repo Go port (chosen).** ~80 lines of Go implementing OKLab ↔ sRGB and OKLCh ↔ OKLab per Björn Ottosson 2020. Zero external dependency; testable against the published test vectors; bytes-stable across Go versions.
- **O2b — `palette` Rust crate.** Mature, broad-scope; supports many color spaces. Heavyweight dep for ~80 lines of math; cross-language (we need both Go and Rust generators).
- **O2c — `oklab` Rust crate.** Lighter than `palette`; Rust-only.
- **O2d — `colorgrad` Go crate.** Has OKLab support; brings in palette / gradient machinery we don't need.

**Criteria.**

- **C1 — Dependency footprint.** Smallest is best for a generator that produces stable bytes.
- **C2 — Cross-language coverage.** Generator emits both Rust and Go; the same OKLab math must run in both.
- **C3 — Exactness / reproducibility.** Re-running the generator on unchanged input must produce byte-identical output.
- **C4 — Maintainability.** ~80 lines of well-documented math vs. a library upgrade path.
- **C5 — Testability.** Available test vectors against the canonical Ottosson implementation.
- **C6 — Aesthetic alignment.** (Q1 only.) Does the choice feel Swiss-minimalist?
- **C7 — CVD-safety risk.** (Q1 only.) Distance from semantic hues; perceptual proximity under deuteranopia / protanopia / tritanopia.

**Assessment.**

|    | O1a (chosen) | O1b | O1c | O2a (chosen) | O2b | O2c | O2d |
|----|-----|-----|-----|-----|-----|-----|-----|
| C1 |     |     |     | ++  | −   | +   | −   |
| C2 |     |     |     | ++  | −−  | −−  | +   |
| C3 |     |     |     | ++  | +   | +   | +   |
| C4 |     |     |     | +   | ++  | +   | +   |
| C5 |     |     |     | ++  | ++  | +   | +   |
| C6 | ++  | +   | −   |     |     |     |     |
| C7 | ++  | −   | −   |     |     |     |     |

For Q1, O1a dominates O1b on hue-distinctness (further from `info` 240) and O1c on collision risk (further from `error` 25). For Q2, O2a wins on C1 / C2 (the cross-language requirement is the disqualifier for the Rust-only crates; in-repo port handles both Go and Rust generators with shared test vectors).

## Decision

We will:

- Pin the **accent hue at `h = 295`** (violet). The three emphasis levels (subtle / default / strong) follow ADR-0031 §SD2's L / C scheme.
- Implement **OKLab as a small in-repo port** at `scripts/ci/designcolors/gen/oklab/` (Go) and at `rust/imzero2/imzero2_egui/src/style/tokens/oklab.rs` (Rust mirror used by the Rust generator path). The math follows Ottosson 2020 verbatim; test vectors are checked in.
- Commit the **semantic palette OKLCh coordinates** per §SD3 below to `palette.toml`.
- Bundle a **scientific palette subset** per §SD4 — Crameri `batlow` / `vik` / `batlowS` defaults plus selected alternates, and the viridis family (van der Walt & Smith, plus Nuñez `cividis`).
- Wire the **generator + verifier pipelines** per §SD5–§SD7. The generator is byte-deterministic; CI re-runs it and verifies the committed `palette_generated.*` files match.

Concrete sRGB hex values produced by running the generator land as an Amendment to this ADR (the M0 execution result), parallel to [ADR-0028](./0028-chlocal-low-latency-sql-cap.md)'s M0 spike Amendment pattern.

## Subsidiary design decisions

### SD1 — Accent hue: `h = 295` (violet)

The semantic palette anchors four status hues (info 240, success 145, warning 80, error 25) plus a hue-neutral role. Adding `accent` requires a fifth hue that:

- Doesn't collide perceptually with any of the four status hues under CVD simulation.
- Reads as "selection / focus / brand emphasis," not "another status."
- Survives the §SD3 Swiss-restrained chroma target (C ≈ 0.10 default) without looking washed out.

Violet at `h = 295` sits in the underpopulated quadrant of the OKLCh hue wheel (75° from `info` 240, 60° from `error` 25). Under deuteranopia and protanopia simulation, violet remains visually distinct from blue (info) and red (error) — the magenta-leaning hue separates from the blue-green axis that those CVDs collapse.

Concrete OKLCh targets for the three accent emphasis levels (per ADR-0031 §SD2 dark-theme spine):

| Emphasis | L | C | h |
|---|---:|---:|---:|
| `accent.subtle` | 0.20 | 0.03 | 295 |
| `accent.default` | 0.68 | 0.10 | 295 |
| `accent.strong` | 0.82 | 0.12 | 295 |

These are *targets* — the OKLCh→sRGB gamut clipping pass at generator runtime may nudge C downward by up to 0.02 for any point that falls outside the sRGB gamut. The post-clip values land in the M0 execution Amendment.

### SD2 — OKLab implementation: in-repo Go + Rust port

The OKLab math (Ottosson 2020) is ~80 lines of vector operations:

- `linear_srgb_to_oklab(r, g, b) -> (L, a, b)` — three matrix multiplies plus a cube-root.
- `oklab_to_linear_srgb(L, a, b) -> (r, g, b)` — inverse: cube plus three matrix multiplies.
- `oklab_to_oklch(L, a, b) -> (L, C, h)` / inverse — polar conversion.
- `srgb_gamma_encode(linear) -> srgb` / `srgb_gamma_decode(srgb) -> linear` — IEC 61966-2-1.

Locations:

- **Go** (generator path): `scripts/ci/designcolors/gen/oklab/oklab.go` + `oklab_test.go` with vectors from Ottosson's reference blog post.
- **Rust** (mirrored for the Rust-side `palette_generated.rs` emitter): `rust/imzero2/imzero2_egui/src/style/tokens/oklab.rs`. The Rust file is *generated from* the Go source via the same `./generate.sh` codegen entry as the rest of the token cross-language mirroring ([`reference_generate_sh`]).

**Why not the `palette` crate** (O2b): it's Rust-only; we need Go for the generator (per the [ADR-0029 §SD8](./0029-imzero2-design-system-and-policy-as-code.md) tooling-in-Go convention). Splitting the math across two crates risks divergence.

**Why an in-repo port instead of, say, a small standalone library** (O2c / O2d analogues): the math is small enough to vendor in full; pinning an external crate adds an upgrade path we don't need to think about. The Ottosson 2020 reference is stable; a single in-repo port serves both languages once codegen mirrors it.

### SD3 — Semantic palette: 18 colors in OKLCh

Six roles × three emphasis levels. C values are Swiss-restrained (default at 0.10 vs typical bright-UI 0.15) per [ADR-0031 §SD2](./0031-imzero2-design-system-color.md).

| Role | subtle (L, C) | default (L, C) | strong (L, C) | h |
|---|---|---|---|---:|
| `info` | (0.20, 0.03) | (0.68, 0.10) | (0.82, 0.12) | 240 |
| `success` | (0.20, 0.03) | (0.68, 0.10) | (0.82, 0.12) | 145 |
| `warning` | (0.20, 0.03) | (0.68, 0.10) | (0.82, 0.12) | 80 |
| `error` | (0.20, 0.03) | (0.68, 0.10) | (0.82, 0.12) | 25 |
| `neutral` | (0.20, 0.00) | (0.68, 0.00) | (0.82, 0.00) | — |
| `accent` | (0.20, 0.03) | (0.68, 0.10) | (0.82, 0.12) | 295 |

Plus the **dark-theme neutral spine** (ten L points, h = 240, C = 0.005) per [ADR-0031 §SD4](./0031-imzero2-design-system-color.md):

| Token | L | Use |
|---|---:|---|
| `bg.extreme` | 0.06 | Modal scrim base |
| `bg.panel` | 0.13 | Default panel fill |
| `bg.faint` | 0.17 | Alternating rows |
| `bg.surface` | 0.20 | Raised cards |
| `border.faint` | 0.26 | Subtle dividers |
| `border.default` | 0.32 | Standard borders |
| `text.disabled` | 0.50 | Disabled text |
| `text.secondary` | 0.72 | Caption / secondary |
| `text.primary` | 0.90 | Body text |
| `text.extreme` | 0.98 | Display / strong emphasis |

Total: 28 generated `Color32` constants. These are *targets*; gamut-clip and contrast-verifier-driven adjustments may nudge C or L by ≤ 0.02 in either direction. The post-adjustment values land in the M0 execution Amendment.

The complete `palette.toml` schema:

```toml
[meta]
oklab_version = "ottosson-2020"     # Ottosson's reference math
gamut = "srgb"                       # target color space for emission
emit = ["rust", "go", "markdown"]    # generator output formats

[neutral]
hue = 240
chroma = 0.005

[neutral.spine]
bg_extreme       = 0.06
bg_panel         = 0.13
bg_faint         = 0.17
bg_surface       = 0.20
border_faint     = 0.26
border_default   = 0.32
text_disabled    = 0.50
text_secondary   = 0.72
text_primary     = 0.90
text_extreme     = 0.98

[semantic.info]
hue = 240
subtle  = { L = 0.20, C = 0.03 }
default = { L = 0.68, C = 0.10 }
strong  = { L = 0.82, C = 0.12 }

[semantic.success]
hue = 145
# ... same emphasis layout ...

# … one block per role: success / warning / error / accent / neutral ...
```

### SD4 — Scientific palette bundle for v1

Per [ADR-0031 §SD3](./0031-imzero2-design-system-color.md), data-encoding palettes are *vendored verbatim* from upstream publications under their CC0 / MIT licenses. v1 bundle:

| Palette | Family | License | Use |
|---|---|---|---|
| `batlowS` | Crameri | MIT | **Default qualitative** (10 categorical colors) |
| `batlow` | Crameri | MIT | **Default sequential** (256-entry LUT) |
| `vik` | Crameri | MIT | **Default diverging** (256-entry LUT) |
| `lapaz` | Crameri | MIT | Alternate sequential (blue-yellow) |
| `oslo` | Crameri | MIT | Alternate sequential (pure-blue) |
| `lajolla` | Crameri | MIT | Alternate sequential (orange-warm) |
| `roma` | Crameri | MIT | Alternate diverging |
| `broc` | Crameri | MIT | Alternate diverging (green-purple) |
| `cork` | Crameri | MIT | Alternate diverging (teal-pink) |
| `viridis` | van der Walt & Smith | CC0 | Alternate sequential (matplotlib classic) |
| `magma` | van der Walt & Smith | CC0 | Alternate sequential (warm) |
| `plasma` | van der Walt & Smith | CC0 | Alternate sequential (purple-to-yellow) |
| `inferno` | van der Walt & Smith | CC0 | Alternate sequential (dark-to-fire) |
| `cividis` | Nuñez et al. | CC0 | Alternate sequential (CVD-explicit) |

Total: 14 vendored LUTs ≈ 14 × 256 × 3 ≈ 11 KB raw RGB data; ~50 KB after pretty-formatting the Rust + Go source files. Each file carries provenance: source DOI / URL, upstream version, upstream SHA-256.

Crameri version pinning: target the **latest stable Crameri release** as of the M0 execution date; SHA-pinned in the LUT file headers. Future bumps are amendments to this ADR.

Subsetting note: Crameri publishes ~50 palettes; we bundle 9 (defaults + 6 alternates). Adding more is Tier 3 per tier3-human-review.md §T3-008 (deferred to M2 plot-integration testing).

### SD5 — Generator pipeline

`scripts/ci/designcolors/gen/main.go` reads `palette.toml` and emits four artefacts:

1. `rust/imzero2/imzero2_egui/src/style/tokens/palette_generated.rs` — Rust `Color32` constants
2. `public/thestack/imzero2/egui2/styletokens/palette_generated.go` — Go `color.RGBA` (or egui-equivalent) constants
3. `doc/design-system/foundations/color.md` — human-readable spec with hex values + contrast table
4. `doc/design-system/foundations/ip-boundary-check.md` — verbatim-search log (§SD7)

The generator is **byte-deterministic**: same input → same bytes out. CI runs the generator with `--verify` mode that re-emits to a tmpdir and compares byte-for-byte against the committed artefacts; any drift fails the build.

OKLCh → sRGB gamut clipping: when a target OKLCh falls outside the sRGB gamut, the generator reduces C by 0.005 in a loop until in-gamut. L is *never* reduced (preserves contrast guarantees). Any clip is recorded in the generator log and visible in `color.md`.

Vendored scientific palette LUTs are generated by `scripts/ci/designcolors/vendor/main.go` (per [ADR-0031 §SD8](./0031-imzero2-design-system-color.md) Flow 2). The two generators share no state; they emit to `data_encoding/<palette>.{rs,go}` files independently.

### SD6 — Verifier pipeline

Three verifier passes run as part of the generator:

- **`contrast/`** — pair-wise WCAG 2.1 contrast for every (fg, bg) declared in `pairs.toml`. Fails on AA violation; warns on AAA miss. Output included in `color.md` as a table.
- **`cvd/`** — Brettel-Viénot-Mollon CVD simulation. Verifies ΔE > 15 for every pair within the same emphasis level of the semantic palette. Fails on violation. Scientific palettes are *not* re-verified (pre-cleared by publication per [ADR-0031 §SD5](./0031-imzero2-design-system-color.md)).
- **`provenance/`** — verifies upstream SHA-256 of each vendored scientific LUT. Fails on tampering or accidental version-bump.

All three passes run on every generator invocation; the M0 execution Amendment will record the first clean pass.

### SD7 — IP boundary check (per ADR-0031 §SD11)

For every generated semantic-palette hex value, the generator searches verbatim against:

- Adobe Spectrum
- Material 3
- IBM Carbon
- Tailwind CSS (slate / blue / red / etc.)
- Radix Colors
- Fluent UI
- Shopify Polaris
- Bootstrap

Each system's published palette is cached as a JSON file under `scripts/ci/designcolors/ip-refs/<system>.json` — a one-time vendor task pinned by URL + retrieval date. The search is case-insensitive (treats `#A0B0C0` and `a0b0c0` as the same) and checks both standalone hex and named-color collisions.

A collision is *not* a violation by itself — sRGB hex collisions are statistically inevitable across enough systems. The combination of (matching hex) **and** (matching role) triggers a perturbation: the OKLCh source is nudged ΔL = 0.02 in either direction, the sRGB is re-derived, and contrast + CVD checks re-run.

Token *names* are searched against the famous ladders (no `100/200/300`, no `primary/secondary/tertiary`, no `slate-500` shape, no `xs/sm/md/lg/xl` ladder). The boundary log is committed to `doc/design-system/foundations/ip-boundary-check.md`.

### SD8 — Phasing

- **M0a — Design + scaffolding (this ADR's proposed scope).** Land the OKLab Go + Rust port; commit `palette.toml` with the SD3 targets; commit `pairs.toml` with the contrast pairs to verify; commit the generator + verifier source.
- **M0b — Execute + amend (follow-on PR).** Run the generator, capture generated `palette_generated.{rs,go}`, `color.md`, `ip-boundary-check.md`, contrast / CVD verifier logs. Land them in a PR plus an Amendment to this ADR ("2026-MM-DD — M0 execution results: 28 generated colors, N IP-boundary collisions handled, AA contrast pass, CVD ΔE > 15 confirmed"). The Amendment pattern mirrors [ADR-0028](./0028-chlocal-low-latency-sql-cap.md).
- **M1 — Wire into apps** (resumes [ADR-0031 M1](./0031-imzero2-design-system-color.md) phasing). Pilot app (carousel) reads from `palette_generated.rs` via the `apply()` function ([ADR-0031 §SD7](./0031-imzero2-design-system-color.md) binding).

M0a is implementable immediately on acceptance of this ADR; M0b is gated on running the generator; M1 belongs to the parent ADR-0031.

## Alternatives

- **`palette` Rust crate (O2b).** Rejected for cross-language coverage; we need Go for the generator.
- **`oklab` Rust crate (O2c).** Same reason; Rust-only.
- **`colorgrad` Go crate (O2d).** Brings palette / gradient machinery we don't need.
- **Pure hand-picked sRGB.** Rejected at intake per [ADR-0031 §SD1](./0031-imzero2-design-system-color.md) — defeats the OKLCh perceptual-uniformity rationale.
- **Constructing our own viridis-equivalent.** Rejected per [ADR-0031 §Alternatives](./0031-imzero2-design-system-color.md) (O6) — reinvents inferior versions of published scientific colormaps.
- **Teal accent (`h = 180`).** Rejected on C7 — too close to `info` (Δh = 60° on a saturated palette risks reading as "another info").
- **Pink accent (`h = 340`).** Rejected on C7 — Δh = 45° from `error` 25, perceptually adjacent under CVD.
- **Swiss-flag red accent.** Rejected at intake — collides with `error`; overweights cultural reference over clarity.
- **Bundling all ~50 Crameri palettes.** Rejected for v1 — bytes we don't need; Tier 3 to extend per T3-008.
- **OKLCh → sRGB clipping by L reduction.** Rejected — would degrade contrast guarantees. Chroma reduction is the standard fix.
- **Light-theme spine in M0.** Rejected per [ADR-0031](./0031-imzero2-design-system-color.md) (dark only for v1); the framework supports adding it later as a separate spine without changing the OKLab math or the scientific palettes.

## Consequences

### Positive

- **Concrete OKLCh coordinates available for review.** Reviewers can see exact target values before any sRGB rendering happens; coordinates are version-controlled and diffable.
- **OKLab math is self-contained.** No external dependency that could change; ~80 lines reviewable in one sitting; bytes-stable across language toolchain bumps.
- **Cross-language consistency by construction.** Single Ottosson reference implementation; Go and Rust both derive from the same source via codegen.
- **M0 execution is small.** Running the generator + verifiers + IP check should take minutes; gates M1 cleanly.
- **IP boundary cleared with reproducible log.** The verbatim-search log in `ip-boundary-check.md` makes the [ADR-0029 §SD12](./0029-imzero2-design-system-and-policy-as-code.md) compliance audit-friendly.
- **Pending Tier 3 rows resolved.** T3-007 (accent hue) and T3-010 (OKLab impl) close on this ADR's acceptance.

### Negative

- **In-repo OKLab port is one more piece to maintain.** ~80 lines; small but must stay correct. Test vectors from Ottosson are checked in; drift fails CI.
- **Crameri version pinning is now load-bearing.** A bump to a new Crameri release would re-emit all the scientific LUT files; the IP boundary check would re-run. Acceptable cost; bump is a Tier 3 amendment.
- **Target OKLCh values may need adjustment at M0b execution.** Gamut clipping or CVD ΔE failures could nudge values. The Amendment pattern handles this; mitigation is well-understood.
- **The OKLab Go port duplicates the Rust port.** Codegen-mirrored from the Go source, but a contributor could edit one and forget the other. Mitigation: `./generate.sh` re-emits the Rust mirror; CI verifies.
- **Pinning specific Crameri / viridis upstream SHAs means upstream drift fails the build.** A force-push or tag-rewrite on the upstream repo would break the SHA verification. Mitigation: vendor the LUTs as in-repo files, not as a runtime fetch; upstream SHA is recorded for provenance only, not for re-fetch.

### Neutral

- **`palette.toml` is the public-stability surface for semantic palette OKLCh targets.** Renaming a role or shifting a hue requires a fleet sweep. Mitigation: changes flow through Tier 3.
- **Generator artefacts (`palette_generated.{rs,go}`, `color.md`, `ip-boundary-check.md`) are checked in.** Generated bytes are auditable; CI verifies they match the source.
- **The accent hue choice is fleet-wide.** Per-app accent overrides require Tier 3 escalation per tier3-human-review.md §Case classes — *Density-policy exemption*-shaped process for accent overrides if a real case emerges.

## Status

Proposed — awaiting review by @spx.

Open questions:

1. **Crameri release version pin.** v1 bundle targets the latest stable Crameri release at the M0b execution date; the version number lands in the Amendment. Re-asked at every Crameri bump (Tier 3).
2. **`palette.toml` `[meta] oklab_version` field semantics.** Currently `"ottosson-2020"` as a marker. If a future revision of OKLab (Ottosson 2024 or similar) ships and the math changes, the version bumps; existing palette.toml stays valid until a Tier 3 ADR commits to the new math.
3. **Should the OKLab port include the inverse `sRGB → OKLCh` transform?** Useful for the IP boundary check (when reverse-engineering published palettes for collision tests). Defer to M0b — implement if the IP check needs it; skip if a forward-only port suffices.
4. **Generator output formats beyond Rust / Go / markdown.** A Figma / Sketch / Adobe XD export is conceivable but out of scope for IDS v1 (no design-tool integration in scope).
5. **Test-vector source for the OKLab port.** Ottosson's blog post lists a handful; the W3C CSS Color Module Level 4 specification has additional vectors. Pick a canonical set in M0a.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only; supersession is recorded, not deleted.

## Updates

### 2026-05-16 — M0b execution: palette generated, two contract refinements

Closes §SD8 M0b. Lands the in-tree generator + verifiers + LUTs + the executed palette artefacts, plus two contract-level refinements that came out of adversarial review while the work was running.

**What landed verbatim from §SD8 M0b.**

- OKLab Go port at `scripts/ci/designcolors/gen/oklab/` with Rust mirror at `rust/imzero2/imzero2_egui/src/style/tokens/oklab.rs`. Test vectors from Ottosson 2020 + W3C CSS Color 4. Forward + inverse + gamma round-trip + OKLab↔OKLCh round-trip all pass.
- `palette.toml` at `rust/imzero2/imzero2_egui/assets/colors/palette.toml` carrying the §SD3 OKLCh targets (28 tokens: 10-step neutral spine plus 6 roles × 3 emphasis).
- `pairs.toml` at `rust/imzero2/imzero2_egui/assets/colors/pairs.toml` declaring the 12 contrast pairs the verifier reads.
- Generator at `scripts/ci/designcolors/gen/main.go` emits four artefacts deterministically: `palette_generated.{rs,go}`, `color.md`, `ip-boundary-check.md`. CI re-runs and compares byte-for-byte.
- IP-boundary cache at `scripts/ci/designcolors/ip-refs/` with 8 published-system anchor JSONs (Spectrum / Material / Carbon / Tailwind / Radix / Fluent / Polaris / Bootstrap).
- Vendor pipeline at `scripts/ci/designcolors/vendor/main.go` produces 13 scientific palette LUTs under `rust/imzero2/imzero2_egui/src/style/data_encoding/` (Rust) and `public/thestack/imzero2/egui2/styletokens/data_encoding/` (Go).

**Two contract refinements (adversarial-review-driven, mid-flight).**

1. **APCA replaces WCAG 2.1 as the primary contrast metric.** The original §SD6 wired WCAG 2.1's relative-luminance ratios as the gate. Andrew Somers' SAPC-APCA (proposed for WCAG 3.0 / Silver) is the technically-correct primitive for dark-only IDS — WCAG 2.1's `(L1 + 0.05) / (L2 + 0.05)` is known-flawed for dark themes (the +0.05 fudge factor lets dark-on-dark misread as passing). New `scripts/ci/designcolors/gen/apca/` implements Myndex Beta 0.1.9 verbatim with the canonical test vectors. WCAG 2.1 stays as a warn-only secondary check so we can spot APCA/WCAG disagreement during calibration and retain the legacy-compliance signal. Per-pair `font_pt` + `font_weight` were added to `pairs.toml` because APCA's threshold is a function of (size, weight) — semantic stuff like "Body=13pt/400" now carries through the verifier instead of bucketing into "body / large / ui".

2. **CSS Color 4 §13 GMA replaces naive chroma-stepping for gamut mapping.** The original §SD5 specified "chroma reduction in a loop" as the gamut-clip strategy. That introduces quantization (step granularity) and hue drift near the gamut boundary (sRGB isn't a cylinder in OKLCh). New `scripts/ci/designcolors/gen/gma/` implements the CSS Color Module Level 4 §13 bisection algorithm: bisect C, project via per-channel sRGB clipping, accept the largest C whose clipped projection is within ΔE_oklab 0.02 of the unclipped target. Result: provably-closest in-gamut sRGB triple by OKLab Euclidean distance. `palette.go` consumes `gma.MapToSrgbU8` in place of `oklab.OklchToSrgbU8`. L is still never reduced — contrast guarantees preserved.

**Palette adjustments forced by the APCA gate.** APCA's Lc-≥-90 floor for Body=13pt/400 exposed that the §SD3 target values were below the gate. Iterated until clean, all within the ±0.02 perturbation bound the ADR anticipates:

| Token | §SD3 target L | Post-iteration L | Driver |
|---|---|---|---|
| `text_primary` | 0.90 | 0.95 | Lc ≥ 90 for Body at 13pt/400 |
| `text_secondary` | 0.72 | 0.78 | Caption legibility on `bg_panel` |
| `border_default` | 0.32 | 0.58 | Lc ≥ 30 ambient UI on `bg_panel` (initial WCAG-driven bump to 0.55 → APCA-driven re-bump to 0.58) |
| `semantic.{info,success,warning,error,accent,neutral}.default` L | 0.68 | 0.80 | Lc ≥ 60 meaningful UI on `bg_panel`; C 0.10→0.12 |
| `semantic.*.strong` L | 0.82 | 0.90 | Symmetry with default-emphasis bump; C 0.12→0.13 |

One pair-level adjustment: `secondary-on-panel` switched `font_weight` 400 → 500 to align Caption usage with the §SD4 weight-roles table. Two pairs are explicitly exempt from the APCA gate (graded but not gated): `disabled-on-panel` (intentional low-contrast per §SD5) and `secondary-on-panel` (Caption=11pt regular is fundamentally below APCA's body-text floor regardless of color choice; resolving requires a Caption size bump to 12pt or a default to Medium weight — deferred).

**IP-boundary collision handled per §SD7.** The first generator pass surfaced one (hex, role) match: `semantic.neutral.subtle` at `#161616` collided with Carbon `gray-100`. Nudged the OKLCh source L 0.20 → 0.22, which is exactly the ΔL=±0.02 perturbation the ADR specifies. Post-nudge re-emit produces 0 collisions across all 8 cached systems.

**CVD remains advisory at M0b** — not promoted to a gate. The Brettel-Viénot-Mollon simulator runs against the semantic palette and reports 68 same-emphasis pairs with ΔE ≤ 15 in CVD-transformed OKLab (down from 70 before the L bumps). The Swiss-restrained C ≈ 0.12 default emphasis is genuinely below CVD safety for adjacent hues at the same L; auto-perturbation is the path forward but isn't implemented. Tier-2 rubric V2 (color-encoding consistency) is the load-bearing backstop today — "color is never the sole encoding channel" stays the rule, and apps are expected to add icon + label to every status surface.

**Cividis omitted from the v1 scientific-palette bundle.** §SD4 listed 14 palettes; the executed bundle ships 13 (Crameri batlow / vik / batlowS / lapaz / oslo / lajolla / roma / broc / cork via cmcrameri upstream txt; viridis / magma / plasma / inferno via `github.com/dim13/colormap` v1.1.0). Cividis (Nuñez et al. 2018 CC0) is neither in `cmcrameri` nor in `dim13/colormap`. Vendoring from matplotlib's hard-coded `_cm_listed.py` is ~10 minutes of work and lands in a follow-up — flagged because cividis is the only sequential palette in §SD3's design space that was *optimised* for CVD-primary use; without it, the "CVD-explicit alternate" the ADR anticipates does not exist.

**Per-package SSIM primitive landed.** While the primary M0b deliverables were running, an adversarial-review point on Tier-2 cost discipline landed a deterministic structural-similarity primitive at `scripts/ci/designreview/ssim/`. Not consumed by anything yet (the §M4 Tier 2 driver isn't built); but the math is in-tree so the LLM pre-filter the driver will need is ready. CLI at `scripts/ci/designreview/ssim/cmd/ssim` runs ad-hoc image comparison: `ssim a.png b.png --threshold=0.99`.

**Closes Tier 3 pending rows.** T3-007 (accent hue, h=295) and T3-010 (in-repo OKLab port) resolved as the ADR proposed. T3-008 (Crameri subset) partial — 9 Crameri palettes shipped; further additions remain Tier 3.

`status` stays `proposed` per the ADR convention. This amendment refines execution detail and records two contract-level upgrades; the human-review flip to `accepted` is owed.

### 2026-05-17 — Accent refinement: hue 295 → 270, default chroma 0.12 → 0.08

Follow-on to the [ADR-0037](./0037-imzero2-design-system-palette-m1-refinement.md) UX investigation. Two related token adjustments inside the existing `semantic.accent` role; no new tokens, no other roles touched.

**Change 1 — accent hue 295 → 270 (blue-violet).** §SD1 of the M0a body committed `h = 295` (pure violet) for "selection / focus / brand emphasis", picking the "underpopulated quadrant of the OKLCh hue wheel". Iterative screenshot review on an uncalibrated desktop revealed the violet read as more decorative than the Swiss-restrained data-density framing in [ADR-0029 §Context](./0029-imzero2-design-system-and-policy-as-code.md) calls for. Investigation:

| Hue tested | sRGB at L=0.80, C=0.12 | Character | CVD advisories |
|---:|---|---|---:|
| 270 | `#A0BAFF` | Blue-violet / periwinkle — restrained, professional | **66** (best) |
| 295 (M0b) | `#C3AEFF` | Pure violet — neutral middle | 68 |
| 320 | `#E0A4EE` | Magenta-pink — energetic, consumer-software register | 68 |
| 250 (with info shifted to 210) | `#80C3FF` | Pure sky-blue — lost violet character entirely | 69 |

The h=250 experiment surfaced an OKLCh perceptual-mapping finding worth recording: **the "violet" perceptual category starts around h=275-285, not h=250**. Below ~h=275 the hue reads as blue regardless of L/C; coordinated moves of `info` away from blue cannot fix this because the perceptual categories are wider than the OKLCh hue degrees suggest. h=270 is the *minimum* hue that still reads as violet rather than blue — picking lower hues forces the accent to either compete with info (kept at h=240) or to require relocating info to cyan/teal territory (significant fleet impact, rejected at intake).

h=270 also marginally improves CVD distinction (66 advisories vs 68 at h=295) — the cooler blue-violet stays slightly further from the warm semantic hues (warning, error) under deuteranopia simulation.

**Change 2 — accent.default chroma 0.12 → 0.08 (muted).** §SD3's M0 table specified `default = { L=0.80, C=0.12 }` uniformly across all six roles (info / success / warning / error / neutral / accent). The "all roles at C=0.12" symmetry made accent visually compete with status hues — both registered at the same saturation level despite carrying very different semantic weight ("this is selected" vs "this is failing").

The refinement breaks the symmetry: accent.default drops to `C = 0.08` while info / success / warning / error stay at `C = 0.12`. Result: accent reads as "supporting emphasis" rather than as a sixth status color. Matches the register Linear, Notion, and recent macOS accent colors operate in.

The C=0.08 pick was tested at three points (0.12 baseline / 0.10 marginally-muted / 0.08 visibly-muted). At C=0.10 the perceptual delta from 0.12 sat below most viewers' just-noticeable-difference threshold on calibrated displays (≈4 RGB units); C=0.08 produced the first clearly-visible muting (~10 RGB units). C=0.05 was projected as "approaching grey-blue" — out of scope for an accent role; not tested.

`accent.subtle` (C=0.03) and `accent.strong` (C=0.13) are **unchanged**. The emphasis ladder now reads `subtle (C=0.03) < default (C=0.08) < strong (C=0.13)` — the default is intentionally closer to subtle than to strong, reflecting the muting intent.

**Verifier results after both changes.** 28 tokens regenerated. 13 APCA pairs gated cleanly (the `body-on-selection` pair added in the post-ADR-0037 fix is the 13th). 0 WCAG warnings. 0 IP-boundary collisions across all 8 cached published-system palettes. 68 CVD advisories (matching the M0b baseline — h=270 saved 2 advisories, C=0.08 added 2 back as the accent moved closer to neutral grey under deuteranopia).

**Updated `palette.toml` for `[semantic.accent]`:**

```toml
[semantic.accent]
hue     = 270.0                        # was 295
subtle  = { L = 0.20, C = 0.03 }       # unchanged
default = { L = 0.80, C = 0.08 }       # C was 0.12
strong  = { L = 0.90, C = 0.13 }       # unchanged
```

**Why this is recorded as an Amendment, not a new ADR.** The change stays within the OKLCh framework, the role taxonomy, the emphasis-level structure, and the APCA-as-primary-gate contract — all of which ADR-0033 commits to. No tokens are added or removed; no new pair classes appear; the generator pipeline is unchanged. The change is a *refinement of two values inside the existing role*, fitting the tier3-human-review.md "Foundation refinement" case class and the M0b amendment precedent.

**Closes T3-007 (accent hue final pick) — revised.** The original M0a resolution of T3-007 at h=295 stands as the ADR's first answer; this amendment supersedes it with h=270 + C=0.08 based on real-world UX feedback. The pending-decisions table flips T3-007 from "resolved at h=295" to "amended to h=270 + accent.default C=0.08" with this ADR's date.

**Screenshot evidence.** The full screenshot tour at the new accent values is captured in `doc/screenshots/tour/` (37 PNGs as of the bundle that includes `idsshowcase.png`). The `idsshowcase.png` capture is the single-image proof: the accent row in the semantic-palette grid visibly reads quieter than info/success/warning/error. Earlier iterations preserved in `/tmp/tour-accent-*` were not committed (transient calibration evidence per the ADR-0037 §SD6 policy).

`status` stays `proposed` per the ADR convention; the M0a + 2026-05-16 + 2026-05-17 amendment chain is owed a single review pass.

### 2026-07-17 — batlowS qualitative subset corrected: even resampling → upstream first-10

**Defect.** The vendor pipeline derived the 10-entry `batlow_s` LUT by *evenly resampling* Crameri's 100-entry `batlowS.txt` (rows 0, 11, 22, …, 99). batlowS is **prefix-ordered by categorical distinctness** — the first N rows *are* the intended N-color qualitative palette, and later rows progressively subdivide the batlow ramp, converging on earlier entries. Even resampling therefore lands in fine-subdivision territory and produced near-duplicate cycle entries: pairs (0, 8) at ~3.7, (1, 9) at ~4.6, and (2, 5) at ~7.5 RGB units apart — far below any just-noticeable difference. In consumers that color adjacent categorical entities from `QualitativeCycle` (treemap cells, series colors, category chips), distinct categories rendered visually identical, silently breaking §SD4's "Default qualitative (10 categorical colors)" contract.

**Fix.** The vendor converter now truncates to the upstream **first 10 rows** (`#011959`, `#faccfa`, `#828231`, `#226061`, `#f19d6b`, `#4d734d`, `#114360`, `#fdb4b4`, `#c09036`, `#175262`; minimum pairwise distance ≈16 RGB units, ≈34 among the first 9) and the emitted provenance line reads "first-10 subset per ADR-0033 §SD4". Entry 0 (batlow's navy anchor) is unchanged; entries 1–9 all change. Both generated artefacts (Go `styletokens/data_encoding/batlow_s.go` and Rust `data_encoding/batlow_s.rs`) were regenerated together. A regression test (`TestQualitativeCyclePairwiseDistinct`) now enforces a squared-distance floor over every cycle pair, so a resampling regression fails CI rather than shipping.

**Pipeline repairs in the same pass** (the vendor tool had been unrunnable since the repository restructure): upstream/emit paths repointed `src/rust/…` → `rust/imzero2/…`; generated-file headers now name the real invocation (`./boxer.sh designsystem colors vendor`); the Rust emitter now writes rustfmt-stable output (unpadded tuples, name-sorted `mod.rs` declarations matching rustfmt's `reorder_modules`/`reorder_imports`), verified with `rustfmt --check` over every emitted `.rs` file so `cargo fmt` no longer drifts the artefacts. Non-`batlow_s` palette *data* is byte-identical to the previous vendoring (headers only).

**Why this is an Amendment, not a new ADR.** §SD4 already commits to vendoring batlowS verbatim as the 10-color qualitative default; the even-resampling was an implementation error against that commitment, not a decision. No palette is added or removed, no consumer API changes; downstream surfaces simply regain the distinctness §SD4 always promised.

## References

- [ADR-0029 — design system + policy-as-code](./0029-imzero2-design-system-and-policy-as-code.md) — §SD12 IP boundary check cross-cuts §SD7 here.
- [ADR-0030 — typography](./0030-imzero2-design-system-typography.md) — sibling foundations ADR; build-pipeline pattern (TOML source-of-truth + generator + committed artefacts) parallels §SD5 here.
- [ADR-0031 — color foundations](./0031-imzero2-design-system-color.md) — parent ADR; §SD13 names M0 deliverables; §SD1 OKLCh; §SD2 semantic palette; §SD3 scientific palettes; §SD7 generator pipeline; §SD11 IP boundary.
- [ADR-0028 — `ch.local.exec` low-latency cap](./0028-chlocal-low-latency-sql-cap.md) — M0 spike + Amendment pattern reused here.
- tier3-human-review.md — resolves T3-007 (accent hue) + T3-010 (OKLab impl); T3-008 (Crameri subset) partially resolved (defaults committed; alternates list pending M2).
- INSPIRATIONS.md — attributions for Crameri (MIT), viridis (CC0), cividis (CC0), Ottosson (OKLab).
- Crameri, F. (2018). *Scientific colour maps* (Version 8.0.1) [Zenodo](https://doi.org/10.5281/zenodo.1243862) — Crameri MIT family.
- van der Walt, S. & Smith, N. (2015). *Default colors for matplotlib (the viridis family).* [bids.github.io/colormap](https://bids.github.io/colormap/) — viridis CC0.
- Nuñez, J. R., Anderton, C. R., & Renslow, R. S. (2018). *Optimizing colormaps with consideration for color vision deficiency.* PLOS ONE 13(7): e0199239 — cividis CC0.
- Björn Ottosson (2020). *A perceptual color space for image processing.* [bottosson.github.io/posts/oklab](https://bottosson.github.io/posts/oklab/) — OKLab math.
- [W3C CSS Color Module Level 4](https://www.w3.org/TR/css-color-4/) — additional OKLab test vectors.
- Brettel, H., Viénot, F., & Mollon, J. D. (1997). *Computerized simulation of color appearance for dichromats.* JOSA A 14(10), 2647–2655 — CVD method for §SD6.
- WCAG 2.1 SC 1.4.3 / 1.4.6 — contrast targets for §SD6.
