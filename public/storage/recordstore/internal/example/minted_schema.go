package example

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	easp "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

// GetMintedSchemaInManipulator builds the KeyStream reference schema
// (gen.Input.KeyStream): a u64 Key the store mints from a tagged id
// stream, so two processes writing one table never pick one id.
func GetMintedSchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	manip.SetTableName("minted")
	manip.SetTableComment("KeyStream reference schema")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64).
		AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
	manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "ts", ctabb.Z64).
		AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
	sec := manip.TaggedValueSection("label").
		SectionStreamingGroup("data").
		AddSectionMembership(common.MembershipSpecLowCardVerbatim)
	sec.TaggedValueColumn("value", ctabb.S).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	return
}
