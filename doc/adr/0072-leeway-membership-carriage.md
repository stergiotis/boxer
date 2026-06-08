---
type: adr
status: accepted
date: 2026-06-07
reviewed-by: "p@stergiotis"
reviewed-date: 2026-06-07
---

# ADR-0072: Leeway membership carriage & representation

## Context

A **membership** is a tag attached to a section attribute. This ADR covers how a
membership rides the wire (plane **D** of the basis, ADR-0070 §Concept basis), the
identity value it carries, and how that value is rendered to a string. It supersedes
ADR-0008's D3 (channel coverage) and its Cut-2 updates, together with the membership-value
and representation half of the late membership-package update shared with ADR-0073.

Membership **meaning** — whether a tag defines or annotates an attribute — is a separate
plane and lives in ADR-0073. Carriage and meaning are independent: any channel can carry
a primary or a secondary tag.

## Decision

### D — The channel is a product of three carriage axes

A membership's **channel** is the product of:

- **cardinality** — `low` (dictionary-encodable) or `high`;
- **identity encoding** — `ref` (a registry id), `verbatim` (inline bytes), or `per-row`
  (the identity is row-carried, not schema-resolved);
- **params** — `absent` or `present`.

The realized set is the eight cells leeway's DML exposes (four scalar channels, two
parametrized, two mixed); a validator rejects unrealized combinations. The channel is an
**in-memory dispatch artifact** — its integer is never serialized. It selects exactly one
write method (`AddMembership<Suffix>P`) and one read accessor (`GetMembValue<Suffix>`) per
section.

### The identity value

`MembershipValue` is the slim wire identity — `{Kind, LowCard, Ref, Verbatim, Params}` — a
comparable struct that *is* the attribute locator key. It lives in a low-level
`membership` package that depends on nothing else in the membership stack, so both the
carriage layer and the meaning classifier (ADR-0073) build on it. The string→value
**resolver** and value→string **renderer** live here too.

### Representation is rendered, not stored

A membership's human-readable string is the **renderer's output**, not a field on the
value. The renderer holds the `RefFormatter` / `VerbatimFormatter` / `ParamsFormatter`
that turn ref ids and inline bytes into display strings; the value struct carries only
wire identity. This is the value-population vs string-representation split: producers emit
identities, consumers render them.

### The one coupling — channel constant per section

Every field targeting one section agrees on a single channel, so the read side resolves
one accessor per section instead of walking all eight. For `per-row` identity (mixed /
parametrized channels) the identity cannot be matched against a fixed schema key, so such
a section carries a **single** membership and the reader consumes its attributes in order.
This is the deliberate read-cost lever named in the Concept basis — the only cross-axis
coupling in the model.

## Alternatives

- **Keep the channel a flat eight-value enum.** Adequate as an *encoding* — the integer
  is never serialized and the suffix table is small. Rejected as the *model*: it presents
  a product of three independent choices as eight atoms. (A flat enum remains a fine
  internal representation; this ADR fixes the vocabulary, not a Go type.)
- **Mixed channels in one section** (relax the coupling). Rejected: the read-side iterator
  type would become section-content-dependent, and authors would have to declare per field
  which channel each membership reads from — paying the per-field flag twice.
- **Store the rendered string on the value.** Rejected: it makes representation stored
  state that producers and consumers must agree to populate; rendering on demand keeps the
  value a pure locator key.

## Consequences

- Every channel the protocol exposes is reachable from a DTO; the codec is the complete
  leeway producer, not just the low-card subset.
- `MembershipValue` as a comparable locator key retires the multi-membership read
  asymmetry: any primary membership locates its attribute (ADR-0073).
- Representation moving to a renderer relocates formatter injection from produce-time to
  read-time; consumers hold a renderer constructed with the appropriate formatters.

## Open questions

Tracked as named follow-ons, not gates on this ADR:

