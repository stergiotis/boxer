---
type: reference
audience: contributor
status: draft
scene:
  launch: widgets
  size: 1300x1000
  stepSettleMs: 300
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# time scrubber forms

The same widget as [time scrubber](./time-scrubber.scene.md) in the forms a host picks when the strip is not the page's subject (ADR-0251 §SD10): one line, an index axis, and two thousand steps. The scene reads each strip's own readout — the three are at different positions on purpose, so each one's words are its own — and captures the page.

```jsonl trace
{"do":"wait","role":"text_input","comment":"the gallery has mounted"}
{"do":"sleep","settleMs":200}
{"do":"focus","role":"text_input"}
{"do":"type","role":"text_input","text":"time scrubber forms"}
{"do":"wait","contains":"time scrubber forms","settleMs":400}
{"do":"click","contains":"time scrubber forms","comment":"expand the demo's section"}
{"do":"wait","valueContains":"Sun 12:00 UTC","role":"label","comment":"the one-line form says the time and nothing else, in the short form the spacing allows"}
{"do":"wait","valueContains":"step 28 of 40","role":"label","comment":"the index strip, parked in the three-hourly block"}
{"do":"wait","valueContains":"· 3 h step","role":"label"}
{"do":"wait","valueContains":"step 901 of 2000","role":"label","comment":"the crowded strip"}
{"do":"wait","valueContains":"· 10 min step","role":"label"}
{"do":"capture","text":"time-scrubber-forms","comment":"the one-line form and the index axis"}
{"do":"scroll_into_view","valueContains":"Two thousand steps","role":"label"}
{"do":"capture","text":"time-scrubber-crowded","settleMs":600,"comment":"two thousand steps as an envelope, the held run as a band on the state lane"}
```
