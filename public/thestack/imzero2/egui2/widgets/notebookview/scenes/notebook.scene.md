---
type: reference
audience: contributor
status: draft
scene:
  launch: widgets
  size: 1200x3700
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Notebook view — a sealed report, its statements, and a stale record

The notebook view of ADR-0371, in the widget gallery's notebook demo. The
subject is a fixture notebook: one markdown document with three stored
records, presentation cells over them, figures in a sentence, an image and a
recording.

The gallery registers no renderer. The two charts and the graph are play's
panels, which play registers and the host binary links: the readout counts
them, so a binary without play fails the scene instead of showing tables.

Five captures. The first is the report: no statement drawn, every record
fresh, the figures in the opening sentence equal to what the records hold.
The second switches the second chart alone to bars: two cells of one
renderer keep their own state. The third folds one cell to its header, the
fourth shows the statements: the notebook. The fifth is after an edit to
one record's statement: what is stored is no longer what the statement would
compute, and the cells that read that record say so while still showing it.

The assertions are on the accessibility tree: the demo's readout counts the
cells, the records, the problems and the renderers; the status badge names
the state; the two cells that filter and compute say what was refused.

```jsonl trace
{"do":"note","text":"ADR-0371 — the fixture notebook drawn as a report"}
{"do":"wait","role":"text_input","comment":"the gallery has mounted"}
{"do":"focus","role":"text_input"}
{"do":"key","text":"A","modifiers":16,"comment":"select whatever filter a previous run left"}
{"do":"type","role":"text_input","text":"notebook","comment":"narrow the gallery to the notebook demo"}
{"do":"wait","contains":"notebook (sealed report)","settleMs":400}
{"do":"drag","x":907,"y":647,"toX":1188,"toY":3650,"steps":8,"settleMs":600,"comment":"the gallery window by its corner, to the size of the viewport, before the demo is opened: the notebook is longer than the window the gallery opens in, and a table first shown below the fold fits its columns to rows it has not drawn. A position, so it goes stale if the gallery's default size moves"}
{"do":"click","contains":"notebook (sealed report)","comment":"expand the demo"}
{"do":"read","valueContains":"notebook:","role":"label","pattern":"notebook: (?P<cells>\\d+) cells · (?P<records>\\d+) records · (?P<problems>\\d+) problems"}
{"do":"expect","of":"cells","eq":21}
{"do":"expect","of":"records","eq":3}
{"do":"expect","of":"problems","eq":0}
{"do":"read","valueContains":"renderers:","role":"label","pattern":"renderers: (?P<renderers>\\d+) registered"}
{"do":"expect","of":"renderers","min":15,"comment":"play's panels, registered by play; the gallery imports none of them"}
{"do":"wait","valueContains":"sealed","role":"label","nth":0,"comment":"the status badge"}
{"do":"wait","valueContains":"412.3 ms","role":"label","comment":"the figure in the sentence, as stored"}
{"do":"wait","valueContains":"WHERE: a record filters","role":"label","comment":"the filtering cell says where the filter belongs"}
{"do":"wait","valueContains":"GROUP BY: a record computes","role":"label","comment":"the computing cell says where the computation belongs"}
{"do":"capture","text":"notebook-report","sidecars":["tree"],"settleMs":800}

{"do":"note","text":"--- two cells of one renderer keep their own state: the second chart alone is switched to bars ---"}
{"do":"click","name":"Bar","nth":1}
{"do":"capture","text":"notebook-second-chart-bars","settleMs":800}
{"do":"click","name":"Line","nth":1}

{"do":"note","text":"--- a cell folded to its header: the whole cell, and nothing under it is built ---"}
{"do":"click","contains":"chart","role":"button","nth":0,"settleMs":400,"comment":"the first chart's header"}
{"do":"wait","value":"folded: 1 cells","role":"label"}
{"do":"capture","text":"notebook-folded","settleMs":800}
{"do":"click","name":"Unfold all","role":"button","settleMs":400}
{"do":"wait","value":"folded: 0 cells","role":"label"}

{"do":"note","text":"--- the notebook: the same text with its statements shown ---"}
{"do":"click","name":"shown (notebook)","role":"radio_button"}
{"do":"wait","contains":"record stats","role":"button","settleMs":400,"comment":"a record's header, over its statement"}
{"do":"capture","text":"notebook-statements","settleMs":800}
{"do":"click","name":"hidden (report)","role":"radio_button"}

{"do":"note","text":"--- an edit to a record's statement: stored, shown, and marked stale ---"}
{"do":"click","name":"edit a record's statement","role":"check_box"}
{"do":"wait","valueContains":"edited since collection: p99_by_hour","role":"label"}
{"do":"wait","valueContains":"reads p99_by_hour, which was edited since collection","role":"label","nth":0}
{"do":"capture","text":"notebook-stale","settleMs":800}
```
