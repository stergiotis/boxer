---
type: reference
audience: contributor
status: draft
scene:
  launch: widgets
  size: 960x720
  stepSettleMs: 350
  requires: ["exe:python3"]
  services: [tilestub]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# portolan

Slippy map widget (ADR-0204) — the gallery demo on a stub tile server: Leaflet's map core in Go on the painter lane, panned here by the drag verb after the tiles landed; markers, a route, the dissolved H3 region, the viewport heatmap, the camera readout. The camera itself is asserted by TestScenePortolanCamera in the integration lane; this scene is the picture

```jsonl trace
{"do":"wait","role":"text_input","comment":"the gallery has mounted"}
{"do":"sleep","settleMs":200}
{"do":"focus","role":"text_input"}
{"do":"type","role":"text_input","text":"portolan (slippy","comment":"narrow the gallery to the one demo"}
{"do":"wait","contains":"portolan (slippy","settleMs":400}
{"do":"click","contains":"portolan (slippy","comment":"expand the demo's section"}
{"do":"wait","valueContains":"loading false","role":"label","settleMs":500,"comment":"every tile of the first view landed"}
{"do":"drag","x":380,"y":450,"toX":500,"toY":510,"steps":16,"durationMs":600,"settleMs":1500,"comment":"a pan by the drag verb (ADR-0204 §SD10); the map is the only widget under that point"}
{"do":"wait","valueContains":"loading false","role":"label","settleMs":400,"comment":"the tiles the pan uncovered landed too"}
{"do":"capture","text":"35_portolan","comment":"the map after the pan: stub tiles, overlays, the readout below"}
```
