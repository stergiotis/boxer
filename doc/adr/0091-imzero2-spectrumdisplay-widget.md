---
type: adr
status: accepted
date: 2026-06-20
reviewed-by: "@spx"
reviewed-date: 2026-06-21
---

# ADR-0091: ImZero2 spectrumdisplay widget — RF/audio spectrum-analyzer display

## Context

We want a **spectrumdisplay** widget under
[`public/thestack/imzero2/egui2/widgets/`](../../public/thestack/imzero2/egui2/widgets/):
a professional spectrum-analyzer *display* — a scrolling waterfall with a labelled
**frequency axis**, a **power/dB axis**, a **colorbar legend**, an optional spectrum
**line trace**, **annotations** (markers, named frequency regions, a tuned-channel
line), and a **cursor readout** in physical units. The motivating consumer is a
Micronet SDR receiver in the sibling `sailing` project, but the widget is generic:
RF waterfalls, audio spectrograms, thermal/rolling-metric heatmaps. A clean-room
survey of established SDR displays (sdrangel, SDR++) fixed the user-facing
requirement set; nothing of their GPL-3 implementation is reproduced.

The decisive finding is that **almost every part already exists in the tree** and the
widget is a composition, not a greenfield build:

- **Waterfall**: [`heatmapscroll`](../../public/thestack/imzero2/egui2/widgets/heatmapscroll/)
  already wraps `colormap` + the Rust `scrollingTexture` opcode
  ([ADR-0058](./0058-imzero2-scrolling-texture-widget.md)). It has `SetDisplaySize`
  (stretch the texture to a pixel rect — window scaling), `SetOrientation(ScrollDown)`
  (the RF-waterfall direction), `PushColumn`, and `HoveredCell()` (cursor → ring cell).
- **Colorbar**: [`colorscale`](../../public/thestack/imzero2/egui2/widgets/colorscale/)
  renders a gradient strip + `finddivisions` ticks + labels + a hover marker from a
  `colormap.Config`. (Its vertical orientation, reserved but unbuilt, is implemented as
  a precondition of this widget — a same-shape follow-up, not an ADR-level decision.)
- **Tick engine**: [`finddivisions`](../../public/math/numerical/finddivisions/)
  (`AxisLayout`, `IterateTicks`, `MapToScreen`, `Heckbert`/`Talbot`/`TalbotLogarithmic`).
  The widget adds only an **engineering-unit formatter** (Hz→kHz/MHz/GHz; dB; s→ms/µs)
  on top — not a new tick algorithm.
- **2D chrome**: the egui2 painter (`PaintCanvas`/`PaintLine`/`PaintDashedLine`/
  `PaintRectFilled`/`PaintText` with H/V anchors/`PaintSenseRegion`) plus
  `AllocateUiAtRect`, `MeasureText`, and `StateManager.GetAvailableSize()` /
  `GetCanvasPointer()`. [`treemap`](../../public/thestack/imzero2/egui2/widgets/treemap/)
  and [`gauge`](./0068-imzero2-gauge-widget.md) are the custom-paint precedents.

Forces specific to this widget:

- **The frequency axis cannot be delegated to `egui_plot`.** The `Plot` binding exposes
  `YGridMarks(values,labels)` but **no X-axis tick formatter and no second/log axis**
  (verified against `methods.out.go`). A frequency (X) ruler with MHz labels must
  therefore be painter-drawn. This single gap pushes the whole design onto the
  painter-overlay substrate rather than a plot.
- **Gutters must stay pixel-aligned with the texture, every frame.** `FetchR21UiRects`
  carries a one-frame lag and is meant for cross-scope affordances; using it to place
  axis gutters would make them jitter against the texture on resize.
- **Hover is inherently one frame late.** `GetCanvasPointer()` is populated by the
  previous Sync; the cursor readout and markers lag one frame (imperceptible), and
  inline `FetchR14CanvasPointer` deadlocks inside deferred-block captures (dock tabs),
  so the cached read is mandatory (the `colorscale` discipline).

