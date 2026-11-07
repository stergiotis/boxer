---
type: reference
audience: contributor
status: draft
scene:
  launch: fibscope
  size: 1000x880
  stepSettleMs: 300
  env:
    BOXER_FIBSCOPE_ID: "1729382256910270471"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# fibscope — a seeded word, the invalid case and the advisor

The window opens on the word `BOXER_FIBSCOPE_ID` names — tag value 5, body
7 — rather than on the worked example, so the split the scene reads is one
it chose. The readout is read and its tag value and body asserted; the
**Invalid** and **Example** buttons are pressed by name; the Trade-offs tab
is reached by name and the advisor's pick for the default magnitude is read
and asserted; F1 opens the app's own help.

```jsonl trace
{"do":"note","text":"--- the seeded word splits into tag value 5 and body 7 ---"}
{"do":"read","valueContains":"tag value ","role":"label","pattern":"^tag value (?P<tv>\\d+)$"}
{"do":"expect","of":"tv","eq":5}
{"do":"read","valueContains":" max","role":"label","pattern":"^body (?P<body>\\d+) / "}
{"do":"expect","of":"body","eq":7}
{"do":"wait","value":"Zeckendorf 5 = 5","role":"label"}
{"do":"capture","text":"fibscope-seeded"}

{"do":"note","text":"--- the invalid word, then the worked example ---"}
{"do":"click","name":"Invalid","role":"button"}
{"do":"wait","value":"not a tagged id — 0x000000000000002a carries no comma","role":"label"}
{"do":"capture","text":"fibscope-invalid"}
{"do":"click","name":"Example","role":"button"}
{"do":"wait","value":"tag value 12","role":"label"}

{"do":"note","text":"--- the advisor sizes the tag for a million ids per category ---"}
{"do":"click","name":"Trade-offs","role":"button"}
{"do":"read","valueContains":"use tag width","role":"label","pattern":"use tag width (?P<width>\\d+) bits"}
{"do":"expect","of":"width","eq":44}
{"do":"capture","text":"fibscope-tradeoffs"}
{"do":"click","name":"Explore","role":"button"}

{"do":"note","text":"--- F1 opens the app's own help ---"}
{"do":"key","text":"F1","settleMs":1200}
{"do":"wait","contains":"Help center","role":"window"}
{"do":"wait","value":"Fibonacci-tagged ids","role":"label","comment":"the reader shows the app's overview, not the index"}
{"do":"capture","text":"fibscope-help"}
```
