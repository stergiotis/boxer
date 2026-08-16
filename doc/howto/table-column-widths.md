---
type: how-to
audience: app author
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Describes the width machinery as of
> 2026-07-31, with play's attr grid as the first adopter (ADR-0151 M4); the
> snippets follow that call site.
>
> Verified against live ClickHouse on that date, at the Go layer: a captured
> width resolves back through a fresh store client, the column tier reaches
> another table, the font rescale survives the round-trip, a clear stays
> cleared, and overrides do not cross apps. Two separate processes were also
> run against the real `boxer.facts` — one captured a width, the other
> resolved it.
>
> Not verified: the rendered behaviour. Nobody has dragged a column in a
> running window and seen the width come back. Everything between the drag
> and the store — the read-back register, the epoch apply, the capture
> gate — is covered by tests rather than by use.

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
built on the first Frame — not at Mount, because the store arrives as a
frame-context capability (see [Reaching a store](#reaching-a-store)).

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

**Nothing is captured for two reports after the widths change.** Because of
that lag, the first report following a re-seed describes the columns as they
were — and the read-back is positional, so matching it against a changed
column set would hand each column its predecessor's width. So `Resolve`
opens a settle window whenever it bumps the epoch: those reports set the
baseline rather than being read as a gesture. A report whose length does not
match the column list is dropped outright, for the same reason. The practical
consequence is that a drag which begins and ends within ~two frames of a new
result landing is not captured.

**Pass the whole column list, in the order the columns are emitted.** Both
of the above depend on it — including the fixed leading column a table never
lets anyone resize. Omitting it shifts every later column onto the wrong
identity, and the length check is what turns that from a silent mis-capture
into a dropped report.

**The column tier crosses tables, so two views of one column collide.** It
matches on `(name, type)` alone — that is what lets a recurring field keep
its width in a differently-shaped result. If your app renders the same column
two ways, though, a width fitted to one is wrong in the other and the tier
will carry it over anyway. `Type` is yours to choose where the data has no
single render type, so give each view its own tag; play appends `;view=row`
or `;view=attr` to the Arrow type for its two Table granularities. Append it
rather than replacing the type, so a real type change still invalidates the
override — and expect a tag you add later to orphan whatever that view had
already stored.

**Growth is captured like a drag.** egui_table grows a column to fit the
widest *visible* cell and stores the result. The resolver cannot tell that
apart from a drag, so scrolling wider content into view can widen a column and
persist it. This matches the "fit it, keep it" stance the ADR takes for
double-click autofit, but it does mean widths ratchet upward. The growth that
is *not* captured is whatever the crate settles on right after a re-seed,
which the settle window above absorbs.

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

**The gesture** is a header context menu, via `c.ContextMenu()`:

```go
et.BeginHeaders(0, colIdx)
c.ContextMenu().Render(
    func() { // the menu
        if c.Button(ids.PrepareStr("clearcol"), clearAtoms).SendResp().HasPrimaryClicked() {
            _ = res.Clear(tableTag, cols[colIdx])
        }
        if c.Button(ids.PrepareStr("clearall"), resetAtoms).SendResp().HasPrimaryClicked() {
            _ = res.ClearAll(tableTag, cols)
        }
    },
    func() { // the header cell itself
        c.LabelAtoms(headerAtoms).Send()
    },
)
et.EndHeaders()
```

`ContextMenu` does not steal clicks from what it wraps — the overlay it
registers senses hover only, and the secondary click comes from the pointer
— so a sortable header keeps its sort click. Menu items are ordinary widgets
and need stable ids: the popup body renders only while open, so an id drawn
from a per-frame counter will drift.

## Reaching a store

The store arrives as an optional frame-context capability, the shape
[ADR-0155](../adr/0155-app-embed-seam.md) §SD1 settled for host-held
collaborators. Acquire it once, on a frame:

```go
if h, ok := ctx.(colwidth.HostI); ok {
    if st := h.ColumnWidthStore(); st != nil {
        res, _ = colwidth.New(st, colwidth.Opts{AppId: ctx.AppId(), MinPoints: 24, MaxPoints: 1200})
        _ = res.Load()
    }
}
```

Absence is not an error — it means the host has no facts store, so widths
come from your defaults and nothing persists. Every affordance still works.

That silence cuts both ways, so if you *re-host* an app that already resolves
its own widths, hand it your frame context rather than calling its render pass
— an app that never sees a context is indistinguishable from one whose host
has no store, and the symptom is simply that nothing is ever saved.

Construct with `ctx.AppId()`, never an identity you compose: ADR-0155 §SD3
makes it the keying identity for embedded and windowed instances alike, so a
column dragged in one follows the content to the other. A composed identity
would fork the rows per embedder.

## See also

- [ADR-0151](../adr/0151-table-column-width-overrides.md) — the decision, the
  tier model, and the wire design.
- [ADR-0148](../adr/0148-app-workingsets.md) — the data-centricity invariant
  the storage choice follows.
- [`egui2/colwidth`](../../public/thestack/imzero2/egui2/colwidth) — the
  resolver; its package doc covers the apply/capture state machine.
- [ADR-0155](../adr/0155-app-embed-seam.md) — §SD1's optional-capability
  shape, which is how the store reaches an app, and §SD3 on why the keying
  identity is the mount context's.
