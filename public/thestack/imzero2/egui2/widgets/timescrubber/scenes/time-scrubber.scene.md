---
type: reference
audience: contributor
status: draft
scene:
  launch: widgets
  size: 1300x900
  stepSettleMs: 300
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# time scrubber

The time strip of ADR-0251 in the widget gallery, with no map under it: a forecast that is hourly for a day and three-hourly after it. The demo simulates what a loader would hold from the position alone and fixes the wall clock, so what the scene reads is a function of what it clicked. The scene steps, sets and clears the range from the playhead, plays into a bracket, and captures the strip. What a pointer does on the band — an edge, the body, a drag shorter than a step, a double click — is asserted by the package's tests in the default lane against scripted registers, because the strip's interior has no accessibility nodes for a trace to aim at.

```jsonl trace
{"do":"wait","role":"text_input","comment":"the gallery has mounted"}
{"do":"sleep","settleMs":200}
{"do":"focus","role":"text_input"}
{"do":"type","role":"text_input","text":"time scrubber (a strip","comment":"narrow the gallery to the one demo"}
{"do":"wait","contains":"time scrubber (a strip","settleMs":400}
{"do":"click","contains":"time scrubber (a strip","comment":"expand the demo's section"}
{"do":"wait","valueContains":"2026-03-01 12:00 UTC · step 13 of 40","role":"label","comment":"the readout is the step's valid time, zone and all"}
{"do":"wait","valueContains":"+2.5 h · 1 h step · range 9–31","role":"label","comment":"how far from the fixed now, how long the bracket under the playhead, and the range"}
{"do":"wait","value":"2026-03-01 12:00","role":"text_input","comment":"the field beside it holds the same instant, in a form it can parse back"}
{"do":"click","contains":"Next","role":"button","comment":"a whole step on"}
{"do":"wait","valueContains":"2026-03-01 13:00 UTC · step 14 of 40","role":"label"}
{"do":"wait","value":"2026-03-01 13:00","role":"text_input","comment":"the field followed the playhead"}
{"do":"click","contains":"Last","role":"button","comment":"the last step of the range, not of the series"}
{"do":"wait","valueContains":"step 31 of 40","role":"label"}
{"do":"wait","valueContains":"· 3 h step","role":"label","comment":"the cadence has coarsened here, and the readout says so because the rate is in steps"}
{"do":"click","contains":"Clear range","role":"button"}
{"do":"click","contains":" In","role":"button","comment":"the range starts at the playhead and runs to the end"}
{"do":"wait","valueContains":"· range 31–40","role":"label"}
{"do":"click","contains":"First","role":"button"}
{"do":"wait","valueContains":"step 31 of 40","role":"label","comment":"First is the range's first, not the series'"}
{"do":"capture","text":"time-scrubber","comment":"bars, caps, state notches, the now line and the tint before it, the range on its band"}
{"do":"click","contains":"Clear range","role":"button"}
{"do":"click","contains":"Play","role":"button"}
{"do":"wait","valueContains":"between steps","role":"label","timeoutMs":20000,"comment":"playback entered a bracket, which it does only once both of its steps are held"}
{"do":"click","contains":"Pause","role":"button"}
```
