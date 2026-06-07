// Package membership is the low-level wire identity of a Leeway membership and
// its string representation, role-agnostic. MembershipValue is the slim,
// comparable attribute-locator key (Kind / LowCard / Ref / Verbatim / Params);
// Renderer turns it into a display string via injectable formatters. The role
// policy (primary/secondary, parameter treatment) layers on top in the
// membershiprole package. See ADR-0072 (carriage & representation) and ADR-0073
// (role); layering is membership ← membershiprole ← consumers.
package membership

// MembershipKindE discriminates the five SinkI.AddMembership* shapes. The zero
// value is MembershipKindNone.
type MembershipKindE uint8

const (
	MembershipKindNone                              MembershipKindE = 0
	MembershipKindRef                               MembershipKindE = 1
	MembershipKindVerbatim                          MembershipKindE = 2
	MembershipKindRefParametrized                   MembershipKindE = 3
	MembershipKindMixedLowCardRefHighCardParam      MembershipKindE = 4
	MembershipKindMixedLowCardVerbatimHighCardParam MembershipKindE = 5
)

// MembershipValue is the slim wire identity of one membership — a comparable
// struct that IS the attribute locator key (ADR-0072). Representation (the
// human-readable string) is the Renderer's output, not stored here.
//
// Field validity depends on Kind:
//
//   - MembershipKindRef:                              LowCard, Ref.
//   - MembershipKindVerbatim:                         LowCard, Verbatim.
//   - MembershipKindRefParametrized:                  LowCard, Ref, Params.
//   - MembershipKindMixedLowCardRefHighCardParam:     Ref, Params.
//   - MembershipKindMixedLowCardVerbatimHighCardParam: Verbatim, Params.
//
// A zero-valued MembershipValue has Kind == MembershipKindNone and reads as "no
// membership"; consumers treat it as empty input rather than panicking.
type MembershipValue struct {
	Kind     MembershipKindE
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
	case MembershipKindRef, MembershipKindRefParametrized, MembershipKindMixedLowCardRefHighCardParam:
		placeholder = mv.Ref == 0 && mv.Params == ""
	case MembershipKindMixedLowCardVerbatimHighCardParam:
		placeholder = mv.Verbatim == "" && mv.Params == ""
	case MembershipKindVerbatim:
		placeholder = mv.Verbatim == ""
	}
	return
}
