---
type: adr
status: accepted
date: 2026-08-01
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-27
---

# ADR-0160: an icicle / flamegraph widget on the implot custom-item lane

## Context

The widget zoo has one space-filling hierarchy form, `treemap`: nested
rectangles whose *area* is the value. It reads well for "what is big", and
badly for "what called what" — a treemap discards the ordering and the depth
of a path, which is exactly the information a stack profile is about.

The missing form is the **icicle plot**: one row per depth, each node a
rectangle whose *width* is its value, children abutting under their parent.
Turned upside down (root at the bottom) the same layout is the **flamegraph**.
Both are one layout with one switch, and both are the conventional way to read
a stack profile.

Two things make this worth building now rather than deferring again:

- The [pprof-as-data](../adr-background-work/pprof-profiles-as-data.md) ladder
  parked exactly this as **M5**, its last rung, after M0–M3 shipped. Captured
  profiles are already queryable as tables and as a call graph; they are
  explorable but not pleasant.
- [ADR-0149](./0149-implot-core-port-painter-lane.md)'s custom-item lane and
  the [ADR-0159](./0159-imzero2-sankey-flow-widget.md) sankey widget that first
  used it turned "a new plot form" from a widget-sized job into a layout-sized
  one. The frame, the gestures and the readbacks are already there.

That second point corrects a claim in the pprof survey, which recorded that
"the ImPlot port is a cartesian core — a flame view is a partition layout, not
an ImPlot item". The first half is right and the conclusion does not follow.
An icicle **is** cartesian: x is the value domain, y is depth. What it is not
is one of the *built-in* items, which is what the `Custom` lane exists for.

The reuse is not incidental. Pointer-anchored wheel zoom on a value axis is
precisely the zoom a flamegraph wants; click-to-zoom is a range assignment on
that axis; double-click-to-reset is the stock fit. None of it needs writing.

## Design space (QOC)

**Question.** What is the smallest widget that renders a stack hierarchy as an
icicle/flamegraph, given the implot port?

| Criterion | Why it matters |
| --- | --- |
| C1 — reuse of the implot frame | pan, zoom, box-zoom, fit, legend, context menu and the plot-space readbacks are the bulk of an interactive plot |
| C2 — readable rows at any depth | a profile 40 frames deep must not render 4 px rows |
| C3 — input shape matches the data that exists | pprof lands as flat rows; a SQL result is flat rows |
| C4 — cost at profile scale | tens of thousands of nodes, redrawn every frame |
| C5 — IDS conformance from the first commit | colour, stroke and spacing from the tokens, not literals |

**Options considered.** O1 a `Custom`-item widget over implot; O2 a standalone
painter widget; O3 an extension of `treemap`; O4 bending the `timeline` widget's
axis into a value domain.

O2 re-rolls what C1 gets for free. O3 shares a coloring vocabulary but nothing
of the layout, and would drag icicle concerns into a widget with a working
navigation model of its own. O4 was rejected in the pprof survey and stays
rejected: `timeline` is genuinely time-typed (tick rulers, duration LOD), and a
value domain would fight all of it.

## Decision

Build `widgets/icicle` (UI-free layout) and `widgets/icicle/view` (the implot
half), following the ADR-0159 package split. Extend implot with the one thing
the form needs and the port lacks: per-axis gesture locks.

### SD1 — Flat rows in, tree derived

The input is columnar, not a pointer tree:

```go
type Tree struct {
    Labels  []string
    Parents []int32   // -1 marks a root
    Self    []float64
}
```

A node's total is `Self + Σ children` — the parent's bar is wider than its
children's, and the uncovered remainder *is* its self value.

This is the shape the data already has. A pprof capture lands as
`stack Array(String)` plus a value column (ADR-0134 / the M1 converter); a
recursive CTE emits a parent column; `treemap`'s pointer `layout.Node` converts
in a few lines. The reverse — demanding a pointer tree — makes every columnar
producer build one first.

