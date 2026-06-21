---
type: how-to
audience: engineer with a specific task
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to plot an ECDF with confidence band

Three recipes covering the common use cases of the `ecdf` widget.
All snippets assume the caller is already inside an imzero2 render
loop with a valid `*c.WidgetIdStack` and intends to wrap the
emission in a `c.Plot(id).Send()` call.

## Recipe 1 — Sorted iid sample

The default workflow: a complete sample is held in memory, sorted,
and rendered as a step ECDF with a 95% simultaneous Berk-Jones
confidence band.

```go
import (
    "slices"

    c "github.com/stergiotis/pebble2impl/src/go/public/thestack/imzero2/egui2/bindings"
    "github.com/stergiotis/pebble2impl/src/go/public/thestack/imzero2/egui2/widgets/ecdf"
)

sample := []float64{ /* …iid observations… */ }
slices.Sort(sample)

r := ecdf.New().SeriesName("p99-latency")
_ = r.Render(sample)

c.Plot(ids.PrepareStr("ecdf-p99")).
    Width(600).Height(300).
    XAxisLabel("latency (ms)").YAxisLabel("F(x)").
    Legend().IncludeY(0).IncludeY(1).
    Send()
```

The `Render` call enqueues the band rectangles and the ECDF
polyline; the subsequent `c.Plot(…).Send()` drains them and
renders inside a single egui_plot block.

## Recipe 2 — Streaming sketch (t-digest)

When the sample size is too large to keep in memory, evaluate F_n
at a fixed grid via a sketch and use `RenderGrid`.

```go
const n = 5_000_000

digest := tdigest.NewTDigest()
for x := range stream {
    digest.Push(x)
}

xs := make([]float64, 200)
fn := make([]float64, 200)
xmin, xmax := digest.Min(), digest.Max()
for i := range xs {
    xs[i] = xmin + (xmax-xmin)*float64(i)/float64(len(xs)-1)
    fn[i] = digest.CDF(xs[i])
}

r := ecdf.New().Method(ecdfbands.BandMethodBerkJones).Alpha(0.01)
_ = r.RenderGrid(xs, fn, n)

c.Plot(ids.PrepareStr("ecdf-streaming")).
    Width(800).Height(400).
    Legend().IncludeY(0).IncludeY(1).
    Send()
```

The band's calibration depends on `n` (the total observations the
sketch consumed), not on the grid resolution.

## Recipe 3 — Comparing band methods on the same sample

For visual comparison of how different families shape the band,
render each in its own subpanel.

```go
methods := []ecdfbands.BandMethodE{
    ecdfbands.BandMethodBerkJones,
    ecdfbands.BandMethodDKW,
    ecdfbands.BandMethodEqualPrecision,
    ecdfbands.BandMethodHigherCriticism,
}

for _, m := range methods {
    r := ecdf.New().Method(m).Alpha(0.05).
        SeriesName(fmt.Sprintf("method=%d", m))
    _ = r.Render(sample)
    c.Plot(ids.PrepareSeq(uint64(0xECDF + uint64(m)))).
        Width(400).Height(300).
        Send()
}
```

## Recipe 4 — Cursor crosshair + status line

Wire a hover-driven vertical crosshair on the plot plus a verbose,
plain-language readout beneath it that describes the cursor's reading
— `F_n(x)`, the nearest observation (or grid point), and the
confidence interval with its provenance (exact family + calibration n,
or the conservative DKW preview). The widget reads the cached r15 plot
hover register each frame, computes the lookups, and emits one
`PlotVLine` plus a fixed-height stack of `LabelAtoms` rows.

