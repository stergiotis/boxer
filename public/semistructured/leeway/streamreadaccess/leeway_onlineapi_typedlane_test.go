package streamreadaccess

import (
	"fmt"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stretchr/testify/require"
)

// buildIRAndSchema runs the manipulator through the IR and forward-generates
// the physical names, so a test can build a dense batch column by column in IR
// order (what NewDriver assumes) with the role of every column at hand.
func buildIRAndSchema(t *testing.T, load func(manip *common.TableManipulator)) (tbl common.TableDesc, ir *common.IntermediateTableRepresentation, conv common.NamingConventionI) {
	t.Helper()
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	load(manip)
	tbl, err = manip.BuildTableDesc()
	require.NoError(t, err)
	ir = common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&tbl, clickhouse.NewTechnologySpecificCodeGenerator()))
	conv, err = ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	return
}

// columnBuilder returns one Arrow array per physical column; fields and arrays
// are appended in IR order.
type columnBuilder func(t *testing.T, cc common.IntermediateColumnContext, role common.ColumnRoleE, phy string) arrow.Array

func buildDenseBatch(t *testing.T, ir *common.IntermediateTableRepresentation, conv common.NamingConventionI, nEntities int, build columnBuilder) arrow.RecordBatch {
	t.Helper()
	var fields []arrow.Field
	var cols []arrow.Array
	var buf []common.PhysicalColumnDesc
	var err error
	for cc, cp := range ir.IterateColumnProps() {
		buf, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, buf[:0], common.TableRowConfigMultiAttributesPerRow)
		require.NoError(t, err)
		require.Len(t, buf, len(cp.Names))
		for j, phy := range buf {
			arr := build(t, cc, cp.Roles[j], phy.String())
			fields = append(fields, arrow.Field{Name: phy.String(), Type: arr.DataType(), Nullable: false})
			cols = append(cols, arr)
		}
	}
	return array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, int64(nEntities))
}

func listU64(pool memory.Allocator, perEntity [][]uint64) arrow.Array {
	lb := array.NewListBuilder(pool, arrow.PrimitiveTypes.Uint64)
	vb := lb.ValueBuilder().(*array.Uint64Builder)
	for _, vs := range perEntity {
		lb.Append(true)
		vb.AppendValues(vs, nil)
	}
	return lb.NewArray()
}

func listF32(pool memory.Allocator, perEntity [][]float32) arrow.Array {
	lb := array.NewListBuilder(pool, arrow.PrimitiveTypes.Float32)
	vb := lb.ValueBuilder().(*array.Float32Builder)
	for _, vs := range perEntity {
		lb.Append(true)
		vb.AppendValues(vs, nil)
	}
	return lb.NewArray()
}

func listBin(pool memory.Allocator, perEntity [][]string) arrow.Array {
	lb := array.NewListBuilder(pool, arrow.BinaryTypes.Binary)
	vb := lb.ValueBuilder().(*array.BinaryBuilder)
	for _, vs := range perEntity {
		lb.Append(true)
		for _, s := range vs {
			vb.Append([]byte(s))
		}
	}
	return lb.NewArray()
}

// A mixed low-card-verbatim / high-card-params channel: one membership is
// (name, params) across the lmv and mvhp columns, co-indexed and counted by
// lmvcard. Entity 0 has one attribute carrying TWO memberships, entity 1 one
// attribute with one — so the cardinality column is exercised. The driver
// must emit one paired call per membership, not a (name, "") then ("",
// params) split, and announce the paired count.
func TestMixedChannelEmitsPairedMemberships(t *testing.T) {
	tbl, ir, conv := buildIRAndSchema(t, func(manip *common.TableManipulator) {
		manip.SetTableName("mixedfix")
		manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
		sec := manip.TaggedValueSection("m").
			AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
		sec.TaggedValueColumn("v", ctabb.U64)
	})
	pool := memory.NewGoAllocator()
	names := [][]string{{"/x", "/y"}, {"/z"}}
	params := [][]string{{"p0", "p1"}, {"p2"}}
	rec := buildDenseBatch(t, ir, conv, 2, func(t *testing.T, cc common.IntermediateColumnContext, role common.ColumnRoleE, phy string) arrow.Array {
		switch {
		case cc.Scope != common.IntermediateColumnScopeTagged:
			b := array.NewUint64Builder(pool)
			b.AppendValues([]uint64{1, 2}, nil)
			return b.NewArray()
		case role == common.ColumnRoleValue:
			return listU64(pool, [][]uint64{{10}, {11}})
		case role == common.ColumnRoleMixedLowCardVerbatim:
			return listBin(pool, names)
		case role == common.ColumnRoleMixedVerbatimHighCardParameters:
			return listBin(pool, params)
		case role == common.ColumnRoleMixedLowCardVerbatimCardinality:
			return listU64(pool, [][]uint64{{2}, {1}})
		}
		t.Fatalf("fixture does not know how to build column %s (role %s)", phy, role)
		return nil
	})
	defer rec.Release()

	d, err := NewDriver(&tbl, ir, DefaultFormatters())
	require.NoError(t, err)
	sink := NewStructuredOutputRecorder()
	require.NoError(t, d.DriveRecordBatch(sink, rec))
	out := sink.String()

	frames := strings.Split(out, "BeginTags()")[1:]
	require.Len(t, frames, 2, "one tag frame per attribute\n%s", out)
	want := [][]string{
		{`AddMembershipMixedLowCardVerbatimHighCardParam(verbatim="/x",params="p0")`, `AddMembershipMixedLowCardVerbatimHighCardParam(verbatim="/y",params="p1")`},
		{`AddMembershipMixedLowCardVerbatimHighCardParam(verbatim="/z",params="p2")`},
	}
	for i, f := range frames {
		f = strings.SplitN(f, "EndTags()", 2)[0]
		require.Equal(t, len(want[i]), strings.Count(f, "AddMembership"), "frame %d: one call per membership, no split halves\n%s", i, f)
		for _, w := range want[i] {
			require.Contains(t, f, w, "frame %d\n%s", i, f)
		}
	}
}

