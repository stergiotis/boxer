---
type: reference
audience: contributor
status: draft
scene:
  launch: "subject_alias = 'audit-trail'"
  size: 1400x900
  requires: [clickhouse]
  env:
    BOXER_AGENT_TEST_GRANTS: "true"
    BOXER_AGENT_COORDINATORS_WHERE: "kind = 'chatlet'"
    BOXER_LLM_SCRIPT: "apps/chatlet/scenes/chatlet-audit-trail.script.jsonl"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chatlet — audit-trail analytics opens ready to work

[ADR-0364](../../../doc/adr/0364-chatlets-the-chat-specialised-by-a-committed-markdown-document.md)
(proposed). A window minted from `apps/chatlet/book/audit-trail.md`, against the
scripted model in
[chatlet-audit-trail.script.jsonl](./chatlet-audit-trail.script.jsonl). A test
grant stands in for the person's approval.

The empty window shows the document's greeting and starters, and the Definition
tab shows the document itself. A starter fills the composer; the person sends
it. The window starts with Apps on and at the document's limits, with nothing set
in the environment for it. The model opens play and runs SQL there, which is
the level the document asks for. The statement reads `keelson.apps`, so the
assertion is the host's own answer: the one committed chatlet was selected by
`BOXER_AGENT_COORDINATORS_WHERE`, and no name list is set.

```jsonl trace
{"do":"note","text":"ADR-0364: a chatlet minted from a document, against a scripted model"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"wait","valueContains":"Ask what happened on this host","role":"label","comment":"the document's greeting"}
{"do":"wait","name":"The last 24 hours","role":"button"}
{"do":"capture","text":"chatlet-empty","sidecars":["svg","tree"]}
{"do":"click","name":"Definition","role":"button"}
{"do":"wait","valueContains":"audit trail for the person","role":"label","comment":"what the model is told"}
{"do":"capture","text":"chatlet-definition","sidecars":["svg","tree"]}
{"do":"click","name":"Grants and widenings","role":"button"}
{"do":"wait","valueContains":"List the task grants of the last 7 days","role":"multiline_text_input","comment":"a starter fills the composer and sends nothing"}
{"do":"click","contains":"Send","role":"button","comment":"the person sends it"}
{"do":"wait","valueContains":"Play shows which chatlets","role":"label","settleMs":1500}
{"do":"wait","valueContains":"⚙ opened play as window 2","role":"label"}
{"do":"wait","valueContains":"⚙ run in window 2 · completed","role":"label"}
{"do":"wait","valueContains":"selected=1 of 1","role":"label","comment":"the predicate selected the chatlet"}
{"do":"capture","text":"chatlet-audit-trail"}
{"do":"click","contains":"newest","role":"button","pointer":true,"comment":"the chatlet takes focus back from play, so F1 opens its help"}
{"do":"key","text":"F1","settleMs":1200}
{"do":"wait","valueContains":"A chat window is one conversation","role":"label"}
{"do":"capture","text":"chatlet-audit-trail-help"}
```
