---
type: reference
audience: contributor
status: draft
scene:
  launch: widgets
  size: 1200x8000
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Notebook samples — each sample drawn from its text and its blobs

The sample notebooks of `mdnotebook/samples` (ADR-0371) in the widget
gallery's notebook samples demo. Three captures, one per sample. The third
is the one this scene exists for: `renderers` holds one cell per renderer,
so its capture is every panel play registers for a notebook, drawn from a
stored record.

The assertions are on the demo's readout: each sample has no problem, which
is what sealed means for a file read with no server.

```jsonl trace
{"do":"note","text":"ADR-0371 — the sample notebooks"}
{"do":"wait","role":"text_input","comment":"the gallery has mounted"}
{"do":"focus","role":"text_input"}
{"do":"key","text":"A","modifiers":16,"comment":"select whatever filter a previous run left"}
{"do":"type","role":"text_input","text":"notebook samples","comment":"narrow the gallery to the samples demo"}
{"do":"wait","contains":"notebook samples (to read","settleMs":400}
{"do":"drag","x":907,"y":647,"toX":1188,"toY":7950,"steps":8,"settleMs":600,"comment":"the gallery window by its corner, to the size of the viewport, before the demo is opened. A position, so it goes stale if the gallery's default size moves"}
{"do":"click","contains":"notebook samples (to read","comment":"expand the demo"}
{"do":"read","valueContains":"sample:","role":"label","pattern":"sample: (?P<cells>\\d+) cells · (?P<records>\\d+) records · (?P<problems>\\d+) problems"}
{"do":"expect","of":"records","eq":2}
{"do":"expect","of":"problems","eq":0}
{"do":"wait","valueContains":"302.2 ms","role":"label","comment":"the figure in the sentence, as stored"}
{"do":"capture","text":"sample-minimal","settleMs":800}

{"do":"note","text":"--- a report: the picture first, how it was collected last ---"}
{"do":"click","name":"sample report","role":"radio_button"}
{"do":"wait","valueContains":"519.5 ms","role":"label","settleMs":400}
{"do":"read","valueContains":"sample:","role":"label","pattern":"sample: (?P<cells>\\d+) cells · (?P<records>\\d+) records · (?P<problems>\\d+) problems"}
{"do":"expect","of":"records","eq":5}
{"do":"expect","of":"problems","eq":0}
{"do":"capture","text":"sample-report","settleMs":1200}

{"do":"note","text":"--- one cell per renderer ---"}
{"do":"click","name":"sample renderers","role":"radio_button"}
{"do":"wait","valueContains":"12 records","role":"label","settleMs":400}
{"do":"read","valueContains":"sample:","role":"label","pattern":"sample: (?P<cells>\\d+) cells · (?P<records>\\d+) records · (?P<problems>\\d+) problems"}
{"do":"expect","of":"records","eq":12}
{"do":"expect","of":"problems","eq":0}
{"do":"capture","text":"sample-renderers","settleMs":2500}
```
