---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
  env:
    BOXER_ATTENTION_AWAY_IDLE: "3s"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — Away for idle time, ended by input

ADR-0328 §SD1 on the headless host, with the idle interval at
three seconds. Without input the host sets the person Away and the control
says so; the pointer moving ends it, back to Here, and the host opens the
away log, empty.

```jsonl trace
{"do":"note","text":"ADR-0328 SD1: Away for idle time"}
{"do":"wait","contains":"New conversation","role":"button"}
{"do":"sleep","settleMs":4000,"comment":"no input for longer than the idle interval"}
{"do":"wait","contains":"Away · ","role":"button","comment":"the host set Away"}
{"do":"hover","x":700,"y":500,"comment":"input"}
{"do":"wait","contains":"While you were away","role":"window","comment":"Away ended; the log opens"}
{"do":"wait","value":"Nothing happened.","role":"label"}
{"do":"wait","contains":"nothing waits on you","role":"button"}
{"do":"capture","text":"chat-away-idle"}
```
