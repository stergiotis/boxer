---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Adoption survey, compiled 2026-07-31,
> the day after [ADR-0149](../adr/0149-implot-core-port-painter-lane.md)'s
> SD7 migration completed and the egui_plot bridge was retired. Claims about
> the tree (package sizes, consumer lists, API surfaces) were verified against
> the working tree on the compile date. This page proposes and ranks; nothing
> here is decided. A decision would land as a dated Update to ADR-0149 (for
> implot API additions) or its own ADR (for a widget rebuild).

# implot adoption survey: the remaining dataviz widgets

The ADR-0149 port shipped M0–M7 and migrated every egui_plot-bridge call
site (ecdf, boxenplot, distsummary, ecdfdigest, the imztop and imzrt
panels, play's projection pane, fibscope, terrainscope, the IDS showcase).
That was the *first* wave: call sites that were already plot-shaped.

The second wave is the widgets that never used the bridge — the bespoke
painter-lane visualizations that hand-roll some subset of axes, ticks,
zooming and hover. ADR-0149's context section named them (`axisruler`,
`spectrumdisplay`, `timeline`; `ecdf`/`boxenplot`/`distsummary` are done)
and its consequences section scoped them: "adoption candidates, not
casualties". This survey walks the candidate list, sorts it into
adopt / compose / leave-alone, and identifies the substrate gaps that
currently block the highest-value adoption.

## What implot offers an adopting widget

The seams that matter, beyond the item breadth listed in the package doc:

- **The interaction machine.** Drag pan, pointer-anchored wheel zoom,
  box-zoom, double-click fit, viewport constraints, follow-mode
  (`AxisFlagsFollow`), plus plot-space readback (`Clicked`,
  `HoverPlotPos`) and `NoInputs` for inert embeddings. Notably it
  consumes the same substrate registers (ADR-0140 wheel, R24 canvas
  pointer) that the bespoke widgets read directly today.
- **`SetupAxisTicks` — the tick-math bridge.** House tick algorithms
  keep their jobs by *feeding* implot rather than being replaced: imztop
  already drives implot y-axes with Talbot ticks from `finddivisions`.
  Calendar ticks from `timeticks` can ride the same seam;
  `TimeTicksLocal` covers the local-calendar default.
- **`colormap.Config` as shared currency.** `Heatmap` and `Histogram2D`
  take the same `*colormap.Config` that `colorscale`, `heatmapscroll`
  and `treemap` coloring use — a plot and its legend can already share
  one palette object; nothing demonstrates it yet.
- **Pixel-space-y precedent.** The `Digital` item renders bottom-pinned
  in pixel space while its x rides the plot transform. That hybrid —
  plot-space x, widget-managed pixel y — is exactly the shape a lane
  chart needs.
- **Sizing.** Plots size from the R18 available-size register and
  collapse gutters under `NoTickLabels`, down to sparkline scale.

And the gaps, verified against the package:

- **No custom-item seam.** The plot↔pixel transform is unexported and
  there is no in-plot draw hook — upstream's `PlotToPixels` /
  `GetPlotDrawList` idiom, the standard ImPlot extension mechanism, was
  not ported. Until it is, a bespoke widget cannot adopt the frame and
  keep its own items. This is the single blocking gap for the second
  wave.
- **No colorbar.** Upstream's `ColormapScale` was not ported and no
  in-tree demo composes the `colorscale` widget beside a plot.
