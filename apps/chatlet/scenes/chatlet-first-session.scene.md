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

The empty window says what the model can and cannot do, and the bar is one
row. The model's name and endpoint are in the state chip's inspector. The
dialog shows the document's sentence apart from the model's plan. The running
turn counts steps without a limit beside them. The first write brings the
document forward beside the transcript. Under the answer are the document's
follow-ups; one fills the composer and sends nothing. The conversation is
watched without anyone asking: the Conversation menu says so.

```jsonl trace
{"do":"note","text":"ADR-0364: a chatlet's first session"}
{"do":"wait","valueContains":"The model, scripted, runs","role":"label"}
{"do":"wait","valueContains":"It cannot publish or export","role":"label"}
{"do":"capture","text":"first-session-empty","sidecars":["svg","tree"]}
{"do":"scroll_into_view","valueContains":"Kept where the host allows","role":"label","settleMs":500,"comment":"where the conversation goes can be reached at the default size"}
{"do":"capture","text":"first-session-empty-end","sidecars":["svg","tree"]}
{"do":"click","name":"Conversation","role":"unknown","pointer":true,"comment":"the state chip's inspector"}
{"do":"wait","valueContains":"scripted at ","role":"label","comment":"the model and its endpoint, behind the chip"}
{"do":"capture","text":"first-session-details","sidecars":["svg","tree"]}
{"do":"click","name":"Conversation","role":"unknown","pointer":true}
{"do":"click","name":"The last 24 hours","role":"button"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"describes itself: Answers questions about this host","role":"label","settleMs":600,"comment":"the document's sentence, in the host's dialog"}
{"do":"wait","valueContains":"as the model wrote it: open play","role":"label","comment":"the model's plan stays its own"}
{"do":"capture","text":"first-session-dialog","sidecars":["svg","tree"]}
{"do":"click","name":"Approve","role":"button"}
{"do":"wait","valueContains":"· step 3","role":"label","comment":"no limit beside the count"}
{"do":"wait","valueContains":"The scripted model wrote this report","role":"label","comment":"the document came forward beside the transcript"}
{"do":"capture","text":"first-session-artefact","sidecars":["svg","tree"]}
{"do":"wait","name":"Explain it simply","role":"button","settleMs":500}
{"do":"click","name":"Explain it simply","role":"button"}
{"do":"wait","valueContains":"Explain your last answer in plain words","role":"multiline_text_input","comment":"filled, not sent"}
{"do":"click","name":"Conversation","role":"button","comment":"the window's Conversation menu"}
{"do":"wait","contains":"Watching","role":"check_box","comment":"watched from the first send"}
{"do":"capture","text":"first-session-followups","sidecars":["svg","tree"]}
```
