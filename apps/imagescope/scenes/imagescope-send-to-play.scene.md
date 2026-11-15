---
type: reference
audience: contributor
status: draft
scene:
  launch: imagescope
  size: 1500x1000
  stepSettleMs: 400
  env:
    BOXER_IMAGESCOPE_SAMPLE: "field"
    BOXER_IMAGESCOPE_REGION: "900,250,160,160"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# imagescope — the region's pixels sent to play

The window opens on the seeded sample with a region marked, as in the main
scene. **Send to play** publishes the region's stored samples as the dataset
`pixels` and opens play on it with the query that restates the statistics
table. The scene asserts what the button reports after the reply — the
number of pixels sent — and that the median ClickHouse computes from the
published pixels is the one the window computed from the planes. The
window's table cells are buttons and play's detail pane uses labels, which
is how the two are told apart.

```jsonl trace
{"do":"note","text":"--- the region is measured in the window ---"}
{"do":"wait","valueContains":"Region: columns 900–1059, rows 250–409","role":"label","settleMs":1500}
{"do":"wait","name":"15653","role":"button","comment":"the window's median of the region"}

{"do":"note","text":"--- Send to play publishes the pixels and opens play on them ---"}
{"do":"click","name":"Send to play","role":"button"}
{"do":"wait","valueContains":"opened in play","role":"label","settleMs":3000}
{"do":"read","valueContains":"opened in play","role":"label","pattern":"opened in play — (?P<pixels>[0-9]+) pixels"}
{"do":"expect","of":"pixels","is":"25600"}
{"do":"wait","contains":"SQL Playground","role":"window"}

{"do":"note","text":"--- ClickHouse's statistics of the published pixels are the window's ---"}
{"do":"wait","value":"25600","role":"label","settleMs":2500,"comment":"count() over the dataset"}
{"do":"wait","value":"15653","role":"label","comment":"quantileExactLow(0.5): the same lower median"}
{"do":"capture","text":"imagescope-in-play"}
```
