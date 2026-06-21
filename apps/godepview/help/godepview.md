---
type: explanation
audience: end-user
status: draft
title: Go Dependency Explorer
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Go Dependency Explorer

Explores this module's Go dependency graph — every package in the transitive
closure (your packages, their external dependencies, and the standard library)
as nodes, with `import` relations as edges. The data is a one-shot snapshot
collected when the window opens, via `go/packages` under the active build tags.

## Three views, one switch

The **view** switch at the top chooses what the window is about. Each view fills
the same three-pane dock — a master table on the left, a graph top-right, a
detail pane bottom-right — around a single focus object, so the panes always
agree. Drag the pane borders to resize; the layout persists.

| View | Master | Graph | Detail | About |
|------|--------|-------|--------|-------|
| **Packages** | every package | the focused package's neighbourhood | its imports / importers | one package |
| **Architecture** | groups (subsystems) | the group quotient | violations, cycles, members | one group |
| **Modules** | external modules | the quotient with modules folded in | a module's footprint + witness | one module |

The **filter** box (shared by the package and module tables) does a substring
match; the count beside it is how many rows the active table shows.

## Packages view

The starting view, and the right one for "what does this one package touch?".

- **table** — one row per package across the whole closure: import path, name,
  class, owning module, `.go` file count, **Out** (packages it imports) and
  **In** (packages that import it). Click a header to sort, again to reverse.
- **graph** — the import neighbourhood of the *focused* package only (the whole
  closure is far too large to draw). **depth**, direction (imports ▸ / importers
  ◂ / both), and **hide stdlib** keep it legible; the **engine** toggle switches
  between a live force/hierarchical layout and a Graphviz-`dot` layered one.
- **detail** — the focused package's metadata plus its direct **Imports** and
  **Imported by** lists, every entry click-to-focus.

## Architecture view

Steps back from packages to **groups** — each `apps/<name>`, each `public/<area>`,
each external module, with the standard library collapsed. The group **quotient**
(one node per group; edges weighted by how many imports cross between them) is
small enough to draw whole. Use it to see how subsystems relate, whether sibling
apps stay independent, and where dependency cycles hide. The **group depth**
slider trades detail for overview; **show external** folds the third-party
modules in.

See the how-to guides *Check that keelson apps stay independent* and *Find
dependency cycles and coupling*.

## Modules view

Rolls the external packages up by their owning module to answer third-party
questions: how many of your packages lean on a module (**fan-in**), whether you
depend on it **directly** or only transitively, and its **blast radius** (the
first-party packages a change would reach). Selecting a module and clicking a
blast-radius package traces the **witness path** — the shortest import chain
explaining why your code pulls the module in.

See the how-to guide *Trace why you depend on a module*.

## Concepts

- **Class** — a package is **stdlib** (standard library), **internal** (in this
  module), or **external** (a third-party dependency).
- **Closure** — the full set of packages reachable by `import` from this module,
  on the order of thousands of nodes. The Packages table scales to all of them;
  the graphs stay legible by drawing a bounded neighbourhood (Packages) or the
  small group quotient (Architecture / Modules).
- **Group / quotient** — packages folded by directory prefix (or by module, for
  externals); the quotient is the graph of those groups.
- **Snapshot** — collection runs once when the window opens and reflects the
  active build tags. Launch through the boxer wrapper so the repo's tags are in
  effect, or tag-gated packages are missing. Test-only imports are not collected.
