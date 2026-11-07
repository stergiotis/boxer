---
type: reference
audience: contributor
status: draft
scene:
  launch: watchbilldemo
  size: 1000x720
  requires: [clickhouse]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# watchbilldemo — the controls, the queue and the help

The watchbill demo as it opens under a host with a worker: the Enqueue
controls, the queue as the worker lists it, and the task monitor. The
scene waits for the first list to land, reads how many jobs it holds and
asserts the count is a number, captures, and ends with F1 on the demo's
own help. Nothing is enqueued: a job row would outlive the scene in the
cell's store.

```jsonl trace
{"do":"wait","name":"Enqueue job","role":"button"}
{"do":"read","valueContains":"job(s), listed","role":"label","pattern":"(?P<jobs>\\d+) job\\(s\\), listed","settleMs":6000}
{"do":"expect","of":"jobs","min":0}
{"do":"capture","text":"watchbilldemo"}

{"do":"note","text":"--- F1 over the window opens the demo's help ---"}
{"do":"key","text":"F1","settleMs":1200}
{"do":"wait","contains":"Help center","role":"window"}
{"do":"wait","value":"The watchbill demo","role":"label","comment":"the reader shows the demo's overview, not the index"}
{"do":"capture","text":"watchbilldemo-help"}
```
