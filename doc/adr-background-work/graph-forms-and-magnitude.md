---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative.

> **Provenance.** Compiled 2026-08-05, ahead of any decision. Claims about this
> repository were read off the working tree on that date and are cited by path;
> the one rendering comparison behind §5 was measured, not recalled (method in
> §5). Nothing here is settled — an ADR, not this page, is where it would
> become so.

# Graph forms in imzero2 — which widget draws what, and the thing none of them can say

## 1 Question and scope

Four widgets in this tree draw nodes joined by edges. They were built one at a
time, each against a consumer that needed it, and the boundaries between them
are recorded in their own ADRs but nowhere together. This page puts them side
by side, names the axes that actually separate them, and identifies a
capability none of them has that a new consumer now needs.

The forcing case is the pprof call graph
([pprof-profiles-as-data](./pprof-profiles-as-data.md) M3), which the
`profile-callgraph` applet book draws through play's Network tab
([ADR-0129](../adr/0129-play-layered-graph-panel.md)). Compared against
`go tool pprof`'s own graphviz output, the panel is missing the encodings that
make a profile *scan*. Whether those belong in `layeredgraph`, and in what
vocabulary, is the decision this page is background for.

In scope: the four widgets' input models and render seams, the graph forms
their consumers actually pass them, and where a magnitude vocabulary would
live. Out of scope: the applet book's own SQL, and the icicle/flamegraph form
([ADR-0160](../adr/0160-imzero2-icicle-flamegraph-widget.md)), which is settled.

## 2 The four widgets

| | `layeredgraph` ([0069](../adr/0069-imzero2-layeredgraph-widget.md)) | `graph` (egui_graphs) | `pipelineview` ([0119](../adr/0119-imzero2-pipelineview-widget.md)) | `sankey` ([0159](../adr/0159-imzero2-sankey-flow-widget.md)) |
| --- | --- | --- | --- | --- |
| Layout | Graphviz `dot` (Sugiyama), host-side in Go | client-side in Rust: random / force-directed / +centre-gravity / hierarchical | recursion over a known stage tree, host-side | stage columns + stacked node faces, host-side |
| Crosses the FFI | positioned geometry | the graph itself; Rust retains positions per widget id | positioned geometry | positioned geometry (implot custom item) |
| Structure | **recovered** from an arbitrary DAG | none assumed | **declared** by the caller | declared, and validated acyclic |
| Node input | `{ID, Label, Shape}` | `(id, label)` + `.Color()` | `{ID, Label, Ports}` | node + its stacked faces |
| Edge input | `{From, To, Label}` | `(from, to)` + `.Color()` + `.Label()` | `{From, To, Label, Volume}` | flow with a conserved value |
| Node magnitude | — | — | — | face height = value |
| Edge magnitude | — | — | **`Volume` → width** | band width = value |
| Per-element colour | `NodeFill`, `NodeText`, `EdgeStroke` | node + edge `.Color()` | `NodeFill`, `NodeText`, `EdgeStroke` | palette + fill modes |
| Line style (dash) | — | — | — | n/a |
| Drawing surfaces, demo aside | **5** | **0** | 1 | 1 |

Surfaces, verified by import: `layeredgraph` is drawn by capinspector's
architecture graph, play's system graph
([ADR-0097](../adr/0097-play-reactive-query-graph.md)), play's Network panel,
play's Flow panel ([ADR-0153](../adr/0153-play-sql-flow-graph-panel.md)), and
`fsmview`. `pipelineview` is drawn by play's Passes tab, and its model is
imported by `sankey/model.go`. Each also has a widget-demo entry, excluded
from the counts; for the `graph` widget (egui_graphs) the demo is the only
call site — see §7.

Two notes on the table. `layeredgraph`'s `Style.EdgeStrokeW` and
`Style.NodeStrokeW` exist but are single global `float32`s, so "—" in the
magnitude rows means *not expressible per element*, not *absent entirely*.
And the header comment of
[`egui2_definition_d_graphs.go`](../../public/thestack/imzero2/egui2/definition/egui2_definition_d_graphs.go)
still states that layout is pinned to `LayoutRandom`; a four-mode `layout`
selector and a `resetLayout` appear later in the same file, so that paragraph
is stale.

## 3 The forms consumers actually pass

