---
type: reference
audience: contributor
status: draft
scene:
  launch: regex_explorer
  size: 1000x700
  requires: [clickhouse]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# regex explorer — the three result tabs

The regex explorer of ADR-0054, at a small window size, where the layout
is tightest. It waits for the engine check, builds a pattern from
cheatsheet tokens inserted at the caret, loads the capture-group showcase,
and walks the three result tabs: the Go matches, the ClickHouse functions
with the Go model's prediction beside each, and the multi-pattern tab with
one line VectorScan refuses. It asserts that a token lands at the caret
rather than at the end, that every modelled function agrees with
ClickHouse, that a replacement reaches the replace functions, and that the
refused line is named with ClickHouse's message while the other lines still
report hits.

```jsonl trace
{"do":"wait","valueContains":"engine check ✓","role":"label","settleMs":1500}
{"do":"type","id":11728253319578034029,"text":"ab"}
{"do":"key","text":"ArrowLeft"}
{"do":"click","name":"Character classes"}
{"do":"click","name":"\\d","role":"button"}
{"do":"click","name":"\\w","role":"button"}
{"do":"read","id":11728253319578034029,"pattern":"^(?P<built>a\\\\d\\\\wb)$"}
{"do":"click","name":"capture groups (user@host)","role":"button"}
{"do":"wait","name":"Matches (3)","settleMs":500}
{"do":"capture","text":"regex-explorer-matches"}
{"do":"click","name":"ClickHouse functions"}
{"do":"wait","valueContains":"ClickHouse answered in","role":"label","settleMs":1000}
{"do":"wait","valueContains":"✓ same","role":"label","nth":5}
{"do":"type","id":380914638141582870,"text":"<\\2>"}
{"do":"wait","valueContains":"<example.com> <test.org> <sub.domain.net>","role":"label","settleMs":800}
{"do":"capture","text":"regex-explorer-functions"}
{"do":"click","name":"Multi-pattern (VectorScan)"}
{"do":"type","role":"multiline_text_input","nth":1,"text":"@test\\.\n(?U)a+\n[0-9]{2}"}
{"do":"wait","valueContains":"failed with error","role":"label","settleMs":1500}
{"do":"read","valueContains":"refused by VectorScan","role":"label","pattern":"(?P<hits>\\d+) of (?P<sent>\\d+) line\\(s\\) hit · (?P<refused>\\d+) refused"}
{"do":"expect","of":"hits","eq":1}
{"do":"expect","of":"refused","eq":1}
{"do":"capture","text":"regex-explorer-multi"}
```
