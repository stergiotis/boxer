---
type: reference
audience: end-user
status: draft
title: Go dependency overview
summary: "Report what the dependency graph collected, and its size"
icon: "📦"
endpoint: introspection
tabs: [table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Go dependency overview

What was collected, and how big it is. Read this first when the other three
applets look empty: the `status` row says whether the graph is there yet.

The collection runs once per process, the first time any `keelson('go_*')`
table is queried, and is cached from then on — so these numbers describe the
working tree as it was when this process first looked, not as it is now. A
`collecting` status means the toolchain is still running; run the query again
in a moment. A `failed` status carries the reason in `error`, and the other
three applets will be empty until the process is restarted.

`root dir` is the module the tables describe. It follows `BOXER_GODEP_ROOT`
when set, and otherwise the nearest `go.mod` above the working directory the
process was started in — so launching boxer from elsewhere changes what these
applets are about. `build tags` matters as much: this repository's tags are
load-bearing, and a collection without them silently omits every tag-gated
package.

```sql
SET param_top = 10;

WITH
  col AS (SELECT * FROM keelson('go_collection')),
  cls AS (
    SELECT class, count() AS n
    FROM keelson('go_packages')
    GROUP BY class
  ),
  mods AS (
    SELECT module_path, count() AS n
    FROM keelson('go_packages')
    WHERE class = 'external'
    GROUP BY module_path
    ORDER BY n DESC, module_path
    LIMIT {top:UInt8}
  ),
  facts AS (
    SELECT 0 AS ord, 'collection' AS section, 'status' AS metric, status AS value FROM col
    UNION ALL SELECT 1, 'collection', 'error', error FROM col
    UNION ALL SELECT 2, 'collection', 'root module', root_module FROM col
    UNION ALL SELECT 3, 'collection', 'root dir', root_dir FROM col
    UNION ALL SELECT 4, 'collection', 'go version', go_version FROM col
    UNION ALL SELECT 5, 'collection', 'build tags', arrayStringConcat(build_tags, ',') FROM col
    UNION ALL SELECT 6, 'collection', 'roots', arrayStringConcat(roots, ' ') FROM col
    UNION ALL SELECT 7, 'collection', 'scope', scope FROM col
    UNION ALL SELECT 8, 'collection', 'collected at', collected_at FROM col
    UNION ALL SELECT 9, 'collection', 'took (ms)', toString(duration_ms) FROM col
    UNION ALL SELECT 10, 'graph', 'packages', toString(num_packages) FROM col
    UNION ALL SELECT 11, 'graph', 'import edges', toString(num_edges) FROM col
    UNION ALL SELECT 12, 'graph', concat('class · ', class), toString(n) FROM cls
    UNION ALL SELECT 13, 'modules', concat('module · ', module_path), toString(n) FROM mods
  )
SELECT section, metric, value
FROM facts
ORDER BY ord, metric
```

## The other three

- **Go packages** — every package in the closure, with the focused package's
  import neighbourhood as a graph.
- **Go architecture** — packages folded into groups: how the subsystems
  relate, which apps depend on each other, where the cycles are.
- **Go modules** — the third-party surface: fan-in, direct versus transitive,
  and what a change to a module would reach.

## The tables behind them

| Table | One row per |
|---|---|
| `keelson('go_packages')` | package: `id`, `import_path`, `name`, `dir`, `module_path`, `class`, `num_go_files`, `num_imports`, `num_imported_by` |
| `keelson('go_imports')` | import edge: `src_id`, `dst_id`, `src_path`, `dst_path` |
| `keelson('go_collection')` | the collection itself (always exactly one row) |
| `keelson('go_package_props')` | surveyed package: the ADR-0080 WASM verdicts and `kind` |

They join with the rest of the corpus — `keelson('adr')`, `keelson('coderef')`,
`keelson('sbom')` — which is the reason these live as tables rather than
inside one app.
