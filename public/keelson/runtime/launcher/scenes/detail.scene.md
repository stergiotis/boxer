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

# launcher — the detail pane, its Details fold and a help page

The detail pane shows the selected app for a person choosing one: the
summary, the actions, the topics and what the app touches. The builder's
columns — the module it comes from, the bus subjects — wait in a closed
*Details* fold. The scene selects the agent console, opens the fold and
reads the module line, then opens one of the app's help pages, which brings
the Help center up on that page.

```jsonl trace
{"do":"type","role":"text_input","text":"agent console"}
{"do":"wait","name":"Details"}
{"do":"capture","text":"launcher-detail-closed","sidecars":["svg","tree"]}
{"do":"click","name":"Details"}
{"do":"read","valueContains":"github.com/","role":"label","pattern":"^(?P<module>\\S+)@?"}
{"do":"wait","valueContains":"runtime.agent","role":"label","nth":0}
{"do":"capture","text":"launcher-detail-open","sidecars":["svg","tree"]}
{"do":"click","contains":"The agent console","role":"button"}
{"do":"wait","contains":"Help center","role":"window"}
{"do":"wait","valueContains":"is a coordinator driven by hand","role":"label"}
{"do":"capture","text":"launcher-detail-help","sidecars":["svg","tree"]}
```
