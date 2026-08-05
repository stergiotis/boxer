---
type: adr
status: proposed
date: 2026-08-05
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not
> implement as if accepted.

# ADR-0167: edge magnitude for `layeredgraph` — width and a continuous ramp

## Context

`layeredgraph` ([ADR-0069](./0069-imzero2-layeredgraph-widget.md)) draws
structure-recovered directed graphs, and play's Network panel
([ADR-0129](./0129-play-layered-graph-panel.md)) exposes it to any query that
emits `edges` / `vertices` CTEs. Five surfaces draw through it today —
capinspector's architecture graph, play's system graph, the Network panel,
play's Flow panel, `fsmview` — and **every one of them is a structure graph
that carries no quantity**. That is why the widget has no way to express one.

The [pprof-as-data](../adr-background-work/pprof-profiles-as-data.md) ladder's
`profile-callgraph` book is the first consumer to bring one. A call graph is a
weighted graph, and against `go tool pprof`'s own graphviz rendering of the
same capture the panel is missing the encodings that make a profile scan.
Measured on a 27.3 s CPU profile drawn twice — once by
`go tool pprof -dot | dot`, once by the Network panel running the book verbatim
over the same converted rows:

| pprof encodes | the panel today |
| --- | --- |
| edge width ∝ weight | `Style.EdgeStrokeW` is one global `float32` |
| node box and font ∝ value | `Node` carries no quantity; the engine sizes boxes to the label |
| a continuous heat ramp on fill and stroke | per-element colour hooks exist, but the panel contract offers only six categorical `tone` families |
| dashed = an indirect call | no line-style field at any layer |
| a provenance box naming what pruning dropped | the panel reports only its own 400/1000 ceiling |

The theory this decision rests on — the three axes, and the three judgements an
ordinal size encoding has to make — is
[graph-forms-and-magnitude](../adr-background-work/graph-forms-and-magnitude.md).
Two of its conclusions are load-bearing here. A call graph is **ordinal, not
conserved**: it has cycles wherever recursion exists, and its weights are not
partitioned without loss among callees. And the panel's `tone` vocabulary is a
*meaning* channel by design — no number of semantic families expresses a
magnitude, so this is an added channel rather than a re-use of that one.

**The prior art that shortens the work.** `pipelineview`
([ADR-0119](./0119-imzero2-pipelineview-widget.md)) already solved the edge half
for its own idiom. Its `view.RenderOpts` carries four per-element hooks —
`NodeFill`, `NodeText`, `EdgeStroke`, `EdgeWidth(from, to, volume)` — and
`layeredgraph`'s carries **the same first three and not the fourth**. Its
`VolumeWidth` mapping already answers the three judgements, including the
square-root curve, and its own comment gives the reason this ADR inherits:
*"this is a schematic, not a Sankey … width here orders and emphasises — it is
not something to read a quantity off."* This ADR does not re-derive any of it.

Node sizing is **not** in this cut; §SD6 records why and what it would take.

## Design space (QOC)

**Question.** How should a quantity attached to an edge reach the drawing?

| Criterion | Why it matters |
| --- | --- |
| C1 — five existing surfaces render byte-identically | none of them passes a quantity, and none should change |
| C2 — the claim stays ordinal | the data does not conserve; the drawing must not imply it does |
| C3 — the caller keeps the mapping | only the caller knows units, range and whether its numbers conserve |
| C4 — consistent with `pipelineview` | one idiom for one concept across two sibling widgets |
| C5 — reaches the Network panel from SQL | the panel is the consumer that motivates it |

**Options.** O1 a quantity on the model plus a width hook, mirroring
`pipelineview`; O2 a richer channel set (size, fill-scale, stroke-scale,
opacity as separate inputs); O3 no widget change — encode the quantity in the
label text.

O2 has no second consumer to justify four channels, and each is another thing
to document, test and keep aligned with the design system; it can be reached
later from O1 without rework. O3 is what the books do today, and it is why the
current picture reads as a wall of equal boxes.

## Decision

Take O1. A quantity rides on the edge model, the renderer gains one hook to map
it, and the panel gains one column to carry it — plus a continuous colour ramp
over the same number.

### SD1 — The quantity is opaque to the widget, and the mapping is the caller's

`layeredgraph.Edge` gains `Weight float64`, documented as an **ordinal**
quantity: it orders and emphasises, and nothing in the widget treats it as
conserved or comparable across drawings. The layout passes it through untouched
— it does not influence rank assignment, ordering or routing, so a weighted
graph and its unweighted twin lay out identically.

`view.RenderOpts` gains `EdgeWidth(from, to string, weight float64) (float32, bool)`,
matching `pipelineview`'s signature exactly. Widths are **pixels**, like
`Style.EdgeStrokeW` and unlike `Style.Rounding`, so a hairline survives
fit-to-view scaling.

