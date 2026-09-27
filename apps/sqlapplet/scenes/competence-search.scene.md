---
type: reference
audience: contributor
status: draft
scene:
  launch: launcher
  size: 1400x900
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# competence-search — the three competence applets answer to one word

The launcher matches Display, Topics and Keywords and never Summary
(ADR-0214). The competence book's three documents carry "Competence" in
their titles and `competence` / `capmap` in their keyword lists (ADR-0168
§SD9), so one typed word must surface all three. The scene types it into
the search field and waits for each row.

```jsonl trace
{"do":"wait","value":"App state","role":"label","settleMs":2000,"comment":"the browse view is up"}
{"do":"focus","role":"text_input","nth":0,"comment":"the launcher's only text input is its search field"}
{"do":"type","role":"text_input","nth":0,"text":"competence","settleMs":500}
{"do":"wait","value":"Competence browser","role":"label","nth":0,"settleMs":2000}
{"do":"wait","value":"Competence overview","role":"label","nth":0,"settleMs":500}
{"do":"wait","value":"Competence links","role":"label","nth":0,"settleMs":500}
{"do":"capture","text":"launcher-competence"}
```