Invariants the design respects: painter idiom (`AllocateUiAtRect` → `Paint*` →
`PaintCanvas`, canvas-relative coordinates); finite sizing (no `INFINITY` ballooning);
demo-registry screenshot ([ADR-0057](./0057-demo-registry-and-drivers.md)); no Rust or
IDL change for any 2D part.

## Design space (QOC)

| Question | Options | Choice & why |
|---|---|---|
| Q1 Axis/tick substrate | (a) **pure-Go painter overlay**; (b) new Rust spectrum widget; (c) `egui_plot` only | **(a)** — composes verified painter primitives, zero Rust, ships now. (c) is impossible: `Plot` has no X-axis tick formatter / no second/log axis. (b) is a large Rust surface, deferred. |
| Q2 Sub-region placement | (a) **deterministic `AllocateUiAtRect` from one available-size number**; (b) per-widget `FetchR21UiRects` | **(a)** — same-frame pixel alignment of gutters↔texture (the `treemap` idiom). (b) lags a frame on resize → jitter. |
| Q3 Tick algorithm | (a) **reuse `finddivisions` + a thin SI/engineering formatter**; (b) hand-rolled nice-numbers | **(a)** — `finddivisions` is tested and already used by `colorscale`; only the engineering-suffix layer is new. |
| Q4 Colorbar | (a) **reuse `colorscale` (implement its reserved vertical mode)**; (b) inline gradient in this package | **(a)** — DRY; one gradient source (`colormap.Config.At`) for data and legend; unblocks `colorscale`'s own vertical mode. |
| Q5 Waterfall relationship | (a) **own a `heatmapscroll`**; (b) re-implement texture streaming | **(a)** — `heatmapscroll` already encapsulates the `scrollingTexture` opcode, `SetDisplaySize` stretch, and hover-back-to-slot mapping. |
| Q6 Hover readout | (a) **`GetCanvasPointer()` cache (1-frame lag)**; (b) inline `FetchR14CanvasPointer` | **(a)** — (b) deadlocks inside dock-tab captures; the cached read is the documented discipline. |

## Decision

Build **`spectrumdisplay`** under `widgets/`: a composite that **owns** a
`heatmapscroll` (waterfall) and a vertical `colorscale` (colorbar), reuses
`finddivisions` for both axes, and paints the frequency/power/time axes, the
annotation/region overlay, and the cursor crosshair on transparent
`AllocateUiAtRect`+`PaintCanvas` overlays placed deterministically from the available
size. Pure Go; no Rust for the 2D path. Register a screenshot demo
([ADR-0057](./0057-demo-registry-and-drivers.md)).

## Subsidiary design decisions

### SD1 — Public API (stateful, pointer-receiver `New` idiom — the heatmapscroll/colorscale shape)

```go
package spectrumdisplay // public/thestack/imzero2/egui2/widgets/spectrumdisplay

// New owns a heatmapscroll (widthSlots time × heightSlots freq bins) and a
// vertical colorscale, both bound to cfg so waterfall and legend stay in lock-step.
func New(ids *c.WidgetIdStack, scopeKey string, cfg *colormap.Config,
        widthSlots, heightSlots uint32) *SpectrumDisplay

// Each frame the caller supplies physical ranges, pushes a dB column, and renders:
func (inst *SpectrumDisplay) SetFrequencyAxis(a AxisSpec)   // Hz across the texture
func (inst *SpectrumDisplay) SetPowerAxis(a AxisSpec)        // dB; left gutter + colorbar
func (inst *SpectrumDisplay) SetWaterfallRange(min, max float64) // colormap range, independent of the line panel
func (inst *SpectrumDisplay) SetMarkers(m []Marker)         // vertical/horizontal/crosshair lines
func (inst *SpectrumDisplay) SetRegions(r []Region)         // named freq bands + the demod passband bracket
func (inst *SpectrumDisplay) SetSplitRatio(f float32)       // FFT↔waterfall height split (when the line panel shows)
func (inst *SpectrumDisplay) SetDisplaySize(wPx, hPx float32) // 0,0 ⇒ GetAvailableSize()
func (inst *SpectrumDisplay) PushColumn(s []float32) colormap.ColumnStats
func (inst *SpectrumDisplay) Render()
func (inst *SpectrumDisplay) Readout() Readout             // freq+dB+age under the cursor (1-frame lag)
```