*Kill clause:* if a caller ever needs the weight to change the layout — heavier
edges shorter, or ranked first — this decision is wrong and the quantity has to
enter the DOT emission instead.

### SD2 — One ready-made mapping, with `pipelineview`'s three judgements

`view.WeightWidth(lay, minW, maxW)` mirrors `pipelineview.VolumeWidth`:
square-root curve over the layout's own maximum; a non-positive weight
*declines* rather than drawing a hairline, because zero is **unknown** and not
**none**; and if no edge in the layout carries a weight, the hook declines
universally so the drawing is unchanged. The last is what satisfies C1
structurally rather than by inspection.

The two functions stay separate rather than being lifted into a shared package:
they are eight lines each, and a shared helper would couple two widgets that
otherwise share nothing but an idiom.

*Kill clause:* if a third widget wants the same curve, lift it then — not now.

### SD3 — Colour by weight is a ramp over the existing hooks, not a new hook

`NodeFill` / `NodeText` / `EdgeStroke` already take arbitrary colours per
element, so a continuous ramp needs no widget change at all: the panel samples
a sequential palette ([ADR-0031](./0031-imzero2-design-system-color.md)) and
returns the colour. What this ADR adds is the panel-side convention, not a
seam.

The ramp is sampled over the same normalised weight the width uses, so width
and colour agree; a reader seeing a thick pale edge would otherwise have to
decide which channel to believe. Per Cleveland & McGill, width is the stronger
cue, so colour is redundant encoding here rather than a second variable.

*Kill clause:* if colour is ever asked to carry a *second* quantity, this
agreement breaks and the two channels need separating with a legend.

### SD4 — The panel takes a numeric column beside `tone`, and normalises

The `edges` CTE gains an optional `weight` column (numeric), and `vertices` may
carry one too for the node ramp. `tone` keeps its meaning: an explicit `tone`
wins over the ramp for that element, because a semantic claim is more specific
than a magnitude one.

Normalisation is the panel's, not the book's: the panel sees the whole result
and can take the maximum, where a book would have to compute it in SQL and
re-state it per query. The panel then hands the widget a mapping, keeping C3 —
the *book* still chooses what number to put in the column.

*Kill clause:* a book that needs a fixed scale across two runs — a
before/after comparison — cannot get it from a per-result maximum, and would
need the scale to become explicit.

### SD5 — Absent weight is not a rendering change

A result with no `weight` column produces no weights, so §SD2's hook declines
and the panel installs no ramp. The five existing surfaces pass nothing through
the Go API and are unaffected for the same reason. This is asserted by test,
not by reading (§Verification plan).

### SD6 — Node sizing is deferred, deliberately

Sizing a node by its value is the other half of what pprof does, and it is a
larger job for a structural reason: an edge's width is a property of how it is
stroked and concerns only the renderer, whereas a node's size changes what the
layout must route around, so it reaches back into the DOT emission and the
engine's box sizing. It also has no prior art in the tree — `pipelineview`
sizes its stages to their labels, and `sankey`'s node faces are stacked link
widths, a consequence of conservation rather than a chosen encoding.

Its own decision needs at least: whether the caller supplies a size or a
quantity to be mapped; how Graphviz's `width`/`height`/`fixedsize` interact
with label fitting; and what happens when a big node's label no longer fits its
box, or a small one's overflows.

Also deferred, with reasons:

- **Dashed edges for indirect calls.** "This edge skips elided frames" is a
  *categorical* claim about an edge, so it belongs with `tone` in the meaning
  vocabulary, not with weight in the magnitude one. Cheap once someone decides
  what the vocabulary of edge kinds is.
- **The pruning-provenance line.** The panel cannot see the `LIMIT` in the
  user's SQL, so it cannot say what was dropped. Every applet that caps a
  result has this problem, which makes it a Network-panel-shaped fix at best
  and an applet-wide question at worst.
- **A legend for the ramp.** Redundant while colour agrees with width (§SD3);
  required the moment it does not.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `widgets/layeredgraph` (exported Go API under `public/`) | added — `Edge.Weight float64`, and `EdgeLayout.Weight` carrying it through | additive field on two structs; zero value is today's behaviour |
| `widgets/layeredgraph/view` (exported Go API under `public/`) | added — `RenderOpts.EdgeWidth`, `WeightWidth`, and the two width constants | mirrors `pipelineview/view`'s names exactly |
| play Network panel contract (ADR-0129) | added — optional `weight` on `edges` and on `vertices` | ADR-0129 gains a dated Update pointing here |
| egui2 IDL | **unchanged** | the painter already strokes a polyline at a given width |
| `goccyengine` DOT emission | **unchanged** | §SD1 keeps the weight out of layout |

## Alternatives

