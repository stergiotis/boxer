---
type: adr
status: accepted
date: 2026-06-12
reviewed-by: "p@stergiotis"
reviewed-date: 2026-06-21
---

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

- **O1** — External store only: a JSON snapshot or a leeway/boxer.facts table
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

`wasmsurvey` gains a `props` command group, four levels down the CLI:

```sh
boxer code analysis golang wasmsurvey props {generate,harvest,drift,verify}
```

The verbs are named bare below; the path is spelled here because the bare form
is not runnable, and generated files that printed it that way were read as
evidence the tool had been removed.

- **`props generate`** — seeds a `package_props.go` in each package from the
  survey's computed verdict. Idempotent-create: it writes only where the file is
  absent, never clobbering a curated one (the *hybrid* lifecycle).
- **`props harvest`** — go/ast-scans the tree for `PackageProps` declarations
  (no survey, no TinyGo) and emits the overview as a `--emit table` text grid or
  `--emit go` source file (`var Table = packageprops.Table{…}`) for embedding the
  whole-repo snapshot into a binary that does not link every package. `--tracked`
  restricts the scan to git-tracked declarations, which is the form that
  regenerates the committed table.
- **`props drift`** — reconciles the committed `--emit go` table against the
  git-tracked declarations and exits non-zero on any difference in either
  direction. Needs neither the survey nor TinyGo. This is what keeps the
  generated artifact honest between regenerations; without it a regen is
  correct only on the day it lands.
- **`props verify`** — reconciles each *declared* `PackageProps` against the
  freshly *computed* verdict and reports mismatches; exits non-zero on a
  regression (a package declared `WASMCompiles` that is now `WASMBlocked`). The
  sound static-red signal (ADR-0078) lets this gate CI without TinyGo. Because
  static mode can only prove red, it leaves most packages *unjudged* rather
  than agreeing with them; unjudged is counted, not listed, so the findings
  that matter are not buried under the abstentions.

Both reconcilers run in `scripts/ci/lint.sh`. They are deliberately outside the
`gov gate` composite (ADR-0179), which publishes to consuming repositories:
these gate *this* repo's table and declarations.

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
  reviewable, and navigable — find-references *is* the overview, `verify` gates
  declaration against reality, and `drift` gates the generated table against the
  declarations. Two artifacts means two gates; a declaration set with only one
  of them is the state this ADR spent two months in.
- The footprint argues for a **staged rollout** (SD: start with a subtree or the
  amenable set, not one mega-commit) so review stays tractable and the shared
  worktree stays calm.
- `Props` is a coordination point: adding a field touches the generator and
  (optionally) every declaration. Keep additions deliberate.

## Alternatives considered

