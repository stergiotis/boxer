package provenance

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	easp "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// TableRowConfig: multiple attributes per row, as in the recordstore example
// and the leeway anchor.
const TableRowConfig = common.TableRowConfigMultiAttributesPerRow

// GetProvenanceSchemaInManipulator builds the descriptor fact table for the
// provenance dimension (ADR-0112 S1): id (Key) + ts (Order) form the envelope,
// and the Provenance component owns two tagged sections — symbol (Host) and
// symbolArray (Stack). No lifecycle: descriptors are content-addressed and
// never deleted. The section names match the lw tags' section parts in
// provenance_dto.go; each carries a single LowCardRef membership lane, all a
// kind-tagged component needs.
func GetProvenanceSchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	manip.SetTableName("provenance")
	manip.SetTableComment("ADR-0112 provenance dimension descriptor")
	loadProvenanceSchema(manip)
	return
}

func loadProvenanceSchema(manip common.TableManipulatorFluidI) {
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "ts", ctabb.Z64).
		AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)

	channels := []common.MembershipSpecE{common.MembershipSpecLowCardRef}

	secSymbol := manip.TaggedValueSection("symbol").
		SectionStreamingGroup("data").
		AddSectionMembership(channels...)
	secSymbol.TaggedValueColumn("value", ctabb.S).
		AddColumnValueSemantics(valueaspects.AspectCanonicalizedValue).
		AddColumnEncodingHints(easp.AspectInterRecordLowCardinality,
			easp.AspectIntraRecordLowCardinality,
			easp.AspectLightGeneralCompression)

	secSymArray := manip.TaggedValueSection("symbolArray").
		SectionStreamingGroup("data").
		AddSectionMembership(channels...)
	secSymArray.TaggedValueColumn("value", ctabb.Sh).
		AddColumnValueSemantics(valueaspects.AspectCanonicalizedValue).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)
}
