---
type: how-to
audience: engineer with a specific task
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to plot an ECDF with confidence band

Four recipes covering the common use cases of the `ecdf` widget. The
widget paints into a plot the host opens with
[`implot.Begin`](../implot/) and closes with `End`; every snippet
assumes the caller is inside an imzero2 render loop with a valid
`ids *c.WidgetIdStack`.

## Recipe 1 — Sorted iid sample

The default workflow: a complete sample is held in memory, sorted,
and painted as a step ECDF with a 95% simultaneous Berk-Jones
confidence band (the zero `Style`).

```go
import (
    "slices"

    "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/ecdf"
    "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

sample := []float64{ /* iid observations */ }
slices.Sort(sample)

p := implot.Begin(ids, "##ecdf-p99", 600, 300)
p.SetupAxes("latency (ms)", "F(x)", implot.AxisFlagsNone, implot.AxisFlagsNone)
p.IncludeY(0)
p.IncludeY(1)
res := ecdf.Paint(p, ecdf.Input{
    Style:  ecdf.Style{SeriesName: "p99-latency"},
    Sorted: sample,
    Band:   ecdf.BandExact,
})
p.End()
```

`Paint` declares the band first (it sits under the curve), then the
step polyline. `res.Err` carries the band library's rejection of the
input, such as an unsorted sample; the curve is still drawn where it
can be.

## Recipe 2 — Streaming sketch (t-digest)

When the sample is too large to keep in memory, evaluate F_n at a
fixed grid from a sketch and pass the grid with the total count. The
band's calibration depends on `N`, not on the grid resolution.

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

st := ecdf.Style{Method: ecdfbands.BandMethodBerkJones, Alpha: 0.01}
band := ecdf.BandPreview
if ecdf.BandReady(st, n) {
    band = ecdf.BandExact
} else {
    _ = ecdf.EnsureBandJob(ecdf.BandJobKey(ids.ProbeSeq("band-job")), nil, n, st)
}

p := implot.Begin(ids, "##ecdf-streaming", 800, 400)
p.IncludeY(0)
p.IncludeY(1)
_ = ecdf.Paint(p, ecdf.Input{Style: st, Xs: xs, FnAt: fn, N: n, Band: band})
p.End()
```

The exact band at large `N` is an O(n²) inversion. `BandExact`
blocks on it unless it is cached, so a frame that cannot wait paints
`BandPreview` — the closed-form DKW band, wider and instant — while
`EnsureBandJob` warms the exact one in the background. The returned
snapshot carries progress for a progress row; `CancelBandJob` stops
a warm-up whose window closed.

## Recipe 3 — Comparing band methods on the same sample

For visual comparison of how different families shape the band,
paint each into its own plot.

```go
methods := []ecdfbands.BandMethodE{
    ecdfbands.BandMethodBerkJones,
    ecdfbands.BandMethodDKW,
    ecdfbands.BandMethodEqualPrecision,
    ecdfbands.BandMethodHigherCriticism,
}

for _, m := range methods {
    p := implot.Begin(ids, fmt.Sprintf("##ecdf-method-%d", m), 400, 300)
    _ = ecdf.Paint(p, ecdf.Input{
        Style:  ecdf.Style{Method: m, Alpha: 0.05, SeriesName: fmt.Sprintf("method=%d", m)},
        Sorted: sample,
        Band:   ecdf.BandExact,
    })
    p.End()
}
```

## Recipe 4 — Cursor crosshair + status line

Wire a hover-driven vertical crosshair on the plot plus a verbose,
plain-language readout beneath it that describes the cursor's reading
— `F_n(x)`, the nearest observation (or grid point), and the
confidence interval with its provenance (exact family + calibration n,
or the conservative DKW preview).

```go
in := ecdf.Input{Style: ecdf.Style{SeriesName: "p99-latency"}, Sorted: sample, Band: ecdf.BandExact}

p := implot.Begin(ids, "##ecdf-p99", 600, 300)
ch := ecdf.At(p, in)                    // reads the hover, computes F_n / band / nearest
_ = ecdf.Paint(p, in)                   // band + ECDF polyline
ecdf.PaintCrosshair(p, in.Style, ch)    // vertical crosshair; no-op when !ch.Valid
p.End()

ecdf.WriteStatusLine(ch)                // the verbose readout below the plot
```

The grid path is the same with `Xs`, `FnAt` and `N` set instead of
`Sorted`. `At`, `Paint` and `PaintCrosshair` all go between `Begin`
and `End`.

For an inline placement where the multi-line paragraph is too tall (a
status bar, a dense table row), use `ecdf.WriteStatusLineTerse(ch)`
instead — one compact line in standard notation, no-op when `!ch.Valid`.
(`boxenplot` mirrors this the other way: terse `WriteStatusLine`, with
`WriteStatusLineVerbose` the explaining paragraph.)

`Crosshair.Valid` is false when the cursor is outside the plot or no
plot has rendered yet this session. `PaintCrosshair` no-ops on
`!ch.Valid`; `WriteStatusLine` does *not* — it emits a one-line hover
hint padded to the same `ecdf.ReadoutLineCount` height as the full
readout, so the status area never reflows as the cursor enters and
leaves the curve.

## Verification

The widget gallery's `ecdf` demo
([egui2_hl_ecdf_demo.go](../../demo/apps/widgets/egui2_hl_ecdf_demo.go))
paints a sample with the crosshair and lets you switch band methods
and α levels live. A correct render shows a translucent band behind a
step curve. To capture it as an image, use the demo registry's
screenshot driver (`IMZERO2_SCREENSHOT_DIR`; see the Screenshots
section of [AGENTS.md](../../../../../../AGENTS.md)).

## Troubleshooting

- **Symptom:** `Paint` returns an error naming an unsorted sample.
  **Cause:** `Sorted` is not non-decreasing.
  **Fix:** `slices.Sort(sample)` before painting. The widget does
  not sort in place, to keep the contract predictable.

- **Symptom:** the error names a grid length mismatch.
  **Cause:** `Xs` and `FnAt` have different lengths — usually `fn`
  was not sized to `len(xs)` after building the x-grid.
  **Fix:** allocate both as `make([]float64, gridN)` together.

- **Symptom:** the frame stalls when the plot first shows a large
  sample.
  **Cause:** `BandExact` blocks on the O(n²) inversion while its
  `(n, α, method)` band is not cached.
  **Fix:** the Recipe 2 pattern — `BandReady`, `BandPreview` meanwhile,
  `EnsureBandJob` in the background.
