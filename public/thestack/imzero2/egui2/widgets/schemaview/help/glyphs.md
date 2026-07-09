---
type: reference
audience: end-user
status: draft
title: Reading the schema inspector
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Reading the schema inspector

The schema inspector shows the *shape* of a Leeway table — its columns and how
they are grouped — not the data in it. The left **structure** pane is a
navigator: a flat list of collapsible sections, each headed by a glyph that
names its kind. The right **detail** pane decodes whichever row you select.

One small vocabulary of glyphs carries the structure at a glance. The **?**
button in the navigator header opens the same key inline; this page is the long
form.

## Section glyphs

Every section in the navigator begins with one glyph identifying its kind.

- **◆ — plain item-type section.** A group of *backbone* columns: the plain,
  always-present values that identify and route a row rather than carry
  payload. They are grouped by entity role — id, timestamp, routing, and
  lifecycle — and each holds one value per row, stored inline.
- **◇ — tagged section.** A *payload* group: a named set of value columns
  addressed by tag, which may repeat per row. Most of a schema's substance
  lives in tagged sections.
- **◈ — co-section group.** A tagged section that belongs to a named group of
  sections meant to be read together and sharing one membership axis — for
  example, several facets of one concept split across sibling sections. The
  group key is shown before the section name.

The same glyph heads the detail pane when you select a section or one of its
columns, in the same accent colour, so the two panes echo each other.

## Membership cardinality

A tagged section can carry a **membership spec** — how many members address the
same tag, and whether they are held verbatim or by reference. When present it
shows as a superscript badge after the section name:

- **ˡ — low cardinality.** Few distinct members: a small, bounded set.
- **ʰ — high cardinality.** Many distinct members, typically stored by
  reference rather than inline.
- **ᵐ — mixed.** Low-cardinality verbatim members alongside high-cardinality
  parameters on the same section.

These mark the *spec* — the shape the section accepts — not a count of members
in any one row. The full decode (each cardinality class as its own chip)
appears in the detail pane under **membership**.

## Annotations

- **· — separator.** Reads between a section and its columns or properties in a
  row label; it carries no meaning of its own.
- **·∅ — value-less section.** A tagged section that declares no value columns:
  it carries membership structure only. Its detail pane shows the membership
  spec, use aspects, and groups, but no column list.

## The detail pane

Selecting a column shows its **canonical type** — expand the type inspector's
pop-out for the layout, members, and Go codec — along with its **scope** and
**item type**, and its **encoding hints** and **value semantics** as chips.
Selecting a section, through its *properties* row or directly when the section
is value-less, shows the membership spec, use aspects, and the co-section and
streaming groups it belongs to.
