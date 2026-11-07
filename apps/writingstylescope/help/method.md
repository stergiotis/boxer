---
type: explanation
audience: end-user
status: draft
title: Compression distance between sections
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Compression distance between sections

The measurement is normalized compression distance, NCD. For two texts x
and y with compressed lengths C(x), C(y), and C(xy) for their
concatenation,

    NCD(x, y) = (C(xy) − min(C(x), C(y))) / max(C(x), C(y))

The compressor is zstd. A pair that shares wording compresses little
further when concatenated, so C(xy) stays near the larger of C(x) and C(y)
and the distance is near 0; a pair that shares nothing compresses to roughly
the sum and the distance is near 1. It is a distance, not a probability, and
nothing in the window turns it into one. The engine is
`public/analytics/similarity/compression` with its `stylometry` sub-package;
the decision and the alternatives are ADR-0175.

What that measures, and what it does not: NCD rewards byte sequences the
compressor can reuse, so copied or lightly edited text scores low and so
does shared boilerplate; it does not measure meaning, and a passage
rewritten in other words is a different byte sequence to it. The texts are
compared as raw Markdown, not rendered: shared formatting structure is
itself evidence, and stripping it would be a second normalisation step.
YAML front matter is excluded, since the same `type:` / `status:` block
would make any two documents look related.

## Sections

A section is one heading's own text — the span up to the next heading,
whatever its level — so a nesting heading owns only its lead-in and nothing
is counted twice. Text before the first heading is a section of its own,
labelled *(preamble)*; a document with no headings is one section. The
heading line stays in its section's text, since a copied section usually
brings its title with it.

## The length floor and its refusals

Compression has a fixed cost per text — the framing — and below a few
hundred bytes that cost is most of C(x). A distance between two very short
sections therefore measures framing more than content, and its cell would
sit in the same distribution as the real ones as noise. Sections shorter
than the floor are left out of the sweep and counted as dropped; the
default of 200 bytes drops an empty nesting heading and keeps a paragraph.
Lower it to admit short sections, knowing their cells are noisier; raise it
to merge a long document's many short sections out of the sweep when it
refuses with *too many sections*, or to keep a sweep cheap. When no section
of one document reaches the floor the sweep refuses with *nothing to
compare*, and the remedy is the floor or more text. The caps — 1 MiB per
pane, 128 sections per document — bound the sweep at 16,384 cells.

## Why there is no threshold

NCD has no absolute scale that survives a change of subject, language, or
section length. "Below 0.4" would be a statement about a corpus nobody
measured; "closer than all but 0.1% of pairs" is a statement about these two
documents. So the number reported beside every cell is its quantile: the
fraction of all pairs in this matrix at or below it. Each cell's background
is every other cell of the same two documents — the same subject matter, the
same language, the same section lengths. The bulk of the ECDF is what
unrelated looks like here; a shared section is a point detached from that
bulk on the left; a pair sitting inside the bulk is what two independently
written sections on one subject look like.

Two consequences follow. The heatmap's colour range spans the matrix's own
minimum and maximum, so a matrix with no shared section still has a
brightest cell; the quantile, and the gap between the closest and the median
pair on the Distribution tab, say whether that cell is detached or merely
the least of the bulk. And two documents that are copies of each other, or
two documents of one section each, have no bulk for anything to be detached
from.

## The band

The ECDF carries a 95 % simultaneous confidence band from the `ecdf`
widget, and two bands exist. The exact one is Berk-Jones, an O(n²) inversion
in the sample size n = rows × cols — up to 16,384 here — so it is solved once
per n in a background job and cached; until it is ready the plot shows the
closed-form Dvoretzky–Kiefer–Wolfowitz band, which is wider, so the curve is
never held up. The line under the plot names which is on screen: *Shaded:
95% simultaneous Berk-Jones band. It assumes independent observations; these
pairs share sections and are not independent, so read it as a scale
reference.* or *Shaded: the wider closed-form (DKW) band, drawn while the
exact one warms. Same caveat — the pairs are not independent.*

The caveat is the method's assumption, stated where its number is. Both
bands' coverage guarantees hold for independent, identically distributed
observations, and the cells are not: every A section appears in every
column of its row and every B section in every row of its column, so a long
or unusual section moves a whole row or column together. Correcting for
that dependence is out of scope. The band is drawn with the caveat rather
than left off because without it the reader has no sense of scale for how
far from the bulk a point must sit before it is worth attention.

## The document-level readings

The Documents tab adds three figures from the stylometry package's own
measurement modes, with document A's kept sections concatenated as the fixed
reference. They compare the documents as wholes and are blind to a single
copied section by construction:

- **profile NCD** — Profile mode: document A against document B's sections
  concatenated and truncated to A's length, as one number. Length-matched,
  so it answers 'do these two documents resemble each other overall', not
  'is any part of one inside the other'.
- **profile CCC** — Conditional complexity of compression: how many extra
  compressed bytes B costs once A is already known. Unnormalised, so it
  tracks length as well as content — readable here only because profile
  mode matched the lengths first. CCC is C(xy) − C(x); on the matrix it
  would read as a map of section lengths, which is why the matrix carries
  NCD.
- **instance sweep** — Instance mode: document A as a fixed reference, each
  B section streamed past it, with running statistics. It stops early once
  the spread of the statistic stabilises — the count and the converged flag
  say whether it did.
