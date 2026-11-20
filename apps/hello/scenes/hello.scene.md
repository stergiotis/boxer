---
type: reference
audience: contributor
status: draft
scene:
  launch: hello
  size: 900x600
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# hello — the window as it opens, and its help

Hello on the headless host: the name field holds its default, the greeting
reads "Hello, world!", the count line is present, and F1 ends the scene on
the window's own help. The scene does not press **Greet**: a scene shares the
persist store with the person's own windows, and a greeting would change the
count they see.

```jsonl trace
{"do":"note","text":"--- the window as it opens ---"}
{"do":"wait","name":"Greet","role":"button","settleMs":1000}
{"do":"wait","value":"Hello, world!","role":"label"}
{"do":"read","valueContains":"greeted ","role":"label","pattern":"greeted (?P<n>\\d+)×"}
{"do":"capture","text":"hello-opened"}

{"do":"note","text":"--- F1 opens the window's help ---"}
{"do":"key","text":"F1","settleMs":1200}
{"do":"wait","contains":"Help center","role":"window"}
{"do":"wait","value":"Hello","role":"label","comment":"the reader shows the app's overview"}
{"do":"capture","text":"hello-help"}
```
