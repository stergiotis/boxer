---
type: adr
status: proposed
date: 2026-06-12
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0080: Per-package property declarations (`packageprops`)

## Context

[ADR-0078](./0078-tinygo-wasm-amenability-survey.md) computes a per-package
TinyGo/wasm verdict but leaves it *recomputed on demand* — its open question #3
(how to persist and track per-package state) is unanswered. The need is broader
than wasm: a place to record **curated, typed facts about each package** that is
(a) a single source of truth co-located with the code, (b) IDE-navigable
(find-references on a state constant lists every package in that state), (c)
readable at runtime by the linked-in packages, and (d) statically harvestable
into an overview table (arrow / leeway).

The repo already has the shape of the answer in `public/compiletimeflags`: a
zero-dependency leaf exposing typed constants the whole tree reads. The proposal
generalizes that to a per-package *record* declared next to each package and
referencing a shared vocabulary.

## Design space (QOC)

**Question.** How should per-package properties (wasm-amenability first, more
later) be recorded so they are co-located, typed, IDE-navigable, runtime-
readable, and harvestable?

**Options.**

- **O1** — External store only: a JSON snapshot or a leeway/runtime.facts table
  (ADR-0078 #3), detached from the package source.
- **O2** — Doc-comment or struct-tag annotations parsed out of band.
- **O3** — A co-located typed declaration `var PackageProps =
  packageprops.Props{…}` in each package, referencing a shared zero-dep
  vocabulary *(chosen)*.
- **O4** — Build-tag/const-file split à la `compiletimeflags` (one const file per
  state).

**Criteria.**

- **C1 — Single source of truth, co-located** with the package it describes.
- **C2 — IDE navigation / refactor-safety** (typed; goto-def, find-refs).
- **C3 — Runtime readable** by the program.
- **C4 — Static harvest** into a table without running the code.
- **C5 — Low ceremony** to add/maintain per package.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | +  | ++ | ++ |
| C2 | −  | −  | ++ | +  |
| C3 | +  | −  | ++ | ++ |
| C4 | ++ | +  | ++ | +  |
| C5 | +  | +  | +  | −  |

O3 is dominant on C1–C4 and only middling on C5 — and the C5 cost is paid by a
generator (`wasmsurvey props generate`) that seeds the declarations, so humans
curate rather than author from scratch. O4 (a const-file per property) does not
scale to a multi-field record; O1 is kept as a *downstream* of O3 (the harvester
can still emit facts), not the primary home.

## Decision

Introduce `public/packageprops`, a zero-dependency leaf exposing a typed `Props`
struct and its property enums (wasm-amenability is the first). Each participating
package declares a top-level value:

```go
package option

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMCompiles,
	WASMJS:           packageprops.WASMCompiles,
	WASMFreestanding: packageprops.WASMCompiles,
}
```

`wasmsurvey` gains a `props` command group:

- **`props generate`** — seeds a `package_props.go` in each package from the
  survey's computed verdict. Idempotent-create: it writes only where the file is
  absent, never clobbering a curated one (the *hybrid* lifecycle).
- **`props harvest`** — go/ast-scans the tree for `PackageProps` declarations
  (no survey, no TinyGo) and emits the overview as a `--emit table` text grid or
  `--emit go` source file (`var Table = packageprops.Table{…}`) for embedding the
  whole-repo snapshot into a binary that does not link every package.
- **`props verify`** — reconciles each *declared* `PackageProps` against the
  freshly *computed* verdict and reports mismatches; exits non-zero on a
  regression (a package declared `WASMCompiles` that is now `WASMBlocked`). The
  sound static-red signal (ADR-0078) lets this gate CI without TinyGo.

### Subsidiary design decisions

- **SD1 — Declaration shape: a top-level `var PackageProps`** (not a function).
  Simplest to harvest (one composite literal per package), readable at runtime as
  `pkg.PackageProps`, and a clean find-references target.
- **SD2 — `packageprops` is a zero-dep pure leaf.** It imports nothing, so the
  universal import edge it gains (every package will import it) introduces no
  cycle and cannot taint any package's own wasm verdict — and it is itself
  trivially `WASMCompiles`. Mirrors `compiletimeflags`.
- **SD3 — Hybrid lifecycle.** `generate` seeds once; the files are then
  human-owned (no `DO NOT EDIT` marker); `verify` keeps reality and declaration
  honest and gates regressions. Declarations are *intent*; the survey is the
  *checker*.
- **SD4 — `Props` is an open struct.** Wasm is the first field group; future
  properties (purity, determinism, ownership, stability…) are added as fields.
  The zero value asserts nothing.
- **SD5 — Harvest emits the table, not `Props` lw-tags.** Keeping `Props` a clean
  Go vocabulary (no leeway tags) avoids a half-built serialization contract; the
  harvester maps declarations → the leeway/arrow table it already knows how to
  build (ADR-0078 reuses godep, ADR-0064).
- **SD6 — Two discovery surfaces, neither reflect-based.** Go has no runtime way
  to enumerate the packages linked into a binary, so discovery is wired: (a) a
  process-global **registry** (`packageprops.Register`/`All`/`Lookup`) that each
  generated file feeds from `init` — since Go runs `init` for every linked
  package and DCE never drops it, `All()` is exactly "what is compiled into this
  binary"; (b) the **`--emit go` Table** (SD above) for the whole repo regardless
  of what a binary links. `packageprops` therefore depends on `sync`+`sort` —
  still no boxer/external deps, still wasm-green (probe-confirmed), so the
  universal import stays benign. The registry uses an explicit import path (not
  `runtime.Callers`), which TinyGo's name-stripping would defeat.

## Consequences

- ~362 new `package_props.go` files and a universal import edge to
  `packageprops`. The payoff: the per-package state becomes committed, diffable,
  reviewable, and navigable — find-references *is* the overview, and `verify` is
  the regression gate.
- The footprint argues for a **staged rollout** (SD: start with a subtree or the
  amenable set, not one mega-commit) so review stays tractable and the shared
  worktree stays calm.
- `Props` is a coordination point: adding a field touches the generator and
  (optionally) every declaration. Keep additions deliberate.

## Alternatives considered

- **Leeway/runtime.facts as the sole store (O1, ADR-0078 #3).** Rejected as the
  *home*: detached from source, not IDE-navigable. Retained as a harvest output.
- **Doc-comment / struct-tag annotations (O2).** Rejected: untyped, not runtime-
  readable, fragile to parse.
- **A const-file-per-property split (O4).** Rejected: works for one boolean
  (`compiletimeflags`), not for a growing multi-field record.

## Status — open questions

1. **Home** — `public/packageprops` (sibling of `compiletimeflags`) vs a `meta/`
   grouping. Proposed: `public/packageprops`.
2. **Generated file name** — `package_props.go` vs `props_gen.go`; and whether
   `generate` ever rewrites (idempotent-create only, per SD3).
3. ~~**Rollout staging**~~ — **done for `public/` 2026-06-12** (see Updates);
   `apps/` remain unsurveyed.
4. **Leeway-facts bridge** — wiring `props harvest` into runtime.facts /
   `godepview` (ADR-0078 #3/#4) once the declarations exist.

## Updates

### 2026-06-12 — seeded across `public/`

Rolled out with `wasmsurvey props generate --overwrite --patterns ./public/...`
(TinyGo 0.41.1, after the `eh` build-tag seam, [9eff543]): **361
`package_props.go` files**, of which **122 compile for wasi (121 js, 120
freestanding)** — up from 73/70/53 before the seam. `go build ./public/...`
passes with all 361 files present, confirming the universal `packageprops`
import is benign (SD2). Two corrections surfaced during the rollout, both now in
the tool:

- The static closure **and** the probe's export enumeration must model TinyGo's
  `tinygo` build tag — otherwise build-tag seams (like `eh`'s tinygo-vs-native
  split) are invisible to the triage, falsely keeping their beneficiaries Red or
  failing the probe as `undefined: probe.X`.
- `generate` must skip `packageprops` itself: writing a `package_props.go` into
  the vocabulary package makes it import itself (an import cycle).
