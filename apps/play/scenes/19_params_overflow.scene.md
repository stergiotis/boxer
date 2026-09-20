---
type: reference
audience: contributor
status: draft
scene:
  launch: play
  size: 1920x1200
  stepSettleMs: 350
  env:
    BOXER_PLAY_WINDOW_SIZE: "1888x1100"
    BOXER_PLAY_FOCUS_TABLE: "1"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# params overflow

Parameters — a buffer declaring more slots than the block has room for: the claims scroll under a ceiling instead of pushing the editor off the bottom of the tab. The heading row and the rule below stay put, so Reset and the boundary with the editor never scroll away. The scene does not run the query — the slots come from the parse, which is what draws the widgets

```sql
SELECT {a:String} AS a, {b:String} AS b, {cc:String} AS c, {d:String} AS d,
       {e:String} AS e, {f:String} AS f, {g:String} AS g, {h:String} AS h,
       {i:String} AS i, {j:String} AS j, {k:String} AS k, {l:String} AS l,
       {m:String} AS m, {n:String} AS n, {o:String} AS o, {p:String} AS p
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1500}
{"do":"wait","value":"a : String","role":"label","comment":"the first claim is drawn"}
{"do":"capture","text":"19_params_overflow"}
{"do":"click","x":150,"y":250,"comment":"park the pointer over the claims — a wheel step goes to whatever is under it"}
{"do":"scroll","x":0,"y":-200,"settleMs":600}
{"do":"capture","text":"19_params_overflow_scrolled","comment":"later claims, the heading and the editor where they were"}
```