- **Extend `sankey` instead (O4).** It validates its input acyclic and claims
  conservation; a call graph is cyclic and does not conserve, so it would be
  rejected at the door and rightly.
- **A shared `magnitude` package for both widgets (O5).** Eight lines of curve
  do not earn a package, and it would couple two widgets that share only an
  idiom.
- **Normalise inside the widget (O6).** Convenient, and it takes the C3
  decision away from the only party that knows what the numbers mean.
- **Reuse `tone` with more families (O7).** A categorical channel cannot carry
  a continuous quantity; adding families makes the vocabulary worse at the job
  it is good at.

## Consequences

### Positive

- The Network panel becomes usable for weighted graphs generally, not just call
  graphs: any query that can compute a per-edge number gets the encoding.
- The two sibling widgets converge rather than diverge — after this,
  `layeredgraph/view` and `pipelineview/view` expose the same four hooks with
  the same signatures, so a reader who knows one knows the other.
- The additive shape means the decision can be reverted by deleting a field and
  a hook; nothing comes to depend on it having happened.

### Negative

- Two near-identical width mappings now exist (§SD2), and a change to the curve
  in one will not propagate to the other. This is a deliberate trade against
  coupling, and it is the kind of duplication that rots quietly.
- A per-result maximum means the same edge can be drawn at two widths in two
  runs (§SD4 kill clause), which is wrong for before/after comparison and is
  not signalled anywhere in the drawing.
- The panel gains a third way to colour an element — `tone`, `group`, and now a
  ramp — and their precedence has to be learned rather than inferred.
- Node sizing stays missing (§SD6), so the strongest difference from the pprof
  reference is only half closed. A reader comparing the two will still see it.

### Neutral

- The weight never reaches Graphviz (§SD1), so layout stays deterministic and
  identical between a weighted graph and its unweighted twin — which also keeps
  the existing layout goldens valid.
- Nothing here needs the IDL, so no regeneration and no Rust rebuild.

## Migration — Tier 1

- **Breaks.** None. Both additions are new fields and a new optional hook; the
  zero value of each is current behaviour.
- **Path.** Nothing to migrate. The five existing surfaces are untouched.
- **Regeneration.** None — no IDL change, so no `egui2gen` run.
- **Old shape.** Not applicable.

## Verification plan — Tier 1

- **Lane.** Default `go test`. `WeightWidth` is pure and is pinned directly:
  widths span `[minW, maxW]` across the weight range, a non-positive weight
  declines, and a layout with no weights declines every edge. The panel's
  claim-resolution tests gain a `weight` column case and a case asserting that
  an explicit `tone` beats the ramp.
- **The C1 assertion.** A test that renders a weightless model and requires the
  hook to be consulted zero times — the property, not a golden image.
- **What would fail.** A curve regression moves the pinned widths; a
  normalisation regression shows as every edge at `maxW`; a break of C1 shows as
  the hook firing on a weightless layout.
- **Gap.** What is actually *painted* rests on the tour capture being looked at,
  as it does for every widget on this lane — the renderer's own drawing is not
  asserted. Whether the square root is the right curve is a judgement, not a
  test: it can only be wrong in the sense of reading badly.

## Status

Proposed 2026-08-05. No code written.

## References

| Source | What it settles |
| --- | --- |
| Cleveland & McGill, *Graphical Perception*, JASA 79(387), 1984 | why width outranks colour saturation, hence §SD3's redundant-encoding choice |
| Gansner, Koutsofios, North & Vo, *A Technique for Drawing Directed Graphs*, IEEE TSE 19(3), 1993 | what `dot` does with edge attributes, and why keeping weight out of layout (§SD1) leaves routing untouched |
| [`go tool pprof`](https://github.com/google/pprof) graph output | the reference rendering the Context measures against: width, node size, heat, dashed-indirect |

### Related ADRs

- [ADR-0069](./0069-imzero2-layeredgraph-widget.md) — the widget this extends;
  gains a dated Update pointing here.
- [ADR-0129](./0129-play-layered-graph-panel.md) — the panel contract §SD4
  extends; gains a dated Update pointing here.
- [ADR-0119](./0119-imzero2-pipelineview-widget.md) — the `EdgeWidth` /
  `VolumeWidth` idiom this ADR mirrors rather than re-derives.
- [ADR-0159](./0159-imzero2-sankey-flow-widget.md) — the conserved-flow widget.
  Its §O5 rejected "extend `layeredgraph`" with *"thick edges, not conserved
  flow"*; that kills `layeredgraph` **as a Sankey**, and does not bear on an
  ordinal claim (§SD1). It is named here so the kill is not later misread as
  covering this decision.
- [ADR-0031](./0031-imzero2-design-system-color.md) — the sequential palettes
  §SD3 samples.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
