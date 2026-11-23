---
type: reference
audience: contributor
status: draft
scene:
  launch: launcher
  size: 1300x900
  layoutExceptions:
    - invariant: clipped
      role: label
      reason: a row elides a summary longer than its width by design; the length budget is held by the tree-wide summary test, not here
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# launcher — the list narrowed to one maturity grade

The launcher lists the apps a host may open. Its maturity checkboxes
(ADR-0375), in the *Filters* fold, restrict the list to the grades ticked,
with none ticked meaning no restriction. The scene types a query that matches
every app and reads how many the list holds, opens the fold, ticks the
incubating grade and asserts that the list holds fewer and at least one, that
the fold's title names the grade, and that a row carries the incubating
badge, and clears the selection again.

```jsonl trace
{"do":"type","role":"text_input","text":"."}
{"do":"read","valueContains":" apps","role":"label","pattern":"^\\d+ of (?P<all>\\d+) apps$"}
{"do":"click","name":"Filters"}
{"do":"wait","name":"Incubating"}
{"do":"wait","name":"Supported"}
{"do":"wait","name":"Core"}
{"do":"capture","text":"launcher-maturity-checks","sidecars":["svg","tree"]}
{"do":"click","name":"Incubating"}
{"do":"read","valueContains":" apps","role":"label","pattern":"^\\d+ of (?P<incubating>\\d+) apps$"}
{"do":"expect","of":"incubating","min":1}
{"do":"expect","of":"incubating","minus":"all","max":-1}
{"do":"read","contains":"Filters","on":"name","pattern":"^Filters · only (?P<grade>Incubating)$"}
{"do":"wait","value":"incubating","role":"label","nth":0}
{"do":"capture","text":"launcher-maturity-incubating","sidecars":["svg","tree"]}
{"do":"click","contains":"Clear","role":"button","nth":0}
{"do":"read","valueContains":" apps","role":"label","pattern":"^\\d+ of (?P<back>\\d+) apps$"}
{"do":"expect","of":"back","minus":"all","eq":0}
{"do":"read","contains":"Filters","on":"name","pattern":"^(?P<plain>Filters)$"}
```
