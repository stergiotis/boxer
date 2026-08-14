---
type: reference
audience: end-user
status: draft
title: Competence browser
icon: "🔍"
endpoint: introspection
tabs: [table, detail, network]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Competence browser

The corpus as a list, one competence's note in full, and its immediate family as
a graph. This is the reading surface: the vault is the editing one, and
`vault_path` on every row says which file to open.

**Click to walk.** Clicking a table row, or a node in the graph, publishes that
competence as `selection_key`, which the graph re-reads: parents above,
children below, hop by hop. Typing a slug into the `selection_key` field does
the same. Empty focuses the root of the largest catalog.

**The whole note is a column.** `body` carries the competence's markdown
reassembled from its sections, declared as `text/markdown` in the column name,
so the Detail tab renders it as prose rather than as a wall of escaped text. It
is the source verbatim — the corpus keeps prose as prose rather than shredding
it into an AST — so what you read here is what is in the file.

**The knobs.** `filter` narrows the table by substring over slug, name and
synopsis. `level` keeps one tier (1 macro ▸ 4 building block) or all of them at
`0`. `catalog` keeps one catalog, empty keeps all. `tag` keeps the competences
carrying one triage tag — the `tags:` frontmatter a reviewer writes — and is
exact rather than a substring, because a tag is a name and `owner` should not
also match `needs-owner`.

`maturity` and `pain` render as `—` when nothing has been assessed, which is not
the same as a zero: a zero is the judgement "none".

```sql
SET param_filter = '';
SET param_level = 0;
SET param_catalog = '';
SET param_tag = '';

WITH
  bodies AS (
    SELECT slug,
           arrayStringConcat(
             arrayMap(x -> concat('# ', x.2, '\n\n', x.3),
                      arraySort(x -> x.1, groupArray((ordinal, heading, `text@text/markdown`)))),
             '\n\n') AS body
    FROM keelson('competencesection')
    GROUP BY slug
  ),
  comps AS (
    SELECT c.slug AS slug,
           c.name AS name,
           c.level AS level,
           c.domain AS domain,
           c.catalog AS catalog,
           if(c.maturity = 255, '—', toString(c.maturity)) AS maturity,
           if(c.pain = 255, '—', toString(c.pain)) AS pain,
           c.section_count AS sections,
           c.synopsis AS synopsis,
           c.owner AS owner,
           c.tags AS tags,
           c.vault_path AS vault_path,
           b.body AS `body@text/markdown`
    FROM keelson('competence') AS c
    LEFT JOIN bodies AS b ON c.slug = b.slug
    WHERE ({filter:String} = ''
             OR position(c.slug, {filter:String}) > 0
             OR position(lower(c.name), lower({filter:String})) > 0
             OR position(lower(c.synopsis), lower({filter:String})) > 0)
      AND ({level:UInt8} = 0 OR c.level = {level:UInt8})
      AND ({catalog:String} = '' OR c.catalog = {catalog:String})
      AND ({tag:String} = '' OR has(c.tags, {tag:String}))
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
  parents AS (
    SELECT r.source_slug AS child, r.target AS parent
    FROM keelson('competencerelation') AS r
    WHERE r.kind = 'parent' AND r.resolution IN ('direct', 'dirref')
  ),
  near AS (
    SELECT id FROM focus
    UNION ALL SELECT parent FROM parents INNER JOIN focus ON parents.child = focus.id
    UNION ALL SELECT child FROM parents INNER JOIN focus ON parents.parent = focus.id
    -- The other parents of the focused competence's children. Strictly this is
    -- a second hop, and it is here because without it a shared building block
    -- is drawn hanging off the focus alone — which is precisely the thing
    -- multi-parenting exists to say, silently unsaid.
    UNION ALL
    SELECT co.parent
    FROM parents AS co
    INNER JOIN parents AS mine ON co.child = mine.child
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
    FROM parents AS p
    INNER JOIN (SELECT DISTINCT id FROM near) AS a ON p.parent = a.id
    INNER JOIN (SELECT DISTINCT id FROM near) AS b ON p.child = b.id
  )
SELECT * FROM comps
```

## Reading it honestly

- The graph draws **one hop** — the focused competence's parents and children,
  not its subtree. Click through to walk further; the Competence map draws the
  whole shape at once.
- One exception to the hop, deliberate: a child with **other parents** brings
  them in too. A building block serving two competences has two edges, and
  drawing only the one that reaches the focus would make a shared thing look
  like ours alone. So a graph can show a competence you did not walk to — it is
  there to explain an edge, not as a walk of its own.
- `sections` counts headings, not words. A competence with one long section and
  one with four short ones both look ordinary here; the Competence map sizes by
  bytes instead.
- A competence whose note has no body at all is still listed, with an empty
  `body` — being unwritten is a fact about the corpus worth seeing.
