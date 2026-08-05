---
type: adr
status: accepted
date: 2026-08-05
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-05
---

# ADR-0167: magnitude for `layeredgraph` — edge width, node scale, and a continuous ramp

## Context

`layeredgraph` ([ADR-0069](./0069-imzero2-layeredgraph-widget.md)) draws
structure-recovered directed graphs, and play's Network panel
([ADR-0129](./0129-play-layered-graph-panel.md)) exposes it to any query that
emits `edges` / `vertices` CTEs. Five surfaces draw through it today —
capinspector's architecture graph, play's system graph, the Network panel,
play's Flow panel, `fsmview` — and **every one of them is a structure graph
that carries no quantity**. That is why the widget has no way to express one.

The [pprof-as-data](../adr-background-work/pprof-profiles-as-data.md) ladder's
`profile-callgraph` book is the first consumer to bring one. Against
`go tool pprof`'s own graphviz rendering of the same capture — measured on a
27.3 s CPU profile drawn twice, once by `go tool pprof -dot | dot` and once by
the Network panel running the book verbatim over the same converted rows — the
panel is missing the encodings that make a profile scan:

| pprof encodes | the panel today |
| --- | --- |
| edge width ∝ weight | `Style.EdgeStrokeW` is one global `float32` |
| node box and font ∝ value | `Node` carries no quantity; the engine sizes boxes to the label |
| a continuous heat ramp on fill and stroke | per-element colour hooks exist, but the panel contract offers only six categorical `tone` families |
| dashed = an indirect call | no line-style field at any layer |
| a provenance box naming what pruning dropped | the panel reports only its own 400/1000 ceiling |

The theory this rests on is
[graph-forms-and-magnitude](../adr-background-work/graph-forms-and-magnitude.md).
Two of its conclusions are load-bearing. A call graph is **ordinal, not
conserved**: it has cycles wherever recursion exists, and its weights are not
partitioned without loss among callees. And the panel's `tone` vocabulary is a
*meaning* channel by design — no number of semantic families expresses a
magnitude, so this is an added channel rather than a re-use of that one.

**The prior art that shortens the edge half.** `pipelineview`
([ADR-0119](./0119-imzero2-pipelineview-widget.md)) already solved it for its
own idiom. Its `view.RenderOpts` carries four per-element hooks — `NodeFill`,
`NodeText`, `EdgeStroke`, `EdgeWidth(from, to, volume)` — and `layeredgraph`'s
carries **the same first three and not the fourth**. Its `VolumeWidth` mapping
already answers the judgement calls, and its own comment gives the reason this
ADR inherits: *"this is a schematic, not a Sankey … width here orders and
emphasises — it is not something to read a quantity off."*

This ADR decides **both** halves — edges and nodes — because they share one
quantity, one normalisation and one colour ramp, and deciding them apart risks
a node vocabulary that contradicts the edge one. They **ship** apart: §Milestones
stages node scale behind edge width, for reasons of implementation risk rather
than of design.

## Design space (QOC)

**Question.** How should a quantity attached to a node or an edge reach the
drawing?

| Criterion | Why it matters |
| --- | --- |
| C1 — five existing surfaces render byte-identically | none passes a quantity, and none should change |
| C2 — the claim stays ordinal | the data does not conserve; the drawing must not imply it does |
| C3 — the caller keeps the mapping | only the caller knows units, range and whether its numbers conserve |
| C4 — one idiom across sibling widgets | `pipelineview` already spells this concept |
| C5 — a label always fits its box | the existing layout/render font contract must not break |

**Options.** O1 one quantity per element plus per-channel mapping hooks; O2 a
richer channel set (size, fill-scale, stroke-scale, opacity as separate
inputs); O3 no widget change — encode the quantity in label text.

O2 has no second consumer to justify four channels, each another thing to
document and keep aligned with the design system; it is reachable from O1 later
without rework. O3 is what the books do today, and it is why the current
picture reads as a wall of equal boxes.

## Decision

Take O1. One ordinal quantity rides on both element types; the renderer gains
one hook per visual channel; the panel gains one column per element type and
owns normalisation.

### SD1 — One quantity, opaque to layout, on both element types

`layeredgraph.Edge` and `layeredgraph.Node` each gain `Weight float64`,
documented as **ordinal**: it orders and emphasises, and nothing in the widget
treats it as conserved or comparable across drawings.

Edge weight never reaches the DOT emission — it does not influence rank
assignment, ordering or routing, so a weight-carrying graph and its
weightless twin lay out identically and the existing layout goldens stay valid.
Node weight necessarily does reach it (§SD3), which is the asymmetry that
stages the two.

*Kill clause:* if a caller ever needs edge weight to change the layout —
heavier edges shorter, or ranked first — this is wrong and the quantity has to
enter the DOT emission for edges too.

