---
type: adr
status: proposed
date: 2026-11-20
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0389: Stability tiers per package, and what a version number promises

## Context

The module exposes several hundred importable packages and few of them live
under `internal/`, so Go's own visibility rule says almost nothing about what
a consumer may rely on. Nothing else did either: the README called the whole
tree alpha, a tag carried no stated meaning, consumers pinned pseudo-versions
of untagged commits, and the changelog entries are date windows rather than
release notes. A large share of the importable surface is examples, demos,
fixtures and generated grammars, and the per-package record
([ADR-0080](./0080-packageprops-per-package-declarations.md)) classified few of
them.

Two house rules make an incompatible change costlier for a consumer than it is
in most Go code. "No Aliases" and "No compatibility shims" (CODINGSTANDARDS)
remove the usual forwarding path for a move or rename, so a break lands in one
commit with no window. And the `go` directive moves at patch level, so every
consumer's own `go` line follows it.

ADR-0080 SD4 anticipated a stability field on `packageprops.Props`. This ADR
adds it, states what it promises, and wires the gates that keep a declaration
honest.

## Decision

1. **A `Stability` field on `packageprops.Props`** (`StabilityE`):
   `StabilityUnspecified` (zero, asserts nothing), `StabilityStable` (covered
   by the compatibility promise), `StabilityExperimental` (public, may change
   in any MINOR without a deprecation window), `StabilityInternal` (an
   implementation detail Go's import rules happen to expose; not for use
   outside the module). Curated like `Kind`; `props generate` preserves a
   declared tier and never seeds one.
2. **Kind stays the role axis and gains `KindTestSupport` and `KindTool`.** A
   role and a promise are different questions: a test-support package can be
   Stable when other modules' tests are meant to import it, and a tool's Go API
   is unpromised while the `boxer` command tree it builds is covered. Making
   them stability values would force one answer onto both questions.
3. **Only Stable is promised.** Unspecified, Experimental, Internal and every
   package without a declaration are outside the promise. An app package under
   `apps/` is not library surface unless its declaration says so; app
   launch-config leaves (`launchcfg` and similar) are Experimental.
4. **`props verify` holds a Stable package to its side.** It fails when a
   Stable package imports (non-test, native build) an Experimental or Internal
   package of the module, or a package of any non-unspecified Kind; when it
   has no package doc comment; or when a demo, example or tool package is
   declared Stable. A Stable package importing an untiered package is counted
   as advisory. Go `internal/` paths are exempt. A candidate that fails is
   demoted rather than the rule relaxed.
5. **`doc/SURFACE.md` is generated** from the tracked declarations and package
   doc comments by `props surface`, and `props surface --check` runs in the
   packageprops lint step beside `props drift`.
6. **The versioning policy is [VERSIONING.md](../../VERSIONING.md):** what a
   version number means, what it covers, deprecation, the toolchain floor and
   support. The choices it makes are listed below.
7. **`scripts/ci/apidiff.sh [<base-ref>]`** compares the exported API of every
   package Stable at HEAD or at the base with x/exp's `apidiff` (go.mod tool
   block), and fails on an incompatible change unless a breaking (`!`) commit
   in the range touches that package's directory. The release script calls it
   by that name.
8. **`boxer gov releasenotes --from <ref> --to <ref>`** compiles the notes of a
   range from git alone; `doc/releases/unreleased.md` is its output for the
   latest tag..HEAD and is regenerated at release time.
9. **`public/packageprops` is Stable by this statement.** It cannot carry a
   declaration (it would import itself); it depends on nothing and grows by
   adding fields, which is a compatible change.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `packageprops.Props`, `Kind` | added `Stability`, `KindTestSupport`, `KindTool` | `props` harvest/generate/drift, `proptable.out.go`, the `go_package_props` introspection table (`stability` column) |
| `props verify` | stability rules added | `scripts/ci/lint/packageprops.sh` |
| `doc/SURFACE.md` | new, generated | `props surface --check` in the same lint step |
| `boxer gov releasenotes` | new command | `doc/releases/unreleased.md` |
| go.mod `tool` block | `golang.org/x/exp/cmd/apidiff` | `scripts/ci/apidiff.sh` |

## Alternatives

- **Tier by directory** (move unpromised code under `internal/` or a
  `stable/` tree). Rejected: hundreds of moves, each one a break for whoever
  imports it, and every later change of tier would be another move.
- **Tiers in doc comments or a side file.** Rejected for ADR-0080's reasons:
  untyped, not navigable, and a second artifact to drift.
- **Tool and test-support as stability values.** Rejected: see Decision 2.
- **`gorelease` instead of `apidiff`.** Tried against the latest tag: it
  works, reports every package of the module (several hundred sections), and
  suggests a version, but it resolves the base through the module proxy, so an
  unpushed ref cannot be a base, and it has no notion of the tiers. Kept as an
  occasional cross-check, not the gate.
- **`apidiff -m` (whole-module mode).** Rejected: it reports every package and
  mixes in generated code; per-package export data from the build cache is
  cheap enough to compare only the Stable set.
