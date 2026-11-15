---
type: reference
audience: contributor
status: draft
scene:
  launch: imagescope
  size: 1100x1000
  stepSettleMs: 400
  env:
    BOXER_IMAGESCOPE_SAMPLE: "float"
    BOXER_IMAGESCOPE_REGION: "0,280,200,120"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# imagescope — a float image with invalid samples

The window opens on the synthetic float sample, which spans six decades and
holds an 80 × 80 block of invalid (NaN) samples, with a region marked over
part of that block. The scene asserts that the statistics count the block's
samples as invalid and the rest as valid, then shows the image under an
asinh stretch, under which the statistics — which are of the samples — do
not move. With its default parameter asinh covers about one decade, so the
background stays black beside the peak; the parameter's slider reaches
further, but no step here can set it, since the headless host's `set_value`
does not reach egui's sliders (noted in ADR-0373 as of 2026-11-15). The scene
covers the float path — invalid samples, the binned histogram, a stretch
over limits that span zero — that no PNG or JPEG reaches.

```jsonl trace
{"do":"note","text":"--- the region over the invalid block is measured ---"}
{"do":"wait","valueContains":"Region: columns 0–199, rows 280–399","role":"label","settleMs":1500}
{"do":"read","valueContains":"Region: columns","role":"label","pattern":"= (?P<pixels>[0-9]+) pixels"}
{"do":"expect","of":"pixels","is":"24000"}
{"do":"wait","name":"6400","role":"button","comment":"the invalid column: 80 × 80 samples"}
{"do":"wait","name":"17600","role":"button","comment":"the samples column: the rest"}
{"do":"capture","text":"imagescope-float-measured"}

{"do":"note","text":"--- an asinh stretch changes the picture, not the statistics ---"}
{"do":"click","name":"Advanced — transfer function, colour map, clipping, smoothing, samples"}
{"do":"click","name":"asinh","role":"button","settleMs":1200}
{"do":"wait","name":"b","role":"slider","comment":"asinh takes a parameter, so its slider is shown"}
{"do":"wait","name":"17600","role":"button","comment":"the statistics are of the samples and do not move"}
{"do":"capture","text":"imagescope-float-asinh"}
```
