---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative.

# Graph forms, and what the size of a thing can mean

Four widgets in this tree draw nodes joined by edges. They were built one at a
time, each against the consumer that needed it, and each records its own
boundary in its own ADR. This page is the theory that sits under all of them:
the axes that decide which form a given dataset wants, and the three
incompatible things the *size* of a drawn element can be claiming.

It deliberately carries no motivation and no snapshot of what is built. Those
belong to whichever ADR is deciding something —
[ADR-0167](../adr/0167-layeredgraph-magnitude.md) is the first to lean on
this page.

## 1 Three axes

**A1 — is the structure declared, or recovered?**

The Sugiyama machinery (cycle removal → layer assignment → crossing
minimisation → coordinate assignment → routing) exists to *recover* rank and
order from a graph that arrives as an unordered edge set. A consumer that
already knows its own series/parallel structure gains nothing from that search
and loses control of the result: a pipeline spine drawn through a
structure-recovery engine bends wherever crossing minimisation prefers, and
straightening it means constraints the engine's seam usually does not expose.
Such a consumer should lay out by recursion over the structure it already has.

The axis is therefore about *what the caller knows*, not about how the picture
looks. Two drawings can be visually similar and sit on opposite sides of it.

**A2 — what does size mean?**

Three answers, and they are not interchangeable. Confusing them is how a
drawing comes to assert something its data does not support.

- **Conserved.** Width or height *is* the value, comparable across the entire
  drawing, because the quantity is preserved from stage to stage. This is the
  strongest claim a diagram can make about size, and it is only honest when the
  data really conserves: every unit entering a node leaves it. It needs a
  global scale and node faces that tile exactly.
- **Ordinal.** Size *orders and emphasises*. It says "this one is bigger than
  that one" and nothing more precise; there is no shared baseline to measure
  against. Most weighted graphs are here, because most quantities on a graph
  are not conserved — a caller's cost is not partitioned without loss among its
  callees, and any cycle destroys conservation outright.
- **Absent.** Size carries nothing; an element is as big as the text inside it.
  This is the honest default, and it is what every drawing does until someone
  decides otherwise.

An ordinal encoding drawn with a conserved-looking presentation is the failure
mode worth naming: stacked faces and a global scale invite the reader to sum
widths, and if the data does not conserve, the sums are wrong.

**A3 — does the drawing claim meaning, or magnitude?**

A *meaning* vocabulary is categorical and semantic: the data says what an
element **is** — an error, a warning, a config channel — and the design system
decides what that looks like. A *magnitude* vocabulary is continuous: the data
says how **much**, and the drawing maps it to a visual quantity.

The two do not substitute for each other. No number of semantic families
expresses a magnitude, and no continuous ramp expresses "this dependency is
forbidden". A drawing that needs both needs both channels.

## 2 Reading a dataset onto the axes

| Form | A1 structure | A2 size | A3 claim |
| --- | --- | --- | --- |
| Wiring / topology diagram | recovered | absent | meaning |
| State machine | recovered | absent | meaning |
| Dependency DAG | recovered | absent | meaning |
| Shell-shaped pipeline schematic | declared | ordinal, optional | meaning |
| Conserved flow | declared | **conserved** | magnitude |
| Stack hierarchy (flamegraph) | declared, as paths | **ordinal** | magnitude |
| **Weighted call graph** | **recovered** | **ordinal** | **magnitude** |

The last row is the one that has no comfortable home, and it is worth being
precise about why its neighbours cannot take it:

- **Not a conserved flow.** A call graph has cycles wherever recursion exists,
  and its weights do not conserve. A conserved-flow renderer is right to reject
  cycles outright, so the mismatch is structural rather than cosmetic.
- **Not a stack hierarchy.** A flamegraph merges by *path*, so one function
  reached from two callers appears twice and recursion unrolls. A call graph
  merges by *function*, so recursion becomes a cycle. They answer different
  questions about the same capture, which is why profiling tools ship both.

So it wants structure recovery (A1) with an ordinal magnitude (A2) presented as
magnitude rather than meaning (A3) — a combination that is unremarkable in
isolation and simply had no consumer until one arrived.

