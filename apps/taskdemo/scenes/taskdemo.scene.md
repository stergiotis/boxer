---
type: reference
audience: contributor
status: draft
scene:
  launch: taskdemo
  size: 1400x900
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# taskdemo — the controls, one short task, and the help

The task primitive's showcase (ADR-0038) on the headless host, which gives
the window a bus and a task supervisor. The scene reads the spawn controls
as the window opens and asserts their defaults — a duration of 5 s, the two
check boxes present, both monitor panes empty — then drags the duration
slider to its minimum of 1 s, starts an items task through its Start button (the `spawn` operation as
the person's gesture), waits for its row to reach the History pane as done,
and ends with F1 on the window's own help.

```jsonl trace
{"do":"note","text":"--- the controls as the window opens ---"}
{"do":"wait","name":"Start items task","role":"button","settleMs":1000}
{"do":"read","value":"5.0","role":"spin_button","pattern":"^(?P<duration>[\\d.]+)$","comment":"the duration slider's value box"}
{"do":"expect","of":"duration","eq":5}
{"do":"wait","name":"Simulate error at 80%","role":"check_box"}
{"do":"wait","name":"Slow Frame (12 ms), which every window waits for","role":"check_box"}
{"do":"wait","value":"(no running tasks)","role":"label"}
{"do":"wait","value":"(no finished tasks yet)","role":"label"}
{"do":"capture","text":"taskdemo-controls"}

{"do":"note","text":"--- a 1 s items task, started by its button, followed to History ---"}
{"do":"drag","name":"duration (s)","role":"slider","x":-200,"y":0,"comment":"anchored on the slider; a drag past its left end clamps the duration to its minimum, 1 s"}
{"do":"read","value":"1.0","role":"spin_button","pattern":"^(?P<short>[\\d.]+)$"}
{"do":"expect","of":"short","eq":1}
{"do":"click","name":"Start items task","role":"button"}
{"do":"wait","valueContains":"kind demo.items","role":"label","comment":"the monitor's row names the task's id and kind"}
{"do":"wait","name":"History (1)","role":"button","settleMs":1500,"comment":"the task ran for a second and ended"}
{"do":"wait","valueContains":"[done] Fake items task · 1.0s","role":"label"}
{"do":"wait","value":"(no running tasks)","role":"label"}
{"do":"capture","text":"taskdemo-finished"}

{"do":"note","text":"--- F1 opens the window's help ---"}
{"do":"key","text":"F1","settleMs":1200}
{"do":"wait","contains":"Help center","role":"window"}
{"do":"wait","value":"The task demo","role":"label","comment":"the reader shows the demo's overview"}
{"do":"capture","text":"taskdemo-help"}
```
