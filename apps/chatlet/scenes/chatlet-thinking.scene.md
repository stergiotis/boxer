---
type: reference
audience: contributor
status: draft
scene:
  launch: "subject_alias = 'audit-trail'"
  size: 1400x900
  env:
    BOXER_AGENT_COORDINATORS_WHERE: "kind = 'chatlet'"
    BOXER_LLM_SCRIPT: "apps/chatlet/scenes/chatlet-thinking.script.jsonl"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chatlet — the reasoning stays out of sight until the person asks for it

[ADR-0364](../../../doc/adr/0364-chatlets-the-chat-specialised-by-a-committed-markdown-document.md)
§SD6 (proposed). The audit-trail document says `thinking: hidden`. The scripted
model in [chatlet-thinking.script.jsonl](./chatlet-thinking.script.jsonl) reasons
for sixteen seconds and then answers. While it reasons the step says so and the
words are not drawn, which the tree of the `chatlet-thinking-hidden` capture
records (the trace has no step for an absence); ticking the setting draws them
at once, in the same turn.

```jsonl trace
{"do":"note","text":"ADR-0364: a chatlet that starts with the reasoning hidden"}
{"do":"wait","valueContains":"with scripted at","role":"label"}
{"do":"click","name":"Settings","role":"button","comment":"before the turn: the Conversations tab repeats the step's line"}
{"do":"wait","contains":"Show the model's reasoning","role":"check_box"}
{"do":"capture","text":"chatlet-thinking-settings","sidecars":["svg","tree"]}
{"do":"click","name":"The last 24 hours","role":"button"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"· reasoning","role":"label","settleMs":1500,"comment":"the step says what the model is doing"}
{"do":"capture","text":"chatlet-thinking-hidden","sidecars":["svg","tree"]}
{"do":"click","contains":"Show the model's reasoning","role":"check_box"}
{"do":"wait","valueContains":"Weighing the question","role":"label","comment":"drawn from the moment the person asks"}
{"do":"capture","text":"chatlet-thinking-shown","sidecars":["svg","tree"]}
{"do":"wait","valueContains":"Nothing to report","role":"label","settleMs":500,"comment":"the turn landed"}
```
