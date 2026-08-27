---
type: reference
audience: end-user
status: draft
title: Competence links
icon: "🔗"
endpoint: introspection
tabs: [table, detail]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Competence links

Every link the corpus declares, and what happened when it was looked up. A
broken link is not a separate finding computed by a separate pass — it is a
relation whose `resolution` says `unresolved`, so the corpus lint is this query
and nothing else.

**Read `resolution` before drawing conclusions.** Four states, and only one of
them is a defect:

| `resolution` | Means | A defect? |
|---|---|---|
| `direct` | The target is a competence, found by its slug. | No |
| `dirref` | The target exists, but only as `{slug}/competence.md`. The link resolves here and dangles in Obsidian. | Mechanical: write `[[slug/competence]]` |
| `external` | The text cannot be a competence slug at all — a citation key like `Jouppi-1990` or `GDPR-Art-17`. It names something outside the corpus. | No |
| `unresolved` | A well-formed slug that no competence in this vault carries. | Yes — mostly |

**"Mostly" is the honest part.** A vault commonly keeps reference notes —
standards, technologies — in a sibling tree that is deliberately *not* read as
competences, because their names collide with competence names. A link into one
of those trees is a well-formed slug this corpus does not carry, so it lands in
`unresolved` beside the genuine typos, and nothing in the data separates them.
On the catalog this model was built against, the large majority of unresolved
links pointed at sibling trees and only a small fraction at nothing — so treat
the `unresolved` count as an upper bound on the defects, not as the defect
count.

`cited_by` is the signal that helps. A missing target named by one competence is
usually a typo; one named by a dozen is usually a note that was never written,
or one that lives in a tree this corpus does not read. Sorting by it puts the
systematic gaps at the top and the one-offs at the bottom.

**The knobs.** `show` picks the resolution (`unresolved` by default, `all` for
everything) and `kind` narrows to one relation kind; both are lists, so the four
resolutions above are the offer rather than four words to spell. `section`
narrows body links to the heading they were found under — `Standards` is where a
catalog keeps its bibliography, and it is where most `external` links live.

```sql
SET param_show = 'unresolved';
SET param_kind = '';
SET param_section = '';
-- play: enum show unresolved,dirref,external,direct,all
-- play: enum kind =All kinds,parent,similar,wikilink

WITH
  rels AS (
    SELECT * FROM keelson('competencerelation')
    WHERE ({show:String} = 'all' OR resolution = {show:String})
      AND ({kind:String} = '' OR kind = {kind:String})
      AND ({section:String} = '' OR section = {section:String})
  ),
  fanin AS (
    SELECT target, resolution, uniqExact(source_slug) AS cited_by
    FROM keelson('competencerelation')
    GROUP BY target, resolution
  )
SELECT r.source_slug AS source,
       r.target AS target,
       r.kind AS kind,
       r.resolution AS resolution,
       r.section AS section,
       f.cited_by AS cited_by,
       c.vault_path AS source_file,
       if(r.kind = 'similar', round(r.ncd, 3), NULL) AS ncd
FROM rels AS r
LEFT JOIN fanin AS f ON r.target = f.target AND r.resolution = f.resolution
LEFT JOIN keelson('competence') AS c ON r.source_slug = c.slug
ORDER BY cited_by DESC, target, source
```

## Fixing what it finds

`source_file` is the file to edit — the vault is authoritative, so a fix is a
markdown edit and a re-Run, not a write through this applet. The three shapes
worth acting on:

- **`dirref`** — the target is right and the spelling is not. Rewriting
  `[[thing]]` as `[[thing/competence]]` makes it resolve in Obsidian too.
- **`unresolved` with a high `cited_by`** — many competences agree that
  something should exist. Either it should be written, or it lives in a
  reference tree and these links are fine as they are.
- **`unresolved` with `cited_by = 1`** — a typo, a rename that was not followed
  through, or a competence that was deleted.

`similar` relations carry `ncd`, a normalised compression distance: lower is
more alike. They come from the vault's own `similar:` front matter, so a corpus
that has never been scored for resemblance has none.