`AxisSpec{Min,Max float64; Unit AxisUnitE; UnitLabel string; Reversed bool; DesiredTicks int}`;
`Marker{Kind MarkerKindE; Freq,Db float64; Color uint32; Label string; Dashed bool}`;
`Region{StartHz,EndHz float64; Label string; Color uint32; Placement PlacementE}`;
`Readout{Freq,Db float64; Age int; BinRow,RingCol uint32; Ok bool}`. Named-type enums end
`E` (`AxisUnitE`, `MarkerKindE`, `PlacementE`).

### SD2 — Deterministic layout (`layout.go`, pure)

`partition(W,H,opts) → rects` splits the box into: left dB/time gutter (width =
widest label via a cached measurer), bottom frequency gutter (height = font + tick
margin), the texture rect (→ `heatmapscroll.SetDisplaySize`), the colorbar strip +
labels, and an optional spectrum-line subpanel sized by the split ratio. Every rect
derives from the same `(W,H)` in the same frame, so nothing lags the texture. The
function is GUI-free and unit-tested.

### SD3 — Engineering tick formatter (`ticks.go`, pure)

`AxisTicks(a AxisSpec) (positions []float64, labels []string, view finddivisions.AxisLayout)`
runs `finddivisions` over `[Min,Max]` then formats each tick with `engFormat`, which
picks **one** SI scale for the whole span (Hz→kHz/MHz/GHz, s→ms/µs/ns; dB/generic
identity) so all ticks share a suffix. Pure and table-driven; unit-tested.

### SD4 — Annotation/region overlay

`Marker`s are vertical/horizontal/crosshair lines (solid or dashed) at physical
coordinates; `Region`s are shaded named frequency bands (top/bottom placement) that
subsume both a "tuned channel" line and a demod-passband bracket — the generic form of
the band-plan and VFO-box ideas from the survey. All are drawn on the texture overlay
canvas in painter space mapped through the frequency axis.

### SD5 — Colorbar via `colorscale` vertical

The owned `colorscale` is constructed `WithOrientation(OrientationVertical)`, sharing
the waterfall's `*colormap.Config`; its dB ticks come from the same range as the
left gutter, so strip, gutter, and texture colors are consistent by construction.

## Alternatives

- **New Rust spectrum widget** — rejected for the 2D display: the painter substrate is
  sufficient and ships with no IDL/Rust change. (A custom-GPU 3D spectrogram surface is
  a genuinely separate, deferred project with its own ADR; out of scope here.)
- **`egui_plot` for the whole display** — rejected: no X-axis tick formatter, no
  second/log axis, and the waterfall is a texture, not a plot series.
- **Inline colorbar in this package** — rejected in favor of reusing `colorscale`
  (DRY, single gradient source); the inline recipe remains the fallback if the
  `colorscale` vertical path ever regresses.
- **Re-implement texture streaming** — rejected: `heatmapscroll` already owns it.

## Consequences

### Positive
- A reusable, demoable spectrum-analyzer display composed from four existing packages
  (`heatmapscroll`, `colorscale`, `finddivisions`, `colormap`); **no Rust changes**.
- Pure helpers (`layout`, `ticks`, hover→physical math) are unit-tested without a GUI.
- Window scaling, MHz/dB axes, colorbar, markers/regions, and cursor readout in one
  widget any SDR/spectrogram app can adopt.

### Negative
- Cursor readout and markers carry the unavoidable one-frame hover lag (canvas-pointer
  cache + capture/fetch discipline).
- `Readout.Db` is an axis-position proxy unless the caller retains the f32 column
  (`heatmapscroll` does not), as in `colorscale`.
- Requires the small `colorscale` vertical-orientation addition (its own reserved mode).
- May motivate promoting `colorscale`'s `cachingMeasurer` into a shared internal package
  rather than copying it.

### Neutral
- Depends on `heatmapscroll`/`colorscale`/`colormap`/`finddivisions`. The 3D spectrogram
  surface and the `Plot` X-axis formatter / log axis are explicitly **out of scope**,
  tracked separately.

