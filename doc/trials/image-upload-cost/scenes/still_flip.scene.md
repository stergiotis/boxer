---
type: reference
audience: contributor
status: draft
scene:
  launch: widgets
  size: 1100x900
  stepSettleMs: 350
  env: {IMZERO2_HEADLESS_DUMP_EVERY: "30"}
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative.

# imagebench, a texture replaced in place under an unchanged frame

The side check of the [image upload-cost trial](../README.md) §3: the
`still` arm uploads one pattern, and 150 frames after `flip` is clicked it
uploads a second pattern to the same texture id while nothing else in the
frame changes. Later `nudge` moves the image by one point, which changes the
mesh over the canvas and nothing about the texture. The host dumps every
thirtieth live frame beside the captures; whether the frames after the swap,
and after the nudge, show the second pattern is read off them by
`still_flip_check.sh`, not asserted here.

```jsonl trace
{"do":"wait","role":"text_input"}
{"do":"sleep","settleMs":200}
{"do":"focus","role":"text_input"}
{"do":"type","role":"text_input","text":"image bench"}
{"do":"wait","contains":"image bench","settleMs":400}
{"do":"click","contains":"image bench"}
{"do":"click","name":"still","role":"button"}
{"do":"click","name":"s1024","role":"button"}
{"do":"sleep","settleMs":2000}
{"do":"click","name":"flip","role":"button"}
{"do":"sleep","settleMs":12000}
{"do":"capture","text":"after-flip"}
{"do":"sleep","settleMs":1500}
{"do":"click","name":"nudge","role":"button"}
{"do":"sleep","settleMs":3000}
{"do":"capture","text":"after-nudge"}
{"do":"sleep","settleMs":2000}
```
