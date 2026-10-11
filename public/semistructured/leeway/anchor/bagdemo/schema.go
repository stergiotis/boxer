// Package bagdemo is a small leeway table and the codecs over it that
// exercise tuple elements with parameters, `,unit` values and value-less
// elements, read back by ReadRow from sections other kinds share
// (ADR-0326). Its sections take the reference channel and both mixed
// channels, which the anchor table does not.
package bagdemo

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	easp "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

// TableRowConfig is the table's row configuration.
const TableRowConfig = common.TableRowConfigMultiAttributesPerRow

// GetSchemaInManipulator builds the table: an id, a symbol section, a
// string-array section and a section without a value column.
func GetSchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("unable to create table manipulator: %w", err)
		return
	}
	manip.SetTableName("bag")
	manip.SetTableComment("tuple elements with parameters (ADR-0326)")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64).
		AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
	specs := []common.MembershipSpecE{
		common.MembershipSpecLowCardRef,
		common.MembershipSpecMixedLowCardRefHighCardParameters,
		common.MembershipSpecMixedLowCardVerbatimHighCardParameters,
	}
	manip.TaggedValueSection("symbol").AddSectionMembership(specs...).
		TaggedValueColumn("value", ctabb.S)
	manip.TaggedValueSection("stringArray").AddSectionMembership(specs...).
		TaggedValueColumn("value", ctabb.Sh)
	manip.TaggedValueSection("null").AddSectionMembership(specs...)
	return
}
