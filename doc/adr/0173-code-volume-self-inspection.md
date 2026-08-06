---
type: adr
status: proposed
date: 2026-08-06
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0173: code-volume self-inspection — first-party vs third-party as keelson tables

## Context

How much of what boxer ships is boxer's own code, and how much is other
people's? The tree could not answer that. A one-off measurement session
established both the numbers and that the answer depends entirely on which
lens is used (one dev box, go1.26.5, `linux/amd64`, the repo's `./tags`):

- **Depended-on**, compiled source lines in the package closure: first-party
  396,247 vs third-party 1,263,112 vs stdlib 338,661 — **3.19 : 1**
  third-to-first. Of the first-party figure, 160,317 lines (40.5%) are
  generated, so against hand-written code the ratio is 5.35 : 1.
- **Shipped**, machine-code bytes in the linked `boxer` binary after
  dead-code elimination: first-party 29.5%, third-party 50.8%, stdlib
  17.5% — **1.72 : 1**.
- **Executed**, statements covered in a 58-second headless GUI session:
  **0.84 : 1** — a render loop runs *more* first-party code than
  third-party. The same instrumented binary driven through a CLI batch
  workload (`adr build`, which writes Arrow) inverts it to 2.93 : 1.

Two further facts shape the design. Supply-chain surface and code surface
are not the same quantity: 877 modules appear in `go mod graph`, 288 in
`go.sum`, 229 in `go.mod` — but only 90 contribute a compiled line to the
`boxer` binary. And a large share of third-party "code" is not logic:
`andybalholm/brotli` reports 255,835 code lines of which 18,059 sit inside
function bodies; the rest is a static dictionary.

Much of the machinery already exists.
[`godepcollect`](../../public/code/analysis/golang/godep/godepcollect/collector.go)
loads the package closure with `go/packages` live, lazily and in-process,
caching one collection per process behind a 5 s first-wait budget, and
serves `keelson('go_packages')` with `import_path`, `module_path`,
`num_go_files` and a `class` column that is already
`stdlib | internal | external` — the first/third-party split at the right
grain ([ADR-0064](0064-godepview-go-dependency-explorer.md),
[ADR-0094](0094-keelson-introspection-tables.md)).
[ADR-0169](0169-continuous-coverage-keelson.md) already serves the executed
lens as `coverage_pkgs` / `coverage_funcs`, keyed on `pkg_path`. **The
contrast this ADR wants is largely a join whose columns do not exist yet.**

What is missing is a volume measure, a shipped-code measure, a module
inventory, and a reachability measure — and these have acquisition costs
and prerequisites that differ by orders of magnitude. A deploy target has
no Go toolchain, no source tree and no module cache; `godepcollect` fails
there by design and reports it through `go_collection.status`.

## Design space (QOC)

**Question.** How should the lenses be acquired and shaped, given that they
need different prerequisites and differ in cost by five orders of magnitude?

**Options.**

- **O1 — One collector.** Extend `godepcollect` to compute every lens in
  its existing single cached collection.
- **O2 — Build-time harvest.** Compute everything in a dev/CI step and
  embed a generated table, the `go_package_props` /
  [`proptable`](../../public/packageprops/proptable/proptable.out.go) shape.
- **O3 — Tiered acquisition (chosen).** One provider per prerequisite
  class, each answering what its inputs allow, all joined on the import
  path; each degrades to empty-not-absent independently.

**Criteria.**

- **C1** — Answers on a deploy target (no toolchain, no source).
- **C2** — Query-time cost, and whether one lens can regress another's.
- **C3** — Fidelity to *this* binary rather than to a tree or a past build.
- **C4** — Maintenance surface: new packages, generators, regeneration steps.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | −− | +  | ++ |
| C2 | −  | ++ | +  |
| C3 | +  | −− | ++ |
| C4 | ++ | −  | +  |

O3 wins on the two criteria that decide it. O1 couples every lens to the
toolchain, so the cheapest and most portable facts (the module list, which
costs 10 µs and needs nothing) become unavailable exactly where they matter
most; it also makes the 1.8 s line-counting pass a tax on every
`go_packages` query. O2 is cheapest at query time but describes the build
that generated it, and the committed `proptable` was found 41 packages
stale during this very session — the drift is not hypothetical. O3 costs
one more provider package than O1, which is the price of C1 and C3.

## Decision

Four providers, one per prerequisite class, keyed on the import path.

### SD1 — `go_modules`: the module inventory of the running binary

`runtime/debug.ReadBuildInfo()` — **10 µs, needs nothing** — yields the main
module, every dependency module with version and checksum, any `replace`,
and the build settings. Measured against the `boxer` binary it reports
exactly the **90** modules that contribute compiled packages, agreeing with
the toolchain-derived closure. Columns: `path`, `version`, `sum`,
`replaced_by`, `is_main`. `FreshnessStatic` — immutable for the process.

This is the only lens that is both free and always available, so it is the
floor: on a stripped-down deploy target with no source, `go_modules` still
answers "what third-party code is in this process".

### SD2 — `go_symbols`: what the linker actually kept

`debug/elf` over `os.Executable()` reads the binary's own symbol table —
**31 ms and 38 MB RSS on the 126 MB `boxer` binary**, 122,408 sized
symbols. Text bytes attributed this way agree with `go tool nm` to within
0.01%. No toolchain, no source, no module cache.

Package grain: `pkg_path`, `module_path`, `num_symbols`, `text_bytes`,
`data_bytes`. Text and data are separate columns because conflating them
lets one zero-filled buffer dominate every real package —
`crypto/internal/fips140/drbg.memory` is a 32 MiB data symbol, 42% of the
binary's sized bytes and not code at all.

Symbol-name-to-package attribution is exact when reconciled against
`go_packages` (longest-prefix match over known import paths) and heuristic
otherwise (the segment before the first `.` after the last `/`); the
heuristic over-splits generic instantiations and linker-synthesised
symbols. The provider therefore reconciles when a `go_packages` collection
is available and sets a `attribution` column to `exact` or `heuristic` so
the difference is visible in SQL rather than silent. `FreshnessStatic`.

### SD3 — `go_packages` volume columns: how much source backs it

Added to the existing table: `code_lines`, `comment_lines`, `blank_lines`,
`generated`, `other_lang_lines`. Lines are classified with `go/scanner`
rather than by regex, so `//` inside a string literal is never counted as a
comment; `generated` is the conventional `Code generated … DO NOT EDIT.`
marker, which matters because it separates 160k generated first-party lines
from the ~236k hand-written ones. `other_lang_lines` counts the C/C++/asm
files compiled with a cgo package — 376,887 lines in the repo-wide closure,
invisible to any Go-only count.

**The pass gets its own lazily-populated cache, not `godepcollect`'s.**
Bare `go list` over the closure costs 0.39 s; adding full scanner
classification over ~7,000 files costs **1.8 s more**. Folding that into
the existing collection would take it from ~1.4 s to ~3.2 s against a 5 s
first-wait budget, making every `go_packages` query pay for a column most
queries do not read. Separate caches keep one lens from regressing another,
which is the C2 half of the QOC.

### SD4 — `go_reach`: reachability, out of process

RTA over whole-program SSA from `main` and `init`, package grain:
`funcs_total`, `funcs_reached`, `lines_total`, `lines_reached`. It answers
what SD2 cannot — *which* code could run, at source-line grain rather than
byte grain — and it is the lens vulnerability tooling uses, so it is worth
having despite its cost.

Measured: **7.07 s wall, 49.5 s CPU, 4.15 GB peak RSS.** The wall time is
tolerable; the resident set is not, inside a long-lived GUI process whose
frame budget is 16 ms and whose memory Go's scavenger returns lazily. Worse,
`golang.org/x/tools/go/ssa` is **absent from the imzero2 host closure**
today, so an in-process implementation would add the analysis machinery to
the very binary it measures.

So the computation runs **out of process**, driven by a
[`bgjob.Runner`](../../public/keelson/runtime/bgjob/bgjob.go) over the
[ADR-0038](0038-keelson-background-task-primitive.md) task protocol: the
job is observable, cancellable and progress-reporting, the 4 GB is isolated
in a child that the OS reclaims on exit, and the host binary gains no new
analysis dependency. The child is a `boxer code analysis golang reach`
subcommand emitting JSON on stdout, resolved through the
[external-binary chokepoint](../../public/extbin) rather than assumed to be
`os.Executable()` — the GUI host is a different binary from the CLI and
need not carry the subcommand.

Until a run completes the table is empty and a `status` column reports
`idle | running | ready | failed`, mirroring `go_collection`'s sticky-failure
discipline: a missing or wedged child costs one attempt, not one per query.

### SD5 — Build identity, so disagreement is legible

SD1 and SD2 describe the **binary**; SD3 and SD4 describe the **source tree
the process happens to sit beside**. On a dev box these diverge the moment
the tree is edited after building, and on a deploy target the tree is
absent entirely. `go_modules` therefore carries the `vcs.revision` and
`vcs.modified` build settings, and `go_collection` already carries its own
collection metadata; a join that mixes the two lenses can compare them and
say so. Nothing silently reconciles them.

### SD6 — Viewing

An applet book ([ADR-0132](0132-sqlapplet-sql-defined-applets.md)) over the
four tables plus `coverage_pkgs`: a party-split overview, a module
inventory ranked by shipped bytes, a treemap
([ADR-0166](0166-play-treemap-panel.md)) sized by source lines and coloured
by party, and the four-lens contrast as one row per module. The contrast is
a SQL join, not new render code — which is the point of keying every lens
on the import path.

### SD7 — Persistence deferred

The tables are process-scoped snapshots. "Is the third-party share growing?"
is a history question and needs the [ADR-0169](0169-continuous-coverage-keelson.md)
§SD6 facts tee, still unbuilt. Recorded as the natural next step, not built
here; nothing in M0–M4 depends on it.

### Milestones

- **M0** — `go_modules` (SD1). Smallest, no prerequisites, immediately useful.
- **M1** — `go_symbols` (SD2), heuristic attribution first, reconciliation second.
- **M2** — `go_packages` volume columns + separate cache (SD3).
- **M3** — `go_reach` subcommand, bgjob wiring, table (SD4).
- **M4** — the applet book (SD6).

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `keelson('go_packages')` schema | 5 columns added | `providersgodep` table builder + its query tests; any applet selecting `*` |
| keelson table catalog | 3 tables added (`go_modules`, `go_symbols`, `go_reach`) | `introspecthost` registrations; `keelson('tables')`/`keelson('columns')` output |
| `boxer` CLI verbs | `code analysis golang reach` added | `entry-points.sh` baseline; extbin resolution roster |
| ADR-0009 env registry | one knob (reach child enable/timeout) | `doc/env-vars.md` regeneration |
| `bgjob` / task subjects | new task kind, no protocol change | nothing — the primitive is generic |

Untouched: `FactsStoreI` and the facts schema (SD7 defers persistence),
`./tags`, the coverage tables, the godep manifest DTOs.

## Alternatives

- **RTA in-process in the GUI host.** 4.15 GB resident inside a 16 ms frame
  budget, and it would add `go/ssa` to the host binary it measures.
- **Reach via a build-time harvest into a generated table.** Cheap at query
  time, but describes a past build; the sibling `proptable` was 41 packages
  stale in this session, which is the failure mode in evidence.
- **Per-symbol rather than per-package `go_symbols`.** 122k rows is
  affordable, but every question asked so far is an aggregate; deferred
  rather than rejected.
- **Reusing the ADR-0169 coverage meta blob as the function inventory.** It
  enumerates every instrumented function with a line span and would give
  SD4's grain almost free — but only in a `-cover` build, so it cannot be
  the general answer. Worth revisiting as a fast path when instrumented.
- **A `code_volume_*` table family parallel to `go_packages`.** A second
  grain to keep in sync, and every question becomes a join, for no gain
  over columns on the table that already carries `class` and `module_path`.

## Consequences

### Positive

- The four-lens contrast becomes a SQL join over existing keys, with no new
  render surface, and joins onward to `coverage_*` and `go_imports`.
- The two cheapest lenses need no toolchain and no source, so a deployed
  process can answer "what is in me" — which `godepcollect` alone cannot.
- Dead third-party weight becomes queryable. The measurement that motivated
  this ADR found `sirkon/dst` linked into every binary at 0% reachability;
  that class of finding becomes a `WHERE` clause instead of a bespoke run.

### Negative

- Four acquisition paths with four failure modes, each needing its own
  status surface and its own empty-not-absent test.
- `go_symbols` couples to the ELF layout of Go binaries and to symbol
  naming; a stripped binary (`-ldflags=-s`) yields an empty table. Nothing
  in the tree strips today, but nothing prevents it either.
- SD4 adds a subprocess dependency to a GUI host, with the extbin
  resolution and failure handling that implies.

### Neutral

- Go side only; the Rust render client's own crate volume is out of scope
  and would need a separate collector.
- Numbers are per-binary, not per-repo: the `boxer` CLI and the imzero2
  host have different closures, and each process reports its own.

## Migration — Tier 1

- **Breaks.** Nothing. Three tables are new; the `go_packages` change is
  additive columns.
- **Path.** Nothing to migrate. An applet selecting `*` from `go_packages`
  sees five more columns.
- **Regeneration.** `doc/env-vars.md` for the new knob. No FFI, no codegen.
- **Old shape.** Not applicable.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the providers (row-level tests over
  fixtures, plus the empty-not-absent gate each table needs); the
  `//go:build integration` lane for the two lenses that read real artifacts
  — `go_symbols` against the test binary's own ELF, `go_reach` against a
  small probe module — since both need a real build to exist.
- **What would fail.** `go_modules` row count diverging from
  `go version -m`; `go_symbols` text-byte totals diverging from
  `go tool nm` beyond a tolerance (they agreed to 0.01% when measured);
  a `go_packages` query exceeding its first-wait budget once the volume
  cache is separate.
- **Gap.** Attribution quality for `go_symbols` in heuristic mode is not
  gated — it is reported through the `attribution` column instead, because
  the only way to check it is the reconciliation the column says is absent.

## Status

Proposed — awaiting review by the boxer maintainer.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.

## References

- [ADR-0064 — godepview Go dependency explorer](0064-godepview-go-dependency-explorer.md)
- [ADR-0094 — keelson introspection tables](0094-keelson-introspection-tables.md)
- [ADR-0169 — continuous coverage as keelson data](0169-continuous-coverage-keelson.md)
- [ADR-0038 — keelson background task primitive](0038-keelson-background-task-primitive.md)
- [ADR-0080 — packageprops per-package declarations](0080-packageprops-per-package-declarations.md)
- [ADR-0132 — SQL-defined applets](0132-sqlapplet-sql-defined-applets.md)