### SD2 — Edge width is a renderer concern, mirroring `pipelineview`

`view.RenderOpts` gains
`EdgeWidth(from, to string, weight float64) (float32, bool)`, matching
`pipelineview`'s signature exactly. Widths are **pixels**, like
`Style.EdgeStrokeW` and unlike `Style.Rounding`, so a hairline survives
fit-to-view scaling.

`view.WeightWidth(lay, minW, maxW)` mirrors `pipelineview.VolumeWidth` and
inherits its three judgements: a **square-root curve** over the layout's own
maximum, because real quantities span orders of magnitude and a linear map
collapses everything but the largest to the minimum; a non-positive weight
**declines** rather than drawing a hairline, because zero is *unknown* and not
*none*; and if no element carries a weight the hook **declines universally**,
which is what satisfies C1 structurally rather than by inspection.

The two mappings stay separate rather than being lifted into a shared package:
they are a few lines each, and a shared helper would couple two widgets that
share nothing but an idiom.

*Kill clause:* if a third widget wants the same curve, lift it then — not now.

### SD3 — Node magnitude scales the font, and the box follows

The engine sizes every box from *label + font + margin*, and reports the font
size it used so the renderer paints labels at the size the boxes were fitted to
— that is the existing no-font-drift contract. Node magnitude extends it rather
than working around it: a weighted node gets a **scaled font size**, set
per-node in the DOT emission (arbitrary attributes are already reachable
per-node), and Graphviz fits the box to the scaled label as it always has.

This is what `go tool pprof` itself does, and it is the reason C5 is satisfiable
at all. The alternative — setting `width`/`height` with `fixedsize` — makes the
box a magnitude and leaves the label to overflow a small one or float in a
large one, which is a second problem to solve for no gain.

The consequence is a seam change: `NodeLayout` carries a per-node `FontSize`,
and the renderer prefers it over `Layout.FontSize`. The global stays as the
fallback for every node that has no weight.

One thing this mechanism does *not* get for free, found by looking at the drawn
result. The engine already pads node boxes past Graphviz's default to absorb
the difference between the metrics it lays out with and the font the painter
draws in — and that error is proportional to the drawn text, so it grows with
the font. A fixed pad calibrated for the layout-wide size is therefore
outgrown by a scaled label, which the painter then clips at the box edge. The
pad scales with the node's own font, relative to the layout-wide size, so an
unweighted node keeps exactly the calibrated value and a weightless graph is
laid out as before.

*Kill clause:* if a caller needs box size decoupled from label size — a fixed
grid of equal boxes with magnitude shown some other way — font scaling is the
wrong mechanism and `fixedsize` returns.

### SD4 — Colour by weight is a ramp over the existing hooks, not a new hook

`NodeFill` / `NodeText` / `EdgeStroke` already take arbitrary colours per
element, so a continuous ramp needs no seam at all: the panel samples a
sequential palette ([ADR-0031](./0031-imzero2-design-system-color.md)) and
returns the colour. What this ADR adds is the convention, not the mechanism.

The ramp is sampled over the same normalised weight the width and scale use, so
the channels agree; a reader seeing a thick pale edge would otherwise have to
decide which to believe. Per Cleveland & McGill, size outranks colour
saturation, so colour is redundant encoding here rather than a second variable.

**The ramp does not use the whole palette.** A sequential palette spans the
full lightness range, so one of its ends sinks into whatever surface the
drawing sits on — and an element carrying a small but *known* weight would then
be harder to see than one carrying none, which inverts the encoding. The ramp
therefore starts at the first palette position whose contrast against the
background reaches the *default stroke's*, so no weighted element is less
visible than an unweighted one. Measured against the dark theme's panel the
default stroke sits at 4.55:1 and the default palette only reaches that around
t≈0.5, so half the ramp is unusable there.

That floor is **derived at render time**, not pinned as a constant, because
both ends of the comparison are theme tokens: under a light theme the palette's
dark end is the visible one and the floor lands elsewhere.
[ADR-0160](./0160-imzero2-icicle-flamegraph-widget.md)'s flame band solves the
same problem with fixed bounds, which it can because it owns its plot surface;
this ramp is drawn on whatever surface the style carries. There is no ceiling:
the far end of the ramp is the most visible colour available, which is what the
heaviest element should be.

*Kill clause:* if colour is ever asked to carry a *second* quantity, the
agreement with size breaks and the channels need separating with a legend.

### SD5 — The panel takes a numeric column per element type, and normalises

`edges` and `vertices` each gain an optional numeric `weight` column. `tone`
keeps its meaning, and an explicit `tone` **wins** over the ramp for that
element: a semantic claim is more specific than a magnitude one.

Normalisation is the panel's, not the book's — the panel sees the whole result
and can take the maximum, where a book would have to compute it in SQL and
re-state it per query. The panel hands the widget a mapping, keeping C3: the
book still chooses what number goes in the column.

