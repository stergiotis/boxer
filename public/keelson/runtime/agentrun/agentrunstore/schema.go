package agentrunstore

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	easp "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// DatabaseName is the default database, the runtime's own.
const DatabaseName = "boxer"

// TableNameRun is the run table's bare name and TableNameEntry the entry
// table's; single lowercase words, as the generator requires.
const (
	TableNameRun   = "agentrun"
	TableNameEntry = "agentrunentry"
)

// EntryOrdinalColumn is the ledger's and the vault's Order role column:
// the entry's ordinal within its run.
const EntryOrdinalColumn = "ordinal"

// EntryRevisionColumn is the entry table's Revision role column
// (gen.Roles.Revision): reads keep the highest revision of each ordinal.
const EntryRevisionColumn = "revision"

// TableRowConfig matches watchbill's: several attributes per row, each in
// a section of its own.
const TableRowConfig = common.TableRowConfigMultiAttributesPerRow

var low = []easp.AspectE{easp.AspectInterRecordLowCardinality, easp.AspectLightGeneralCompression}
var plain = []easp.AspectE{easp.AspectLightGeneralCompression}

// GetRunSchemaInManipulator builds the run table: envelope id (the run
// id), ts (the write instant), lifecycle for latest-wins, and one section
// per attribute.
func GetRunSchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	manip.SetTableName(TableNameRun)
	manip.SetTableComment("agent runs, one row each, latest-wins (ADR-0308 §SD1)")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.S).AddColumnEncodingHints(plain...)
	manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "ts", ctabb.Z64).AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
	manip.PlainValueColumn(common.PlainItemTypeEntityLifecycle, "lifecycle", ctabb.U8).AddColumnEncodingHints(plain...)
	sec := sectionOf(manip)
	sec("runCell", ctabb.S, low...)
	sec("runConversation", ctabb.S, plain...)
	sec("runOwnerApp", ctabb.S, low...)
	sec("runCoordinator", ctabb.S, low...)
	sec("runState", ctabb.S, low...)
	sec("runFingerprint", ctabb.S, low...)
	sec("runBuild", ctabb.S, low...)
	sec("runProfile", ctabb.S, low...)
	sec("runSettings", ctabb.S, plain...)
	sec("runLabelLevel", ctabb.U32, plain...)
	sec("runLabelTier", ctabb.U32, plain...)
	sec("runLabelExposure", ctabb.S, low...)
	sec("runLabelOrigin", ctabb.S, low...)
	sec("runCreated", ctabb.Z64, plain...)
	sec("runLastOrdinal", ctabb.U32, plain...)
	sec("runClosed", ctabb.Z64, plain...)
	sec("runDigest", ctabb.S, plain...)
	sec("runHead", ctabb.S, plain...)
	sec("runVersion", ctabb.U32, plain...)
	sec("runDeletedBy", ctabb.S, low...)
	sec("runDeletedAt", ctabb.Z64, plain...)
	sec("runHolder", ctabb.S, low...)
	sec("runHolderPid", ctabb.U32, plain...)
	sec("runEpoch", ctabb.U32, plain...)
	return
}

// GetEntrySchemaInManipulator builds the entry table, the ledger: an
// envelope of its own — id (the run id, the Key), ordinal (the Order),
// revision (the Revision role: a tombstone is the entry at its next
// revision, ADR-0308 §SD8) — over the boxer.facts data sections, so the
// ledger reads with the trail's read surface (plan002 B0). Bodies and
// calls are the vault's (ADR-0320 §SD1).
func GetEntrySchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	manip.SetTableName(TableNameEntry)
	manip.SetTableComment("agent run journal entries, one row each, append-only (ADR-0308 §SD2)")
	journalEnvelope(manip)
	manip.PlainValueColumn(common.PlainItemTypeEntityLifecycle, EntryRevisionColumn, ctabb.U32).AddColumnEncodingHints(plain...)
	factsschema.LoadFactsDataSections(manip)
	return
}

// journalEnvelope is the envelope the ledger and the vault share: the run
// id as the Key and the entry's ordinal as the Order, so a run reads as
// one range in ordinal order.
func journalEnvelope(manip common.TableManipulatorFluidI) {
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.S).AddColumnEncodingHints(plain...)
	manip.PlainValueColumn(common.PlainItemTypeEntityId, EntryOrdinalColumn, ctabb.U64).AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
}

// sectionOf returns the one-attribute section declarator the store-owned
// tables use: a tagged-value section named for the attribute, carrying
// one "value" column, on the "data" streaming group.
func sectionOf(manip common.TableManipulatorFluidI) func(name naming.StylableName, ct canonicaltypes.PrimitiveAstNodeI, hints ...easp.AspectE) {
	channels := []common.MembershipSpecE{
		common.MembershipSpecLowCardRef,
		common.MembershipSpecLowCardVerbatim,
		common.MembershipSpecHighCardRef,
		common.MembershipSpecMixedLowCardRefHighCardParameters,
	}
	return func(name naming.StylableName, ct canonicaltypes.PrimitiveAstNodeI, hints ...easp.AspectE) {
		s := manip.TaggedValueSection(name).SectionStreamingGroup("data").AddSectionMembership(channels...)
		s.TaggedValueColumn("value", ct).AddColumnEncodingHints(hints...)
	}
}
