---
type: reference
audience: contributor
status: draft
scene:
  launch: capinspector
  size: 1400x1000
  stepSettleMs: 400
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# capinspector — the capability inspector

A tour of the inspector's surfaces: the empty state of a plain open, a
status-bar chip retargeting the open window, the wrapped picker, the wiring
graph with its legend and activity strip, a click on a graph node, the per-cap
prose and live-consumer line, the three storage-schema sections, a status-bar
chip reopening a closed window, and F1 opening the inspector's own help.

The graph is painter-only, so the node click is by coordinates; the step after
it asserts which cap became selected, so a layout change fails the scene rather
than clicking somewhere else. The storage-schema sections are found by
scrolling, because the schema widget's dock tabs leave the accessibility tree
while they are off-screen. The schema's glyph-legend toggle has no
accessibility node, so the scene does not open it.

```jsonl trace
{"do":"note","text":"--- a plain open has no selection ---"}
{"do":"wait","value":"Pick a capability above.","role":"label","settleMs":1500}
{"do":"capture","text":"capinspector-empty"}

{"do":"note","text":"--- a status-bar chip retargets the open window (the raise path) ---"}
{"do":"click","contains":"persist:","role":"button"}
{"do":"wait","value":"runtime.persist.* state","role":"label"}
{"do":"click","contains":"adhoc ","role":"button","comment":"the status segment (\"adhoc\" and a state glyph), not the picker's \"adhoc.*\"; the segment has its own entry now"}
{"do":"wait","value":"adhoc.* ad-hoc datasets","role":"label"}

{"do":"note","text":"--- the picker; the graph, its legend and the activity strip ---"}
{"do":"click","name":"Audit + facts backend","role":"button","settleMs":1200,"comment":"the first frame of a cap lays the graph out; give it a moment"}
{"do":"wait","valueContains":"Backend: runtime/factsstore","role":"label"}
{"do":"wait","valueContains":" no active backend","role":"label","comment":"the legend row is up; each entry is its swatch glyph and its text in one label"}
{"do":"wait","valueContains":" to inactive backend","role":"label"}
{"do":"wait","value":"This capability is a runtime-level service; every app inherits it implicitly.","role":"label","comment":"facts has no per-app filter"}
{"do":"capture","text":"capinspector-facts","comment":"facts selected: green cap, amber chstore, legend and activity strip below"}

{"do":"note","text":"--- a click on a graph node selects its cap ---"}
{"do":"click","x":401,"y":491,"pointer":true,"comment":"the 'Subject router' node; coordinates because the graph has no accessibility nodes"}
{"do":"wait","value":"Subject family: (all subjects)","role":"label"}
{"do":"click","name":"llm.* model inference","role":"button"}
{"do":"wait","valueContains":"Live consumers in this registry:","role":"label","comment":"llm has a per-app filter, so the line lists the apps that declare it"}
{"do":"capture","text":"capinspector-llm"}

{"do":"note","text":"--- the storage schema of each cap that declares a table ---"}
{"do":"click","name":"Audit + facts backend","role":"button"}
{"do":"scroll_into_view","name":"Storage schema — boxer.facts","role":"button"}
{"do":"click","name":"Storage schema — boxer.facts","role":"button","settleMs":1000}
{"do":"hover","x":470,"y":400}
{"do":"scroll","x":0,"y":-350,"settleMs":800}
{"do":"wait","name":"structure","role":"button","comment":"the schema widget's navigator tab: the TableDesc built"}
{"do":"capture","text":"capinspector-facts-schema"}
{"do":"scroll","x":0,"y":-2000,"settleMs":800}
{"do":"capture","text":"capinspector-facts-schema-scrolled","comment":"scrolled past the dock's top: its tab strip holds at the scroll area's edge and nothing paints over the picker or the title bar"}

{"do":"click","name":"runtime.persist.* state","role":"button"}
{"do":"scroll_into_view","name":"Storage schema — boxer.persiststate","role":"button"}
{"do":"click","name":"Storage schema — boxer.persiststate","role":"button","settleMs":1000}
{"do":"hover","x":470,"y":400}
{"do":"scroll","x":0,"y":-350,"settleMs":800}
{"do":"wait","name":"structure","role":"button"}
{"do":"capture","text":"capinspector-persist-schema"}

{"do":"click","name":"watchbill durable work","role":"button"}
{"do":"scroll_into_view","name":"Storage schema — boxer.watchbill","role":"button"}
{"do":"click","name":"Storage schema — boxer.watchbill","role":"button","settleMs":1000}
{"do":"hover","x":470,"y":400}
{"do":"scroll","x":0,"y":-350,"settleMs":800}
{"do":"wait","name":"structure","role":"button"}
{"do":"capture","text":"capinspector-watchbill-schema"}

{"do":"note","text":"--- a status-bar chip opens a closed inspector on its cap (the fresh-open path) ---"}
{"do":"click","name":"Close window","role":"button","settleMs":600}
{"do":"click","contains":"facts:","role":"button","settleMs":1200}
{"do":"wait","valueContains":"Backend: runtime/factsstore","role":"label"}

{"do":"note","text":"--- F1 over the inspector opens the inspector's help ---"}
{"do":"key","text":"F1","settleMs":1200}
{"do":"wait","contains":"Help center","role":"window"}
{"do":"wait","value":"Inspecting capabilities","role":"label","comment":"the reader shows the inspector's overview, not the index"}
{"do":"capture","text":"capinspector-help"}
```
