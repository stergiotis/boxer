package streamreadaccess

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// buildClassifyFixture assembles a small leeway table exercising all three
// column buckets — a plain/backbone id, a tagged section with a scalar value, a
// set value (which forces a cardinality support column), and a membership — then
// forward-generates the physical column names the same way ClassifyArrowColumns
// resolves them, so every column round-trips. It returns the IR, naming
// convention, an Arrow schema carrying those physical names, and the row config.
func buildClassifyFixture(t *testing.T) (ir *common.IntermediateTableRepresentation, conv common.NamingConventionI, schema *arrow.Schema, rowCfg common.TableRowConfigE) {
	t.Helper()

	manip, err := common.NewTableManipulator()
	if err != nil {
		t.Fatalf("new manipulator: %v", err)
	}
	manip.SetTableName("classifyfix")
	// Backbone: a plain entity-id column and an entity-timestamp (a datetime), so
	// the temporal test can assert the backbone datetime is Temporal() straight
	// from the classification.
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64).
		AddColumnValueSemantics(valueaspects.AspectHumanReadable, valueaspects.AspectMachineReadable)
	manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "eventTime", ctabb.Z64).
		AddColumnValueSemantics(valueaspects.AspectHumanReadable, valueaspects.AspectMachineReadable)
	// Tagged section "metric": a scalar value, a scalar datetime value, a set value
	// (→ support column), and a low-card-ref membership (→ membership +
	// membership-support columns).
	metric := manip.TaggedValueSection("metric").
		SectionStreamingGroup("data").
		AddSectionMembership(common.MembershipSpecLowCardRef)
	metric.TaggedValueColumn("value", ctabb.F64).
		AddColumnValueSemantics(valueaspects.AspectHumanReadable, valueaspects.AspectMachineReadable)
	metric.TaggedValueColumn("seen", ctabb.Z64).
		AddColumnValueSemantics(valueaspects.AspectHumanReadable, valueaspects.AspectMachineReadable)
	metric.TaggedValueColumn("tags", ctabb.Sh).
		AddColumnValueSemantics(valueaspects.AspectHumanReadable, valueaspects.AspectMachineReadable)

	tbl, err := manip.BuildTableDesc()
	if err != nil {
		t.Fatalf("build table desc: %v", err)
	}

	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir = common.NewIntermediateTableRepresentation()
	if err = ir.LoadFromTable(&tbl, tech); err != nil {
		t.Fatalf("load ir: %v", err)
	}

	conv, err = ddl.NewHumanReadableNamingConvention(":")
	if err != nil {
		t.Fatalf("naming convention: %v", err)
	}
	rowCfg = common.TableRowConfigMultiAttributesPerRow

	// Forward-generate the physical names, in IR order, into schema fields. The
	// Arrow types are irrelevant to classification (it resolves by name), so a
	// single placeholder type keeps the fixture terse.
	var fields []arrow.Field
	var buf []common.PhysicalColumnDesc
	for cc, cp := range ir.IterateColumnProps() {
		buf, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, buf[:0], rowCfg)
		if err != nil {
			t.Fatalf("map to physical: %v", err)
		}
		for _, phy := range buf {
			fields = append(fields, arrow.Field{Name: phy.String(), Type: arrow.PrimitiveTypes.Int64, Nullable: true})
		}
	}
	schema = arrow.NewSchema(fields, nil)
	return
}