It also sidesteps a semantic mismatch. `treemap/layout.Node.TotalSize()`
*ignores* a parent's own `Size` and sums only children, which is right for a
treemap and wrong for a profile: a function with both self samples and callees
would have its self time silently vanish. Reusing that type would have meant
encoding self time as a synthetic child, i.e. inventing frames that were never
sampled.

No ordering constraint is imposed on the rows (`Parents[i] < i` would have made
roll-up a single reverse pass, but a producer cannot always promise it), so
depth is computed with an explicit walk that detects cycles and reports them as
a validation error rather than hanging.

Multiple roots are laid out side by side at depth 0. A forest is the natural
shape of a per-goroutine or per-thread profile, and synthesising a virtual root
would invent a total that no sample supports.

### SD2 — The layout emits plot space; orientation is a layout option

`Compute` returns nodes already in plot coordinates: x in **value units** (so
the axis ticks read as samples, bytes or seconds, and auto-fit is meaningful),
y in **row units**, one row per depth.

Orientation is applied there, not in the view: an icicle emits `Y0 = -(d+1)`,
`Y1 = -d` so the root sits at the top of a plot whose y grows upward, and a
flamegraph emits `Y0 = d`, `Y1 = d+1`. Keeping it in the layout is what lets
the hit test be a plain plot-space query in the UI-free package, the same
property ADR-0159 §SD3 relies on.

This differs from the sankey's unit box deliberately. There, geometry is a
diagram in an abstract square; here the x axis carries a quantity the reader is
meant to compare, and normalising it away would throw that out.

### SD3 — Rows hold a pixel height, held by a locked depth axis

The depth axis's *span* is derived, not chosen: `span = min(rows, areaH/RowPx)`,
with the plot-area height read back each frame.

`RowPx` (18 by default) is therefore a **minimum**, not a fixed value, and the
`min` is what makes it one. Once a tree is deeper than the pane, the span is
`areaH/RowPx` and rows settle at exactly `RowPx` while the axis scrolls — the
case C2 is about. While a tree still fits, the span is the tree's own depth, so
the rows divide the height and come out taller. Pinning rows at `RowPx` in that
case too would be defensible and looks wrong: a three-deep tree would occupy 54
px of a 380 px pane and leave the rest blank.

The alternative — pin the full depth range always and let rows divide the
height — fails C2 the moment a stack is deep, and makes the wheel scale rows
and values together, which reads as the whole picture sliding rather than a
zoom.

Holding a derived span needs two things the port did not have, hence SD4. A
third consequence is host-visible: because implot retains a plot's ranges per
plot id and the initial limits are applied `CondOnce`, swapping in a different
tree would leave the plot looking at the *previous* tree's value window. A
`Layout` carries no identity for the widget to compare against, so the host
says when that happened, via an `Opts.ResetView` flag it sets for one frame.

With those, the interaction contract is:

| gesture | effect |
| --- | --- |
| wheel | zooms the value axis, anchored at the pointer; rows keep their height |
| drag | pans the value axis and scrolls through depth |
| click a frame | zooms the value axis to that frame's span |
| double-click | fits the value axis back to the whole profile |
| right-click → *Fit Y axis* | not offered: the depth axis is `NoZoom`, and a menu fit is a gesture kind like any other (SD4) |
| right-click → *Fit both* | fits the value axis only |

`ResetView` outranks a click resolved the same frame. Both are `CondAlways` and
the zoom is applied second, so on the frame a layout is swapped the click —
resolved against the *outgoing* layout's transform — would otherwise silently
undo the reset that was the point of the swap.

Depth scrolling is a pan of the depth axis rather than an enclosing scroll
area, because an implot plot captures the wheel for its own zoom — a plot
inside a `ScrollArea` would swallow the scroll it was meant to delegate.
`SetupAxisLimitsConstraints` bounds the scroll to the tree.

### SD4 — Two additions to implot, both narrow

