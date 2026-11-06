---
type: reference
audience: contributor
status: draft
scene:
  launch: widgets
  size: 1200x800
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# console — the gallery demo, typed into

The widget gallery's `console` demo (ADR-0359): a terminal screen with no
process behind it, which echoes what is typed as a cooked tty would. The
scene types a line into the screen and presses Enter, then reads the
screen back through the accessibility node the console carries (named
`terminal`, its value the visible text) and captures it.

```jsonl trace
{"do":"note","text":"ADR-0359 — the console demo in the widget gallery"}
{"do":"wait","role":"text_input","comment":"the gallery has mounted; its filter box is the only text input while every demo is folded"}
{"do":"focus","role":"text_input"}
{"do":"type","role":"text_input","text":"console"}
{"do":"wait","contains":" console","role":"button","settleMs":400}
{"do":"click","contains":" console","role":"button","comment":"expand the demo"}
{"do":"wait","name":"terminal","settleMs":600}
{"do":"read","name":"terminal","pattern":"(?P<banner>imzero2 console)","comment":"the banner reached the screen"}
{"do":"type","name":"terminal","text":"echo hello"}
{"do":"key","text":"Enter","settleMs":300}
{"do":"read","name":"terminal","pattern":"you typed echo hello \\((?P<typed>\\d+) characters\\)","comment":"the keys went out through Go and the echo came back"}
{"do":"expect","of":"typed","eq":10}
{"do":"capture","text":"console-echo","settleMs":500}
```
