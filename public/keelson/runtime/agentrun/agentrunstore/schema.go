package agentrunstore

import (
	"slices"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	easp "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// DatabaseName is the default database, the runtime's own.
const DatabaseName = "boxer"

// TableNameRun is the run table's bare name and TableNameEntry the entry
// table's; single lowercase words, as the generator requires.
const (
	TableNameRun   = "agentrun"
	TableNameEntry = "agentrunentry"
)

// LedgerComponentPaths are the ledger store's hand-written components: the
// entry and the records that are metadata (ADR-0325 §SD1). The evidence
// profile adds the chain and the written-by component (ADR-0327 §SD2).
var LedgerComponentPaths = []string{"./entry_dto.go", "./request_dto.go", "./reply_dto.go", "./compact_dto.go",
	"./approvaldecision_dto.go", "./head_dto.go", "./profile_dto.go", "./turnstate_dto.go", "./tools_dto.go"}

// VaultComponentPaths are the vault store's hand-written components: the
// body row and the records that hold content (ADR-0325 §SD1). The evidence
// profile adds the shredded attributes and the opening (ADR-0327 §SD4).
var VaultComponentPaths = []string{"./body_dto.go", "./endorse_dto.go", "./grant_dto.go", "./question_dto.go",
	"./answer_dto.go", "./approvalcontent_dto.go", "./copy_dto.go"}

// TableNameEntryVault is the vault table's bare name: the ledger's with the
// profile's suffix (ADR-0327 §SD4).
const TableNameEntryVault = TableNameEntry + "vault"

// ChainScheme is the hash constructions the journal's chain digests and
// commitments are under; its name is recorded on every entry. The fourth
// version of the form: the mark and the written-by component are in the
// item, and the scheme is a parameter (ADR-0327 §SD9).
var ChainScheme = evidence.NewScheme("agentrun-chain/v4")

// VaultDDLTail is what the vault table's CREATE TABLE gains after the
// composed settings: a deletion that rewrites a part leaves the part it
// replaced on disk as an outdated part until the server's cleanup
// thread removes it, after old_parts_lifetime (480 s by default) and on
// the cleanup period, which starts at cleanup_delay_period (30 s plus a
// random share) and backs off to max_cleanup_delay_period (300 s) while
// it finds nothing to do. The vault pins all of them to a second, so the
// bytes an erasure removed from the table leave the disk within seconds
// of it, not minutes (ADR-0320 §SD4). The tail is appended verbatim, so
// it continues the SETTINGS clause the composed DDL ends with.
const VaultDDLTail = ", old_parts_lifetime = 1, cleanup_delay_period = 1, cleanup_delay_period_random_add = 0, max_cleanup_delay_period = 1"

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
// id, a tagged u64 of vocab.IdStreamAgentRun; ADR-0325 §SD5), ts (the write instant), lifecycle for latest-wins, and one section
// per attribute.
func GetRunSchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	manip.SetTableName(TableNameRun)
	manip.SetTableComment("agent runs, one row each, latest-wins (ADR-0308 §SD1)")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64).AddColumnEncodingHints(plain...)
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
// revision (the Revision role: a later lease epoch's row supersedes an
// earlier holder's at the same ordinal, ADR-0319 §SD7) — over the
// boxer.facts data sections, so the ledger reads with the trail's read
// surface (plan002 B0). The sections also take the verbatim name +
// parameters channel and the shredder's value-less sections, since the
// vault table is derived from this description (ADR-0327 §SD4) and holds
// the shredded arguments and calls. Content is the vault's (ADR-0320
// §SD1).
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
	factsschema.LoadFactsDataSections(manip, common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
	factsschema.LoadValuelessSections(manip, append(slices.Clone(factsschema.FactsMembershipSpecs), common.MembershipSpecMixedLowCardVerbatimHighCardParameters)...)
	return
}

// journalEnvelope is the envelope the ledger and the vault share: the run
// id as the Key and the entry's ordinal as the Order, so a run reads as
// one range in ordinal order.
func journalEnvelope(manip common.TableManipulatorFluidI) {
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64).AddColumnEncodingHints(plain...)
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