- **No `SetupAxisFormat`.** No custom tick-formatter callback
  (engineering units like `spectrumdisplay`'s Hz/kHz/MHz);
  `SetupAxisTicks` covers the need by supplying labels wholesale.
- **Ship-once images.** The image route re-ships the whole RGBA raster
  on version bump — right for bounded heatmaps, wrong for per-column
  streaming (that job belongs to ADR-0058's `scrollingTexture`).
- Known deviations that stay relevant: horizontal-only y-axis label (no
  rotated-text paint command), text measurement deferred (SD6).

## Widget-by-widget

| Widget | Lines (w/ tests) | Substrate today | Verdict |
| --- | --- | --- | --- |
| `timeline` | 3.1k | painter canvas + axisruler + timeticks | **adopt the frame, keep the items** — blocked on the custom-item seam |
| `spectrumdisplay` | 1.0k | heatmapscroll + colorscale + axisruler composite (ADR-0091) | keep; defer any implot composition |
| `heatmapscroll` | 0.5k | `scrollingTexture` GPU ring (ADR-0058) | keep — implot's image protocol would regress streaming |
| `colorscale` / `colormap` | 1.0k / 0.7k | painter strip / palette LUT | **compose with implot; demonstrate** — cheap win |
| `axisruler` | 0.2k | stateless tick painter | keep; retirement only as a consequence |
| `treemap` | 3.8k | egui Frames + SenseClick + animated zoom | leave alone |
| `gauge` | 1.0k | painter arcs | leave alone |
| `timerangepicker` | 0.5k | presets/validation control | leave alone |
| `worldmap` / `basemap` | 1.3k / 0.1k | CPU raster + pixel picking | leave for now; geo needs its own dialogue |
| diagram family (`layeredgraph`, `pipelineview`, `scctree`, `fsmview`, `schemaview`, `mappingplanview`, `kanban`) | — | layout-driven | out of scope |
| `metricsoverlay` | 0.1k | HUD host | nothing to do (distsummary already migrated) |

### timeline — adopt the frame, keep the items

The widget is two things fused: a *plot frame* (time axis, calendar
ticks, drag pan, anchored wheel zoom with clamps, cursor readout — built
on the same ADR-0140 wheel and canvas-pointer registers implot uses) and
a *bespoke item set* (greedy lane packing, interval rects, flag stagger,
background bands, rug/density strip, now-line, rollover context rows,
selection and per-item hit testing). Of its 78 functions, roughly a
third are frame work that implot now provides; the item half is the
widget's actual value and has no implot counterpart.

The adoption shape: implot owns the x-axis (time scale, gestures, fit,
constraints, context menu), calendar ticks feed in via `SetupAxisTicks`
from `timeticks` (localization stays where it is), and the lane/flag/band
painters become custom items on the pixel-space-y pattern the `Digital`
item established — lanes never rescale vertically under zoom, exactly as
today. Hit testing and selection semantics stay widget-owned, reading
`HoverPlotPos` plus the existing canvas-pointer register.

What it would gain beyond deleted code: box-zoom, double-click fit,
viewport constraints, the native context menu, and one interaction idiom
shared with every migrated plot.

Risks, stated up front: the behavioral surface is large and committed
(play's timeline and detail-timeline panes, demo goldens, an open 13px
flag-spill issue), calendar-localized ticks via `SetupAxisTicks` are
exercised nowhere yet, and the rollover context rows live *above* the
plot area, so the custom-item seam must not clip custom drawing to the
plot area unconditionally. This is the largest and highest-value item in
the survey, it is blocked on E1 below, and it warrants a design dialogue
before code — not a weekend refactor.

### spectrumdisplay + heatmapscroll — keep the streaming substrate

`heatmapscroll` is not a painter-lane widget: it pushes single columns
into a GPU ring texture (`scrollingTexture`, ADR-0058) and never
re-uploads the backlog. implot's image route is ship-once-per-version —
adopting it for a waterfall means re-shipping the full raster every
scroll step. That is a regression, not a migration; the two protocols
serve different data shapes, and bounded/static heatmaps are already
served by `implot.Heatmap`'s rect and texture routes.

The genuinely attractive future — pan/zoom implot axes *around* a
streaming waterfall — needs a composition design that does not exist
yet (an implot frame hosting a foreign widget in its plot area, or a
painter-lane streaming-texture opcode). Deferred; the axis/tick code the
composite would shed is small, and its consumers (imzrt sched, imztop
cpu heatmap) lose nothing by waiting.

### colorscale + colormap — compose and demonstrate

`colormap` already *is* implot's palette engine (`heat.go` imports it) —
that half of the M4 "colormap integration" landed. What never landed is
the visible half: no in-tree example pairs an implot heatmap with a
`colorscale` legend, though both ends speak `*colormap.Config` and the
pairing is plain widget composition (the spectrumdisplay layout idiom).
The cheap win is a gallery demo: heatmap + vertical colorscale sharing
one Config, plus hover linkage (`colorscale.OnHover` highlighting the
hovered value band — the `HoverBand` decorator on treemap is the
precedent). An `implot`-side `ColormapScale` convenience can follow if
composition proves awkward; it should delegate to `colorscale`, not
duplicate it.

No locator unification is proposed: `colorscale` keeps `finddivisions`
(Talbot with typesetting scoring is better for its use than implot's
nice-number locator), `timeline` keeps `timeticks`, implot keeps its
ported locators. `SetupAxisTicks` is the standing bridge whenever house
tick math should drive an implot axis; three tick families coexisting is
a documented policy, not an accident.

### axisruler — keep; retirement is a consequence, not a goal

Deliberately dumb (the caller computes ticks; it paints baseline, marks,
edge-anchored labels) and small. It retires only if both consumers leave
— timeline via the adoption above, spectrumdisplay deferred — so it
stays, and it remains the right tool for tick rulers on canvases that
are not plots.

### Leave-alones, with reasons

- **treemap** — egui Frames with `SenseClick` and animated drill zoom;
  no coordinate axes anywhere. Its implot-relevant surface is already
  shared (`colormap` palettes, `colorscale.HoverBand` legend linking).
  The M0 batched-rect opcodes do not apply — it does not paint rects.
- **gauge** — radial axes; neither the port nor upstream ImPlot has
  them.
- **timerangepicker** — a validated picker control (presets, timezones),
  not a chart. A future "brush over a sparkline" affordance could use
  implot, but that is a new feature, not an adoption.
- **worldmap / basemap and the play geo panels** — raster composition
  and per-pixel picking. ADR-0149 SD5/SD6 already point their future at
  `paintImage` (geo underlays, CPU-rasterized concave fills); putting a
  graticule-bearing implot frame around a mercator raster is plausible
  but couples to the play map's raster contract (ADR-0096/0114) and
  deserves its own dialogue.
- **diagram family** (layeredgraph, pipelineview, scctree, fsmview,
  schemaview, mappingplanview, kanban) — layout-driven, no coordinate
  plots.

## Substrate gaps, ranked

- **E1 — custom-item lane** (the enabler). Export the plot↔pixel
  transform (`PlotToPixels` / `PixelsToPlot`), add a draw hook running
  with the plot's canvas active — clipped to the plot area by default,
  opt-out for gutter drawing — plus a way for custom items to
  participate in fit (`IncludeX`/`IncludeY` already exist) and, where
  wanted, the legend. Upstream-faithful (the `GetPlotDrawList` idiom is
  ImPlot's documented extension path), small, and it unblocks timeline
  and every future bespoke overlay.
- **E2 — `SetupAxisFormat`.** Callback tick formatting (engineering
  units). Cheap; optional as long as `SetupAxisTicks` suffices.
- **E3 — rotated text paint command.** Fixes the horizontal y-axis
  label deviation; benefits any axis-heavy widget.
- **E4 — text-measurement fetcher.** Stays deferred per SD6; the
  estimation idiom holds for numeric ticks.
- **E5 — `DraggedBy` response flag.** Restores upstream's right-drag
  box-zoom; recorded deviation, low urgency.

## New consumers (not migrations)

- **ADR-0150 M4 (timeseries anomaly).** Detector overlays are
  implot-shaped out of the box: `Shaded` anomaly windows, `TagX` at
  detections, `InfLinesH` thresholds, `AxisFlagsFollow` for rolling
  live windows. The loadstudy work has no UI yet; when it grows one, it
  starts on implot per SD7's "new chart work targets the port".
- Any new panel or widget needing axes starts on implot; the bespoke
  path remains for non-plot geometry (the diagram family's substrate).

## Proposed sequence

1. **P1 — colorbar composition demo** (small). Gallery demo pairing
   `implot.Heatmap` with a `colorscale` legend on one shared
   `colormap.Config`, including hover linkage. No API changes; it
   documents the intended pattern and finishes M4's integration story
   visibly.
2. **P2 — E1, the custom-item lane** (small, high leverage). Land with
   its own gallery demo (e.g. a custom-drawn overlay on a stock plot);
   record the API addition as a dated Update to ADR-0149.
3. **P3 — timeline onto the frame** (large). Design dialogue first,
   then the rebuild behind the existing public API, goldens updated
   deliberately. Calibration note: ADR-0149's effort estimate ran ~4×
   high; even so this is the largest single item in the survey.
4. **P4 — recorded deferrals.** Waterfall-axes composition, geo-frame
   dialogue, `ColormapScale` convenience, E2/E3/E5. Each is descoped
   rather than gating P1–P3.

Not proposed, deliberately: porting treemap/gauge/diagram widgets onto
implot (wrong substrate or no axes), moving `heatmapscroll` onto the
image route (streaming regression), unifying the three tick-locator
families (`SetupAxisTicks` bridges them at zero churn), or eagerly
deprecating `axisruler` (it still has two consumers and a role).