| Form | Consumer | Structure | Magnitude | Drawn by |
| --- | --- | --- | --- | --- |
| Capability / architecture wiring | capinspector | small directed | none | layeredgraph |
| Reactive query wiring | play system graph | DAG | none | layeredgraph |
| SQL flow structure | play Flow tab | DAG | none | layeredgraph |
| State machine | `fsmview` | cycles, self-loops | none | layeredgraph |
| Bipartite relation | Network tour scene | two ranks | none | layeredgraph |
| Dependency DAG | godep / topology books | DAG | counts, as *label text* | layeredgraph |
| Shell-shaped pipeline | play Passes tab | declared series/parallel | `Volume`, optional | pipelineview |
| Conserved flow | disk bytes by stage | DAG, acyclic required | conserved | sankey |
| **Weighted call graph** | `profile-callgraph` | directed, **cyclic**, **non-conserved** | value on **nodes and edges** | layeredgraph — with no way to say so |

The observation this table exists to make: **every layeredgraph consumer but
the last is a structure graph.** Not one carries a magnitude; the closest any
gets is putting a count into the label string. The call graph is the first
weighted graph to reach that widget, which is why the vocabulary was never
built — nothing had asked for it.

## 4 The axes that separate them

Three, and they cut across the widgets differently.

**A1 — is the structure declared or recovered?** ADR-0119 states this most
clearly: the Sugiyama machinery exists to *recover* rank and order from an
arbitrary DAG, and a consumer that already knows its series/parallel structure
should lay out by recursion instead. `pipelineview` and `sankey` take declared
structure; `layeredgraph` recovers it; `graph` assumes none. A consumer on the
wrong side of this axis fights its widget — ADR-0119 records that a pipeline
spine drawn through Graphviz bends unless constrained through a seam that does
not expose the constraint.

**A2 — what does size mean?** Three answers are in the tree, and they are not
interchangeable:

- **Conserved** — width *is* the value, comparable across the whole diagram,
  because the quantity is preserved stage to stage. Sankey, and only sankey.
- **Ordinal** — width *orders and emphasises*, and is not something to read a
  quantity off. `pipelineview`'s volume overlay, explicitly (§6).
- **Absent** — size carries nothing; the box is as big as its label.
  Everything else.

