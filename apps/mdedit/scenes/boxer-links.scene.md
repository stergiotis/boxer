---
type: reference
audience: contributor
status: draft
scene:
  launch: mdedit
  size: 1400x700
  env:
    IMZERO2_DESKTOP_LAYOUT: tile
    BOXER_MDEDIT_DOC: "# Links the host follows\n\n[Count to five](boxer:open/app/play/query@2?sql=SELECT+number+FROM+system.numbers+LIMIT+5)\n\n[A link to no app](boxer:open/app/nosuch)\n"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# mdedit — a boxer: link shows the host's reading on hover and is followed by the host

Every surface that renders markdown claims `boxer:` links and asks the host
what each does (ADR-0409). The hover is the host's reading, not
the link's words: the app the link reaches, the target, who wrote the text
it was found in — a document's author is not known, so external text — and
what a click would do. A link the host would refuse says so before anyone
clicks it. The first link calls play's `query`, a run; nobody's grant
stands behind it, so a click opens a playground with the statement in its
buffer, runs nothing, and the status line says a link put it there. The
trace hovers both links and reads the tooltips, clicks the first, and reads
the new window's status line. It captures the hover only: the playground's
toolbars are wider than half the viewport, which is play's layout and not
this scene's concern.

```jsonl trace
{"do":"wait","role":"multiline_text_input","settleMs":800,"comment":"the window is up in Split; the source pane holds the seeded document"}
{"do":"click","contains":"Read","role":"button","settleMs":800,"comment":"the reading mode: the preview alone"}
{"do":"hover","valueContains":"Count to five","role":"label","settleMs":600,"comment":"the pointer over the first link"}
{"do":"read","valueContains":"a click: puts the work in place","role":"label","on":"value","pattern":"(?s)^(?P<hover>SQL playground — replace the buffer with SQL, run it.*\\na new SQL playground window\\nwith .*SELECT number FROM system.numbers LIMIT 5.*\\neffect: run\\nwritten by: external text\\na click: puts the work in place for you to run)$","comment":"the host's reading, asked for off the render goroutine and shown once it lands: a run nobody's grant stands behind is staged"}
{"do":"capture","text":"hover","sidecars":["tree"]}
{"do":"hover","valueContains":"A link to no app","role":"label","settleMs":600,"comment":"the pointer over the second link"}
{"do":"read","valueContains":"does not open","role":"label","on":"value","pattern":"^(?P<refused>boxer link — does not open: .*no app this host opens has that alias.*)$","comment":"a link the host refuses says so before it is clicked"}
{"do":"click","valueContains":"Count to five","role":"label","pointer":true,"settleMs":1500,"comment":"a press on the first link's words"}
{"do":"read","valueContains":"following the link","role":"label","on":"value","pattern":"^(?P<status>following the link)$","comment":"mdedit asked the host to follow it"}
{"do":"wait","contains":"SQL Playground","role":"window","settleMs":1000,"comment":"the host opened the playground"}
```
