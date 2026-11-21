---
type: reference
audience: contributor
status: draft
scene:
  launch: imagescope
  size: 1100x1000
  stepSettleMs: 400
  env:
    BOXER_IMAGESCOPE_SAMPLE: "bands"
    BOXER_IMAGESCOPE_REGION: "60,20,40,40"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# imagescope — a stack: a time stepped, three bands, frame coordinates

The window opens on the synthetic stack from `BOXER_IMAGESCOPE_SAMPLE`:
three acquisitions along a time axis and four bands, B2, B3, B4 and B8,
placed in a projected reference system at 10 m a pixel, with the region of
`BOXER_IMAGESCOPE_REGION` marked. The scene reads the placement line, steps
the time axis with its **›** button and waits for the selection to be shown
and the region measured again, switches to three bands as red, green and
blue, and hovers the image to read the readout's frame coordinates. The
image is painted and has no node in the tree, so the hover is the one step
aimed by coordinates: the middle of the image as this window size lays it
out.

```jsonl trace
{"do":"note","text":"--- the stack opens on its first time and first band, placed ---"}
{"do":"wait","valueContains":"day 0 · 0 d · 1 of 3","role":"label","settleMs":1500}
{"do":"wait","valueContains":"placed in EPSG:32632","role":"label"}
{"do":"read","valueContains":"placed in EPSG:32632","role":"label","pattern":"pixel (?P<px>[0-9.]+) × [0-9.]+ m"}
{"do":"expect","of":"px","is":"10"}
{"do":"wait","valueContains":"Region: columns 60–99, rows 20–59","role":"label"}
{"do":"capture","text":"imagescope-stack-first"}

{"do":"note","text":"--- the time steps on; the selection is taken and the region measured again ---"}
{"do":"click","name":"›","role":"button","nth":0,"settleMs":1200}
{"do":"wait","valueContains":"day 16 · 16 d · 2 of 3","role":"label"}
{"do":"wait","valueContains":"time day 16 · band B2","role":"label","comment":"the phase line names the selection shown"}
{"do":"wait","valueContains":"Region: columns 60–99, rows 20–59","role":"label","comment":"the marked region is kept"}

{"do":"note","text":"--- three bands, as red, green and blue ---"}
{"do":"click","name":"three bands","role":"button","settleMs":1200}
{"do":"wait","valueContains":"bands B2, B3, B4 as red, green, blue","role":"label"}
{"do":"capture","text":"imagescope-stack-three"}

{"do":"note","text":"--- the readout gives the pointer in the frame ---"}
{"do":"hover","x":550,"y":560,"settleMs":800}
{"do":"read","valueContains":"EPSG:32632)","role":"label","pattern":"E (?P<e>[0-9.]+) m  N (?P<n>[0-9.]+) m"}
{"do":"expect","of":"e","min":500000,"max":501600}
{"do":"expect","of":"n","min":5198800,"max":5200000}
{"do":"capture","text":"imagescope-stack-readout"}
```
