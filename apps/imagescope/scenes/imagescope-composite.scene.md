---
type: reference
audience: contributor
status: draft
scene:
  launch: imagescope
  size: 1500x1000
  stepSettleMs: 400
  env:
    BOXER_IMAGESCOPE_SAMPLE: "bands"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# imagescope — two bands as a composite, linked and then unlinked

The window opens on the synthetic stack from `BOXER_IMAGESCOPE_SAMPLE`, at
its first time and first band. The scene chooses **composite** on the Bands
row, which shows the band shown and the next one, B2 and B3, as layers in
green and magenta under one recipe over the range of their data together
(ADR-0401 §SD1, §SD4), and waits for the phase line to name the composite
and for the legend under the image. It then clears **Linked**: each layer
keeps its own recipe, and the window states beside the image that the
colours are not comparable with another view's (§SD2). The statement is a
label in the accessibility tree, found by its text.

```jsonl trace
{"do":"note","text":"--- the stack opens on its first band ---"}
{"do":"wait","valueContains":"time day 0 · band B2","role":"label","settleMs":1500}

{"do":"note","text":"--- two bands as a linked composite ---"}
{"do":"click","name":"composite","role":"button","settleMs":1200}
{"do":"wait","valueContains":"composite of B2 in green, B3 in magenta, linked","role":"label"}
{"do":"wait","valueContains":"bands B2, B3 for a composite","role":"label","comment":"the selection taken holds both bands"}
{"do":"wait","name":"Linked","role":"check_box"}
{"do":"capture","text":"imagescope-composite-linked"}

{"do":"note","text":"--- unlinked: the statement is drawn beside the image ---"}
{"do":"click","name":"Linked","role":"check_box","settleMs":800}
{"do":"wait","valueContains":"unlinked: each layer has its own stretch","role":"label"}
{"do":"wait","valueContains":"composite of B2 in green, B3 in magenta, unlinked","role":"label"}
{"do":"capture","text":"imagescope-composite-unlinked","sidecars":["svg","tree"]}
```
