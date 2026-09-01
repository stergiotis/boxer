// Package useaspects is the closed vocabulary of section-usage aspects
// (ADR-0182). Admission criterion: an aspect family is admissible when its
// meaning is anchored in mathematics, a long-lived open standard, a practice
// predating the current tooling generation, or a format the engine itself
// commits to; its domain is closed under that anchor, or it is a genuinely
// independent boolean. Open-domain technique-, tier- or brand-shaped
// information belongs in canonical types, TableOptions or the catalog — not
// here. Numbering is the wire format (segments in physical column names):
// append-only between migration windows, family-grouped as of v2.
package useaspects

import "slices"

const (
	// Governance data kinds. Authorization = grants and policy (who may);
	// Access = access records (who did); Audit = examinations of controls
	// (what was checked).

	AspectCompliance    AspectE = 0
	AspectRisk          AspectE = 1
	AspectPrivacy       AspectE = 2
	AspectSecurity      AspectE = 3
	AspectAuthorization AspectE = 4
	AspectAccess        AspectE = 5
	AspectAudit         AspectE = 6
	AspectQuality       AspectE = 7
	AspectPolicy        AspectE = 8
	AspectOwnership     AspectE = 9

	// W3C PROV kinds (record-level provenance). Lineage, by contrast, is
	// artifact/column-level derivation topology between datasets.

	AspectProvenanceEntity   AspectE = 10 // see https://www.w3.org/TR/prov-overview/
	AspectProvenanceActivity AspectE = 11 // see https://www.w3.org/TR/prov-overview/
	AspectProvenanceAgent    AspectE = 12 // see https://www.w3.org/TR/prov-overview/
	AspectProvenanceRelation AspectE = 13 // see https://www.w3.org/TR/prov-overview/
	AspectLineage            AspectE = 14

	// Classification = the section carries class labels assigned to things;
	// Taxonomy = the section carries the classification system itself.

	AspectClassification AspectE = 15
	AspectTaxonomy       AspectE = 16

	AspectCatalog       AspectE = 17
	AspectUnit          AspectE = 18 // e.g. SI unit
	AspectSpatial       AspectE = 19
	AspectWorkflow      AspectE = 20
	AspectLinking       AspectE = 21 // i.e. references, hyperlinks, graph edges, hyper edges ...
	AspectTesting       AspectE = 22
	AspectDevice        AspectE = 23
	AspectDocumentation AspectE = 24
	AspectCollaboration AspectE = 25
	AspectInterop       AspectE = 26
	AspectEvolution     AspectE = 27

	AspectMetrics AspectE = 28
	AspectLog     AspectE = 29
	AspectProfile AspectE = 30 // i.e. performance profiling data

	// Source-of-truth authority (exclusive).

	AspectCodeSourceOfTruth     AspectE = 31
	AspectDataSourceOfTruth     AspectE = 32
	AspectExternalSourceOfTruth AspectE = 33

	// Refinement stages (exclusive): Staging = raw as received; Core =
	// cleansed and conformed; Semantical = semantically modeled for
	// consumption.

	AspectQualityStaging    AspectE = 34
	AspectQualityCore       AspectE = 35
	AspectQualitySemantical AspectE = 36

	// Attribute history treatment (exclusive): what happens to a prior value
	// when a new one arrives. Values that never change carry the value-level
	// Immutable aspect instead.

	AspectHistoryRetained    AspectE = 37 // changes append; prior values stay readable
	AspectHistoryOverwritten AspectE = 38 // only the current value is kept
	AspectHistoryDual        AspectE = 39 // a current view is maintained alongside retained history

	// Traffic Light Protocol dissemination marking (exclusive), per FIRST
	// TLP 2.0 (https://www.first.org/tlp/, authoritative August 2022; 2.0
	// renamed 1.0's WHITE to CLEAR). States who may receive the data.

	AspectTlpClear       AspectE = 40 // no limit on disclosure
	AspectTlpGreen       AspectE = 41 // limited disclosure, community
	AspectTlpAmber       AspectE = 42 // limited disclosure, organization and its clients on a need-to-know basis
	AspectTlpAmberStrict AspectE = 43 // limited disclosure, organization only
	AspectTlpRed         AspectE = 44 // named recipients only, no further disclosure

	// Section-uniformity hints for the membership-role classifier
	// (exclusive; ADR-0007/0073).

	AspectSectionMembershipsAllPrimary   AspectE = 45 // every membership in this tagged-value section defines an attribute's identity
	AspectSectionMembershipsAllSecondary AspectE = 46 // every membership in this tagged-value section annotates an existing attribute

	// Single-membership declarations, one independent boolean per membership
	// channel (ADR-0213): every attribute of the section carries EXACTLY ONE
	// membership on the declared channel. The declaration is a writable
	// schema statement of the layout the read side already knows how to
	// exploit — the channel's <role>card column is omitted, the flattened
	// membership array is co-indexed with the value array, and the bare
	// `value[indexOf(ident, lit)]` form is licensed (ADR-0066/ADR-0181's
	// structural fast path). The DML enforces the arity at write time.
	// Meaningful only with the matching channel in the section's
	// MembershipSpec; the table validator rejects a declaration without its
	// channel.

	AspectSectionSingleMembershipHighCardRef                            AspectE = 47
	AspectSectionSingleMembershipHighCardVerbatim                       AspectE = 48
	AspectSectionSingleMembershipHighCardRefParametrized                AspectE = 49
	AspectSectionSingleMembershipLowCardRef                             AspectE = 50
	AspectSectionSingleMembershipLowCardVerbatim                        AspectE = 51
	AspectSectionSingleMembershipLowCardRefParametrized                 AspectE = 52
	AspectSectionSingleMembershipMixedLowCardRefHighCardParameters      AspectE = 53
	AspectSectionSingleMembershipMixedLowCardVerbatimHighCardParameters AspectE = 54
)

