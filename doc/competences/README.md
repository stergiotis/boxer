---
type: reference
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# The competence vault

This directory is the competence corpus — what the toolkit can do, what in
the tree realises each part, how the parts relate, and what people have
claimed about each one and on what basis. It is read by the
`public/gov/capmapcorpus` package, served as the `competence`,
`competencesection`, `competencerelation`, `competenceclaim` and
`competenceevidence` keelson tables, and loaded into `boxer.facts` by
`boxer capmap load` — and written back out of it by `boxer capmap dump`
([ADR-0168](../adr/0168-capmap-business-capability-corpus.md),
[ADR-0337](../adr/0337-competences-anchored-to-the-tree-and-assessed-by-evidenced-claims.md)).

**Two words for one thing, on purpose.** The literature — and this vault —
calls these units *business capabilities*, and the files say so: a
directory-backed one is a `capability.md` and links to it are
`[[slug/capability]]`. boxer calls them **competences** everywhere it names
them itself, because "capability" already means the runtime's security
capabilities here (ADR-0026), and one word for two unrelated ideas makes every
mention ambiguous. Keep writing the vault in the vault's language; the parser
translates (ADR-0168 §SD6).

**The `boxer` catalog is versioned; every other catalog here is git-ignored.**
ADR-0168 §SD7 ignored the whole directory because which catalogs may be
published had not been settled, and the first catalog was lost from a working
tree that way. ADR-0337 §SD5 narrows the rule to the catalogs it was written
for: `boxer/` is committed and CI parses it; a catalog derived from another
source stays out by the same mechanism. The providers are empty rather than
erroring when the directory is bare, as the ADR tables are.

## What belongs here

One directory per catalog root, holding Obsidian-shaped markdown. A competence
with children is a directory holding a `capability.md`; a leaf is a plain
`{slug}.md` beside its siblings. The `boxer` catalog has three level-1 roots,
one per customer type (ADR-0337 §SD4):

```
doc/competences/boxer/
  toolbelt/            <- for a consumer of the toolkit
    capability.md
    watchbill/
      capability.md    <- level 2: a subsystem with a customer of its own
      durable-work.md  <- level 3: something a customer could want alone
  cell/                <- for a person operating agents in the cell
  practice/            <- for the maintainer of this repository
```

The slug is the directory name, or the filename without its extension,
normalised to `LowerSpinalCase` — so `AdversarialRobustness`,
`adversarial_robustness` and `adversarial-robustness` are one competence. Slugs
are unique across the whole vault, not per branch.

Two things do **not** belong here, both because the parser reads this tree as a
single slug namespace:

- **Reference notes** — standards, technologies, papers. Competences cite them
  by wikilink, and a citation key (`Jouppi-1990`, `GDPR-Art-17`) is not a
  well-formed slug, so such a link is reported as `external` rather than broken.
- **Other note kinds** that happen to share a name with a competence.

## Front matter

```yaml
---
name: "Durable work"           # display name: a what, not a how
abbrev: DW                     # short form, optional
synopsis: "jobs that outlive a process"   # optional
level: 2                       # 1 root, 2 subsystem, 3 competence, 4 building block
parent_ids:
  - "[[toolbelt/capability]]"  # level 4 may carry several
domain: boxer-toolbelt
catalog: boxer
realised_by:                   # anchors into the tree, kind:key (ADR-0337 §SD1)
  - adr:0223
  - pkg:public/keelson/runtime/watchbill
  - app:watchbill
  - howto:watchbill-jobs
assessment:                    # claims, one per dimension (ADR-0337 §SD2)
  - dimension: readiness
    rating: 7
    basis: [adr:0223, howto:watchbill-jobs, positioning:watchbill]
    by: "<handle>"
    at: 2026-10-26
  - dimension: evolution
    rating: product
    basis: [positioning:watchbill]
    by: "<handle>"
    at: 2026-10-26
tags: [needs-review]           # the triage channel
---
```

**Anchors** are typed strings the parser resolves against the tree, and not
Obsidian links. The kinds and their keys: `adr` (four-digit number), `pkg`
(repository-relative import path), `app` (the app's directory name), `table`
(`boxer.facts` or `keelson/<name>`), `howto`, `explanation`, `positioning`
and `trial` (the page's slug), `env` (variable name), `applet` (the minted
slug), `scene` (the file stem without `.scene`), `skill` (the directory name).
An anchor resolves to `direct`, `unresolved`, or `unchecked` when the parse
runs where that kind's corpus is out of reach; an anchor to a retired decision
resolves and is listed separately.

**Claims** carry a dimension, a rating on its scale, a basis of anchors, an
assessor and a date. The registered dimensions:

| Dimension | Scale | What it measures |
| --- | --- | --- |
| `readiness` | 1–9: named, decided, built in part, built end to end, exercised against the real engine, documented, positioned, measured, operated | how far the competence has travelled |
| `evolution` | `genesis`, `custom`, `product`, `commodity` | what kind of thing it is in the world; not a maturity |
| `importance` | `foundational`, `differentiating`, `innovating` | why the toolkit carries it |
| `pain` | 0–5: none, a known rough edge, costs a maintainer monthly, weekly, blocks a consumer without a workaround, blocks the toolkit's own workload | what it costs the people around it |

A dimension absent from the list is *unassessed*. A claim with an empty basis
is an affirmation, and the lint lists it; so is one with no assessor or date,
or a basis the tree does not carry. An unknown dimension or a rating off the
scale fails the parse. The `competenceclaim` table shows each readiness claim
beside the rung the anchors *support* — the highest rung every one of whose
lower rungs some signal holds — and the two differing is a row to read, not
an error.

`maturity:` and `pain:` as bare 0..5 keys, with `255` for not assessed, are
read for older vaults and written back only when recorded; new notes make a
claim instead. The sixteen `lifecycle_<phase>_{by,at}` keys are read and
written the same way and are not authored for the boxer catalog.

The body is h1-delimited prose, kept verbatim rather than decomposed (ADR-0168
§SD5), so the vault round-trips. The recommended sections for a new note are
*Scope*, *Mechanisms*, *Evidence*, *Boundaries* and *Standards*; the parser
does not enforce them.

## Checking it

`boxer capmap parse` reads the vault and reports counts, the files that were not
competences, the links and anchors that did not resolve, anchors to retired
decisions, and claims with problems — no database needed:

```sh
go run -tags="$(cat ./tags)" ./public/app capmap parse
```

It finds this directory by walking up from the working directory; set
`BOXER_CAPMAP_VAULT` to read one kept somewhere else. Anchors resolve against
the checkout the command runs in. doclint does not walk this directory
(`--exclude 'doc/competences/'` in the governance gate): the notes carry the
vault's front matter, not the doc standard's, and this command is their lint.

`boxer capmap similar` is the one verb that writes here. It measures how alike
every pair of notes' prose is — the normalised compression distance, 0 for
identical text and 1 for nothing shared — and records each competence's nearest
neighbours as a `similar:` stanza in its frontmatter, which the parser reads
back as scored relations and the lint lens lists. Ancestors and descendants are
never paired, comparison stays within a catalog unless `--cross` asks for the
mapping between two, and a note without prose is left alone. Run it with
`--dry-run --out report.json` first; the write touches only the notes whose
stanza changes, and comes back as an ordinary diff:

```sh
go run -tags="$(cat ./tags)" ./public/app capmap similar --dry-run --out /tmp/similar.json
```

## Keep it publishable

This repository is public. Notes here must carry no sibling or private
repository names, no personal filesystem paths, and no individuals' details —
the same rule the rest of the tree follows. An assessor's `by` is a handle,
not a name.
