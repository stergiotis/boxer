---
type: adr
status: accepted
date: 2026-05-01
reviewed-by: "p@stergiotis"
reviewed-date: 2026-05-01
---

# ADR-0007: Membership Role Classifier and Section Uniformity Use-Aspects

## Context

Leeway's membership graph mixes two semantically distinct kinds of tag in one mechanism: those that *define* an attribute's identity (`/hostname`, `/metrics/cpu`, `/users/_/email` — what the document is *about*) and those that *annotate* an existing attribute (`errormsg`, classification flags, governance labels — metadata applied to a value). The protocol carries them uniformly: both end up as memberships in the same column with `membership-card` summing across them.

Downstream tooling needs the distinction. JSON Schema generation maps primary memberships to `properties` + `required`, secondary memberships to either `additionalProperties` or a scoped extension slot. Data-contract field declarations carry primary identities; PII / classification rules attach to attributes, not to whole sections. Quality SQL phrased per attribute presupposes attribute keys, which presuppose primary memberships. Without a first-class role distinction the contract surface either over-claims (every observed tag becomes `required`) or under-claims (everything is loose annotation).

The leeway-advanced documentation already shows the labeling case (a `/metrics/error` value receiving a separate `errormsg` low-card-verbatim membership) but treats it as a section-level mechanic; the protocol provides no signal that one is the identity and the other is the annotation.

A separate, descriptive approach was considered: mine frequent itemsets per result-set (Association Rule Mining) and treat the mined patterns as the contract surface. That avoids authoring overhead at the cost of stability, parameter sensitivity, and the awkward fact that ARM answers "what is reliably present?" — a *descriptive* question — when the question we actually want answered is "what does this attribute *mean*?" — a *prescriptive* one. Mining is retained in this design as a drift-detection signal, not as the contract input.

The role determination must operate on the **value level** — what `streamreadaccess.SinkI.AddMembership*` actually pushes — not on the schema-level `MembershipDesc`. The schema describes which membership *kinds* a section accepts; the classification decision needs the concrete tag content (`verbatim="/hostname"` vs `verbatim="errormsg"`), the kind discriminator, and any params. Conflating the two levels was an early misstep in this design.

Forces the decision must respect:

- **Application owns policy.** The rule for "this membership is primary" is application-specific. A telemetry pipeline distinguishes path-shaped vs label-shaped tags by naming convention; a graph-shaped store does it by registry lookup; a generic ingest may use a hybrid. The decision mechanism must be pluggable.
- **No structural reorganization.** Existing Leeway tables mix primary and secondary memberships in the same section's membership column. The design must work on those tables as-is; rewriting them to segregate by section or co-section is an *option*, not a *requirement*.
- **Leeway primitives stay intact.** Sections, co-sections, membership-specs, value-aspects, use-aspects all exist. The mechanism rides on them, doesn't replace them, and doesn't add new physical column roles.

## Decision

We adopt a **classifier abstraction**: role and parameter treatment for any given Leeway membership are computed on demand by an application-supplied `ClassifierI` that consumes value-level membership instances (mirroring `SinkI.AddMembership*` shapes) and returns a `(role, paramTreatment)` pair. The framework ships a `DefaultClassifier` covering the common path-prefix case. Two new section-level use-aspects let a section advertise that all its memberships have the same role, which the classifier honours as a short-circuit.

Concretely:

