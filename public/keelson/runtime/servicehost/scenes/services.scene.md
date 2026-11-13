---
type: reference
audience: contributor
status: draft
scene:
  launch: play
  size: 1600x900
  stepSettleMs: 350
  env:
    KEELSON_SERVICES_WHERE: "kind = 'demo'"
    BOXER_PLAY_WINDOW_SIZE: "1560x820"
    BOXER_PLAY_AUTORUN: "1"
    BOXER_PLAY_FOCUS_TABLE: "1"
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# services — a headless app the host runs

The service host (ADR-0352) under a predicate that selects the
demo service: `keelson('services')` lists every headless app the process
registered, with the tick service selected and running and its instance
key, and the lifecycle rows the host wrote for it.

```sql
SELECT id, selected, state, instance_key, started_at FROM keelson('services') ORDER BY id
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":4000}
{"do":"wait","contains":"running","comment":"the result shows the tick service selected and its Mount returned"}
{"do":"capture","text":"services"}
```
