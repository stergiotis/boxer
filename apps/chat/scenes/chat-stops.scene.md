---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
  requires: [clickhouse]
  env:
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-stops.script.jsonl"
    BOXER_CHAT_QUESTIONS: "true"
    BOXER_CHAT_DRAFT: "Write me the report."
    BOXER_CHAT_STOPS: '[{"name":"before-asking","then":"pause","tool_match":"^ask_user$"},{"name":"time-box","then":"ask","after":"2h"}]'
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — a stop holds the turn, and Continue goes on from where it was

ADR-0370 (proposed) on the headless host, against the scripted model in
[chat-stops.script.jsonl](./chat-stops.script.jsonl). The window is seeded
with two stops of the person's own: one that pauses the turn when the model
calls `ask_user`, and a two-hour time box that stays quiet. The model's
first reply asks for `ask_user`. The stop fires after that reply and before
the call runs: the turn waits on the person with the stop's name and the row
it returned, and the Stops tab lists the firing as waiting. Continue lets the
same turn go on, so the model's question appears next, and its answer ends
the turn. The firing then reads as continued.

Outside the Simple view each stop of the person's own shows its statement in
an SQL editor, laid out over lines and coloured. Before anything is sent the
trace types a comment into the time box's statement: the tab says the edit is
not in force, Apply takes it, and the stop is from then on declared by its
statement.

The checks run in the host's local SQL worker, so the scene needs the engine.
The form of a stop of one's own is not filled in here; the default-lane tests
of `chat_stops_test.go` and `chat_stops_app_test.go` cover construction, a
refused statement and an edit.

```jsonl trace
{"do":"note","text":"ADR-0370: a stop holds the turn"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Stops","role":"button","comment":"the tab, before anything fired"}
{"do":"wait","valueContains":"In force · 2","role":"label"}
{"do":"wait","valueContains":"each tool call of the turn named like ^ask_user$","role":"label"}
{"do":"capture","text":"chat-stops-in-force","sidecars":["tree"]}
{"do":"type","valueContains":"intDiv(working_ms, 7200000)","role":"multiline_text_input","text":"/* two hours */","comment":"an edit in the time box's statement editor; a comment is a statement wherever the caret is"}
{"do":"wait","valueContains":"edited; the statement in force is the one before your edit","role":"label"}
{"do":"capture","text":"chat-stops-edited"}
{"do":"click","name":"Apply","role":"button"}
{"do":"wait","valueContains":"fires on the rows its statement returns","role":"label","comment":"the stop is declared by its statement now"}
{"do":"click","name":"SQL","role":"radio_button","comment":"the form, for a stop declared as a statement"}
{"do":"scroll_into_view","name":"Add stop","role":"button"}
{"do":"capture","text":"chat-stops-form-sql"}
{"do":"click","name":"Time","role":"radio_button"}
{"do":"click","contains":"Send","role":"button","comment":"the seeded draft"}
{"do":"wait","valueContains":"The stop \"before-asking\" fired","role":"label","comment":"held after the reply, before ask_user runs"}
{"do":"wait","valueContains":"waiting for you","role":"label","comment":"the tab lists the firing"}
{"do":"capture","text":"chat-stops-held"}
{"do":"click","name":"Continue","role":"radio_button"}
{"do":"click","contains":"Answer","role":"button"}
{"do":"wait","valueContains":"Which format?","role":"label","comment":"the same turn went on: the model's own question"}
{"do":"wait","valueContains":"you continued","role":"label"}
{"do":"click","name":"Markdown","role":"radio_button"}
{"do":"click","contains":"Answer","role":"button"}
{"do":"wait","valueContains":"Here is the report","role":"label","comment":"answered; the acknowledged row did not fire again"}
{"do":"capture","text":"chat-stops-answered"}
```