- **Seed `Stability` from a heuristic in `props generate`.** Rejected: a
  promise is a decision, not an inference.

## Consequences

### Positive

- A consumer can tell, per package, what a version number promises, and a
  release has a mechanical list of what broke.
- The gates make a tier hard to state falsely: a Stable package cannot rest on
  a weaker one without `props verify` failing.

### Negative

- The rules demote several candidates whose only fault is an import edge (see
  Choices 3). The promise is smaller than the code that deserves it until those
  edges move.
- The deprecation window adds a carve-out to two house rules, and every
  forwarding alias or wrapper is a debt with a due date. codelint's alias rule
  (CS008) still fires on such an alias, so each one carries a
  `//boxer:lint disable=CS008` annotation whose reason names the window.

### Neutral

- `Props` grows a field; ordinary declarations stay byte-identical because an
  unset field is not emitted.

## Migration — Tier 1

Nothing to migrate: every change is additive. A consumer that switches on
`packageprops.Kind` sees two more values.

## Verification plan — Tier 1

- **Lane.** `scripts/ci/lint/packageprops.sh` (props drift, props surface
  --check, props verify --mode static); `scripts/ci/apidiff.sh` at release; unit
  tests of the stability rules, the surface rendering and the release-notes
  parsing in the default `go test` lane.
- **What would fail.** A Stable package gaining an import of an Experimental,
  Internal or non-library package, or losing its package doc; a declaration
  changed without regenerating the table or the surface document; an
  incompatible change to a Stable package with no breaking commit touching it.
- **Gap.** The rules check imports, not whether a weaker package's types appear
  in a Stable package's API; stored and wire formats are covered by a separate
  contract snapshot mechanism, not by these gates; a breaking commit without
  `!` is caught by apidiff only for Stable packages.

## Choices for review

Every item below is a choice made on the owner's behalf.

1. The four tiers, and test-support and tool as Kinds rather than tiers.
2. Only Stable is promised; Unspecified means outside the promise.
3. Stable → Internal edges fail like Stable → Experimental ones. This demoted
   `leeway/canonicaltypes` (it imports its generated grammar and
   `parsing/antlr4utils`), `leeway/mappingplan`, `dsl/marshalling` and
   `recordstore/chexec` among others. The other import rules demoted, for
   example, `leeway/common` (it imports the example package
   `canonicaltypes/sample`), `keelson/runtime/help` (it imports the tool
   package `gov/docstd`) and `db/clickhouse/chclient` (it imports
   `db/fairlead` and `clickhouseenv`). An alternative is to allow an Internal
   import whose types do not appear in the Stable package's exported API; that
   needs a type-checked rule.
4. A Stable candidate without a package doc comment is left Unspecified, not
   Experimental, so it can be promoted once documented. This covers
   `observability/eh`, `eh/eb`, `observability/ph`, `observability/vcs`,
   `functional`, `caching`, `unsafeperf`, `hashing/splitmix64`,
   `identity/identifier`, `semistructured/cbor`, the leeway runtime set that
   has no doc (`naming`, `dml/runtime`, `readaccess/runtime`,
   `readaccess/fatruntime`, `canonicaltypes/ctabb`) and
   `markdown/obsidian` with its `resolver` and most `ext/*`.
5. `compiletimeflags` is Unspecified rather than Internal: every widget reaches
   it through the bindings, and it is a one-constant build-tag switch.
6. Widgets the ADR-0267 conformance test covers are Stable candidates; the
   packages that test exempts (value types, layout engines, tokenizers) are
   left unclassified, though some are widely imported.
7. The host composition roots (`windowhost`, `hostboot`, `servicehost`,
   `hostmain`) are Experimental, not Internal: an app's `main` uses them, and a
   tier is per package, so their Options types cannot be promised alone.
8. The three pushout conformance suites keep their curated
   `KindIntegrationTest`.
9. `public/packageprops` is Stable by Decision 9.
10. Everything in [VERSIONING.md](../../VERSIONING.md): the meaning of MINOR and
    PATCH, what is covered, the one-MINOR deprecation window and its carve-out
    from "No Aliases" and "No compatibility shims", the toolchain-floor rule,
    and support for the latest tag and `main` only.
11. `apidiff.sh` counts a breaking commit as owning a package's change when it
    touches a file directly in that package's directory.

## Status

Proposed — awaiting review by the maintainer.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0080](./0080-packageprops-per-package-declarations.md) — the per-package record this extends.
- [ADR-0078](./0078-tinygo-wasm-amenability-survey.md) — the survey `props verify` reconciles against.
- [ADR-0267](./0267-imzero2-go-widget-api-contract-immediate-and-semi-retained.md) — the widget contract the Stable widgets follow.
- [ADR-0375](./0375-an-app-states-its-maturity-a-grade-on-the-manifest-and-two-columns-on-keelson-apps.md) — the separate maturity axis apps carry.
- [VERSIONING.md](../../VERSIONING.md), [doc/SURFACE.md](../SURFACE.md).
