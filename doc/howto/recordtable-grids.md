---
type: how-to
audience: app author showing an Arrow record as a table
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Written against
> `widgets/recordtable` as play's results grid uses it (2026-10-14).

# Showing an Arrow record as a table

[`widgets/recordtable`](../../public/thestack/imzero2/egui2/widgets/recordtable)'s
`RenderGrid` draws a record as an egui_table grid: a row-number column,
sortable headers, cells whose whole rect selects the row, and column widths
the user drags and keeps. Its package doc has the minimal call. This page is
the wiring around it and the two choices the call cannot make for you.

## Keep the state where it survives the frame

The grid is immediate-mode: everything it remembers lives in values you own.
Make them fields of your app, never locals of the render function:

```go
type App struct {
    grid     recordtable.State     // the fit, the width read-back, the cell replay
    sort     recordtable.SortState // which column, which way
    selected int64                 // a record row, -1 for none
    widths   *colwidth.Resolver    // nil until the first frame
    widthsInit bool
}
```

A `State` or `SortState` built per frame compiles and renders, then forgets:
the sort resets on every frame and the grid fits its columns on every frame.

## Acquire the width resolver on the first frame

The store comes from the frame context, so it cannot be picked up in Mount.
Ask once, and accept that it may be absent — then widths are not kept and
everything else works:

```go
if !inst.widthsInit {
    inst.widthsInit = true
    if h, ok := ctx.(colwidth.HostI); ok {
        if store := h.ColumnWidthStore(); store != nil {
            d := styletokens.ActiveDensity()
            res, err := colwidth.New(store, colwidth.Opts{
                AppId: ctx.AppId(), InstanceKey: ctx.InstanceKey(),
                MinPoints: float64(recordtable.DragMinWidth(d)),
                MaxPoints: float64(recordtable.MaxColumnWidth),
            })
            if err == nil {
                _ = res.Load() // a failed load only means no stored widths this session
                inst.widths = res
            }
        }
    }
}
```

The two bounds are the grid's own drag bounds; a stored floor below the drag
floor makes a column jump on the next start. One resolver serves every grid
in the app: give each grid its own `TableTag` (or `ScopeKey`, which the tag
defaults to) and keep it stable, because stored widths are filed under it.
[table-column-widths](./table-column-widths.md) explains the tiers and what
the resolver does with a drag.

## Describe the columns

A `Column` names an Arrow column and how to show it. The defaults cover a
plain value: the field name as the caption, `name — type` on hover, the
un-glossed value as the text, left-aligned for strings.

- **A caption that is not the field name** goes in `Header`; small lines
  under it (a type tag, a unit) in `Sub`. Stored widths stay keyed on the
  field name, so renaming a caption does not lose them.
- **A cell that needs formatting, a colour or a link** gets a `Cell`
  function. It receives the record row and returns the text and the look;
  the grid sets selection and truncation itself.
- **A computed column** has `Arrow: -1`, a `Header` and a `Cell`. It is
  never sortable.
- **A column rendered two ways in two grids** needs `WidthView` (one per
  grid) or `WidthType`, or a width fitted to one view is carried to the
  other through the column tier.

## Render, then act on what came back

```go
order := inst.sort.OrderFor(rec, rec.NumRows())
res := recordtable.RenderGrid(recordtable.Input{
    Ids: ids, ScopeKey: "jobs", Record: rec, Columns: cols,
    Widths: inst.widths, Sort: &inst.sort, Order: order,
    Selected: inst.selected, State: &inst.grid,
})
if res.SortClicked >= 0 {
    inst.sort.Clicked(res.SortClicked)
}
if res.Clicked >= 0 {
    inst.selected = res.Clicked
}
```

The grid reports a header click and a row click; it applies neither. A host
that skips the `SortClicked` line has headers that show "click to sort" and
do nothing. Both clicks, like the widths, are read back from the host, so
they arrive one frame after they were made.

`Selected` and `Clicked` are record rows, not display positions, so a
selection stays on its row across a sort and a page turn. `Order` can be a
filter's rows instead of the sort's; `PageStart` and `PageEnd` window it for
a pager.

## Two choices the grid cannot make for you

**`Refit` — when the columns should fit their content again.** The grid fits
once per column set, on the first frame with rows. Set `Refit` on a frame
whose content is a new answer — play sets it on every new query result, so
the widths belong to the result on screen. Leave it off for a table that is
polled or appended to: there the content changes under the reader, and a
fit on each change moves the columns while they read. A width the user
dragged sits every fit out.

**`CellsKey` — whether unchanged cells may be replayed.** By default the grid
rebuilds its cells every frame, which is cheap for a few hundred rows on a
desktop. Where the cells show up in a profile — play's did, on a handheld
and under wasm — pass a `CellsKey`: a comparable value that changes whenever
anything the cells read changes that the grid cannot see itself. The grid
already accounts for the window, the selection, the sort and the columns;
the key must cover the rest — the record, the order, and everything a
`Cell` function reads (a display option, a background job's progress). A
key that misses one shows stale cells until something else moves. Leave it
nil unless you have measured a reason.

## When not to use RenderGrid

`RenderGrid` draws one grid row per record row. A grid that breaks that —
play's per-attribute view, which draws several rows per entity — builds on
the same parts directly: `SortState` for the sort, `Chrome` for the inset
cells and headers, `DragMinWidth` for the floor. Play's
`play_table_attr.go` is the worked example.

## See also

- The [recordtable package doc](../../public/thestack/imzero2/egui2/widgets/recordtable/doc.go)
  — the minimal call.
- [table-column-widths](./table-column-widths.md) — the resolver, its tiers,
  and resetting a width.
- [ADR-0151](../adr/0151-table-column-width-overrides.md) — why widths are
  Go's and how they are keyed.
- [ADR-0267](../adr/0267-imzero2-go-widget-api-contract-immediate-and-semi-retained.md)
  — the immediate-mode widget contract the grid follows.
