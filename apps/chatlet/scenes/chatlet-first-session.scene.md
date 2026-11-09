---
type: reference
audience: contributor
status: draft
scene:
  launch: "subject_alias = 'audit-trail'"
  size: 1400x900
  env:
    BOXER_AGENT_COORDINATORS_WHERE: "kind = 'chatlet'"
    BOXER_LLM_SCRIPT: "apps/chatlet/scenes/chatlet-first-session.script.jsonl"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chatlet — a first session, for someone who has not used one before

[ADR-0364](../../../doc/adr/0364-chatlets-the-chat-specialised-by-a-committed-markdown-document.md)
§SD6 (proposed). A window from `apps/chatlet/book/audit-trail.md`, against the
scripted model in
[chatlet-first-session.script.jsonl](./chatlet-first-session.script.jsonl), with
no test grant: the access dialog is the host's, and a person answers it.

The empty window says what the model can and cannot do, and the bar is folded.
Details unfolds it, and the model line appears only there. The dialog shows the
document's sentence apart from the model's plan. The running turn counts steps
without a limit beside them. The first write brings the Artefact tab forward. Under the
answer are the document's follow-ups; one fills the composer and sends nothing.
The conversation is watched without anyone asking.

```jsonl trace
{"do":"note","text":"ADR-0364: a chatlet's first session"}
{"do":"wait","valueContains":"with scripted at","role":"label"}
{"do":"wait","valueContains":"It cannot publish or export","role":"label"}
{"do":"capture","text":"first-session-empty","sidecars":["svg","tree"]}
{"do":"scroll_into_view","valueContains":"kept on boxer.facts","role":"label","settleMs":500,"comment":"where the conversation goes can be reached at the default size"}
{"do":"capture","text":"first-session-empty-end","sidecars":["svg","tree"]}
{"do":"click","contains":" Details","role":"button"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"capture","text":"first-session-details","sidecars":["svg","tree"]}
{"do":"click","contains":" Details","role":"button"}
{"do":"click","name":"The last 24 hours","role":"button"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"describes itself: Answers questions about this host","role":"label","settleMs":600,"comment":"the document's sentence, in the host's dialog"}
{"do":"wait","valueContains":"as the model wrote it: open play","role":"label","comment":"the model's plan stays its own"}
{"do":"capture","text":"first-session-dialog","sidecars":["svg","tree"]}
{"do":"click","name":"Approve","role":"button"}
{"do":"wait","valueContains":"· step 3","role":"label","comment":"no limit beside the count"}
{"do":"wait","valueContains":"The scripted model wrote this report","role":"label","comment":"the Artefact tab came forward: a hidden tab is not laid out"}
{"do":"capture","text":"first-session-artefact","sidecars":["svg","tree"]}
{"do":"wait","name":"Explain it simply","role":"button","settleMs":500}
{"do":"click","name":"Explain it simply","role":"button"}
{"do":"wait","valueContains":"Explain your last answer in plain words","role":"multiline_text_input","comment":"filled, not sent"}
{"do":"click","contains":" Details","role":"button"}
{"do":"wait","contains":"Watching","role":"button","comment":"watched from the first send"}
{"do":"capture","text":"first-session-followups","sidecars":["svg","tree"]}
```
