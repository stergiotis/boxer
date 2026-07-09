---
type: how-to
audience: engineer with a specific task
status: stable
reviewed-by: "@stergiotis"
reviewed-date: 2026-07-09
---

# How to plug a custom Detail panel into the `play` app

The [`play` app](../../apps/play/)'s **Detail** tab renders a card for the row
selected in **Table** (or a point in **Projection** / an event in **Timeline**).
By default the body is the leeway card stack, or a prefix-grouped ad-hoc layout
for non-leeway results. A library that re-uses `play.PlayApp` can replace that
body with a domain-specific view — without forking the app.

The seam is one function type and two methods:

```go
// DetailContentFunc renders the body of the Detail panel for one selected row.
type DetailContentFunc func(rec arrow.RecordBatch, schema *arrow.Schema, row int64)

// Install your renderer; nil restores the built-in body.
func (inst *PlayApp) SetDetailContent(fn DetailContentFunc)

// The built-in body — exported so a custom renderer can delegate to it.
func (inst *PlayApp) RenderDefaultDetailContent(rec arrow.RecordBatch, schema *arrow.Schema, row int64)
```

`PlayApp` always draws the header (row position + entity identity) itself; your
renderer only draws the **body** below it. (`c` in the examples is the egui2
bindings import, aliased `c` as elsewhere in the app.)

## Install the renderer

Set it once, right after you build the `PlayApp` — the same place
[`PlayLauncher.Mount`](../../apps/play/app_register.go) does its wiring. It takes
effect on the next frame; there is nothing to unregister:

```go
inner := play.NewPlayApp(client, graph, initSQL)
inner.SetDetailContent(func(rec arrow.RecordBatch, schema *arrow.Schema, row int64) {
	// Pattern A or B, below. Capture `inner` to reach RenderDefaultDetailContent.
})
```

## Pattern A — wrap the built-in (most common)

Keep the leeway/ad-hoc card and append your own widgets under it. Delegating to
`RenderDefaultDetailContent` means you inherit the schema-shape handling and the
scroll contract for free:

```go
inner.SetDetailContent(func(rec arrow.RecordBatch, schema *arrow.Schema, row int64) {
	inner.RenderDefaultDetailContent(rec, schema, row) // the normal card…
	c.Separator().Horizontal().Send()
	for rt := range c.RichTextLabel("MISSION CONTEXT") {
		rt.Small().Weak()
	}
	// …then your domain widgets.
})
```

## Pattern B — replace the body entirely

Render your own layout straight from the record batch. `ValueStr` reads any
column generically; [`play_format.go`](../../apps/play/play_format.go)'s
`formatCell` is the richer in-repo reference (NULL handling, time formatting):

```go
inner.SetDetailContent(func(rec arrow.RecordBatch, schema *arrow.Schema, row int64) {
	for range c.ScrollArea().Vscroll(true).KeepIter() { // see the scroll contract below
		for i := 0; i < schema.NumFields(); i++ {
			col := rec.Column(i)
			for range c.Horizontal().KeepIter() {
				for rt := range c.RichTextLabel(schema.Field(i).Name) {
					rt.Weak()
				}
				c.Label(col.ValueStr(int(row))).Truncate().Send()
			}
		}
	}
})
```

## The contract your renderer must respect

- **You run inside the pane's `Vertical` scope.** Emit widgets directly; do not
  open a top-level panel expecting to be at the pane root.
- **`rec` is non-nil and `row` is in range.** The Detail panel gates on a valid
  schema and a selected row before your renderer runs, so you need not re-check
  the bounds ([`play_detail_panel.go`](../../apps/play/play_detail_panel.go)).
- **Own your scrolling.** The Detail dock tab deliberately does *not* wrap the
  body in an outer `ScrollArea`, because the leeway card table self-scrolls (an
  outer scroll hands it unbounded height and crops its tail sections). Content
  that can overflow and is not self-scrolling must add its own `ScrollArea`, as
  Pattern B does and as the ad-hoc branch of the built-in does.
- **Scope your id stack.** Any multi-child widget must scope its widget ids
  (`c.IdScope(...)` off a dedicated `c.WidgetIdStack`); a mismatched id stack
  vets clean but panics at render. See
  [AGENTS.md § egui2 / imzero2](../../AGENTS.md).
- **Render-thread only.** The renderer is called every frame from the frame
  loop, like any other widget body — no blocking work, and no `Fetcher.Fetch*`
  calls inline (they must run from `StateManager.Sync`).

## Further reading

- [The Detail panel](../../apps/play/play_detail.go) and its
  [PanelI observer](../../apps/play/play_detail_panel.go) — where the header is
  drawn and the body is dispatched.
- [ADR-0097](../adr/0097-play-reactive-query-graph.md) — the reactive
  query-graph and the PanelI channel model the Detail tab plugs into.
- [imzero2 widget authoring](../../public/thestack/imzero2/egui2/) — the id
  stack, deferred bodies, and the FFI render pipeline your renderer emits into.
