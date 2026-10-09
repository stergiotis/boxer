---
type: how-to
audience: end-user
status: draft
title: Reading a leeway table you have not seen
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Reading a leeway table you have not seen

A leeway table holds records of many kinds side by side, each row carrying only
the attributes it has, so a census of its tags says which names occur but not
which records exist or what their values look like. Play's panes read a leeway
result directly — its structure from the column names, its record kinds by
clustering, one row in full — so the cheaper order is to let them read first and
write SQL to check what they show. The steps below are that order; each names
the pane and the operation an agent calls for it.

## 1. The structure, from the names

A physical column name carries the structure it was written with — section,
column, role, type, encoding — so the schema is in the catalog before any row
is read. For a table on the endpoint, `describe_table` lists its sections, the
handle to write for each column (`section:column`), and each section's
membership channels: a *verbatim* channel's tags are names written out, a *ref*
channel's are registry ids that `keelson('memberships')` names. For a result,
the Schema pane shows the same structure, read from the result's column names;
`get_schema` reads it. In SQL, `leeway.columns` decodes every column of every
table — the snippet *The schema, decoded* has the queries.

Write handles, never physical names. A handle's spelling folds — `u32Array:lv`
and `u32-array:lv` are one column — and play prints it as the columns spell it.

## 2. A result the panes can read

```sql
SELECT * FROM anchor.facts LIMIT 3000
```

The Table's headers, the Detail card, the Schema pane and the Projection read a
result whose columns are a leeway table's, as stored: `SELECT *`, or a subset of
the columns that keeps their names. An aggregate, an alias or a join is not
leeway-shaped, and those panes say so instead of drawing; `list_panes` reports
it per pane. A census — tags counted with `arrayJoin` and `GROUP BY` — is an
aggregate, so it replaces the result the panes read; run it after the steps
below, or in a second window.

`LIMIT` takes the rows the server reads first, which on a table ordered by time
is one stretch of it. A spread across the table samples by key:

```sql
SELECT * FROM anchor.facts WHERE cityHash64(`id:id`) % 10 = 0 LIMIT 3000
```

## 3. One row in full

The Detail card reads the selected row as attributes: each named by its first
membership or its plain column, with its section, its values, its further
memberships as labels, and the `LW_GET` handle that reads it in SQL. `get_detail`
returns the same for any row. One row says what a record of its kind carries,
and the handles are the SQL to read those attributes across the table. Binary
values are shown as hex when they are not printable text; the column holds the
bytes.

## 4. The record kinds

**Compute projection** (`compute_projection`) clusters the rows by which
sections and attributes each carries — the *structure* feature set — so rows of
one kind group together whatever their values. Its status line counts the
clusters; `get_projection` gives their sizes. **Why these clusters**
(`explain_clusters`) gives each cluster a rule as SQL over handles with its
precision and recall; *by attributes* reads values and labels too, which is
where a record kind told apart by a label's value shows. The **archetypes**
view (`get_archetypes`) gives each cluster's typical values, its extremes, and the
rows that break its pattern.

A run belongs to the result it was computed over: the next result in the window
drops it. **Publish as dataset** (`publish_projection`) keeps it, as
`keelson('projection')` — one row per entity with its cluster — and
`keelson('projection_rules')` — one row per cluster and reading, with its rule —
so the clusters
can be checked with SQL afterwards:

```sql
SELECT cluster, count() AS n
FROM keelson('projection')
GROUP BY cluster
ORDER BY cluster
```

## 5. The vocabularies, then the SQL

With the kinds known, the tags of each are the attributes its archetype lists,
and the census is a check rather than the first reading. The snippets read
attributes by tag: *Read a tagged attribute by its tag* (`LW_GET`), *Read every
attribute carrying a tag* (`LW_SEL`), and *Membership ids and names* for a ref
channel. A tagged section stores its attributes in parallel arrays, so an
`arrayJoin` over one of them changes the row grain; `LW_GET` and `LW_SEL`
locate an attribute without it.

## For an agent

The steps above as calls, after a grant that covers the endpoint `get_state`
names:

1. `describe_table` for the table, or `get_schema` once a result is in.
2. `set_sql` with a `SELECT *`, `run`, then `list_panes` to see which panes draw it.
3. `get_detail` on a row.
4. `compute_projection`, `get_projection` until done, `explain_clusters`,
   `get_archetypes`; `publish_projection` before the next run if the clusters
   are to be checked with SQL.
5. `list_snippets` and `read_snippet` for the SQL that reads attributes by tag.

The reads are bounded: `sample_rows` returns at most 50 rows and 8 KiB of
cell text, `get_projection` returns points only when asked, and `get_archetypes`
says when its byte bound left exception rows out.
