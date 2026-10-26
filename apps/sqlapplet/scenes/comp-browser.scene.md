---
type: reference
audience: contributor
status: draft
scene:
  launch: "subject_alias = 'comp-browser'"
  size: 1600x1100
  env: {BOXER_SQLAPPLET_WINDOW_SIZE: "1560x1020"}
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# comp-browser — the competence vault read as one screen

The competence book's browser (ADR-0168 M11): the vault under
`doc/competences` served live by the `competence*` keelson tables, drawn as
a treemap with the focused note beside it and the corpus as a list
underneath. The vault is git-ignored, so what the scene shows depends on the
checkout; an empty vault yields an empty table rather than an error.

The window is opened wider than the host's 900×640 fallback, because at that
size the four panes have no height and the capture would show the browser's
chrome with nothing in it. The walk itself — a row click re-focusing the
graph — is checked over a fixture vault by
`TestSceneCompBrowserWalksFromTheTable`, in the integration lane, since what
this scene can assert depends on whatever vault the checkout holds.

```jsonl trace
{"do":"read","valueContains":"rows ·","role":"label","pattern":"(?P<rows>\\d+) rows","settleMs":3000}
{"do":"expect","of":"rows","min":1,"comment":"a populated vault; an empty one is the ADR's documented off-repo state, not a pass"}
{"do":"capture","text":"comp-browser","settleMs":1000}
```
