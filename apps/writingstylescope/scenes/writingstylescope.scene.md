---
type: reference
audience: contributor
status: draft
scene:
  launch: writingstylescope
  size: 1000x940
  stepSettleMs: 400
  env:
    BOXER_WRITINGSTYLESCOPE_DOCS: "apps/writingstylescope/scenes/harbour-a.md:apps/writingstylescope/scenes/harbour-b.md"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# writingstylescope — two seeded documents with one shared section

The window opens on two fixture documents named by
`BOXER_WRITINGSTYLESCOPE_DOCS`, so the panes hold text of the scene's own
rather than the person's drafts, and the first sweep runs on the first
frame. Each document has four sections over the floor and two below it —
the status line above the title and the title heading, which has no text
of its own; one section of A was copied into B under another heading. The status row's summary is read and its pair count asserted, the
Matrix tab is reached by name and lists the pairs, the Distribution tab is
reached by name and its closing line is read to assert that the closest pair
sits far below the median — the copied section, detached from the bulk — and
F1 opens the app's own help.

```jsonl trace
{"do":"note","text":"--- the seeded documents are swept on the first frame ---"}
{"do":"wait","valueContains":"sections, 16 pairs","role":"label","settleMs":1500}
{"do":"read","valueContains":"sections, 16 pairs","role":"label","pattern":"(?P<rows>\\d+) × (?P<cols>\\d+) sections, (?P<pairs>\\d+) pairs"}
{"do":"expect","of":"rows","eq":4}
{"do":"expect","of":"pairs","eq":16}
{"do":"wait","valueContains":"A: 4 sections (2 below the floor)","role":"label"}
{"do":"capture","text":"writingstylescope-documents"}

{"do":"note","text":"--- the matrix tab ranks the pairs ---"}
{"do":"click","name":"Matrix","role":"button"}
{"do":"wait","value":"Closest pairs","role":"label"}
{"do":"wait","value":"The long hull","role":"label"}
{"do":"capture","text":"writingstylescope-matrix"}

{"do":"note","text":"--- the copied section is detached from the bulk ---"}
{"do":"click","name":"Distribution","role":"button"}
{"do":"wait","valueContains":"Where each pair sits among all the others.","role":"label"}
{"do":"read","valueContains":"Closest pair ","role":"label","pattern":"Closest pair (?P<best>[\\d.]+), median pair (?P<median>[\\d.]+)"}
{"do":"expect","of":"best","max":0.2}
{"do":"expect","of":"median","min":0.5}
{"do":"capture","text":"writingstylescope-distribution"}
{"do":"click","name":"Documents","role":"button"}

{"do":"note","text":"--- F1 opens the app's own help ---"}
{"do":"key","text":"F1","settleMs":1200}
{"do":"wait","contains":"Help center","role":"window"}
{"do":"wait","value":"Shared writing between two documents","role":"label","comment":"the reader shows the app's overview, not the index"}
{"do":"capture","text":"writingstylescope-help"}
```
