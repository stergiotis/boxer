---
type: reference
audience: contributor
status: draft
scene:
  launch: imagescope
  size: 1500x1100
  stepSettleMs: 400
  env:
    BOXER_IMAGESCOPE_SAMPLE: "bands"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# imagescope — an expression over two bands, run, and its result in the list

The window opens on the synthetic stack from `BOXER_IMAGESCOPE_SAMPLE`,
showing its first band. The Operations section opens on the expression
operator with one input, the band shown; **Add input** adds the next band.
The scene types an expression over the two, finds the plan stated before
anything runs, presses **Run**, and finds the result in the Images row as
r1, shown, with the line that says what was made and the record under its
fold.

```jsonl trace
{"do":"note","text":"--- the stack opens; the operations section waits for an expression ---"}
{"do":"wait","valueContains":"day 0 · 0 d · 1 of 3","role":"label","settleMs":1500}
{"do":"wait","valueContains":"cannot run: expression is required","role":"label"}

{"do":"note","text":"--- a second band and an expression over both ---"}
{"do":"scroll_into_view","name":"Add input","role":"button"}
{"do":"click","name":"Add input","role":"button"}
{"do":"scroll_into_view","name":"Parameter expression"}
{"do":"capture","text":"imagescope-operation-panel"}
{"do":"type","name":"Parameter expression","text":"(B3 - B2) / (B3 + B2)"}
{"do":"wait","valueContains":"inputs: B2, B3","role":"label"}
{"do":"wait","valueContains":"Plan: 160 × 120 gray f32","role":"label"}
{"do":"capture","text":"imagescope-operation-planned"}

{"do":"note","text":"--- run: the result joins the images and is shown ---"}
{"do":"click","name":"Run","role":"button"}
{"do":"wait","name":"r1","role":"button","settleMs":800}
{"do":"wait","valueContains":"made r1 · expr of B2, B3 · 160 × 120 · gray f32","role":"label"}
{"do":"wait","valueContains":"viewing: r1 · expr of B2, B3","role":"label","comment":"the phase line names the image shown"}
{"do":"wait","contains":"Record of r1: expr@1","comment":"the record's fold"}
{"do":"capture","text":"imagescope-operation-result"}
```