- **Leeway/boxer.facts as the sole store (O1, ADR-0078 #3).** Rejected as the
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
4. ~~**Leeway-facts bridge**~~ — **done**: `props harvest --emit go` feeds
   `proptable`, which keelson surfaces as the `go_package_props` introspection
   table (ADR-0094). That it is *queryable* is what made its staleness matter
   enough to gate.
5. ~~**Keeping the generated table honest**~~ — **done 2026-08-14**: `props
   drift`, run in `lint.sh` (see Updates).

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

### 2026-07-02 — `Kind`: a package-role classification field

Added the first non-wasm field to `Props` (the growth SD4 anticipated): `Kind`,
classifying a package's *primary role* when it is not ordinary library code —
`KindDemo`, `KindExample`, `KindIntegrationTest`, with the zero `KindUnspecified`
asserting nothing. A single enum (mutually-exclusive roles), not a bitset: a
package reads as one thing, and the enum stays open for later roles.

Kind differs from the WASM* verdicts in one way that shaped the tooling: **there
is no survey that computes it**, so it is pure curated intent.

- **Not reconciled.** `props verify` checks only the WASM* verdicts (which have a
  computable oracle); it never flags Kind.
- **Preserved across re-seed.** `props generate --overwrite` rewrites a file from
  the survey verdict, which would wipe a hand-set Kind. Generate now reads the
  existing declaration first and carries its Kind through; a curated value always
  wins. Only when nothing is declared does it fall back to a directory-name
  **heuristic** (`demo`/`demos` → Demo, `example`/`examples` → Example).
- **Emitted only when set.** `renderPropsFile` and the `--emit go` table write a
  `Kind` field only for a classified package, so ordinary declarations stay
  byte-identical and the zero value keeps asserting nothing. `harvest --emit
  table` gains a `kind` column (blank for the common case).

`KindIntegrationTest` was applied by hand — never by the heuristic — because no
reliable automatable signal exists: the `test` dir-name suffix over-includes a
code-generation tool (`dsl/genbuildertest`), and "non-test source imports
`testing`" over-includes production libraries that merely ship a test helper
(`config/env`, `dsl/nanopass`, `leeway/dml`). Selecting by *inspection* of role,
the tagged set is the three executable conformance contracts that drive real
implementations end-to-end — `pushout/{envelope/codectest, exchange/exchangetest,
repo/storagetest}`. Deliberately left `KindUnspecified`: `unittest` (unit-test
assertion/mocks, not an integration harness) and `genbuildertest` (a codegen
tool). The many `*_integration_test.go` *files* inside library packages have no
package to carry the mark and stay unclassified.

Example packages were seeded by hand too
(`semistructured/leeway/{dml,readaccess}/example` → `KindExample`); the
directory-name heuristic covers future `demo`/`example` dirs. Hand tags survive a
re-seed because `generate` preserves them.

The static `proptable` regen was deferred: a full `harvest --emit go` currently
folds in an untracked in-flight `package_props.go` from concurrent work, and no
consumer reads Kind from the static table yet (the source declarations and the
runtime registry already carry it). A later regen on a settled tree picks it up.

### 2026-07-18 — `Review` field group proposed (ADR-0131)

[ADR-0131](./0131-systematic-adversarial-code-review.md) *(proposed)* adds a
per-package adversarial-review marker as the next `Props` field group — the
growth SD4 anticipated. It pairs a gofmt-normalized source digest and review
provenance (reconciled by a review-aware `props verify`, the way the WASM*
verdicts are) with a human-curated `ReviewState` verdict (unreconciled, like
`Kind`); the heavy findings content stays in a sidecar, keeping `Props` a clean
vocabulary (§SD5). This entry is a signpost — the full field-group description
lands here when ADR-0131 is accepted and the field is implemented.

### 2026-08-14 — the 2026-07-02 deferral was wrong; drift is gated instead

That entry deferred the `proptable` regen because a full `harvest --emit go`
folds in untracked in-flight declarations from concurrent work, and said "a
later regen on a settled tree picks it up". Both halves have since proved
wrong, and the second is the instructive one.

**"A settled tree" is not a state this repository reaches.** The working tree
is shared by concurrent sessions by design (AGENTS.md says so), so the
precondition is unmet nearly always — an unbounded deferral wearing a short
one's clothes. It held for two months and the table drifted 63 packages behind
while the entry read as a scheduling note.

**The defect was never that the table was behind.** It was that nothing
detected it. A regen is correct on the day it lands and decays from the next
commit, so "regenerate later" could not have been the fix at any date. The fix
is `props drift`, now in `lint.sh`.

**Scoping the comparison dissolves the blocker rather than waiting it out.**
Drift compares against *git-tracked* declarations, which is the honest
comparison independent of any of this: the table is a committed artifact, so
it should agree with committed declarations, and a package that exists only in
someone's working tree is not in the repository yet. Untracked work is
therefore invisible to both the check and `harvest --tracked`, and the
contamination the deferral was waiting out cannot occur. Tracked *paths* with
working-tree content, not content at HEAD — otherwise a regen could never be
committed alongside the declaration it encodes.

**Wiring `props verify` at the same time found four wrong declarations**, none
of which anything had reported because §Decision's claim that it "gates CI" had
never been implemented. `chlocalbroker`, `componentview` and (transitively)
`timerangepicker/evaluator` were declared amenable in the 2026-06-12 rollout,
which was true then, and each later grew an import edge reaching `arrow-go`.
`sysmvocab` was different and worse: declared amenable on 2026-08-14, hours
before this entry, reasoning "pure registry declarations … no syscalls, no
I/O". That reasoning was correct about the package's own code and produced the
wrong verdict anyway, because the property is over the transitive closure and
its registry reaches `arrow-go` through `leeway/common`. No reader computes a
closure by inspection; that is the whole reason a survey exists. All four are
now declared blocked, each naming its blame edge.

One tooling change was needed to make `verify` usable as a gate rather than
merely runnable. Its first run reported 882 mismatches, of which 870 were
`computed=unknown` — static mode declining to judge, since it proves only red
— and 12 were the real regressions. Abstentions are now counted rather than
listed (`--show-unjudged` restores them). A gate whose output is 98% noise is a
gate that gets tuned out, which is a fair account of why this one sat unwired.

### 2026-08-29 — 139 `WASMBlocked` declarations rest on a seed that was wrong

The 2026-08-14 entry turned four declarations Blocked because their closures
reach `arrow-go`, which the survey's static seed classified
unsupported-external. That seed was never probed; probed, arrow-go compiles
and runs under TinyGo ([ADR-0078](./0078-tinygo-wasm-amenability-survey.md),
Updates 2026-08-29), and the entry is removed. Every tracked declaration that
was Blocked *only* through it — 139, the four above included — moves to
`WASMUnknown`: static mode proves only red, and none of these packages has
been probed, so nothing is asserted. Declarations that are Blocked without a
static red are probe results from the 2026-06-12 rollout and are untouched.

The transitive-closure lesson in that entry stands unchanged — `sysmvocab`'s
own code still tells a reader nothing about its verdict — only the verdict it
led to turned out to be unfounded rather than wrong-by-omission. Comments in
the affected files keep their blame edge and drop the claim that arrow-go
does not compile.