1. **Two levels are kept distinct.** Schema `MembershipDesc` describes *what kinds of memberships a section allows*. Value-level `MembershipValue` describes *the concrete tag instance the driver pushes*. The classifier consumes the latter; the section is passed alongside as `SectionContext` (carrying name plus the section's `useaspects.AspectSet`).

2. **Four-quadrant role model.** Per-membership classification is `MembershipRoleE × ParamTreatmentE`:
   - Role: `Primary` (defines attribute identity) or `Secondary` (annotates an existing attribute).
   - Param treatment: `Identity` (params contribute to attribute identity, e.g. `/users/abc-123/email`), `Index` (params are dimensional indices within one attribute, e.g. `/embedding/_`), or `None` (no params on this membership).

3. **Two new use-aspects.** `useaspects.AspectSectionMembershipsAllPrimary` and `useaspects.AspectSectionMembershipsAllSecondary` declare that every membership in the section has the same role. The classifier honours these as short-circuits when present; their absence means "decide per-membership."

4. **`DefaultClassifier` policy.** Honour the section use-aspects first; for verbatim-shaped kinds, treat a `PathPrefix`-prefixed verbatim (default `/`) as Primary, anything else as Secondary; ref-shaped kinds default to Primary; param-treatment defaults to Identity for parametrized kinds, None otherwise.

5. **Co-section primary/secondary segregation is an organizational pattern, not a requirement.** A team that wants the rich/lean subsetting story declares a primary section and bolts on a secondary co-section with no value columns whose memberships are pure annotations; vertical subsetting drops or keeps the secondary co-group whole. Mixed-role sections work just as well; the classifier handles both because it acts at value grain.

6. **Mining is observability, not contract input.** Frequent-itemset mining stays as a drift-detection tool: changes in observed itemsets between result-sets become candidate signals for promoting a label to primary, splitting an attribute, or deprecating an unused tag. Mining never produces the wire-form classification.

7. **Implementation location.** The classifier package lives at `public/semistructured/leeway/membershiprole/`. Card emitters, schema-document writers, and data-contract generators each accept a `ClassifierI`; default to `DefaultClassifier` when none is supplied.

### Subsidiary design decisions

- **SD1 — Classifier consumes value, not schema.** The classifier interface takes a `MembershipValue` (kind discriminator, lowCard flag, ref / verbatim / params payload, human-readable companions) and a `SectionContext` (name plus use-aspect set). It does *not* take `common.MembershipDesc` because that struct describes slot-types and lacks the tag content needed to decide. Mirroring `SinkI.AddMembership*` shapes keeps the boundary between protocol and policy clean.

- **SD2 — Default classifier ships out-of-the-box.** `DefaultClassifier` short-circuits on use-aspect, then falls back to `strings.HasPrefix(verbatim, "/")` for verbatim-shaped kinds. Ref-shaped kinds default to Primary; applications needing a registry-based decision wrap or replace the default. The configurable `PathPrefix` field lets non-`/` conventions work without a custom classifier.

- **SD3 — Use-aspects are advisory uniformity hints.** Setting `AspectSectionMembershipsAllPrimary` on a section that contains a clearly secondary tag (per the application's classifier) is a contract bug, but the classifier is free to either honour the hint or override on a per-membership basis. Validation tooling may cross-check; the protocol does not.

- **SD4 — Param treatment is per-membership, not per-section.** A section may legitimately mix `paramsInIdentity` and `paramsAsIndex` memberships (e.g. `/users/_` for identity-bearing UUIDs alongside `/embedding/_` for dimensional indices). The classifier returns the param treatment alongside the role; section-level uniformity is *role* uniformity, not *param-treatment* uniformity.

- **SD5 — Multi-primary aliasing is a multi-membership case.** When the classifier returns `Primary` for several memberships on the same attribute, the consumer should emit one logical attribute keyed by the lexicographically smallest primary path with an `aliases` field listing the others. This preserves the leeway-advanced multi-membership / aliasing semantic at the consumer level (e.g. JSON layer).

- **SD6 — Secondary co-sections are value-less by convention.** The first cut treats sections whose effective uniformity is `AllSecondary` as membership-only by convention; future use cases for *value-bearing* secondary memberships (annotation values like `evidence_strength=0.95`) can opt back in by adding a section-level aspect that re-permits them. Keeping the default conservative avoids consumer-side shape drift between releases.

- **SD7 — Pre-existing dangling aspects (41–55) are wired up alongside.** The use-aspects enum had constants `AspectCodeSourceOfTruth` through `AspectQualitySemantical` defined but absent from `AllAspects` and `AspectE.String()`, leaving them un-encodable (`IsValid()` returned false). Adding the new section-uniformity aspects (56–57) requires extending `AllAspects` and `String()`; the previously-orphaned aspects are wired up in the same change so the enum is consistent and `MaxAspectExcl` matches `len(AllAspects)`.

## Alternatives

- **Statistical / ARM-based mining as the contract surface.** Mine frequent itemsets per result-set; promote stable patterns to "contract memberships." Rejected as the *primary* mechanism: ARM is parameter-sensitive (support thresholds need pinning to be reproducible), gives wrong answers for rare-but-critical fields, and conflates "what's reliably present" with "what defines an attribute." Mining is retained for drift detection.

- **Per-section role declaration only (no per-membership classifier).** Force every section into uniform role via use-aspects; require co-section segregation for mixed roles. Cleaner at the schema level but breaks for existing tables and forces a structural rewrite where one isn't needed. The classifier abstraction subsumes this — applications that want pure-section-role can implement a classifier that consults only the use-aspect.

- **Storing role on `common.MembershipDesc` directly.** Add a `MembershipRoleE` field to `MembershipDesc`. Plausible but conflates the two levels: `MembershipDesc` describes the kind of slot, not the role of the tag content that flows through it. A section may legitimately hold both roles in the same kind-slot when classified by name. Rejected by SD1.

- **Use-aspect–first design without a classifier.** Express role only through section use-aspects; require all-uniform sections. Loses the per-value flexibility (mixed-role sections, naming-convention defaults). Rejected because the value-level decision is the simpler primitive.

## Consequences

### Positive

- **Existing Leeway tables work without modification.** The classifier reads what's already there; no schema migration needed.
- **Attribute keying becomes well-defined.** Consumer-side rules for primary path → attribute key, secondary path → annotation slot are mechanical given the classifier's output.
- **Section-level use-aspect channel is the canonical home for uniformity hints.** No sidecar registries; consumers consult `sec.UseAspects` directly.
- **Application policy is pluggable.** Naming convention, registry lookup, hybrid — all are classifier implementations on the same interface.
- **Co-section segregation is an option, not a tax.** Teams that want it (rich/lean subsetting, audit overlays added by separate teams) get it for free; teams that don't aren't forced.
- **Use-aspects enum is internally consistent.** SD7's drive-by repair leaves `AllAspects` matching `MaxAspectExcl` and unblocks future additions.

### Negative

- **Classifier consistency is a per-application concern.** Two consumers reading the same data through different classifiers will produce different keying. Mitigation: schema documents may record the classifier identity (or its declared rules) so consumers can verify they're using the same policy.
- **Default classifier is opinionated.** Path-prefix `/` is the convention in most leeway-advanced examples but is not universal. Configurable `PathPrefix` covers most variations; non-prefix-based applications need a custom classifier.
- **`DefaultClassifier` lacks section-canonical-type access.** It cannot distinguish `Identity` versus `Index` param treatment for parametrized kinds; it defaults to `Identity` and applications wanting `Index` (e.g. `/embedding/_` on a homogenous-array section) wrap the default.
- **Consumers (card emitters, contract/schema generators, contract validators) need cutover.** Each consumer takes a classifier on its own schedule; until then they continue to operate without role distinction.

### Neutral

- **Classifier policy is reviewable, not enforced.** A wrong classifier produces wrong but valid output; nothing in the protocol detects "you misclassified `/hostname` as secondary." Cross-validation between schema document and emitted data is a post-hoc check.
- **Multi-primary co-grouping retains its existing shape.** The lat/lng + h3 case where both co-sections carry primary value columns is unaffected; this ADR only structures the secondary case.

### Derived practices

- **New leeway tables specify primary/secondary at design time.** Either through section use-aspects (when uniform), through naming convention (relying on `DefaultClassifier`), or through a custom classifier registered with the consumer.
- **Annotation overlays ship as secondary co-sections.** When a team adds PII flags, governance labels, or ML feature tags to an existing primary table, the natural shape is a value-less co-section with `AspectSectionMembershipsAllSecondary`.

## Open questions

Tracked as named follow-ons, not gates on this ADR:

1. **`MembershipRoleE` annotation on `MembershipDesc`.** Whether the schema struct should eventually carry an explicit per-membership role override (in addition to the classifier's computed answer). Useful for tables where the convention's defaults need targeted overrides; defer until a real use case appears.
2. **Mining tool scope.** What counts as a drift signal, what threshold triggers human review, and where the tool lives (boxer or downstream).
3. **Consumer cutover.** Card emitters, JSON Schema / data-contract generators, contract validators — each consumes the classifier on its own schedule.
4. **Section-canonical-type access in classifier.** Whether `SectionContext` should grow a canonical-type field so the default classifier can distinguish `ParamTreatmentIndex` (homogenous-array sections) from `ParamTreatmentIdentity`.
5. **`AspectSet.Contains(AspectE) bool` helper.** A direct membership test (avoiding `IterateAspects`) is convenient enough that it likely belongs alongside `IterateAspects` / `MaxEncodedAspect`; deferred because the encoder file carries a "DO NOT EDIT" disclaimer suggesting copy-paste maintenance with sister packages.

## Status

Accepted on 2026-05-01.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## Updates

### 2026-06-07 — `MembershipValue` moves to the `membership` package; this classifier stays the policy on top

`MembershipValue`, `MembershipKindE`, and `IsPlaceholder` move to a new
`membership` package (slimmed to the wire identity; the `HumanReadable*` fields
become a renderer's output), together with the string↔value mapping — see
ADR-0008. This classifier is unchanged in intent but now classifies
`membership.MembershipValue`: `MembershipRoleE` (primary/secondary) and
`ParamTreatmentE` stay here as the discrimination policy. The read-path
assumption is sharpened — only **primary** memberships are discriminative and
any one of them locates its attribute; secondaries annotate (ADR-0008).
Layering is `membership` ← `membershiprole` ← consumers.

## References

- [`../../public/semistructured/leeway/membershiprole/`](../../public/semistructured/leeway/membershiprole/) — classifier package introduced by this ADR.
- [`../../public/semistructured/leeway/useaspects/lw_useaspects_enum.go`](../../public/semistructured/leeway/useaspects/lw_useaspects_enum.go) — `AspectSectionMembershipsAllPrimary` / `AspectSectionMembershipsAllSecondary` constants.
- [`../../public/semistructured/leeway/streamreadaccess/leeway_onlineapi_types.go`](../../public/semistructured/leeway/streamreadaccess/leeway_onlineapi_types.go) — `SinkI.AddMembership*` shapes that `MembershipValue` mirrors.
- [`../../public/semistructured/leeway/common/lw_types.go`](../../public/semistructured/leeway/common/lw_types.go) — `MembershipSpecE` and `SectionDesc` (carrier of `UseAspects`).
