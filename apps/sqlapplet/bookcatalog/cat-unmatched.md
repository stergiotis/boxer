---
type: reference
audience: end-user
status: draft
title: Unmatched opaque tables
icon: "🕳"
endpoint: default
tabs: [table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Unmatched opaque tables

The tables this instance holds that are neither leeway nor shaped like anything
a play panel knows how to draw. It is the catalog's to-do list, and it is meant
to shrink from either end.

**Two ways to act on a row.** Either the table really is a shape the panels
should know — in which case the fix is a battery in
`public/gov/datacatalog/panelshapes`, three lines and a refresh, and the shape
vocabulary is data precisely so that this is cheap. Or the table is fine and
simply is not a picture, which most tables are; a long list here is not a
backlog.

**`normalized_schema` is what a shape is matched against**: the columns in
position order, `;name:type;`, with `LowCardinality` stripped and `Nullable(T)`
written `T?`. Reading a row's string next to
[`keelson('panel_shapes')`](#the-shape-vocabulary) is how you work out which
pattern would have to change.

**Views and dictionaries land here too.** The catalog discovers whatever
`system.tables` reports and takes `system.columns` at its word; a View's columns
are its result columns, which is the right thing to match a panel contract
against.

```sql
SELECT
  c.database          AS database,
  c.name              AS name,
  c.engine            AS engine,
  c.n_columns         AS n_columns,
  c.normalized_schema AS normalized_schema,
  c.run_id            AS run_id,
  c.discovered_at     AS discovered_at
FROM boxer.tables_catalog AS c
WHERE c.kind = 'opaque'
  AND (c.database, c.name) NOT IN (
    SELECT database, name FROM boxer.tables_opaque_shapes
  )
ORDER BY c.n_columns DESC, c.database, c.name
```

## The shape vocabulary

The batteries themselves are served by any process with the introspection plane
up, one row per (shape, pattern) — a shape is an AND of patterns because RE2 has
no lookahead, so "a `lane` column and a `title` column" cannot be one regex:

```text
SELECT shape, ordinal, pattern, note FROM keelson('panel_shapes') ORDER BY shape, ordinal
```

`boxer datacatalog shapes` prints the same list from the CLI, and
`boxer.tables_opaque_shapes` is the join of that vocabulary against this
instance already materialized.