## Status

Accepted — 2026-06-21 (reviewed by @spx). AI-drafted.

Status lifecycle: `proposed → accepted → (deferred | deprecated | superseded by ADR-XXXX)`.

## Updates

### 2026-06-21 — IDS theming, shared `axisruler`, and a label-clipping fix

Three first-use corrections after the widget was exercised on a real scene; the
SD1–SD5 decisions stand, so `status` / `reviewed-date` are not re-stamped.

- **Chrome now reads as one surface (IDS tokens).** The hand-picked hex defaults
  (`DefaultBg = 0x12121a`, axis/label/grid greys, …) were three near-but-unequal
  darks: the gutters/line-panel (`0x12121a`), the colorbar
  ([`colorscale`](../../public/thestack/imzero2/egui2/widgets/colorscale/)'s
  `0x1a1a22`), and the egui panel behind (`NeutralBgPanel`) — so every sub-rect of
  the widget was individually visible. All chrome colors are now sourced from the
  IDS neutral spine / semantic palette ([ADR-0031](./0031-imzero2-design-system-color.md)):
  gutters, line panel, and colorbar all paint `NeutralBgPanel`, so the composite
  shows no internal seams against a panel. `colorscale.DefaultBg` was migrated to
  `NeutralBgPanel` fleet-wide (it is a shared legend); axis baselines/ticks use
  `NeutralBorderFaint`, axis labels `NeutralTextSecondary`, annotation labels
  `NeutralTextPrimary`, the trace `InfoDefault` — the same tokens the
  [`timeline`](../../public/thestack/imzero2/egui2/widgets/timeline/) axis uses.

- **Axis labels render through a shared `axisruler`.** The gutter painting (a
  baseline, tick marks, and anchored labels) was duplicated here and in the
  timeline. It is now one leaf package,
  [`axisruler`](../../public/thestack/imzero2/egui2/widgets/axisruler/): the caller
  computes ticks (this widget via `finddivisions`, the timeline via `timeticks`)
  and hands `[]Tick{Pos,Label}` + a `SideE` + a `Style` to `axisruler.Paint`, which
  owns the visual treatment. Both the frequency gutter (`SideBottom`) and the
  dB/time gutter (`SideLeft`) now go through it, and `timeline.paintAxis` was
  retrofitted onto it with geometry preserved (the calendar rollover rows remain
  the timeline's own concern). This is the "reuse the timeline's axis labels for
  the frequency/range axes" follow-through.

- **Labels no longer clip — the SD2 "widest label" rule is now implemented.** SD2
  specified a left-gutter "width = widest label via a cached measurer" but the
  first cut used a fixed `DefaultLeftGutterW = 48`; the bottom gutter was a too-
  short 18 px and the end ticks centered (so a frequency label at the colorbar
  edge overhung). The left gutter is now sized per frame from the widest
  power/time label (`leftGutterWidth`, an ASCII-width estimate clamped to
  `[28,96]`), the bottom gutter is 22 px (descender room), and `axisruler`
  edge-anchors the first/last label inward so it stays inside the rect.

The demo gained a collapsed **Features** bullet list enumerating the widget's
capabilities.

## References

- [ADR-0058 — ImZero2 scrolling-texture widget](./0058-imzero2-scrolling-texture-widget.md) — the `heatmapscroll` backing opcode.
- [ADR-0068 — ImZero2 gauge widget](./0068-imzero2-gauge-widget.md) — the painter-widget precedent (`AllocateUiAtRect`→`Paint*`→`PaintCanvas`).
- [ADR-0057 — Demo registry and drivers](./0057-demo-registry-and-drivers.md) — screenshot-demo registration.
- [`colorscale`](../../public/thestack/imzero2/egui2/widgets/colorscale/), [`heatmapscroll`](../../public/thestack/imzero2/egui2/widgets/heatmapscroll/), [`finddivisions`](../../public/math/numerical/finddivisions/), [`colormap`](../../public/thestack/imzero2/egui2/widgets/colormap/) — composed packages.
- `sailing` ADR-0004 — the comprehensive-spectrum-analyzer consumer that motivates this widget.
