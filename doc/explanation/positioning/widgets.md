---
type: explanation
audience: prospective consumers and integrators evaluating adoption
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

> Where this page and an ADR disagree, the ADR is the record. The
> product-level statement is [positioning-statement](../positioning-statement.md);
> this page applies the same template to one subsystem. The set and its
> review checklist are in the [folder README](./README.md).

# widgets — positioning

Positions the widget catalog on top of [imzero2](./imzero2.md): the
scientific and data-display core — a ported plot kernel and the widgets
built on it, distributions, spectra and waveforms, a ported map kernel with
vector fields over it, graph views with a shared camera and canvas — and
the boards, editors and plumbing beside it. The workbench that turns a
query into one of these is positioned by [play](./play.md).

## Short form

For an app author who needs scientific and data displays — plots,
distributions, spectra, maps, flows, graphs, flame graphs — inside a Go app,
widgets is a catalog drawn by Go code[^godrawn] with Go-owned state: a
ported plot kernel with one interaction idiom, a ported map kernel, a shared
camera and hosting protocol for the graph and map views, one colour-map
configuration, and a design system a lint enforces.

Unlike one crate or one JavaScript library per need — a plot crate, a map
crate, a graph crate — each with its own state, interaction model and a
bridge per feature, widgets keeps the state in Go where the data already is.
The cost is ported code that must track its upstreams, tessellation as the
performance ceiling, and finishing touches such as context menus arriving
later.

## Full form

**For** an app author, or the workbench acting for someone at the keyboard,
who needs scientific and data displays — plots with one interaction idiom,
distributions, spectra and waveforms, maps with vector fields, graph views,
flame graphs — inside a Go app, with the widget's state beside the data,

**widgets is** a catalog drawn by Go code[^godrawn] on imzero2: a port of a
plot kernel and the widgets built on it, a port of a map kernel with a
particle layer, graph views sharing one camera and one hosting protocol, and
a design system a lint enforces,

**that** keeps every widget's state in Go, so a widget can be fed from a
query result directly and draws on every host imzero2 supports, and gives
the plot family one interaction idiom instead of a bridge per feature,

**unlike** one crate or one JavaScript library per need, each with its own
state, interaction model and bridge per feature,

**widgets** puts the whole catalog on one drawing path with one owner of
state. The cost is ported kernels that must track their upstreams and carry
their attribution, tessellation as the performance ceiling, and finishing
touches such as context menus arriving later.

## What each clause rests on

| Slot | Clause | Rests on |
| --- | --- | --- |
| For | an app author, or the workbench for an analyst | [why-boxer](../why-boxer.md) P7 ("a widget kit where speed of assembly pays"); [ADR-0097](../../adr/0097-play-reactive-query-graph.md) |
| is a | a ported plot kernel and its reusers | [ADR-0149](../../adr/0149-implot-core-port-painter-lane.md); [ADR-0159](../../adr/0159-imzero2-sankey-flow-widget.md), [ADR-0160](../../adr/0160-imzero2-icicle-flamegraph-widget.md) |
| is a | a ported map kernel; particles over a batched opcode | [ADR-0204](../../adr/0204-leaflet-map-core-port.md), [ADR-0249](../../adr/0249-vector-fields-on-the-map-particles-over-a-batched-segment-opcode.md) |
| is a | shared camera and hosted-canvas protocol | [ADR-0228](../../adr/0228-hosted-canvas-rendering-and-graph-widget-sharing.md), [ADR-0224](../../adr/0224-graphview-go-graph-widget-painter-lane.md) |
| is a | scoped input capture | [ADR-0140](../../adr/0140-imzero2-hover-scoped-wheel-capture.md), [ADR-0177](../../adr/0177-imzero2-focus-scoped-keyboard-capture.md) |
| is a | design system enforced by a lint | [ADR-0029 §SD8](../../adr/0029-imzero2-design-system-and-policy-as-code.md) |
| that | state in Go; fed from a query result; every host | [ADR-0204](../../adr/0204-leaflet-map-core-port.md) ("Rust gains nothing"); [ADR-0250](../../adr/0250-a-sql-backed-vector-field-source-and-plays-vector-field-pane.md); [ADR-0232](../../adr/0232-play-first-class-consumer-columnar-graphview-engine-vocabulary.md) |
| that | one interaction idiom for the plot family | [ADR-0149](../../adr/0149-implot-core-port-painter-lane.md) |
| that | graph and map views share navigation and hosting | [ADR-0225](../../adr/0225-graphview-navigation-layer-and-radial-layout.md), [ADR-0228](../../adr/0228-hosted-canvas-rendering-and-graph-widget-sharing.md) |
| unlike | a plot crate with a seam per feature | [ADR-0149](../../adr/0149-implot-core-port-painter-lane.md) Alternatives (the ecosystem plot crate; a retained-mode Go plot library; DOM chart libraries; cgo to the C++ original, "structurally dead") |
| unlike | a map crate with its own network stack | [ADR-0204](../../adr/0204-leaflet-map-core-port.md) Alternatives (the ecosystem map crate brings an HTTP and TLS stack; browser map libraries an order of magnitude larger) |
| unlike | a graph crate that pins the UI ring | [ADR-0224](../../adr/0224-graphview-go-graph-widget-painter-lane.md) Alternatives; [ADR-0176](../../adr/0176-native-tree-widget.md), [ADR-0119](../../adr/0119-imzero2-pipelineview-widget.md) |
| widgets | one lane, one owner of state | [ADR-0149](../../adr/0149-implot-core-port-painter-lane.md), [ADR-0228](../../adr/0228-hosted-canvas-rendering-and-graph-widget-sharing.md) |
| trade | tracking upstreams and attribution; tessellation ceiling; chrome second; pick latency | [ADR-0149](../../adr/0149-implot-core-port-painter-lane.md) Consequences and §SD8; [ADR-0204](../../adr/0204-leaflet-map-core-port.md); [ADR-0228 §SD2, §SD4](../../adr/0228-hosted-canvas-rendering-and-graph-widget-sharing.md) |

