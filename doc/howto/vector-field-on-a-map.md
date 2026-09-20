---
type: how-to
audience: engineer with a specific task
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** The recipes are the ones the
> `flowonmap` gallery demo runs; the page itself has not been reviewed.

# How to draw a vector field on a map

You have a gridded two-component field — a 10 m wind, an ocean current, the
gradient of a scalar — possibly millions of vectors per time step and many
steps, and you want to see the flow on a map that still pans and zooms.

Two packages do it
([ADR-0249](../adr/0249-vector-fields-on-the-map-particles-over-a-batched-segment-opcode.md)):
[the vectorfield package](../../public/science/geo/vectorfield/) is the data contract and
an in-memory pyramid behind it, and
[the flowoverlay package](../../public/thestack/imzero2/egui2/widgets/portolan/flowoverlay/)
draws a source as drifting particles inside a portolan map's overlay callback.
Read [egui2_hl_flowonmap_demo.go](../../public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_flowonmap_demo.go)
beside this page.

**What the picture does not say.** The animation shows direction and relative
speed, not transport: a particle's pace is a screen quantity, the same at every
zoom. A trail is a streamlet of the field at the display time, not a
trajectory. Between two steps the field is blended linearly, so a moving
front fades across instead of travelling. Say so next to the map if your
readers will take the particles for parcels of air.

## 1 Get the data into a `Grid`

Decoding GRIB or NetCDF is not in the tree. Whatever reads your file hands the
pyramid one step at a time through `vectorfield.StepLoaderI`:

```go
func (l *myLoader) LoadStepE(ctx context.Context, step int) (vectorfield.Grid, error)
```

A `Grid` is rows north to south, columns west to east, `U` east and `V` north,
`NaN` for missing, with `West` and `North` the position of sample (0, 0) — a
node, not a pixel edge. **Everything below is the loader's job, and getting it
wrong produces a plausible map**, because each of these errors is smooth:

- **Components must be earth-relative.** Models on Lambert, polar-stereographic
  or rotated grids usually store components along the grid's axes. Rotate them
  — after collocating staggered components, before resampling to lat/lon.
- **Row order.** GRIB commonly scans north to south, many NetCDF products
  south to north. Flip the rows; do *not* flip the sign of `V`.
- **Longitudes 0–360 are fine**, and so is a repeated 360° column; the pyramid
  detects a full circle and drops a repeat. A regional grid is never wrapped.
- **Missing values become `NaN` before anything else** — bitmap, fill value
  (tested on the raw integer of a packed variable), sentinels like 9999 and
  9.999e20. Set both components where either is missing.
- **Pick the level by surface kind and value**, not by a number: a 10 m wind
  and a 10 hPa wind share one.

The [survey behind the ADR](../adr-background-work/vector-field-flow-visualization-survey.md)
§6 has who shipped each of these.

## 2 Build the source

```go
src, err := vectorfield.NewPyramidE(ctx, vectorfield.Meta{
	Name: "GFS 10 m wind", Quantity: "wind", Unit: "m/s",
	Surface:  vectorfield.Surface{Kind: vectorfield.SurfaceKindHeightAboveGround, Value: 10, Unit: "m"},
	Steps:    steps,   // valid times; they need not be evenly spaced
	SpeedMax: 40,      // tops the palette and sets the fastest pace
}, loader, vectorfield.PyramidOptions{CacheBytes: 2 << 30})
```

Leave the geometry fields of `Meta` zero and the first step loaded supplies
them. `CacheBytes` bounds the decoded steps in bytes; the two most recently
used are kept whatever it says.

Run the conformance suite against any source you write yourself:

```go
vectorfieldtest.Run(t, src, vectorfieldtest.Options{Covered: true, MissingStep: -1})
```

