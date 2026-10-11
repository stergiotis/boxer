package agentrunstore

import (
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
	return
}

// GetEntrySchemaInManipulator builds the entry table: envelope id (the run
// id), ts (the ordinal as an instant), revision (the Revision role: a
// tombstone is the entry at its next revision, ADR-0308 §SD8), and the
// entry's attributes.
func GetEntrySchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	manip.SetTableName(TableNameEntry)
	manip.SetTableComment("agent run journal entries, one row each, append-only (ADR-0308 §SD2)")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.S).AddColumnEncodingHints(plain...)
	manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "ts", ctabb.Z64).AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
	manip.PlainValueColumn(common.PlainItemTypeEntityLifecycle, EntryRevisionColumn, ctabb.U32).AddColumnEncodingHints(plain...)
	sec := sectionOf(manip)
	sec("entryOrdinal", ctabb.U32, plain...)
	sec("entryAt", ctabb.Z64, plain...)
	sec("entryKind", ctabb.S, low...)
	sec("entryKey", ctabb.S, plain...)
	sec("entryRole", ctabb.S, low...)
	sec("entryBody", ctabb.S, plain...)
	sec("entryCalls", ctabb.S, plain...)
	sec("entryBodyDigest", ctabb.S, plain...)
	sec("entryCallsDigest", ctabb.S, plain...)
	sec("entryName", ctabb.S, low...)
	sec("entryBook", ctabb.S, low...)
	sec("entryForm", ctabb.S, low...)
	sec("entryTarget", ctabb.U32, plain...)
	sec("entryFrom", ctabb.U32, plain...)
	sec("entryEffect", ctabb.S, low...)
	sec("entryTurn", ctabb.S, low...)
	sec("entryLabelLevel", ctabb.U32, plain...)
	sec("entryLabelTier", ctabb.U32, plain...)
	sec("entryLabelExposure", ctabb.S, low...)
	sec("entryLabelOrigin", ctabb.S, low...)
	sec("entryWriterRun", ctabb.S, low...)
	sec("entryWriterTurn", ctabb.S, low...)
	sec("entryWriterOrdinal", ctabb.U32, plain...)
	sec("entryPrev", ctabb.U32, plain...)
	sec("entryDigest", ctabb.S, plain...)
	sec("entryKeyId", ctabb.S, low...)
	sec("entryParent", ctabb.U32, plain...)
	sec("entryAttachments", ctabb.S, plain...)
	return
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
