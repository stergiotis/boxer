---
type: explanation
audience: end-user
status: draft
title: Go Dependency Explorer
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Go Dependency Explorer

Explores this module's Go dependency graph: every package in the transitive
closure (your packages, their external dependencies, and the standard
library) as nodes, with `import` relations as edges.

## What you see

The window is a three-pane dock you can drag-resize:

- **packages** (master) — one row per package across the whole closure.
  Columns: import path, package name, class (stdlib / internal / external),
  owning module, `.go` file count, out-degree (**Out** = packages it
  imports), and in-degree (**In** = packages that import it). Click a column
  header to sort; click again to reverse.
- **neighborhood** (graph) — the import neighborhood of the *focused* package
  only, not the whole closure (which is far too large to draw). Click a table
  row, a graph node, or a detail-pane entry to set the focus.
- **detail** — the focused package's full path, class, module, directory, file
  count, and degrees, plus its direct **Imports** and **Imported by** lists.
  Every list entry is click-to-focus, so you can walk the graph even when a
  neighborhood is too large to draw.

## Controls

- **Filter** — substring match on the import path.
- **stdlib / internal / external** — toggle which classes the table shows.
- **depth** — how many import hops out from the focus the graph expands.
- **imports ▸ / importers ◂ / both** — which direction the neighborhood
  follows: packages the focus imports, packages that import the focus, or
  both.
- **hide stdlib** — drop standard-library packages from the *graph* (the
  table still lists them). Stdlib hubs like `fmt` and `errors` are imported by
  almost everything, so hiding them keeps the neighborhood legible.
- **engine** — switch the graph between **live** (interactive force/hierarchical
  layout, drag nodes around) and **layered** (a Graphviz-`dot` Sugiyama layout
  computed in-process, with arrow-headed edges and pan/zoom).
- **clear** — in the detail pane, drops the current focus.

## Notes

- The neighborhood graph is capped (currently 200 nodes); a hub package whose
  raw neighborhood is most of the closure shows its closest neighbors and a
  "capped — narrow with …" note. Use depth, direction, or **hide stdlib** to
  bring it into range, or read the complete import/importer lists in the detail
  pane.
- The live engine lays out hierarchically; the collected (production) import
  graph is acyclic, so the levels are well-defined. Test-only imports are
  not collected this iteration.
- The data shown is a one-shot snapshot collected when the window opened,
  via `go/packages`. Collection reflects the active build tags — launch
  through the boxer wrapper so the repo's tags are in effect, or the graph
  will be missing tag-gated packages.
- The collected snapshot is a marshallgen-serializable manifest (ADR-0064);
  a future iteration persists it to `runtime.facts` and reads historical
  snapshots back.
