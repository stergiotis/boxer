---
type: adr
status: proposed
date: 2026-06-02
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted. (The first-cut tokens + migration described in §SD3/§SD6 have landed in the working tree ahead of review, per the maintainer's request to write code then document; revert is a token-file delete + four one-line reverts.)

# ADR-0065: ImZero2 design system — surface size archetypes

## Context

[ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) and the foundations sub-ADRs ([0030](./0030-imzero2-design-system-typography.md) type, [0031](./0031-imzero2-design-system-color.md) color, [0032](./0032-imzero2-design-system-spacing-density-motion.md) spacing/density/motion) tokenize everything that reaches `egui::Style` — but **window/surface sizing was never tokenized**. Apps hardcode `app.SurfaceHints.PreferredWidth/Height` as bare literals: configview 720×600, helphost 900×640, logviewer 1100×600, idsshowcase 860×620, leewaywidgets 1100×700, regex_explorer 1100×720, sccmap 1024×720, plus the `windowhost.windowDefaultSize` fallback 960×720.

The trigger: a recent fix to regex_explorer's window ballooning (its editors requested `DesiredWidth(INFINITY)` and the resizable `egui::Window` grew to the host edge) surfaced two gaps — (1) window sizing has no design-system home, so the literals drift, and (2) the "tethered inspector" use the maintainer cares about wants a *compact* preset that none of the current literals provides.

Forces specific to sizing:

- **Logical points, not pixels.** imzero2/egui works entirely in egui logical points; `pixels_per_point` maps those to the monitor's physical DPI (managed by eframe from the OS — imzero2 never queries physical monitor resolution and does not expose `pixels_per_point` to Go). A 720-pt window is 720 pt on 1080p and on 4K alike.
- **egui already clamps to host.** With `Area.constrain` (default true, which imzero2's `egui::Window` does not disable), egui clamps every window's size *and* position to `ctx.content_rect()` ≡ the host window each frame. The "don't exceed the host" requirement is therefore already satisfied; the open question is only what *preferred* sizes apps should use, consistently.
- **Screenshot-independent.** The screenshot/tour driver sizes captures from `SurfaceHints.ScreenshotStage` ([ADR-0057](./0057-demo-registry-and-drivers.md)), not `PreferredWidth/Height`. Changing window sizes does not move screenshot baselines.
- **IDS culture: defer until needed.** The IDS repeatedly parks speculative scope (light theme, cross-app patterns) until a real consumer appears. Surface sizing should land the minimal grounded set, not a speculative responsive system.

## Design space (compact)

**Q1 — Dependency axis.** (a) physical screen resolution; (b) logical points, fixed; (c) host-window-relative fractions. — **(b) chosen.** (a) double-applies DPI (egui already scales points→pixels); (c) needs plumbing that does not exist (the live host size is not exposed to Go) and egui's clamp already covers the overflow case (a) and (c) were both meant to solve.

**Q2 — Token form.** (a) role-based archetypes (Inspector/Tool/Workspace); (b) a numeric size ladder like `PxTable`; (c) independent width-scale × height-scale. — **(a) chosen.** A ladder/independent-scale is the heavier "dimension scale" option and over-serves a handful of window sizes; archetypes carry intent.

**Q3 — Migration breadth.** (a) snap every app to its nearest archetype; (b) snap only clean fits, leave the rest on literals; (c) tokens only, migrate nothing. — **(b) chosen.** (a) gratuitously resizes apps with deliberate or in-between sizes; (c) leaves the feature unadopted.

## Decision

Introduce three role-based **surface size archetypes** in egui logical points as Go-only `styletokens`: `SurfaceInspector` (420×560), `SurfaceTool` (720×600), `SurfaceWorkspace` (1100×720). Apps set `SurfaceHints.PreferredWidth/Height` from them; egui's host-clamp remains the hard ceiling. Migrate the four apps that fit an archetype within ±80 pt on both axes; leave the rest on documented literals.

## Subsidiary design decisions

### SD1 — Units: logical points, density- and resolution-independent

Token values are egui logical points (`SurfaceSize{W,H uint16}`, matching `SurfaceHints`'s `uint16` fields). They do **not** scale with physical screen resolution — `pixels_per_point` already does that, and multiplying would double-apply DPI (giant UI on HiDPI). The IDS axis for "bigger/smaller UI" is **density** (Tight/Standard/Roomy), a user preference, not a resolution read. Surface sizes are density-*independent* in this cut (see Alternatives O4).

### SD2 — Archetypes are *preferred* sizes; the host is the ceiling

Because egui clamps every window to the host window, an archetype is the size the window *opens* at; on a smaller host egui shrinks it to fit, on a larger host it stays put (it does not grow to fill). No max-size enforcement is added here — the host clamp is the max.

### SD3 — The three archetypes

| Token | Size (pt) | Role | Grounding |
|-------|----------:|------|-----------|
| `SurfaceInspector` | 420×560 | compact accessory surface that tethers to a caller widget (property inspectors; regex-as-inspector) | the tethered-inspector role that motivated this ADR; no current app is this small |
| `SurfaceTool` | 720×600 | focused, single-purpose window | exact: configview |
| `SurfaceWorkspace` | 1100×720 | wide, data-dense workspace (tables, plots, multi-pane) | exact: regex_explorer; near: logviewer, leewaywidgets, sccmap |

### SD4 — Go-only, no Rust mirror, no drift test

Unlike spacing/rounding/stroke/motion/typography, surface sizes never reach `egui::Style`: keelson windows are created host-side from Go (`windowhost` passes the size to `c.Window`). So `surface.go` has **no** `…/style/tokens/*.rs` counterpart and is **not** covered by `styletokens_drift_test.go`. This asymmetry is documented in the file header so no one expects a `surface.rs`.

### SD5 — API surface and override policy

```go
type SurfaceSize struct{ W, H uint16 } // egui logical points
var SurfaceInspector = SurfaceSize{W: 420, H: 560}
var SurfaceTool      = SurfaceSize{W: 720, H: 600}
var SurfaceWorkspace = SurfaceSize{W: 1100, H: 720}
```

Wiring is explicit at the call site (`PreferredWidth: styletokens.SurfaceWorkspace.W, …`); no `SurfaceHints` constructor is added, because `styletokens` sits below `runtime/app` in the layering and must not import it. Archetypes are **guidance, not a straitjacket**: an app with a genuinely content-driven size may still set literals.

### SD6 — Migration (first cut)

Snapped to an archetype (within ±80 pt on both axes — behaviour change ≤ a few rows):

| App | Was | Now | Δ |
|-----|----:|-----|---|
| configview | 720×600 | `SurfaceTool` | 0 |
| regex_explorer | 1100×720 | `SurfaceWorkspace` | 0 |
| leewaywidgets | 1100×700 | `SurfaceWorkspace` | +20 h |
| sccmap | 1024×720 | `SurfaceWorkspace` | +76 w |

Left on literals (deferred — see Open Questions): logviewer (1100×600 — +120 h is too far), helphost (900×640) and idsshowcase (860×620) (both sit > 80 pt from Tool *and* Workspace widths), logdemo (720×280 — a deliberate short log strip), and the `windowDefaultSize` 960×720 fallback (a host-side neutral default matching no archetype).

The regex_explorer content-width fix (`editorWidth = 800`) stays a local constant — content/component widths are the deferred "dimension scale" (Alternatives O3), not a window archetype.

## Alternatives

- **O1 — Size by physical screen resolution.** *Rejected.* Double-applies DPI; egui already maps points→pixels via `pixels_per_point`. Resolution is never the right axis for logical-point sizes.
- **O2 — Host-relative / responsive (fractions of the host).** *Deferred.* Needs new plumbing to expose the live host size to Go (today only the initial config size + panel-local `GetAvailableSize` exist), and egui's host clamp already prevents overflow. Revisit if a surface genuinely needs to scale proportionally with the host.
- **O3 — Full dimension scale (content max-widths, sidebar widths, min sizes).** *Deferred.* Would give `editorWidth`-style content widths a home, but there is only one consumer today. Revisit when a second appears; likely paired with a lint rule.
- **O4 — Density-aware surface sizes.** *Deferred.* Plausible (sizes are more like spacing than like rounding), but per [ADR-0032 §Q3](./0032-imzero2-design-system-spacing-density-motion.md) the bar is "does it earn the complexity"; with three fixed archetypes it does not yet.
- **O5 — `max_size`/`constrain_to` on the imzero2 Window IDL.** *Deferred.* A true sub-host hard cap would need an IDL method + regenerated Go/Rust + a Rust client rebuild. The host clamp already caps at the host, and archetypes set the preferred size, so no IDL change is needed for this cut.
- **O6 — `SurfaceDialog` (~560×420) and a short-strip archetype.** *Deferred.* No current `SurfaceWindowed` app uses either (the filepickers draw their own `egui::Window` and are not windowed apps; logdemo is the lone strip). Add when a real consumer appears.

## Consequences

### Positive

- Scattered window-size literals collapse onto a small, intent-carrying vocabulary; the inspector preset the tethered-inspector role wanted now exists.
- Zero new plumbing; reuses egui's existing host clamp as the ceiling. Screenshot baselines are unaffected (sizes ≠ `ScreenshotStage`).
- Fits the IDS pattern (semantic tokens, app-side references) and is trivially reversible.

### Negative

- Coarse: three archetypes don't fit the middle band (860–1024 pt), so four apps stay on literals — partial adoption.
- No lint enforcement yet (cf. L3 for spacing), so new apps can still hardcode sizes and drift.
- `SurfaceInspector`'s value is unvalidated by a real consumer; it may need tuning when the first tethered inspector lands.

### Neutral

- Go-only token with no Rust mirror — an intentional asymmetry vs. the other token files (SD4).
- Tokens are `var` (struct values can't be `const` in Go), so technically mutable — same as the palette tokens.

## Status

Proposed — awaiting review by @spx. Tokens + the four migrations have landed in the working tree (build/vet/gofmt/drift-test green).

Open questions:

1. **Lint rule (L13?).** Should a Tier-1 rule flag integer literals in `SurfaceHints.PreferredWidth/Height` and require a `styletokens.Surface*` reference (mirroring L3 for spacing)? Needs an allowlist for the deferred apps and the host-side fallback. Defer until the archetype set stabilizes.
2. **Deferred apps.** logviewer, helphost, idsshowcase — adopt a (possibly new) archetype, or keep as deliberate overrides? Each is a per-app UX call; revisit alongside Q3.
3. **A middle archetype.** The 860–1024 pt band is unserved. Add a fourth role (e.g. a medium "App" surface ~960×640) or hold the line at three and accept overrides? Hold for now; let real need decide (cf. ADR-0032 §Q "magnitude ladder extension").
4. **`windowDefaultSize` fallback.** Should the host-side 960×720 default reference a token, or stay a deliberately neutral non-archetype default? Left as-is pending Q3.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only.

## References

- [ADR-0029 — ImZero2 design system: foundations, policy-as-code](./0029-imzero2-design-system-and-policy-as-code.md) — parent framework.
- [ADR-0032 — ImZero2 design system: spacing, density, motion](./0032-imzero2-design-system-spacing-density-motion.md) — closest sibling; its §SD8 (direct source constants) and §Q3 (density-responsiveness bar) rationale are reused here; its 2026-05-16 Update already touched window margins/rounding.
- [ADR-0057 — demo registry and drivers](./0057-demo-registry-and-drivers.md) — `ScreenshotStage` is the capture size, independent of `PreferredWidth/Height`.
- `public/keelson/designsystem/styletokens/surface.go` — the tokens.
- `public/keelson/runtime/app/manifest.go` — `SurfaceHints`; `public/keelson/runtime/windowhost/windowhost.go` — `windowDefaultSize` + the single `c.Window` site that consumes the size.
- [`egui::ViewportBuilder::with_inner_size`](https://docs.rs/egui/latest/egui/viewport/struct.ViewportBuilder.html#method.with_inner_size) and [`egui::Window`](https://docs.rs/egui/latest/egui/containers/struct.Window.html) — logical-point sizing + host-clamp behaviour relied on by SD2.