**Per-axis gesture locks.** `AxisFlagsNoPan`, `AxisFlagsNoZoom`, and
`AxisFlagsLock` for both — the last is upstream's `ImPlotAxisFlags_Lock`, which
locks an axis's ends against user input. Upstream does not split it; the split
is what lets a depth axis be scrolled but not scaled.

The locks apply **per gesture kind, not to the resulting range**. That is the
one subtlety: an anchored wheel zoom moves an axis's centre as well as its
span, so a "let it zoom, then restore the span" implementation would leave the
axis panned by the wheel. Pan, wheel, box-zoom and fit each consult the locks
separately, and an axis no gesture moved is not written back at all — which
also stops a purely vertical drag from round-tripping the value axis through
the transform on every frame.

"Every gesture kind" has to include the ones that do not come from the
pointer. The **context menu's fit actions** are a gesture kind, and the first
implementation missed them: they set the pending-fit flag directly and so
reached past a `NoZoom` axis, collapsing the depth axis to a single row. A fit
now goes through one helper that consults the locks, and the menu does not
offer an action a locked axis would decline. `Plot.FitNext()` is deliberately
*not* filtered — it is the caller asking in its own right, not a user gesture
— which is why the widget also declares its depth extent (see the note in
SD7).

**`AxisLimits(axis)`** — upstream's `GetPlotLimits`, narrowed to one axis. A
caller that derives a span from the plot area must re-assert it when the pane
resizes, and to do that without discarding the scroll position a pan has since
put there, it has to be able to read the range back.

Both are generic. Any future categorical, lane or gantt axis wants the same
pair, and neither is icicle-specific enough to belong in the widget.

### SD5 — Hit testing is plot-space and row-indexed

The layout keeps, per depth, the row's node indices sorted by x. A hit test
maps y to a row by arithmetic and then binary-searches that row: O(log n) per
probe, and correct at any zoom because it never touches pixels — the same
reasoning as ADR-0159 §SD3.

`PathTo` walks the parent chain to the root, which is what a host needs for a
breadcrumb or a tooltip; the widget does not draw either. Hover and selection
come back as indices and the host decides what to say about them, as the sankey
returns hits rather than rendering its own chrome.

One qualification the first implementation got wrong: pixel-free is right for
the *layout*, and not sufficient for the *probe*. The view culls frames too
narrow to draw (SD7), so a layout-only hit test reports nodes that were never
painted — a hover naming a function with nothing under the pointer, and a
selection ring with no rectangle to trace. The cull is therefore applied a
second time in `view.Probe`, which recovers the value axis's mapping from
`PlotAreaPrev` and `AxisLimits` and runs the *same* predicate the renderer
does. The pixel knowledge stays in the view; the layout stays pixel-free; and
because both go through one function they cannot drift apart. Before the first
render there is no mapping and nothing has been drawn, so every hit stands.

This assumes the value axis is linear, which the widget always configures. A
host driving implot itself and choosing a log axis would get a probe that
disagrees with what it drew.

### SD6 — Colour is a data encoding, from the IDS tokens

Two schemes, plus a caller override:

- **By label** (default) — a hash of the frame name into the warm band of the
  Lajolla sequential scale (`styletokens.Sequential`, t ∈ [0.30, 0.90]): the
  flamegraph's conventional red-through-yellow, from the IDS tokens rather than
  the classic random warm RGB. The hash keeps what the randomness never had —
  a given function keeps its colour everywhere it appears, including across
  two captures, which is what makes the picture comparable. The first cut
  hashed into the seven-hue qualitative cycle (ADR-0156) instead; with seven
  bins, unrelated neighbours collided exactly rather than merely landing near
  each other, and the picture stopped reading as a flamegraph. The band's ends
  are where Lajolla stops reading as fire — browns sinking toward the plot
  surface below it, creams washing into it above — and its monotone lightness
  keeps two names apart under CVD, where the classic equal-lightness warm
  noise is not.
