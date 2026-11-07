---
type: reference
audience: contributor
status: draft
scene:
  launch: sqlappletcreator
  size: 1000x800
  env:
    BOXER_SQLAPPLETCREATOR_DRAFT: '{"Sql":"SELECT id, display\nFROM keelson(''apps'')\nORDER BY display","Endpoint":"introspection","Slug":"apps-by-name","Title":"Apps by name","Summary":"The registered apps, by display name"}'
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# sqlappletcreator — a seeded draft

The creator opened with no caller: the draft comes from
`BOXER_SQLAPPLETCREATOR_DRAFT`, whose `Sql` and `Endpoint` take the launch
config's path and whose other fields fill the form. The buffer reads
`keelson('apps')`, so the endpoint is the in-process one and the note under
the editor says so. The scene reads the slug back out of its field and
expects the seeded value, captures the form without pressing Save — a save
needs the applet store, and the window's catalog covers it — and ends on F1
over the window, which opens the creator's own help.

```jsonl trace
{"do":"wait","role":"multiline_text_input","settleMs":600,"comment":"the editor holds the seeded buffer"}
{"do":"wait","valueContains":"Endpoint: introspection","role":"label"}
{"do":"read","role":"text_input","nth":0,"pattern":"^(?P<slug>[a-z0-9-]+)$","comment":"the slug field, the first of the four single-line fields"}
{"do":"expect","of":"slug","is":"apps-by-name"}
{"do":"capture","text":"sqlappletcreator-draft"}
{"do":"key","text":"F1","settleMs":1200}
{"do":"wait","contains":"Help center","role":"window"}
{"do":"wait","value":"Authoring a SQL applet","role":"label","comment":"the reader shows the creator's overview, not the index"}
{"do":"capture","text":"sqlappletcreator-help"}
```
