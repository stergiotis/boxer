---
type: reference
audience: contributor
status: draft
scene:
  launch: splashscreen
  size: 900x700
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# splashscreen — boxer's About page, the Build tab and the help

The splash window opened on boxer's artwork, then boxer's About page: its
copyright, licence and NOTICE. The scene checks the licence line is
boxer's, captures the page, then the Build tab with the running binary's
provenance, and ends with F1 on the window's own help, whose page carries
the same content.

```jsonl trace
{"do":"wait","contains":"Splash","role":"button"}
{"do":"capture","text":"splashscreen"}
{"do":"click","contains":"About","role":"button"}
{"do":"wait","value":"Licensed under the MIT License.","role":"label","comment":"boxer's licence line, on boxer's page"}
{"do":"capture","text":"splashscreen-about"}
{"do":"click","contains":"Build","role":"button"}
{"do":"wait","valueContains":"Revision: ","role":"label","comment":"the Build tab is up"}
{"do":"capture","text":"splashscreen-build"}

{"do":"note","text":"--- F1 over the window opens the window's help ---"}
{"do":"key","text":"F1","settleMs":1200}
{"do":"wait","contains":"Help center","role":"window"}
{"do":"wait","value":"About boxer","role":"label","comment":"the reader shows the overview, not the index"}
{"do":"capture","text":"splashscreen-help"}
```
