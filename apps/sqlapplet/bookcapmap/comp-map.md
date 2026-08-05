---
type: reference
audience: end-user
status: draft
title: Competence map
icon: "🗺"
endpoint: introspection
tabs: [treemap, table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Competence map

The whole hierarchy at once, as nested rectangles: each competence's **area** is
how much has been written about it, each colour a branch of the tree. It is the
picture a competence catalog is usually drawn as, and the one shape the browser
cannot give you — the browser shows one competence's family, this shows the
proportions of all of them.

The Treemap tab reads the *nodes* contract — one row per competence, carrying
its own id, its parent, and its own value — which is what makes an interior
competence's own prose visible. A macro competence that has been written about
at length and one that is a bare heading over twelve children look different
here: the first gets a rectangle of its own inside its box, the second does not.

**Drilling.** Click a container's header strip to descend into it; the
breadcrumb walks back out. The panel's own `show` control (`drill` ▸ one
frontier at a time, `full` ▸ every level at once) decides how much of the tree
is drawn before you click.

**The knobs.** `size_by` picks the area: `bytes` is the competence's own prose,
`count` gives every competence the same area so the picture becomes shape rather
than volume. `color_by` picks the colour: `branch` is the level-2 ancestor —
which part of the toolbelt this belongs to, and the reading that makes the map
worth colouring at all — with `domain`, `catalog` and `level` as alternatives.

```sql
SET param_size_by = 'bytes';
SET param_color_by = 'branch';

WITH RECURSIVE
  parents AS (
    SELECT source_slug AS child, min(target) AS parent
    FROM keelson('competencerelation')
    WHERE kind = 'parent' AND resolution IN ('direct', 'dirref')
    GROUP BY source_slug
  ),
  up AS (
    SELECT slug AS node, slug AS anc, 0 AS d FROM keelson('competence')
    UNION ALL
    SELECT u.node, p.parent, u.d + 1
    FROM up AS u
    INNER JOIN parents AS p ON u.anc = p.child
    WHERE u.d < 8
  ),
  meso AS (
    SELECT u.node AS node, argMin(u.anc, u.d) AS b
    FROM up AS u
    INNER JOIN keelson('competence') AS a ON u.anc = a.slug
    WHERE a.level = 2
    GROUP BY u.node
  ),
  topmost AS (
    SELECT node, argMax(anc, d) AS b FROM up GROUP BY node
  ),
  prose AS (
    SELECT slug, sum(bytes) AS bytes FROM keelson('competencesection') GROUP BY slug
  ),
  nodes AS (
    SELECT c.slug AS id,
           p.parent AS parent,
           -- abbrev first: a cell is a rectangle, and "AlgArch" fits where
           -- "Algebraic Architecture — Categorical Patches & Merges" is three
           -- truncated words. The full name rides along as its own column for
           -- the Table tab.
           multiIf(c.abbrev != '', c.abbrev, c.name != '', c.name, c.slug) AS label,
           if({size_by:String} = 'count', 1, toFloat64(s.bytes)) AS value,
           if({size_by:String} = 'count', '', 'B') AS unit,
           multiIf({color_by:String} = 'domain', c.domain,
                   {color_by:String} = 'catalog', c.catalog,
                   {color_by:String} = 'level', concat('L', toString(c.level)),
                   if(m.b != '', m.b, t.b)) AS color,
           c.name AS name,
           c.level AS level,
           s.bytes AS own_bytes,
           c.section_count AS sections
    FROM keelson('competence') AS c
    LEFT JOIN parents AS p ON c.slug = p.child
    LEFT JOIN prose AS s ON c.slug = s.slug
    LEFT JOIN meso AS m ON c.slug = m.node
    LEFT JOIN topmost AS t ON c.slug = t.node
  )
SELECT id, parent, label, value, unit, color, name, level, own_bytes, sections
FROM nodes
ORDER BY level, id
```

## Reading it honestly

- **Area is own prose, not subtree prose.** The treemap adds the children up
  for you; `value` is what the competence itself says. That is the contract's
  point, and it is why a macro competence with a long Vision and Scope shows a
  rectangle of its own rather than having its words silently redistributed into
  its children.
- **A competence with several parents is drawn under one of them** — the
  alphabetically first. A treemap is a partition of area, so a node cannot be in
  two boxes at once; the browser's graph is where multi-parenting is visible
  without being flattened. Level 4 is where this bites.
- **A competence nothing has been written about has zero area** under
  `size_by = 'bytes'` and vanishes. Switch to `count` to see it — an unwritten
  competence is a finding, and it should not be possible to lose it by choosing
  a size channel.
- **Cells are labelled by `abbrev`**, which is the vault's own short form, and
  fall back to the full name only when there is none. The Table tab carries
  both, so a cell you cannot place is one click from being named.
- **There are usually more branches than colours.** The qualitative palette has
  seven; this repository's own catalog has thirteen level-2 competences, so six
  of them reuse a colour. The panel reports the overlap in its status line
  rather than hiding it — read colour as "these two boxes are the same branch"
  only when they are adjacent under one parent, and read the breadcrumb
  otherwise. `color_by = 'level'` has four values and never collides, at the
  cost of saying something the nesting already showed.
- **`color_by = 'branch'` falls back** to a competence's topmost ancestor when
  no level-2 ancestor exists, so a shallow catalog still colours. The ancestor
  walk is depth-bounded at 8; a cycle in `parent_ids` would otherwise not
  terminate, and a hierarchy deeper than that is not one.
- Colour is inherited upward by the panel: a container the query gave no colour
  takes its children's, and only when they agree. The status line reports how
  many cells were coloured that way and how many were left mixed.
