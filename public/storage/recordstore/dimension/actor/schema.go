package actor

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	easp "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// TableRowConfig: multiple attributes per row, as in the provenance dimension.
const TableRowConfig = common.TableRowConfigMultiAttributesPerRow

// GetActorSchemaInManipulator builds the descriptor fact table of the actor
// dimension (ADR-0295 §SD8): id (Key) + ts (Order) form the envelope, and the
// Actor component owns one symbol section. No lifecycle: descriptors are
// content-addressed and never deleted. The section carries a single
// LowCardRef membership lane and no HighCardRef lane, so the generated store
// refuses stampers of its own — interning a descriptor never recurses.
func GetActorSchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	manip.SetTableName("actor")
	manip.SetTableComment("ADR-0295 actor dimension descriptor")
	loadActorSchema(manip)
	return
}

func loadActorSchema(manip common.TableManipulatorFluidI) {
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "ts", ctabb.Z64).
		AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)

	secSymbol := manip.TaggedValueSection("symbol").
		SectionStreamingGroup("data").
		AddSectionMembership(common.MembershipSpecLowCardRef)
	secSymbol.TaggedValueColumn("value", ctabb.S).
		AddColumnValueSemantics(valueaspects.AspectCanonicalizedValue).
		AddColumnEncodingHints(easp.AspectInterRecordLowCardinality,
			easp.AspectIntraRecordLowCardinality,
			easp.AspectLightGeneralCompression)
}