var MaxAspectExcl = slices.Max(AllAspects) + 1

var AllAspects = []AspectE{
	AspectCompliance,
	AspectRisk,
	AspectPrivacy,
	AspectSecurity,
	AspectAuthorization,
	AspectAccess,
	AspectAudit,
	AspectQuality,
	AspectPolicy,
	AspectOwnership,
	AspectProvenanceEntity,
	AspectProvenanceActivity,
	AspectProvenanceAgent,
	AspectProvenanceRelation,
	AspectLineage,
	AspectClassification,
	AspectTaxonomy,
	AspectCatalog,
	AspectUnit,
	AspectSpatial,
	AspectWorkflow,
	AspectLinking,
	AspectTesting,
	AspectDevice,
	AspectDocumentation,
	AspectCollaboration,
	AspectInterop,
	AspectEvolution,
	AspectMetrics,
	AspectLog,
	AspectProfile,
	AspectCodeSourceOfTruth,
	AspectDataSourceOfTruth,
	AspectExternalSourceOfTruth,
	AspectQualityStaging,
	AspectQualityCore,
	AspectQualitySemantical,
	AspectHistoryRetained,
	AspectHistoryOverwritten,
	AspectHistoryDual,
	AspectTlpClear,
	AspectTlpGreen,
	AspectTlpAmber,
	AspectTlpAmberStrict,
	AspectTlpRed,
	AspectSectionMembershipsAllPrimary,
	AspectSectionMembershipsAllSecondary,
	AspectSectionSingleMembershipHighCardRef,
	AspectSectionSingleMembershipHighCardVerbatim,
	AspectSectionSingleMembershipHighCardRefParametrized,
	AspectSectionSingleMembershipLowCardRef,
	AspectSectionSingleMembershipLowCardVerbatim,
	AspectSectionSingleMembershipLowCardRefParametrized,
	AspectSectionSingleMembershipMixedLowCardRefHighCardParameters,
	AspectSectionSingleMembershipMixedLowCardVerbatimHighCardParameters,
}

