---
type: reference
audience: end-user
status: draft
title: Go packages
icon: "🕸"
endpoint: introspection
tabs: [table, network, detail]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Go packages

Every package in the transitive closure as a table, and the *focused*
package's import neighbourhood as a graph. The closure is on the order of a
thousand packages — far too many to draw — so the table is the scalable
surface and the graph only ever shows one package's local neighbourhood.

**Click to walk.** Clicking a table row, or a node in the graph, publishes
that package as `selection_key`, which the neighbourhood re-reads: the graph
follows your clicks hop by hop. Typing a path into the `selection_key` field
in the parameters strip does the same thing. Empty means "this module's
most-imported package".

**The knobs.** `filter` narrows the table by substring. `depth` (1–3) and
`dir` (`imports` ▸ / `importers` ◂ / `both`) shape the walk. `dir` starts on
`importers` because the default focus is the most-imported package, which is
by definition a leaf in the other direction — the interesting question about
a hub is who leans on it. `hide_std`
drops standard-library packages from it — on by default, and load-bearing:
`fmt` and `errors` are reached by most of the closure, so an importers walk
that includes them stops being a neighbourhood and becomes the whole graph.
`max_nodes` caps what the graph draws, closest-first. `depth` starts at 1 —
one hop is a neighbourhood you can read; two hops on a hub package fills the
cap and becomes a band of unreadable boxes.

The `wasm` column joins the ADR-0080 declarations, which cover only surveyed
first-party packages: `—` means no verdict was declared, not that it fails.

```sql
SET param_filter = '';
SET param_depth = 1;
SET param_dir = 'importers';
SET param_hide_std = 1;
SET param_max_nodes = 120;

WITH RECURSIVE
  pkgs AS (
    SELECT p.import_path AS key,
           p.name AS name,
           p.class AS class,
           p.module_path AS module,
           p.num_go_files AS files,
           p.num_imports AS out_deg,
           p.num_imported_by AS in_deg,
           if(w.import_path = '', '—', concat(toString(w.wasm_compiles), '/3')) AS wasm,
           p.dir AS dir
    FROM keelson('go_packages') AS p
    LEFT JOIN keelson('go_package_props') AS w ON p.import_path = w.import_path
    WHERE {filter:String} = '' OR position(p.import_path, {filter:String}) > 0
    ORDER BY in_deg DESC, key
  ),
  focus AS (
    -- With nothing clicked yet, focus this module's most-imported package.
    -- Deliberately not the most-imported package overall: that is always a
    -- standard-library hub whose own imports `hide_std` then removes, so the
    -- applet would mount on an empty graph.
    SELECT if({selection_key:String} != '',
              {selection_key:String},
              (SELECT key FROM pkgs WHERE class = 'internal' ORDER BY in_deg DESC, key LIMIT 1)) AS id
  ),
  hop AS (
    SELECT i.src_path AS from_id, i.dst_path AS to_id
    FROM keelson('go_imports') AS i
    INNER JOIN keelson('go_packages') AS d ON i.dst_path = d.import_path
    WHERE {dir:String} IN ('imports', 'both')
      AND ({hide_std:UInt8} = 0 OR d.class != 'stdlib')
    UNION ALL
    SELECT i.dst_path, i.src_path
    FROM keelson('go_imports') AS i
    INNER JOIN keelson('go_packages') AS s ON i.src_path = s.import_path
    WHERE {dir:String} IN ('importers', 'both')
      AND ({hide_std:UInt8} = 0 OR s.class != 'stdlib')
  ),
  walk AS (
    SELECT id, 0 AS depth FROM focus
    UNION ALL
    SELECT DISTINCT h.to_id, w.depth + 1
    FROM hop AS h
    INNER JOIN walk AS w ON h.from_id = w.id
    WHERE w.depth < {depth:UInt8}
  ),
  reached AS (
    SELECT id, min(depth) AS depth
    FROM walk
    GROUP BY id
    ORDER BY depth, id
    LIMIT {max_nodes:UInt16}
  ),
  vertices AS (
    SELECT r.id AS id,
           arrayStringConcat(arraySlice(splitByChar('/', r.id), -2), '/') AS label,
           p.class AS `group`,
           if(r.depth = 0, 'accent', '') AS tone,
           if(r.depth = 0, 'box', 'ellipse') AS shape
    FROM reached AS r
    INNER JOIN keelson('go_packages') AS p ON r.id = p.import_path
  ),
  edges AS (
    SELECT i.src_path AS source, i.dst_path AS target
    FROM keelson('go_imports') AS i
    INNER JOIN reached AS a ON i.src_path = a.id
    INNER JOIN reached AS b ON i.dst_path = b.id
  )
SELECT * FROM pkgs
```

## What the numbers mean

- **class** — `stdlib`, `internal` (this module) or `external` (a third-party
  dependency), relative to the collection's root module.
- **out_deg / in_deg** — how many packages this one imports, and how many
  import it. Both are materialised at collection time, so sorting by fan-in
  costs no traversal.
- **files** — `.go` files the build selects under the collected tags.

## Bounds, stated

The neighbourhood walk is depth-capped by `depth` and node-capped by
`max_nodes`; past the cap the closest nodes win and the rest are dropped. The
graph panel caps again at a few hundred vertices and reports it in its status
line. Test-only imports are not collected at all, so a `_test.go`-only
dependency is invisible here.
