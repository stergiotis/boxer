---
type: adr
status: accepted
date: 2026-05-25
reviewed-by: "@stergiotis"
reviewed-date: 2026-05-25
---

# ADR-0048: Go file and package naming for pebble2impl

## Context

Boxer's [`CODINGSTANDARDS.md`](../../../boxer/CODINGSTANDARDS.md) (canonical for new Go code per [CLAUDE.md](../../CLAUDE.md)) covers *symbol* naming exhaustively — interface `…I`, enum `…E`, `Set`/`Get`/`Is` prefixes, opposite verb pairs — but is silent on Go *file* and *package* names. Pebble2impl has nontheless converged on conventions through practice; a survey of 1062 Go files under `./src/go/` and `./apps/` (2026-05-25, branch `main`) shows:

- **File basenames:** snake_case in 1051/1062 files (99 %). 11 outliers are camelCase (`encryptedHash.go`, `keyGenerator.go`, `parseTreeSexpr.go`, …) including one misspelling (`encyptedHash.go`).
- **Package names:** lowercase no-underscore in ~98 %. 8 outliers carry either an underscore (`data_encoding`, `dml_cbor`, `hn_explorer`, `regex_explorer`, `leewaywidgets_demo`) or a capital (`encryptedHash`, `findAnchor`, `wrapInArray`).
- **Generated files:** the `.out.go` dotted extension is used in ~36 checked-in generated files (`enums.out.go`, `affordances.out.go`, …); `.gen.go` is mentioned in [boxer's `ENGINEERING_PRACTICES.md`](../../../boxer/doc/ENGINEERING_PRACTICES.md) as the build-time equivalent but is not yet used here.
- **App-prefix discipline is uneven.** `apps/imztop/` uses `imztop_*.go` for 23/26 files (88 %); `apps/capdemo/`, `apps/capinspector/`, `apps/taskdemo/` use it for 11–33 %. Search ergonomics differ markedly between apps.
- **Test conventions** (`_test.go`, external `_test` package suffix) are Go-standard and uniformly followed; no OS/arch build-suffix files exist (build constraints use `//go:build` directives instead).

The pressures motivating a spec now:

1. **ADR-0035 introduced the `keelson` namespace** and moved standalone apps to top-level `apps/`. New packages are landing under those trees weekly; codifying conventions before they accumulate more outliers is cheaper than after.
2. **Mechanical enforcement is feasible.** All de-facto conventions are expressible as regex; the pattern from [`scripts/ci/entry-points-baseline.txt`](../../scripts/ci/entry-points-baseline.txt) (lint with a grandfathered baseline) generalises directly.
3. **The shared-worktree constraint** makes a tree-wide rename actively disruptive: multiple Claude sessions run against this worktree, and long staged sequences get clobbered by concurrent sessions. Any policy that demands an immediate sweep is impractical.

## Design space (QOC)

**Question.** What policy regime should pebble2impl adopt to keep Go file and package naming consistent without imposing a tree-wide rename?

**Options.**

- **O1 — No spec.** Status quo; conventions drift implicitly. New code may diverge further from de-facto.
- **O2 — Spec + immediate full normalize.** Codify rules and rename all 19 outliers (11 files + 8 packages) in one PR before merging the spec.
- **O3 — Spec + baseline + opportunistic migration.** Codify rules, write `naming-baseline.txt` enumerating today's violations, lint blocks new violations immediately; existing ones migrate when their owning code is otherwise touched. Mirrors the Entry-Points enforcement pattern already in [`scripts/ci/lint.sh`](../../scripts/ci/lint.sh).
- **O4 — Spec only, no enforcement.** Document rules; rely on review.

**Criteria.**

- **C1 — Coordination cost.** Blast radius against the shared worktree and concurrent agents.
- **C2 — Drift prevention.** Does the regime mechanically catch *new* violations?
- **C3 — Migration progress.** Does the regime reduce existing violations over time?
- **C4 — Implementation effort.** Lines of script and ADR text to land it.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | −− | +  | ++ |
| C2 | −− | ++ | ++ | −  |
| C3 | −− | ++ | +  | −  |
| C4 | ++ | −  | +  | +  |

O3 dominates O1 and O4 on drift prevention and migration without paying O2's coordination cost.

## Decision

We will adopt the following naming rules for new Go files and packages under `./src/go/...` and `./apps/...`, enforced by `scripts/ci/file-naming.sh` with grandfathered exceptions in `scripts/ci/naming-baseline.txt`.

### Rules

| # | Subject | Pattern | Notes |
|---|---|---|---|
| N1 | File basename | `^[a-z][a-z0-9_]*\.go$` | snake_case; lowercase; digits allowed mid-name |
| N2 | Checked-in generated file | `…\.out\.go` | dotted extension; basename must remain snake_case |
| N3 | Build-time generated file | `…\.gen\.go` | reserved for generators that emit at `go generate` time; not used in tree today |
| N4 | Go-recognised suffixes | `_test.go`, `_(linux\|darwin\|freebsd\|windows\|amd64\|arm64\|386\|riscv64\|wasm).go` | the only `_`-suffixes the spec lets the Go toolchain interpret specially |
| N5 | Test-utility helpers shared cross-package | `…_testutils.go` | per boxer `CODINGSTANDARDS.md` §Testing |
| N6 | Package name | `^[a-z][a-z0-9]*$` | lowercase, no underscores; external test packages `<pkg>_test` exempt |
| N7 | App-prefix | files directly under `apps/<n>/` must match `^<n>_[a-z0-9_]*\.go$` | exemptions: `main.go`, `doc.go`, `app_register.go`, `*_test.go`, `<n>.go` (the canonical app file), any file in a *subdir* of `apps/<n>/` |

### Scope

- **In scope:** every `.go` file under `./src/go/...` and `./apps/...`.
- **Out of scope:** `experiments/*`, `scripts/dev/*`, `attic/`, vendored third-party code. Matches the audit scope in CLAUDE.md `Entry Points (audit + baseline)`.

### Enforcement

- A new step `file-naming` in `scripts/ci/lint.sh` runs `scripts/ci/file-naming.sh --baseline scripts/ci/naming-baseline.txt --strict`. Non-zero exit on any violation not in the baseline.
- The baseline is hand-curated. Removing a line means the corresponding file or package has been renamed to conform; new violations must **not** be added to the baseline.
- The linter is pure bash + standard POSIX tools; no Go build step, no external dependencies.

### Migration

- The 11 camelCase files and 8 non-conformant packages identified by the 2026-05-25 survey ship in the initial `naming-baseline.txt`.
- Migration is opportunistic: when a baselined file or package is renamed for other reasons (refactor, scope change, typo fix — `encyptedHash` is a candidate), the baseline line is removed in the same commit.
- No dedicated rename PR is planned. The user has stated this preference explicitly; ADR-0035-style namespace work is the higher-priority migration.

## Alternatives

- **O1 No spec.** Rejected: the survey already shows drift (11 → ?) and ADR-0035 will accelerate it.
- **O2 Immediate full normalize.** Rejected: 19 renames across the tree at once is incompatible with the shared-worktree workflow (memory: `project_shared_worktree`); high coordination cost for a cosmetic win.
- **O4 Spec only, advisory.** Rejected: an unenforced spec is invisible to agents and to humans skimming a diff; the drift the spec exists to prevent will continue.

## Consequences

### Positive

- **Grep-friendly across apps.** Mandating the `<n>_` prefix under `apps/<n>/` means `grep -r 'imztop_panel'` finds all imztop panels even when the dir is open in a sibling worktree, and `grep -r '_test\.go'` enumerates tests by glob — already true elsewhere, now true under `apps/` too.
- **Drift is bounded.** Any new violation lands as a failing CI step with a specific file or package named; no spec interpretation needed.
- **Aligns with `go vet`.** Rule N6 matches the standard Go tooling's view of package names; downstream consumers (`pkgsite`, `golint`-derivatives) stop emitting warnings for the migrated packages.
- **Mirrors the established Entry-Points pattern.** Reviewers and agents who know how the entry-points baseline works understand the naming baseline immediately.

### Negative

- **Existing violations linger.** Until each baselined file or package is touched for other reasons, the inconsistency remains visible. The linter does not push migration; it only prevents regression.
- **Baseline file is hand-curated.** Removing a line is the canonical "this is fixed" signal; if a rename is committed without the baseline edit, the next lint pass will surface the violation as "new". This is a small mental load on the contributor.
- **Bash regex must stay readable.** Compounding the rules into a single grep pipeline would be unmaintainable; the linter intentionally uses one pass per rule.

### Neutral

- The decision is silent on rule N3 (`*.gen.go`) actually being used. No file in pebble2impl emits to that path today; the rule reserves the extension so a future build-time generator does not have to amend this ADR to use it.
- Test-package external-suffix files (`<pkg>_test`) are recognised but the spec does not require them; `_test.go` files may be in-package or external as the test author chooses, per Go-standard practice.

## Status

Accepted 2026-05-25.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../../../boxer/doc/DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0035 — keelson namespace introduction](0035-keelson-namespace-introduction.md) — motivates `apps/` top-level layout enforced by rule N7.
- [`scripts/ci/entry-points-baseline.txt`](../../scripts/ci/entry-points-baseline.txt) — pattern this ADR mirrors for enforcement.
- [CLAUDE.md "pebble2impl-local supplement"](../../CLAUDE.md) — the build-tags and Conventional-Commits rules sit alongside this naming spec as pebble-local extensions to boxer.
- [Effective Go — Package names](https://go.dev/doc/effective_go#package-names) — upstream authority for rule N6.
- [boxer `CODINGSTANDARDS.md`](../../../boxer/CODINGSTANDARDS.md) — covers symbol naming; this ADR is the file/package counterpart.