```go
import (
    c "github.com/stergiotis/pebble2impl/src/go/public/thestack/imzero2/egui2/bindings"
    "github.com/stergiotis/pebble2impl/src/go/public/thestack/imzero2/egui2/widgets/ecdf"
)

// Construct the plot id ONCE as an AbsoluteWidgetId. ids.PrepareStr
// is not interchangeable here — it XORs the surrounding stack-top
// into the derived id, and the renderer's hover-id match would fail
// silently. See EXPLANATION.md §"Plot-id filter".
plotID := c.MakeAbsoluteIdStr("ecdf-p99")

r := ecdf.New().SeriesName("p99-latency")
ch := r.At(plotID, sample)   // reads cached hover, computes F_n/band/nearest
_ = r.Render(sample)          // band rectangles + ECDF polyline
r.PaintCrosshair(ch)          // vertical crosshair (no-op when !ch.Valid)

c.Plot(plotID).
    Width(600).Height(300).
    XAxisLabel("latency (ms)").YAxisLabel("F(x)").
    Legend().IncludeY(0).IncludeY(1).
    Send()

c.AddSpace(styletokens.PaddingDefault(styletokens.DensityStandard))
ecdf.WriteStatusLine(ch)      // verbose readout: "Cursor at value x = …" /
                              // "Empirical CDF F_n(x) = … — an estimated …% …" /
                              // "Nearest observation X_(i) = …" /
                              // "Simultaneous 95% confidence band (exact, Berk-Jones, n=…): F(x) ∈ […]"
```

The grid path is symmetric: swap `r.At(plotID, sample)` for
`r.AtGrid(plotID, xs, fnAt, n)` after `r.RenderGrid(xs, fnAt, n)`.
Order of `At` / `Render` / `PaintCrosshair` is free as long as
everything lands before `c.Plot(plotID).Send()` drains the
registers.

`Crosshair.Valid` is false when the cursor is outside the plot,
when the hover refers to a different plot id, or when no plot has
rendered yet this session. `PaintCrosshair` no-ops on `!ch.Valid`;
`WriteStatusLine` does *not* — it emits a one-line hover hint
("Hover over the curve to read F(x) and its confidence interval.")
padded to the same `ecdf.ReadoutLineCount` height as the full readout,
so the status area never reflows as the cursor enters and leaves the
curve. (If you build your own readout, the pure `formatReadout` is not
exported; reproduce the hint yourself on the `!ch.Valid` branch.)

To name the band in an always-visible line of your own (independent of
hover) — e.g. while the cursor is off the curve — read
`r.BandMethod()` for the configured exact family.

## Verification

The widget is covered by the carousel screenshot tour:

```bash
./scripts/dev/hmi_screenshots.sh
```

look for `ecdf.png` under `doc/screenshots/tour/`. A correct
render shows a solid translucent fill behind a crisp step curve.
The carousel's `ecdf` demo also lets you toggle live between band
methods and α levels to inspect the geometric differences.

## Troubleshooting

- **Symptom:** band renders as dense diagonal hatching across the
  plot.
  **Cause:** the underlying `PlotPolygon` primitive was invoked
  with a non-convex single polygon — egui_plot's tessellator can't
  fill it cleanly.
  **Fix:** ensure you are calling `Render` or `RenderGrid` on this
  widget rather than constructing your own band polygon — the
  widget already emits one convex rectangle per ECDF plateau to
  bypass this case.

- **Symptom:** `Render` returns `sample not sorted at i=…`.
  **Cause:** input slice is not non-decreasing.
  **Fix:** `slices.Sort(sample)` before calling. The widget does
  not sort in place to keep the contract predictable.

- **Symptom:** `RenderGrid` returns `xs and fnAt length mismatch`.
  **Cause:** the two grid slices are different lengths — usually
  forgotten to size `fn` to `len(xs)` after building the x-grid.
  **Fix:** allocate both as `make([]float64, gridN)` together.

- **Symptom:** the crosshair never appears while hovering and
  `WriteStatusLine` stays absent, even though `c.Plot.Send()` is
  clearly being called.
  **Cause:** the plot id was constructed via `ids.PrepareStr(...)`
  instead of `c.MakeAbsoluteIdStr(...)`. `PrepareStr → Derive` XORs
  the surrounding `WidgetIdStack` top into the hash, so the id
  flowing into the plot block (and stored on hover) doesn't equal
  what `plotID.Derive()` returns inside `Renderer.At`.
  **Fix:** `plotID := c.MakeAbsoluteIdStr("…")` once at frame top;
  pass the same `plotID` to both `r.At(plotID, …)` and
  `c.Plot(plotID).Send()`. AbsoluteWidgetId's `Derive()` is the
  identity (modulo non-zero check), so the round-trip is stable.
