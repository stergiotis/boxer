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

# launcher — the kind checkboxes behind a closed fold

The launcher's kind checkboxes (ADR-0158 §SD5) sit in a *Filters* fold that
starts closed. The scene opens the fold, hides demos, and reads the fold's
title, which names what is hidden so a narrowed list shows while the fold is
closed. It then shows demos again and reads the title back to its plain form.

```jsonl trace
{"do":"wait","name":"Filters"}
{"do":"capture","text":"launcher-details-closed","sidecars":["svg","tree"]}
{"do":"click","name":"Filters"}
{"do":"wait","name":"Demos"}
{"do":"click","name":"Demos"}
{"do":"read","contains":"Filters","on":"name","pattern":"^Filters · (?P<hidden>\\d+) kinds? hidden$"}
{"do":"expect","of":"hidden","eq":1}
{"do":"capture","text":"launcher-details-demos-hidden","sidecars":["svg","tree"]}
{"do":"click","name":"Demos"}
{"do":"read","contains":"Filters","on":"name","pattern":"^(?P<plain>Filters)$"}
```