*Kill clause:* a book needing a fixed scale across two runs — a before/after
comparison — cannot get it from a per-result maximum, and would need the scale
to become explicit.

### SD6 — Absent weight is not a rendering change

A result with no `weight` column produces no weights, so §SD2's hook declines,
no node font is scaled, and the panel installs no ramp. The five existing
surfaces pass nothing through the Go API and are unaffected for the same
reason. This is asserted by test, not by reading (§Verification plan).

### SD7 — Deferred, deliberately

- **Dashed edges for indirect calls.** "This edge skips elided frames" is a
  *categorical* claim, so it belongs with `tone` in the meaning vocabulary, not
  with weight in the magnitude one. Cheap once someone decides what the
  vocabulary of edge kinds is.
- **The pruning-provenance line.** The panel cannot see the `LIMIT` in the
  user's SQL, so it cannot say what was dropped. Every applet that caps a
  result has this problem, which makes it an applet-wide question rather than a
  Network-panel one.
- **A legend for the ramp.** Redundant while colour agrees with size (§SD4);
  required the moment it does not.
- **Weight-aware layout.** §SD1's kill clause; no consumer wants it.

## Milestones

| # | Deliverable | Why here |
| --- | --- | --- |
| M1 ✓ | `Edge.Weight`, `RenderOpts.EdgeWidth`, `WeightWidth`, the panel's `weight` column on `edges`, and the colour ramp (§SD2, §SD4, §SD5) | touches only the renderer and the panel; no engine change, so the layout goldens cannot move |
| M2 ✓ | `Node.Weight`, per-node font in the DOT emission, `NodeLayout.FontSize`, the renderer preferring it, and `vertices.weight` (§SD3) | changes what Graphviz is asked to lay out, so it can move geometry and wants its own verification pass |

M1 alone closes the strongest of the missing encodings and leaves the picture
coherent; M2 is not a prerequisite for anything in M1. Splitting them is
implementation staging — both are decided here. Both shipped 2026-08-05 (see
§Status); the split still earned its keep, because M2's geometry change wanted
a verification pass M1 did not.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `widgets/layeredgraph` (exported Go API under `public/`) | added — `Edge.Weight` (M1); `Node.Weight` and `NodeLayout.FontSize` (M2) | additive fields; the zero value of each is today's behaviour |
| `widgets/layeredgraph/view` (exported Go API under `public/`) | added — `RenderOpts.EdgeWidth`, `WeightWidth`, two width constants (M1); per-node font preference (M2) | mirrors `pipelineview/view`'s names exactly |
| `goccyengine` DOT emission | **unchanged** in M1; per-node `fontsize` in M2 | M2 only; edge weight never reaches it (§SD1) |
| play Network panel contract (ADR-0129) | added — optional `weight` on `edges` (M1) and on `vertices` (M2) | ADR-0129 gains a dated Update pointing here |
| egui2 IDL | **unchanged** | the painter already strokes a polyline at a width and draws text at a size |

## Alternatives

- **Extend `sankey` instead (O4).** It validates its input acyclic and claims
  conservation; a call graph is cyclic and does not conserve, so it would be
  rejected at the door and rightly.
- **`width`/`height` + `fixedsize` for node magnitude (O5).** Makes the box the
  magnitude and orphans the label — §SD3.
- **A shared `magnitude` package for both widgets (O6).** A few lines of curve
  do not earn a package, and it would couple two widgets that share only an
  idiom.
- **Normalise inside the widget (O7).** Convenient, and it takes the C3
  decision away from the only party that knows what the numbers mean.
- **Reuse `tone` with more families (O8).** A categorical channel cannot carry a
  continuous quantity; adding families makes the vocabulary worse at the job it
  is good at.

## Consequences

### Positive

- The Network panel becomes usable for weighted graphs generally, not just call
  graphs: any query that can compute a per-element number gets the encoding.
- The two sibling widgets converge — after M1, `layeredgraph/view` and
  `pipelineview/view` expose the same four hooks with the same signatures, so a
  reader who knows one knows the other.
- Node magnitude arrives through the *existing* font contract rather than
  beside it (§SD3), so there is one story about how a label relates to its box,
  not two.
- The additive shape means either milestone can be reverted by deleting fields;
  nothing comes to depend on it having happened.

### Negative

- Two near-identical width mappings exist after M1 (§SD2), and a change to the
  curve in one will not propagate to the other. A deliberate trade against
  coupling, and the kind of duplication that rots quietly.
- A per-result maximum means the same element can be drawn at two sizes in two
  runs (§SD5 kill clause) — wrong for before/after comparison, and not
  signalled in the drawing.
