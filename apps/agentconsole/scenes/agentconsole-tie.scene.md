---
type: reference
audience: contributor
status: draft
scene:
  launch: agentconsole
  size: 1400x900
  env:
    BOXER_AGENT_TEST_GRANTS: "true"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# agentconsole — a command meets the person's edit

ADR-0269 §SD4 on the headless host. The console opens the operations demo,
takes a test grant over it, and calls its operations through the host's
dispatcher while the scene types into the same note.

- A command's value shows in the demo's note on the next frame, and the
  person's typing builds on it: the frontend took the new value without an
  override.
- The person's edit lands in the frame's write-back and moves the note's
  revision. Because the task had read the note, the change pauses the task
  in that window: the badge says so, and a write is refused until the
  coordinator's next turn, which lists the change.
- After the turn, a write that still expects the revision the task last saw
  reaches the queue and ends as a conflict; the person's text stands. A
  write after a fresh read applies.

Node ids anchor the text fields: the console is the first window and the demo
the second, and an id is a function of the window and the widget's path.

```jsonl trace
{"do":"note","text":"ADR-0269 M2: a command applies after the frame's write-back; the person wins a tie"}
{"do":"click","name":"Open operations demo","role":"button","settleMs":1500}
{"do":"read","valueContains":"opened window","role":"label","pattern":"opened window (?P<demo>\\d+)"}
{"do":"expect","of":"demo","eq":2}
{"do":"click","name":"Request grant","role":"button"}
{"do":"wait","valueContains":"grant task-","role":"label"}
{"do":"click","name":"Call","role":"button","comment":"k1: get_state with {}, the read the writes below start from"}
{"do":"wait","valueContains":"k1 · get_state · completed","role":"label"}
{"do":"focus","id":3557791720187110425,"comment":"the console's operation field"}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":3557791720187110425,"text":"set_note"}
{"do":"focus","id":13823422873042478622,"comment":"the console's arguments field"}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":13823422873042478622,"text":"{\"text\":\"from the agent\"}"}
{"do":"click","name":"Call","role":"button","comment":"k2: no expectation given, so the dispatcher uses the revision k1 read"}
{"do":"wait","valueContains":"k2 · set_note · rendered","role":"label","settleMs":500}
{"do":"read","id":10593799850196568197,"pattern":"^(?P<agentValue>from the agent)$","comment":"the demo's note took the command's value"}
{"do":"focus","id":10593799850196568197,"comment":"the demo's note"}
{"do":"type","id":10593799850196568197,"text":" and the person","comment":"the person's edit lands in the write-back and moves the note's revision"}
{"do":"read","id":10593799850196568197,"pattern":"(?P<merged>from the agent.*and the person|and the person.*from the agent)","comment":"the edit builds on the command's value"}
{"do":"click","name":"Call","role":"button","comment":"k3: set_note again; the person changed what the task read"}
{"do":"wait","valueContains":"k3 · set_note · refused · paused","role":"label","comment":"the dispatcher pauses the task here until its next turn"}
{"do":"wait","contains":"agent · act · paused","role":"button","comment":"the badge says so"}
{"do":"click","name":"Turn","role":"button","comment":"the coordinator's turn: what others changed, and the pause lifted"}
{"do":"wait","valueContains":"person changed note in window 2","role":"label"}
{"do":"click","name":"Call","role":"button","comment":"k4: set_note without reading again, still expecting the revision k2 left"}
{"do":"wait","valueContains":"k4 · set_note · conflict","role":"label","comment":"the queue's re-check: the person's write-back moved the note"}
{"do":"wait","valueContains":"moved since it was read","role":"label"}
{"do":"read","id":10593799850196568197,"pattern":"(?P<kept>and the person)","comment":"the person's text stands"}
{"do":"capture","text":"agentconsole-tie"}
{"do":"focus","id":3557791720187110425}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":3557791720187110425,"text":"get_state"}
{"do":"focus","id":13823422873042478622}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":13823422873042478622,"text":"{}"}
{"do":"click","name":"Call","role":"button","comment":"k5: read again"}
{"do":"wait","valueContains":"k5 · get_state · completed","role":"label"}
{"do":"focus","id":3557791720187110425}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":3557791720187110425,"text":"set_level"}
{"do":"focus","id":13823422873042478622}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":13823422873042478622,"text":"{\"level\":80}"}
{"do":"click","name":"Call","role":"button","comment":"k6: a write after a fresh read applies"}
{"do":"wait","valueContains":"k6 · set_level · rendered","role":"label"}
{"do":"read","valueContains":"level 80.0","role":"label","pattern":"level (?P<level>[\\d.]+)"}
{"do":"expect","of":"level","eq":80}
```
