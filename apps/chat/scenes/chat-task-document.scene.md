---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
  requires: [clickhouse, "exists:boxer.fssnap"]
  env:
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-artefact.script.jsonl"
    BOXER_CHAT_ARTEFACT: "true"
    BOXER_CHAT_DRAFT: "Draft a plan in the artefact."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — top task: get a document out

The third of the chat's top tasks (ADR-0404 §SD7, proposed), against the
scripted model in [chat-artefact.script.jsonl](./chat-artefact.script.jsonl).
The person sets *Changes* to Ask first from the bar's words, so each write
waits. The first proposal brings the document forward beside the transcript,
as a decision that says what Accept and Reject each do. Accepted twice, the
document is exported: the confirmation is the person's own act, so Export is
the filled button, and it says what is written, for how long and who can read
it.

```jsonl trace
{"do":"note","text":"top task: accept a proposed change, export the document"}
{"do":"wait","contains":"Send","role":"button"}
{"do":"click","contains":"Model:","role":"button","comment":"the bar's words open Settings"}
{"do":"click","name":"Ask first","role":"radio_button","comment":"Changes: each write waits"}
{"do":"click","name":"View","role":"button","comment":"the window's View menu"}
{"do":"click","pointer":true,"name":"Settings","role":"check_box","comment":"close Settings"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"The model proposes a change to the document","role":"label","settleMs":500}
{"do":"wait","valueContains":"Reject discards it","role":"label"}
{"do":"capture","text":"chat-task-document-proposal","sidecars":["svg","tree"]}
{"do":"click","contains":"Accept","role":"button"}
{"do":"wait","valueContains":"as revision 2","role":"label","settleMs":500,"comment":"the second write, a proposal of its own"}
{"do":"click","contains":"Accept","role":"button"}
{"do":"wait","valueContains":"Drafted the plan in the artefact","role":"label","settleMs":1000}
{"do":"click","contains":"Export","role":"button","comment":"the document's Export"}
{"do":"wait","valueContains":"Export this document?","role":"label"}
{"do":"wait","valueContains":"Anyone who can read the shared file store","role":"label"}
{"do":"capture","text":"chat-task-document-export","sidecars":["svg","tree"]}
{"do":"click","name":" Export","role":"button","comment":"the confirmation's Export"}
{"do":"wait","valueContains":"exported revision 2 to the shared file store","role":"label","settleMs":500}
```
