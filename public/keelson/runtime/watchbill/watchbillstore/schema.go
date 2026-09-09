package watchbillstore

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	easp "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// DatabaseName is the default database, the runtime's own; [Layout]
// overrides it for a consumer that keeps its tables elsewhere.
const DatabaseName = "boxer"

// TableNameJob is the job table's bare name; a single lowercase word, as
// the generator requires.
const TableNameJob = "watchbill"

// TableNameEvent is the event table's bare name.
const TableNameEvent = "watchbillevent"

// TableRowConfig matches persiststore's: several attributes per row, each
// in a section of its own.
const TableRowConfig = common.TableRowConfigMultiAttributesPerRow

// GetJobSchemaInManipulator builds the job table: envelope id (the job
// id), ts (the request instant), and one section per attribute.
func GetJobSchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	manip.SetTableName(TableNameJob)
	manip.SetTableComment("watchbill jobs, one row each, claimed in place (ADR-0223 §SD1)")
	loadEnvelope(manip)
	sec := sectionOf(manip)
	// The request: what the job is, and how it may be run.
	sec("jobKind", ctabb.S, easp.AspectInterRecordLowCardinality, easp.AspectLightGeneralCompression)
	sec("jobSubject", ctabb.S, easp.AspectLightGeneralCompression)
	sec("jobQueue", ctabb.S, easp.AspectInterRecordLowCardinality, easp.AspectLightGeneralCompression)
	sec("jobPriority", ctabb.U32, easp.AspectLightGeneralCompression)
	sec("jobMaxAttempts", ctabb.U32, easp.AspectLightGeneralCompression)
	sec("jobBackoff", ctabb.S, easp.AspectInterRecordLowCardinality, easp.AspectLightGeneralCompression)
	sec("jobBackoffBaseMs", ctabb.U64, easp.AspectLightGeneralCompression)
	sec("jobTimeoutMs", ctabb.U64, easp.AspectLightGeneralCompression)
	sec("jobOwnerApp", ctabb.S, easp.AspectInterRecordLowCardinality, easp.AspectLightGeneralCompression)
	sec("jobRequesterRun", ctabb.S, easp.AspectInterRecordLowCardinality, easp.AspectLightGeneralCompression)
	sec("jobArgsKind", ctabb.S, easp.AspectInterRecordLowCardinality, easp.AspectLightGeneralCompression)
	sec("jobArgs", ctabb.Y, easp.AspectLightGeneralCompression)
	// The state: what the worker rewrites. Each is one array element the
	// claim and the transitions set and guard on.
	sec("jobState", ctabb.S, easp.AspectInterRecordLowCardinality, easp.AspectLightGeneralCompression)
	sec("jobAttempt", ctabb.U32, easp.AspectLightGeneralCompression)
	sec("jobRunAfter", ctabb.Z64, easp.AspectLightGeneralCompression)
	sec("jobWorkerRun", ctabb.S, easp.AspectInterRecordLowCardinality, easp.AspectLightGeneralCompression)
	sec("jobFinishedAt", ctabb.Z64, easp.AspectLightGeneralCompression)
	sec("jobLastError", ctabb.S, easp.AspectLightGeneralCompression)
	return
}

// GetEventSchemaInManipulator builds the event table: envelope id (the
// job id), ts (the transition instant), and the transition's attributes.
func GetEventSchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	manip.SetTableName(TableNameEvent)
	manip.SetTableComment("watchbill transitions, one row each, append-only (ADR-0223 §SD2)")
	loadEnvelope(manip)
	sec := sectionOf(manip)
	sec("eventState", ctabb.S, easp.AspectInterRecordLowCardinality, easp.AspectLightGeneralCompression)
	sec("eventAttempt", ctabb.U32, easp.AspectLightGeneralCompression)
	sec("eventWorkerRun", ctabb.S, easp.AspectInterRecordLowCardinality, easp.AspectLightGeneralCompression)
	sec("eventError", ctabb.Y, easp.AspectLightGeneralCompression)
	sec("eventNote", ctabb.S, easp.AspectLightGeneralCompression)
	return
}

// loadEnvelope declares the two plain columns both tables share. There is
// no lifecycle column: the job row is rewritten rather than tombstoned,
// and an event is never retracted.
func loadEnvelope(manip common.TableManipulatorFluidI) {
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.S).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "ts", ctabb.Z64).
		AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
}

// sectionOf returns the one-attribute section declarator persiststore
// uses: a tagged-value section named for the attribute, carrying one
// "value" column, on the "data" streaming group.
func sectionOf(manip common.TableManipulatorFluidI) func(name naming.StylableName, ct canonicaltypes.PrimitiveAstNodeI, hints ...easp.AspectE) {
	channels := []common.MembershipSpecE{
		common.MembershipSpecLowCardRef,
		common.MembershipSpecLowCardVerbatim,
		common.MembershipSpecHighCardRef,
		common.MembershipSpecMixedLowCardRefHighCardParameters,
	}
	return func(name naming.StylableName, ct canonicaltypes.PrimitiveAstNodeI, hints ...easp.AspectE) {
		s := manip.TaggedValueSection(name).
			SectionStreamingGroup("data").
			AddSectionMembership(channels...)
		s.TaggedValueColumn("value", ct).AddColumnEncodingHints(hints...)
	}
}
