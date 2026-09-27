---
type: reference
audience: contributor
status: draft
scene:
  launch: "subject_alias = 'comp-browser'"
  size: 1400x900
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# comp-browser — the competence vault read as one screen

The competence book's browser (ADR-0168 M11): the vault under
`doc/competences` served live by the `competence*` keelson tables, drawn as
a treemap with the focused note beside it and the corpus as a list
underneath. The vault is git-ignored, so what the scene shows depends on the
checkout; an empty vault yields an empty table rather than an error.

```jsonl trace
{"do":"read","valueContains":"rows ·","role":"label","pattern":"(?P<rows>\\d+) rows","settleMs":3000}
{"do":"expect","of":"rows","min":1,"comment":"a populated vault; an empty one is the ADR's documented off-repo state, not a pass"}
{"do":"capture","text":"comp-browser","settleMs":1000}
```
