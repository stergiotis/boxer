---
type: reference
audience: contributor
status: draft
scene:
  launch: splashscreen
  size: 900x700
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# splashscreen — the About tab and the help

The splash window opened on its artwork, then the About tab: identity,
licence and build provenance. The scene reads the version from the
identity line and asserts it is the manifest's, captures the tab, and ends
with F1 on the window's own help, whose page carries the same About
content.

```jsonl trace
{"do":"wait","contains":"Splash","role":"button"}
{"do":"capture","text":"splashscreen"}
{"do":"click","contains":"About","role":"button"}
{"do":"read","valueContains":"version","role":"label","pattern":"version (?P<version>\\d+\\.\\d+\\.\\d+)"}
{"do":"expect","of":"version","is":"0.1.0"}
{"do":"wait","valueContains":"Revision: ","role":"label","comment":"the Build section is up"}
{"do":"capture","text":"splashscreen-about"}

{"do":"note","text":"--- F1 over the window opens the window's help ---"}
{"do":"key","text":"F1","settleMs":1200}
{"do":"wait","contains":"Help center","role":"window"}
{"do":"wait","value":"About boxer","role":"label","comment":"the reader shows the overview, not the index"}
{"do":"capture","text":"splashscreen-help"}
```
