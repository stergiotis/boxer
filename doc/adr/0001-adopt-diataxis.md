---
type: adr
status: accepted
date: 2026-04-16
reviewed-by: "p@stergiotis"
reviewed-date: 2026-04-16
---

# ADR-0001: Adopt Diátaxis and ADRs as the Documentation Framework

## Context

The boxer repository spans deeply nested, technically diverse Go
packages (FEC codecs, DSL compilers, ClickHouse tooling, imzero
bindings, semistructured/leeway, …). As the repository has grown,
documentation pressure produced a pattern of overlapping, ad-hoc
Markdown files — `DESIGN.md`, `SPEC.md`, `ARCH.md`, `NOTES.md` — with
no shared taxonomy. Observed symptoms:

- Readers could not predict where to look for a given question.
- Files mixed timeless theory ("how Golay24 works") with mutable
  choices ("why we picked codec A over B"), making either half hard to
  update in isolation.
- Tooling and LLM traversal dead-ended on implicit relationships and
  bare directory references.
- API reference drifted from the code because it lived in Markdown
  rather than in doc comments consumed by `pkgsite`.

We need a single, enforceable framework that:

1. Tells contributors exactly where new documentation belongs.
2. Keeps Reference docs next to the code so `pkgsite` stays canonical.
3. Separates timeless theory from mutable decisions so each can evolve
   independently.
4. Produces artifacts that survive refactors and package moves.

## Design space (QOC)

**Question.** What documentation framework should govern this repository to prevent sprawl, keep Reference next to code, and separate timeless theory from mutable decisions?

**Options.**

- **O1** — Status quo: ad-hoc `DESIGN.md` / `SPEC.md` / `ARCH.md` files, no taxonomy.
- **O2** — Diátaxis alone (Reference / How-To / Explanation / Tutorial), no ADRs.
- **O3** — ADRs alone (append-only decision log), no descriptive taxonomy.
- **O4** — A bespoke in-repo framework designed from scratch.
- **O5** — External wiki or docs site, separated from the code.
- **O6** — Diátaxis + ADRs *(chosen)*.

**Criteria.**

- **C1 — Prevents sprawl:** does the framework stop overlapping ad-hoc files from accumulating?
- **C2 — Separates theory from decisions:** are timeless mechanics and mutable choices in artifacts with distinct lifecycles?
- **C3 — Covers full doc surface:** does the framework address Reference, How-To, Explanation, Tutorial, *and* Decisions?
- **C4 — Community familiarity:** do new contributors already know the conventions, lowering onboarding cost?
- **C5 — Docs next to code:** does Reference live adjacent to the code it describes, so it cannot drift?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 | O5 | O6 |
|----|----|----|----|----|----|----|
| C1 | −− | ++ | −  | +  | −  | ++ |
| C2 | −  | −  | ++ | +  | −  | ++ |
| C3 | −  | +  | −− | +  | +  | ++ |
| C4 | −  | ++ | +  | −− | +  | ++ |
| C5 | −  | ++ | +  | +  | −− | ++ |

O6 is `++` on every criterion; no other option is Pareto-optimal against it.

## Decision

We adopt two complementary conventions:

1. **[Diátaxis](https://diataxis.fr/)** for descriptive documentation.
   Every doc is classified as Reference, How-To, Explanation, or
   Tutorial. Formats are fixed: Reference in Go doc comments and
   `doc.go`; How-To in `example_test.go`; Explanation in
   `EXPLANATION.md`; Tutorials in top-level `TUTORIAL.md` files.
2. **Architecture Decision Records (ADRs)** for decisions. ADRs live
   in `doc/adr/NNNN-<slug>.md`, are append-only, and follow a fixed
   structure (Context, Decision, Alternatives, Consequences, Status).

Packages may add an optional `README.md` as a package-level overview,
typed as a normal Diátaxis artifact (almost always `type: reference`);
leaf packages adequately served by `doc.go` do not need one (see
[`doc/DOCUMENTATION_STANDARD.md`](../DOCUMENTATION_STANDARD.md) §3).

The full standard is codified in
[`doc/DOCUMENTATION_STANDARD.md`](../DOCUMENTATION_STANDARD.md).

## Alternatives

The QOC matrix above carries the comparative assessment. The notes below capture nuance not visible in the ratings.

- **O1 — Status quo.** Produced the sprawl that prompted this ADR; doing nothing was not viable.
- **O2 — Diátaxis alone.** Decisions would leak into `EXPLANATION.md`, conflating timeless theory with mutable choices. Keeping them in separate artifacts gives each its own lifecycle — ADRs are append-only, Explanation is mutable.
- **O3 — ADRs alone.** Covers decisions only, leaving Reference / How-To / Explanation / Tutorial ungoverned.
- **O4 — Bespoke framework.** Diátaxis and ADRs have community mindshare and external documentation; inventing a local system imposes onboarding cost on every new contributor.
- **O5 — External wiki.** Separates docs from code, making Reference drift inevitable and violating the "docs next to code" goal.

## Consequences

### Positive

- A contributor asking "where does this doc go?" gets a deterministic
  answer from the standard.
- Reference documentation stays executable and `pkgsite`-visible by
  virtue of being Go doc comments and `example_test.go` files.
- Decisions accumulate in a single, chronologically ordered,
  append-only log, so the history of "why it is this way" is
  recoverable.
- Banned file patterns (`DESIGN.md`, `SPEC.md`, `NOTES.md`, …) are
  explicit, giving reviewers a concrete rule to enforce.

### Negative

- Existing ad-hoc Markdown files across the tree must be migrated.
  This is a one-time cost but non-trivial in scope.
- Contributors unfamiliar with Diátaxis must learn the quadrants
  before writing their first doc. Templates lower this cost but do not
  eliminate it.
- ADRs add lightweight process overhead for decisions that would
  previously have been made implicitly in code review.

### Neutral

- The enforcement tool (`docs-lint`, Go-native, declared under
  `go.mod`'s `tool` directive and invoked via scripts under
  `./scripts/`) is specified but not yet implemented. Tracked as a
  follow-up.
- Diagramming and changelog conventions are intentionally out of scope
  for this ADR and will be addressed separately.

## Status

Accepted — 2026-04-16. This is the first ADR in the repository and
supersedes no prior record.

## Updates

- **2026-04-19 — Router sub-type withdrawn.** The original decision
  introduced an optional `type: router` README that listed links to
  companion Diátaxis artifacts. In practice no package adopted a pure
  router: READMEs that were added carried substantive prose and used
  `type: reference`, duplicating `doc.go`/`pkgsite` when they did not.
  The `router` type was removed from the standard and from DL001's
  enum; package READMEs are now ordinary Diátaxis artifacts. The
  repository-root `README.md` is separately exempted from the
  front-matter requirement, since it is the GitHub project landing
  page rather than a package-scoped doc. The core Diátaxis + ADR
  decision above is unchanged.

## References

- [`doc/DOCUMENTATION_STANDARD.md`](../DOCUMENTATION_STANDARD.md) — the
  operational codification of this decision.
- [Diátaxis framework](https://diataxis.fr/).
- Michael Nygard, "Documenting Architecture Decisions" (2011) — the
  original ADR proposal.
- [go.dev/doc/comment](https://go.dev/doc/comment) — Go doc comment
  conventions referenced by the standard.
