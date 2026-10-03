---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
  requires: [clickhouse]
  env:
    BOXER_LLM_ENDPOINT: "http://127.0.0.1:9/v1"
    BOXER_LLM_MODEL: "m"
    BOXER_LLM_TIMEOUT: "5s"
    BOXER_CHAT_DRAFT: "Hello?"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — a failed turn, inspected

ADR-0265 §SD4 against an endpoint on the discard port, which refuses the
connection. The turn fails at once; its bubble says why, and Details shows
the failure's class, the call's id, its time and the error's whole text.
Copy details puts them on the clipboard, and Open call in play opens the
call's row of `keelson('llm_calls')` (ADR-0254, 2026-10-03 update).

```jsonl trace
{"do":"note","text":"chat: a failure's details, copied and opened in play"}
{"do":"wait","valueContains":"Chat with m","role":"label"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"Not answered","role":"label","settleMs":500}
{"do":"click","contains":"Details","comment":"the collapsing header"}
{"do":"wait","valueContains":"call: llm-","role":"label","settleMs":500}
{"do":"capture","text":"chat-failure-details"}
{"do":"click","contains":"Copy details","role":"button"}
{"do":"wait","valueContains":"copied the failure's details","role":"label"}
{"do":"click","contains":"Open call in play","role":"button"}
{"do":"wait","valueContains":"opened the call record in play","role":"label"}
{"do":"wait","valueContains":"1 rows","role":"label","settleMs":1500,"comment":"play reads the call's row"}
{"do":"capture","text":"chat-failure-details-play"}
```