It checks the contract's form — gutter, node registration, one spelling of
missing, the request as the bound on size, wrap as the source's property. It
cannot tell whether you rotated your components correctly; test that against
a fixture with a known answer (a uniform geographic westerly encoded in the
grid's own frame is the classic one).

## 2a Or leave the field in ClickHouse

When the field is already in a table, skip the loader and the pyramid:
[the sqlfield package](../../public/science/geo/vectorfield/sqlfield/) is a
source whose every window is one reduction query
([ADR-0250](../adr/0250-a-sql-backed-vector-field-source-and-plays-vector-field-pane.md)),
so nothing larger than a window leaves the server and the field may be any
size. It costs a query per step for each settled view, where the pyramid
costs none.

```go
src, err := sqlfield.NewSourceE(ctx, queryer, sqlfield.Relation{From: "weather.wind_10m"},
	sqlfield.Options{Meta: vectorfield.Meta{Name: "10 m wind", Unit: "m/s", SpeedMax: 40}})
```

The relation yields `lat`, `lon`, `u`, `v` and optionally `t`, one row per node
per step, on a grid regular in latitude and longitude. Everything §1 makes the
loader's job is the relation's here — rotate, filter to one level and run,
`nullIf` the sentinels — and `Relation.Head` carries a `WITH` list when that
takes a query. `NewSourceE` refuses a step with more rows than the grid has
nodes and nodes off a regular grid; it cannot tell whether components were
rotated. `queryer` is whatever runs a statement with parameters and returns
Arrow. Run the conformance suite of §2 against your relation and a real
server, as the package's integration test does.

In the `play` app this is the **Vector field** pane: name the relation
`vector_field` in a CTE and the pane does the rest. Its help page has a query
that needs no table.

## 3 Draw it

```go
layer := flowoverlay.New(src, flowoverlay.Options{})
defer layer.Close()

// every frame:
layer.SetTime(displayTime)
m.Render(w, h, func(p portolan.Projector) {
	land.Draw(p, atlas, landoverlay.DefaultStyle()) // call order is paint order
	layer.Draw(p)
})
```

The layer asks the source for windows on goroutines of its own and keeps the
last good one on screen; nothing in `Draw` waits. `layer.Opts` is read every
frame, so a slider writes a field. The ones that matter:

| Option | What it trades |
| --- | --- |
| `Density` | particles per 1000 px²; the count follows the canvas |
| `SpeedMax` | the magnitude at full pace and at the top of the palette — set it to the field's realistic maximum, not its theoretical one, or everything crawls in the palette's slow colours |
| `MinPacePx` | the floor that keeps slow flow creeping; raise it for a field that is mostly weak |
| `TrailTicks`, `MaxAgeTicks` | trail length; how long a particle may circle an eddy |
| `Palette` | trails over light tiles want a darker ramp than the default |
| `Paused` | an animating layer keeps its window from idling; this stops it |

`layer.At(ll)` reads the field under the pointer. It returns the vector mean's
components *and* the scalar mean of the magnitude; at a low zoom they differ
wherever directions disagree inside a sample. Show speed from the scalar mean
and say which one a direction readout uses.

## 4 Under a graph

The layer takes no pointer, so it composes with a hosted graphview guest
([graph-on-a-map](./graph-on-a-map.md)) by call order alone: draw the layer,
then `gv.HostedPaint`.

## 5 Captures and tests

A capture must not depend on a goroutine or the clock:

```go
layer.Opts.Synchronous = true // ask the source on the frame goroutine
layer.Opts.FixedTicks = 120   // run exactly this many ticks, then rest
```

The picture is then a function of `Seed`, the view and the field, complete on
the first frame.

## Troubleshooting

- **Nothing moves, `Stats().WindowCols` is 0.** No window has arrived: read
  `Stats().LastError`. A request is retried after a short back-off.
- **Particles only in part of the map.** That is where the source has data; a
  regional field gathers its particles inside itself.
- **Everything crawls at the floor pace.** `SpeedMax` is far above the field's
  values, or the unit is not what you think.
- **The flow turns smoothly the wrong way away from the middle of a regional
  model.** Grid-relative components, not rotated (§1).
- **A gap between the trails and the coast.** Sampling is strict: a particle
  stops a sample short of missing data and never crosses it. The gap is one
  sample of the window on screen and shrinks as you zoom in.

## References

- [ADR-0249](../adr/0249-vector-fields-on-the-map-particles-over-a-batched-segment-opcode.md)
  — the decisions, and what each guards against.
- [The survey](../adr-background-work/vector-field-flow-visualization-survey.md)
  — what other implementations do and the defects they shipped.
- [ADR-0250](../adr/0250-a-sql-backed-vector-field-source-and-plays-vector-field-pane.md)
  — the ClickHouse-backed source and play's pane.
- [ADR-0204](../adr/0204-leaflet-map-core-port.md) — the map and its projector.
- The `flowonmap` demo in the widget gallery.
