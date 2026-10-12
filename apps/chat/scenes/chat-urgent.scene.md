---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
  env:
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-urgent.script.jsonl"
    BOXER_CHAT_QUESTIONS: "true"
    BOXER_CHAT_DRAFT: "Draw the wind above 15 m/s."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — an urgent conversation reaches the person while they are Busy

ADR-0323 §SD4 and its update of 2026-10-12, with ADR-0328, on the headless
host, against the scripted model in [chat-urgent.script.jsonl](./chat-urgent.script.jsonl).
After the first answer the person clicks Urgent in the chat window; the
window only asks, and the host opens a card of its own, in which Make
urgent sets the conversation's waiting items to critical. The person then
sets Busy and sends a second message, and the model asks a question. It
would wait while Busy; as the conversation is urgent, its card opens at
once.

```jsonl trace
{"do":"note","text":"ADR-0323 SD4: Urgent, confirmed in host chrome"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Send","role":"button","comment":"the seeded draft"}
{"do":"wait","valueContains":"needs a speed","role":"label"}
{"do":"click","contains":"Urgent","role":"button","comment":"the window asks the host"}
{"do":"wait","name":"Make urgent","role":"button","comment":"the host's card, opened at once: the request came from the active window"}
{"do":"capture","text":"chat-urgent-card"}
{"do":"click","name":"Make urgent","role":"button"}
{"do":"wait","contains":"Urgent ✓","role":"button","comment":"the window reads the host's table again"}
{"do":"click","contains":"nothing waits on you","role":"button"}
{"do":"click","contains":"Busy"}
{"do":"click","name":"Close","role":"button"}
{"do":"wait","contains":"Busy · ","role":"button"}
{"do":"type","role":"multiline_text_input","text":"Go on.","comment":"a second message: the scripted model answers by position"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","contains":"Your turn — critical","role":"window","comment":"the question opens at once, Busy notwithstanding"}
{"do":"capture","text":"chat-urgent-busy"}
```
