---
type: reference
audience: contributor
status: draft
scene:
  launch: mdedit
  size: 900x500
  env:
    IMZERO2_DESKTOP_LAYOUT: tile
    BOXER_MDEDIT_DOC: "# A link in a paragraph\n\n[Open the notes, a link whose words are the whole paragraph](mdedit://0123456789abcdef/20260101T120000.000000000Z/notes/a.md)\n\nThe paragraph after it is prose, and a click on it opens nothing.\n"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# mdedit — a link inside a paragraph is clicked where its words are

A paragraph whose text carries a link is one label (`labelAtomsLinked`,
ADR-0180's tenth-run update): the host matches the pointer against the
link's glyphs and reports the click as the link's index, and mdedit follows
a link it claims (`mdeditlink.Claimed`). The trace clicks the paragraph at
its centre with the pointer — the paragraph is the link, so the centre is
on its words — and reads the status line mdedit writes when it follows a
link, which proves the click reached the Go side as a link click and not
as a press on prose. The second paragraph is prose; a click on it leaves
the status alone. The link names a snapshot that does not exist, so the
window it opens shows the handed-over text and says the snapshot cannot be
read; that window is not this scene's concern.

```jsonl trace
{"do":"wait","role":"multiline_text_input","settleMs":800,"comment":"the window is up in Split; the source pane holds the seeded document"}
{"do":"click","contains":"Read","role":"button","settleMs":800,"comment":"the reading mode: the preview alone"}
{"do":"click","valueContains":"Open the notes","role":"label","pointer":true,"settleMs":600,"comment":"a pointer press on the paragraph's centre, which is on the link's words"}
{"do":"read","valueContains":"opening a.md","role":"label","on":"value","pattern":"^(?P<status>opening a\\.md in a new window)$","comment":"mdedit followed the link: the status says so"}
{"do":"click","valueContains":"The paragraph after it","role":"label","pointer":true,"settleMs":600,"comment":"a press on prose"}
{"do":"read","valueContains":"opening a.md","role":"label","on":"value","pattern":"^(?P<still>opening a\\.md in a new window)$","comment":"the status is as it was: prose reports no link"}
{"do":"capture","text":"links","sidecars":["svg","tree"]}
```