## 3 An ordinal size encoding that stays honest

A widget that maps a number to a stroke width or an element size faces three
judgements. They are recorded here because they are properties of the *form*,
not of any one widget, and the tree has already answered them once.

**The curve should not be linear.** Real quantities on a graph — byte counts,
nanoseconds, call counts — span orders of magnitude. A linear map collapses
every element but the largest to the minimum width as soon as they do, which
destroys exactly the ordering the encoding exists to show. A square root
spreads the small end while keeping the large end ordered, and it does not
pretend to be measurable, which suits an ordinal claim.

**Zero must mean *unknown*, not *none*.** A model that lets a caller omit the
quantity has two absences to distinguish: "there is no flow here" and "I did
not say". Drawing an omitted quantity as the minimum width asserts the first
when the caller meant the second. Declining to override — keeping the default
width — is the reading that adds no claim.

**A drawing with no quantities anywhere must render exactly as before.** This
is what makes the encoding additive rather than a change of behaviour: every
existing caller passes nothing, and the normalisation has no maximum to scale
against, so the mapping declines universally and the picture is unchanged.

**Where the mapping lives** follows from A2. Only the caller knows whether its
numbers conserve, what units they are in, and what range is meaningful. A
widget that normalises internally has decided on the caller's behalf; one that
takes a mapping function, and takes its result in *pixels*, has not. Pixels
rather than layout units matter when the drawing is scaled to fit — a hairline
should stay a hairline.

## 4 Which widget suits which form

| | structure | layout runs | size can mean |
| --- | --- | --- | --- |
| `layeredgraph` ([ADR-0069](../adr/0069-imzero2-layeredgraph-widget.md)) | recovered | host-side, Graphviz `dot` | absent; ordinal on both once [ADR-0167](../adr/0167-layeredgraph-magnitude.md) lands |
| `graph` (egui_graphs) | none assumed | client-side simulation | absent |
| `pipelineview` ([ADR-0119](../adr/0119-imzero2-pipelineview-widget.md)) | declared, a stage tree | host-side recursion | ordinal, on edges |
| `sankey` ([ADR-0159](../adr/0159-imzero2-sankey-flow-widget.md)) | declared, validated acyclic | host-side | conserved |
| `icicle` ([ADR-0160](../adr/0160-imzero2-icicle-flamegraph-widget.md)) | declared, as paths | host-side, plot space | ordinal |

The first column is A1, the third is A2.

Sizing an *edge* and sizing a *node* are not the same difficulty, and the
asymmetry is structural rather than incidental. An edge's width is a property
of how it is stroked: the routing is already decided, so width concerns only
the renderer and cannot move anything. A node's size changes what the layout
must leave room for and route around, so it necessarily reaches back into
layout — which means a weighted drawing and its weightless twin are no longer
the same picture.

That gives node sizing a second question an edge never faces: what happens to
the label. A layout engine that fits boxes to their labels can be asked to
scale the *label* instead of the box, and the fit then follows for free; asking
for a box size directly makes the box the magnitude and leaves the text to
overflow a small one or float in a large one. The first keeps one story about
how a label relates to its box; the second introduces a second.

## References

| Source | What it settles |
| --- | --- |
| Sugiyama, Tagawa & Toda, *Methods for Visual Understanding of Hierarchical System Structures*, IEEE SMC 11(2), 1981 | the layered framework A1 is about, and what "recovered structure" costs |
| Gansner, Koutsofios, North & Vo, *A Technique for Drawing Directed Graphs*, IEEE TSE 19(3), 1993 | the `dot` refinement of it, including why spline routing follows rank assignment |
| Riehmann, Hanfler & Froehlich, *Interactive Sankey Diagrams*, IEEE InfoVis 2005 | the conserved reading, and what a diagram must guarantee to earn it |
| Cleveland & McGill, *Graphical Perception*, JASA 79(387), 1984 | why length and position outrank area and colour saturation, which is why an ordinal encoding on width beats one on fill |
