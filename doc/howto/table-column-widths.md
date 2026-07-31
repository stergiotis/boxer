---
type: how-to
audience: app author
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Describes the width machinery as of
> 2026-07-31. The resolver and both table wires exist; **no app is wired to
> them yet** — the seam that would let an app reach stored overrides is still
> open (see [Not wired yet](#not-wired-yet)). Treat the call-site snippets as
> the intended shape, not as code copied from a working adopter.

# Controlling table column widths

By default both table surfaces size their own columns and remember what the
user dragged, in egui memory that nothing serializes — so every adjustment
dies at app exit, and moving a table to another pane forgets it.
[ADR-0151](../adr/0151-table-column-width-overrides.md) puts Go in charge
instead: widths resolve from stored user overrides, and the user's drags are
captured back.

This page covers what an app author has to know to use that, and the
behaviours that will surprise you if you don't.

## The model in one paragraph

A width resolves through three tiers, most specific first: **instance** (this
column, in this table), **shape** (this column, in any table with the same
column set), **column** (this column, anywhere in the app). Miss all three and
you get the default the call site passes. Columns are identified by *what they
are* — name plus render type — not by position, so reordering columns keeps
their widths and a type change deliberately drops the override.

## Using the resolver

`egui2/colwidth` is pure Go and holds no egui state. One resolver per app,
built at Mount once storage is available:

```go
res, err := colwidth.New(store, colwidth.Opts{AppId: myAppId})
if err != nil { /* … */ }
if err := res.Load(); err != nil { /* non-fatal: defaults still work */ }
```

Per frame, describe the columns, resolve, and pass the epoch:

```go
cols := []colwidth.Column{
    {Name: "name",  Type: "String"},
    {Name: "count", Type: "UInt64"},
}
widths := res.Resolve(tableTag, cols, fontSize, myDefaults)

for i, w := range widths {
    c.EtColumn(float32(w)).Resizable(true).Send()
    _ = i
}
et := c.EndETable(ids.PrepareStr(tableTag), numRows, rowH, 1, 0).
    ApplyWidths(res.Epoch(tableTag))
```

After the table has rendered, feed back what the crate settled on and let the
debounce write:

```go
if fetched, ok := et.ColumnWidths(); ok {
    res.Observe(tableTag, cols, toF64(fetched), fontSize, firstShow, time.Now())
}
if _, err := res.Flush(time.Now()); err != nil { /* log; entries retry */ }
```

`Flush` is safe to call every frame — entries still moving are left pending,
and a failed write stays dirty so the next call retries rather than dropping a
width the user set.

## Things that will surprise you

**Everything lags one frame.** `ColumnWidths()` reports what the *previous*
frame settled on, like every other read-back in the binding. `ok` is false
until a table has shown once.

**Growth is captured like a drag.** egui_table grows a column to fit the
widest *visible* cell and stores the result. The resolver cannot tell that
apart from a drag, so scrolling wider content into view can widen a column and
persist it. This matches the "fit it, keep it" stance the ADR takes for
double-click autofit, but it does mean widths ratchet upward.

**Don't call `ApplyWidths` with a fresh epoch every frame.** The epoch means
"my resolved widths changed". `Resolve` bumps it only when they actually did.
Feeding a counter would re-assert widths on every frame and make dragging
impossible — which is the failure the epoch exists to prevent.

**`c.Table` and `c.NewTable` lose an in-flight drag when the epoch changes.**
egui_extras exposes no way to seed its stored widths, so the apply there is a
state reset. On the frame the epoch changes, a drag in progress on that table
is discarded. Accepted, because overrides only change on user action. These
surfaces also have **no read-back**: they apply overrides but do not capture
drags, so their widths only change when Go says so.

**The shape tier is read-only.** A drag writes the instance and column tiers.
The shape tier is matched on read — it exists so a table under a new tag can
inherit widths — but nothing writes it yet.

## Clearing an override

`Clear` returns one column to defaults; `ClearAll` does a whole table. Both
tombstone the stored rows and drop any unflushed capture, so a drag that has
not been written yet cannot resurrect the value a moment later.

```go
if err := res.Clear(tableTag, cols[i]); err != nil { /* log */ }
if err := res.ClearAll(tableTag, cols); err != nil { /* log */ }
```

The next `Resolve` returns different widths, bumps the epoch, and the table
re-seeds — the call site does nothing else.

**On the gesture.** ADR-0151 M6 calls for this on a header *context menu*.
egui2 has no context-menu binding, so there is nothing to hang it on today.
Until one exists, the gesture is a secondary click on the header cell:

```go
et.BeginHeaders(0, colIdx)
if c.LabelAtoms(headerAtoms).SendResp().HasSecondaryClicked() {
    _ = res.Clear(tableTag, cols[colIdx])
}
et.EndHeaders()
```

This is a recipe, not a component: header content is app-specific, and a
helper that rendered it would be more intrusive than the two lines it saved.

## Not wired yet

No app uses any of this. The resolver reads and writes column-width facts, and
an app cannot reach a fact store: `MountContextI` exposes `Storage()` and
`Bus()`, and handing an app a record store is not an option because its
executor takes raw SQL, which would put an app outside the capability model.
ADR-0151's M4 is blocked on that seam; the options are costed in
[persist-api-surface-recordstore](../adr-background-work/persist-api-surface-recordstore.md).

Until it is resolved, `colwidth.StoreI` can be satisfied by anything with the
three column-width methods — including `factsstore.InMemoryFactsStore`, which
is what the resolver's own tests use. That is enough to develop against, and
not enough to ship a user-visible feature.

## See also

- [ADR-0151](../adr/0151-table-column-width-overrides.md) — the decision, the
  tier model, and the wire design.
- [ADR-0148](../adr/0148-app-workingsets.md) — the data-centricity invariant
  the storage choice follows.
- [`egui2/colwidth`](../../public/thestack/imzero2/egui2/colwidth) — the
  resolver; its package doc covers the apply/capture state machine.