func TestClassifyArrowColumns_buckets(t *testing.T) {
	ir, conv, schema, rowCfg := buildClassifyFixture(t)

	classes := ClassifyArrowColumns(ir, conv, schema, rowCfg)
	if len(classes) == 0 {
		t.Fatal("expected a non-empty classification")
	}
	// Every forward-mapped column round-trips, so the classifier must resolve
	// every schema field exactly once — including the support and membership
	// columns the Driver consumes internally and never surfaces.
	if len(classes) != schema.NumFields() {
		t.Fatalf("classified %d columns, schema has %d — some column was dropped", len(classes), schema.NumFields())
	}
	for _, cl := range classes {
		if cl.ArrowIdx < 0 || cl.ArrowIdx >= schema.NumFields() {
			t.Fatalf("class %+v has out-of-range ArrowIdx", cl)
		}
		if got := schema.Field(cl.ArrowIdx).Name; got != cl.Physical {
			t.Errorf("ArrowIdx %d resolves to %q but Physical is %q", cl.ArrowIdx, got, cl.Physical)
		}
	}

	count := func(pred func(ColumnClass) bool) int {
		n := 0
		for _, cl := range classes {
			if pred(cl) {
				n++
			}
		}
		return n
	}
	metric := func(c ColumnClass) bool { return string(c.SectionName) == "metric" }

	if got := count(func(c ColumnClass) bool {
		return c.Backbone() && c.PlainItemType == common.PlainItemTypeEntityId &&
			c.Class == ColumnRoleClassValue && string(c.LeewayName) == "id"
	}); got != 1 {
		t.Errorf("expected exactly one backbone entity-id value column, got %d; classes=%+v", got, classes)
	}
	if got := count(func(c ColumnClass) bool {
		return metric(c) && c.Class == ColumnRoleClassValue && string(c.LeewayName) == "value" && !c.NonScalar()
	}); got != 1 {
		t.Errorf("expected exactly one scalar metric value column, got %d", got)
	}
	if got := count(func(c ColumnClass) bool {
		return metric(c) && c.Class == ColumnRoleClassValue && string(c.LeewayName) == "tags" && c.NonScalar()
	}); got != 1 {
		t.Errorf("expected exactly one non-scalar metric value column, got %d", got)
	}
	// The set value forces a cardinality/length support column, and the
	// membership forces a membership-support column: at least one support column.
	if got := count(func(c ColumnClass) bool { return metric(c) && c.Class == ColumnRoleClassSupport }); got < 1 {
		t.Errorf("expected at least one metric support column, got %d; classes=%+v", got, classes)
	}
	if got := count(func(c ColumnClass) bool { return metric(c) && c.Class == ColumnRoleClassMembership }); got < 1 {
		t.Errorf("expected at least one metric membership column, got %d; classes=%+v", got, classes)
	}
	// No backbone column is ever classified as membership.
	if got := count(func(c ColumnClass) bool { return c.Backbone() && c.Class == ColumnRoleClassMembership }); got != 0 {
		t.Errorf("backbone columns must not be membership, got %d", got)
	}
}

// TestClassifyArrowColumns_temporal asserts the classifier populates a temporal
// canonical type end-to-end, so ColumnClass.Temporal() — the signal the play
// Detail timeline uses to find datetime columns — is true for a datetime value
// column and the backbone entity-timestamp, and false for a non-temporal value
// column and every support / membership column.
func TestClassifyArrowColumns_temporal(t *testing.T) {
	ir, conv, schema, rowCfg := buildClassifyFixture(t)
	classes := ClassifyArrowColumns(ir, conv, schema, rowCfg)

	find := func(pred func(ColumnClass) bool) (ColumnClass, bool) {
		for _, cl := range classes {
			if pred(cl) {
				return cl, true
			}
		}
		return ColumnClass{}, false
	}

	// A tagged Z64 value column is Temporal by canonical type — no name match.
	if seen, ok := find(func(c ColumnClass) bool {
		return string(c.LeewayName) == "seen" && c.Class == ColumnRoleClassValue
	}); !ok {
		t.Fatal("no metric.seen value column")
	} else if !seen.Temporal() {
		t.Errorf("seen (Z64) must be Temporal; canonicalType=%v", seen.CanonicalType)
	}

	// The backbone entity-timestamp is Temporal — detection does not, in practice,
	// depend on the PlainItemType==EntityTimestamp fallback.
	if ts, ok := find(func(c ColumnClass) bool {
		return c.PlainItemType == common.PlainItemTypeEntityTimestamp
	}); !ok {
		t.Fatal("no backbone entity-timestamp column")
	} else if !ts.Temporal() {
		t.Errorf("backbone entity-timestamp must be Temporal; canonicalType=%v", ts.CanonicalType)
	}

	// A non-temporal (F64) value column is not Temporal.
	if val, ok := find(func(c ColumnClass) bool {
		return string(c.LeewayName) == "value" && c.Class == ColumnRoleClassValue
	}); !ok {
		t.Fatal("no metric.value column")
	} else if val.Temporal() {
		t.Error("value (F64) must not be Temporal")
	}

	// No support or membership column is Temporal — this is what keeps len / card
	// support columns off the timeline even though they share a datetime section.
	for _, cl := range classes {
		if (cl.Class == ColumnRoleClassSupport || cl.Class == ColumnRoleClassMembership) && cl.Temporal() {
			t.Errorf("support/membership column %q must not be Temporal", cl.Physical)
		}
	}
}

func TestClassifyArrowColumns_nilInputs(t *testing.T) {
	if got := ClassifyArrowColumns(nil, nil, nil, common.TableRowConfigMultiAttributesPerRow); got != nil {
		t.Errorf("expected nil for nil inputs, got %+v", got)
	}
}
