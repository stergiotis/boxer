---
type: reference
audience: contributor
status: draft
scene:
  launch: play
  size: 1600x1000
  requires: [clickhouse]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# appcenter — the Running page from the Window menu

ADR-0348: the Window menu's Running windows opens the app center on its
Running tab. The scene starts with play alone, opens the tab from the
menu, and asserts that it lists both windows.

```jsonl trace
{"do":"wait","name":"Run","role":"button","comment":"play has mounted"}
{"do":"click","name":"Window","pointer":true}
{"do":"click","name":"Running windows","role":"button"}
{"do":"read","valueContains":"window(s).","role":"label","pattern":"^(?P<windows>2) window\\(s\\)\\.","settleMs":1000,"comment":"play and the app center itself"}
{"do":"capture","text":"appcenter-running-from-menu","settleMs":500}
```
