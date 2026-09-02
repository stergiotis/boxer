package membershiprole

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
)

// PathPrefixClassifier classifies via section use-aspect hint plus a
// path-prefix naming convention.
//
// It was called DefaultClassifier, which said where it sat rather than what it
// did — a name that reads as "the one to use unless you know better" for a
// classifier whose rule is one specific naming convention, and that a reader
// of the read path could not evaluate without opening it (ADR-0183 D6). The
// default is still nil (every membership primary); this is the classifier a
// caller opts into.
//
// The rename ships without a compatibility alias: CODINGSTANDARDS "Typing →
// No Aliases" forbids the `type X = Y` form (codelint CS008), so the old name
// is gone rather than deprecated. Every in-tree caller moved with it.
//
// Decision order:
//
//  1. If the section's UseAspects contain
//     [useaspects.AspectSectionMembershipsAllPrimary] or
//     [useaspects.AspectSectionMembershipsAllSecondary], that answer wins.
//  2. For verbatim-shaped kinds, a [PathPrefixClassifier.PathPrefix]-prefixed
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
type PathPrefixClassifier struct {
	// PathPrefix is the prefix that marks a verbatim membership as primary.
	// Empty value defaults to "/".
	PathPrefix string
}

var _ ClassifierI = PathPrefixClassifier{}
var _ PinnableI = PathPrefixClassifier{}

// Pin identifies the classifier and its one configuration knob (PinnableI).
func (inst PathPrefixClassifier) Pin() string {
	return "path-prefix(prefix=" + strconv.Quote(inst.effectivePrefix()) + ")"
}

func (inst PathPrefixClassifier) Classify(sec SectionContext, mv membership.MembershipValue) (role MembershipRoleE, paramTreatment ParamTreatmentE) {
	role = inst.classifyRole(sec, mv)
	paramTreatment = inst.classifyParamTreatment(mv)
	return
}

func (inst PathPrefixClassifier) effectivePrefix() (prefix string) {
	prefix = inst.PathPrefix
	if prefix == "" {
		prefix = "/"
	}
	return
}

func (inst PathPrefixClassifier) classifyRole(sec SectionContext, mv membership.MembershipValue) (role MembershipRoleE) {
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

func (inst PathPrefixClassifier) classifyParamTreatment(mv membership.MembershipValue) (paramTreatment ParamTreatmentE) {
	// A params blob is present exactly for the three per-row identity encodings
	// (ADR-0072) — derive it rather than re-enumerating them.
	if mv.Kind.HasParams() {
		paramTreatment = ParamTreatmentIdentity
	}
	return
}
