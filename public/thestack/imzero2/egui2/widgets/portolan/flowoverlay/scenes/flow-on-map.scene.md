---
type: reference
audience: contributor
status: draft
scene:
  launch: widgets
  size: 1100x900
  stepSettleMs: 350
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# flow on a map

The vector-field layer of ADR-0249 in the widget gallery: an analytic jet and a row of vortices, served through the in-memory pyramid and drawn by `portolan/flowoverlay` as particles over the offline country outlines. The scene waits for the first window to arrive and the trails to fill, checks that a pan inside the window's margin asks the source for nothing, and captures the map. What the picture cannot show — that pace is a screen quantity, that an orbit closes, that a late reply is dropped — is asserted by the package's tests in the default lane; this scene is the picture and the request count.

```jsonl trace
{"do":"wait","role":"text_input","comment":"the gallery has mounted"}
{"do":"sleep","settleMs":200}
{"do":"focus","role":"text_input"}
{"do":"type","role":"text_input","text":"flow on a map","comment":"narrow the gallery to the one demo"}
{"do":"wait","contains":"flow on a map","settleMs":400}
{"do":"click","contains":"flow on a map","comment":"expand the demo's section"}
{"do":"read","valueContains":"segments in one paintSegments","role":"label","pattern":"(?P<particles>\\d+) particles, (?P<segments>[1-9]\\d{3,}) segments","comment":"the window arrived and the trails have filled: thousands of segments, one opcode"}
{"do":"expect","of":"particles","min":1000}
{"do":"read","valueContains":"requests","role":"label","pattern":"(?P<before>\\d+) requests"}
{"do":"expect","of":"before","eq":2,"comment":"one request served the first view, and one fetched the steps the time strip says playback reaches next (ADR-0251 SD9)"}
{"do":"drag","x":520,"y":420,"toX":620,"toY":460,"steps":12,"durationMs":500,"settleMs":1200,"comment":"a pan of 100 x 40 pixels, inside the margin the window was asked with"}
{"do":"read","valueContains":"requests","role":"label","pattern":"(?P<after>\\d+) requests, (?P<dropped>\\d+) late"}
{"do":"expect","of":"after","minus":"before","eq":0,"comment":"the margin is what a pan spends"}
{"do":"hover","x":560,"y":540,"settleMs":600,"comment":"over the map, which has no node of its own"}
{"do":"read","valueContains":"vector mean","role":"label","pattern":"vector mean (?P<vmean>[\\d.]+) m/s .* scalar mean (?P<smean>[\\d.]+) m/s","comment":"the readout tells the two means apart"}
{"do":"expect","of":"smean","minus":"vmean","min":-0.051,"comment":"the scalar mean is never the shorter, to the readout's rounding"}
{"do":"wait","valueContains":"step 1 of 9","role":"label","comment":"the time strip (ADR-0251) reads the first step"}
{"do":"click","contains":"Next","role":"button","comment":"a whole step on"}
{"do":"wait","valueContains":"03:00 UTC · step 2 of 9","role":"label","comment":"the strip's readout is the step's valid time"}
{"do":"click","contains":"Last","role":"button"}
{"do":"wait","valueContains":"step 9 of 9","role":"label","settleMs":1500,"comment":"and the layer has fetched the last step's window"}
{"do":"capture","text":"flow-on-map","comment":"jet, vortices and trails over the outlines"}
```
