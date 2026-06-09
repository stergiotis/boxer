---
type: adr
status: accepted
date: 2026-06-06
reviewed-by: "p@stergiotis"
reviewed-date: 2026-06-08
---

# ADR-0068: ImZero2 gauge widget — radial read-only dial

## Context

We want a **gauge** widget under [`public/thestack/imzero2/egui2/widgets/`](../../public/thestack/imzero2/egui2/widgets/): a compact readout that maps **one scalar onto a bounded range**, annotated with colored zones, ticks, and a value readout. It joins the observability family already in the tree — `metricsoverlay`, `runtimestatus`, `taskmonitor`, `jobprogress`, the imzrt dashboard ([ADR-0061](./0061-imzero2-imzrt-go-runtime-dashboard.md)). Its distinct niche is **a single scalar judged against thresholds/zones** — not progress-over-time (a sparkline), not change (a delta), not a bare number.

A short design dialogue with the maintainer fixed the two top-level choices and the substrate before this ADR:

- **Geometry: a radial 270° needle dial** — the iconic speedometer form — chosen over the linear/bullet form and the semicircle. (The linear/bullet form is the data-ink-optimal, accessibility-preferred shape per Stephen Few, and remains the most likely *second* renderer; see Deferred.)
- **Interaction: read-only display.** A dashboard readout, not an input. Rotary *input* is already covered by the maintainer's [`imgui_knobs`](https://github.com/stergiotis/imgui_knobs) Go port, so the gauge takes its value by copy and mutates nothing.

A survey of the field (ECharts gauge, Plotly `indicator`, Grafana Gauge/Bar-gauge, ApexCharts `radialBar`, AnyChart, D3, imgui-knobs) confirms every library converges on **one value-on-range model with a pluggable renderer** and differs only in shape; the recurring vocabulary is `value`, `min`/`max`, zones/bands, ticks, target marker, value formatting, and (radial) start/end angle. The default radial sweep across the field is **270°** (ECharts default `startAngle 225 → endAngle −45`); we adopt it.

Forces specific to this widget:

- **The painter substrate fits a needle dial, and only a needle dial.** The egui2 painter ([`bindings`](../../public/thestack/imzero2/egui2/bindings/)) exposes `PaintPolyline`, `PaintLine`, `PaintArrow`, `PaintCircleFilled/Stroke`, `PaintRectFilled/Stroke`, `PaintText`, `PaintSenseRegion`, `PaintCubicBezier` — but **no arc and no filled-polygon** primitive (verified against `methods.out.go`/`factories.out.go`). A needle dial is exactly what those render well: a *thick stroked `PaintPolyline`* sampled at ~1°/segment is a clean colored arc band (butt caps give crisp radial zone boundaries), and the needle, hub, ticks, and center readout are lines / a circle / text. `treemap` is the custom-paint precedent ([`treemap.go`](../../public/thestack/imzero2/egui2/widgets/treemap/treemap.go)).
- **IDS owns every color, size, and type ramp — the widget hardcodes nothing.** Zones are semantic `styletokens.Tone` (the role API this widget motivated promoting into IDS — [ADR-0031](./0031-imzero2-design-system-color.md) §Updates 2026-06-06); every other color is a neutral/semantic token; text sizes come from the type scale × density ([ADR-0030](./0030-imzero2-design-system-typography.md)); the bridge is always `color.Hex(token.AsHex())` ([ADR-0035](./0035-keelson-namespace-introduction.md) layering).
- **The established widget contract is the value-receiver `Renderer`.** `distsummary` / `regexsummary` / `canonicaltypesummary` ([ADR-0067](./0067-imzero2-canonicaltype-entry-and-tethered-inspector.md)) all use `New(idPrefix)` + fluent copy-returning setters + `Render(idGen, …)`. A read-only dial has no cross-frame state, so v1 is fully stateless (no package `sync.Map`).
- **Accessibility is a hard IDS rule.** [ADR-0031](./0031-imzero2-design-system-color.md) §SD5: "color is never the sole encoding channel." A traffic-light dial bends the IDS single-hue-default ethos, so the value is **always** drawn as text and zones carry **labels** for redundant encoding.