- M2 makes geometry depend on the weights, so a layout golden covering a
  weighted graph is no longer stable under a change of weights. Goldens must
  stay weightless or accept re-recording.
- Font scaling has a floor: below roughly the design system's smallest step a
  label stops being legible, so very small nodes will clamp and lose their
  ordering against each other.
- The panel gains a third way to colour an element — `tone`, `group`, and now a
  ramp — whose precedence has to be learned rather than inferred.

### Neutral

- Edge weight never reaches Graphviz (§SD1), so M1 cannot move any existing
  layout.
- Nothing here needs the IDL, so no regeneration and no Rust rebuild.

## Migration — Tier 1

- **Breaks.** None. Every addition is a new field or a new optional hook; the
  zero value of each is current behaviour.
- **Path.** Nothing to migrate. The five existing surfaces are untouched.
- **Regeneration.** None — no IDL change, so no `egui2gen` run.
- **Old shape.** Not applicable.

## Verification plan — Tier 1

- **Lane.** Default `go test`. `WeightWidth` is pure and pinned directly:
  widths span `[minW, maxW]` across the weight range, a non-positive weight
  declines, and a layout with no weights declines every edge. The panel's
  claim-resolution tests gain a `weight` column case and a case asserting an
  explicit `tone` beats the ramp.
- **The C1 assertion.** A test rendering a weightless model and requiring the
  hook to be consulted zero times — the property, not a golden image.
- **M2 additionally.** A weightless model must produce byte-identical DOT to
  today's, so the emission change is provably inert without weights; and a
  weighted model must round-trip its per-node font size into `NodeLayout`.
- **What would fail.** A curve regression moves the pinned widths; a
  normalisation regression shows as every element at maximum; a break of C1
  shows as the hook firing on a weightless layout, or as the DOT diffing.
- **Gap.** What is actually *painted* rests on the tour capture being looked
  at, as for every widget on this lane. Whether the square root is the right
  curve is a judgement, not a test — it can only be wrong in the sense of
  reading badly. And C5 is argued structurally (§SD3), not asserted: nothing
  measures that a scaled label fits its box, because the layout engine is the
  thing that guarantees it.

## Status

Accepted 2026-08-05, with both milestones built and verified the same day.

- **M1** — `Edge.Weight` and `EdgeLayout.Weight` carrying it through the engine
  without reaching the DOT emission; `view.RenderOpts.EdgeWidth` and
  `view.WeightWidth`; the panel's `weight` column on `edges`, its
  normalisation, and the colour ramp; `profile-callgraph` emitting the weight
  it already computed.
- **M2** — `Node.Weight`, `LayoutOpts.NodeFontSize` and `WeightFontSize`, the
  per-node font in the DOT emission, `NodeLayout.FontSize` and the renderer
  preferring it; the panel's `vertices.weight`, the node ramp with its paired
  ink, and node weights entering the layout cache key; `profile-callgraph`
  sizing its functions.

Three details in the shipped code were found by looking at the drawn result
rather than by design, and each is written up where it belongs — the ramp's
legibility floor (§SD4), the ink paired with a ramped node body (§SD4's
consequence, in the panel), and the box pad scaling with the font (§SD3).
That is worth recording as a property of this kind of work: the encodings are
about how a picture *reads*, and no test asserts that.

§SD7's four deferrals are untouched.

## References

| Source | What it settles |
| --- | --- |
| Cleveland & McGill, *Graphical Perception*, JASA 79(387), 1984 | why size outranks colour saturation, hence §SD4's redundant-encoding choice |
| Gansner, Koutsofios, North & Vo, *A Technique for Drawing Directed Graphs*, IEEE TSE 19(3), 1993 | how `dot` sizes nodes from label metrics, which is the mechanism §SD3 leans on |
| [`go tool pprof`](https://github.com/google/pprof) graph output | the reference the Context measures against, and the precedent for scaling the font rather than the box |

### Related ADRs

- [ADR-0069](./0069-imzero2-layeredgraph-widget.md) — the widget this extends;
  gains a dated Update pointing here.
- [ADR-0129](./0129-play-layered-graph-panel.md) — the panel contract §SD5
  extends; gains a dated Update pointing here.
- [ADR-0119](./0119-imzero2-pipelineview-widget.md) — the `EdgeWidth` /
  `VolumeWidth` idiom §SD2 mirrors rather than re-derives.
- [ADR-0159](./0159-imzero2-sankey-flow-widget.md) — the conserved-flow widget.
  Its §O5 rejected "extend `layeredgraph`" with *"thick edges, not conserved
  flow"*; that kills `layeredgraph` **as a Sankey**, and does not bear on an
  ordinal claim (§SD1). Named here so the kill is not later misread as covering
  this decision.
- [ADR-0031](./0031-imzero2-design-system-color.md) — the sequential palettes
  §SD4 samples.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
