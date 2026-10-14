// Package example is a store of audit events generated the way a consumer
// below the runtime generates one, and its binding to the recorder: a
// table of its own, boxer's origin and audit-event components, the seal
// component the evidence profile emits, the canonical wire, and membership
// ids of the table's own. The recorder's tests and its conformance test
// run against it; a consumer copies its shape.
package example

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	easp "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

// The table the store is generated under.
const (
	TableName = "auditexample"
	Database  = "auditrec"
	// Table is the database-qualified table the store writes.
	Table = Database + "." + TableName
	// TableRowConfig is the row shape the store is generated under.
	TableRowConfig = common.TableRowConfigMultiAttributesPerRow
)

// SchemaInManipulator builds the table: a uint64 id as the Key, a
// timestamp as the Order, the recorder's natural key as a pass-through
// envelope column, and the three facts data sections the components bind
// by name — symbol, stringArray, u64Array.
func SchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	manip.SetTableName(TableName)
	manip.SetTableComment("auditrec example: boxer's origin and audit event, sealed per flush")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64).
		AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
	manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "ts", ctabb.Z64).
		AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "naturalKey", ctabb.Y).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	specs := []common.MembershipSpecE{
		common.MembershipSpecLowCardRef,
		common.MembershipSpecHighCardRef,
		common.MembershipSpecMixedLowCardRefHighCardParameters,
	}
	array := func(ct canonicaltypes.PrimitiveAstNodeI) canonicaltypes.PrimitiveAstNodeI {
		return canonicaltypes.PromoteScalarPrim(ct, canonicaltypes.ScalarModifierHomogenousArray)
	}
	manip.TaggedValueSection("symbol").SectionStreamingGroup("data").AddSectionMembership(specs...).
		TaggedValueColumn("value", ctabb.S).
		AddColumnEncodingHints(easp.AspectInterRecordLowCardinality, easp.AspectIntraRecordLowCardinality, easp.AspectLightGeneralCompression)
	manip.TaggedValueSection("stringArray").SectionStreamingGroup("data").AddSectionMembership(specs...).
		TaggedValueColumn("value", array(ctabb.S)).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	manip.TaggedValueSection("u64Array").SectionStreamingGroup("data").AddSectionMembership(specs...).
		TaggedValueColumn("value", array(ctabb.U64)).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	return
}

// genIds is the membership-id snapshot the store is generated under: the
// components' memberships as their `lw:` tags spell them, under ids of
// this table's own. The generated ExampleMembershipIds is the store's copy.
var genIds = map[string]uint64{
	"runtimeRun":              7001,
	"runtimeApp":              7002,
	"runtimeLifecycleTileKey": 7003,
	"runtimeKindAuditEvent":   7101,
	"auditEventDomain":        7102,
	"auditEventAction":        7103,
	"auditEventOutcome":       7104,
	"auditEventPrincipal":     7105,
	"auditEventPrincipalBy":   7106,
	"auditEventOriginBy":      7107,
	"auditEventPurpose":       7108,
	"auditEventNode":          7109,
	"auditEventSubject":       7110,
	"auditEventRetention":     7111,
	"auditEventRefTypes":      7112,
	"auditEventRefValues":     7113,
	"auditEventAttrKeys":      7114,
	"auditEventAttrValues":    7115,
	"auditEventPrincipalVia":  7116,
	"evidenceKindSeal":        7201,
	"evidenceSealStream":      7202,
	"evidenceSealPrev":        7203,
	"evidenceSealDigest":      7204,
	"evidenceSealScheme":      7205,
	"evidenceSealRowIds":      7206,
	"evidenceSealRowDigests":  7207,
}
