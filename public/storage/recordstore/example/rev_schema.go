package example

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	easp "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

// GetRevSchemaInManipulator builds the Revision-role reference schema
// (gen.Roles.Revision): a string Key, a u64 Order bound by name, and a u32
// rev column bound as the Revision, so (id, ord) is one identity a later
// row supersedes in place — the shape of a journal whose entry is
// tombstoned at its next revision rather than at a disguised Order.
func GetRevSchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	manip.SetTableName("rev")
	manip.SetTableComment("Revision-role reference schema")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.S).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "ord", ctabb.U64).
		AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
	manip.PlainValueColumn(common.PlainItemTypeEntityLifecycle, "rev", ctabb.U32).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)

	sec := manip.TaggedValueSection("note").
		SectionStreamingGroup("data").
		AddSectionMembership(common.MembershipSpecLowCardVerbatim)
	sec.TaggedValueColumn("value", ctabb.S).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	return
}
