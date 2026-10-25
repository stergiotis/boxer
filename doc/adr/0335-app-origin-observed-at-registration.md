---
type: adr
status: proposed
date: 2026-10-25
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0335: An app's origin is observed at registration, not declared — package and module from the call stack and the build information

## Context

The launcher ([ADR-0214](./0214-launcher-as-app-summary-detail-recall.md))
lists every app in the process's registry, and the registry does not care
which module an app comes from. A downstream module that imports boxer adds
its own apps by importing their packages: each package's `init()` calls
`RegisterFactory`, and the app sits in the launcher beside boxer's own
([ADR-0026](./0026-app-runtime-and-capability-subjects.md)). Nothing on screen
or in `keelson.apps` says which module contributed a row.

The question comes up when a user wants to know where an app came from:
whose code a window runs, which version, whether it is a local fork. Most
users never ask, so the answer belongs where a reader goes deliberately (the
launcher's detail pane, a SQL query), not on the launcher row.

Go already records the facts needed. A registration runs inside the
registering package's code, so the call stack names that package. The
binary's build information (`runtime/debug.ReadBuildInfo`) lists every
module it was built from, with version, `go.sum` hash and any `replace`.
The env-var registry ([ADR-0009](./0009-environment-variable-registry.md))
already reads the call stack for its `Origin`, though it guesses the module
from the first three path segments.

## Decision

The registry records an `Origin` for every registration, worked out from
what happened rather than declared by the author, and the two read surfaces
show it.

### SD1 — Observed, on the registration, not on the manifest

`app.Origin` sits on `app.Registration` (and `Registry.Origin(id)`), beside
`Singleton` and `OperationsDiagnostic`, the other facts the host observed.
It is not a `Manifest` field: a manifest is what an author claims, and a
claimed origin can be wrong or go stale with nothing to check it against.
`Manifest.Version` stays what it is, the app's own version string, which
can differ from the version of the module it ships in.

### SD2 — The package is the first frame outside the registry

At registration the registry walks the call stack to the first frame whose
package is not `keelson/runtime/app`. Both entry points (the package-level
helpers and the `Registry` methods) therefore resolve to the same caller.
The package path is read from the function symbol. A type-argument list is
cut off before parsing, and the linker's `%2e` escape for a dot in the last
path element is undone.

### SD3 — The module is the longest build-info path containing the package

The package resolves to the longest module path among `Main` and `Deps`
that equals the package path or is a path-segment prefix of it, so a nested
module wins over its parent. Package `main` belongs to the main module. The
record carries the path, the selected version, the `go.sum` hash, the
`replace` target if any, and whether the module is the main one. The build
information is read once per process.

### SD4 — Two read surfaces, both opt-in

- **`keelson.apps`** gains `package`, `module`, `module_version`,
  `module_sum`, `module_replace` and `module_main`. "What did this module
  install" is a `WHERE module = …` or `GROUP BY module` away.
- **The launcher's detail pane** gains a "Provided by" row:
  `module@version`, then `→ replacement` when there is one. The launcher row
  and menus are unchanged.

### SD5 — What the origin does not say

- **It names the code that registered, not the author of the definition.** An
  applet minted from a book is registered by `apps/sqlapplet`. Its origin is
  that package's module, and the book id stays the applet's provenance
  ([ADR-0299](./0299-sql-applets-a-tab-loads-from-its-origin.md)).
- **`(devel)` is not a version.** A module built from a local directory
  reports it: a `go.work` member, a `replace` to a directory, and the main
  module when the build has no VCS stamp (with one, the main module carries a
  pseudo-version derived from the revision). The launcher drops `(devel)` and
  shows the module path alone; the binary's VCS revision in `keelson.build`
  is the nearest stand-in.
- **A wrapper names itself.** Code that registers apps on behalf of another
  package appears as the origin. None was found registering across a module
  boundary when this was written. If one appears, an explicit `RegisterFactoryFrom(origin, …)` is
  the deferred answer, not a deeper stack heuristic.

## Alternatives

- **A declared `Manifest.Module` field.** Rejected: every app author would
  have to fill it in, nothing could check it against the build, and a
  copied manifest would carry another module's name.
- **The three-segment heuristic `config/env` uses.** Rejected for this use:
  it is wrong for vanity import paths (`gopkg.in/…`), nested modules and
  paths with fewer segments. The build information is exact and costs one
  read per process.
- **A launcher grouping or filter by module.** Deferred: it would make
  module a navigation axis for every user, while the need is occasional.
  The `keelson.apps` columns let a launch limit
  ([ADR-0272](./0272-limit-launchable-apps-with-a-sql-predicate.md)) or a
  query select by module already.
- **Showing it in the app center ([ADR-0260](./0260-app-center-one-page-per-app.md)).**
  Deferred, not rejected: the page reads registrations and can add the row
  when someone wants it there.

## Consequences

### Positive

- Every app, native or from another module, can say where it came from, and
  no app author had to change anything.
- A user can see a forked or replaced module in the detail pane rather than
  only by reading `go.mod`.

### Negative

- Each registration walks the stack, at most 32 frames. That is negligible
  for `init()`-time registration and for applets minted at runtime.
- A build without module information (`go run` of a loose file, a stripped
  build-info section) shows only the package.

### Neutral

- The detail pane gains one row, shown only when there is an origin to show.

## Surfaces — Tier 1

- **Exported Go API under `public/`:** `app.Origin` (new),
  `app.Registration.Origin` (new field), `Registry.Origin` (new method). All
  additive.
- **`keelson.apps` schema:** six new columns appended. Queries that select
  columns by name are unaffected.

## Migration — Tier 1

None. Nothing is removed or renamed, and registrations need no change.

## Verification plan — Tier 1

The default `go test` lane covers it in three places:

- `keelson/runtime/app`: symbol parsing, module resolution against a
  synthetic build information (nested module, replace, non-segment prefix,
  `main`, no build information), and a registration made from an external
  test package that must resolve to that package and to boxer's module.
- `keelson/runtime/introspect/providers`: the new columns.
- `keelson/runtime/launcher`: the "Provided by" label.

## Status

Proposed — awaiting review by the code owner. Built alongside the proposal.

## References

- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — the app runtime and its registry.
- [ADR-0214](./0214-launcher-as-app-summary-detail-recall.md) — the launcher's detail pane the row lands in.
- [ADR-0158](./0158-app-classification-topics-keywords-kind.md) — `kind`, the other provenance column on `keelson.apps`.
- [ADR-0009](./0009-environment-variable-registry.md) — the env-var registry's call-stack `Origin`, the precedent.
- [ADR-0272](./0272-limit-launchable-apps-with-a-sql-predicate.md) — the launch limit a `module` predicate can feed.
- [ADR-0260](./0260-app-center-one-page-per-app.md) — the app center, a deferred second surface.
- [ADR-0299](./0299-sql-applets-a-tab-loads-from-its-origin.md) — an applet's own provenance, distinct from its registering code.
