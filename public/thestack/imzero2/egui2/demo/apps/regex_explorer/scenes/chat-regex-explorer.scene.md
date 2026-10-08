---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
  requires: [clickhouse]
  env:
    BOXER_AGENT_TEST_GRANTS: "true"
    BOXER_AGENT_COORDINATORS: "chat"
    BOXER_LLM_SCRIPT: "public/thestack/imzero2/egui2/demo/apps/regex_explorer/scenes/chat-regex-explorer.script.jsonl"
    BOXER_CHAT_APPS: "true"
    BOXER_CHAT_DRAFT: "What does ClickHouse's extractAll return for a user@host pattern?"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — the coordinator drives the regex explorer

The regex explorer's operations (ADR-0269) on the headless host, called by
the chat as coordinator with the scripted model in
[chat-regex-explorer.script.jsonl](./chat-regex-explorer.script.jsonl). A
test grant stands in for the person's approval. The model opens an
explorer, reads its state, sets a capture-group pattern and a haystack,
brings the ClickHouse functions tab on screen, and reads the matches and
the function results. The explorer asks ClickHouse through its own query
lanes, as it does for a person; the agent only reads what they hold.

```jsonl trace
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"extractAll returns the user names","role":"label","settleMs":1500}
{"do":"wait","valueContains":"⚙ opened regex_explorer as window 2","role":"label"}
{"do":"wait","valueContains":"⚙ Reading the explorer · get_state in window 2 · completed","role":"label"}
{"do":"wait","valueContains":"⚙ set_inputs in window 2 · rendered","role":"label"}
{"do":"wait","valueContains":"⚙ show_tab in window 2 · rendered","role":"label"}
{"do":"wait","valueContains":"⚙ get_matches in window 2 · completed","role":"label"}
{"do":"wait","valueContains":"⚙ get_functions in window 2 · completed","role":"label"}
{"do":"wait","value":"(\\w+)@([\\w.]+)","role":"text_input","comment":"the agent's pattern, in the explorer"}
{"do":"wait","valueContains":"✓ same","role":"label","nth":5,"comment":"the functions tab, on screen and answered"}
{"do":"capture","text":"chat-regex-explorer"}
```
