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
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Files pane — the synthesised tree

ADR-0200's play panel interns a result into a file system and hands it to the
browser widget. The interning is table-tested and the widget has its own
scene; what neither sees is the join — a panel that builds a correct tree and
then draws nothing, or draws it against the wrong ids, looks identical to both.

The rows here are literals, so any reachable ClickHouse runs it.

```sql
SELECT e.1 AS path, e.2 AS size, e.3 AS is_dir FROM (SELECT arrayJoin([('src/api/handler.go', 4096, 0), ('src/api/router.go', 2048, 0), ('src/main.go', 1024, 0), ('README.md', 512, 0)]) AS e)
```

```jsonl trace
{"do":"note","text":"ADR-0200 M7 — a result with a path column, browsed"}
{"do":"sleep","settleMs":3500,"comment":"play mounts a dock, restores a layout, runs the pipeline and then the query"}
{"do":"wait","valueContains":"directories","role":"label","settleMs":600,"comment":"the status line: the interning happened and says what it made"}
{"do":"wait","value":"src","role":"label","comment":"a directory NO row named — synthesised because rows nest under it"}
{"do":"wait","value":"README.md","role":"label","comment":"and a row that is a leaf of the root"}
{"do":"capture","text":"files-pane-list","comment":"list mode: the synthesised tree beside the query's own columns"}
{"do":"click","value":"src","role":"label","pointer":true,"comment":"select the synthesised directory"}
{"do":"key","text":"Enter","comment":"Enter enters a directory rather than reporting it"}
{"do":"wait","value":"api","role":"label","settleMs":400,"comment":"one level down, the second synthesised directory"}
{"do":"wait","value":"main.go","role":"label","comment":"and the file beside it"}
{"do":"note","text":"--- a row-backed entry publishes the row cursor, and Detail follows it ---"}
{"do":"click","value":"main.go","role":"label","pointer":true}
{"do":"wait","value":"src/main.go","role":"label","settleMs":800,"comment":"Detail is showing the ROW behind the entry — the selection signal crossed the panels"}
{"do":"wait","valueContains":"row 3 / 4","role":"label","comment":"and it is the right row: main.go is the third of the four the query returned"}
{"do":"note","text":"--- the outline draws the same tree ---"}
{"do":"click","contains":"Outline","role":"button"}
{"do":"wait","value":"main.go","role":"label","settleMs":600}
{"do":"capture","text":"files-pane-outline","comment":"outline mode over the same interned tree"}
```
