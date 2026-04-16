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

## Status

Proposed — awaiting review by <code owner(s)>.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## References

- <Link to related ADR, PR, issue, or external spec.>
