---
type: reference
audience: contributor
status: draft
scene:
  launch: widgets
  size: 1400x1000
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Tree widget — pointer interaction

ADR-0176's verification plan asks for one thing no unit test can reach: that
clicking a row selects it and clicking a disclosure control folds it, in a real
render. The interesting failure is SD6's — a doubled `row_ui` replay emits
every row's widget ids twice, which does not break rendering or clicking, only
read-back, silently and newest-wins. So the scene asserts the selection, not
the picture: a capture would still look correct.

The subject is the gallery's "tree outline" demo (ADR-0176 M4); its readout
lines are what the assertions watch. Run it with
`scripts/dev/scene.sh` and this file's path (ADR-0248).

```jsonl trace
{"do":"note","text":"ADR-0176 M4 — the tree widget's pointer interaction, asserted through the accessibility tree"}
{"do":"wait","role":"text_input","comment":"the gallery has mounted; its filter box is the only text input while every demo is folded"}
{"do":"focus","role":"text_input"}
{"do":"type","role":"text_input","text":"tree","comment":"narrow the gallery so the demo lands near the top of the scroll"}
{"do":"wait","contains":"tree outline","settleMs":400}
{"do":"click","contains":"tree outline","comment":"expand the demo's section"}
{"do":"wait","name":"collapse all","settleMs":400,"comment":"the demo body is up"}
{"do":"scroll_into_view","name":"collapse all","settleMs":600,"comment":"the rows must be ON SCREEN, not merely laid out: the etable emits only its visible range, and a pointer press is a position"}

{"do":"note","text":"--- baseline: fold everything, so exactly one row and one disclosure control remain ---"}
{"do":"click","name":"collapse all"}
{"do":"wait","valueContains":", 1 rows on screen","role":"label","comment":"CollapseAll left only the root"}

{"do":"note","text":"--- click-to-expand: the disclosure control, not the row ---"}
{"do":"click","name":"\ue13a","comment":"Phosphor caret-right — the only collapsed disclosure control on screen is the root's. A PUA codepoint because the glyph comes from the bundled icon font, not the text font: the solid triangles it replaced escaped to the CJK fallback (see render.go)"}
{"do":"wait","valueContains":", 4 rows on screen","role":"label","comment":"Animalia opened over its three phyla"}

{"do":"note","text":"--- click-to-select: the row, past its label, by pointer ---"}
{"do":"click","value":"Chordata","role":"label","pointer":true}
{"do":"wait","value":"selected: [Chordata]","role":"label","comment":"the click reached the row's sense region behind the label"}

{"do":"note","text":"--- the row click did NOT toggle: a row that names something selects it ---"}
{"do":"read","valueContains":" rows on screen","role":"label","pattern":", (?P<rows>\\d+) rows on screen"}
{"do":"expect","of":"rows","eq":4,"comment":"still four rows, so selecting did not fold or unfold"}
{"do":"capture","text":"tree-outline","comment":"the selected row, whose outline must be closed on all four sides — the M4 defect was a missing bottom edge"}

{"do":"note","text":"--- and the state is Go's: reveal opens a buried node's ancestors and selects it ---"}
{"do":"click","name":"reveal Danaus plexippus"}
{"do":"wait","value":"selected: [Danaus plexippus]","role":"label","comment":"four ranks and a phylum away from anything that was open"}
```
