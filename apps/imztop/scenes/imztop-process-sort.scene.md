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
and the first row's RSS cell are the assertions: after the descending sort
the top row carries a resident size in MiB or GiB, which a sort that left
the CPU order in place would not guarantee. The captures show the rows and
the heat tint, which follows the CPU column only while it drives the
order. Which processes appear depends on the machine the scene runs on.
The scene ends on F1 over the window, which opens imztop's own help.

Each header is listed twice in the tree — egui_table lays the header row
out twice — so the anchors take the first. A body cell is a button named
by its text, and the RSS column precedes VSZ, so the first cell with a
byte unit is the first row's RSS.

```jsonl trace
{"do":"wait","name":"Processes","role":"button"}
{"do":"click","name":"Processes","role":"button","settleMs":2500}
{"do":"wait","name":"CPU%~ ▼","role":"button","nth":0}
{"do":"capture","text":"imztop-sort-cpu"}
{"do":"click","name":"RSS","role":"button","nth":0,"settleMs":800}
{"do":"wait","name":"RSS ▼","role":"button","nth":0}
{"do":"read","contains":"iB","role":"button","nth":0,"on":"name","pattern":"^(?P<rss>[\\d.]+) (?P<unit>[KMG]iB)$","comment":"the first row's RSS cell"}
{"do":"expect","of":"unit","matches":"^[MG]iB$"}
{"do":"expect","of":"rss","min":1}
{"do":"capture","text":"imztop-sort-rss-desc"}
{"do":"click","name":"RSS ▼","role":"button","nth":0,"settleMs":800}
{"do":"wait","name":"RSS ▲","role":"button","nth":0}
{"do":"capture","text":"imztop-sort-rss-asc"}
{"do":"key","text":"F1","settleMs":1200}
{"do":"wait","contains":"Help center","role":"window"}
{"do":"wait","value":"Watching a host","role":"label","comment":"the reader shows imztop's overview, not the index"}
{"do":"capture","text":"imztop-help"}
```
