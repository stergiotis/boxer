// Package membership is the low-level wire identity of a Leeway membership and
// its string representation, role-agnostic. MembershipValue is the slim,
// comparable attribute-locator key (Kind / LowCard / Ref / Verbatim / Params);
// Renderer turns it into a display string via injectable formatters. The role
// policy (primary/secondary, parameter treatment) layers on top in the
// membershiprole package. See ADR-0072 (carriage & representation) and ADR-0073
// (role); layering is membership ← membershiprole ← consumers.
package membership

// MembershipValue is the slim wire identity of one membership — a comparable
// struct that IS the attribute locator key (ADR-0072). Representation (the
// human-readable string) is the Renderer's output, not stored here.
//
// Field validity depends on Kind — an IdentityEncoding, the vocabulary shared
// with the write-side channel (see identity.go):
//
//   - IdentityRef:        LowCard, Ref.
//   - IdentityVerbatim:   LowCard, Verbatim.
//   - IdentityPerRowBlob: LowCard, Params (the parametrized carrier; Ref is always 0).
//   - IdentityPerRowId:   Ref, Params (the mixed-ref carrier).
//   - IdentityPerRowName: Verbatim, Params (the mixed-verbatim carrier).
//
// A zero-valued MembershipValue has Kind == IdentityNone and reads as "no
// membership"; consumers treat it as empty input rather than panicking.
type MembershipValue struct {
	Kind     IdentityEncoding
	LowCard  bool
	Ref      uint64
	Verbatim string
	Params   string
}

// IsPlaceholder reports whether mv is a driver-emitted "spec-slot is empty" tag
// rather than a real membership.
//
// The Leeway driver emits one AddMembership* call per membership-spec slot
// declared on a section, regardless of whether the slot carries data for the
// current attribute. Empty slots manifest as zero-ref / empty-verbatim /
// empty-params payloads; treating them as real memberships would make every
// such attribute resolve to the same "ref:0" key. Keyed off the wire identity
// alone (ADR-0073) — the human-readable fields it formerly also checked were
// redundant with these (an empty slot formats to an empty string).
func IsPlaceholder(mv MembershipValue) (placeholder bool) {
	switch mv.Kind {
	case IdentityRef, IdentityPerRowBlob, IdentityPerRowId:
		placeholder = mv.Ref == 0 && mv.Params == ""
	case IdentityPerRowName:
		placeholder = mv.Verbatim == "" && mv.Params == ""
	case IdentityVerbatim:
		placeholder = mv.Verbatim == ""
	}
	return
}
