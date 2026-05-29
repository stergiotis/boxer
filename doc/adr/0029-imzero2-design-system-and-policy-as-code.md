---
type: adr
status: proposed
date: 2026-05-14
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0029: ImZero2 design system — foundations, data-intensive patterns, and policy-as-code

## Context

The ImZero2 app fleet has crossed the threshold where ad-hoc per-app styling produces visible UX inconsistency. The carousel demo, snarl node editor ([ADR-0021](./0021-imzero2-snarl-node-editor-binding.md)), time-range picker ([ADR-0016](./0016-imzero2-time-range-picker.md)), `imztop` ([ADR-0020](./0020-imzero2-imztop-resource-monitor.md)), `regex_explorer`, the play scratchpad, the dock demo, and the walkers map binding all draw from the same egui widget set, but each author has picked density, palette, spacing, label casing, iconography, and status semantics in isolation. The app catalog introduced by [ADR-0026](./0026-app-runtime-and-capability-subjects.md) only accelerates this growth — and several in-flight efforts (the Grafana-parts replacement, the Kafka port ADR-0005 admin UI, the leeway query browser) will land more apps in the same shape.

The maintainer admires Adobe Spectrum's organisation (foundations → components → patterns) and finds its written guidance easy to digest and apply. Copying any of it is out of bounds: trademark on the name, copyright on the prose and illustrations, proprietary token names and palette values, and scope explosion against a single-maintainer project. The same constraint applies to Material 3 and IBM Carbon: structure and concepts are absorbed freely; expression must be original.

Four further forces shape the design:

- **Data-intensive surface.** Most ImZero2 apps are tables of thousands-to-millions of rows, time-series plots, system-metrics dashboards, live-SQL panels, and dense forms. Visual polish must compose with information density; chrome-heavy aesthetics that work on consumer mobile patterns do not fit. Whatever the system says about color and typography must support qualitative / sequential / diverging encoding for plots without colliding with semantic status colors.
- **Go with the grain of egui.** The explicit prior is *do not fork or override the widget set*. Theme via `egui::Style` / `Visuals` / `Spacing`; document conventions on top. Custom-painted replacements for egui primitives are not in scope; the polish-vs-complexity tradeoff lives entirely inside what the existing widget set already exposes.
- **Performance non-negotiable.** ImZero2 renders continuously at 60–120 Hz (`project_imzero2_continuous_rendering`). Any enforcement, telemetry, or audit machinery that touches the render path is rejected by construction. The frame-timing surface ([reference_imzero2_frame_timing]) leaves no slack for design-system overhead.
- **Aesthetic sensibility — Swiss-minimalist and scientific.** The maintainer's stated preferences are (a) Swiss / International Typographic Style — grid discipline, restraint, hierarchy via type weight and spacing rather than ornament; canonical references in Müller-Brockmann's *Grid Systems in Graphic Design* and the Hofmann / Vignelli tradition — and (b) scientifically-grounded color, particularly Stéfan van der Walt and Nathaniel Smith's viridis family (CC0) and Fabio Crameri's scientific-colormaps family (MIT-licensed, Swiss-authored). These preferences land the foundations at the minimal, restrained, perceptually-uniform end of their respective design spaces. Dark theme only for v1 — daylight-office light-theme support is deferred until a real need surfaces.

A subtler force: the maintainer prefers *policy-as-code* — automate what is automatable; reserve human time for the irreducible visual-judgment cases. Casing of button labels, palette adherence, layout invariants are mechanical; visual hierarchy, density feel, color-encoding semantics resist mechanical detection. The system should be architected around that split, not around uniform manual review. The screenshot tour mechanism (`IMZERO2_SCREENSHOT_DIR`) already produces per-window PNG artefacts and is the natural surface for the perception-tier checks.

Existing one-off rules — the [ADR-0013](./0013-imzero2-stateful-widget-contract.md) stateful-widget contract drift guard, the `AllocateUiAtRect` warning, the FFFI databinding reset rule, the r14 click cascade rule, the egui_extras column-ordering rule, the egui_dock bounded-allocation rule — are *proto*-design-system rules already, each gated by a test or a documentation note. ADR-0013's `TestStatefulWidgetsAreGated` is precedent for mechanical enforcement of a stylistic rule via a Go test; this ADR generalises the pattern.

## Design space (QOC)

**Question.** How should we provide UX cohesion across the growing fleet of ImZero2 apps such that (a) the system respects egui's grain (no widget forks, no override of the framework's substrate), (b) clears IP / license risk, (c) is opinionated enough for a non-design-expert maintainer to apply unaided, (d) imposes zero runtime overhead on the 60–120 Hz render path, (e) automates as much enforcement as mechanically achievable, and (f) reserves human review for the irreducible visual-judgment cases?