const InvalidAspectEnumValueString = "<invalid AspectE>"

func (inst AspectE) IsValid() bool {
	return inst < MaxAspectExcl
}
func (inst AspectE) String() string {
	switch inst {
	case AspectCompliance:
		return "compliance"
	case AspectRisk:
		return "risk"
	case AspectPrivacy:
		return "privacy"
	case AspectSecurity:
		return "security"
	case AspectAuthorization:
		return "authorization"
	case AspectAccess:
		return "access"
	case AspectAudit:
		return "audit"
	case AspectQuality:
		return "quality"
	case AspectPolicy:
		return "policy"
	case AspectOwnership:
		return "ownership"
	case AspectProvenanceEntity:
		return "provenance-entity"
	case AspectProvenanceActivity:
		return "provenance-activity"
	case AspectProvenanceAgent:
		return "provenance-agent"
	case AspectProvenanceRelation:
		return "provenance-relation"
	case AspectLineage:
		return "lineage"
	case AspectClassification:
		return "classification"
	case AspectTaxonomy:
		return "taxonomy"
	case AspectCatalog:
		return "catalog"
	case AspectUnit:
		return "unit"
	case AspectSpatial:
		return "spatial"
	case AspectWorkflow:
		return "workflow"
	case AspectLinking:
		return "linking"
	case AspectTesting:
		return "testing"
	case AspectDevice:
		return "device"
	case AspectDocumentation:
		return "documentation"
	case AspectCollaboration:
		return "collaboration"
	case AspectInterop:
		return "interop"
	case AspectEvolution:
		return "evolution"
	case AspectMetrics:
		return "metrics"
	case AspectLog:
		return "log"
	case AspectProfile:
		return "profile"
	case AspectCodeSourceOfTruth:
		return "code-source-of-truth"
	case AspectDataSourceOfTruth:
		return "data-source-of-truth"
	case AspectExternalSourceOfTruth:
		return "external-source-of-truth"
	case AspectQualityStaging:
		return "quality-staging"
	case AspectQualityCore:
		return "quality-core"
	case AspectQualitySemantical:
		return "quality-semantical"
	case AspectHistoryRetained:
		return "history-retained"
	case AspectHistoryOverwritten:
		return "history-overwritten"
	case AspectHistoryDual:
		return "history-dual"
	case AspectTlpClear:
		return "tlp-clear"
	case AspectTlpGreen:
		return "tlp-green"
	case AspectTlpAmber:
		return "tlp-amber"
	case AspectTlpAmberStrict:
		return "tlp-amber-strict"
	case AspectTlpRed:
		return "tlp-red"
	case AspectSectionMembershipsAllPrimary:
		return "section-memberships-all-primary"
	case AspectSectionMembershipsAllSecondary:
		return "section-memberships-all-secondary"
	case AspectSectionSingleMembershipHighCardRef:
		return "single-membership-high-card-ref"
	case AspectSectionSingleMembershipHighCardVerbatim:
		return "single-membership-high-card-verbatim"
	case AspectSectionSingleMembershipHighCardRefParametrized:
		return "single-membership-high-card-ref-parametrized"
	case AspectSectionSingleMembershipLowCardRef:
		return "single-membership-low-card-ref"
	case AspectSectionSingleMembershipLowCardVerbatim:
		return "single-membership-low-card-verbatim"
	case AspectSectionSingleMembershipLowCardRefParametrized:
		return "single-membership-low-card-ref-parametrized"
	case AspectSectionSingleMembershipMixedLowCardRefHighCardParameters:
		return "single-membership-mixed-low-card-ref"
	case AspectSectionSingleMembershipMixedLowCardVerbatimHighCardParameters:
		return "single-membership-mixed-low-card-verbatim"
	}
	return InvalidAspectEnumValueString
}
func (inst AspectE) Value() uint8 {
	return uint8(inst)
}
