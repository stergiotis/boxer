---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
  env:
    BOXER_AGENT_TEST_GRANTS: "true"
    BOXER_AGENT_COORDINATORS: "chat"
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-coordinator-window-tree.script.jsonl"
    BOXER_CHAT_APPS: "true"
    BOXER_CHAT_DRAFT: "What does the demo app show?"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — the coordinator reads a window tree

ADR-0301 against the scripted model in
[chat-coordinator-window-tree.script.jsonl](./chat-coordinator-window-tree.script.jsonl):
the chat opens the operations demo under a test grant and reads its window
tree with `read_window_tree`. The capture goes through the capture service
like a PNG, replayed on the client; the transcript's tool line counts the
messages of the stream the tree holds, and its details show the outline the
model read: its reference, the demo's window by key, and the demo's Clear
button by role and name.

```jsonl trace
{"do":"note","text":"chat: the coordinator reads a window tree"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"I read the demo window's tree","role":"label","settleMs":1500}
{"do":"wait","valueContains":"⚙ Reading the demo's window · read the window tree of windows","role":"label"}
{"do":"click","name":"Details","nth":2,"settleMs":600,"comment":"the read's details hold the outline the model got"}
{"do":"wait","valueContains":"button \"Clear\"","comment":"the demo's Clear button, by role and name"}
{"do":"wait","valueContains":"tree t1","comment":"the reference an anchor cites"}
{"do":"wait","valueContains":"· Window · window ","comment":"the top-level message names its window (ADR-0301)"}
{"do":"capture","text":"chat-coordinator-window-tree","settleMs":800}
```
