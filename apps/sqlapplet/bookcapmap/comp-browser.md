---
type: reference
audience: end-user
status: draft
title: Competence browser
icon: "🔍"
endpoint: introspection
tabs: ["treemap:nodes", "detail@side", "table@bottom", "network@bottom"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Competence browser

The whole hierarchy as nested rectangles, one competence's note in full beside
it, and the corpus as a list underneath. This is the reading surface: the vault
is the editing one, and `vault_path` on every row says which file to open.

**Four panes, three places.** The map fills the body, the focused note's detail
sits beside it, and the list runs along the bottom — the placement the `tabs:`
entries declare with their `@side` and `@bottom` suffixes. The family graph
shares the bottom with the list, a tab away, because it answers a question the
list cannot and the two want the same width. Drag any divider; the layout the
document declares is a preset, not a constraint.

**The map reads its own lane.** `treemap:nodes` binds the Treemap tab to the
`nodes` CTE rather than to the query's result, which is what lets one document
serve a rectangle contract and a human-readable list at the same time. The
nodes arm — one row per competence carrying its own id, parent and value — is
the one that makes an interior competence's own prose visible: a macro
competence written about at length and one that is a bare heading over twelve
children look different here, because the first gets a rectangle of its own
inside its box.

**Click to walk.** Clicking a table row, or a node in the graph, publishes that
competence as `selection_key`, which the graph re-reads: parents above,
children below, hop by hop. Typing a slug into the `selection_key` field does
the same. Empty focuses the root of the largest catalog.

**The whole note is a column.** `body` carries the competence's markdown
reassembled from its sections, declared as `text/markdown` in the column name,
so the Detail tab renders it as prose rather than as a wall of escaped text. It
is the source verbatim — the corpus keeps prose as prose rather than shredding
it into an AST — so what you read here is what is in the file.

**The knobs, and which pane each one reaches.** `level` shapes the **list**: it
keeps one tier (1 macro ▸ 4 building block) or all of them at `0`, and picks
from a list rather than taking a number. `catalog` reaches the **map** as well,
because a catalog is a whole subtree and removing one leaves the rest a tree,
while a tier filter would cut interior nodes out from under their children and
is therefore not allowed near the partition. It stays a text field, because the
catalogs are whatever the vault holds and a declared list could only ever be
out of date.

**Four knobs, and the reason is the map.** Each prelude-bound knob costs a row
of the pane strip whatever it filters, and the panels that draw into a fixed
box have a floor below which they keep their size and scroll rather than
shrink — so six knobs did not make the treemap smaller, they made it *clipped*,
losing its deepest leaves to carry two controls. The two that went are the two
that filter on nothing here: a substring `filter` over slug, name and synopsis,
which the Table's own column filters already do and which a predicate bar is
the designed answer to, and a `tag` filter over the `tags:` frontmatter, which
no note in the shipped catalog carries. The tag channel itself is intact — the
column is on every row and the overview counts its coverage — so this is a
control removed, not a signal. It is a workaround: the strip stacks one knob
per row because a free-text field asks for the whole line, and a strip that
flowed would carry all six in one.

`size_by` and `color_by` are the map's own. `size_by` picks the area: `words`
counts whitespace-separated tokens in the competence's own prose, `bytes`
counts its markdown source including the syntax, and `count` gives every
competence the same area so the picture becomes shape rather than volume.
`color_by` picks the colour: `branch` is the level-2 ancestor — which part of
the toolbelt this belongs to, and the reading that makes the map worth
colouring at all — with `domain`, `catalog` and `level` as alternatives.

`maturity` and `pain` render as `—` when nothing has been assessed, which is not
the same as a zero: a zero is the judgement "none".

**The map's own controls are the panel's, not this document's.** Its `show` bar
is a depth ladder — `drill` for the frontier's children and one level under
them, `3 deep` and `4 deep` for as many levels below the frontier, `full` for
the whole subtree — and a four-tier competence map is read at `4 deep`, where
every tier is visible and the leaves still carry their labels; `full` on a
catalog of a thousand notes is a mosaic. The colour key here is a row of
swatches, because `color_by` names a category. The panel's other legend — a
gradient bar with a readout of where the values sit, min to max through the
quartiles and the upper tail — appears when a document's `color` is a number,
and this one's is not: a column is a string or a number by its type, not by a
parameter, so a document picks one. Here the branch reading won.

```sql
SET param_level = 0;
SET param_catalog = '';
SET param_size_by = 'words';
SET param_color_by = 'branch';
-- play: enum level 0=All levels,1=Macro,2=Meso,3=Micro,4=Block
-- play: enum size_by words=prose words,bytes=prose bytes,count=one each
-- play: enum color_by branch,domain,catalog,level

WITH RECURSIVE
  sections AS (
    SELECT slug,
           sum(bytes) AS bytes,
           sum(words) AS words,
           arrayStringConcat(
             arrayMap(x -> concat('# ', x.2, '\n\n', x.3),
                      arraySort(x -> x.1, groupArray((ordinal, heading, `text@text/markdown`)))),
             '\n\n') AS body
    FROM keelson('competencesection')
    GROUP BY slug
  ),
  -- Every parent edge, for the graph: a competence with three parents has
  -- three rows, which is the whole point of carrying relations as facts.
  parentedges AS (
    SELECT r.source_slug AS child, r.target AS parent
    FROM keelson('competencerelation') AS r
    WHERE r.kind = 'parent' AND r.resolution IN ('direct', 'dirref')
  ),
  -- One parent per child, for the map: a treemap partitions area, so a node
  -- has to be in exactly one box.
  treeparent AS (
    SELECT child, min(parent) AS parent FROM parentedges GROUP BY child
  ),
  up AS (
    SELECT slug AS node, slug AS anc, 0 AS d FROM keelson('competence')
    UNION ALL
    SELECT u.node, p.parent, u.d + 1
    FROM up AS u
    INNER JOIN treeparent AS p ON u.anc = p.child
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
  visible AS (
    SELECT slug FROM keelson('competence')
    WHERE {catalog:String} = '' OR catalog = {catalog:String}
  ),
  nodes AS (
    SELECT c.slug AS id,
           -- A parent the catalog filter removed is blanked rather than left
           -- dangling: the survivor becomes a root of what is drawn, which is
           -- what it is, instead of pointing at a rectangle that is not there.
           if(p.parent IN (SELECT slug FROM visible), p.parent, '') AS parent,
           -- abbrev first: a cell is a rectangle, and "AlgArch" fits where
           -- "Algebraic Architecture — Categorical Patches & Merges" is three
           -- truncated words. The full name rides along for the Table tab.
           multiIf(c.abbrev != '', c.abbrev, c.name != '', c.name, c.slug) AS label,
           multiIf({size_by:String} = 'count', 1.0,
                   {size_by:String} = 'bytes', toFloat64(s.bytes),
                   toFloat64(s.words)) AS value,
           multiIf({size_by:String} = 'count', '',
                   {size_by:String} = 'bytes', 'B',
                   'w') AS unit,
           multiIf({color_by:String} = 'domain', c.domain,
                   {color_by:String} = 'catalog', c.catalog,
                   {color_by:String} = 'level', concat('L', toString(c.level)),
                   if(m.b != '', m.b, t.b)) AS color,
           c.name AS name,
           c.level AS level,
           s.words AS own_words,
           s.bytes AS own_bytes,
           c.section_count AS sections
    FROM keelson('competence') AS c
    INNER JOIN visible AS v ON c.slug = v.slug
    LEFT JOIN treeparent AS p ON c.slug = p.child
    LEFT JOIN sections AS s ON c.slug = s.slug
    LEFT JOIN meso AS m ON c.slug = m.node
    LEFT JOIN topmost AS t ON c.slug = t.node
  ),
  comps AS (
    SELECT c.slug AS slug,
           c.name AS name,
           c.level AS level,
           multiIf(c.level = 1, 'Macro', c.level = 2, 'Meso',
                   c.level = 3, 'Micro', c.level = 4, 'Block',
                   toString(c.level)) AS tier,
           c.domain AS domain,
           c.catalog AS catalog,
           if(c.maturity = 255, '—', toString(c.maturity)) AS maturity,
           if(c.pain = 255, '—', toString(c.pain)) AS pain,
           s.words AS words,
           s.bytes AS bytes,
           c.section_count AS sections,
           c.synopsis AS synopsis,
           c.owner AS owner,
           c.tags AS tags,
           c.vault_path AS vault_path,
           s.body AS `body@text/markdown`
    FROM keelson('competence') AS c
    LEFT JOIN sections AS s ON c.slug = s.slug
    WHERE ({level:UInt8} = 0 OR c.level = {level:UInt8})
      AND ({catalog:String} = '' OR c.catalog = {catalog:String})
    ORDER BY level, slug
  ),
  focus AS (
    -- Nothing clicked yet: the root of the largest catalog, which is the one
    -- reading a competence map usually starts from. A level-1 competence is a
    -- root by construction.
    SELECT if({selection_key:String} != '',
              {selection_key:String},
              (SELECT slug FROM keelson('competence')
               WHERE level = 1 ORDER BY section_count DESC, slug LIMIT 1)) AS id
  ),
  near AS (
    SELECT id FROM focus
    UNION ALL SELECT parent FROM parentedges INNER JOIN focus ON parentedges.child = focus.id
    UNION ALL SELECT child FROM parentedges INNER JOIN focus ON parentedges.parent = focus.id
    -- The other parents of the focused competence's children. Strictly this is
    -- a second hop, and it is here because without it a shared building block
    -- is drawn hanging off the focus alone — which is precisely the thing
    -- multi-parenting exists to say, silently unsaid.
    UNION ALL
    SELECT co.parent
    FROM parentedges AS co
    INNER JOIN parentedges AS mine ON co.child = mine.child
    INNER JOIN focus ON mine.parent = focus.id
  ),
  vertices AS (
    SELECT c.slug AS id,
           if(c.name != '', c.name, c.slug) AS label,
           c.domain AS `group`,
           if(c.slug = (SELECT id FROM focus), 'accent', '') AS tone,
           if(c.slug = (SELECT id FROM focus), 'box', 'ellipse') AS shape
    FROM keelson('competence') AS c
    INNER JOIN (SELECT DISTINCT id FROM near) AS n ON c.slug = n.id
  ),
  edges AS (
    SELECT p.parent AS source, p.child AS target
    FROM parentedges AS p
    INNER JOIN (SELECT DISTINCT id FROM near) AS a ON p.parent = a.id
    INNER JOIN (SELECT DISTINCT id FROM near) AS b ON p.child = b.id
  )
SELECT * FROM comps
```

## Reading it honestly

- **Area is own prose, not subtree prose.** The treemap adds the children up
  for you; `value` is what the competence itself says. That is the contract's
  point, and it is why a macro competence with a long Vision and Scope shows a
  rectangle of its own rather than having its words silently redistributed into
  its children.
- **A competence with several parents is drawn under one of them** — the
  alphabetically first. A treemap is a partition of area, so a node cannot be in
  two boxes at once; the graph is where multi-parenting stays visible without
  being flattened. Level 4 is where this bites.
- **A competence nothing has been written about has zero area** under `words`
  or `bytes`, and vanishes. Switch to `count` to see it — an unwritten
  competence is a finding, and it should not be possible to lose it by choosing
  a size channel.
- **`words` and `bytes` disagree by more than a constant.** Bytes count the
  markdown source, so a section dense with wikilinks and headings weighs more
  than the same number of words of plain prose. Neither is wrong; `words` is
  closer to "how much was said" and `bytes` to "how much is stored".
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
- **With every catalog shown, the map is a forest**, one root per catalog, and
  the panel draws it as one. Pick a catalog to get a single tree.
- The graph draws **one hop** — the focused competence's parents and children,
  not its subtree. Click through to walk further; the map has the whole shape.
- One exception to the hop, deliberate: a child with **other parents** brings
  them in too. A building block serving two competences has two edges, and
  drawing only the one that reaches the focus would make a shared thing look
  like ours alone. So a graph can show a competence you did not walk to — it is
  there to explain an edge, not as a walk of its own.
- `sections` counts headings, not words. A competence with one long section and
  one with four short ones both look ordinary by that column; `words` is the
  one that separates them.
- A competence whose note has no body at all is still listed, with an empty
  `body` — being unwritten is a fact about the corpus worth seeing.
