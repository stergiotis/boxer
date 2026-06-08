//go:build llm_generated_opus47

package membershiprole

import (
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
)

// DefaultClassifier classifies via section use-aspect hint plus a
// path-prefix naming convention.
//
// Decision order:
//
//  1. If the section's UseAspects contain
//     [useaspects.AspectSectionMembershipsAllPrimary] or
//     [useaspects.AspectSectionMembershipsAllSecondary], that answer wins.
//  2. For verbatim-shaped kinds, a [DefaultClassifier.PathPrefix]-prefixed
//     verbatim is Primary; any other verbatim is Secondary.
//  3. For ref-shaped kinds, the default is Primary. Applications needing a
//     registry-based decision wrap or replace this classifier.
//
// Parameter treatment defaults to Identity for parametrized kinds. The
// ParamTreatmentIndex case (e.g. /embedding/_ on a homogenous-array section)
// is application-specific and requires a custom classifier; the default does
// not have section-canonical-type information available to detect it.
//
// The zero value is usable; PathPrefix defaults to "/".
type DefaultClassifier struct {
	// PathPrefix is the prefix that marks a verbatim membership as primary.
	// Empty value defaults to "/".
	PathPrefix string
}

var _ ClassifierI = DefaultClassifier{}

func (inst DefaultClassifier) Classify(sec SectionContext, mv membership.MembershipValue) (role MembershipRoleE, paramTreatment ParamTreatmentE) {
	role = inst.classifyRole(sec, mv)
	paramTreatment = inst.classifyParamTreatment(mv)
	return
}

func (inst DefaultClassifier) effectivePrefix() (prefix string) {
	prefix = inst.PathPrefix
	if prefix == "" {
		prefix = "/"
	}
	return
}

func (inst DefaultClassifier) classifyRole(sec SectionContext, mv membership.MembershipValue) (role MembershipRoleE) {
	if sec.HasUseAspect(useaspects.AspectSectionMembershipsAllPrimary) {
		role = MembershipRolePrimary
		return
	}
	if sec.HasUseAspect(useaspects.AspectSectionMembershipsAllSecondary) {
		role = MembershipRoleSecondary
		return
	}
	switch mv.Kind {
	case membership.IdentityVerbatim, membership.IdentityPerRowName:
		if strings.HasPrefix(mv.Verbatim, inst.effectivePrefix()) {
			role = MembershipRolePrimary
		} else {
			role = MembershipRoleSecondary
		}
	case membership.IdentityRef, membership.IdentityPerRowBlob, membership.IdentityPerRowId:
		role = MembershipRolePrimary
	default:
		role = MembershipRoleNone
	}
	return
}

func (inst DefaultClassifier) classifyParamTreatment(mv membership.MembershipValue) (paramTreatment ParamTreatmentE) {
	// A params blob is present exactly for the three per-row identity encodings
	// (ADR-0072) — derive it rather than re-enumerating them.
	if mv.Kind.HasParams() {
		paramTreatment = ParamTreatmentIdentity
	}
	return
}
