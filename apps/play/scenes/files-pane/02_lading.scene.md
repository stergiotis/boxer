---
type: reference
audience: contributor
status: draft
scene:
  launch: play
  size: 1600x1000
  stepSettleMs: 350
  env:
    BOXER_PLAY_WINDOW_SIZE: "1600x1000"
    BOXER_PLAY_AUTORUN: "1"
    BOXER_PLAY_FOCUS_FILES: "1"
  requires: ["clickhouse", "table:boxer.fssnap"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Files pane — a lading snapshot

A lading snapshot browsed inside play (ADR-0200 M7): the Files pane over
`fs('*')`. It reads whatever mounts the local store holds and is skipped when
it holds none.

```sql
SELECT path, size, mtime, is_dir, content_hash, text FROM fs('*') ORDER BY path LIMIT 500
```

```jsonl trace
{"do":"note","text":"ADR-0200 M7 — a lading snapshot browsed inside play"}
{"do":"sleep","settleMs":4000,"comment":"as above, plus the macro expansion and a read of the store"}
{"do":"wait","valueContains":"directories","role":"label","settleMs":800,"comment":"the status line, so the interning ran over real entries"}
{"do":"wait","contains":"Hidden names","settleMs":400,"comment":"the pane's own strip"}
{"do":"capture","text":"files-pane-lading","comment":"a snapshot, browsed from a query"}
```
