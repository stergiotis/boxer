package common_test

import (
	"fmt"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
)

// ExampleTableManipulator describes a small events table: two plain columns
// that every row carries, and two tagged-value sections whose values are
// labelled by membership. The snippet in the leeway EXPLANATION.md §1 is the
// body of this example.
func ExampleTableManipulator() {
	manip, err := common.NewTableManipulator()
	if err != nil {
		panic(err)
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

	tbl, err := manip.BuildTableDesc()
	if err != nil {
		panic(err)
	}
	fmt.Println(tbl.DictionaryEntry.Name, len(tbl.PlainValuesNames), len(tbl.TaggedValuesSections))
	// Output:
	// events 2 2
}

// ExampleIntermediateTableRepresentation_LoadFromTable expands a TableDesc
// into the intermediate representation: one entry per logical column, with
// the support columns (membership, cardinality) a target needs made explicit.
// The target's code generator decides which support columns a membership
// channel costs, so it is passed in here.
func ExampleIntermediateTableRepresentation_LoadFromTable() {
	manip, err := common.NewTableManipulator()
	if err != nil {
		panic(err)
	}
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
	manip.TaggedValueSection("symbol").
		AddSectionMembership(common.MembershipSpecLowCardVerbatim).
		TaggedValueColumn("value", ctabb.S)
	tbl, err := manip.BuildTableDesc()
	if err != nil {
		panic(err)
	}

	ir := common.NewIntermediateTableRepresentation()
	err = ir.LoadFromTable(&tbl, clickhouse.NewTechnologySpecificCodeGenerator())
	if err != nil {
		panic(err)
	}
	nPlain, nTagged := ir.Length()
	fmt.Println(nPlain, nTagged)
	// Output:
	// 1 3
}
