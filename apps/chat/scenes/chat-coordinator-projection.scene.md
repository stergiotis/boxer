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
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-coordinator-projection.script.jsonl"
    BOXER_CHAT_APPS: "true"
    BOXER_CHAT_DRAFT: "Cluster a sample of boxer.facts and tell me what sets the clusters apart."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — the coordinator computes and reads play's Projection pane

ADR-0270's update of 2026-10-03 against the scripted model in
[chat-coordinator-projection.script.jsonl](./chat-coordinator-projection.script.jsonl):
the chat opens play under a test grant, runs a leeway-shaped query over the
local `boxer.facts`, reads the Projection pane — a command expects its
resource read first — computes it over the structure feature set, and reads it with `get_projection` and `explain_clusters`. The play
window draws the clustered graph; the chat's transcript lists the calls.
Needs a ClickHouse server on the default endpoint holding `boxer.facts`.

```jsonl trace
{"do":"note","text":"chat: the coordinator computes and reads the Projection pane"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"The projection is computed","role":"label","settleMs":1500}
{"do":"wait","valueContains":"⚙ Clustering the rows · compute_projection in window 2","role":"label"}
{"do":"capture","text":"chat-coordinator-projection-calls","comment":"before the outcome is asserted, so a refusal shows its reason"}
{"do":"wait","valueContains":"⚙ Clustering the rows · compute_projection in window 2 · rendered","role":"label"}
{"do":"wait","valueContains":"⚙ Reading the clusters · get_projection in window 2 · completed","role":"label"}
{"do":"wait","valueContains":"⚙ Reading why the clusters · explain_clusters in window 2","role":"label"}
{"do":"wait","valueContains":"⚙ Reading why the clusters · explain_clusters in window 2 · completed","role":"label"}
{"do":"wait","valueContains":"cluster(s)","role":"label","settleMs":2000,"comment":"the Projection pane's status line: the run landed"}
{"do":"capture","text":"chat-coordinator-projection"}
```