- **By depth** — a sequential ramp (`styletokens.Sequential`), which reads the
  structure rather than the identity.

One warm band means adjacent siblings are similar in colour by construction,
so every rectangle is inset by a hairline gap: neighbours are separated by the
background rather than by a border, which costs no extra paint opcode. Text ink, the selection
ring and the stroke ladder are token values (rules L2/L10), so the widget is
IDS-conformant on its first commit rather than after a lint pass.

The label ink is chosen against the fill by WCAG relative luminance, which is
now `implot.RelativeLuminance` — hoisted so the lane has one such formula
rather than one per widget. The switch point between the two neutral inks is
*derived* from them (`sqrt((Ld+0.05)(Ll+0.05)) - 0.05`, which is 0.1734 for
the palette as it stands) rather than written down, so regenerating the IDS
palette moves it instead of leaving a stale constant. The first draft used a
rounded 0.18, which picked the worse of the two inks over about 1.4% of the
colour space — 4.32:1 where 4.58:1 was available. The flame band crosses the
switch — a dark-to-light ramp must — so its worst fill reads at the switch's
own 4.45:1, a hair under WCAG AA's 4.5 and confined to that single crossing;
the classic flamegraph's black-on-deep-red reads nearer 3:1.

implot's own `contrastText` is deliberately left on upstream's Rec.601 rule,
so a ported ImPlot chart keeps the ink upstream would have given it. The two
rules disagree on about 7% of the colour space; converting it would change a
shipped widget's appearance, which is a separate decision from this one.

### SD7 — Culling is by visible range, pruning is by fraction

Two different jobs, deliberately not conflated:

- **Pruning** (`MinFraction`, layout-time, default off) drops subtrees below a
  fraction of the total and counts them in the report. Resolution-independent
  and reproducible.
- **Culling** (view-time, always on) skips what cannot be seen: rows outside
  the depth window, nodes outside the visible value range — both found by
  inverting the frame transform — and rectangles too narrow to be worth
  drawing.

The width threshold is worth stating exactly, because it is not the fraction
the constant reads as. Each edge is snapped to a whole pixel first (neighbours
share a boundary value and so round identically, which keeps the separating
gap uniform), then the gap is taken off the right edge, and what remains must
be at least `minRectPx` = 0.75 px. Since snapped edges are integers, the
surviving width is a whole number of pixels, so the effective cut is at **2 px
of un-inset span** — not the half-pixel an earlier draft of this ADR claimed.
`view.snapX` is the single place that decides it, and `Probe` calls the same
function (SD5).

Everything surviving both is drawn in a single batched `PaintRectsFilled`, so
frame cost tracks *visible* nodes, not tree size. This is what makes C4
tractable: at any zoom the number of surviving rectangles is bounded by half
the plot's width in pixels times the number of visible rows. The label pass
reads the batch buffers rather than re-projecting and re-hashing each node, so
that bound is paid once per frame rather than twice.

A frame narrower than the threshold is dropped rather than drawn as a sliver
or merged with its neighbours. Drawing slivers would forfeit the bound above;
coalescing runs of them into one rectangle would read better and is the
obvious next cut, recorded in SD8.

### SD8 — Deferred, deliberately

Not in this cut, recorded so they do not gate it:

- ~~**A pprof-specific applet book.**~~ *Landed.* The generic `play` panel of
  SD9 drew a capture already — `SELECT stack, value FROM keelson('<handle>')`
  is the whole query, since the M1 converter's output *is* the folded contract
  — so what was left of the ladder's M5 was a committed book, not any widget or
  panel work. `bookpprof/profile-flame.md` is it, and imzrt's Explore now seeds
  the same projection with `Tab: icicle`. Both rescale the value (nanoseconds
  and bytes read badly raw) through an inner alias, `value / d AS value` being
  a cyclic alias to ClickHouse. Naming `icicle` in an applet's `tabs:` needed
  the slug added to sqlapplet's result-panel roster — it had been registered in
  play and classified in neither list, which the tab-policy gate had been
  failing on since this ADR's panel landed.
