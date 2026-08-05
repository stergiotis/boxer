---
type: reference
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# The competence vault

This directory is the business-capability corpus — what the toolbelt can do,
how the parts relate, and (when anyone has judged it) how mature each part is.
It is read by the `public/gov/capmapcorpus` package, served as the
`competence` / `competencesection` / `competencerelation` keelson tables, and
ingested into `boxer.facts` by `boxer capmap ingest`
([ADR-0168](../adr/0168-capmap-business-capability-corpus.md)).

**Two words for one thing, on purpose.** The literature — and this vault —
calls these units *business capabilities*, and the files say so: a
directory-backed one is a `capability.md` and links to it are
`[[slug/capability]]`. boxer calls them **competences** everywhere it names
them itself, because "capability" already means the runtime's security
capabilities here (ADR-0026), and one word for two unrelated ideas makes every
mention ambiguous. Keep writing the vault in the vault's language; the parser
translates (§SD6).

**Everything here except this file is git-ignored.** That is deliberate, and
recorded as §SD7: which catalogs may be published, and under what licence, has
not been settled, and committing the tree now would decide it by accident. So a
working tree and CI see different amounts of data — the providers are empty
rather than erroring when the directory is bare, which is the behaviour the ADR
tables already established.

## What belongs here

One directory per catalog root, holding Obsidian-shaped markdown. A competence
with children is a directory holding a `capability.md`; a leaf is a plain
`{slug}.md` beside its siblings.

```
doc/competences/
  boxer-toolbelt/           <- a level-1 competence, and a directory
    capability.md
    observability/
      capability.md
      observability-zerolog-logging.md
```

The slug is the directory name, or the filename without its extension,
normalised to `LowerSpinalCase` — so `AdversarialRobustness`,
`adversarial_robustness` and `adversarial-robustness` are one competence.

Two things do **not** belong here, both because the parser reads this tree as a
single slug namespace:

- **Reference notes** — standards, technologies, papers. Competences cite them
  by wikilink, and a citation key (`Jouppi-1990`, `GDPR-Art-17`) is not a
  well-formed slug, so such a link is reported as `external` rather than broken.
  A reference note whose name *is* a well-formed slug (`madr`, `diataxis`)
  would collide with the competence namespace, which is why they live outside.
- **Other note kinds** that happen to share a name with a competence.

## Front matter

```yaml
---
name: "Continuous Profiling"   # display name
abbrev: ObsProf                # short form, optional
synopsis: "(synonyms: …)"      # optional
level: 3                       # 1 macro, 2 meso, 3 micro, 4 building block
parent_ids:
  - "[[observability]]"        # level 4 may carry several
domain: boxer-toolbelt
catalog: boxer
maturity: 255                  # 0..5, or 255 for "not assessed"
pain: 255                      # 0..5, or 255 for "not assessed"
---
```

`255` is the not-assessed sentinel, deliberately far from the 0..5 scale so a
reader who forgets to filter it produces an obviously wrong answer rather than a
plausible one. As of 2026-08-05 every competence in this vault carries it: the
hierarchy is written down, the scoring is not.

The body is h1-delimited prose, kept verbatim rather than decomposed (§SD5), so
the vault round-trips. Headings are not constrained to a fixed set.

## Checking it

`boxer capmap parse` reads the vault and reports counts, the files that were not
competences, and the links that did not resolve — no database needed:

```sh
go run -tags="$(cat ./tags)" ./public/app capmap parse
```

It finds this directory by walking up from the working directory; set
`BOXER_CAPMAP_VAULT` to read one kept somewhere else.

## Keep it publishable

This repository is public and the ignore rule is one `git add -f` away from
being bypassed. Notes here must carry no sibling or private repository names, no
personal filesystem paths, and no individuals' details — the same rule the rest
of the tree follows, applied here because a git-ignored file is not a guarantee.
