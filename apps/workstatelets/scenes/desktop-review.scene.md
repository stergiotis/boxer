---
type: reference
audience: contributor
status: draft
scene:
  launch: "subject_alias = 'ws-desktop-review'"
  size: 1400x900
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# workstatelets — a document opens its windows in its layout

[ADR-0369](../../../doc/adr/0369-workstatelets-a-desktops-windows-and-layout-as-a-markdown-document.md)
(proposed) on the headless host. The launch names a workstatelet's alias, and
the host opens the two windows of
[desktop-review.md](../book/desktop-review.md) instead of a window of its own:
a playground and the app center, under the main-and-stack layout the document
names. Window ▸ Save desktop as workstatelet then writes the two open windows
back out as a document of its own; a scene's host keeps that in memory.

```jsonl trace
{"do":"note","text":"workstatelets: desktop-review opens two windows"}
{"do":"wait","name":"Run","role":"button"}
{"do":"capture","text":"workstatelet-desktop-review","settleMs":1500}
{"do":"click","name":"Window","role":"button"}
{"do":"click","contains":"Save desktop as workstatelet","role":"button"}
{"do":"type","name":"Workstatelet title","text":"My desk"}
{"do":"click","name":"Save","role":"button"}
{"do":"wait","valueContains":"Saved as ws-my-desk (2 windows)","role":"label"}
{"do":"capture","text":"workstatelet-saved","settleMs":500}
```