- **Differential flamegraphs.** Signed values (red/blue against a baseline)
  need a second tree and a diff model; the widget rejects negative values today
  rather than half-supporting them.
- **Text measurement.** Label fitting uses `implot.Elide`, which budgets in
  pixels against the lane's shared estimate (`implot.EstimateTextWidth`:
  rune-counted, charging the East Asian wide blocks a full em) so what it
  returns fits what it was cut for. The elision itself lives beside the
  estimate rather than in this widget, because it is the same decision — two
  widgets eliding by different rules is exactly what that file exists to
  stop. Real measurement is *not* unavailable — `bindings.MeasureText` is live
  and `widgets/colorscale` drives it on a one-frame lag. What ADR-0149 §SD6
  defers is a fetcher inside implot for its own tick and legend sizing.
  Adopting measurement here would want a shared cache and a frame of lag for
  what is a few percent of width, so the estimate stands.
- **Coalescing sub-threshold runs.** A run of siblings each too narrow to draw
  currently leaves background where a blended block would read as "many small
  things". It is the best-looking answer to the SD7 cull and the most work:
  the merged rectangle needs a fill, and the hit test needs to resolve to
  something sensible inside it.
- **A reverse index from `Node.Index` to layout position.** A host that found
  a node by other means — a search result, a row in a table — has to scan
  `Layout.Nodes` to highlight it. `func (l *Layout) ByIndex(int32) int` over a
  lazily built map is a few lines, but it is new public surface and no
  consumer needs it yet.
- **Search / highlight-by-pattern.** Wants a host-side query surface.
- **Minimap / value-axis overview.** Only earns its keep once a real profile is
  driving the widget.

### SD9 — The `play` panel: two contracts, over the active result

`apps/play/play_icicle_panel.go` is the **Icicle** dock tab, a `PanelI` over
this package. Most of it follows the ADR-0097 channel contract and so is not a
decision. Three things are.

**Two column contracts, not one.** *Folded* — `stack` (an array) plus `value`,
one row per root-to-leaf path, interned into a trie with the interior frames
synthesised. *Nodes* — `id`, `parent`, `value`, one row per node, straight onto
`Tree`. Neither converts to the other in a line of SQL: the folded shape is what
a capture and a `splitByChar` produce and needs no recursive CTE, while the node
shape is what a recursive CTE emits and is the only one of the two in which an
interior node can carry a value of its own — the very distinction SD1 rejected
`treemap`'s type over. A list-typed `stack` wins when a schema satisfies both,
and the status line names the mode rather than leaving it to be inferred.

**The active result, not a named CTE.** Sankey and Network take private lanes
because they need two inputs and want the final `SELECT` left alone. One input
needs neither, so this binds `chMain` as World and Kanban do, and any query
naming the columns draws without being restructured.

**Selection is local anyway.** Binding the active result would allow the row
cursor, and it is still not published: in folded mode a frame is a path
*prefix*, so an interior frame spans many rows and a leaf frame is one row only
when the stacks happen to be unique. A clicked frame publishes its label as
`selection_key` — a value, which is also what a follow-up query wants
(`WHERE has(stack, {selection_key:String})`).

Rows the form cannot draw are dropped and *counted* rather than failing the
picture, and the two caps behave differently on purpose: a path past the depth
cap is truncated and a folded path past the node cap lands its value on the
deepest ancestor already interned, so both cost DEPTH rather than quantity and
the total stays conserved.

Two notes for the next consumer, both found by looking at a capture:

