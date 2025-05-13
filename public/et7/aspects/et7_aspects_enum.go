package aspects

import "slices"

const (
	DataAspectIndefinite         DataAspectE = 0
	DataAspectCompliance         DataAspectE = 1
	DataAspectRisk               DataAspectE = 2
	DataAspectPrivacy            DataAspectE = 3
	DataAspectProvenanceEntity   DataAspectE = 4 // see https://www.w3.org/TR/prov-overview/
	DataAspectProvenanceActivity DataAspectE = 5 // see https://www.w3.org/TR/prov-overview/
	DataAspectProvenanceAgent    DataAspectE = 6 // see https://www.w3.org/TR/prov-overview/
	DataAspectProvenanceRelation DataAspectE = 7 // see https://www.w3.org/TR/prov-overview/
	DataAspectLineage            DataAspectE = 8
	DataAspectCatalog            DataAspectE = 9
	DataAspectSecurity           DataAspectE = 10
	DataAspectAuthorization      DataAspectE = 11
	DataAspectAccess             DataAspectE = 12
	DataAspectAudit              DataAspectE = 13
	DataAspectQuality            DataAspectE = 14
	DataAspectPolicy             DataAspectE = 15
	DataAspectOwnership          DataAspectE = 16
	DataAspectMetrics            DataAspectE = 17
	DataAspectLog                DataAspectE = 18
	DataAspectCollaboration      DataAspectE = 19
	DataAspectInterop            DataAspectE = 20
	DataAspectEvolution          DataAspectE = 21
	DataAspectClassification     DataAspectE = 22
	DataAspectTaxonomy           DataAspectE = 23
	DataAspectUnit               DataAspectE = 24 // e.g. SI unit
	DataAspectProfile            DataAspectE = 25 // i.e. performance profiling data
	DataAspectSpatial            DataAspectE = 26
	DataAspectOrgUnit            DataAspectE = 27
	DataAspectOrgRole            DataAspectE = 28
	DataAspectOrgProcess         DataAspectE = 29
	DataAspectOrgFinance         DataAspectE = 30
	DataAspectBusinessAsset      DataAspectE = 31
	DataAspectBusinessPartner    DataAspectE = 32
	DataAspectBusinessActivity   DataAspectE = 33
	DataAspectBusinessChannel    DataAspectE = 34
	DataAspectWorkflow           DataAspectE = 35
	DataAspectLinking            DataAspectE = 36 // i.e. references, hyperlinks, graph edges, hyper edges ...
	DataAspectTesting            DataAspectE = 37
	DataAspectDevice             DataAspectE = 38
)

var MaxDataAspectExcl = slices.Max(AllDataAspects) + 1

var AllDataAspects = []DataAspectE{
	DataAspectIndefinite,
	DataAspectCompliance,
	DataAspectRisk,
	DataAspectPrivacy,
	DataAspectProvenanceEntity,
	DataAspectProvenanceActivity,
	DataAspectProvenanceAgent,
	DataAspectProvenanceRelation,
	DataAspectLineage,
	DataAspectCatalog,
	DataAspectSecurity,
	DataAspectAuthorization,
	DataAspectAccess,
	DataAspectAudit,
	DataAspectQuality,
	DataAspectPolicy,
	DataAspectOwnership,
	DataAspectMetrics,
	DataAspectLog,
	DataAspectCollaboration,
	DataAspectInterop,
	DataAspectEvolution,
	DataAspectClassification,
	DataAspectTaxonomy,
	DataAspectUnit,
	DataAspectProfile,
	DataAspectSpatial,
	DataAspectOrgUnit,
	DataAspectOrgRole,
	DataAspectOrgProcess,
	DataAspectOrgFinance,
	DataAspectBusinessAsset,
	DataAspectBusinessPartner,
	DataAspectBusinessActivity,
	DataAspectBusinessChannel,
	DataAspectWorkflow,
	DataAspectLinking,
	DataAspectTesting,
	DataAspectDevice,
}

const InvalidAspectEnumValueString = "<invalid DataAspectE>"

func (inst DataAspectE) IsValid() bool {
	return inst < MaxDataAspectExcl
}
func (inst DataAspectE) String() string {
	switch inst {
	case DataAspectIndefinite:
		return "indefinite"
	case DataAspectCompliance:
		return "compliance"
	case DataAspectRisk:
		return "risk"
	case DataAspectPrivacy:
		return "privacy"
	case DataAspectProvenanceEntity:
		return "provenance-entity"
	case DataAspectProvenanceActivity:
		return "provenance-activity"
	case DataAspectProvenanceAgent:
		return "provenance-agent"
	case DataAspectProvenanceRelation:
		return "provenance-relation"
	case DataAspectLineage:
		return "lineage"
	case DataAspectCatalog:
		return "catalog"
	case DataAspectSecurity:
		return "security"
	case DataAspectAuthorization:
		return "authorization"
	case DataAspectAccess:
		return "access"
	case DataAspectAudit:
		return "audit"
	case DataAspectQuality:
		return "quality"
	case DataAspectPolicy:
		return "policy"
	case DataAspectOwnership:
		return "ownership"
	case DataAspectMetrics:
		return "metrics"
	case DataAspectLog:
		return "log"
	case DataAspectCollaboration:
		return "collaboration"
	case DataAspectInterop:
		return "interop"
	case DataAspectEvolution:
		return "change-evolution"
	case DataAspectClassification:
		return "classification"
	case DataAspectTaxonomy:
		return "taxonomy"
	case DataAspectUnit:
		return "unit"
	case DataAspectProfile:
		return "profile"
	case DataAspectSpatial:
		return "spatial"
	case DataAspectOrgUnit:
		return "organization-unit"
	case DataAspectOrgRole:
		return "organization-role"
	case DataAspectOrgProcess:
		return "organization-process"
	case DataAspectOrgFinance:
		return "organization-finance"
	case DataAspectBusinessAsset:
		return "business-asset"
	case DataAspectBusinessPartner:
		return "business-partner"
	case DataAspectBusinessActivity:
		return "business-activity"
	case DataAspectBusinessChannel:
		return "business-channel"
	case DataAspectLinking:
		return "linking"
	case DataAspectTesting:
		return "testing"
	case DataAspectWorkflow:
		return "workflow"
	case DataAspectDevice:
		return "device"
	}
	return InvalidAspectEnumValueString
}