// viewSink records what the typed lane delivers and fails the test if the
// driver ever falls back to the text lane for it. The DebugSink supplies the
// structural SinkI surface; only the value writes and the capabilities are
// overridden here.
type viewSink struct {
	*DebugSink
	t        *testing.T
	scalars  []string
	ranges   []string
	coTags   []string
	textHits int
}

func (inst *viewSink) Write(p []byte) (int, error) {
	inst.textHits++
	return len(p), nil
}
func (inst *viewSink) WriteString(s string) (int, error) {
	inst.textHits++
	return len(s), nil
}
func (inst *viewSink) WriteArrowScalar(arr arrow.Array, flatIdx int) {
	inst.scalars = append(inst.scalars, fmt.Sprintf("%s@%d=%s", arr.DataType(), flatIdx, arr.ValueStr(flatIdx)))
}
func (inst *viewSink) WriteArrowRange(arr arrow.Array, start int, end int) {
	inst.ranges = append(inst.ranges, fmt.Sprintf("%s[%d:%d]", arr.DataType(), start, end))
}
func (inst *viewSink) BeginCoSectionTags(sectionName naming.StylableName, useAspects useaspects.AspectSet) {
	inst.coTags = append(inst.coTags, string(sectionName))
}

var _ SinkI = (*viewSink)(nil)
var _ ArrowValueSinkI = (*viewSink)(nil)
var _ CoSectionTagSinkI = (*viewSink)(nil)

// The typed lane: a sink implementing ArrowValueSinkI receives Arrow views —
// the exact Float32, not its text rendering — for plain scalars, tagged
// scalars and containers, and is never handed a formatted string. The
// co-section capability names the section each tag run belongs to.
func TestArrowValueSinkReceivesViewsNotText(t *testing.T) {
	tbl, ir, conv := buildIRAndSchema(t, func(manip *common.TableManipulator) {
		manip.SetTableName("viewfix")
		manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
		point := manip.TaggedValueSection("point").
			SectionCoSectionGroup("geo").
			AddSectionMembership(common.MembershipSpecLowCardVerbatim)
		point.TaggedValueColumn("lat", ctabb.F32)
		manip.TaggedValueSection("labels").
			SectionCoSectionGroup("geo").
			AddSectionMembership(common.MembershipSpecLowCardVerbatim).
			AddSectionUseAspects(useaspects.AspectSectionMembershipsAllSecondary)
		poly := manip.TaggedValueSection("poly").
			AddSectionMembership(common.MembershipSpecLowCardVerbatim)
		poly.TaggedValueColumn("pts", ctabb.F32h)
	})
	pool := memory.NewGoAllocator()
	rec := buildDenseBatch(t, ir, conv, 1, func(t *testing.T, cc common.IntermediateColumnContext, role common.ColumnRoleE, phy string) arrow.Array {
		switch {
		case cc.Scope != common.IntermediateColumnScopeTagged:
			b := array.NewUint64Builder(pool)
			b.Append(7)
			return b.NewArray()
		case role == common.ColumnRoleValue && cc.SectionName == "point":
			return listF32(pool, [][]float32{{0.1}})
		case role == common.ColumnRoleValue && cc.SectionName == "poly":
			return listF32(pool, [][]float32{{1.5, 2.5, 0.1}})
		case role == common.ColumnRoleLength: // poly's per-attribute array length
			return listU64(pool, [][]uint64{{3}})
		case role == common.ColumnRoleLowCardVerbatim:
			return listBin(pool, [][]string{{"/" + string(cc.SectionName)}})
		case role == common.ColumnRoleLowCardVerbatimCardinality:
			return listU64(pool, [][]uint64{{1}})
		}
		t.Fatalf("fixture does not know how to build column %s (role %s)", phy, role)
		return nil
	})
	defer rec.Release()

	d, err := NewDriver(&tbl, ir, DefaultFormatters())
	require.NoError(t, err)
	sink := &viewSink{DebugSink: NewStructuredOutputRecorder(), t: t}
	require.NoError(t, d.DriveRecordBatch(sink, rec))

	require.Zero(t, sink.textHits, "a typed sink must never be handed the text lane")
	require.Equal(t, []string{"uint64@0=7", "float32@0=0.1"}, sink.scalars, "plain id then the point's Float32 view (the text would be 0.1 too — the view carries the exact float32)")
	require.Equal(t, []string{"float32[0:3]"}, sink.ranges, "the poly array as one range view — three elements, which the driver mis-sliced to one before the len-column fix")
	require.Equal(t, []string{"point", "labels"}, sink.coTags, "each co-section's tags are announced with their own section context")
	// The structural frames are still driven for the typed sink.
	out := sink.String()
	require.Contains(t, out, `BeginCoSectionGroup("geo")`)
	require.Equal(t, 2, strings.Count(out, "BeginTags()"), "one frame per attribute: the merged geo attribute and the poly attribute\n%s", out)
}
