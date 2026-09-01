package useaspects

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/aspectcodec"
)

// Families is the registry of aspect families (ADR-0182 SD3): documentation
// of record and the source for exclusivity validation and, later, DQL
// authoring diagnostics. Exclusive families admit at most one member per set.
var Families = []aspectcodec.Family[AspectE]{
	{Name: "provenance", Members: []AspectE{AspectProvenanceEntity, AspectProvenanceActivity, AspectProvenanceAgent, AspectProvenanceRelation}, Exclusive: false},
	{Name: "source-of-truth", Members: []AspectE{AspectCodeSourceOfTruth, AspectDataSourceOfTruth, AspectExternalSourceOfTruth}, Exclusive: true},
	{Name: "refinement-stage", Members: []AspectE{AspectQualityStaging, AspectQualityCore, AspectQualitySemantical}, Exclusive: true},
	{Name: "attribute-history", Members: []AspectE{AspectHistoryRetained, AspectHistoryOverwritten, AspectHistoryDual}, Exclusive: true},
	{Name: "tlp", Members: []AspectE{AspectTlpClear, AspectTlpGreen, AspectTlpAmber, AspectTlpAmberStrict, AspectTlpRed}, Exclusive: true},
	{Name: "section-uniformity", Members: []AspectE{AspectSectionMembershipsAllPrimary, AspectSectionMembershipsAllSecondary}, Exclusive: true},
	{Name: "single-membership", Members: []AspectE{
		AspectSectionSingleMembershipHighCardRef,
		AspectSectionSingleMembershipHighCardVerbatim,
		AspectSectionSingleMembershipHighCardRefParametrized,
		AspectSectionSingleMembershipLowCardRef,
		AspectSectionSingleMembershipLowCardVerbatim,
		AspectSectionSingleMembershipLowCardRefParametrized,
		AspectSectionSingleMembershipMixedLowCardRefHighCardParameters,
		AspectSectionSingleMembershipMixedLowCardVerbatimHighCardParameters,
	}, Exclusive: false},
}

// CheckFamilyExclusivity rejects sets carrying more than one member of an
// exclusive family.
func CheckFamilyExclusivity(set AspectSet) (err error) {
	name := aspectcodec.FirstExclusivityViolation(Families, set.Contains)
	if name != "" {
		err = eb.Build().Str("family", name).Str("set", string(set)).Errorf("aspect family admits at most one member per set")
	}
	return
}

// SanitizeFamilyExclusivity drops all but the first-encountered member of
// each exclusive family; sample generators use it to produce valid sets.
func SanitizeFamilyExclusivity(aspects []AspectE) (out []AspectE) {
	return aspectcodec.KeepFirstPerExclusiveFamily(Families, aspects)
}
