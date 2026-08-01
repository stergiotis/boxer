---
type: adr
status: proposed
date: YYYY-MM-DD
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-NNNN: <short decision title>

## Context

<What forces are at play? What constraints, incidents, or requirements
prompted this decision? A reader a year from now should be able to
reconstruct the pressures without external context.>

## Design space (QOC) — optional

> Use this section only when the decision has ≥3 viable options evaluated against ≥3 explicit criteria. Delete this entire section if unused; below the threshold, the prose `Alternatives` list is sufficient. Notation: Questions, Options, Criteria (MacLean, Bellotti, Young, Moran, 1991).

**Question.** <The single design question this ADR answers.>

**Options.**

- **O1** — <name and one-line description>
- **O2** — <name and one-line description>
- **O3** — <name and one-line description>

**Criteria.**

- **C1** — <dimension + how assessed>
- **C2** — <dimension + how assessed>
- **C3** — <dimension + how assessed>

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | ++ | +  | −  |
| C2 | −  | ++ | +  |
| C3 | +  | −  | ++ |

## Decision

<The choice we are making, stated in one or two sentences. Prefer the
active voice, e.g. "We will …".>

## Surfaces — Tier 1

> An inventory, not prose: which named contracts change shape, and what moves with them. Required when the decision touches a core surface per [CODINGSTANDARDS § What triggers an ADR](../../../CODINGSTANDARDS.md#what-triggers-an-adr); optional for a leaf decision. Delete this section if unused. `Consequences` records what the change *costs*; this section records what it *reaches*.

| Surface | Change | Moves with it |
| --- | --- | --- |
| <named contract — encoding, registry, IDL, exported API, gate> | <added / reshaped / removed> | <the downstream pass, generated artifact, or consumer that must change in the same commit> |

## Alternatives

- **<Alternative A>.** <One sentence on why rejected.>
- **<Alternative B>.** <One sentence on why rejected.>

## Consequences

### Positive

- <What becomes easier, safer, or cheaper.>

### Negative

- <What becomes harder, costlier, or locked in.>

### Neutral

- <Effects that are neither clearly good nor bad but worth recording.>

## Migration — Tier 1

> What breaks, and what a reader who is already on the old shape does about it. Delete this section if the decision breaks nothing. State "nothing to migrate" explicitly rather than deleting when the surface changed but the change is additive — the distinction is what a later reader needs.

- **Breaks.** <What stops compiling, parsing, or round-tripping. Name the symbol / encoding / key.>
- **Path.** <The steps, in order. Link a recipe under `doc/migration/` when the steps run past a few lines.>
- **Regeneration.** <Which generators must re-run, and whether both sides of an FFI boundary need rebuilding.>
- **Old shape.** <Deprecated-then-removed, removed outright, or kept indefinitely — and if removed, when.>

## Verification plan — Tier 1

> How the next reader will know this still holds. Name the lane, not the intention. Optional for a leaf decision; delete this section if unused. "None, because <reason>" is a valid entry — an unverifiable decision is worth marking as one.

- **Lane.** <default `go test`, the `//go:build integration` lane, a golden, a headless scene, the screenshot tour, a benchmark, a property test.>
- **What would fail.** <The observable that goes red if the decision is violated or silently regressed.>
- **Gap.** <What this plan does not cover, and why that is acceptable.>

## Status

Proposed — awaiting review by <code owner(s)>.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- <Link to related ADR, PR, issue, or external spec.>