## Boundary

The coherence claim has edges, and they are the honest part of the page:

- **The stateful-widget contract** ([ADR-0013](../../adr/0013-imzero2-stateful-widget-contract.md))
  governs FFI-bound primitives, not the Go painter widgets. There is no
  single stateful contract across the catalog, and this page does not
  claim one.
- **Not every widget rides the plot kernel.** Timeline, spectrum, scrolling
  heat map, gauge, tree map, world map, the diagram family, waveform and
  the time scrubber draw their own way; the timeline's move onto the plot
  frame is recorded as pending in the adoption survey.
- **Boards and editors** — tree map, tree, chat view, card grid — use
  frames and tables rather than Go-drawn geometry.
- **A SQL-backed source inside a widget** exists for the vector field only
  ([ADR-0250](../../adr/0250-a-sql-backed-vector-field-source-and-plays-vector-field-pane.md));
  elsewhere SQL binding is play's panel contract. The columnar declaration
  exists for the graph view only
  ([ADR-0232](../../adr/0232-play-first-class-consumer-columnar-graphview-engine-vocabulary.md)).
- Built under proposed ADRs: the time scrubber
  ([ADR-0251](../../adr/0251-a-time-strip-for-stepped-series.md)) and the
  chat view ([ADR-0239](../../adr/0239-play-chat-panel-and-chatview-widget.md)).
  Pending: retiring the superseded graph crate from the build.
- The design-system rule "no widget library" in [ADR-0029 §SD1](../../adr/0029-imzero2-design-system-and-policy-as-code.md)
  sits in tension with this catalog; the page positions the catalog and
  leaves the rule to that ADR.

## Further reading

- [why-boxer](../why-boxer.md) P7 — the premise this kit enacts; P1 for the ports.
- [positioning-statement](../positioning-statement.md) — the product-level statement.
- [implot-adoption-survey](../implot-adoption-survey.md); [graph-on-a-map](../../howto/graph-on-a-map.md); [vector-field-on-a-map](../../howto/vector-field-on-a-map.md); the [imzero2 skill](../../skills/imzero2/SKILL.md) widget sections.
- Decisions: [ADR-0149](../../adr/0149-implot-core-port-painter-lane.md),
  [ADR-0204](../../adr/0204-leaflet-map-core-port.md),
  [ADR-0224](../../adr/0224-graphview-go-graph-widget-painter-lane.md),
  [ADR-0228](../../adr/0228-hosted-canvas-rendering-and-graph-widget-sharing.md),
  [ADR-0232](../../adr/0232-play-first-class-consumer-columnar-graphview-engine-vocabulary.md),
  [ADR-0249](../../adr/0249-vector-fields-on-the-map-particles-over-a-batched-segment-opcode.md),
  [ADR-0250](../../adr/0250-a-sql-backed-vector-field-source-and-plays-vector-field-pane.md).
- Reference: https://pkg.go.dev/github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot

[^godrawn]: The widget's geometry is computed in Go and sent as drawing commands; the Rust side rasterizes them and keeps no widget state.