**A3 — does the drawing claim meaning or magnitude?** ADR-0129's Network panel
vocabulary is `tone` and `group`: six semantic families where the query says
what a vertex *means* ("a forbidden dependency is not category 4, it is an
error") and the design system says what that looks like. That is a *meaning*
vocabulary. A profile is the opposite case — one kind of thing, wildly
different magnitudes — and no amount of categorical tone expresses it.

Placing the call graph on all three: structure **recovered** (A1 → layeredgraph
is the right widget), magnitude **ordinal** (A2 → not sankey), and the drawing
needs **magnitude** where the panel offers only meaning (A3 → the gap).

## 5 What the comparison actually showed

Method, so the numbers can be re-derived: a 27.3 s CPU profile of the nanopass
`passes_test` run was put through the `pprofarrow` converter, loaded into
ClickHouse, and drawn twice — once by `go tool pprof -dot | dot -Tpng` at
`-nodecount=25`, once by play's Network tab running the `profile-callgraph`
book verbatim at `edge_cap=26` over the same converted rows. Value
conservation was checked across the transfer (both totals 87.33 s).

What pprof encodes that the panel cannot:

1. **Edge width ∝ weight.** The strongest cue in the reference; the hot path is
   visible without reading a number. Not expressible: `Style.EdgeStrokeW` is
   one global value.
2. **Node box and font ∝ value.** Hot functions are physically large. Not
   expressible: `Node` carries no magnitude and the engine sizes boxes to fit
   the label.
3. **Continuous heat colour** on fill, border and stroke. The widget *can*
   already do this — `NodeFill`/`EdgeStroke` are per-element hooks — but the
   panel contract offers only the six categorical tones of A3.
4. **Dashed edges** marking an indirect call (frames elided between caller and
   callee). No line-style field exists at any layer.
5. **A provenance box** — total, and what the pruning dropped
   (*"Dropped 782 nodes, 20 edges; showing top 25 of 192"*). The panel reports
   only its own 400/1000 ceiling; it cannot see the `LIMIT` in the user's SQL.
6. **Flat as well as cumulative** per node, and an `(inline)` edge annotation.
   Both are book-level, not widget-level.

One finding cuts the other way, and it was tested rather than assumed:
**multi-line node labels already work.** Graphviz sizes a taller box for an
embedded newline and the painter renders both lines. With fully-qualified Go
names on one line the boxes reach ~500 px, the graph splits into components
laid side by side, and fit-to-view shrinks the result until nothing is legible;
splitting the package off onto its own line fixes most of that with no code
change. Whatever else is decided, that is a book edit available today.

## 6 The prior art not to re-derive

`pipelineview` has already solved the edge half of this, and its reasoning
transfers almost verbatim. Its
[`view.RenderOpts`](../../public/thestack/imzero2/egui2/widgets/pipelineview/view/view.go)
carries four per-element hooks — `NodeFill`, `NodeText`, `EdgeStroke`, and
`EdgeWidth(from, to, volume) (float32, bool)`. `layeredgraph`'s `RenderOpts`
carries **the same first three and not the fourth**. The two files are
otherwise parallel by construction.

The ready-made mapping is `VolumeWidth(lay, minW, maxW)`, and three of its
judgements are the ones a call graph would otherwise have to re-litigate:

- **A square-root curve, not linear.** Stated in its own comment: *"this is a
  schematic, not a Sankey. Its edges share no baseline and its stages do not
  conserve, so width here orders and emphasises — it is not something to read a
  quantity off."* A linear map collapses every edge but the largest to a
  hairline once volumes span orders of magnitude, which byte counts — and CPU
  nanoseconds — usually do.
- **Zero means unknown, not none.** A non-positive volume *declines* the hook
  and keeps the default width, rather than drawing a hairline that asserts an
  absence the caller never claimed.
- **If nothing carries a volume, the hook declines everything**, so a diagram
  without magnitudes renders exactly as it did before.

That last property is what makes the extension safe here: all five existing
layeredgraph surfaces pass no magnitude, and all five must keep rendering
unchanged.

There is one thing pipelineview does *not* solve. Its stages are sized to their
labels too, so **node magnitude has no prior art anywhere in the tree** except
sankey's stacked faces, which depend on conservation. That half is genuinely
new work and is a larger job than the edge half: edge width touches only the
view, which draws the splines itself, whereas node size reaches into how the
engine sizes boxes and therefore how Graphviz routes around them.

## 7 The `graph` widget question

ADR-0069 kept `graph` (egui_graphs) deliberately: *"sibling, not replacement —
`graph` stays for genuinely force-directed/live use; `layeredgraph` owns static
layered layout."* On the compile date that use has not materialised: the only
`c.Graph(` call sites in the tree are the widget demo's three. That is the same
shape as the snarl binding
([ADR-0021](../adr/0021-imzero2-snarl-node-editor-binding.md), analysed in
[snarl-port-analysis](./snarl-port-analysis.md)).

It is not a candidate for this work regardless: it has the same colour-only
per-element vocabulary, its rendering quality is capped by the crate's stock
shapes (ADR-0069 records labels scaling with zoom and crude arrowheads, and the
crate being effectively frozen), and its hierarchical mode is the rudimentary
one ADR-0069 rejected. Whether it is retired or kept as a demo-only showcase is
a separable decision, worth taking explicitly rather than by drift.

## 8 Where a magnitude vocabulary would live

Three layers, and they can land independently.

**L1 — the widget seam** (`layeredgraph`, ADR-0069). A magnitude on `Node` and
`Edge`, and an `EdgeWidth` hook on `RenderOpts` mirroring pipelineview's. Edge
width needs only the view. Node size needs the engine and is the one piece that
changes what Graphviz is asked to do.

**L2 — the render style.** A line-style field for the dashed/indirect
distinction. Small, and independent of everything else.

**L3 — the panel contract** (ADR-0129). Numeric columns beside the categorical
`tone`, normalised panel-side, plus the pruning-provenance line the panel
cannot currently derive.

Options for the shape of the vocabulary at L1:

- **V1 — one numeric per node and one per edge, normalised by the caller.** The
  pipelineview precedent exactly. Smallest surface; the host decides what its
  numbers mean, which is the property that keeps the widget honest about A2.
- **V2 — a richer channel set** (size, fill-scale, stroke-scale, opacity as
  separate inputs). More expressive, and every channel is another thing to
  document, test and keep consistent with the design system.
- **V3 — leave the widget alone; encode magnitude in the label text.** What the
  books do today. Free, and it is why the current picture reads as a wall of
  equal boxes.

V1 is the one that matches the prior art, and it is what §6's argument
supports; V2's extra channels have no second consumer to justify them yet.

## 9 Open questions

1. **One new ADR, or dated Updates on 0069 and 0129?** Both are *accepted*, so
   neither takes a silent rewrite. A new ADR is the better fit if the decision
   spans both layers and introduces the magnitude-vs-meaning vocabulary; dated
   Updates suffice if the scope is edge width alone.
2. **Does node magnitude ship in the same cut as edge magnitude,** or does it
   wait? They are different sizes of job (§6) and independently landable (§8).
3. **`graph` (egui_graphs): retire, or keep demo-only?** — §7.
4. **Where does normalisation live** — the panel (which sees the whole result
   and can pick min/max) or the book (which knows what the number means)?
   pipelineview puts the mapping host-side and the widget takes pixels.
5. **Does the pruning-provenance line generalise** beyond the profile books, or
   is it `profile-callgraph` chrome? Every applet that caps a result has the
   same honesty problem.
