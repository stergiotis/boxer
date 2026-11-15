---
type: reference
audience: contributor
status: draft
scene:
  launch: imagescope
  size: 1100x1000
  stepSettleMs: 400
  env:
    BOXER_IMAGESCOPE_SAMPLE: "field"
    BOXER_IMAGESCOPE_REGION: "900,250,160,160"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# imagescope — a sample image, a recipe change and a measured region

The window opens on the synthetic field from `BOXER_IMAGESCOPE_SAMPLE`, with
the region of `BOXER_IMAGESCOPE_REGION` marked, so no file picker and no drag
are needed. The scene reads the region's line and asserts its pixel count,
changes the limits to a percentile stretch and checks that the statistics —
which are of the samples — did not move, shows one-to-one, and ends with F1
on the app's own help. The image itself is painted and has no name in the
tree; what the scene asserts is the text beside it.

```jsonl trace
{"do":"note","text":"--- the seeded sample opens and its region is measured ---"}
{"do":"wait","valueContains":"Region: columns 900–1059, rows 250–409","role":"label","settleMs":1500}
{"do":"read","valueContains":"Region: columns","role":"label","pattern":"= (?P<pixels>[0-9]+) pixels"}
{"do":"expect","of":"pixels","is":"25600"}
{"do":"wait","name":"Send to play","role":"button"}
{"do":"capture","text":"imagescope-measured"}

{"do":"note","text":"--- a percentile stretch changes the picture, not the region line ---"}
{"do":"click","name":"1–99 %","role":"button"}
{"do":"wait","valueContains":"Region: columns 900–1059, rows 250–409","role":"label","settleMs":1200}
{"do":"capture","text":"imagescope-percentile"}

{"do":"note","text":"--- one sample per screen pixel: the line under the image stops saying means ---"}
{"do":"click","name":"1:1","role":"button","settleMs":1200}
{"do":"capture","text":"imagescope-actual"}
{"do":"click","name":"Fit","role":"button","settleMs":800}

{"do":"note","text":"--- clearing the region returns the pane to its instruction ---"}
{"do":"click","name":"Clear region","role":"button"}
{"do":"wait","valueContains":"No region is marked","role":"label"}

{"do":"note","text":"--- F1 opens the app's own help ---"}
{"do":"key","text":"F1","settleMs":1200}
{"do":"wait","contains":"Help center","role":"window"}
{"do":"wait","value":"An image as samples","role":"label","comment":"the reader shows the app's overview, not the index"}
{"do":"capture","text":"imagescope-help"}
```
