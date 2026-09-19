---
type: reference
audience: contributor
status: draft
scene:
  launch: tally
  size: 1400x1000
  requires: [clickhouse, "table:boxer.fssnap"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# tally — browsing the local lading store

ADR-0200 M2: open the app, pick a mount, enter a directory, select a file, and
walk the bottom leaf's tabs — preview, recorded attributes, history, diff,
find, disk usage, problems — each asserted through the accessibility tree, with
a capture per tab for a human to look at.

The scene needs a reachable ClickHouse with a provisioned lading store holding
two mounts. The anchors below match

    boxer fs snapshot --mount 0x3BFE363BCF148002 --name boxer-doc doc

taken from the repository root, and a second mount named `lading-src` for the
diff. Without the store the runner reports the scene as skipped; with a store
that lacks those mounts it fails at its first mount wait.

```jsonl trace
{"do":"note","text":"ADR-0200 M2 — tally against the local store"}
{"do":"wait","value":"Mounts","role":"label","settleMs":500,"comment":"the window is up; the mount list is a lane and may still be loading"}
{"do":"wait","contains":"boxer-doc  ·","role":"button","settleMs":400,"comment":"the mount list arrived (the count suffix tells the mount button from the breadcrumb root)"}
{"do":"click","contains":"boxer-doc  ·","role":"button"}
{"do":"wait","name":"Follow latest","settleMs":600,"comment":"the mount is selected: its snapshots and the follow toggle are up"}
{"do":"note","text":"--- a known starting point: the app restores the last workingset on a plain open (ADR-0148), so pane A may be anywhere; target A, switch its mount away and back, which resets it to the root following latest ---"}
{"do":"click","name":"A","role":"button","settleMs":200}
{"do":"click","contains":"lading-src  ·","role":"button","settleMs":400}
{"do":"click","contains":"boxer-doc  ·","role":"button","settleMs":600}
{"do":"wait","name":"Follow latest","settleMs":400}
{"do":"note","text":"--- enter a directory: select by pointer, then Enter ---"}
{"do":"click","value":"adr","role":"label","pointer":true,"nth":0,"settleMs":300,"comment":"nth 0: pane A comes first in the tree; pane B shows the same directory"}
{"do":"key","text":"Enter"}
{"do":"wait","name":"adr","role":"button","nth":0,"settleMs":600,"comment":"the breadcrumb grew a segment: we are inside"}
{"do":"note","text":"--- narrow the listing with the quick filter, then select the file: preview and info follow ---"}
{"do":"focus","role":"text_input","nth":0,"comment":"pane A's quick filter is the first text input in the window (pane B has its own)"}
{"do":"type","role":"text_input","nth":0,"text":"0198","settleMs":300}
{"do":"click","value":"0198-fs-snapshot-store.md","role":"label","pointer":true,"nth":0,"settleMs":300}
{"do":"wait","valueContains":"0198-fs-snapshot-store.md  ·","role":"label","settleMs":1500,"comment":"the preview header names the file and its size"}
{"do":"capture","text":"tally-preview","comment":"pane A inside adr with 0198-fs-snapshot-store.md selected, its preview below"}
{"do":"click","name":"Info","role":"button","settleMs":400,"comment":"the Info tab of the bottom leaf — a dock tab is a button named by its title, so it resolves wherever the layout puts it"}
{"do":"wait","value":"content_hash","role":"label","settleMs":1500,"comment":"the Info grid carries the recorded BLAKE3 hash"}
{"do":"capture","text":"tally-info","comment":"the Info tab: the entry's attributes from fs()"}
{"do":"note","text":"--- History: the selected path across every snapshot of the mount ---"}
{"do":"click","name":"History","role":"button","settleMs":400}
{"do":"wait","valueContains":" across ","role":"label","settleMs":1500,"comment":"the history header: N snapshot(s) carry the path"}
{"do":"capture","text":"tally-history","comment":"the History tab: timeline flags and the versions table"}
{"do":"note","text":"--- Diff: point pane B at the other mount and compare pane A's directory against it ---"}
{"do":"click","name":"B","role":"button","settleMs":300,"comment":"the Mounts clicks now address pane B"}
{"do":"click","contains":"lading-src  ·","role":"button","settleMs":600}
{"do":"click","name":"Diff","role":"button","settleMs":400}
{"do":"wait","valueContains":"added · ","role":"label","settleMs":2500,"comment":"the diff summary: counts of added / removed / modified"}
{"do":"capture","text":"tally-diff","comment":"the Diff tab: pane A's directory against pane B's snapshot, coloured by change"}
{"do":"note","text":"--- Find: a name search under pane A's directory ---"}
{"do":"click","name":"Find","role":"button","settleMs":400}
{"do":"click","name":"A","role":"button","settleMs":300,"comment":"search in pane A's directory, not B's"}
{"do":"focus","role":"text_input","nth":3,"comment":"the find pattern box. Measured, not reasoned: with the Find tab up the accessibility tree lists the bottom leaf's inputs first and in reverse (needle, min, ext, pattern), then the pane filters"}
{"do":"type","role":"text_input","nth":3,"text":"0198","settleMs":200}
{"do":"click","name":"Search","role":"button","settleMs":300}
{"do":"wait","value":"1 result(s) in this directory","role":"label","settleMs":1500,"comment":"the pattern matched exactly the one ADR"}
{"do":"capture","text":"tally-find","comment":"the Find tab: results of the name search"}
{"do":"note","text":"--- Du: directory totals and the treemap ---"}
{"do":"click","name":"Du","role":"button","settleMs":400}
{"do":"wait","valueContains":"Disk usage","role":"label","settleMs":2500}
{"do":"capture","text":"tally-du","comment":"the Du tab: the one-pass du table and the file treemap"}
{"do":"note","text":"--- Problems: unreadable entries, and the audit on demand ---"}
{"do":"click","name":"Problems","role":"button","settleMs":400}
{"do":"wait","valueContains":"unreadable entries","role":"label","settleMs":1500}
{"do":"capture","text":"tally-problems","comment":"the Problems tab"}
```
