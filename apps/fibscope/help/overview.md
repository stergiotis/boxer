---
type: explanation
audience: end-user
status: draft
title: Fibonacci-tagged ids
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Fibonacci-tagged ids

Fibscope is for learning the fibonacci-coded tagged-id scheme by
manipulating it. A tagged id packs a category (the tag) and a per-category
counter (the body) into one 64-bit word. Drag the values below and watch the
bits. The app is a front-end over the identity packages: it mints nothing,
reads nothing and holds no capability — everything on screen is a bit
operation over one `uint64`, recomputed every frame. The scheme, the
packages that define it and what each readout means are on the **How a
tagged id is built** page.

An agent sets the id under inspection and reads how it splits through the
window's operations catalog, and another app opens a window on an id of its
own through the launch config; both are on the **Operating fibscope as an
agent** page. The window persists nothing: a plainly opened window starts on
the worked example.

## Explore tab

The id under inspection is one 64-bit word; three controls edit it and every
readout derives from it.

- **tag** — The category. Stored as the fibonacci code of (tag value − 1) in
  the high bits. Small values get short codes — give hot categories small
  tags. Dragging it recomposes the id under the new tag, keeping the body
  where it still fits under the new tag width; the value is clamped to
  1 … 4294967295.
- **body** — The per-category counter, in the low bits. Body 0 is reserved,
  so counting starts at 1. Its ceiling shrinks as the tag gets wider. A
  body too large for the tag is masked to the tag's body region. On a word
  that is not a tagged id the body edit is ignored, since there is no tag to
  put it under.
- **id** — The whole id as one UInt64 — type a decimal or 0x-hex value to
  inspect how it splits, including words that are not valid tagged ids.
  Exact across the full 64-bit range.
- **Example** restores the how-to's worked example — tag value 12, body 1,
  id 12393906174523604993. **Invalid** loads 42, the teaching case below.

The bit strip paints the 64 bits, most significant first, in three runs —
the fibonacci code, the `11` comma (underlined) and the body — with a
legend. The readouts under it, each with its tooltip:

- *tag value* — Decoded back from the bits. 0 means the word carries no
  comma and is not a tagged id.
- *tag width* — Full tag width in bits, including the trailing 11 comma.
  Frequency-adaptive: value 1 costs 2 bits and leaves 62 for the body; the
  largest uint32 tags cost 47.
- *body / max* — The body value, and the largest body this tag can hold:
  2^(64−width) − 1.
- *fibonacci code* — the tag's bits; *Zeckendorf* — the tag value as its
  sum of non-consecutive Fibonacci numbers.
- *same-tag range* — the first and last id that carry this tag; *id* — the
  composed word, decimal and hex.
- *filter* and *expands to* — the ClickHouse filter `LW_ID_HAS_TAG(id, <tag
  value>)` and the SQL it expands to for this tag. The expansion is
  recomputed when the tag changes, not every frame.

### The invalid case

**Invalid** loads the word 42, whose binary has no adjacent `11` pair. The
strip paints it in one neutral run with the legend *no comma — inspecting
raw bits*; the readout says *not a tagged id — 0x000000000000002a carries no
comma*; the SQL line says the decode applies to valid tagged ids only. This
word has no adjacent 11 pair, so it carries no comma and is not a
well-formed tagged id — Split returns zeros rather than garbage. Any word
typed into the id field is kept as typed, valid or not, so its split can be
inspected.

## Trade-offs tab

Pick tag values by the id space each category needs: a narrow tag leaves a
huge body but few categories; a wide tag is the reverse.

- **max ids / category** — a logarithmic slider from 1 to 2^60. How many
  distinct ids you expect in the busiest category. The advisor picks the
  widest tag (the most categories) that still leaves room for this many
  bodies.
- The advisor's line — *use tag width W bits*, the tag values of that width
  and how many categories that is, and the ids each can hold — followed by:
  Give the hottest categories the smallest tag values in that range — they
  compress tightest.
- The plot: body headroom (log2 of the ids per tag) and tag capacity (log2
  of the tag values) against every code width from 2 to 47, with a vertical
  line at the advisor's pick.
- The stats table: Every code width: how many tag values it holds, and the
  largest body under it. Wider tag ⇒ more categories, fewer ids each. The
  *overhead* column is the code width over a plain binary encoding of the
  class's largest tag value. Each rate column is the time to fill one tag's
  body space at that steady ingress rate — max ids ÷ rate, humanized — from
  100 Hz to 10 MHz. The advisor's row is marked and scrolled into view when
  the pick changes.
