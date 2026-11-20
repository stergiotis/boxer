package readback_test

import (
	"fmt"
	"time"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/readback"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

type checkout struct {
	_     struct{}  `kind:"checkout"`
	Id    uint64    `lw:",id"`
	Ts    time.Time `lw:",ts"`
	Event string    `lw:"event,symbol,verbatim"`
	Geo   string    `lw:"user.geo,symbol,verbatim"`
	Total float64   `lw:"cart.total,f64,verbatim"`
}

// ExampleGenerator_Generate joins a DTO's mapping plan against the physical
// table and prints the WHERE-clause filter that selects the rows carrying
// every mandatory field of the DTO exactly once. Verbatim memberships are
// embedded as string literals, so no id registry is involved.
func ExampleGenerator_Generate() {
	manip, err := common.NewTableManipulator()
	if err != nil {
		panic(err)
	}
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
	ir := common.NewIntermediateTableRepresentation()
	err = ir.LoadFromTable(&tbl, clickhouse.NewTechnologySpecificCodeGenerator())
	if err != nil {
		panic(err)
	}
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	if err != nil {
		panic(err)
	}

	info := readback.NewInformationRetrieval(conv)
	err = info.LoadTable(ir, common.TableRowConfigMultiAttributesPerRow)
	if err != nil {
		panic(err)
	}
	plan, err := marshallreflect.PlanFor[checkout]()
	if err != nil {
		panic(err)
	}
	art, err := readback.NewGenerator(info, readback.NewLookupResolver(nil)).Generate(plan)
	if err != nil {
		panic(err)
	}
	fmt.Println(art.Filter)
	// Output:
	// hasAll("tv:symbol:lv:lv:y:124:::0::", ['event', 'user.geo']) AND has("tv:f64:lv:lv:y:124:::0::", 'cart.total') AND countEqual("tv:symbol:lv:lv:y:124:::0::", 'event') = 1 AND countEqual("tv:symbol:lv:lv:y:124:::0::", 'user.geo') = 1 AND countEqual("tv:f64:lv:lv:y:124:::0::", 'cart.total') = 1
}
