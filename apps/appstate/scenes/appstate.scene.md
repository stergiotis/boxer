---
type: reference
audience: contributor
status: draft
scene:
  launch: appstate
  size: 1300x800
  requires: [clickhouse]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# appstate — what each app keeps

ADR-0185's manager window over the host's own state store: the apps that
keep something across restarts on the left, the selected app's entries in
the middle, a delete per entry and a forget per app. The scene asserts that
the window reads `keelson('app_state')` through the host's endpoint without
an error — the status line reports the read — and captures the result;
what is listed is whatever the live store holds.

```jsonl trace
{"do":"wait","name":"Refresh","role":"button","settleMs":500}
{"do":"read","valueContains":"entries across","role":"label","pattern":"(?P<entries>\\d+) entries across (?P<apps>\\d+) apps","settleMs":4000}
{"do":"expect","of":"entries","min":0}
{"do":"capture","text":"appstate"}
```
