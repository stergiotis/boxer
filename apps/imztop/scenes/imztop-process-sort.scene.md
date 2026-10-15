---
type: reference
audience: contributor
status: draft
scene:
  launch: imztop
  size: 1400x900
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# imztop — sorting the process table

The process table opens sorted by smoothed CPU, descending. A click on the
RSS header sorts by resident memory, descending first, as a measurement
does; a second click flips it to ascending rather than dropping the sort,
because the list has no order of its own to return to. The header glyphs
are the assertions; the captures show the rows and the heat tint, which
follows the CPU column only while it drives the order. Which processes
appear depends on the machine the scene runs on.

Each header is listed twice in the tree — egui_table lays the header row
out twice — so the anchors take the first.

```jsonl trace
{"do":"wait","name":"Processes","role":"button"}
{"do":"click","name":"Processes","role":"button","settleMs":2500}
{"do":"wait","name":"CPU%~ ▼","role":"button","nth":0}
{"do":"capture","text":"imztop-sort-cpu"}
{"do":"click","name":"RSS","role":"button","nth":0,"settleMs":800}
{"do":"wait","name":"RSS ▼","role":"button","nth":0}
{"do":"capture","text":"imztop-sort-rss-desc"}
{"do":"click","name":"RSS ▼","role":"button","nth":0,"settleMs":800}
{"do":"wait","name":"RSS ▲","role":"button","nth":0}
{"do":"capture","text":"imztop-sort-rss-asc"}
```
