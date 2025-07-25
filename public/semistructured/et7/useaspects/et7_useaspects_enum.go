package useaspects

import "slices"

const (
	AspectIndefinite         AspectE = 0
	AspectCompliance         AspectE = 1
	AspectRisk               AspectE = 2
	AspectPrivacy            AspectE = 3
	AspectProvenanceEntity   AspectE = 4 // see https://www.w3.org/TR/prov-overview/
	AspectProvenanceActivity AspectE = 5 // see https://www.w3.org/TR/prov-overview/
	AspectProvenanceAgent    AspectE = 6 // see https://www.w3.org/TR/prov-overview/
	AspectProvenanceRelation AspectE = 7 // see https://www.w3.org/TR/prov-overview/
	AspectLineage            AspectE = 8
	AspectCatalog            AspectE = 9
	AspectSecurity           AspectE = 10
	AspectAuthorization      AspectE = 11
	AspectAccess             AspectE = 12
	AspectAudit              AspectE = 13
	AspectQuality            AspectE = 14
	AspectPolicy             AspectE = 15
	AspectOwnership          AspectE = 16
	AspectMetrics            AspectE = 17
	AspectLog                AspectE = 18
	AspectCollaboration      AspectE = 19
	AspectInterop            AspectE = 20
	AspectEvolution          AspectE = 21
	AspectClassification     AspectE = 22
	AspectTaxonomy           AspectE = 23
	AspectUnit               AspectE = 24 // e.g. SI unit
	AspectProfile            AspectE = 25 // i.e. performance profiling data
	AspectSpatial            AspectE = 26
	AspectOrgUnit            AspectE = 27
	AspectOrgRole            AspectE = 28
	AspectOrgProcess         AspectE = 29
	AspectOrgFinance         AspectE = 30
	AspectBusinessAsset      AspectE = 31
	AspectBusinessPartner    AspectE = 32
	AspectBusinessActivity   AspectE = 33
	AspectBusinessChannel    AspectE = 34
	AspectWorkflow           AspectE = 35
	AspectLinking            AspectE = 36 // i.e. references, hyperlinks, graph edges, hyper edges ...
	AspectTesting            AspectE = 37
	AspectDevice             AspectE = 38
	AspectDocumentation      AspectE = 39
)

var MaxAspectExcl = slices.Max(AllAspects) + 1

var AllAspects = []AspectE{
	AspectIndefinite,
	AspectCompliance,
	AspectRisk,
	AspectPrivacy,
	AspectProvenanceEntity,
	AspectProvenanceActivity,
	AspectProvenanceAgent,
	AspectProvenanceRelation,
	AspectLineage,
	AspectCatalog,
	AspectSecurity,
	AspectAuthorization,
	AspectAccess,
	AspectAudit,
	AspectQuality,
	AspectPolicy,
	AspectOwnership,
	AspectMetrics,
	AspectLog,
	AspectCollaboration,
	AspectInterop,
	AspectEvolution,
	AspectClassification,
	AspectTaxonomy,
	AspectUnit,
	AspectProfile,
	AspectSpatial,
	AspectOrgUnit,
	AspectOrgRole,
	AspectOrgProcess,
	AspectOrgFinance,
	AspectBusinessAsset,
	AspectBusinessPartner,
	AspectBusinessActivity,
	AspectBusinessChannel,
	AspectWorkflow,
	AspectLinking,
	AspectTesting,
	AspectDevice,
	AspectDocumentation,
}

const InvalidAspectEnumValueString = "<invalid AspectE>"

func (inst AspectE) IsValid() bool {
	return inst < MaxAspectExcl
}
func (inst AspectE) String() string {
	switch inst {
	case AspectIndefinite:
		return "indefinite"
	case AspectCompliance:
		return "compliance"
	case AspectRisk:
		return "risk"
	case AspectPrivacy:
		return "privacy"
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
	case AspectCatalog:
		return "catalog"
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
	case AspectMetrics:
		return "metrics"
	case AspectLog:
		return "log"
	case AspectCollaboration:
		return "collaboration"
	case AspectInterop:
		return "interop"
	case AspectEvolution:
		return "change-evolution"
	case AspectClassification:
		return "classification"
	case AspectTaxonomy:
		return "taxonomy"
	case AspectUnit:
		return "unit"
	case AspectProfile:
		return "profile"
	case AspectSpatial:
		return "spatial"
	case AspectOrgUnit:
		return "organization-unit"
	case AspectOrgRole:
		return "organization-role"
	case AspectOrgProcess:
		return "organization-process"
	case AspectOrgFinance:
		return "organization-finance"
	case AspectBusinessAsset:
		return "business-asset"
	case AspectBusinessPartner:
		return "business-partner"
	case AspectBusinessActivity:
		return "business-activity"
	case AspectBusinessChannel:
		return "business-channel"
	case AspectLinking:
		return "linking"
	case AspectTesting:
		return "testing"
	case AspectWorkflow:
		return "workflow"
	case AspectDevice:
		return "device"
	case AspectDocumentation:
		return "documentation"
	}
	return InvalidAspectEnumValueString
}
