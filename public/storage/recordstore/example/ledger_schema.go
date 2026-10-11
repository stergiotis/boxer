package example

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	easp "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

// GetLedgerSchemaInManipulator builds the evidence-profile reference
// schema (ADR-0327 (proposed) §SD2): a uint64 id as the Key (the scope; a
// component's plain id binding needs the column named so, as the facts
// envelope names it), a uint64 ordinal as the Order, and the three facts
// data sections the profile's components bind by name — symbol,
// stringArray and u64Array — with the facts membership specs, so a
// component written against this table reads as it would on any
// facts-shaped one.
func GetLedgerSchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	manip.SetTableName("ledger")
	manip.SetTableComment("ADR-0327 evidence-profile reference schema")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64).
		AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "ordinal", ctabb.U64).
		AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
	specs := []common.MembershipSpecE{
		common.MembershipSpecLowCardRef,
		common.MembershipSpecHighCardRef,
		common.MembershipSpecMixedLowCardRefHighCardParameters,
	}
	strh := canonicaltypes.PromoteScalarPrim(ctabb.S, canonicaltypes.ScalarModifierHomogenousArray)
	manip.TaggedValueSection("symbol").SectionStreamingGroup("data").AddSectionMembership(specs...).
		TaggedValueColumn("value", ctabb.S).
		AddColumnEncodingHints(easp.AspectInterRecordLowCardinality, easp.AspectIntraRecordLowCardinality, easp.AspectLightGeneralCompression)
	manip.TaggedValueSection("stringArray").SectionStreamingGroup("data").AddSectionMembership(specs...).
		TaggedValueColumn("value", strh).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	manip.TaggedValueSection("u64Array").SectionStreamingGroup("data").AddSectionMembership(specs...).
		TaggedValueColumn("value", canonicaltypes.PromoteScalarPrim(ctabb.U64, canonicaltypes.ScalarModifierHomogenousArray)).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	return
}

// ledgerGenIds is the membership-id snapshot the ledger store is generated
// under: the profile's memberships and the fixture's, far from declaration
// order so a positional id leaking into an artefact could not match. (The
// generated LedgerMembershipIds is the store's own copy of it.)
var ledgerGenIds = map[string]uint64{
	"ledgerNoteText":                9001,
	"evidenceKindChain":             9101,
	"evidenceChainPrev":             9102,
	"evidenceChainDigest":           9103,
	"evidenceChainScheme":           9104,
	"evidenceChainMark":             9105,
	"evidenceChainMarkTarget":       9106,
	"evidenceChainMarkComponent":    9107,
	"evidenceChainHeadDigest":       9108,
	"evidenceChainCommitComponents": 9109,
	"evidenceChainCommitments":      9110,
	"evidenceKindWrittenBy":         9201,
	"evidenceWrittenByRun":          9202,
	"evidenceWrittenByApp":          9203,
	"evidenceWrittenByInstance":     9204,
	"evidenceWrittenByOriginBy":     9205,
	"evidenceWrittenByPrincipal":    9206,
	"evidenceWrittenByPrincipalBy":  9207,
	"evidenceWrittenByPurpose":      9208,
	"evidenceWrittenByCorrelation":  9209,
	"evidenceWrittenByBatch":        9210,
}