Invariants the design must respect:

- **IDS-token-only.** No literal colors, type sizes, or stroke widths; designlint governs.
- **Painter idiom.** `AllocateUiAtRect` → `Paint*` → `PaintCanvas`, canvas-relative coordinates (the `treemap` shape).
- **Finite sizing.** The dial is a finite square; no `DesiredWidth(INFINITY)` ballooning ([ADR-0065](./0065-imzero2-design-system-surface-sizes.md) context).
- **Demo registry / capture ([ADR-0057](./0057-demo-registry-and-drivers.md)).** Register a `registry.Demo{}`; the TestDriver screenshots it automatically.

## Design space (compact)

**Q1 — Geometry.** (a) linear / bullet; (b) **radial 270° needle dial**; (c) 180° semicircle; (d) progress ring. — **(b) chosen** (maintainer): the recognizable KPI form, and the one the painter renders directly. (a) is the data-ink/accessibility-optimal form (Few) and the most likely second renderer (Deferred); the value-on-range model is built so it slots behind a style switch.

**Q2 — Interaction.** (a) **read-only display**; (b) interactive knob; (c) both. — **(a) chosen** (maintainer): a readout. Rotary input is already `imgui_knobs`; read-only keeps the contract `Render(idGen, value)` with no mutation and no state.

**Q3 — Rendering substrate.** (a) **painter / canvas**; (b) `egui_plot` via `PlotPolygon`. — **(a) chosen**: a needle dial needs stroked arc bands + a needle, which the painter draws in pixel space with no axis/zoom/pan machinery to suppress. (b) offers *filled* wedges (crisp annular sectors, round caps) but is not needed for a needle dial; filled-wedge zones via (b) are Deferred.

**Q4 — Zone color model.** (a) raw `color.Color`; (b) **semantic `styletokens.Tone` + `Label`**. — **(b) chosen**: IDS-idiomatic and theme-consistent, and the label is the WCAG-required redundant channel. This widget being the second consumer to *paint* a tone is what triggered promoting tone into IDS ([ADR-0031](./0031-imzero2-design-system-color.md) §Updates).

## Decision

Build **`gauge`** under `widgets/` — a read-only **radial 270° needle dial** on the painter substrate, configured by the `distsummary` value-receiver idiom, colored and sized entirely from IDS tokens, with zones expressed as `styletokens.Tone`. Default sweep `225° → −45°` (the field-standard 270° arc). Register a screenshot demo ([ADR-0057](./0057-demo-registry-and-drivers.md)). The unified value-on-range model is structured so a linear/bullet renderer can be added later behind a style switch (Deferred).

## Subsidiary design decisions

### SD1 — Public API: value-receiver `Renderer` + fluent setters

