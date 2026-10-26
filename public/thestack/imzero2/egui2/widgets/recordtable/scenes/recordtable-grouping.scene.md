---
type: reference
audience: contributor
status: draft
scene:
  launch: widgets
  size: 1400x1000
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# recordtable — grouped rows, in a real render

ADR-0336's verification plan leaves two things to a render no unit test
reaches: that the band — a full-width row block under the aggregate cells —
takes the click that folds its group, and how the band looks beside the
cells it shares a line with. The first is asserted here through the readout
the gallery's "recordtable (grouped)" demo prints (a driver reads a cell's
text, not a band's layout); the second is the capture, judged by eye.

Run it with `scripts/dev/scene.sh` and this file's path (ADR-0248).

```jsonl trace
{"do":"note","text":"ADR-0336 M2 — grouped rows: the band folds, selects when its key is a row, and the aggregates sit under their column"}
{"do":"wait","role":"text_input","comment":"the gallery has mounted; its filter box is the only text input while every demo is folded"}
{"do":"focus","role":"text_input"}
{"do":"type","role":"text_input","text":"recordtable","comment":"narrow the gallery so the demo lands near the top of the scroll"}
{"do":"wait","contains":"recordtable (grouped)","settleMs":400}
{"do":"click","contains":"recordtable (grouped)","comment":"expand the demo's section"}
{"do":"wait","name":"collapse all","settleMs":400,"comment":"the demo body is up"}
{"do":"scroll_into_view","name":"collapse all","settleMs":600,"comment":"the rows must be ON SCREEN: the etable emits only its visible range, and a pointer press is a position"}
{"do":"wait","value":"lines: 37","role":"label","comment":"section › path, everything open: 23 readings under 14 groups"}

{"do":"note","text":"--- baseline: fold everything, so only the five section bands remain ---"}
{"do":"click","name":"collapse all"}
{"do":"wait","value":"lines: 5","role":"label","comment":"cargo, engine, hull, note and the null section, last"}

{"do":"note","text":"--- the chevron unfolds one level: cargo's own children stay folded ---"}
{"do":"click","name":"","nth":0,"comment":"Phosphor caret-right, a PUA codepoint from the icon font; the first collapsed band is cargo"}
{"do":"wait","value":"lines: 6","role":"label","comment":"cargo opened over its one child group, holds"}
{"do":"wait","value":"last action: toggled cargo","role":"label","comment":"the grid reported the toggle; the host's Apply folded it"}

{"do":"note","text":"--- the band itself folds too: a click past the label, on the row block ---"}
{"do":"click","value":"engine","role":"label","pointer":true,"comment":"the band's label is not a button; the press reaches the Frame behind it"}
{"do":"wait","value":"lines: 7","role":"label"}
{"do":"wait","value":"last action: toggled engine","role":"label"}

{"do":"note","text":"--- a leaf click selects its record row, through the group lines ---"}
{"do":"click","name":"expand all"}
{"do":"wait","value":"lines: 37","role":"label"}
{"do":"click","name":"/holds/0/mass","nth":0,"comment":"the first of three readings of that path, Aurora's"}
{"do":"wait","value":"selected: Aurora cargo /holds/0/mass","role":"label"}
{"do":"click","name":"aggregate: mean","comment":"to the sum; the demo refits on the change, now that the grid is on screen — the first fit saw only the rows the gallery's scroll had uncovered"}
{"do":"wait","name":"aggregate: sum","role":"button"}
{"do":"wait","value":"last action: aggregate toggled","role":"label"}
{"do":"capture","text":"recordtable-grouping-section","comment":"the bands at three depths beside the sum under the value column, a selected leaf, stripes on the leaves only"}

{"do":"note","text":"--- a level whose key is a record row: the band selects, the chevron still folds ---"}
{"do":"click","name":"vessel › path"}
{"do":"wait","value":"lines: 45","role":"label","comment":"three vessels, every path open under each: the expansion is keyed by path, so the earlier expand-all carries over"}
{"do":"click","name":"collapse all"}
{"do":"wait","value":"lines: 3","role":"label","comment":"the three vessel bands, all on screen"}
{"do":"click","value":"Boreas","role":"label","pointer":true,"comment":"the band's label; its row block reports GroupClicked, not a toggle"}
{"do":"wait","value":"selected vessel: Boreas","role":"label"}
{"do":"wait","value":"lines: 3","role":"label","comment":"nothing folded or unfolded"}
{"do":"click","name":"\ue13a","nth":1,"comment":"Phosphor caret-right on the second band: the chevron still folds a band whose click selects"}
{"do":"wait","value":"last action: toggled Boreas","role":"label"}
{"do":"wait","value":"lines: 8","role":"label","comment":"Boreas over draught, engines, holds, its length reading and the null path group"}
{"do":"capture","text":"recordtable-grouping-vessel","comment":"the selected vessel band painted with the selection fill"}

{"do":"note","text":"--- and the expansion is Go's: reveal opens a buried row's ancestors and selects it ---"}
{"do":"click","name":"collapse all"}
{"do":"wait","value":"lines: 3","role":"label"}
{"do":"click","name":"reveal Calypso's engine temperature"}
{"do":"wait","value":"selected: Calypso engine /engines/0/temp","role":"label"}
{"do":"wait","value":"lines: 11","role":"label","comment":"Calypso, engines and 0 opened; its other groups stay folded, as does the rest"}
```
