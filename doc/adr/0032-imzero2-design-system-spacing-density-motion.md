---
type: adr
status: proposed
date: 2026-05-14
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0032: ImZero2 design system — spacing, density, and motion

## Context

[ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD3 (density modes), §SD6 (spacing, rounding, stroke widths), and §SD7 (motion) reserve concrete values for foundations sub-ADRs. This sub-ADR makes them concrete in one combined document because the three concerns are tightly coupled at the token-API level (density scales spacing) and small enough individually that three separate ADRs would carry more boilerplate than content.

Use-case forces (recap):

- **Data-intensive surface.** Dense tables, plots, dashboards; spacing must compose with information density, not compete with it.
- **Swiss-minimalist aesthetic** ([ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) Context fourth force). Grid discipline, restraint, generous whitespace within the constraints of density, hierarchy carried by spacing and type-weight rather than ornament. Motion is functional, not expressive.
- **Performance non-negotiable.** All values are `const`; density resolution happens once at startup; no per-frame computation.
- **Accessibility.** OS-level reduced-motion preferences must be respected; WCAG 2.1 SC 2.3.3 (animation from interactions) is the floor.

Token count is small enough — ~30 distinct values across spacing, rounding, stroke, motion — that a TOML→generator pipeline (the pattern used by colour in [ADR-0031](./0031-imzero2-design-system-color.md) and font build in [ADR-0030](./0030-imzero2-design-system-typography.md)) adds friction without reducing review burden. These foundations live as direct source constants.

## Design space (compact)

The big-shape choices in [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) are settled (2 px base spacing, 3 density modes, 3 motion durations, egui-native animations). What remains:

**Q1 — How does density scale the spacing tokens?**

- **O1 (chosen) — Per-density independent ladders.** Tight, Standard, Roomy each have their own 8-value ladder; tokens look up the active density's column.
- O2 — Multiplicative factor on a single ladder (e.g., Tight × 0.75). Risks off-grid pixel values; rounding to 2 px produces the same effective result as O1 for most values.
- O3 — Tokens are absolute regardless of density (density only affects type sizes). Defeats the purpose for data-density work.

**Q2 — Naming style for spacing tokens?**

- **O1 (chosen) — Purpose-based public API** (`Padding.Default`, `Gap.Items`) on top of a numeric magnitude ladder (`Px[0]`..`Px[7]`).
- O2 — Numeric only (`Spacing.Px8`, `Spacing.Px12`, …). Minimal but the call site loses semantic intent.
- O3 — T-shirt sizes (`Spacing.Sm`, `Spacing.Md`, …). Common in other systems but the size↔value mapping needs memorisation and the naming carries no IDS character.

**Q3 — Rounding and stroke responsiveness to density?**

- **O1 (chosen) — Not density-responsive.** Rounding is aesthetic identity; strokes are perceptual constants (≥ 1 px or they vanish). Density doesn't sensibly apply.
- O2 — Density-responsive. Tight gets smaller rounding and 1 px strokes; Roomy gets larger. Adds complexity without payoff.

**Q4 — Reduced-motion behavior?**

- **O1 (chosen) — Respect OS preference; all motion durations resolve to 0 when set.** Accessibility-first; one flag at startup; cheap.
- O2 — Ignore OS preference; expose IDS-level config only. Loses accessibility.
- O3 — Reduce motion to half durations. Less common pattern; harder to reason about; accessibility advocates argue for instant transitions over half-speed.

## Decision

We commit to:

- **Density model**: 3 presets (Tight / Standard / Roomy) with independent on-grid spacing ladders per density (§SD1).
- **Spacing**: 8-value magnitude ladder per density (§SD2 table) with purpose-based public tokens (§SD2 named list).
- **Rounding**: 4-value scale (0 / 2 / 4 / 6 px), density-independent (§SD3).
- **Stroke widths**: 3 values (1.0 / 1.5 / 2.0 px), density-independent (§SD4).
- **Motion**: 3 duration tokens (Quick 80 ms, Standard 160 ms, Slow 320 ms), egui-native easing, OS reduced-motion-aware (§SD5).

All values are `const` in Rust and `const`-equivalent in Go (§SD6); the Go side is codegen-mirrored via `./generate.sh` ([reference_generate_sh]).

## Subsidiary design decisions

### SD1 — Density model

Three presets, set once at app startup (per [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD3):

```rust
pub enum Density {
    Tight,
    Standard,
    Roomy,
}
```

Active preset comes from per-user config `$XDG_CONFIG_HOME/imzero2/density.toml` or the `IMZERO2_DENSITY` env var; default `Standard`. The active preset applies fleet-wide for the app — mixed-density screens within one app are a Tier 2 rubric V4 finding ([ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD9).

Density affects:

- **Spacing tokens** (§SD2) — each purpose-based token resolves to a different magnitude per density
- **Type sizes** (per [ADR-0030](./0030-imzero2-design-system-typography.md) §SD3) — ±1 pt around Standard

Density does *not* affect:

- **Rounding** (§SD3) — aesthetic identity, density-independent
- **Stroke widths** (§SD4) — perceptual constants, density-independent
- **Motion durations** (§SD5) — temporal, not spatial

### SD2 — Spacing scale and density-resolved tokens

**Magnitude ladder** — 8 values per density, all multiples of 2 px (the 2 px grid invariant from [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD6):

| Index | Tight | Standard | Roomy |
|-------|------:|---------:|------:|
| Px[0] | 2     | 2        | 4     |
| Px[1] | 2     | 4        | 6     |
| Px[2] | 4     | 6        | 8     |
| Px[3] | 6     | 8        | 12    |
| Px[4] | 8     | 12       | 16    |
| Px[5] | 12    | 16       | 24    |
| Px[6] | 16    | 24       | 32    |
| Px[7] | 24    | 32       | 48    |

Tight collapses the smallest values toward the 2 px minimum; Roomy extends the largest to 48 px (off Standard's 32 px ceiling). All values stay on the 2 px grid.

**Purpose-based public tokens** — each names a *use*, not a magnitude; the value comes from the active density's column at the indicated index:

| Token | Index | Standard value | Use |
|-------|------:|---------------:|-----|
| `Padding.Hair` | 0 | 2  | Hairline padding (tight inline content) |
| `Padding.Inner` | 1 | 4  | Inside small widgets (button text padding, badge interior) |
| `Padding.Tight` | 2 | 6  | Tight container padding |
| `Padding.Default` | 3 | 8  | Default control padding, inline gaps |
| `Padding.Outer` | 4 | 12 | Panel inner padding, card content |
| `Padding.Loose` | 5 | 16 | Generous panel padding, dialog content |
| `Gap.Inline` | 2 | 6  | Between inline items (chip stacks, label clusters) |
| `Gap.Items` | 3 | 8  | Between list items (table rows, menu items) |
| `Gap.Sections` | 5 | 16 | Between major sections within a panel |
| `Gap.Panels` | 6 | 24 | Between panels at the layout level |
| `Margin.Frame` | 6 | 24 | Outside panel margins (panel-to-window-edge) |

Apps reach for `Padding.Default` (resolves to 8 px in Standard, 6 px in Tight, 12 px in Roomy) rather than typing `8.0` literally. [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD8 Tier 1 lint L3 flags literal floats in spacing positions outside the token module.

**Grid alignment rule** (doc rule, not Tier 1): all positions and sizes are multiples of 2 px. Top-level layout placement (panel positions, major divider lines) uses multiples of `Padding.Default` (8 px in Standard density) for major alignment. Tier 2 rubric V9 (new — proposed in §Status Q3) verifies grid alignment visually. Tier 1 mechanical enforcement is heuristic-heavy and false-positive-prone for runtime-computed positions; we keep this as a doc rule + rubric.

### SD3 — Rounding scale

Four levels, density-independent:

| Token | Value (px) | Use |
|-------|-----------:|-----|
| `Rounding.None` | 0 | Sharp corners — the Swiss default for most surfaces |
| `Rounding.Sm`   | 2 | Subtle softening — buttons, badges, inline chips |
| `Rounding.Md`   | 4 | Cards, dialogs, panels |
| `Rounding.Lg`   | 6 | Floating windows, modals, popovers |

The Swiss-minimalist default leans toward `Rounding.None` and `Rounding.Sm`. Larger rounding suggests softness inconsistent with the design intent; `Rounding.Lg` is reserved for the few floating-window cases where the rounded edge serves a functional purpose (visual separation from screen edges, distinct elevation tier).

### SD4 — Stroke widths

Three values, density-independent:

| Token | Value (px) | Use |
|-------|-----------:|-----|
| `Stroke.Hair`    | 1.0 | Subtle dividers, table grid lines, faint borders |
| `Stroke.Regular` | 1.5 | Standard borders, panel outlines, control borders |
| `Stroke.Strong`  | 2.0 | Focus rings, active-state outlines, emphasised dividers |

Hairline strokes (1 px) are the most common in Swiss-aligned interfaces — they carry separation without visual weight. The 1.5 px regular is the typical UI border; 2 px strong is reserved for state indication and focus.

**Sub-pixel rendering caveat.** 1.5 px strokes on non-fractional DPI displays may render unevenly. Mitigation: egui's stroke rendering interpolates; the visual character is acceptable across typical desktop DPIs (96–192). If a real artefact surfaces in M1 testing on a real display (not just synthetic captures), `Stroke.Regular` drops to 1.0 px with `Stroke.Strong` carrying both the regular-border and emphasis roles. Trigger format mirrors [ADR-0030](./0030-imzero2-design-system-typography.md) §SD10 (artefacts on ≥ 2 OS/DPI combinations).

### SD5 — Motion: durations, easing, reduced-motion

**Three duration tokens:**

| Token | Duration | Use |
|-------|---------:|-----|
| `Motion.Quick`    |  80 ms | State changes that should feel instantaneous (hover, focus, button-press feedback) |
| `Motion.Standard` | 160 ms | Default transitions (panel open/close, menu expand, DragValue finalise) |
| `Motion.Slow`     | 320 ms | Deliberate transitions (modal entrance, drawer slide, page-level state change) |

**Easing.** Limited to what egui exposes via `Context::animate_bool` and the `emath::easing` module. We do *not* override the default; we do *not* import a Bezier or spring-physics library. Swiss-minimalist motion is functional ("this thing is now in that state"), not expressive ("this thing wants you to notice it").

**Tour-budget constraint.** Any animation must complete within the screenshot tour's 4-frame budget *or* be pre-configured to a final state via mechanisms like `CollapsingHeader::DefaultOpen(true)` ([feedback_collapsingheader_tour]). `Motion.Slow` (320 ms ≈ 19 frames at 60 Hz) is *not* tour-friendly when triggered mid-tour; affected components use the DefaultOpen-style preset for tour captures. The pattern doc `foundations/motion.md` (M2) catalogues which mechanism applies per widget; Tier 2 rubric V8 verifies that tour outputs don't show mid-animation frames.

**Reduced-motion preference.** Respected from OS-level setting: Linux `gtk-enable-animations=false` (DBus or GSettings read); macOS `NSReduceMotion` (CoreFoundation read); Windows `SystemParametersInfo(SPI_GETCLIENTAREAANIMATION, …)`. When reduced-motion is active, all `Motion.*` durations resolve to `Duration::ZERO` — animations skip to final state instantly. Accessibility commitment (WCAG 2.1 SC 2.3.3).

Implementation: single `MOTION_ENABLED: AtomicBool` set once at startup from the OS detection; token accessors return `Duration::ZERO` on disabled. Relaxed atomic load is cheap; the flag is effectively immutable post-init. No per-frame check beyond the duration-fetch call.

The tour script automatically sets `IMZERO2_MOTION=off` for conformance-mode captures (per [ADR-0030](./0030-imzero2-design-system-typography.md) §SD11 conformance/branding split), so tour outputs are deterministic regardless of OS state. Branding-mode captures may run with motion enabled if the maintainer wants animated promo assets.

### SD6 — Token API surface

**Rust:**

```rust
// Density-independent constants.
pub const ROUNDING_NONE: f32 = 0.0;
pub const ROUNDING_SM:   f32 = 2.0;
pub const ROUNDING_MD:   f32 = 4.0;
pub const ROUNDING_LG:   f32 = 6.0;

pub const STROKE_HAIR:    f32 = 1.0;
pub const STROKE_REGULAR: f32 = 1.5;
pub const STROKE_STRONG:  f32 = 2.0;

pub const MOTION_QUICK_MS:    u32 = 80;
pub const MOTION_STANDARD_MS: u32 = 160;
pub const MOTION_SLOW_MS:     u32 = 320;

// Density-resolved spacing — table-lookup accessors:
pub fn padding_default(d: Density) -> f32 { /* PX_TABLE[3][d as usize] */ }
pub fn padding_outer  (d: Density) -> f32 { /* PX_TABLE[4][d as usize] */ }
pub fn gap_items      (d: Density) -> f32 { /* PX_TABLE[3][d as usize] */ }
// … one accessor per purpose token in §SD2.

// Motion accessor with reduced-motion check:
static MOTION_ENABLED: AtomicBool = AtomicBool::new(true);

pub fn motion_quick() -> Duration {
    if MOTION_ENABLED.load(Ordering::Relaxed) {
        Duration::from_millis(MOTION_QUICK_MS as u64)
    } else {
        Duration::ZERO
    }
}
```

**Go-side mirror** via `egui2/styletokens` codegen ([reference_generate_sh]):

```go
const (
    RoundingNone float32 = 0.0
    RoundingSm   float32 = 2.0
    // …
)

func PaddingDefault(d DensityE) float32 { /* table lookup */ }
```

No TOML pipeline; the constants live directly in source. Updates flow through normal PR review.

### SD7 — Binding to `egui::Style` / `Spacing`

The IDS `apply(visuals, spacing, density)` function writes the spacing scale into `egui::Spacing` and related fields. Abbreviated mapping:

| IDS token | `egui::Spacing` / `Style` field |
|-----------|--------------------------------|
| `padding_default(d)` | `button_padding`, related interact dimensions |
| `padding_outer(d)`   | `window_margin.left/right/top/bottom` |
| `padding_inner(d)`   | combo / slider / drag-value internal padding (derived) |
| `gap_items(d)`       | `item_spacing.x`, `item_spacing.y` |
| `gap_inline(d)`      | `icon_spacing` |
| `ROUNDING_MD`        | `window_rounding`, `menu_rounding` |
| `ROUNDING_SM`        | `button_rounding` (egui ≥ 0.34) |
| `STROKE_HAIR`        | most divider / faint-border strokes |
| `STROKE_REGULAR`     | `window_stroke.width` |
| `STROKE_STRONG`      | `selection.stroke.width` (focus rings) |

The full mapping is M1's deliverable in `policy/spacing-egui-binding.md`. Apps that need to deviate (e.g., a custom dialog wants a tighter `window_margin`) escalate via Tier 3.

### SD8 — Source of truth: direct source constants

No TOML pipeline. The ~30 values across spacing / rounding / stroke / motion live directly in Rust source under `rust/imzero2/imzero2_egui/src/style/tokens/`:

```
tokens/
├── density.rs      # Density enum + active-preset reader
├── spacing.rs      # PX_TABLE (3×8) + purpose-based accessors
├── rounding.rs     # 4 const + apply helper
├── stroke.rs       # 3 const + apply helper
└── motion.rs       # 3 const + reduced-motion flag + duration accessors
```

Go-side mirror at `public/thestack/imzero2/egui2/styletokens/`; codegen-mirrored.

Justification (vs. colour's TOML pipeline): the value count is small (~30) and the values are integer / fixed-point — there is no perceptual-space math at design time analogous to OKLCh→sRGB gamut mapping. Direct constants are simpler to review and require no generator.

### SD9 — Phasing

- **M0 — Commit token values + egui binding.** Land the const tables and the §SD7 binding in Rust + Go. Pilot app (carousel demo) uses the tokens. Density resolution at startup; no reduced-motion handling yet.
- **M1 — Density preset wiring.** Per-user config + env-var override; verify visual results in carousel demo across Tight / Standard / Roomy.
- **M2 — Pattern docs.** `foundations/spacing.md`, `foundations/density.md`, `foundations/motion.md`. Document the grid-alignment doc-rule, the tour-budget constraint for motion, and per-widget motion-mechanism catalogue.
- **M3 — Reduced-motion plumbing.** OS detection across Linux / macOS / Windows; wire `MOTION_ENABLED` flag; verify behaviour with OS toggle.
- **M4 — Tier 1 lint L3 graduation.** Move L3 from warn to error per-app as backfill completes ([ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD8); one follow-on PR per app.

Each milestone is independently shippable.

## Alternatives

- **Multiplicative density factor** (Tight × 0.75 / Roomy × 1.5). Rejected on Q1: produces off-grid pixel values; rounding to 2 px collapses most rows to the same effective result as the per-density ladder. Per-density ladders are explicit and easier to reason about.
- **Density-responsive rounding and strokes.** Rejected on Q3: rounding is aesthetic identity; strokes are perceptual constants. Density only affects spacing and type.
- **Custom easing curves (cubic-bezier, spring physics).** Considered for richer motion feel. Rejected: defeats Swiss-minimalist restraint; egui's built-in easing is sufficient; performance and dependency cost rejected.
- **IDS-level motion opt-out only (ignore OS setting).** Rejected on Q4: OS-level accessibility settings are authoritative; an IDS-level opt-in *increase* on top of OS-disabled is also rejected (a user who disables motion at the OS level should not have apps re-enable it).
- **Single combined "size" token covering padding + gap + margin.** Considered for simplicity. Rejected: padding-inside vs. gap-between vs. margin-outside are distinct concerns at the call site; collapsing them costs more clarity than it saves.
- **Per-app density preset (vs. fleet-wide).** Per [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD3, density is set once per app, fleet-wide; we honour that here.
- **TOML pipeline like the colour or font ADRs.** Considered for consistency. Rejected: 30 values is too small to justify the friction; direct source constants are reviewable in PRs without indirection.
- **Per-screen density override within one app.** Considered for cases like "help dialog should always be Roomy." Rejected for v1; Tier 3 escalation if a real case emerges in M4 backfill.
- **A formal column-grid abstraction (12-column or 16-column layout grid).** Considered as a Swiss-aesthetic anchor. Rejected: egui's immediate-mode layout doesn't fit the retained-tree assumptions of CSS-style grid systems; apps that want explicit column-grid layout can compose on top of spacing tokens. Tier 3 ADR can introduce a `Grid` token system if a real need surfaces.

## Consequences

### Positive

- **Density modes work uniformly.** A single switch at startup re-resolves all spacing tokens; apps written without density awareness still benefit because the tokens drive everything.
- **Performance invariant intact.** All values are `const`; density resolution is a table lookup once at startup; reduced-motion is one atomic-bool check at token-accessor call.
- **Swiss-restrained motion.** Three durations, simple easing, accessibility-honoured. Marketing line: "we don't animate to delight; we animate to communicate state."
- **Lint-enforceable.** Tier 1 L3's rule set has concrete values to flag against; backfill is mechanical.
- **No TOML pipeline overhead.** Value count is small; direct source constants suffice; reviews are immediate-mode.
- **Tier 2 rubric reuse.** V4 (density consistency) and V8 (animation feel) already exist; V9 (grid alignment) is the only proposed addition.

### Negative

- **Density ladders are hand-tuned per preset.** Tight and Roomy are not algorithmic transformations of Standard; future scale changes require updating three columns. Mitigation: the ladder is small (8 × 3 = 24 numbers) and reviewable in a single table.
- **Reduced-motion adds an OS-detection dependency.** Three platform-specific code paths (Linux DBus/GSettings, macOS CoreFoundation, Windows `SystemParametersInfo`). Mitigation: detection is one-time at startup; failure falls back to motion-enabled.
- **No formal grid abstraction in v1.** Apps that want explicit column-grid layout must build it on top of spacing tokens. Acceptable for v1; Tier 3 ADR if a real use case emerges.
- **`Stroke.Regular` and `Density::Standard` share the word "Regular/Standard" in different scopes.** The type system disambiguates; prose authors must be explicit. Same applies to `Motion::Standard`.

### Neutral

- **Token names are public-stability.** Renaming `Padding.Default` to `Padding.Base` would be a fleet sweep. Mitigation: settle naming in M0 before broader adoption.
- **Reduced-motion flag is fleet-wide per session.** Apps can't selectively re-enable a specific animation when the user has disabled motion. By design.
- **Tour conformance mode auto-disables motion.** Branding mode keeps motion enabled if the maintainer wants animated promo assets. ([ADR-0030](./0030-imzero2-design-system-typography.md) §SD11 split applies here too.)

## Status

Proposed — awaiting review by @spx. M0 (token values + egui binding) can begin immediately on acceptance.

Open questions:

1. **Stroke 1.5 px sub-pixel rendering.** §SD4 anticipates possible artefacts on integer-DPI displays; M1 testing will confirm. Trigger format mirrors [ADR-0030](./0030-imzero2-design-system-typography.md) §SD10 (artefacts on ≥ 2 OS/DPI combinations).
2. **Tour-default motion.** Confirmed in §SD5: conformance mode auto-disables; branding mode honours. Open question Q2 is whether to make this also user-configurable for branding (e.g., a `--motion-fps=30` for slowed-down promo captures). Defer; out of scope for v1.
3. **Tier 2 rubric V9 (grid alignment).** Proposed new rubric: "are all explicit panel positions and divider lines multiples of 2 px / 8 px?" Heuristic; LLM-graded. Adds to the rubric set in [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD9 if accepted; deferred to M4 of [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md).
4. **Magnitude ladder extension.** Roomy `Px[7]` = 48 extends past Standard's 32. If real consumers need values > 32 in Standard or > 48 in Roomy, extend the ladder consistently rather than introducing one-off custom values. M2 backfill will surface real need.
5. **Density per-screen override.** A few use cases (help dialog wants Roomy even in Tight-default app; a "compact mode" sub-panel inside a Roomy parent) might want this. Out of scope for v1; Tier 3 escalation if a real case emerges.
6. **Reduced-motion override.** Should there be a user-config opt-in to *force* motion despite OS preference (`IMZERO2_MOTION=force`)? Accessibility position: no — OS preference is authoritative. The maintainer can use branding mode for animated promo assets without overriding the runtime invariant.
7. **OS-detection crate selection.** Rust: a single cross-platform "OS animation preference" crate, or three platform-specific bindings? `dark-light` does similar work for light/dark detection and could be a model. Defer to M3.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only.

## Updates

### 2026-05-16 — Window-margin tightening + window-rounding drift recovery

Two related refinements to the §SD7 binding driven by real-world UX feedback on uncalibrated desktop screens: the M1 implementation's window chrome read as "large + round" — the title-bar surround felt generously padded and the window corners felt softer than the Swiss-minimalist intent of [§SD3](#sd3--rounding-scale).

**1. `window_margin` mapping refinement.** The §SD7 binding table originally mapped `padding_outer(d) → window_margin.left/right/top/bottom`. At Standard density `padding_outer` resolves to 12 px, which wraps each window in 12 px of inner padding on all four sides — including above and below the title-bar baseline. Real-world feedback flagged this as "the title bar feels boxed in by extra whitespace below it."

Refinement: bind `window_margin` to `padding_default(d)` instead (8 px at Standard, 6 px at Tight, 12 px at Roomy). The title-bar surround tightens by 4 px top + 4 px bottom = 8 px of vertical content reclaimed per window. At Standard density this is enough to reveal one additional content row in a typical screenshot-tour capture.

The §SD7 table row updates accordingly:

| IDS token | `egui::Spacing` / `Style` field |
|-----------|--------------------------------|
| `padding_default(d)` | `button_padding`, **`window_margin.left/right/top/bottom`**, related interact dimensions |
| ~~`padding_outer(d)`~~ | ~~`window_margin.left/right/top/bottom`~~ (was here; reassigned to `padding_default`) |

`padding_outer` retains its other uses (panel inner padding, card content, `Spacing.indent`); only the window-margin binding moves down one rung.

**2. `window_corner_radius` drift recovery.** §SD7 specifies `ROUNDING_MD → window_corner_radius, menu_corner_radius`, but the apply path at `rust/imzero2/imzero2_egui/src/style/tokens/mod.rs::apply_rounding` had drifted to `ROUNDING_LG` (6 px) for windows specifically — likely a manual override that pre-dated this ADR. The real-world feedback on "windows look quite round" surfaced the drift.

Recovery: `apply_rounding` now writes `ROUNDING_MD` (4 px) to both `window_corner_radius` and `menu_corner_radius`, matching the §SD7 spec and unifying the two surfaces' rounding tier. Side-effect: windows and menus now share the same rounding tier, which reads as a more coherent visual language than the previous LG-windows / MD-menus split.

This is not a token change — `ROUNDING_LG` stays defined per §SD3 and is still available for cases that genuinely want the larger curve. The available rounding ladder (None / Sm / Md / Lg = 0 / 2 / 4 / 6 px) is unchanged.

**3. Per-density resolution table (post-amendment).** For reference, `window_margin` now resolves to:

| Density | `padding_default(d)` | `window_margin` |
|---|---:|---:|
| Tight | 6 px | 6 px |
| Standard | 8 px | 8 px |
| Roomy | 12 px | 12 px |

**Cost.** No runtime cost (still apply-once at startup). No token additions. No APCA / WCAG / CVD interaction (geometry-only). One Rust file modified (`tokens/mod.rs`); the corresponding Go side has no equivalent binding (the apply runs Rust-side only).

**Reverting.** A future ADR could revert either change independently: window_margin back to `padding_outer` is a one-token swap, window_corner_radius back to LG is a one-token swap. The `ROUNDING_LG` constant is intentionally kept available for that path.

`status` stays `proposed` per the ADR convention.

## References

- [ADR-0029 — ImZero2 design system: foundations, data-intensive patterns, policy-as-code](./0029-imzero2-design-system-and-policy-as-code.md) — parent; §SD3 (density), §SD6 (spacing), §SD7 (motion).
- [ADR-0030 — ImZero2 design system: typography](./0030-imzero2-design-system-typography.md) — sibling foundations ADR; type-scale density rule parallels spacing density rule; §SD11 conformance/branding mode reused for motion in §SD5.
- [ADR-0031 — ImZero2 design system: color foundations](./0031-imzero2-design-system-color.md) — sibling foundations ADR; same const-at-runtime / generator-at-design-time pattern, simpler here because value count is smaller.
- [`egui::Spacing`](https://docs.rs/egui/latest/egui/style/struct.Spacing.html) — target API for §SD7.
- [`egui::Context::animate_bool`](https://docs.rs/egui/latest/egui/struct.Context.html#method.animate_bool) — motion primitive for §SD5.
- [`egui::emath::easing`](https://docs.rs/emath/latest/emath/easing/) — easing curves consumed but not extended.
- WCAG 2.1 Success Criterion 2.3.3 (Animation from Interactions) — basis for §SD5 reduced-motion behaviour. [https://www.w3.org/TR/WCAG21/#animation-from-interactions](https://www.w3.org/TR/WCAG21/#animation-from-interactions)
