---
type: reference
audience: contributor
status: draft
scene:
  launch: widgets
  size: 1400x1000
  env: {IMZERO2_MARKDOWN_DEMO_PATH: "doc/explanation/positioning/keelson.md"}
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Markdown widget — a footnote reference's hover gloss

ADR-0255's verification plan asks for what no parse test can reach: that
pointing at a footnote reference in a rendered document shows its definition.
The subject is a real consumer, a positioning page that glosses house terms
with footnotes, loaded into the widget gallery's markdown demo through the
demo's seed variable.

The assertion is on the accessibility tree, not the picture. The definition's
text is on the page once already — in the numbered list at the end, which the
tree does not flag hidden even while it sits below the window — so the
tooltip is the *second* label carrying that text, and it exists only while
the pointer is on the reference. Reading it with `nth: 1` fails if no tooltip
opened. Run it with `scripts/dev/scene.sh` and this file's path (ADR-0248).

```jsonl trace
{"do":"note","text":"ADR-0255 — a footnote reference's tooltip carries its definition, asserted through the accessibility tree"}
{"do":"wait","role":"text_input","comment":"the gallery has mounted; its filter box is the only text input while every demo is folded"}
{"do":"focus","role":"text_input"}
{"do":"key","text":"A","modifiers":16,"comment":"select whatever filter a previous run left, so typing replaces it"}
{"do":"type","role":"text_input","text":"markdown","comment":"narrow the gallery to the markdown demo"}
{"do":"wait","contains":"markdown (Obsidian-flavored)","settleMs":400}
{"do":"click","contains":"markdown (Obsidian-flavored)","comment":"expand the demo; its Init has already loaded the seeded file"}
{"do":"wait","valueContains":"loaded doc/explanation/positioning/keelson.md","role":"label","comment":"the seed reached the Load section"}

{"do":"note","text":"--- the first reference in the Short form: [1], the bus ---"}
{"do":"scroll_into_view","value":"[1]","role":"label","nth":0,"settleMs":600,"comment":"the window is shorter than the viewport; a hover is a position, so the marker must be on screen"}
{"do":"hover","value":"[1]","role":"label","nth":0,"settleMs":1500,"comment":"past egui's tooltip delay"}
{"do":"read","valueContains":"The in-process message bus","role":"label","nth":1,"pattern":"^(?P<tip>.+)$","comment":"the tooltip: the second label with the definition's text"}
{"do":"expect","of":"tip","is":"The in-process message bus every app request travels over; a NATS client can stand in for it."}
{"do":"capture","text":"footnote-hover","comment":"the superscript marker in the link tone, and its tooltip"}

{"do":"note","text":"--- the definitions, for a reader without hover: a numbered list after a rule at the end ---"}
{"do":"scroll_into_view","valueContains":"that expose runtime state","role":"label","settleMs":600,"comment":"the last definition, [^introspection]"}
{"do":"wait","value":"4. ","role":"label","comment":"numbered in order of first reference: bus, capability, facts, introspection"}
{"do":"capture","text":"footnote-definitions"}
```
