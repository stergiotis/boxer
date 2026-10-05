---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
  requires: [clickhouse]
  env:
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-questions.script.jsonl"
    BOXER_CHAT_QUESTIONS: "true"
    BOXER_CHAT_DRAFT: "Write me the report."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — the model asks the person with a form

ADR-0265 §SD7 on the headless host, against the scripted model in
[chat-questions.script.jsonl](./chat-questions.script.jsonl): with
Questions on, the model calls `ask_user` with two questions, one of several
choices and one of a single choice. The turn waits while the form is in
the pending bubble; Answer is refused until both questions are answered,
then the same turn continues to the model's reply, and the transcript's tool
line says what was chosen.

The note fields and the answer of one's own are text inputs, which the
headless tree does not list, so the trace does not type into them; the
default-lane test `TestAskUserWaitsForTheFormInsideTheTurn` covers both.

```jsonl trace
{"do":"note","text":"ADR-0265 SD7: ask_user as an inline form"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Send","role":"button","comment":"the seeded draft"}
{"do":"wait","valueContains":"Which sections should","role":"label"}
{"do":"capture","text":"chat-questions-open"}
{"do":"click","contains":"Answer","role":"button","comment":"nothing chosen yet"}
{"do":"wait","valueContains":"Each question needs a choice","role":"label"}
{"do":"click","name":"Summary","role":"check_box"}
{"do":"click","name":"Risks","role":"check_box"}
{"do":"click","name":"Markdown","role":"radio_button"}
{"do":"capture","text":"chat-questions-chosen"}
{"do":"click","contains":"Answer","role":"button"}
{"do":"wait","valueContains":"Here is the report","role":"label","comment":"the same turn, answered"}
{"do":"wait","valueContains":"Sections: Summary, Risks; Format: Markdown","role":"label","comment":"each click landed on its own option"}
{"do":"capture","text":"chat-questions-answered"}
```