- **An options bar must not be `.Frameless()`.** `StyleSegmented` shows the
  selection by *filling* the selected segment, and `Frame(false)` draws no
  background — so a frameless segmented bar renders its selected and unselected
  options identically. Two captures of this pane differing only in orientation
  came back with pixel-identical control rows, which is how it was found; the
  Flow, Network and Sankey panels turned out to have it too, six bars between
  them, and are fixed with this cut. (Experiments sets `Frameless` as well and
  was never affected: it also sets `StyleSelectable`, the one arm that ignores
  the flag.) All of them, and this panel's four bars, now use
  `StyleSelectable` — frameless *and* highlighting, which is the denser
  reading and the one look every options bar in `play` now shares. `Frameless`
  itself is kept, documented, and left with no caller: it is still right inside
  a ComboBox popup, where the popup states the selection.
- The plot is sized to the pane, not to an aspect. It began as ADR-0159's fixed
  ratio — the same leaf minus the same control row and status line, so the two
  landed within a pixel of each other — and moved to the leaf's own height once
  `captureUiAvailableRect` made that readable (ADR-0159's 2026-08-15 Update).
  The pane rule is worth more to this form than to the sankey's: the depth axis
  holds rows at `RowPx` rather than scaling, so a plot past the leaf hid a whole
  row of frames instead of shrinking the picture, and a taller leaf now buys
  rows rather than margin.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `widgets/icicle` (exported Go API under `public/`) | added — `Tree`, `Options`, `OrientationE`, `OrderE`, `Report`, `Layout`, `Node`, `Compute`, plus `Tree.Validate` / `Tree.Len` and `Layout.NodeAt` / `DepthAt` / `RowDist` / `PathTo` | `package_props.go` registration (ADR-0080) |
| `widgets/icicle/view` (exported Go API under `public/`) | added — `Opts` (including `ResetView`), `ColorModeE`, `Hit` (with `None`), `NodeHit`, `Renderer`, `Setup`, `Probe`, `Draw`, `Show`, `ZoomTo`, `DefaultRowPx`, `DefaultFontSize` | imports `implot` and the bindings; the only half that touches UI |
| `widgets/implot` (exported Go API under `public/`) | added — `AxisFlagsNoPan`, `AxisFlagsNoZoom`, `AxisFlagsLock`, `AxisLimits`, `Elide`, `RelativeLuminance`, `ContrastRatio` | additive; existing flag values unchanged. The context menu no longer offers a fit for a `NoZoom` axis — no shipped plot sets one except this widget |
| demo registry (`egui2_hl_registrations.go`) | added — one `icicle` demo entry | registry is a named list; adding a member is not itself a decision |
| `play` dock tabs | added — the **Icicle** tab (SD9): frozen `DockID` 22, a `ShapeContract` mark, `selection_key` in `Writes`, and the derived `BOXER_PLAY_FOCUS_ICICLE` knob | the tab-registry counts in `play_tabs_test.go`, `play_tab_marks_test.go` and `play_panes_menu_test.go`; `doc/env-vars.md` regenerates |
| `play` internals | `sankeyCellValue` renamed `quantityCellValue` | package-private; one reader of a numeric cell now serves both panels |
| `play` Flow / Network / Sankey options bars | rendering fixed — six `.Frameless()` bars that drew no selected state at all now use `StyleSelectable` (SD9's first note) | appearance only; no API moved, and `widgets/selector` gained the warning on `Frameless` rather than a behaviour change |
| egui2 IDL | **unchanged** | no new paint opcode; batched rects, polylines and text all exist |

## Alternatives

- **Standalone painter widget (O2).** Full control over the depth axis, at the
  cost of re-rolling pan, zoom, fit, legend and the readbacks. Rejected on C1:
  the port exists so that a new plot form is a layout plus a draw closure.
- **Extend `treemap` (O3).** Shares the coloring vocabulary and nothing else —
  squarified nesting and depth rows have no layout in common, and `treemap`'s
  drill navigation is a different interaction model. Rejected on cohesion.
- **A pointer tree as the input (SD1).** Fewer conversions for hand-built
  callers, more for every real producer, and it inherits the parent-`Size`
  mismatch described in SD1. Rejected on C3.
- **Proportional row heights.** Zero implot change and always fills the pane;
  rejected on C2 — deep stacks become unreadable and the wheel distorts rows.
- **Re-rooting on click instead of an axis zoom.** The conventional flamegraph
  behaviour, and strictly more code: an axis zoom reaches the same picture, and
  keeps ancestors visible above the zoomed frame instead of replacing them with
  a breadcrumb. Rejected on C1 with a note that a re-root can be added later as
  a layout option without disturbing the view.

## Consequences

### Positive

- A second form on the custom-item lane, which is evidence the lane generalises
  rather than having been shaped around the sankey.
- The implot locks and `AxisLimits` are reusable by anything with a
  non-cartesian second axis; the port moves one step closer to upstream parity.
- pprof M5 becomes a panel-sized job rather than a widget-sized one.
- Colour stability by function name makes two captures visually comparable
  without a diff mode.

### Negative

- implot grows two more flags. The flag space is a `uint32` and the additions
  are additive, but every flag is a thing a reader must now understand.
- Per-axis locks are a cross-cutting rule, and a cross-cutting rule is only as
  good as its coverage. The context-menu fit was missed on the first pass and
  collapsed the depth axis; the fix routes every fit through one helper, but
  the general hazard — a new gesture path that writes a range without asking —
  remains, and only a reader who knows the rule will apply it.
- The hit test now needs the plot's transform, which it recovers from
  readbacks rather than being handed. That is one more place assuming a linear
  value axis, and one more thing that is inert on the first frame.
- The derived depth span is a one-frame feedback loop: the first frame has no
  plot area to read, so it uses the requested height as an estimate and
  corrects on the second. A widget that appears and is screenshotted in the
  same frame may capture the estimate.
- Label fitting is still an estimate, so labels are dropped conservatively;
  some rectangles that could hold a short label will not get one. Kerning and
  font fallback are unmodelled, so the error is a few percent either way.
- Two colour schemes and an override is more surface than a single scheme, and
  the hash-based default places similar-coloured siblings next to each other
  by construction — one warm band — so the gap is what tells them apart.

### Neutral

- No IDL change and no new paint opcode; the widget renders through calls that
  already exist, so it reaches every export lane that batched rects reach —
  unlike ADR-0159's concave-polygon route, which the headless scene lane drops.
- The widget has no persistent state of its own beyond the plot's, so a host
  holds hover and selection.

## Migration — Tier 1

Nothing to migrate: both packages are new, and the implot change is additive.
An axis that sets none of the new flags behaves exactly as before — the locks
resolve to all-false and every gesture path is the one that shipped.

The one behavioural change to an existing plot is the per-axis write-back: an
axis that a gesture did not move is no longer reassigned from the transform.
That removes a slow float drift during single-axis drags; no plot depended on
the drift.

## Verification plan — Tier 1

- **Layout unit tests** — roll-up correctness (`Self + Σ children`), tiling
  (siblings abut and never exceed the parent's span), value conservation
  against the input total, ordering under each `OrderE`, forest layout, cycle
  and malformed-input rejection, pruning counts, and orientation sign
  (`RowDist` inverts `rowSpan` for every node in both orientations).
- **Hit-test agreement** — the probe resolves to the same node the layout
  placed, for a sweep of points across every row, including the boundaries
  between abutting siblings; a frame the renderer culled resolves to *no* hit
  while the layout on its own still reports it (SD5); and a non-finite
  coordinate is rejected rather than converted, in both orientations.
- **Golden layouts** — committed for a hand-built tree in both orientations, so
  a layout change has to be deliberate.
- **Derived depth span** — `depthSpan` and `depthWindow` are pure functions so
  SD3 can be tested without a live plot: the cap at the tree's depth, the
  first frame with no plot area, a resize re-asserting the span while keeping
  the root-side edge, `ResetView` in both orientations, a steady state where a
  pan sticks, and the epsilon below which a drag must not be fought.
- **implot locks** — pure unit tests over the gesture helpers: a NoZoom axis is
  bit-for-bit unmoved by the wheel yet still pans, a NoPan axis is unmoved by a
  drag, `AxisFlagsLock` is both, the new bits do not collide with the existing
  flags, `AxisLimits` reads back what was set, and a fit — from the
  double-click *or the context menu* — skips a NoZoom axis without clearing a
  fit already pending.
- **Visual** — a gallery demo capture in both orientations and both colour
  schemes, taken from a pristine worktree build (a shared working tree risks a
  stale binary and an FFI-desynchronised capture).
- **IDS** — `designlint` over both new packages, expected clean; no raw colour
  constructors, no literal stroke widths.
- **The `play` panel (SD9)** — unit tests over both contracts and both builds:
  the resolver's precedence and each of its three rejections; folded interning
  (one node per distinct prefix, a repeated path summing, a path that is a
  prefix of another landing on the interior node), empty path elements skipped,
  the depth cut and the drop counters; node-mode two-pass resolution with a
  child ahead of its parent, an unknown or self parent demoted to a root, and a
  duplicate id dropped. Every one of them asserts the accepted rows' total
  survives, since a panel that quietly loses value is the failure with no
  symptom. Plus a tour scene (`08_icicle`) capturing both orientations, which
  is what the two notes at the end of SD9 came from.

## Status

Accepted 2026-08-27.

Built: the widget, and the `play` panel of SD9 (2026-08-02) — SD9 was written
against the panel rather than ahead of it, so it records what the binding
conceded rather than proposing it.

## References

### Method sources (clean room — papers and public documentation only)

Implemented from the documents below. `d3-flame-graph` (Apache-2.0) and
`speedscope` (MIT) were read as documentation only; no external layout source
was consulted, per ADR-0119 §SD6.

| Source | Extracted |
| --- | --- |
| Kruskal & Landwehr, *Icicle Plots: Better Displays for Hierarchical Clustering*, The American Statistician 37(2), 1983 | the icicle form itself: depth rows, width as value, children abutting under the parent |
| Gregg, *The Flame Graph*, Communications of the ACM 59(6), 2016 | the inverted orientation, self-vs-total reading, why sibling order should be stable rather than temporal |
| Stasko, Catrambone, Guzdial, McDonald, *An evaluation of space-filling information visualizations for depicting hierarchical structures*, IJHCS 53(5), 2000 | the form-choice evidence: depth rows beat nested areas for structural tasks, which is the treemap-vs-icicle call in Context |
| [d3-flame-graph](https://github.com/spiermar/d3-flame-graph) (docs) | conventional interaction vocabulary — click to zoom, double-click to reset, hover for self/total |
| [speedscope](https://github.com/jlfwong/speedscope) (docs) | the left-heavy ordering convention and why a fixed row height is preferred to a proportional one |

### Related ADRs

- [ADR-0149](./0149-implot-core-port-painter-lane.md) — the implot port: the
  custom-item lane this widget renders through, the shared text-width estimate
  it fits labels with, and the surface the per-axis locks, `Elide` and
  `RelativeLuminance` extend.
- [ADR-0159](./0159-imzero2-sankey-flow-widget.md) — the sankey widget: the
  package split, the plot-space hit-test argument, and the first user of the
  custom-item lane.
- [ADR-0156](./0156-qualitative-palette-dark-surface.md) — the qualitative
  palette the by-label colouring first cycled, before the flame band replaced
  it.
- [ADR-0031](./0031-imzero2-design-system-color.md) — the sequential palettes
  both colour modes sample: the by-label flame band and the by-depth ramp.
- [ADR-0080](./0080-packageprops-per-package-declarations.md) — the
  `package_props.go` registration both new packages carry.
- [ADR-0057](./0057-demo-registry-and-drivers.md) — the demo registry the
  gallery entry joins.
