---
type: reference
audience: end-user
status: draft
title: Uncovered functions
icon: "🔎"
endpoint: introspection
tabs: [table, detail]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Uncovered functions

The work list at function grain: which functions have not run at all, or
started but did not finish, ordered by how much code is at stake. The map
says which boxes are dark; this names the functions inside them.

**The knobs.** `pkg` filters by import path (`ILIKE` pattern, `%` for all —
paste a package from the map to zoom in). `show` picks the population:
`uncovered` (never entered), `partial` (entered, units left), or `all`
(every function, covered ones included, for totals and sorting by anything).

```sql
SET param_pkg = '%';
SET param_show = 'uncovered';

WITH f AS (
  SELECT pkg_path, func, src_file, lit,
         covered_units, total_units, total_stmts,
         round(100 * covered_units / nullIf(total_units, 0), 1) AS pct
  FROM keelson('coverage_funcs')
  WHERE pkg_path ILIKE {pkg:String}
),
sel AS (
  SELECT *
  FROM f
  WHERE multiIf({show:String} = 'uncovered', covered_units = 0,
                {show:String} = 'partial', covered_units > 0 AND covered_units < total_units,
                true)
)
SELECT pkg_path, func, src_file, total_stmts, total_units, covered_units, pct, lit
FROM sel
ORDER BY total_stmts DESC, pkg_path, func
LIMIT 500
```

## Reading it honestly

- **"Uncovered" means uncovered in this session** — an error path no session
  exercises and a feature simply not driven yet look identical here. The
  number is an upper bound on the gap, not a defect count.
- **Function literals (`lit = true`) are closures**; their generated names
  (`Foo.func1`) locate poorly. The `src_file` column is the better pointer
  for them.
- **The 500-row cap keeps the table honest to render**; it drops the tail of
  the sort order, never the head — the biggest uncovered functions are
  always on the first page. Narrow `pkg` to see a specific tail.
- Generated code is instrumented like everything else, and big generated
  files dominate any statement-ordered list. Filtering it out needs a
  convention this table does not have; read `src_file` before reading a row
  as neglect.
