# Go Dependency Explorer

Explores this module's Go dependency graph: every package in the transitive
closure (your packages, their external dependencies, and the standard
library) as nodes, with `import` relations as edges.

## What you see

- **Table** — one row per package across the whole closure. Columns: import
  path, package name, class (stdlib / internal / external), owning module,
  `.go` file count, out-degree (**Out** = packages it imports), and
  in-degree (**In** = packages that import it). Click a column header to
  sort; click again to reverse.
- **Graph** — the import neighborhood of the *focused* package only, not the
  whole closure (which is far too large to draw). Click a table row, or a
  node in the graph, to set the focus.

## Controls

- **Filter** — substring match on the import path.
- **stdlib / internal / external** — toggle which classes the table shows.
- **depth** — how many import hops out from the focus the graph expands.
- **imports ▸ / importers ◂ / both** — which direction the neighborhood
  follows: packages the focus imports, packages that import the focus, or
  both.
- **clear** — drop the current focus.

## Notes

- The graph is laid out hierarchically; the collected (production) import
  graph is acyclic, so the levels are well-defined. Test-only imports are
  not collected this iteration.
- The data shown is a one-shot snapshot collected when the window opened,
  via `go/packages`. Collection reflects the active build tags — launch
  through the boxer wrapper so the repo's tags are in effect, or the graph
  will be missing tag-gated packages.
- The collected snapshot is a marshallgen-serializable manifest (ADR-0064);
  a future iteration persists it to `runtime.facts` and reads historical
  snapshots back.