```go
package gauge // public/thestack/imzero2/egui2/widgets/gauge

type Renderer struct {
    idPrefix string
    min, max float64           // scale bounds                          default 0, 100
    startDeg, endDeg float32    // sweep, IDS angle convention (SD3)      default 225, -45
    size     SizeE             // SizeSm|Md|Lg, density-scaled (SD4)     default SizeMd
    diameter float32           // explicit override in logical pt; 0 = derive from size
    zones    []Zone            // colored bands; empty => neutral track
    zoneMode ZoneModeE         // ZoneAbsolute (default) | ZonePercentage
    majorTicks, minorTicks int // 0 major => derive a sensible count
    showTicks, showValue   bool // default true, true
    label  string              // metric name, drawn under the dial
    format FormatFunc          // value -> readout; default humanize
    suffix string              // "%", "ms", …
    needleFollowsZone bool     // needle takes the active zone's tone; default false (neutral)
    density       styletokens.DensityE      // cached in New() from DensityFromEnv()
    accessibility styletokens.AccessibilityE // cached in New() from AccessibilityFromEnv()
}

type Zone struct { From, To float64; Tone styletokens.Tone; Label string }
type ZoneModeE uint8 // ZoneAbsolute | ZonePercentage
type SizeE     uint8 // SizeSm | SizeMd | SizeLg
type FormatFunc func(v float64) string

func New(idPrefix string) Renderer        // documented defaults above
func (r Renderer) Range(min, max float64) Renderer
func (r Renderer) Sweep(startDeg, endDeg float32) Renderer
func (r Renderer) Size(s SizeE) Renderer
func (r Renderer) Diameter(px float32) Renderer
func (r Renderer) Zones(z ...Zone) Renderer
func (r Renderer) ZoneMode(m ZoneModeE) Renderer
func (r Renderer) Ticks(major, minor int) Renderer
func (r Renderer) Label(s string) Renderer
func (r Renderer) Format(fn FormatFunc) Renderer
func (r Renderer) Suffix(s string) Renderer
func (r Renderer) ShowValue(b bool) Renderer
func (r Renderer) NeedleFollowsZone(b bool) Renderer
func (r Renderer) Render(idGen c.WidgetIdCreatorI, value float64) // consumes idGen.Derive() once

func TrafficLight(min, max float64) []Zone // ToneSuccess/Warning/Error bands, each Labeled
```

Value receiver, copy-returning setters, defaults in `New`, `Render(idGen, value)` consuming `idGen.Derive()` exactly once — the `distsummary` idiom verbatim. Setters accept and normalize bad input (e.g. `Diameter` ≤ 0 falls back to the `size` derivation; `nil` `Format` is a no-op). Stateless: no package `instanceStates` map, since a read-only dial keeps nothing across frames.

### SD2 — The IDS paint recipe (no literal colors, sizes, or strokes)

Every value is a token, bridged with `color.Hex(token.AsHex())`:

| Part | Color token | Size / width |
|------|-------------|--------------|
| Unzoned track | `NeutralBorderFaint` | band thickness ≈ 0.12·radius (derived; no token covers thick arcs) |
| Zone band | `zone.Tone.Fill()` | same band thickness |
| Major ticks | `NeutralTextSecondary` | `StrokeRegular` (1.5) |
| Minor ticks | `NeutralBorderFaint` | `StrokeHair` (1.0) |
| Tick labels | `NeutralTextSecondary` | `ScaledPt(CaptionPt, density)` |
| Needle | `NeutralTextPrimary` (or active `Tone.Fill()` if `needleFollowsZone`) | `StrokeStrong` (2.0) |
| Hub | fill `NeutralBgSurface`, stroke `NeutralBorderDefault` | `StrokeRegular` |
| Value readout | `NeutralTextExtreme` | `ScaledPt(DisplayPt, density)` |
| Metric label | `NeutralTextSecondary` | `ScaledPt(CaptionPt, density)` |

The band thickness is the one *dimension* with no token (the stroke tokens are 1–2 pt, for borders); it is derived as a fraction of the radius so the dial scales coherently. `density` and `accessibility` are read once in `New()` (the `boxenplot.New` precedent), not per frame.

### SD3 — Sweep geometry & value→angle mapping

Angles use the field/ECharts convention — **degrees, 0° at 3 o'clock, counter-clockwise positive** — converted to egui's y-down screen space inside the arc sampler. Default `startDeg = 225`, `endDeg = −45` sweeps the bottom-gap 270° dial. `valueToAngle(v)` linearly maps `[min,max] → [startDeg,endDeg]` and **clamps** the *needle* to the sweep; the *readout* always shows the true value (an out-of-range value is flagged in the readout, never silently pinned). The arc is sampled at a fixed angular step (≈1°) into the `xs,ys` of a single `PaintPolyline` per band.

### SD4 — Sizing: `SizeE` preset + explicit override; internal, not a surface archetype

