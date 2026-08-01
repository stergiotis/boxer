---
type: reference
audience: end-user
status: draft
title: Go modules
icon: "📚"
endpoint: introspection
tabs: [table, network, detail]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Go modules

The third-party surface, rolled up by owning module: how much of your code
leans on each dependency, whether you asked for it or inherited it, and what a
change to it would reach.

**Click a module** in the table — or a node in the graph — to focus it. The
graph then draws the **witness path**: the shortest import chain from the
focused module's most-connected first-party importer down to the module,
which is the concrete answer to *why do we depend on this at all*.

**The columns that are not obvious.**

- **direct** — some first-party package imports a package of this module.
  Derived structurally from the graph, not parsed from `go.mod`, so a module
  that is `require`d but only reached through another dependency reads `0`.
- **fan_in** — how many first-party packages import it directly.
- **blast** — how many first-party packages a change to it could reach,
  following imports backwards. This is the number that says whether an
  upgrade is a morning or a week.
- **importers / reached** — the actual package lists behind `fan_in` and
  `blast`, capped for display. Click a row and read them in the Detail tab.

```sql
SET param_min_packages = 1;
SET param_blast_depth = 12;
SET param_witness_depth = 6;
SET param_list_cap = 40;

WITH RECURSIVE
  fp AS (SELECT import_path FROM keelson('go_packages') WHERE class = 'internal'),
  ext AS (SELECT import_path, module_path FROM keelson('go_packages') WHERE class = 'external'),
  -- Direct first-party → module edges: the seed for everything below.
  direct AS (
    SELECT e.module_path AS module, i.src_path AS importer
    FROM keelson('go_imports') AS i
    INNER JOIN ext AS e ON i.dst_path = e.import_path
    WHERE i.src_path IN (SELECT import_path FROM fp)
  ),
  -- Reverse reachability inside first-party code, seeded on the direct
  -- importers. Depth-bounded so the walk terminates on a pathological graph;
  -- measured on this repository the answer stops changing at depth 8.
  up AS (
    SELECT module, importer AS path, 0 AS depth FROM direct
    UNION ALL
    SELECT u.module, i.src_path, u.depth + 1
    FROM keelson('go_imports') AS i
    INNER JOIN up AS u ON i.dst_path = u.path
    WHERE i.src_path IN (SELECT import_path FROM fp) AND u.depth < {blast_depth:UInt8}
  ),
  blast AS (
    SELECT module, uniqExact(path) AS n, arraySlice(groupUniqArray(path), 1, {list_cap:UInt16}) AS paths
    FROM up
    GROUP BY module
  ),
  fanin AS (
    SELECT module, uniqExact(importer) AS n,
           arraySlice(groupUniqArray(importer), 1, {list_cap:UInt16}) AS paths
    FROM direct
    GROUP BY module
  ),
  mods AS (
    SELECT e.module_path AS key,
           count() AS packages,
           if(f.n > 0, 1, 0) AS direct,
           f.n AS fan_in,
           b.n AS blast,
           f.paths AS importers,
           b.paths AS reached
    FROM ext AS e
    LEFT JOIN fanin AS f ON e.module_path = f.module
    LEFT JOIN blast AS b ON e.module_path = b.module
    GROUP BY key, direct, fan_in, blast, importers, reached
    HAVING packages >= {min_packages:UInt16}
    ORDER BY blast DESC, fan_in DESC, key
  ),
  focus AS (
    SELECT if({selection_key:String} != '',
              {selection_key:String},
              (SELECT key FROM mods ORDER BY blast DESC, fan_in DESC, key LIMIT 1)) AS module
  ),
  -- The witness walk starts at the focused module's best-connected direct
  -- importer and follows imports forward until it lands in the module.
  origin AS (
    SELECT d.importer AS path
    FROM direct AS d
    INNER JOIN focus AS f ON d.module = f.module
    INNER JOIN keelson('go_packages') AS p ON d.importer = p.import_path
    ORDER BY p.num_imported_by DESC, d.importer
    LIMIT 1
  ),
  chain AS (
    SELECT path AS id, [path] AS trail, 0 AS depth FROM origin
    UNION ALL
    SELECT i.dst_path, arrayPushBack(c.trail, i.dst_path), c.depth + 1
    FROM keelson('go_imports') AS i
    INNER JOIN chain AS c ON i.src_path = c.id
    WHERE c.depth < {witness_depth:UInt8} AND NOT has(c.trail, i.dst_path)
  ),
  witness AS (
    SELECT argMin(c.trail, c.depth) AS trail
    FROM chain AS c
    INNER JOIN ext AS e ON c.id = e.import_path
    INNER JOIN focus AS f ON e.module_path = f.module
  ),
  vertices AS (
    SELECT arrayJoin(trail) AS id,
           arrayStringConcat(arraySlice(splitByChar('/', arrayJoin(trail)), -2), '/') AS label,
           'witness' AS `group`,
           '' AS tone
    FROM witness
    UNION ALL
    SELECT f.module, f.module, 'module', 'accent' FROM focus AS f
  ),
  edges AS (
    SELECT trail[number + 1] AS source, trail[number + 2] AS target, '' AS label
    FROM witness
    ARRAY JOIN range(length(trail) - 1) AS number
    UNION ALL
    SELECT w.trail[length(w.trail)], f.module, 'in module'
    FROM witness AS w, focus AS f
  )
SELECT * FROM mods
```

## Bounds, stated

`blast` is bounded by `blast_depth` hops of reverse reachability and the
witness chain by `witness_depth` forward hops. On this repository the blast
answer is identical at depth 8, 12 and 24 (140 first-party packages for the
Arrow module), so the default is a safety rail; a deeper graph would need it
raised. `importers` and `reached` are capped at `list_cap` entries — the
counts beside them are exact, the lists are a sample.

A module with no witness path at `witness_depth` draws an empty graph: either
nothing first-party reaches it within the bound, or it is reached only through
another module's packages, which this walk does not cross.