**Options.**

- **O1 — Nothing; per-app discretion.** Status quo. Each app author chooses palette / density / iconography / casing independently. Cohesion emerges (or doesn't) by osmosis.
- **O2 — Clone Adobe Spectrum.** Port the published guidelines wholesale: token names, component anatomy, copy, examples. Maximum head-start; maximum IP exposure.
- **O3 — Native NIH; no inspirations cited.** Build everything from first principles. No license issues; loses access to a well-developed taxonomy.
- **O4 — Curated, original, egui-flow-respecting system with attributed inspirations and policy-as-code enforcement (chosen).** Inspirations consulted freely; expression fully original; layered onto `egui::Style` with no widget forks; enforcement split across mechanical / LLM-screenshot / human tiers.
- **O5 — Adopt Material 3 or IBM Carbon via wholesale port.** Like O2 with a different parent system; same IP issues with possibly more permissive code licenses but still encumbered token names, identity, and copy.

**Criteria.**

- **C1 — UX cohesion delivered.** Does the system make two apps written by different authors at different times look like one product to a stakeholder?
- **C2 — IP / license safety.** Risk of trademark, copyright, or substantial-similarity claims.
- **C3 — egui-flow respect.** Does the system live on top of `egui::Style`, or does it require widget forks, custom paint, or framework-level overrides?
- **C4 — Maintenance cost vs perceived polish.** Effort to maintain the system per unit of visual improvement across the fleet.
- **C5 — Runtime performance impact.** Cost paid by running apps to participate in the system.
- **C6 — Enforcement leverage.** Fraction of rules that are machine-checkable vs. requiring per-PR human attention.
- **C7 — Onboarding cost.** Effort for a new app author to start producing system-conformant output.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 | O5 |
|----|----|----|----|----|----|
| C1 | −− | ++ | +  | +  | ++ |
| C2 | ++ | −− | ++ | ++ | −  |
| C3 | ++ | −− | +  | ++ | −  |
| C4 | ++ | −− | −  | +  | −  |
| C5 | ++ | +  | ++ | ++ | +  |
| C6 | −− | +  | +  | ++ | +  |
| C7 | −− | +  | −  | +  | +  |

O4 dominates on C2, C3, C5, C6 without dropping below acceptable on C1, C4, C7. O2 and O5 win on C1 (their head-start) but the IP penalty is dispositive. O1 loses the entire premise — we are writing this ADR precisely because the fleet has outgrown ad-hoc styling. O3 loses C1 vs O4 because borrowing a *taxonomy structure* (the foundations / patterns split is not protected expression) materially helps cohesion without exposing the protected parts.

## Decision

We will introduce the **ImZero2 Design System (IDS)** as an original, three-tier discipline:

1. **Foundations** — a thin token layer (palette, type scale, spacing, rounding, density, motion) materialised as an `egui::Style` / `Visuals` overlay, never as a widget fork.
2. **Data-intensive patterns** — original prose covering density toggles, tables, time-range pickers, plot color encoding, semantic status (loading / stale / live / error), legends, empty states, iconography. Component coverage is *via* the egui widget set; IDS does not ship its own widgets.
3. **Policy-as-Code** — enforcement split across three tiers:
   - **Tier 1 — Mechanical.** Compile-time / lint-time AST checks: token-only usage, label casing, layout invariants, banned APIs. Zero runtime cost.
   - **Tier 2 — Recorded data with LLM review.** Offline CI passes over the per-app screenshot tour outputs; rules that need perception (clutter, color-encoding semantics, hierarchy, animation feel) are graded by an LLM against a documented rubric. Advisory until calibration converges; tightened as the rubric stabilises.
   - **Tier 3 — Human review.** New tokens, new patterns, density exemptions, novel widget compositions. Captured as follow-on ADRs.

Inspirations (Adobe Spectrum, IBM Carbon, Material 3, Refactoring UI) are consulted freely for *structure and concepts* and cited in a single `doc/design-system/INSPIRATIONS.md`; no token names, hex values, prose passages, illustrations, or example code are lifted.

This ADR is meta — it sets the architecture and the policy taxonomy. Subsequent ADRs (numbered into the 003x range) will populate specific tokens, patterns, and rule sets.

## Subsidiary design decisions

### SD1 — Three-tier scope: no components, no widgets

We will *not* author a "Components" section, a widget library, or custom-painted equivalents of egui's primitives. The Foundations tier sets the style; egui's existing widget set (Button, Checkbox, Slider, DragValue, TextEdit, Table, CollapsingHeader, ScrollArea, …) **is** the component layer. The Patterns tier documents *which widget to reach for under which conditions*, not how to draw replacements.

This removes ~80% of a typical design-system's surface area, eliminates the polish-vs-complexity tradeoff at its root, and sidesteps the largest IP risk. It also commits the project to whatever egui's widget set can express — accepted, with the explicit understanding that genuinely-required custom paint is a Tier 3 escalation (SD10).

### SD2 — Token layer over `egui::Style`

Tokens live in two surfaces:

- **Authoritative Rust module** at `src/rust/imzero2_egui/src/style/tokens/`. Each token is a named `const`; the module exposes a function `apply(visuals: &mut Visuals, spacing: &mut Spacing, density: Density)` that materialises the token set into egui's existing fields.
- **Parallel Go enum surface** at `src/go/public/thestack/imzero2/egui2/styletokens/`, used by app authors writing the Go-side render code. Codegen via `./generate.sh` ([reference_generate_sh]) keeps the two in lockstep; drift is a build break.

Apps call `apply` once on startup; IDS never reads from this module at render time — tokens are constants. Adding, removing, or renaming a token requires a Tier 3 ADR.

### SD3 — Density presets (original names)

Three density modes: **Tight**, **Standard**, **Roomy**. Deliberately chosen original names that avoid Spectrum's "Compact / Regular / Spacious" or Material's "dense / regular / comfortable" verbatim. The active preset is a single `u8` the app sets at startup; it scales spacing / padding tokens by a fixed factor. The mode lives in a `DensityE` enum on the Go side and a `Density` enum on the Rust side, per boxer's enum-suffix convention.

Density is per-app, not per-window-within-app; mixed-density screens are a Tier 2 finding (rule V4, SD9).

### SD4 — Color tokens: two disjoint palettes

Two palettes that do not overlap:

- **Semantic palette** — `info` / `success` / `warning` / `error` / `neutral` / `accent`. Used for status badges, validation, buttons, focus rings. Tied to the egui::Visuals widget colors.
- **Data-encoding palette** — qualitative (categorical), sequential (single-hue ramps), diverging (two-hue ramps). Used by plots, heatmaps, table cell shading.

The palettes are *disjoint* to prevent a chart color colliding with a status color (e.g., red as "error" vs red as "high temperature"). This is the kind of rule mechanical lints cannot enforce alone; SD9's Tier 2 rule V2 grades cross-app color-encoding consistency.

Color values are picked from scratch: CIE L\*C\*h\* space, perceptually uniform spacing, WCAG AA contrast verified. Method is documented in `foundations/color.md`; concrete hex values are original. No hex value is lifted from Spectrum, Material, Tailwind, or any other published palette; SD12 documents the boundary check.

### SD5 — Type scale

A small fixed scale: **Display** / **Heading** / **Body** / **Caption** / **Micro**. Numeric content uses a monospace variant (`Body.Mono`, `Caption.Mono`) for tabular alignment — this is a data-density requirement, not a stylistic choice; numeric tables that lose column-alignment are unreadable at the row counts we target.

Font family selection is deferred to a foundations-typography sub-ADR. Both leading candidates (Inter + JetBrains Mono; DM Sans + JetBrains Mono) are OFL-licensed and embeddable.

### SD6 — Spacing, rounding, stroke widths

Spacing scale on a 2 px base: **2, 4, 6, 8, 12, 16, 24, 32**. Multiples of 2 only — no 1 px to prevent sub-pixel rounding artefacts at fractional DPI scales. Rounding scale: **0, 2, 4, 6**. Stroke widths: **1, 1.5, 2**.

All values surface as named constants in the token module. Tier 1 lint L3 (SD8) flags literal floats in `egui::Spacing` fields outside the token module; allowlisted exceptions: `0.0`, `1.0` (hairline strokes), `NaN` (sentinel).

### SD7 — Motion conventions

egui-native animations only: `Context::animate_bool`, `animate_value_with_time`. No custom timeline framework, no tween library. Three duration tokens: **Quick** (80 ms), **Standard** (160 ms), **Slow** (320 ms). Easing limited to what egui exposes.

Tour-screenshot constraint: any animation that crosses a render-boundary must complete within the tour's frame budget (currently 4 frames) or be configured with a `DefaultOpen(true)`-style preset. This rule already exists for `CollapsingHeader` ([feedback_collapsingheader_tour]); IDS generalises it.

### SD8 — Policy-as-Code Tier 1: mechanical lints

A new tool `src/go/cmd/designlint/` — a Go AST walker built on `golang.org/x/tools/go/analysis` — implements per-rule passes:

- **L1 — Label casing.** Extracts string literals at call sites of `c.Button(...)`, `c.MenuItem(...)`, `c.Header(...)`, `c.SelectableLabel(...)`, etc.; classifies casing; flags violations against per-widget-class policy (e.g., Title Case for buttons, Sentence case for menu items, Section Case for panel headers — concrete policy in `policy/tier1-mechanical.md`).
- **L2 — Token-only palette.** Flags `egui::Color32::from_rgb(...)`, `Color32::from_hex(...)`, and Go-side equivalents outside the token module.
- **L3 — Token-only spacing.** Flags literal floats appearing in `egui::Spacing` field assignments or as direct arguments to spacing-aware APIs outside the token module. Heuristic; the allowlist (SD6) prevents the common false positives.
- **L4 — Direct Style mutation outside tokens.** Flags `ctx.set_visuals(...)` / `ctx.set_style(...)` / `style.spacing.* = ...` / `style.visuals.* = ...` outside the token module. Apps customise via density preset; one-off overrides require Tier 3.
- **L5 — `AllocateUiAtRect` inside flow containers.** Flags `c.AllocateUiAtRect(...)` inside a lexical `Vertical(...)` / `Horizontal(...)` block ([project_imzero2_allocate_ui_at_rect]). Heuristic with false positives; opt-out via `// designlint:ignore=L5` comment annotation with a justification.
- **L6 — Required `PanelCentral` for full-screen apps.** Top-level apps annotated `// imzero2:fullscreen` must contain a `c.PanelCentral()` call ([reference_egui2_panel_central]).
- **L7 — Status semantics via enum, not color literal.** Apps reaching for color-by-status must use the `statussemantics` enum (deferred package), not raw color constants — couples L2 to SD4.
- **L8 — Mandatory `Tree(id).Send()` drain after node ops.** Lifts [feedback_tree_drain] into a Tier 1 rule.
- **L9 — `HasPrimaryClicked` for RadioButton.** Lifts [feedback_radio_haspricked] into a Tier 1 rule.

The drift guard `TestStatefulWidgetsAreGated` from [ADR-0013](./0013-imzero2-stateful-widget-contract.md) is left in place as a Go test; it could be ported into `designlint`, but the test form is precedent and shipping. New mechanical rules going forward should land as `designlint` passes; existing per-package tests are not migrated unless a maintenance reason arises.

`designlint` is wired into `scripts/ci/lint.sh` alongside vet / staticcheck / errcheck / nilaway / doclint. Each rule is configurable via `designlint.yaml` (per-rule severity: warn / error / off, per-rule allowlist file paths). Rules graduate from warn to error per-app as backfill completes (SD14, M5).

Runtime cost: **zero**. All Tier 1 checks are build-time only.

### SD9 — Policy-as-Code Tier 2: offline LLM review of screenshot artefacts

A new driver `scripts/ci/designreview/` walks `IMZERO2_SCREENSHOT_DIR` outputs from each app's standard tour ([reference_screenshot_tour]). For each PNG (and short PNG sequence where animations matter), the driver invokes an LLM with three inputs:

- The screenshot (or sequence).
- A rubric scoped to one rule at a time.
- The current token reference, so the LLM grades against the system, not its own taste.

Output: structured JSON findings (per rule × per screenshot) plus a markdown report under `doc/design-system/reviews/<run-id>/`. Findings are *advisory* during M4 calibration; individual rules graduate to error gates as their false-positive rate is bounded.

Rules pinned for Tier 2 (not exhaustive):

- **V1 — Clutter / hierarchy.** Single screenshot. "Is the focal point obvious? Are secondary controls visually subordinated?"
- **V2 — Color-encoding consistency.** Two screenshots from different apps. "Do the chart colors carry the same meaning across these apps?"
- **V3 — Empty-state quality.** Screenshot of a panel with no data. "Is the empty state informative (icon + help text + CTA), or just blank?"
- **V4 — Density consistency within a screen.** Single screenshot. "Do adjacent panels share a density preset?"
- **V5 — Legend completeness.** Plot screenshots. "Are series, axes, and units all labelled? Is the unit and time-range visible?"
- **V6 — Iconography consistency.** Cross-app sample. "Does each icon mean the same thing in every app where it appears?"
- **V7 — Typographic rhythm.** Single screenshot. "Are heading-to-body and body-to-caption spacings consistent?"
- **V8 — Animation feel.** PNG sequence. "Do transitions complete within the tour budget? Are they smooth or jittery?"

**Cost discipline.** LLM calls are billable. The driver:

- Runs only when design-relevant files change (token module, designlint rules, screenshot-tour code, or any app UI code touching tokens or status colors) — a manifest filter in `scripts/ci/designreview/manifest.go`.
- Caps cost per CI run (default $0.50).
- Caches by `(screenshot blake3, rubric version, model name)` — re-running over an unchanged screenshot is free.
- Model choice deferred to the M4 design pass; the existing skill `adversarial-review` is the closest precedent. LM Studio local-model path is open ([feedback_lmstudio_qwen3_reasoning] cautions on Qwen 3.x reasoning) but cost-bounded; Anthropic API is the conservative default.

Runtime cost: **zero** for the running app. CI cost is bounded by the manifest filter and cache.

### SD10 — Policy-as-Code Tier 3: human review

Reserved for cases that cannot be checked mechanically and that exceed what a rubric-graded LLM can usefully advise on:

- Adding, removing, or renaming a token.
- Adding, removing, or rewriting a Tier 2 rubric rule.
- Density-policy exemption for an app.
- Novel pattern that the data-intensive patterns doc set does not yet cover.
- Cross-app conventions (command palette, keybinding map, global search shape) requiring fleet migration.
- Custom-painted widget that the no-widgets boundary (SD1) does not accommodate.

Each Tier 3 case is captured as a follow-on ADR using boxer's canonical template via `scripts/new-doc.sh adr`. No design-system-specific ADR schema; the existing template's `Subsidiary design decisions` section is sufficient for token batches or pattern doc sets.

### SD11 — Doc layout

```
doc/design-system/
├── INSPIRATIONS.md             # attributions; non-normative; sources only
├── foundations/
│   ├── color.md                # SD4
│   ├── typography.md           # SD5
│   ├── spacing.md              # SD6
│   ├── motion.md               # SD7
│   └── density.md              # SD3
├── patterns/
│   ├── tables.md
│   ├── time-range-picker.md
│   ├── plots.md
│   ├── status-and-legends.md
│   ├── empty-states.md
│   └── iconography.md
├── policy/
│   ├── tier1-mechanical.md     # SD8 — rule catalog with current severities
│   ├── tier2-llm-review.md     # SD9 — rubric versions, model, cost notes
│   └── tier3-human-review.md   # SD10 — process and ADR list
└── reviews/                    # SD9 LLM review reports, per CI run

src/rust/imzero2_egui/src/style/tokens/             # Rust-authoritative tokens
src/go/public/thestack/imzero2/egui2/styletokens/   # Go enum surface; codegen-mirrored
src/go/cmd/designlint/                              # Tier 1 tool (Go AST walker)
scripts/ci/designlint.sh                            # lint.sh integration
scripts/ci/designreview/                            # Tier 2 driver
```

Each doc carries Diátaxis front-matter per boxer's standard. Foundations and policy are `reference`; patterns are `explanation`; `INSPIRATIONS.md` is `explanation`. New docs are seeded with `scripts/new-doc.sh`.

### SD12 — IP / attribution boundary

- **Allowed.** Reading any public design system; absorbing its taxonomy and category vocabulary (the foundations-vs-patterns split, the density-mode concept, the qualitative/sequential/diverging plot-palette taxonomy — these are not protected expression); citing in `INSPIRATIONS.md`.
- **Forbidden.** Copying token names verbatim (Spectrum's "100 / 200 / 300" scale, Material's "primary / secondary / tertiary" tier names), hex values, prose passages, illustrations, example screenshots, marketing copy.
- **Boundary check (per token batch).** A Tier 3 review of token additions includes (a) verbatim-search of the proposed token names across Spectrum, Material, Carbon, Fluent, Polaris, Tailwind, and Bootstrap, and (b) sanity check that the chosen hex value is not a known palette anchor in any of those systems. The check is documented in the token-batch ADR.
- **Pre-cleared: scientifically-published palettes.** Colormaps published as scientific results under CC0 or MIT — the viridis family (van der Walt & Smith 2015, CC0), cividis (Nuñez et al. 2018, CC0), and the Crameri *Scientific colour maps* family (batlow / vik / batlowS / lapaz / oslo / etc., MIT) — are pre-cleared for direct adoption. They are public-good scientific outputs rather than design-system tokens; their use is the *more* original choice, since hand-rolling a perceptually-uniform sequential ramp would reinvent an inferior version of a published result. Adopting them carries one obligation: preserve the attribution required by the publication (citation in `INSPIRATIONS.md` plus a comment in the consuming code).

The `INSPIRATIONS.md` file lists the systems consulted and the *category* of thing borrowed (e.g., "Spectrum — foundations-vs-patterns structural split"; "IBM Carbon — data-density typographic conventions"; "Refactoring UI — palette-from-scratch method"). It is non-normative; updates do not require an ADR.

### SD13 — Performance posture (hard invariant)

**No design-system code runs in the render path.**

- Tokens are `const`; `apply(visuals, spacing, density)` is called once at startup.
- Tier 1 lints are build-time.
- Tier 2 review is offline CI; reads tour PNGs, never engages with the running app.
- Tier 3 is human-time.

This is non-negotiable for the data-intensive 60–120 Hz use case. Any future proposal that requires per-frame instrumentation for design-system enforcement is rejected at intake; the screenshot-tour surface is sufficient for everything Tier 2 needs.

### SD14 — Phasing

- **M0 — Inspirations + boundary check.** Write `INSPIRATIONS.md`; verify proposed naming conventions are free of substantial-similarity issues across the major systems listed in SD12. ~half a day.
- **M1 — Tokens module + density preset.** SD2 / SD3 / SD4 / SD5 / SD6 / SD7. Both Rust and Go surfaces; codegen via `./generate.sh`. One pilot app (carousel demo) wired to use the tokens. Lints not yet enforced.
- **M2 — Tier 1 mechanical lints.** SD8. `designlint` tool, integration into `lint.sh`. Rules ship as `warn`; per-app graduation to `error` is opt-in via a `// designlint:strict` annotation that flips all rules for that app.
- **M3 — First three data-intensive patterns.** `patterns/tables.md`, `patterns/status-and-legends.md`, `patterns/plots.md`. Each pattern doc cites which existing apps already comply and which need migration.
- **M4 — Tier 2 LLM review.** SD9. `designreview` driver; first rubric pass covers V1 (clutter), V3 (empty-state), V5 (legend completeness) — the three most automatable rules. Advisory only; cost cap enforced. Includes the model-and-provider sub-ADR.
- **M5 — Backfill across the app fleet.** Apply tokens + lints + patterns to existing apps (regex_explorer, snarl, time-range-picker, imztop, dock demo, walkers, …). Each app is one follow-on PR; the maintainer can stage or hand-off ([project_shared_worktree] cautions on concurrent commits).

Each milestone is independently shippable; M2 does not gate M3; M4 does not gate M5.

## Alternatives

- **O1 — Per-app discretion.** Rejected on C1; the status quo. The fleet has outgrown ad-hoc styling; the cost of inaction is the visible inconsistency this ADR is written to address.
- **O2 — Clone Adobe Spectrum.** Rejected on C2 (trademark on "Spectrum"; copyrighted prose, examples, illustrations, proprietary palette; their `100 / 200 / 300` token scale is the most-borrowed-by-accident artefact in the industry) and C3 (Spectrum's component anatomy presumes retained-DOM widgets, ARIA accessibility, CSS theming — none of which match egui's immediate-mode grain). Even at zero IP risk, the scope is wildly disproportionate to a single-maintainer project.
- **O3 — Native NIH; no inspirations cited.** Rejected on C1; the Spectrum-style taxonomy (foundations / patterns / policies) and the qualitative/sequential/diverging plot-palette concept are *structural* assets, freely usable. Reinventing the structure costs time without buying anything; refusing to cite the influences is performatively pure rather than substantively safer.
- **O5 — Material 3 / IBM Carbon port.** Same IP / license issues as O2 with a different parent. Material 3 has a permissive Apache-2.0 license on its *code* artifacts, but token names and the Material identity are protected. The line is blurry enough to be worth avoiding.
- **Custom widget library on top of egui.** Considered for cases where egui's defaults look spare. Rejected on C3 and C4: forks the widget set, doubles maintenance, defeats "go with the grain of egui." Custom paint is reserved for Tier 3 escalations where no egui composition can express the requirement.
- **Runtime telemetry-driven design audits.** Considered for "live" density / palette / casing checks against the running render tree. Rejected on C5: violates the performance invariant. The screenshot tour already provides a zero-cost offline surface that achieves the same coverage without instrumenting the hot path.
- **Single-tier human-only enforcement.** Rejected on C4 / C6: stylistic uniformity (label casing, palette adherence) is exactly the kind of rule that human review degrades on — reviewers tire of pointing them out and they leak. Mechanical detection is necessary.
- **Single-tier mechanical-only enforcement.** Rejected on C1: visual hierarchy, color-encoding semantics, density feel resist mechanical detection. Without Tier 2, the system blesses code that passes lints but looks chaotic.
- **Vendoring an existing token system at the hex-value level (e.g., the Tailwind palette).** Considered as a head-start for SD4. Rejected: the Tailwind palette is MIT-licensed *as code* but `slate-500` is a token name and a specific OKLab anchor that downstream products carry as a recognisable identity. Generating perceptually-uniform tokens from scratch is cheap; the head-start is not worth the entanglement.

## Consequences

### Positive

- **UX cohesion becomes incremental.** Each app touching tokens picks up the system's defaults; the fleet converges over time rather than requiring a Big Bang migration.
- **Performance budget intact.** Zero runtime overhead; all enforcement is build-time or offline-CI. Honours the 60–120 Hz invariant.
- **Maintainer effort scales with system size, not fleet size.** Adding a new app costs nothing beyond calling `apply()` and reaching for the tokens; the system's per-app marginal cost is approximately zero after tokens are wired in.
- **Policy is enforceable, not aspirational.** Tier 1 catches mechanical drift on every PR; Tier 2 catches visual drift on every screenshot-tour run; Tier 3 reserves human attention for genuine novelty. The split is honest about which rules each tier can actually enforce.
- **IP exposure cleared.** Inspirations are attributed; expression is original; the boundary check (SD12) gates token additions per batch.
- **Existing constraints find a home.** `AllocateUiAtRect`, click cascade, stateful-widget contract, FFFI databinding lag, tree-drain, RadioButton click semantics, egui_extras column ordering — currently scattered as one-off memory notes and per-package tests — fold into the Tier 1 rule catalog and the patterns docs.
- **Screenshot tour earns a second customer.** The existing `IMZERO2_SCREENSHOT_DIR` artefact pipeline (used today for visual testing) becomes the substrate for Tier 2 reviews without duplicating effort.

### Negative

- **Token churn is irreversible without migration cost.** Renaming a token requires a fleet sweep. Mitigation: front-load naming work in M1; require Tier 3 ADR for token changes; codegen surface ensures Go and Rust stay in sync.
- **Tier 2 LLM cost has no natural ceiling without manifest discipline.** A poorly-tuned manifest filter could run the full rubric on unrelated PRs. Mitigation: per-CI-run cost cap (SD9); audit logs of LLM spend per PR; cache-by-screenshot-hash; M4 design pass picks the model and provider with explicit attention to cost.
- **The system bites the maintainer first.** Every new pilot app and every existing app pays the migration tax in M2 / M5. Mitigation: phased rollout; lint warnings before errors; per-app opt-in via `// designlint:strict` annotation; backfill is one PR per app, sequenceable.
- **The "no widgets" boundary may be tested.** A pattern that genuinely demands custom paint (sparkline-in-a-table-cell, micro-bar-chart-as-badge, custom snarl pin styles) forces either a Tier 3 deviation or a stretch of the egui-native palette. Mitigation: pattern docs document the boundary explicitly; deviations are ADR'd; the precedent is the snarl binding ([ADR-0021](./0021-imzero2-snarl-node-editor-binding.md)) which already lives at this boundary.
- **Tier 2 rubrics are subjective.** LLM grading depends on the rubric, the model, and the screenshot. Expect calibration churn for the first few rubric versions; expect arguments about whether "the focal point is obvious" is fairly graded. Mitigation: advisory until calibration converges; rubric versions explicit; cache invalidated on rubric bump; humans can override LLM findings.
- **`designlint` is one more CI step to maintain.** AST walkers have edge cases; rules need exceptions. Mitigation: rule severity is per-rule configurable; annotation-based opt-out for cases the AST cannot disambiguate; the tool itself is small (one binary, one config file).
- **`bytebufferpool`-style new dependency surface for Tier 2.** The LLM-review driver brings an HTTP client, a caching layer, and a JSON schema. Acceptable cost; isolated under `scripts/ci/designreview/` and not on the runtime path.

### Neutral

- **The design system becomes a public-stability surface for the apps.** Renaming a token or removing a pattern requires a deprecation cycle. ADR-0026 has the same property for cap subjects; the project is comfortable with this shape.
- **One more axis of CI failure.** A new app PR can fail on `designlint` in addition to vet / staticcheck / errcheck / nilaway / doclint. Acceptable cost; failures are local and easy to fix.
- **`INSPIRATIONS.md` is non-normative.** Updating it does not require an ADR. Adding inspirations should still go through PR review; removing one should be rare.
- **The pattern doc set grows over time.** New patterns land as M5 backfill exposes them. The doc set is the curated working set, not a comprehensive list.

## Status

Proposed — awaiting review by @spx. M0 (inspirations + boundary check) and M1 (tokens module) can begin as soon as accepted; M4 (Tier 2 LLM review) depends on the cost-cap policy and the screenshot-tour pipeline being CI-integrated.

Open questions:

1. **Font family choice.** Inter + JetBrains Mono vs DM Sans + JetBrains Mono vs system-default. Deferred to a foundations-typography sub-ADR. Both leading candidates are OFL-licensed.
2. **Tier 2 LLM model + provider.** Which model grades screenshots; whether grading runs against the Anthropic API or via local LM Studio. Affects cost ceiling and reproducibility. Deferred to the M4 design pass.
3. **App opt-in vs default-on.** Should new apps default to the design system, or opt in via annotation? Default-on once tokens are stable; opt-in via `// designlint:strict` during M1 / M2 transition.
4. **Cross-app patterns** (command palette, keybinding map, global search). Are these in scope of IDS or a parallel concern? Deferred; revisit when a second app needs them.
5. **i18n / localization.** Does label-casing policy need to vary per locale? Out of scope for v1; revisit if a non-English locale lands.
6. **Stylelint-style auto-fix for Tier 1.** Should `designlint` ship a `--fix` mode that rewrites violating literals to token references? Defer; M2 ships warn-only and reads; auto-fix can land in M2.5 once the rule set is stable.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only; supersession is recorded, not deleted.

## Updates

### 2026-05-16 — Path landings: keelson namespace + `cmd/designsystem` consolidation

Records three deviations from the §SD2 / §SD9 / §SD11 / §SD14 path proposals that landed during M1 / M2 implementation. No design decision in §SD1–§SD10 is modified; the §SD11 layout sketch is superseded by the paths recorded here.

1. **Go token surface moved.** §SD2 / §SD11 specified `src/go/public/thestack/imzero2/egui2/styletokens/`. Actual landing is **`src/go/public/keelson/designsystem/styletokens/`** per [ADR-0035](./0035-keelson-namespace-introduction.md), which pulled cross-app platform packages out of `thestack/` into the `keelson/` namespace before this ADR's M1 wiring. Same package contents, different import path; codegen mirror against the Rust source-of-truth at `src/rust/imzero2_egui/src/style/tokens/` is unchanged. The drift guard `styletokens_drift_test.go` lives in the new location.

2. **Tier 2 driver consolidated into the `cmd/designsystem` multi-tool.** §SD9 / §SD11 specified a standalone `scripts/ci/designreview/` driver with `manifest.go` adjacent. Actual landing:
    - Library at **`src/go/public/keelson/designsystem/review/`** (today only `review/ssim/` exists; the driver itself awaits M4).
    - CLI surface as a subcommand of the IDS multi-tool: **`./src/go/cmd/designsystem review …`**, sharing the binary that hosts `colors gen` / `colors vendor` and the provider abstraction (Anthropic / LM Studio).
    - Manifest filter at `src/go/public/keelson/designsystem/review/manifest.go`.

    Rationale: a single Go binary with subcommands shares the IDS dependency surface and matches the way the palette / vendor generators are already invoked; a `scripts/ci/` shell wrapper added nothing. [tier2-llm-review.md](../design-system/policy/tier2-llm-review.md) already records the consolidated paths.

3. **`scripts/ci/designlint.sh` not split out.** §SD11 anticipated a separate shell wrapper invoked from `lint.sh`. Actual: `designlint` is wired directly into `scripts/ci/lint.sh` (the `step_begin "designlint"` block) and driven via **`go vet -vettool=`** over a tempfile-built binary. The `-tags` flag on `multichecker.Main` is documented "no effect (deprecated)" and silently no-ops; the `vettool` protocol is the supported tag-aware analyzer path, so the invocation lives in `lint.sh` rather than behind a wrapper. The `cmd/designlint/main.go` package doc reflects the corrected invocation contract ([feedback_multichecker_tags_deprecated]).

Other proposed paths (`src/rust/imzero2_egui/src/style/tokens/`, `src/go/cmd/designlint/`, `doc/design-system/{foundations,patterns,policy}/`, the `doc/design-system/reviews/` output dir) landed as specified.

`status` stays `proposed` per the ADR convention.

## References

- [ADR-0013 — ImZero2 stateful widget contract](./0013-imzero2-stateful-widget-contract.md) — precedent for mechanical enforcement of a stylistic rule via a Go test; `TestStatefulWidgetsAreGated` is the model for SD8.
- [ADR-0014 — ImZero2 context-typed Ui](./0014-imzero2-context-typed-ui.md) — typed Ui scopes the surface that token-aware patterns inhabit.
- [ADR-0016 — ImZero2 time-range picker](./0016-imzero2-time-range-picker.md) — early data-intensive pattern; first pattern doc candidate.
- [ADR-0020 — ImZero2 imztop resource monitor](./0020-imzero2-imztop-resource-monitor.md) — data-density showcase; SD9 V5 calibration target.
- [ADR-0021 — ImZero2 snarl node editor binding](./0021-imzero2-snarl-node-editor-binding.md) — existing case that lives near the SD1 no-widgets boundary; precedent for Tier 3 escalation shape.
- [ADR-0026 — App runtime and capability subjects](./0026-app-runtime-and-capability-subjects.md) — the app catalog this design system stylises.
- `doc/skills/imzero2/SKILLS.md` — culling, registers, block skipping; existing platform constraints absorbed by the policy catalog.
- [`spectrum.adobe.com`](https://spectrum.adobe.com/) — inspiration only; foundations-vs-patterns structural vocabulary consulted, no content lifted.
- [`carbondesignsystem.com`](https://carbondesignsystem.com/) — inspiration; data-density heritage.
- [`m3.material.io`](https://m3.material.io/) — inspiration; token-system structure.
- [`refactoringui.com`](https://www.refactoringui.com/) — inspiration; palette-from-scratch method.
- `golang.org/x/tools/go/analysis` — Go AST analysis framework used by `designlint` (SD8).
