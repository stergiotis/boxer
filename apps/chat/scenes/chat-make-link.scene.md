---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1600x900
  env:
    BOXER_AGENT_TEST_GRANTS: "true"
    BOXER_AGENT_COORDINATORS: "chat"
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-make-link.script.jsonl"
    BOXER_CHAT_APPS: "true"
    BOXER_CHAT_ARTEFACT: "true"
    BOXER_CHAT_DRAFT: "Give me a link that puts a statement in play."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — a link the model minted runs under its task's grant, and after the task as the person's own click

ADR-0409 on the headless host, against the scripted model in
[chat-make-link.script.jsonl](./chat-make-link.script.jsonl). A test grant
stands in for the person's approval.

- Turn 1: the model asks to open play, mints a link with `make_link` — the
  dispatcher checks `set_sql` in a new play window against the grant and
  runs nothing — and writes the link into the artefact.
- The hover names the task whose grant the link carries. A click opens a
  play window as the task's launch and sets its buffer as the task: the
  window's badge names the task, and the status line carries no link mark.
- Turn 2: the task stops. The same link, clicked again, has no grant behind
  it: it is followed as the person's own click on a link a model wrote — a
  document change in a new window — and play marks the statement it put
  there.

```jsonl trace
{"do":"wait","contains":"Send","role":"button"}
{"do":"click","contains":"Send","role":"button","comment":"turn 1, the seeded draft"}
{"do":"wait","valueContains":"The link is in the artefact.","role":"label","settleMs":1500}
{"do":"wait","valueContains":"Put the answer in play","role":"label","comment":"the link, in the artefact's document"}
{"do":"hover","valueContains":"Put the answer in play","role":"label","settleMs":600}
{"do":"read","valueContains":"under the grant of task-","role":"label","on":"value","pattern":"(?s)^(?P<hover>SQL playground — replace the SQL buffer\\na new SQL playground window\\nwith .*SELECT 42 AS answer.*\\neffect: document\\nwritten by: a model\\nunder the grant of task-.*\\na click: runs under the task's grant, checked again at the click)$","comment":"the host's reading names the grant"}
{"do":"capture","text":"chat-make-link-hover","sidecars":["tree"]}
{"do":"click","valueContains":"Put the answer in play","role":"label","pointer":true,"settleMs":1500,"comment":"the person follows the link"}
{"do":"wait","contains":"SQL Playground","role":"window","settleMs":1000,"comment":"the task's launch"}
{"do":"wait","valueContains":"SELECT 42 AS answer","role":"multiline_text_input","comment":"the buffer the task's set_sql wrote"}
{"do":"wait","contains":"agent · act","role":"button","comment":"the badge: the window is the task's"}
{"do":"focus","name":"Message","role":"multiline_text_input","comment":"the chat's composer"}
{"do":"type","name":"Message","role":"multiline_text_input","text":"Stop the task."}
{"do":"click","contains":"Send","role":"button","comment":"turn 2"}
{"do":"wait","valueContains":"Stopped the task.","role":"label","settleMs":1000}
{"do":"click","valueContains":"Put the answer in play","role":"label","pointer":true,"settleMs":1500,"comment":"the same link, with no grant behind it now"}
{"do":"read","valueContains":"came from a link","role":"label","on":"value","pattern":"^(?P<mark>the statement came from a link a model wrote; read it before you run it)$","comment":"followed as the person's click on a model's link: the statement in a new window, marked"}
{"do":"read","valueContains":"followed as your own click","role":"label","on":"value","pattern":"^(?P<note>SQL playground: set_sql applied \\(the task's grant no longer covers it: followed as your own click\\))$","comment":"the chat's action line says whose click it was"}
```
