---
name: Widgets
abbrev: Wd
synopsis: "plots, maps, graphs, tables, editors and the rest"
level: 2
parent_ids:
  - "[[toolbelt/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - positioning:widgets
  - adr:0267
  - adr:0149
  - adr:0204
  - adr:0151
  - adr:0334
  - pkg:public/thestack/imzero2/egui2/widgets
  - howto:recordtable-grids
  - howto:graph-on-a-map
  - howto:vector-field-on-a-map
  - trial:flow-particles-frame-cost
  - trial:portolan-land-frame-cost
  - skill:imzero2-fetchers
  - scene:08_chart_bars
assessment:
  - dimension: readiness
    rating: 8
    basis: [positioning:widgets, howto:recordtable-grids, trial:flow-particles-frame-cost]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [positioning:widgets, howto:recordtable-grids, trial:flow-particles-frame-cost]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [positioning:widgets, howto:recordtable-grids, trial:flow-particles-frame-cost]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

The parts an interface is made of, each a package with one contract: plots, maps, graph views, flows and treemaps, record tables, the SQL editor, markdown editing, the chat view, gauges and inspectors. The customer is someone building a data app who wants a widget that already knows how to show an Arrow record.

Its competences: [[plots]], [[maps-and-geo]], [[graph-views]], [[flows-and-hierarchies]], [[layered-graphs-and-diagrams]], [[vector-fields]], [[record-tables]], [[tree-widget]], [[sql-editor]], [[markdown-editing]], [[chat-view]], [[timeline-and-time-strip]], [[gauge-spectrum-audio]], [[value-inspectors]], [[process-views]], [[footnote-glosses]].

# Mechanisms

Each widget is a Go package under the three-shape contract, draws through the framed pipeline, and takes its data as Arrow where it shows data; the painter lane carries what a canvas draws.

# Evidence

The positioning page; the how-tos for tables, maps and vector fields; play's panel scenes; the frame-cost trials for particles and portolan.

# Boundaries

Widgets show; they do not fetch. A widget takes a record and a fetcher runs at frame end.
