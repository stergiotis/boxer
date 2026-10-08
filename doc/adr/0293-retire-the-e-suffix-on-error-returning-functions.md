---
type: adr
status: proposed
date: 2026-10-08
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0293: retire the `E` suffix on error-returning functions

## Context

[CODINGSTANDARDS](../../CODINGSTANDARDS.md) asked that a function returning an
error carry an `E` suffix (`OpenE`). The rule was never enforced by codelint, and
the tree did not follow it: when the rule was reviewed, roughly one in twenty
exported error-returning functions under `public/` carried the suffix, and most
of those had no error-free twin the suffix would have told apart. A rule that
describes a minority of the code teaches a reader nothing at a call site — the
`error` in the signature already says it — and costs a decision at every new
function.

The same letter is the mandatory suffix for enum types (`WeekdayE`, codelint
CS006). Two meanings for one trailing capital made `ParseFormatE` ambiguous:
a parser returning an error, or a function named for the `FormatE` type.

## Decision

We drop the `E` suffix for error-returning functions. The enum-type `E`
suffix (CS006 / CS007) is unchanged. Existing functions lose the suffix in
boxer and in the repositories that build against it in the same workspace;
where the bare name was taken — by a type, a field, or an error-free twin — the
function gets a descriptive name instead (`adscore.CurveAt`,
`mdspan.Doc.ResolveHeading`, `peaks.ComputeOverview`).

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API under `public/` | every `…E` function or method returning an error is renamed | downstream modules' call sites, at their next boxer bump |
| `watchbill.HandlerI` | `RunE` → `Run`; `HandlerFunc`'s `Run` field → `RunFunc` | every handler implementation and `HandlerFunc` literal |
| `sink.SinkI` | `SeekE` → `SeekFrame`, not `Seek`, because `go vet` reserves `Seek` for the `io.Seeker` signature | sink implementations |
| CODINGSTANDARDS | the function-suffix rule is removed | — |

## Alternatives

- **Enforce the suffix with a codelint rule.** Rejected: it would rename the
  majority of the tree to match the minority, and keep the clash with the
  enum suffix.
- **Keep the suffix only where an error-free twin exists.** Rejected: the twins
  are few and already differ in signature; `MustX` is the Go idiom for the
  panicking side when one is wanted.
- **Leave the existing names and stop requiring the suffix for new code.**
  Rejected: two spellings for the same shape would stay in the tree
  indefinitely, and the enum ambiguity with them.

## Consequences

### Positive

- One meaning for a trailing `E`: an enum type.
- Names read like the rest of the Go ecosystem at call sites.

### Negative

- A source-breaking rename of exported API for every module that pins boxer.
- Commit history and older documents outside the tree keep the old names.

### Neutral

- Functions named with an `E` suffix that return no error (test helpers, an
  `E`-typed constructor) are untouched; the decision is about the convention,
  not about the letter.

## Migration — Tier 1

- **Breaks.** Every renamed function at its call sites; `watchbill.HandlerFunc{Run: …}`
  literals; implementations of `watchbill.HandlerI`, `procpool.SpawnerI` and
  `sink.SinkI`.
- **Path.** Drop the trailing `E` at the call site; the compiler names each
  one. For the collisions, the new name is in the commit that renames it.
- **Regeneration.** None — no generator emitted a renamed function.
- **Old shape.** Removed outright; no deprecated aliases (type and function
  aliases are not used in this tree).

## Verification plan — Tier 1

- **Lane.** None, because the decision removes a rule rather than adding one.
- **Gap.** Nothing stops a new `…E` function from being written. Review is the
  check; a lint rule was considered and rejected above.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [CODINGSTANDARDS § Function & Method Naming](../../CODINGSTANDARDS.md#function--method-naming)
- [ADR-0011](./0011-codelint.md) — codelint, which carries the enum-suffix rules CS006 / CS007.