1. **The carriage axes are a queryable product, not a dispatch key.**
   `Cardinality` / `Identity` / `HasParams` name the channel's position on the
   three-axis grid, but every behavioural branch dispatches on the flat
   `MembershipChannel` enum through the denormalised `UsesCarrier` /
   `EmbedsLiteralName` / `NeedsKindVar` booleans (each equivalent to one identity
   value). The axis methods are consumed only by their own consistency test; the
   table validator keeps the booleans and the axes from drifting but reads the
   descriptor fields, not these methods. Either route one real dispatch site
   through the axes — the carrier-vs-simple branch (`Identity == PerRow` in place
   of `UsesCarrier`) or `dql.channelSpec` — or state plainly that they are a
   documented product, not a dispatch input. *(Decided 2026-06-08 — documented,
   not routed: labelled a product-for-reasoning at the accessor definition, since
   piecemeal routing relocates the denormalisation rather than removing it.
   Wiring dispatch through the axes is deferred to Open question 2, where it pays
   off once the identity axis is bijective.)*
2. **The identity axis is not bijective.** `ChannelIdentity.PerRow` collapses
   three distinct per-row encodings — a uint64 id, a `[]byte` name, and an opaque
   params blob — disambiguated only by the separate `carrierValueField` ("Id" /
   "Name" / ""). So `(cardinality, identity, params)` does not yet key the
   channel. Splitting `PerRow` into `PerRowId` / `PerRowName` / `PerRowBlob` would
   make the triple a true key and fold `carrierValueField` (and
   `CarrierValueIsBytes`) away. *(Resolved 2026-06-08 — `ChannelIdentityE` now
   aliases the shared five-way `membership.IdentityEncoding`; `CarrierValueField`
   / `CarrierValueIsBytes` derive from it and the `carrierValueField` /
   `carrierValueBytes` descriptor fields are gone.)*
3. **`MembershipKindE` duplicates the channel's identity × params.** The
   read-side `membership.MembershipKindE` (Ref / Verbatim / RefParametrized / the
   two Mixed shapes) and the write-side `mappingplan.MembershipChannel`
   independently re-encode ref / verbatim / parametrized / mixed — `Kind` is the
   channel minus cardinality. One shared identity-encoding type (plus a params
   bool), with `Kind` *derived* rather than re-declared, would retire the second
   enumeration and the hand-maintained correspondence. Pairs with #2 (the same
   five-way identity distinction). *(Resolved 2026-06-08 — `MembershipKindE` is
   retired; `MembershipValue.Kind` is now a `membership.IdentityEncoding`, the
   single five-way vocabulary the channel's `ChannelIdentityE` also aliases.
   `IdentityEncoding.HasParams` supplies the params bool, collapsing the
   classifier's param-treatment switch to a derivation.)*

## Status

Accepted on 2026-06-07. Re-cuts and supersedes parts of ADR-0008 and the
membership-value/representation half of ADR-0007's late update.

Implementation status (2026-06-07): all eight channels, the `MembershipValue` identity,
and the per-section channel coupling are **implemented**. The channel now exposes its
three carriage axes (`Cardinality` / `Identity` / `HasParams`) on the descriptor table,
init-validated against the dispatch fields, while the flat enum stays the dispatch key —
the explicit-product-without-rewrite form (a flat enum is a fine internal representation,
per Alternatives). The low-level `membership` package is **extracted** (the slim, comparable
`MembershipValue` + a `Renderer`); representation moved to read-time rendering — the driver
emits identities only and consumers (card emitters, the leeway widget) render via a
`membership.Renderer`. **Implemented** (refactor Phase 3b, 2026-06-08).

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are
append-only; supersession is recorded, not deleted.

## References

- [ADR-0070 §Concept basis](0070-leeway-entity-assembly.md) — the shared axis model.
- [ADR-0073](0073-leeway-membership-role.md) — membership meaning (role, treatment), which classifies the `MembershipValue` defined here.
- [ADR-0008](0008-leeway-marshall-extensions.md) — superseded; D3 + Cut-2 re-cut here.
- [`../../public/semistructured/leeway/dml/runtime/lw_dml_types.go`](../../public/semistructured/leeway/dml/runtime/lw_dml_types.go) — the `AddMembership*P` write methods the channel selects.
- [`../../public/semistructured/leeway/readaccess/runtime/lw_ra_rt_types.go`](../../public/semistructured/leeway/readaccess/runtime/lw_ra_rt_types.go) — the `GetMembValue*` read accessors.
