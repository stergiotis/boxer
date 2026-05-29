---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# keelson — the platform spine

`keelson` is the namespace under `src/go/public/keelson/` that gathers the *platform spine* of pebble2impl: the runtime that hosts apps, the data-centrality glue that connects apps to ClickHouse and leeway, and the security model that mediates capability use. It is deliberately distinct from the apps it hosts (`apps/`) and from the ImZero GUI framework (`src/go/public/thestack/imzero2/`, headed for upstream into boxer).

The name is the nautical term for the internal timber running along the inside of a ship's keel, tying the floor frames to it. The metaphor matters: the keelson is structural, *internal*, and invisible to passengers — apps and users see the surface (UIs, CLI), not the spine that distributes load underneath.

## Background

Before this namespace existed, the platform pieces lived alongside the apps and the GUI framework under a single historical prefix (`src/go/public/thestack/`). Three problems followed:

- **No visible boundary.** `ls src/go/public/thestack/` could not tell a reader which subdirectories were platform versus GUI versus app. Newcomers had to read `Manifest.Caps` files or trace imports to figure it out.
- **The GUI framework is leaving.** Adjacent work commits to upstreaming `imzero2` and `fffi2` into boxer. The residue — the platform — needs an identity before that move, not after.
- **Apps had no home.** AppI implementations (`imztop`, `capinspector`, `capdemo`) lived under `thestack/` as siblings of `imzero2/`, indistinguishable from framework code.

[ADR-0035](../../../../doc/adr/0035-keelson-namespace-introduction.md) records the decision to introduce this namespace and the migration that landed it. The repo and the Go module path stay `github.com/stergiotis/pebble2impl` — keelson is a *directory boundary*, not a module boundary.

## How it works

The namespace decomposes into three pillars, each a top-level subdirectory:

### `runtime/` — the Go monolith runtime

The pieces that ferry capability calls between apps and the in-proc bus, persist facts, mediate filesystem access, host windows, and manage the audit/heartbeat/lifecycle ledgers. See [ADR-0026](../../../../doc/adr/0026-app-runtime-and-capability-subjects.md) for the broader runtime contract and [ADR-0036](../../../../doc/adr/0036-runtime-buscodec.md) for the bus codec.

Packages inside: `app` (AppI, Manifest, Registry, LegacyFuncApp); `capbroker`, `persist`, `fsbroker`, `windowhost`, `inprocbus`, `buscodec` (brokers and bus); `factsschema`, `factsstore`, `rowmarshall` (fact schema, persistence, row-based leeway-shred marshal); `logbridge`, `logviewer`, `audit`, `heartbeat`, `runinfo`, `vocab` (observability and provenance).

### `data/` — data-centrality

Runtime-side ClickHouse glue. Lifted out of `runtime/` because these packages serve both runtime *and* app code (an app talks to chclient directly when it needs to issue SQL; the runtime uses chstore to persist facts). Keeping them as siblings of `runtime/` rather than nested under it makes the bidirectional consumption visible.

Packages inside: `chclient` (the SQL client); `chlocalbroker` and `chlocalpool` (the low-latency `clickhouse-local` worker pool — see [ADR-0028](../../../../doc/adr/0028-chlocal-low-latency-sql-cap.md)).

The *leeway* columnar protocol itself currently lives in `src/go/public/boxerstaging/leeway/` pending upstream into boxer; it is consumed by both the runtime fact store and app code, but does not move into `keelson/data/` because it is not pebble2impl's to keep.

### `security/` — capability enforcement

The cap-cross-checker. The runtime owns capability *mediation* (in `runtime/capbroker`); this pillar owns capability *enforcement* — the build-time check that an app's declared `Manifest.Caps` matches its static call graph.

Packages inside: `capslock` (the ADR-0026 §SD10 cross-checker). The `src/go/cmd/capslock-check` binary is a thin shim that imports this package.

## Invariants

- **Repo and module path are stable.** `github.com/stergiotis/pebble2impl/src/go/public/keelson/...` is the prefix for every package in this namespace. The repo name does not change to "keelson"; the Go module path does not change. Anything that depends on this remaining true should not be broken by future renames.
- **`keelson/runtime/` and `keelson/data/` are container directories.** No `.go` files live at those levels — only inside their subdirectories. Tooling that walks Go packages discovers `keelson/runtime/app`, `keelson/runtime/capbroker`, etc., not `keelson/runtime` itself.
- **`Manifest.Id` matches import path.** [ADR-0026](../../../../doc/adr/0026-app-runtime-and-capability-subjects.md) makes `AppIdT` equal to the full Go import path of the app's package. Moving an app changes its identity; this is acknowledged and accepted (the runtime is pre-stable, historical fact rows tagged by old AppId are orphaned by the keelson migration).
- **Apps do not live in `keelson/`.** Standalone AppIs live at `apps/<name>/`. The keelson namespace hosts the platform, not the things hosted on it.

## Trade-offs

- **Subdirectory vs. module rename.** A full `pebble2impl → keelson` rename (repo + `go.mod`) would have communicated the boundary in every import statement, at the cost of touching ~53% of the tree, breaking downstream consumers (boxer go.mod), and committing to a name change before it stabilises. The subdirectory boundary buys discoverability for a fraction of the cost and stays reversible. See [ADR-0035 design space](../../../../doc/adr/0035-keelson-namespace-introduction.md).
- **`data/` as a sibling of `runtime/` vs. nested under it.** The lift is intentional: nesting CH glue under `runtime/` reads "data is a runtime internal," but apps consume `chclient` directly. Sibling pillars make the bidirectional consumption visible. The cost is one extra layer in the path.
- **ImZero stays in `thestack/`.** Disentangling the GUI framework from its host topology before the boxer-upstream move would be churn for no payoff. `thestack/` persists as a shrinking residue until that move completes; keelson lives alongside it, not inside it.

## Further reading

- Decisions: [ADR-0035: keelson namespace](../../../../doc/adr/0035-keelson-namespace-introduction.md), [ADR-0026: app runtime + caps](../../../../doc/adr/0026-app-runtime-and-capability-subjects.md), [ADR-0028: chlocal cap](../../../../doc/adr/0028-chlocal-low-latency-sql-cap.md), [ADR-0036: buscodec](../../../../doc/adr/0036-runtime-buscodec.md).
