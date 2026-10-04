---
type: reference
audience: contributor
status: draft
scene:
  launch: widgets
  size: 1100x900
  stepSettleMs: 350
---

> **Status: draft — pre-human-review.** Generated for a host profile.

# landbench profile cell

```jsonl trace
{"do":"wait","role":"text_input"}
{"do":"sleep","settleMs":200}
{"do":"focus","role":"text_input"}
{"do":"type","role":"text_input","text":"land bench"}
{"do":"wait","contains":"land bench","settleMs":400}
{"do":"click","contains":"land bench"}
{"do":"click","name":"world-z0","role":"button"}
{"do":"click","name":"nofill","role":"button"}
{"do":"sleep","settleMs":5000}
{"do":"sleep","settleMs":5000}
{"do":"sleep","settleMs":5000}
{"do":"sleep","settleMs":5000}
{"do":"sleep","settleMs":5000}
{"do":"tree","text":"bench ","role":"label"}
```
