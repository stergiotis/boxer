---
type: adr
status: accepted
date: 2026-05-19
reviewed-by: "p@stergiotis"
reviewed-date: 2026-05-19
---

# ADR-0043: ImZero2 timeline widget — calendar-axis interval + point visualization

## Context

The ImZero2 demo carousel had widgets for hierarchical data (`treemap`), streaming scalar fields (`heatmapscroll`), structured inspection (`fieldview`), and error chains (`errorview`), but no widget for *time-shaped* data — calendar-axis visualization of (a) **point events** like git commits, alerts, log lines, and (b) **interval events** like LLM sessions, deploys, scheduled tasks. The need was concrete: LLM session logs and git activity were the two most common things downstream apps (`imztop`, the planned Grafana-replacement panes) want to surface, and there was no in-tree pattern to copy.

A literature survey at the start of the work confirmed the canonical academic reference for *mixed* point + interval timelines is **LifeLines** (Plaisant et al., CHI '96, UMd HCIL) and its successor **EventFlow / LifeFlow** (Monroe et al. 2013): same axis, different glyphs (dot vs. bar). The richest survey is Aigner / Miksch / Schumann / Tominski, *Visualization of Time-Oriented Data* (2nd ed. 2023, mirrored at timeviz.net). Production tools converge on the same idioms — Grafana State Timeline, perfetto, ECharts, Plotly Gantt, Tableau, D3's `d3-layout-timeline`: rug strip for points (raw marks below threshold, density bins above), greedy left-to-right lane packing for intervals, calendar-aware tick ladder for the axis.

Forces shaping the decision:

- **No upstream renderer fits.** `egui_plot` is wired into the Rust side but isn't exported to Go bindings, and its series/transform model doesn't represent laned interval bars cleanly. `walkers` is the only crate that handles pan/zoom natively, and it does so entirely Rust-side via its own gesture stack. The wgpu escape hatch is explicitly out of scope per the Grafana-replacement memory.
- **Existing primitives exist for ~80% of what we need.** `boxer/public/math/numerical/timeticks` already produces the calendar-aware tick ladder. `widgets/timerangepicker` already exposes Grafana-style range presets on the same `(FromEpochMS, ToEpochMS, TzID)` wire shape. `PaintCanvas` already supports `paintRectFilled` / `paintLine` / `paintText` / `paintSenseRegion` + a whole-canvas `Sense(click, drag, hover)`. The Crameri batlow palette + sequential lookup is wired via `styletokens.Sequential`.
- **`StateManager.GetCanvasPointer` existed, but the rest of the input layer didn't.** Pan and zoom need scroll delta, modifier keys, and (for auto-fit) `ui.available_size`; none were surfaced to Go. The decision needs to either work around the gap or extend FFFI2.
- **Composite widgets are not ADR-0013 primitives.** The existing [[ADR-0013]] *Stateful widget contract* governs FFFI2 atomic primitives (Checkbox, Slider, …) — apply blocks routed through `applyCodeWidgetRustOnEvent`, `SendRespVal(*T)` databindings. Composite widgets (`treemap`, anything assembled from primitives + PaintCanvas) sit outside that contract; `treemap` already established the pattern of receiver-owned state + `c.IdScope(scopeKey)`. The timeline widget should *not* invent a third pattern.

Constraints inherited from the rest of the stack:

- **CPU-bound immediate-mode budget.** No GPU shaders, no Datashader-style out-of-process rasterization. Target ~100k visible events per pane (consistent with the Grafana-replacement scope), at 60 fps.
- **`int64` epoch-ms wire format.** Matches `timerangepicker.EvaluatedRange{FromEpochMS, ToEpochMS, TzID}`; consistent with how times move through the bus and ClickHouse query layer.
- **No `egui_plot` Go binding.** Adding one would be a multi-week project on its own and orthogonal to this widget.
- **No CGO.** Pure-Go layout code; Rust-side changes only in `interpreter.rs` + definition files.

## Design space (QOC)

**Question.** How should an ImZero2 widget visualise (point + interval) events on a single calendar axis, scale to ~10–100k events at 60 fps without GPU work, and acquire pan / zoom / auto-fit gestures despite the host framework not surfacing them today?

**Options.**

- **O1 — Bind `egui_plot` to Go and reshape it for laned rectangles.** Lift the Rust crate into the FFFI2 IDL; build the timeline atop `Plot::show` with `BarChart`/`Polygon` items for intervals and `Points` for events. Pan/zoom inherited from `egui_plot`'s native handlers.
- **O2 — Custom widget over `PaintCanvas` with a pure-Go layout package and ADR-first design (chosen architecture, develop-first method).** Lane-pack and LOD-bin in Go; paint rectangles + ticks + tooltips on a `PaintCanvas`. Add FFFI2 input fetchers as needed for gestures the framework doesn't yet expose.
- **O3 — Defer the widget; pipe events into the existing Grafana time-series panel.** Wait for ADR-0016's time-range-picker work to mature into a full Grafana-replacement panel and treat the timeline as one of its `panel.kind` variants.

**Criteria.**

- **C1 — Fit for laned interval data.** How natively the option represents *N independent rows of non-overlapping intervals* without bolting custom primitives onto a series model.
- **C2 — Cost to ship M0-shaped scope.** Engineering days from "no widget" to "demo carousel entry that renders a 3-day LLM-session fixture", end-to-end.
- **C3 — Gesture acquisition path.** How easily we can wire pan / zoom / auto-fit, given egui's input surface and FFFI2's existing fetcher pattern.
- **C4 — Visual control granularity.** Whether per-bar / per-tick coloring, lane labels, and crosshair fit the abstraction without fighting it.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 egui_plot | O2 PaintCanvas | O3 defer |
|----|:--:|:--:|:--:|
| C1 | −  | ++ | +  |
| C2 | −− | ++ | ++ |
| C3 | ++ | +  | ++ |
| C4 | −  | ++ | +  |

## Decision

We build a custom composite widget at `public/thestack/imzero2/egui2/widgets/timeline/`, layered as:

- **SD0 — Pure-Go layout package** (`widgets/timeline/layout/`). Wire types `PointEvent{TMS int64, KindID int32, Intensity float32}` and `IntervalEvent{FromMS, ToMS int64, KindID int32, Intensity float32, LaneHint string}`; algorithms `PackLanes` (greedy left-to-right), `LODIndex` (multi-resolution sparse bins), `TickMap` (wraps `boxer/public/math/numerical/timeticks` + maps to screen-x). 41 unit tests, `-race -count=2` clean. No UI dependency, fully testable from `go test`.

- **SD1 — Composite widget shell mirroring treemap.** `Timeline.New(ids, scopeKey, intervals, opts...)` returns a `*Timeline` whose caller holds the pointer across frames. `Render()` wraps the body in `c.IdScope(ids.PrepareStr(scopeKey))`. All state lives on the receiver; the widget does NOT use [[ADR-0013]]'s `SendRespVal` databinding contract because that contract applies only to FFFI2 atomic primitives defined in `egui2_definition_d_widgets.go`, not to composites. This invariant is captured in [[feedback_composite_widget_state]].

- **SD2 — `PaintCanvas` as the render surface.** Each frame the widget queues `paintRectFilled` (lane bars), `paintLine` (axis baseline + tick marks + cursor crosshair), `paintText` (tick labels + rollover rows + lane labels + tooltip), then commits one `PaintCanvas(...).Background(...).Sense(click=false, drag=false, hover=true)`. Tooltips paint *before* the canvas drain so they layer on top of bars in z-order.

- **SD3 — LifeLines-style band layout.** Vertical structure top-to-bottom: rug strip (height `defaultRugStripH = 24`, suppressed when no points) → gap → lane rows (`defaultLaneHeight = 22`, `defaultLaneGap = 4` between) → axis baseline (1 px) → tick marks (6 px) + tick labels (11 pt) → rollover rows (one per calendar unit transition the axis spans, 16 px each). Horizontally the time axis spans `[labelW, effW]` where `labelW` is sized to the longest `LaneHint` and zero when no lane has a hint.

- **SD4 — Rug-strip raw/density threshold.** When `len(visible_points) ≤ rawPointThreshold` (default 500) the strip paints one `paintLine` per visible `PointEvent` at `MapMSToX(TMS)`, tinted via `styletokens.Sequential(rugColormap, Intensity)`. Above the threshold the strip switches to one `paintRectFilled` per `LODIndex.BucketsForRange` bucket, tinted by `count / visible_max_count` — same Crameri batlow palette so the *encoding* of "more here = warmer" stays consistent across modes.

- **SD5 — Greedy lane packing with hint pinning.** `PackLanes` partitions input into (a) hint-pinned lanes — one per distinct `LaneHint` in first-seen order, may overlap because the caller asserted the lane invariant — and (b) auto lanes — sorted by `FromMS`, greedily placed in the lowest-indexed lane whose last-`ToMS ≤ current-FromMS`. O(n log n + n × L_auto). Hint lanes appear first in the output; the carousel demo uses `claude` / `gpt` / `gemini` hints to keep per-provider rows aligned.

- **SD6 — Calendar-aware ticks via boxer/timeticks.** `TickMap.ComputeTickMap(viewMin, viewMax, axisStartPx, axisEndPx, loc, prevStep)` defers to `timeticks.TimeTicks` for the uPlot-derived ladder, then precomputes screen-x for every tick and every rollover-row run. The widget passes `loc=nil` (UTC) and `prevStep=TimeStep{}` (no hysteresis) — sufficient for M0–M4; locale and hysteresis are caller-controllable via future option additions.

- **SD7 — Auto-fit via captured-then-fetched available size.** First-frame fallback is `WithContainerSize(w, h)` (or hard defaults `1024 × 220`). Each subsequent frame: `c.CaptureAvailableSize()` writes `ui.available_size()` into `interpreter.r18_avail_w/h`; `StateManager.Sync` drains it; the next frame's `effectiveContainerW/H` reads the cached value. One-frame lag on resize is acceptable and barely visible. The widget always fills the available rect when the capture is valid; `WithContainerSize` is the fallback + minimum, never a pin.

- **SD8 — Ctrl+scroll zoom via egui's `zoom_delta()`.** egui intercepts Ctrl+scroll AND touchpad pinch AND keyboard ±, normalising all three into one multiplicative `Context::input(|i| i.zoom_delta())`. We surface that as `fetchR19ZoomDelta` and consume it in `applyZoomInput`: the view span multiplies by `1.0 / zoom_delta`, anchored at the cursor X so the time under the cursor stays fixed. First user interaction "pins" the auto-fit range into explicit `viewMinMS/MaxMS` so subsequent data updates don't drag the user's view around.

- **SD9 — No scroll-pan; tooltip-only interaction otherwise.** Plain wheel scroll is *not* consumed by the widget — it passes through to whatever scrollable parent contains the widget, avoiding the double-action bug from M3's first iteration. Hover state via `StateManager.GetCanvasPointer` drives a one-frame-lagged in-Go hit test over visible interval bars and rug-strip buckets; tooltip text is painted as a `NeutralBgFaint` rect + `NeutralTextExtreme` text block above the cursor, clamped to canvas bounds. A 1-px cursor crosshair spans the data area at `HoverX`, suppressed when the cursor is over the label band.

- **SD10 — FFFI2 input fetchers extend the framework, not just the widget.** The `widgets/timeline/` work added four new R-slot fetchers (`fetchR16ScrollDelta`, `fetchR17Modifiers`, `fetchR18AvailableSize` with its companion `captureAvailableSize` procedural op, and `fetchR19ZoomDelta`), `StateManager.Get*` accessors, and the Sync drain wiring. These are framework-level primitives, usable by any future custom-PaintCanvas widget that needs pan / zoom / auto-fit. The full pattern (definition → `./generate.sh` → struct field if Ui-scoped capture is needed → StateManager cache) is documented in [[reference_fffi2_input_fetchers]].

- **SD11 — Animation deliberately deferred.** The original M4 plan was viewport tween via `AnimateBoolWithTimeBind`. After implementing scroll-zoom and using it interactively, egui's per-frame `zoom_delta` already feels smooth — adding interpolation would add 1–2 frames of perceived latency to every gesture for marginal visual gain. The animation slot is empty by intent, not by omission.

- **SD12 — Develop-first methodology, ADR-last.** The user explicitly chose "develop before ADR" at the start of the work because the visual + interaction tradeoffs were not predictable from desk research alone. M0–M4 ran as five separately-committed milestones (M0+M1 fused into one commit, M2 / M3-plumbing / M3-widget / M3-fix / M4 as their own commits). Two FFFI2 side-quests (one for input fetchers, one for `zoom_delta`) were discovered during M3 implementation and required the user's explicit go-ahead because of the scope expansion. This ADR codifies what survived; it intentionally does NOT prescribe the M0–M4 sequencing as a process to follow for the next widget.

Implementation landed across six commits on the timeline branch:

| Commit | Scope |
|---|---|
| `a7acc819` | M0+M1 — pure-Go layout package + minimum PaintCanvas widget shell + LLM-session demo |
| `c0b334d1` | M2 — rug strip with raw/density LOD switch + synthetic git-commit fixture |
| `b9e8c263` | M3 plumbing — R16/R17/R18 fetchers + captureAvailableSize op |
| `35bd0600` | M3 widget — auto-scale + hover tooltip + (initial) scroll pan/zoom |
| `c87e1291` | M3 fix — WithContainerSize no-pin + Ctrl+scroll via new R19 zoom_delta |
| `fc232e4a` | M4 — lane labels + cursor crosshair + dropped scroll-pan |

## Alternatives

- **O1 egui_plot binding.** Rejected for M0 scope. The Rust crate would need a Go binding pass alone larger than every milestone of this widget combined, and the resulting abstraction would still fight the laned-interval model (`Plot::show` is series-oriented, not row-oriented). egui_plot remains a reasonable choice for *line-chart* widgets in the Grafana-replacement series; this ADR makes no claim about its fitness there.
- **O3 defer to a future panel.** Rejected because the user's immediate need (LLM session log visualization, git-commit overlay) had no near-term substitute and the Grafana-replacement work isn't scheduled to produce a panel framework on the relevant horizon.
- **Drag-pan as part of M3.** Rejected this round because egui's `Response::is_dragged()` signal isn't surfaced through R14 today and the FFFI2 round to add it was a larger detour than scroll-zoom alone delivered. Future drag-pan work would extend R14 (or add a new R-slot) with `Dragged` + `DragDeltaX/Y` fields; the widget side would derive per-frame deltas in Go from frame-to-frame `HoverX/Y` while drag is active. Tracked as `Mz` in the project memory.
- **Animation tween on zoom (originally planned for M4).** Rejected as overkill — see SD11.
- **`WithContainerSize` as a hard pin.** Tried in M3 first-pass, broke auto-fit when the demo passed sensible defaults. Replaced in `c87e1291` with the fallback-only semantics now in SD7.
- **Modifier check (`mods.Ctrl`) for zoom-gating.** Tried in M3 first-pass, never fired because egui consumes Ctrl+scroll into `zoom_delta` *before* it reaches `smooth_scroll_delta`. Replaced in `c87e1291` with the `zoom_delta`-driven path now in SD8. The trap is documented in [[reference_fffi2_input_fetchers]] as a permanent footgun warning.

## Consequences

### Positive

- **First in-tree timeline widget** with LifeLines-grade mixed point + interval representation, ready for downstream apps (`imztop`, future Grafana-replacement panes) to consume by passing `[]*layout.IntervalEvent` and `[]*layout.PointEvent`.
- **Pure-Go layout package is independently useful.** `PackLanes`, `LODIndex`, `TickMap` are all testable from `go test` with no UI. Any future widget needing greedy interval packing or multi-scale point density (e.g. a Gantt-flavoured task viewer) can import `widgets/timeline/layout/` directly.
- **Four new FFFI2 input fetchers** (R16/R17/R18/R19) are now framework-level primitives. The next custom-PaintCanvas widget needing pan/zoom/auto-fit copies a template, not invents one.
- **Composite-vs-primitive distinction is now explicit.** The split between [[ADR-0013]] (FFFI2 primitives) and the receiver-owned composite pattern (treemap, timeline) was tacit before; this ADR documents it as a deliberate boundary and points future widget authors at the right pattern.

### Negative

- **Hand-painted canvas means we own all the rendering details forever.** Adding e.g. high-DPI font sizing, RTL text, accessibility tree integration, or theme switching requires touching this widget — no upstream library fix will help. This is the cost of choosing PaintCanvas over egui_plot.
- **Hit-testing is O(N) per frame** (linear walk over visible lane bars + buckets to find the hover hit). Acceptable at the documented ~10–100k visible-event target; will need an R-tree if the budget grows.
- **One-frame lag on every cached input** (cursor pos, scroll delta, available size, zoom delta) because all four ride the `StateManager.Sync` end-of-frame drain. Visually fine for hover and resize; for very fast gestures the lag is perceptible to careful users.
- **Drag-pan still missing.** The FFFI2 round to add `is_dragged` + drag-delta plumbing is documented but not scheduled. Keyboard navigation, range-select, and shift-modifier interactions are likewise unbuilt.

### Neutral

- **Animation is absent by design** (SD11). If a future case actually needs viewport interpolation, `AnimateBoolWithTimeBind` from the treemap precedent is the wire to extend.
- **WithContainerSize survives as a fallback knob** rather than a pin. Callers who genuinely need a fixed-size widget will need to wrap it in a sized container externally; this is the same constraint the carousel imposes on every other demo.
- **No `egui_plot` binding has been added.** Future line-chart widgets in the Grafana-replacement work may still benefit from one; this ADR does not preclude that.

## Status

Accepted — 2026-05-19, reviewed by `p@stergiotis`. The M0–M4 implementation (`a7acc819`, `c0b334d1`, `b9e8c263`, `35bd0600`, `c87e1291`, `fc232e4a`) constitutes the accepted design; subsequent refinements follow the standard edit-policy tiers.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [`DOCUMENTATION_STANDARD.md`](../DOCUMENTATION_STANDARD.md) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-05-19 — Annotation selection outline: stroke → back-fill emulation

User-reported: when an annotation is selected, the selection border is
clipped at the top edge of the canvas. Root cause: the selection used
`PaintRectStroke` with bounds at `y=-1..FlagH+1` to give a 1-pixel
margin around the flag, but the Rust drain calls `painter.rect_stroke`
with `StrokeKind::Outside`, which pushes the stroke another 1-2 px
beyond the bounds → `y=-3..-1` lives above the canvas top and gets
clipped silently.

Fix: switch to a two-fill emulation. When an annotation is selected,
paint a slightly-larger rect (`flagX0-2..flagX1+2`, `y=0..FlagH+2`) in
the selection colour FIRST, then the flag fill on top. The visible
margin between the two reads as the outline; nothing paints outside
the canvas. New `annotationSelectionInsetPx = 2` constant. The interval
+ rug-bucket selection strokes are unchanged — they sit inside the
data area where outside-stroke clearance is available.

### 2026-05-19 — Visual polish from screenshot-driven review

Captured the demo via `scripts/dev/hmi_screenshots.sh` and reviewed the actual output. Three visual flaws identified and fixed:

- **`DefaultVisuals.LaneHeight` 22 → 28 px.** Interval bars at the 3-day default zoom were 5–20 px wide and 22 px tall — they read as dashes, not spans. The taller lane height gives session bars enough visual presence to convey duration. Optional override via `WithVisuals(func(v) { v.LaneHeight = ... })` remains.
- **`rugDensityMinTint = 0.3` floors the rug-strip density colormap.** Batlow's dark third sits at luminance close to `NeutralBgSurface`; low-count cells (count = 1, 2) effectively vanished. Remapping `tint ∈ [0, 1]` to `[0.3, 1.0]` before the palette lookup keeps the relative-density encoding while ensuring every visible bucket is readable. Applies to density mode only — raw mode preserves per-point Intensity intact.
- **Demo's bottom info area gains separators + bold section headers.** `c.Separator()` between regions plus a new `renderStrongLabel` helper (Atoms+Strong) for "Selection" / "Annotations panel" / "Recent clicks" headings. The redundant per-region label inside `renderSiblingAnnotationPanel` dropped now that the section header lives outside it.

Demo Stage bumped 680 → 720 to accommodate the taller content (compositor permitting; the screenshot tour on Wayland/COSMIC clamps to ~694 px regardless, documented separately).

### 2026-05-19 — Cross-widget selection setters: SelectIntervalByPointer + SelectBucketAt

Closes the cross-widget driver-API asymmetry: `SelectAnnotationByNumber`
existed since M6 but the analogous interval and bucket setters didn't.
Sibling widgets driving the timeline's selection now have a complete
surface:

- **`SelectIntervalByPointer(ev *layout.IntervalEvent)`** — sets selection
  when ev is in the current intervals slice (verified via `slices.Contains`);
  silent no-op for nil or stranger pointers.
- **`SelectBucketAt(tMS int64)`** — resolves time → bucket via
  `LODIndex.BucketAt` at the cached last-frame view (`lastViewMinMS`,
  `lastViewMaxMS`, `lastViewPxWidth`, written at the end of `renderBody`);
  silent no-op before the first render, when no points are attached, or
  when no event lives at that time at the picked scale. The picked
  bucket is what would have been hit had the user clicked at `tMS` on
  the rug strip.

Three new private fields on `Timeline` capture the view snapshot;
otherwise the change is purely additive. 7 new tests covering hit /
miss / nil / no-render / no-LOD / no-event-at-time. The deferred shelf
loses an item: the only remaining backlog is `Mz` drag-pan, still
gated on the FFFI2 `is_dragged` round.

### 2026-05-19 — Canvas height is content-driven; WithContainerSize → WithContainerWidth

`totalH = max(content, effH)` was meant as "minimum canvas height" but
acted as "greedily fill parent". A 200-px-of-content timeline placed in
a 600-px-tall panel painted 200 px of glyphs followed by 400 px of empty
canvas; sibling labels rendered below sat under that gap with no
adjacent context. Fix: drop the `max`; canvas height is sum of
annotation band + rug + lane rows + axis + rollover rows, full stop.

Cascading cleanup:

- `Visuals.LaneHeight`-based content sizing is all the layer needs;
  `containerH`, `defaultContainerH`, `effectiveContainerH` deleted.
- `WithContainerSize(w, h)` renamed to `WithContainerWidth(w)` — the
  H parameter no longer has a meaningful interpretation. Breaking
  change for the one in-tree caller (demo updated).
- `computeVerticalLayout` signature loses the `effH` parameter.
- Two demo-side test names + their bodies dropped the `H` half.

Demo result: the timeline now occupies exactly its content height and
the cursor readout / sibling panel / recent-clicks list render directly
below the axis with no empty padding.

### 2026-05-19 — Click callback consolidation: 3 options → 1 SelectionListener

`WithOnIntervalClick` / `WithOnRugBucketClick` / `WithOnAnnotationClick`
collapse into a single `WithOnSelection(SelectionListener)` where
`SelectionListener = func(SelectionInfo)`. Callers dispatch on
`SelectionInfo.Kind`:

  timeline.WithOnSelection(func(sel timeline.SelectionInfo) {
      switch sel.Kind {
      case timeline.SelectionInterval:   useInterval(sel.Interval)
      case timeline.SelectionBucket:     useBucket(sel.Bucket)
      case timeline.SelectionAnnotation: useAnnotation(sel.Annotation)
      case timeline.SelectionNone:       // click cleared selection
      }
  })

Semantic addition: the listener now fires on click-miss / click-same
gestures that reset the selection (with `Kind == SelectionNone`), where
the previous trio fired only on positive hits. Callers who only care
about positive selections gate on `sel.Kind != SelectionNone`. The
internal `Timeline.fireSelectionListener()` helper centralises the
"fire after selection mutates" rule so dispatchClick's four branches
all use one path.

Demo updated: a single `formatSelectionClickLine(sel)` helper formats
the click ring buffer entry, including a `"(selection cleared)"` line
for `SelectionNone` events. Three callback closures collapse to one.

### 2026-05-19 — Maintainability + API clarity sweep (Commits A–E)

Adversarial review identified a set of API / maintainability gaps; landed across five commits in one session:

- **A — Validation policy + doc sweep.** Adopted treemap's `# Validation policy` block in the package godoc and a `// Validation:` line on every `With*` option spelling out the panic / nil-clears / no-op semantics that were previously implicit. Dropped two rotted comments ("previous slice-receiving form", "previously hard-coded constants"). Added "all dimensions are egui logical pixels" to the Visuals docstring. Retyped `emptyFallbackHours` → `emptyFallback time.Duration` so it matches the sized-types neighbours.
- **B — LaneOf accessor + Bucket pointer.** New `LaneAssignment.LaneOf(ev) (int32, bool)` replaces direct `EventLane[ev]` access — the map's missing-key-reads-as-0 was indistinguishable from "lane 0", a real footgun. `SelectionInfo.Bucket` switched from value to `*layout.Bucket` for pointer symmetry with `Interval` / `Annotation` (one rule: non-nil iff `Kind` matches).
- **C — IntervalEvent.Validate().** New sentinel `ErrIntervalInverted` + `IntervalEvent.Validate()` method; `PackLanes` calls it and silently drops failures (same treatment as nil pointers). One existing fixture-test was unintentionally relying on the no-validation behaviour and gained explicit `ToMS` values.
- **D — verticalLayout struct.** Bundled the per-frame Y geometry + axis bounds (`topReserved`, `rugTopY`, `laneBaseY`, `laneAreaH`, `axisBaselineY`, `rolloverStartY`, `totalH`, `axisStartPx`, `axisEndPx`) into one struct computed once in `computeVerticalLayout()` and threaded through every paint* / hitTest* method. Replaces 4–7-positional-parameter signatures with `(tm, vl, ...)`. New `vl.clipToAxis(x0, x1)` helper consolidates the bar-out-of-view check that was previously inlined in three places. Behaviourally identical; the diff is mostly mechanical.
- **E — timeline_test.go for pure-Go paths.** 40+ tests covering `clamp01` (incl. NaN), `cursorInsideCanvas`, `splitLines`, formatters, `New` (defaults + panics), `computeViewRange` (auto / explicit / empty), `pinToCurrentView` (ok return + idempotency), `SetIntervals` (interactive-pin-drop vs caller-pin-preserve), selection lifecycle (`Selection`, `SelectAnnotationByNumber`, `ClearSelection`, `SetAnnotations`-clears-kind), `effectiveContainerW/H` (avail-override + NaN-fallback), `computeLabelW` (with/without hints), `computeVerticalLayout` (extras-present vs not), `clipToAxis`. Pure-Go orchestration is now covered; only the FFFI-coupled render plumbing is uncovered (would need an egui mock).

### 2026-05-19 — PackLanes: heap-based placement, O(n²) → O(n log n) worst case

`PackLanes`' auto-lane placement loop replaced its linear-scan-per-event (`for each event { for each lane { ... } }`) with a `container/heap` min-heap keyed on `(lastTo, laneIdx)`. The pathological worst case — fully-overlapping input where every event creates a new lane — drops from O(n²) to O(n log n). Bench on a Ryzen AI Max+ Pro 395:

- `BenchmarkPackLanes_FullyOverlapping_10k`: 1.34 ms/op (was conservatively ~50–100 ms with the linear scan)
- `BenchmarkPackLanes_ModestOverlap_10k`: 0.80 ms/op (was dominated by the sort, ~same)

**Behavioural shift**: the previous algorithm picked the lowest-indexed lane that fits; the heap picks the lane that became free earliest (smallest `lastTo`, tiebreak on smaller `laneIdx`). For non-overlapping and fully-overlapping inputs the outputs are identical; for mixed-overlap inputs individual events may land in different lanes — same lane count, both valid greedy schedules. `TestPackLanes_GreedyFillsEarliestLane` renamed to `TestPackLanes_GreedyReusesEarliestFreedLane` and its `c → lane 0` assertion updated to `c → lane 1` (b's lane, freed at 60, beats a's lane, freed at 100). Other 12 tests passed unchanged.

The earlier "callers expecting fully-overlapping data should partition by LaneHint" workaround in the godoc is gone — no longer necessary.

### 2026-05-19 — Small carry-over fixes: pin ordering, LOD scales panic-on-non-ascending, doc clarifications

Five low-effort items from the prior-review carry-over list:

- **`pinToCurrentView` now returns `ok bool` and bails before mutating state when the auto-fit range is degenerate.** Callers (currently `applyZoomInput`) check the return rather than relying on the post-mutation `spanMS<=0` early-exit, which previously left `explicitRange=true` + `interactivePin=true` on empty data — a no-op zoom that silently disabled future auto-fit.
- **`layout.BuildLODIndex` panics on non-ascending scales** with a clear message naming the offending pair, instead of the silent `+1ms` auto-bump that papered over caller bugs by producing subtly-wrong LOD choices later. Existing test (`TestBuildLODIndex_DefendsAgainstNonAscendingScales`) updated to `TestBuildLODIndex_PanicsOnNonAscendingScales`.
- **`PackLanes` godoc** now documents the O(n²) worst case for fully-overlapping input and points callers at the two mitigations (LaneHint partitioning today, future min-heap-based placement).
- **`LaneCount()` godoc** documents the snapshot-from-last-Render semantics — after `SetIntervals` (or any data swap) the value remains the previous frame's count until the next Render reruns the packer.
- **`tooltipCharWidthPx` godoc** explicitly notes the ASCII-only width estimate, the underestimate factor for CJK/emoji (2–4×), and the reason (egui's text-measurement primitives aren't surfaced through FFFI2 yet). Promotable to `Visuals` if a caller actually needs to tune.

### 2026-05-19 — Post-Visuals-refactor review sweep: closure form for WithVisuals, band tooltips wired, doc cleanup

Three follow-ups from the final-pass review:

- **`WithVisuals` switched from struct-receiving to closure-receiving form.** The previous shape `WithVisuals(Visuals)` allowed a caller to pass a partial struct literal (`Visuals{LaneHeight: 30}`) which silently nilled every color field and produced an invisible widget on first render. New shape `WithVisuals(func(v *Visuals))` operates on the pre-defaulted struct so callers can only ever *modify* defaults, never replace the whole bag. Nil modify is a no-op.
- **`BackgroundBand.Label` is now wired into the tooltip.** Previously documented as "shown in the hover tooltip" but never consumed. Added `hitTestBackgroundBand` (uses `MapXToMS` to compare hover time to band ranges) + `formatBandTooltip` (label + UTC time range). Slots in as the lowest-priority tooltip tier after annotation > interval > rug bucket — bands are a "context" tooltip, not an event tooltip.
- **Doc + naming sweep.** `WithNowLine` docstring no longer references the old `colorTooltipFg` field. `layout/types.go` package doc drops the stale "future M1" qualifier. New `Visuals.AnnotationFgColor` (defaults to the same `NeutralTextExtreme` value) replaces the conceptually-wrong reuse of `TooltipFgColor` for annotation flag text — same value, correct intent.

### 2026-05-19 — Deferred items landed: pin semantics, MapXToMS rounding, BucketAt direct lookup, Visuals collapse

Resolves the three "deferred" items called out in the prior bug-fix-sweep update:

- **`interactivePin` flag separates user-driven pans from caller pins.** `pinToCurrentView` sets it true; `WithRange` / `SetRange` set it false. `SetIntervals` / `SetPoints` / `SetAnnotations` now drop the interactive pin and revert to auto-fit so new data isn't silently invisible after a zoom. Caller-driven pins survive data swaps because the caller meant the absolute window.
- **`MapXToMS` uses `math.Floor` on the ms offset.** `int64(...)` truncates toward zero, which for `px` LEFT of `AxisStartPx` extrapolated AT or RIGHT of `ViewMin` — wrong direction. Floor fixes left/right asymmetry without affecting the in-range case. Unit test covers the left-extrapolation path.
- **`LODIndex.BucketAt(tMS, t0, t1, pxWidth) → (Bucket, scaleMS, ok)`** is a single-map-lookup replacement for the iterate-all-visible-buckets loop that `hitTestRugBucket` used. Three improvements at once: ends the `scaleMS` recompute drift between `hitTestRugBucket` and `formatRugTooltip` (they now share one source of truth), eliminates the per-mousemove bucket-slice allocation, and shrinks the hit-test to O(1) at the chosen scale.

The fourth deferred item, **Visuals struct collapse**, also lands:

- **SD15 — `Visuals` struct + `DefaultVisuals()` + `WithVisuals(v)` subsume 22 individual fields and 4 redundant options.** Timeline struct shrinks 22 fields → 3 (containerW, containerH, visuals). Removed options: `WithLaneHeight`, `WithRugStripHeight`, `WithIntensityColormap`, `WithRugColormap`. Callers tweak visuals via `v := timeline.DefaultVisuals(); v.LaneHeight = 30; v.IntensityColormap = styletokens.SequentialMagma; tl := timeline.New(..., timeline.WithVisuals(v))`. The struct has no internal invariants — fields are exported, free composition. Mirrors treemap's `StyleI` / `WithStyle` pattern but without the per-element-dispatch interface (timeline's visuals don't depend on per-event state). Internal-only tuning constants (`tickLabelFontSize`, `tooltipPaddingX`, `annotationHitCorridorPx`, etc.) remain `const` — promotion to `Visuals` is opt-in for fields the API actually surfaces.

### 2026-05-19 — Bug-fix sweep + hit-test refactor (post-adversarial-review)

Five defects identified during an adversarial review and fixed in one pass:

1. **Crosshair cut through annotation flags.** The vertical crosshair painted from `y=0`; with annotations enabled, the flag band occupies the top 18 px, so the crosshair slashed through every flag it overlapped. Now starts at `topReserved` (just below the flag band).
2. **`clamp01` propagated NaN.** Both `v <= 0` and `v >= 1` are false for NaN, so the value passed through unchanged into `styletokens.Sequential`, where the LUT index calculation also tolerates NaN silently — net effect was random/zero RGBA on intervals with malformed Intensity. Added an explicit `v != v` arm returning 0.
3. **`computeViewRange` degenerate padding.** `padMS = int64(span * 0.02)` rounded to 0 for any span shorter than 50 ms, producing a `[t,t]` viewport that `ComputeTickMap` rejects as degenerate → blank canvas. Now `max(_, 1)`.
4. **Selection card showed previous-frame state in the demo.** Demo rendered the card *before* `tl.Render()`, so the card read last frame's `Selection()`. Reordered: render the timeline first, then the card + cursor readout + sibling panel — all "what was just hovered/clicked" feedback now lives below the widget where it logically reads current state.
5. **Duplicated interval hit-test.** `dispatchClick` and `hitTestTooltipText` each had a nested-loop scan of `laneAssn.Lanes × Lane.Items` for the bar-under-cursor. Two implementations would have drifted as the click and hover semantics evolve independently — extracted as `hitTestInterval(tm, laneBaseY, cursorX, cursorY) → (*IntervalEvent, hint)`. Net ~20 LOC saved and one consistent point of failure for "what is under the cursor".

Known-but-deferred items from the same review: `pinToCurrentView` + `SetIntervals` interaction (data update mid-pan silently invisible — should reset `explicitRange=false` on data change or document), the 13-option surface (could collapse into a `StyleI` mirroring treemap), and the `BucketsForRange` per-mousemove allocation (real but sub-ms; defer until profiled). The "MinInt64 overflow in `weekendBands`" theoretical concern is not addressed — production data never exhibits that range.

### 2026-05-19 — Background bands + now line (M7, adds SD14, departs from retained-mode for view-dependent data)

Two additions and one API-shape correction:

- **SD14 — Background bands as iter.Seq producers, not retained slices.** `layout.BackgroundBand{FromMS, ToMS int64, Color uint32, Label string}` represents shaded time ranges — weekend overlays, office-hours, maintenance windows, alert windows. Crucially, callers register a `BackgroundBandProducer = func(viewMinMS, viewMaxMS int64) iter.Seq[BackgroundBand]`, not a `[]*BackgroundBand` slice: bands like "all weekends in this view" depend on the current view range and would force the caller to recompute + replace on every zoom/pan if we used the retained slice idiom. The widget calls the producer once per frame with the current view; producers walk the relevant calendar units once and yield only the bands that intersect the view. No allocation on most frames.

  This is a deliberate departure from the retained slice pattern used by `WithIntervals` / `WithPointEvents` / `WithAnnotations` — those data shapes are caller-known finite sets that don't change with the view (lane packing + LOD indexing want the full input anyway). View-dependent producers are the right shape only when "what would be visible" is computable from the view bounds. The earlier surfaces stay retained for the reasons above; new view-dependent surfaces should follow the producer pattern.

- **Now line.** `WithNowLine(bool)` paints a 1.5 px solid vertical at `time.Now().UnixMilli()` when the current wall-clock falls inside the view. Skipped silently for historical / future-only views — no off-screen now indicator, matching Grafana convention.

- **Z-order**: bands paint first (under everything), then rug/lanes/axis/rollover, then annotations, then now line, then crosshair + tooltip. Lowest-impact layering for legibility.

The demo gained two producer examples (`weekendBands`, `officeHoursBands`) and a `composeBandProducers` helper so callers can stack multiple producers without writing the iter glue themselves.

### 2026-05-19 — Annotations + cross-widget linking (M6, refines SD9, adds SD13)

Grafana's annotation idiom landed: time-pinned vertical dashed markers with numbered flags. Captured as a new design specification:

- **SD13 — Annotations as cross-widget linkable markers.** `layout.Annotation{TMS, Number int32, PaletteIdx int32, Label string}` is rendered as a `PaintDashedLine` from below the flag band to the axis baseline, plus a flag rect at the top containing `Number` centered. Color is `styletokens.QualitativeCycle(PaletteIdx)` (BatlowS, 10 entries, CVD-safe). Annotations render *after* lanes + axis, so they layer on top and take precedence in hit-testing. `Number` is caller-supplied — *not* derived from slice index — because sibling widgets reference annotations by number, and slice indices shift on add/remove. The widget exposes `SelectAnnotationByNumber(int32)` for the sibling-driven direction; the `OnAnnotationClick` callback handles the widget-driven direction. Caller wires both directions.

The FFFI2 round to support this added `paintDashedLine(fromX,fromY,toX,toY,dashLen,gapLen,col,strokeWidth)` as a new `PaintCmd::DashedLine` variant in `interpreter.rs`. The Rust drain decomposes via `egui::Shape::dashed_line`, so the wire cost is one opcode per annotation regardless of dash count — the in-Go simulation that would have cost O(annotations × segments) is avoided. SD11's "animation deliberately deferred" note still holds; SD10 grows by one fetcher-adjacent primitive.

Selection model extended: `SelectionAnnotation` added to `SelectionKindE`; `SelectionInfo` gains an `Annotation *layout.Annotation` field. `SetAnnotations` clears any annotation selection on data swap, mirroring the existing `SetIntervals` / `SetPoints` invariants.

The demo grew a sibling annotations panel (a `c.SelectableLabel` per annotation) that reflects the timeline's selection AND drives it on click, demonstrating the bidirectional cross-widget link the API was designed for.

### 2026-05-19 — Selection model + first-class demo feedback (refines SD9)

The click extension landed earlier in the day used stderr (`zerolog.Info`) for visual confirmation. Promoting click feedback to a first-class part of the widget surface:

- **Selection state on the widget.** `SelectionKindE` enum (`SelectionNone | SelectionInterval | SelectionBucket`) + `SelectionInfo{Kind, Interval, Bucket}` snapshot returned by `Timeline.Selection()`. `ClearSelection()` provided for host-driven clear. `SetIntervals` clears any interval selection; `SetPoints` clears any bucket selection (the previously-held pointer / bucket-StartMS may not survive the data swap).
- **Click semantics.** `dispatchClick` now toggles selection: click an unselected target to select it, click the same one again to clear, click off-target to clear. Callbacks fire after the selection update so observers see the current state.
- **Visual highlight.** Selected interval bars and selected rug-strip buckets get a `colorSelectionStroke` (`NeutralTextExtreme`) 2 px outline painted on top of the fill. Raw-mode rug marks (single `paintLine` per point) are intentionally not stroke-highlighted — too small to be visually useful.
- **Demo card + ring-buffer log.** The demo renders a `c.Label` selection card above the timeline (intervals show hint + UTC time range + duration + intensity; buckets show start + count + Σintensity) and a capped-5 "Recent clicks" list below. The previous `zerolog.Info` calls are dropped — the labels are the primary signal.

### 2026-05-19 — Click handlers added (refines SD9)

SD9 read "no scroll-pan; tooltip-only interaction otherwise". The "otherwise" clause is now a refinement: the widget also surfaces primary-click events. Sense is flipped to `Sense(click=true, drag=false, hover=true)` and the cached `CanvasPointerValue.Clicked` flag (edge-triggered by egui's `Response::clicked()`) drives a `dispatchClick` that runs the same hit-test the hover tooltip uses. Two new options register callbacks:

- `WithOnIntervalClick(func(*layout.IntervalEvent))` — fires when a lane bar is hit; lane-bar hits take precedence over rug-strip hits.
- `WithOnRugBucketClick(func(layout.Bucket))` — fires when the rug strip is hit on a position that resolves to a visible LOD bucket. In raw-mode renders the bucket typically represents one event; the caller inspects `b.Count` to distinguish.

The rug bucket hit-test was extracted from `formatRugTooltip` into a shared `hitTestRugBucket(tm, cursorX) (Bucket, bool)` helper so the tooltip and click paths agree on which bucket is "under the cursor" — no possibility of drift between what the tooltip claims and what the click reports.

Drag, double-click, right-click, and keyboard activation remain unbuilt. Drag is still gated on the same FFFI2 round as the deferred `is_dragged` work.

## References

- [ADR-0013 — Stateful widget contract](./0013-imzero2-stateful-widget-contract.md) — the FFFI2-primitive contract this widget deliberately sits outside of.
- [ADR-0016 — Time range picker](./0016-imzero2-time-range-picker.md) — established the `(FromEpochMS, ToEpochMS, TzID)` wire shape this widget consumes.
- [ADR-0031 — Design system color foundations](./0031-imzero2-design-system-color.md) — provided the Crameri batlow sequential palette used for interval + rug-strip tinting.
- [ADR-0032 — Design system spacing/density/motion](./0032-imzero2-design-system-spacing-density-motion.md) — motion ladder we would tap into if SD11 reverses.
- [ADR-0035 — Keelson namespace](./0035-keelson-namespace-introduction.md) — explains why `styletokens` is imported from `keelson/designsystem/` rather than the local widget tree.
- `public/thestack/imzero2/egui2/widgets/timeline/` — implementation.
- `boxer/public/math/numerical/timeticks` — calendar-aware tick generator (uPlot-derived ladder).
- Plaisant, Milash, Rose, Widoff, Shneiderman, *LifeLines: Visualizing Personal Histories*, CHI '96 — canonical reference for mixed point + interval timelines.
- Monroe, Lan, Lee, Plaisant, Shneiderman, *Temporal Event Sequence Simplification*, IEEE InfoVis 2013 — EventFlow successor; aggregation patterns.
- Aigner, Miksch, Schumann, Tominski, *Visualization of Time-Oriented Data* (2nd ed. 2023), mirrored at <https://timeviz.net>.
