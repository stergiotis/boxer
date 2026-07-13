---
type: explanation
audience: anyone querying leeway tables, and contributors to the resolver
status: draft
---

> **Status: draft — pre-human-review.** Understanding-oriented; it decides
> nothing. Where this page and an ADR disagree, the ADR is the record — here
> that is [ADR-0116](../adr/0116-play-leeway-column-handle-resolution.md), which
> also carries the rejected options and their kill-reasons.

# Leeway column names

A leeway table stores each attribute under a long, technical *physical* column
name like `tv:symbol:value:val:s:m:0:24:0::data`. This page explains why those
names are shaped that way, and how the playground lets you query them with short
**handles** (`` `symbol:value` ``) instead — including what a handle is, what it
is not, and how one becomes a physical name before a query runs.

## Why the physical names look the way they do

Leeway maps a logical, semi-structured schema onto a flat columnar layout for
ClickHouse. A key design choice is that the **physical column name carries the
whole authored structure** — section, column, role, type, encoding and use
aspects, co-section and streaming groups. Nothing else has to be consulted to
reconstruct the schema: given only the list of column names, the mapping is
fully recoverable (this is what `DiscoverTableFromColumnNames` does, and what
the playground's Detail and Schema tabs rely on).

That self-describing property is worth a lot — it means a leeway table needs no
side-catalog to be understood — but it makes the names long and, to a human,
unmemorable. So the name is precise and stable, yet nobody wants to type it.

## The anatomy of a physical name

Two prefixes matter. A **tagged** (payload) column begins `tv:`:

```
tv : symbol : value : val : s : … (aspect and config components) … : data
     ^section  ^column ^role ^type
```

- **section** groups values of a kind (`symbol`, `geoPoint`, `timeRange`);
- **column** is the attribute within it (`value`, `pointLat`, `beginIncl`);
- **role** distinguishes a value column (`val`) from the membership machinery
  that rides alongside it (refs, cardinalities, lengths — named after their
  role, e.g. `tv:symbol:lr:lr:…`);
- the rest encodes the canonical type and the aspect/config bitmasks.

A **plain/backbone** column begins with an item-type prefix instead —
`id:id:u64:2k:0:0:`, `id:naturalKey:y:g:0:0:` — for the entity id, natural key,
timestamp, lifecycle, transaction, and opaque columns.

One consequence to keep in mind: a section groups values by *kind*, and a
membership discriminator (which logical field a value belongs to) is stored as
**row data**, not in the column name. So the descriptive name of a
membership-packed field is not recoverable from the name alone — see *What
handles do not do*.

## Handles: the short form

The playground resolves a backtick-quoted handle to the physical name before the
query ships. The rule is **colon-always**: a colon is the sole marker of a
handle, and a bare identifier is always ordinary SQL.

- `` `section:column` `` — one column. Sections are the tagged sections above,
  plus six plain sections derived from the backbone prefix: `id`, `routing`,
  `timestamp`, `lifecycle`, `transaction`, `opaque`. So `` `symbol:value` ``,
  `` `geoPoint:pointLat` ``, `` `id:id` `` all work. Any column resolves this
  way — a value column *or* a support column — so a specific handle never
  mis-reports "no such column".
- `` `section:*` `` — all of a section's **value** columns (the data, not the
  machinery). It expands wherever the identifier sits: a comma-separated list in
  the projection, and a co-positional unnest in `ARRAY JOIN`.

Both sides fold to a style-independent form, so `geoPoint:pointLat`,
`geo_point:point-lat`, and `geo-point:pointLat` are one handle. Both must be
quoted, because a bare `geoPoint:pointLat` cannot parse — in the SQL grammar a
lone colon is only the ternary `cond ? a : b`.

A physical name pasted verbatim still works: it has many colons, so it is not
mistaken for a handle and passes through untouched.

## How a handle becomes a physical name

Resolution is a client-side nanopass pass that runs on the SQL just before it
ships (the `StagePreExecute` seam, ADR-0108) — never in the database. It is
scope-aware: it knows each `SELECT`'s tables, and resolves a handle against the
table it belongs to. For the schema it needs, it asks the live endpoint's
`system.columns` for that table's physical names (lazy, cached per session) and
reconstructs the sections and columns the same way the result tabs already do.

Because it substitutes an *identifier* — not a `COLUMNS('…')` matcher — it works
in every clause, `WHERE` and `GROUP BY` and `ARRAY JOIN` as much as the
projection. It is one-directional: the SQL sent to the server carries physical
names; nothing renames the output.

## Reading results: labels

The reverse mapping runs on results. Each result column's physical name is shown
in the Table header as its handle form — `symbol:value`, `geoPoint:pointLat`,
`id:id` — with the physical name on hover. So a header reads exactly as you
would type it, and a raw `SELECT *` (dominated by support columns) reads
friendly throughout rather than as a wall of `tv:…`.

## Catching mistakes early: Diagnostics

Because a colon marks intent unambiguously, a handle that names no known section,
or a known section's non-existent column, is a confident mistake. The
**Diagnostics** tab lists these — with candidate suggestions for a wrong column
— computed client-side, *before* you Run, so a typo like `` `geoPoint:lat` ``
(→ "did you mean: pointLat, pointLng, h3?") never needs a round-trip to surface.
A bare identifier is never flagged; it is ordinary SQL.

## What handles do not do

Handles reach what the physical name spells out: sections and columns. They do
**not** address a membership-packed field by its descriptive name (e.g.
`droneStatus`, sharing the `symbol` column with other symbol fields), because
that identity is row data, not part of the name. Distinguishing those is a
data-level predicate, out of scope for name resolution.

## Reading list

- The playground's *Example queries* how-to (Snippets tab) — the same queries in
  handle form.
- [ADR-0116](../adr/0116-play-leeway-column-handle-resolution.md) — the decision
  record for the resolver, labels, and diagnostics, with the alternatives that
  were rejected.
- The `leeway-beginner` and `leeway-advanced` skills — the backbone/payload
  model and the physical encoding this page summarises.
