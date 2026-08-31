---
type: reference
audience: end-user
status: draft
title: Competence overview
summary: "Count what the competence vault holds and how much is judged"
icon: "🗂"
endpoint: introspection
tabs: [table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Competence overview

What the vault holds, and how much of it has been judged. Read this first when
the other three applets look empty: the `competences` row says whether there is
a corpus at all.

The tables read the vault **live** — markdown on disk, not a database — so these
numbers describe the working tree as it is now, and an edit shows up on the next
Run. Zero competences means the process found no vault: it walks up from the
working directory looking for `doc/competences`, so launching boxer from
elsewhere changes what these applets are about. `BOXER_CAPMAP_VAULT` points at
one kept somewhere else.

**Tags are the triage channel.** A reviewer walking the corpus marks what they
found — `needs-owner`, `merge-candidate` — in the note's `tags:` frontmatter,
and the `tag · …` rows below count each one. They are a different signal from
maturity and pain: a score says how good something is, a tag says what somebody
decided to do about it. A corpus nobody has walked has none.

**Assessment coverage is the row worth looking at.** Maturity and pain are
human judgements with no oracle, and an unassessed competence carries the
sentinel `255` rather than a zero — because a zero is a judgement ("none") and
the absence of one is not. If `assessed` reads 0, the hierarchy has been written
down and the scoring has not, which is the state this repository's own catalog
shipped in.

```sql
SET param_top = 12;

WITH
  comps AS (SELECT * FROM keelson('competence')),
  secs AS (SELECT * FROM keelson('competencesection')),
  rels AS (SELECT * FROM keelson('competencerelation')),
  doms AS (
    SELECT domain, count() AS n
    FROM comps
    GROUP BY domain
    ORDER BY n DESC, domain
    LIMIT {top:UInt8}
  ),
  facts AS (
    SELECT 0 AS ord, 'corpus' AS section, 'competences' AS metric, toString(count()) AS value FROM comps
    UNION ALL SELECT 1, 'corpus', 'catalogs', toString(uniqExact(catalog)) FROM comps
    UNION ALL SELECT 2, 'corpus', 'domains', toString(uniqExact(domain)) FROM comps
    UNION ALL SELECT 3, 'corpus', 'sections', toString(count()) FROM secs
    UNION ALL SELECT 4, 'corpus', 'prose (KiB)', toString(round(sum(bytes) / 1024, 1)) FROM secs
    UNION ALL SELECT 5, 'corpus', 'relations', toString(count()) FROM rels
    UNION ALL SELECT 10, 'hierarchy', concat('level ', toString(level)), toString(count())
      FROM comps GROUP BY level
    UNION ALL SELECT 11, 'hierarchy', 'roots (no parent)', toString(count())
      FROM comps WHERE slug NOT IN (
        SELECT source_slug FROM rels WHERE kind = 'parent' AND resolution IN ('direct', 'dirref'))
    UNION ALL SELECT 20, 'assessment', 'assessed (maturity)', toString(countIf(maturity != 255)) FROM comps
    UNION ALL SELECT 21, 'assessment', 'assessed (pain)', toString(countIf(pain != 255)) FROM comps
    UNION ALL SELECT 22, 'assessment', 'with an owner', toString(countIf(owner != '')) FROM comps
    UNION ALL SELECT 23, 'assessment', 'with a lifecycle record', toString(countIf(length(lifecycle_phases) > 0)) FROM comps
    UNION ALL SELECT 24, 'assessment', 'tagged', toString(countIf(length(tags) > 0)) FROM comps
    UNION ALL SELECT 25, 'assessment', concat('tag · ', t), toString(count())
      FROM comps ARRAY JOIN tags AS t GROUP BY t
    UNION ALL SELECT 30, 'links', concat(resolution, ' · ', kind), toString(count())
      FROM rels GROUP BY resolution, kind
    UNION ALL SELECT 40, 'domains', concat('domain · ', domain), toString(n) FROM doms
  )
SELECT section, metric, value
FROM facts
ORDER BY ord, metric
```

## The other three

- **Competence browser** — one competence at a time: its metadata, its whole
  note as markdown, and its parents and children as a graph.
- **Competence map** — the hierarchy as a treemap, sized by how much has been
  written about each part and coloured by domain.
- **Competence links** — every link the corpus declares, and which of them
  point at nothing.

## The tables behind them

| Table | One row per |
|---|---|
| `keelson('competence')` | competence: `slug`, `name`, `abbrev`, `synopsis`, `domain`, `catalog`, `owner`, `level`, `maturity`, `pain`, `tags`, `section_count`, `vault_path`, `fact_id`, and the three `lifecycle_*` arrays |
| `keelson('competencesection')` | body section: `slug`, `ordinal`, `heading`, `bytes`, `` `text@text/markdown` `` |
| `keelson('competencerelation')` | link: `source_slug`, `target`, `kind`, `resolution`, `section`, `ncd`, `source_fact_id`, `target_fact_id` |

`fact_id` is the id `boxer capmap load` writes into `boxer.facts` for the same
competence, so the live view here and the persisted history there join without
either side knowing how the other derives it.
