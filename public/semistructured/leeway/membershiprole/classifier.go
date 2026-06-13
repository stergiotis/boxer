package membershiprole

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
)

// MembershipRoleE answers whether a membership defines an attribute's identity
// or annotates an existing one.
type MembershipRoleE uint8

const (
	MembershipRoleNone      MembershipRoleE = 0
	MembershipRolePrimary   MembershipRoleE = 1
	MembershipRoleSecondary MembershipRoleE = 2
)

// ParamTreatmentE answers whether a membership's parameters contribute to
// attribute identity (each (path, param) is its own attribute), are dimensional
// indices within a single attribute, or are absent.
type ParamTreatmentE uint8

const (
	ParamTreatmentNone     ParamTreatmentE = 0
	ParamTreatmentIdentity ParamTreatmentE = 1
	ParamTreatmentIndex    ParamTreatmentE = 2
)

// SectionContext provides per-section state that a classifier may consult.
// UseAspects carries the section-level uniformity hints
// [useaspects.AspectSectionMembershipsAllPrimary] /
// [useaspects.AspectSectionMembershipsAllSecondary]; the section name is
// available for applications that key their policy on it.
type SectionContext struct {
	Name       naming.StylableName
	UseAspects useaspects.AspectSet
}

// HasUseAspect reports whether the section's UseAspects set contains the given
// aspect. Returns false on encoding errors or when UseAspects is empty.
func (inst SectionContext) HasUseAspect(target useaspects.AspectE) (has bool) {
	return inst.UseAspects.Contains(target)
}

// ClassifierI decides the role and parameter treatment of a single membership
// instance.
//
// Implementations must be deterministic for a given (sec, mv) pair: a
// classifier returning Primary on one call and Secondary on a later call with
// the same input is a contract violation that produces inconsistent
// downstream output (mixed byAttribute keys, drifting labels, etc.).
type ClassifierI interface {
	Classify(sec SectionContext, mv membership.MembershipValue) (role MembershipRoleE, paramTreatment ParamTreatmentE)
}
