---
type: reference
audience: contributor
status: draft
scene:
  launch: agentconsole
  size: 1400x900
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# agentconsole — no grant yet, and the help

The console as it opens, before anything is asked of the host: no window
has been opened, the grant line reads `no grant`, and the Operation field
holds `get_state`, the read a write starts from. The scene asserts that
state, captures it, and ends with F1 on the console's own help. The grant
and call paths are the other console scenes, which need ClickHouse and a
test grant.

```jsonl trace
{"do":"note","text":"--- the console before a grant ---"}
{"do":"wait","name":"Request grant","role":"button","settleMs":1000}
{"do":"read","value":"no grant","role":"label","pattern":"^(?P<grant>.*)$"}
{"do":"expect","of":"grant","is":"no grant"}
{"do":"wait","value":"get_state","role":"text_input","comment":"the operation field's default"}
{"do":"wait","name":"Open operations demo","role":"button"}
{"do":"capture","text":"agentconsole-no-grant"}

{"do":"note","text":"--- F1 opens the console's help ---"}
{"do":"key","text":"F1","settleMs":1200}
{"do":"wait","contains":"Help center","role":"window"}
{"do":"wait","value":"The agent console","role":"label","comment":"the reader shows the console's overview"}
{"do":"capture","text":"agentconsole-no-grant-help"}
```
