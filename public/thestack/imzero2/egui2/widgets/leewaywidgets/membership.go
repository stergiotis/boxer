//go:build llm_generated_opus47

package leewaywidgets

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/membershiprole"
)

// isPlaceholderMembership detects driver-emitted "spec-slot is empty" tags.
//
// The Leeway driver emits one AddMembership* call per membership-spec slot
// declared on the section, regardless of whether the slot carries data for
// the current attribute. Empty slots manifest as zero-ref / empty-verbatim /
// empty-params payloads. Treating them as real memberships would make every
// such attribute resolve to the same "ref:0" key.
func isPlaceholderMembership(mv membershiprole.MembershipValue) (placeholder bool) {
	switch mv.Kind {
	case membershiprole.MembershipKindRef, membershiprole.MembershipKindRefParametrized:
		placeholder = mv.Ref == 0 && mv.HumanReadableRef == "" && mv.Params == "" && mv.HumanReadableParams == ""
	case membershiprole.MembershipKindMixedLowCardRefHighCardParam:
		placeholder = mv.Ref == 0 && mv.HumanReadableRef == "" && mv.Params == "" && mv.HumanReadableParams == ""
	case membershiprole.MembershipKindMixedLowCardVerbatimHighCardParam:
		placeholder = mv.Verbatim == "" && mv.HumanReadableValue == "" && mv.Params == "" && mv.HumanReadableParams == ""
	case membershiprole.MembershipKindVerbatim:
		placeholder = mv.Verbatim == "" && mv.HumanReadableValue == ""
	}
	return
}
