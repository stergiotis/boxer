---
type: adr
status: accepted
date: 2026-06-07
reviewed-by: "p@stergiotis"
reviewed-date: 2026-06-07
---

# ADR-0073: Leeway membership role & param-treatment

## Context

Leeway's membership graph mixes two kinds of tag in one mechanism: those that **define**
an attribute's identity (`/hostname`, `/users/_/email` — what the data is *about*) and
those that **annotate** an existing attribute (classification flags, governance labels —
metadata applied to a value). Downstream tooling needs the distinction: JSON Schema maps
identities to `properties` / `required` and annotations to extension slots, and attribute
keying presupposes identities.

This ADR covers membership **meaning** — plane **E** of the basis (ADR-0070 §Concept
basis) — and the mechanism that decides it (plane **F**). Meaning is independent of
carriage: the role of a tag does not depend on which channel (ADR-0072) carries it. It
supersedes ADR-0007 and the meaning half of its late membership-package update.

The decision operates at the **value level** — on the concrete
`membership.MembershipValue` a driver pushes (ADR-0072), not on the schema-level
`MembershipDesc`, which describes which membership *kinds* a section accepts but lacks the
tag content needed to decide.

## Decision

### E1 — Role: primary or secondary

A membership is **primary** (defines an attribute's identity) or **secondary** (annotates
an existing attribute). On the read side only primary memberships are discriminative: any
one of them locates its attribute by the key
`(Kind, Ref | Verbatim [, Params when the treatment is Identity])`, and the value plus all
secondary memberships hang off it.

### E2 — Param-treatment: identity, index, or none

A membership's parameters are treated as `Identity` (params join the attribute key, e.g.
`/users/abc-123/email`), `Index` (params are dimensional indices within one attribute,
e.g. `/embedding/_`), or `None`. Treatment is independent of role: a section may mix
identity-bearing and index-bearing memberships, so section-level uniformity is *role*
uniformity, not *treatment* uniformity.

### F — Role is computed by a pluggable classifier

Role and param-treatment are computed on demand by an application-supplied `ClassifierI`
that consumes a `membership.MembershipValue` and a `SectionContext` (name + use-aspect
set) and returns `(role, paramTreatment)`. The framework ships a `DefaultClassifier`
honouring a path-prefix convention (a configurable prefix, default `/`, marks primary).
Two section use-aspects — `AspectSectionMembershipsAllPrimary` /
`AspectSectionMembershipsAllSecondary` — let a section declare uniform role and
short-circuit the classifier.

Mining (frequent-itemset analysis) is retained as a **drift signal**, never as contract
input: it answers "what is reliably present?", a descriptive question, where role asks
"what does this attribute mean?", a prescriptive one.

### Layering

`membership` (value + representation, ADR-0072) ← `membershiprole` (this policy) ←
consumers (card, the streamreadaccess driver, leeway widgets). The classifier is the only
place role lives; nothing below it knows primary from secondary.

## Alternatives

- **ARM-mined contract surface.** Promote stable mined itemsets to "contract
  memberships." Rejected as the primary mechanism: parameter-sensitive, wrong for
  rare-but-critical fields, and answers the descriptive question, not the prescriptive
  one. Retained for drift detection.
- **Role on `MembershipDesc`.** Add a role field to the schema struct. Rejected: conflates
  the schema level (which kinds a section accepts) with the value level (the role of the
  tag content flowing through). One kind-slot may hold both roles by name.
- **Section-role-only, no classifier.** Force uniform role per section via use-aspects.
  Rejected: breaks for existing mixed-role tables and loses naming-convention defaults.
  The classifier subsumes it — a section-role-only policy is a classifier that consults
  only the use-aspect.

## Consequences

- Existing leeway tables work unmodified; the classifier reads what is already there.
- Attribute keying is mechanical given the classifier's output.
- Classifier consistency is a per-application concern: two consumers with different
  classifiers key differently. Schema documents may record the classifier identity so
  consumers can verify they share a policy.

## Status

Accepted on 2026-06-07. Re-cuts and supersedes ADR-0007.

Implementation status (2026-06-07): the classifier, default policy, and section
use-aspects are **implemented** over the current `MembershipValue`; re-targeting them onto
the extracted `membership` package (ADR-0072) is **pending** (refactor Phase 3).

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are
append-only; supersession is recorded, not deleted.

## References

- [ADR-0070 §Concept basis](0070-leeway-entity-assembly.md) — the shared axis model.
- [ADR-0072](0072-leeway-membership-carriage.md) — the `membership.MembershipValue` this classifies, and representation.
- [ADR-0007](0007-leeway-membership-role-classifier.md) — superseded; re-cut here.
- [`../../public/semistructured/leeway/membershiprole/`](../../public/semistructured/leeway/membershiprole/) — the classifier package.
- [`../../public/semistructured/leeway/useaspects/lw_useaspects_enum.go`](../../public/semistructured/leeway/useaspects/lw_useaspects_enum.go) — the section-uniformity use-aspects.