[ADR-0065](./0065-imzero2-design-system-surface-sizes.md) tokenizes *window/surface* sizes; a gauge dial is widget-internal, which §SD5 there says is a local constant, not an archetype. So `SizeSm/Md/Lg` map to a small local diameter ladder (logical pt), density-scaled like the typography/spacing ladders. Proposed starting ladder (to calibrate against the first real consumer, à la `SurfaceInspector`'s open caveat):

| `SizeE` | Tight | Standard | Roomy |
|---------|------:|---------:|------:|
| `SizeSm` | 88 | 96 | 112 |
| `SizeMd` | 132 | 144 | 168 |
| `SizeLg` | 192 | 208 | 240 |

`Diameter(px)` overrides the preset for content-driven sizing (ADR-0065 §SD5 "guidance, not a straitjacket").

### SD5 — Zones: semantic tone + label, with presets

`Zone{From, To, Tone, Label}` over `styletokens.Tone`. `zoneMode` switches whether `From/To` are absolute values or `[0,1]` fractions of `[min,max]` (Grafana's Absolute/Percentage). Empty `zones` ⇒ a single `NeutralBorderFaint` track. `TrafficLight(min,max)` returns three equal `ToneSuccess`/`ToneWarning`/`ToneError` bands, each with a default `Label` ("ok"/"warn"/"critical") so the classic dial is WCAG-safe out of the box. `needleFollowsZone` colors the needle with the active zone's `Tone.Fill()` (color-by-value); default off (neutral needle) for restraint.

### SD6 — Painter mechanics

`Render` allocates the square via `AllocateUiAtRect(x, y, x+d, y+d).KeepIter()`, emits the paint commands in z-order (track/zone bands → minor ticks → major ticks → tick labels → needle → hub → readout → label), then flushes with `PaintCanvas(idGen-derived-id, d, d)`. Each zone band and the unzoned track are one thick `PaintPolyline`; ticks are `PaintLine`; the needle is `PaintArrow` (or `PaintLine` + the hub `PaintCircleFilled`); labels and readout are `PaintText` (anchor `1/1` = center/center for the readout, computed anchors around the rim for tick labels). Coordinates are canvas-relative; the center sits at `(d/2, slightly above d/2)` to balance the bottom gap of a 270° sweep.

### SD7 — Accessibility (carries the [ADR-0031](./0031-imzero2-design-system-color.md) §SD5 obligation)

The numeric value is **always** rendered as text (`showValue` defaults true and is documented as not meant to be disabled for a11y-critical surfaces), and every zone carries a `Label` (redundant encoding beyond color). `New()` caches `AccessibilityFromEnv()`; under high-contrast it widens band thickness / lifts readout contrast (the `boxenplot` precedent of tweaking by accessibility). A **monochrome** remap of tone bands to a `SequentialDefault()` ramp (which adapts to `IDS_ACCESSIBILITY`, where fixed tone hues do not) is **Deferred** — labels carry redundancy in the interim.

### SD8 — Testable internals (the `distsummary` test ethos: pure functions, no headless render)

`valueToAngle(v, min, max, startDeg, endDeg)` (clamping), `resolveZones(zones, mode, min, max)` → absolute, `zoneAt(v, zones)`, `arcPoints(cx, cy, r, startDeg, endDeg, segPerDeg)` → `xs,ys`, `tickAngles(min, max, major, minor, startDeg, endDeg)` → angles + labels, and the `SizeE`×density → diameter lookup. Tests assert constructor defaults, fluent-setter immutability (value-receiver returns copies), monotonic value→angle, zone boundary resolution, and tick placement. No egui rendering is exercised.

### SD9 — Demo (ADR-0057)

One `registry.Demo{}`, Category `"Widgets"`, showing a row of dials: a bare `Gauge("cpu", v)`, a `TrafficLight` dial, a `Percentage`-zoned dial with a custom `Suffix`, and a `needleFollowsZone` variant — exercising the IDS recipe across tones/sizes. Capture is automatic.

## Deferred (IDS "defer until needed")

- **Linear / bullet renderer** — the Few-optimal, data-ink, stack-many form; the value-on-range model is built to take it behind a `style` switch. The most likely next cut.
- **Arc-fill / progress mode** (ECharts `progress`): a thick `PaintPolyline` from `startDeg` to the value angle instead of a needle; additive.
- **Interactive knob mode** — stays with `imgui_knobs`, or a later `Editable(*float64)` mode if a single value-set surface wants it.
- **Plot-substrate filled wedges** — crisp annular sectors / round caps via `egui_plot` `PlotPolygon`, only if a future need wants them.
- **Monochrome / high-contrast zone remap** to `SequentialDefault()` (SD7).
- **Tethered inspector / value history** ([ADR-0046](./0046-imzero2-value-inspector-infrastructure.md)) — a dial has no hidden depth in v1; add only if a value's provenance/history needs popping.

## Alternatives

The options are weighed per-question in [Design space (compact)](#design-space-compact); the rejected/deferred ones, and why:

- **Linear / bullet gauge** (Q1a) — the data-ink-optimal, accessibility-preferred form (Few). Not chosen for v1 by maintainer preference for the recognizable radial KPI form, but kept as the most likely *second* renderer behind a `style` switch (see Deferred).
- **180° semicircle / progress-ring geometry** (Q1c/d) — narrower variants of the same value-on-range model; no advantage over the 270° dial for v1.
- **Interactive knob** (Q2b/c) — rotary *input* is already covered by the maintainer's `imgui_knobs` port, so the gauge stays read-only and `Render(idGen, value)` mutates nothing.
- **`egui_plot` substrate with filled wedges** (Q3b) — crisp annular sectors and round caps, but a needle dial needs only the stroked arc bands the painter draws directly; filled-wedge zones are deferred.
- **Raw `color.Color` zones** (Q4a) — rejected for semantic `styletokens.Tone` + `Label`, which is IDS-idiomatic, theme-aware, and carries the WCAG-required redundant channel.

## Consequences

### Positive

- Native to the painter — no new Rust primitive, no `egui_plot` baggage; a needle dial is the best fit for the stroked-only command set.
- Fully IDS-governed: tones for zones, neutral/text tokens for parts, type-scale × density for text, stroke tokens for lines. Consistent with every other widget; designlint-clean.
- Stateless and idiomatic: the `distsummary` `Renderer` contract, trivially testable pure internals.
- WCAG-honest: value-as-text + labeled zones satisfy [ADR-0031](./0031-imzero2-design-system-color.md) §SD5 even with a traffic-light palette.

### Negative

- A radial dial is the lower-data-ink form (Few's critique); we ship it first by maintainer choice and owe the linear/bullet renderer to make the accessible form available.
- Arc bands are *stroked-polyline approximations*, not vector arcs: very wide bands on a tight radius can look faceted at coarse sampling and have butt (not round) caps. Mitigation: ~1°/segment sampling; round caps need the Plot substrate (Deferred).
- The `SizeE` diameter ladder (SD4) is unvalidated by a real consumer until the first one lands — same caveat as `SurfaceInspector`.

### Neutral

- Depends on `styletokens.Tone` ([ADR-0031](./0031-imzero2-design-system-color.md) §Updates) — this widget is the second tone-painting consumer that motivated the promotion; it imports `styletokens`, not `badge`.
- Band thickness is the one derived dimension with no token (a fraction of radius), by design.

## Status

Accepted — 2026-06-08 (reviewed by p@stergiotis). The gauge has landed in [`widgets/gauge`](../../public/thestack/imzero2/egui2/widgets/gauge/) (build/test/vet green, screenshot-verified); the ADR-0031 §Updates tone promotion it depends on landed 2026-06-06.

Open questions:

1. **Tone home — resolved.** [ADR-0031](./0031-imzero2-design-system-color.md) §Updates resolved this to `styletokens.Tone` (landed 2026-06-06, build/test/vet green); the gauge's `Zone.Tone` is `styletokens.Tone`.
2. **`SizeE` ladder values (SD4).** The proposed diameters are unvalidated; calibrate against the demo and the first real consumer.
3. **Default tick count.** With `majorTicks = 0`, derive how? Proposed: a "nice" count from the range (≈5–7 majors) with min/max always labeled. Confirm at implementation.
4. **`needleFollowsZone` as a default?** Off (neutral needle) is the restrained choice; revisit if maintainers find the color-by-value default more legible on real dashboards.
5. **Band cap aesthetics.** If butt-capped polyline bands read poorly at `SizeLg`, the Plot-substrate wedge path (Deferred) is the escalation, not a per-segment hack.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only.

## Updates

### 2026-06-07 — needle reshaped to a neutral rhomboid; `needleFollowsZone` removed

Two changes to the needle; the radial-dial decision and everything else above stand.

- **Shape.** The needle is now a **filled rhomboid silhouette** — a long thin kite (tip at the band, widest at the shoulders just past the hub, a short counterweight tail behind it) — instead of a single stroked `PaintLine`. It is emitted as one `PaintPolygonFilled` in canvas-relative coordinates via a new `needlePolygon` helper (`internal.go`; geometry as `needleHalfWidthFrac` / `needleShoulderFrac` / `needleTailFrac` of the radius), drawn behind the hub cap. This **corrects a premise** in the body: the painter *does* expose a filled-polygon primitive — `PaintPolygonFilled` (also used by `layeredgraph`'s arrowheads) — so the §Design-space "no arc and no filled-polygon primitive" note and SD6's "the needle is `PaintArrow` (or `PaintLine`)" hold only for the *arc bands* now (still stroked `PaintPolyline`, because there is genuinely no native **arc**); the needle is filled geometry.
- **Color.** `needleFollowsZone` — the struct field, the `NeedleFollowsZone` setter, and its demo/test uses — is **removed**. The needle is now **always** neutral `NeutralTextPrimary` ink and encodes the value by **angle and shape only**; color stays a single, band-only channel. This resolves **Open question #4** in the permanent-neutral direction: color-by-value on the needle is redundant with the labeled zone bands and adds a second, weaker color channel that cuts against the SD7 "color is never the sole signal" discipline. The SD2 table's "Needle" row is now simply `NeutralTextPrimary`, *filled* (not `StrokeStrong`).

Built, `go test` / `go vet` green, and screenshot-verified via the widgets TestDriver (ADR-0057) at all three demo dials (plain / TrafficLight / large tone-zoned) — the needle reads as a neutral monochrome pointer in every case, including when it points into a colored zone.

### 2026-06-09 — readout auto-fit; the dial now keeps a small per-instance measurement cache

The center value readout now shrinks to fit the dial's inner opening. The first real consumer — the componentview battery dial ([ADR-0075](./0075-leeway-typed-component-views.md)) — exposed the gap: a four-digit value plus a unit suffix ("8500 mAh") at the `DisplayPt` readout size overran the arc band and collided with the interior `2500`/`7500` tick labels, because the readout was placed center-anchored at a fixed font with no width check. `paintValue` now measures the readout with egui's `MeasureText` and scales the font down so the string fits the chord of the band's inner edge at the readout's height (`fit.go`: `readoutAvailWidth` for the geometry, `fitFontForWidth` for the clamp). It never upscales past the design `DisplayPt` and floors at `readoutMinFontFrac` of it, so short readouts ("78%", "240 ms") are untouched and a very small dial still renders a legible value.

This **supersedes the statelessness claims** in the body — Context ("a read-only dial has no cross-frame state, so v1 is fully stateless (no package `sync.Map`)"), SD1 ("Stateless: no package `instanceStates` map"), and Consequences ("Stateless and idiomatic"). `MeasureText` returns its width through a databinding populated on the next `StateManager.Sync` — one frame late — so the widget now keeps exactly one piece of cross-frame state: a package `fitStates sync.Map` of per-instance readout measurements, keyed by the stable canvas-id hash (the `colorscale` cachingMeasurer + `distsummary` instanceStates precedent). The first frame a readout string is seen uses an approximation and the dial settles to the exact fit on the next; the map holds one entry per live dial (keyed by instance, not value, so changing telemetry does not grow it) and is not evicted. The configured `Renderer` itself stays immutable and copied by value, and the pure scaling decision (`fitFontForWidth`) is unit-tested per SD8.

Independently, the battery consumer (`componentview/renderers.go`) switched off the generic `TrafficLight` preset — which reads low as "ok"/green and high as "critical"/red — to charge-appropriate reversed zones (red < 20%, amber 20–50%, green > 50%, percentage mode) so a full pack reads green and a flat one red; `TrafficLight`'s own doc flags this as the "high is the good end" case. That is a consumer change, not a widget change, but it is what surfaced the four-digit-mAh overflow. The consumer also pins an explicit `Diameter(115)` — the first calibration of the SD4 ladder against a real consumer (Open question #2), an override rather than a `SizeE` preset.

Built, `go test` / `go vet` green (with new `fit.go` pure-helper tests), and screenshot-verified via the widgets TestDriver: the gauge demo's three dials (240 ms / 78% / 88°C) are unchanged, and the componentview battery dials fit "8500 mAh" / "900 mAh" inside the dial at both the default and the 115 px diameter — full charge green, low charge red.

## References

- [ADR-0031 — ImZero2 design system: color foundations](./0031-imzero2-design-system-color.md) — §Updates 2026-06-06 promotes `styletokens.Tone`, which this widget consumes for zones; §SD5 is the accessibility rule SD7 carries.
- [ADR-0030 — ImZero2 design system: typography](./0030-imzero2-design-system-typography.md) — the type scale + `ScaledPt(density)` used for the readout / tick / label text.
- [ADR-0032 — ImZero2 design system: spacing, density, motion](./0032-imzero2-design-system-spacing-density-motion.md) — `DensityE` / `DensityFromEnv()` and the stroke tokens.
- [ADR-0035 — keelson namespace introduction](./0035-keelson-namespace-introduction.md) — the keelson/thestack layering that makes the `color.Hex(token.AsHex())` bridge necessary.
- [ADR-0065 — ImZero2 design system: surface size archetypes](./0065-imzero2-design-system-surface-sizes.md) — §SD5 (internal sizes are local constants, not archetypes) and the finite-sizing rule SD4 follows.
- [ADR-0057 — demo registry and drivers](./0057-demo-registry-and-drivers.md) — the `registry.Demo{}` + TestDriver capture path SD9 registers with.
- [ADR-0067 — ImZero2 canonicaltype entry + tethered type inspector](./0067-imzero2-canonicaltype-entry-and-tethered-inspector.md) — the freshest value-receiver `Renderer` + demo-registration template SD1/SD9 follow.
- [ADR-0046 — ImZero2 value inspector infrastructure](./0046-imzero2-value-inspector-infrastructure.md) — the tethered-inspector path a value-history variant would use (Deferred).
- [ADR-0061 — ImZero2 imzrt Go runtime dashboard](./0061-imzero2-imzrt-go-runtime-dashboard.md) — the observability surface family the gauge joins.
- [`treemap.go`](../../public/thestack/imzero2/egui2/widgets/treemap/treemap.go) — the custom-paint precedent (`AllocateUiAtRect` → `Paint*` → `PaintCanvas`).
- [`imgui_knobs`](https://github.com/stergiotis/imgui_knobs) — the maintainer's Go port of `imgui-knobs`; the rotary *input* control that makes the gauge read-only (Q2).
- ECharts gauge series — source of the field-standard 270° default sweep (`startAngle 225 → endAngle −45`). [https://echarts.apache.org/en/option.html](https://echarts.apache.org/en/option.html#series-gauge).
- Few, S. (2006). *Information Dashboard Design* — the bullet-graph critique of radial gauges motivating the deferred linear/bullet renderer. [https://en.wikipedia.org/wiki/Bullet_graph](https://en.wikipedia.org/wiki/Bullet_graph).
