---
type: reference
audience: contributor
status: draft
scene:
  launch: configview
  size: 1300x900
  layoutExceptions:
    - invariant: hit-size
      role: button
      name: "\ue13a"
      reason: the tree widget's expander of a closed section is 13 points wide, one under the floor; it is the widget's, shared by every tree
    - invariant: hit-size
      role: button
      name: "\ue136"
      reason: the same expander on an open section
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# configview — the registry, narrowed by a search and by Only set

The config inspector lists the environment variables the process declares,
grouped by category, each section with how many of its variables are set.
The scene reads a section's count, types a search and asserts that the
section now counts fewer variables, opens the section to show a variable's
row, and then ticks Only set, which leaves no section for a search whose
only match is not set.

```jsonl trace
{"do":"wait","name":"Only set","role":"check_box"}
{"do":"read","valueContains":"observability  (","role":"label","pattern":"observability\\s+\\((?P<set0>\\d+) / (?P<all>\\d+) set\\)"}
{"do":"expect","of":"all","min":2}
{"do":"capture","text":"configview-sections","sidecars":["svg","tree"]}
{"do":"type","role":"text_input","text":"BOXER_LOG_LEVEL"}
{"do":"read","valueContains":"observability  (","role":"label","pattern":"observability\\s+\\((?P<set1>\\d+) / (?P<hits>\\d+) set\\)"}
{"do":"expect","of":"hits","eq":1}
{"do":"expect","of":"hits","minus":"all","max":-1}
{"do":"click","valueContains":"observability  (","role":"label","pointer":true,"count":2}
{"do":"wait","valueContains":"BOXER_LOG_LEVEL","role":"label"}
{"do":"capture","text":"configview-search","sidecars":["svg","tree"]}
{"do":"click","name":"Only set","role":"check_box"}
{"do":"wait","valueContains":"no vars match the current filter","role":"label"}
{"do":"capture","text":"configview-only-set"}
```
