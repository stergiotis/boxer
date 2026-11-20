package firsttable

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

// TableRowConfig is the row layout the generated code is built for: one row
// per entity, each section carrying any number of attributes.
const TableRowConfig = common.TableRowConfigMultiAttributesPerRow

// NewSchema describes the events table: an id and a timestamp on every row,
// a section of strings and a section of 64-bit floats. Both sections carry
// low-cardinality verbatim memberships, so an attribute's label is stored as
// text beside its value and no id registry is needed.
func NewSchema() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("unable to create table manipulator: %w", err)
		return
	}
	manip.SetTableName("events")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
	manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "ts", ctabb.Z64)
	manip.TaggedValueSection("symbol").
		AddSectionMembership(common.MembershipSpecLowCardVerbatim).
		TaggedValueColumn("value", ctabb.S)
	manip.TaggedValueSection("f64").
		AddSectionMembership(common.MembershipSpecLowCardVerbatim).
		TaggedValueColumn("value", ctabb.F64)
	return
}
