package streamreadaccess

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stretchr/testify/require"
)

// A co-section group whose second section is membership-only — the
// annotation-overlay pattern (a primary section plus a co-section that carries
// labels and no value columns). Co-sections share topology, not membership
// columns, so the merged tagged value must carry the tags of EVERY section in
// the group. Until 2026-08-21 driveCoGroup drove only the first section's
// membership columns and the overlay's tags were silently dropped; this pins
// the fix on the dense NewDriver path.
//
// Two entities; entity 0 has two attributes, entity 1 has one. Section "point"
// carries a u64 value and a verbatim path per attribute; section "labels"
// carries one verbatim label per attribute and nothing else.
func TestCoGroupDrivesMembershipsOfEverySection(t *testing.T) {
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	manip.SetTableName("cogroupfix")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
	point := manip.TaggedValueSection("point").
		SectionCoSectionGroup("geo").
		AddSectionMembership(common.MembershipSpecLowCardVerbatim)
	point.TaggedValueColumn("v", ctabb.U64)
	manip.TaggedValueSection("labels").
		SectionCoSectionGroup("geo").
		AddSectionMembership(common.MembershipSpecLowCardVerbatim)

	tbl, err := manip.BuildTableDesc()
	require.NoError(t, err)
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&tbl, tech))
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)

	// Per entity, per attribute data. The batch is built column by column in
	// IR order, which is what the dense NewDriver path assumes.
	attrsPerEntity := []int{2, 1}
	pointValues := [][]uint64{{10, 11}, {12}}
	pointPaths := [][]string{{"/a/0", "/a/1"}, {"/a/2"}}
	labels := [][]string{{"lbl0", "lbl1"}, {"lbl2"}}

	pool := memory.NewGoAllocator()
	var fields []arrow.Field
	var cols []arrow.Array
	var buf []common.PhysicalColumnDesc
	for cc, cp := range ir.IterateColumnProps() {
		buf, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, buf[:0], common.TableRowConfigMultiAttributesPerRow)
		require.NoError(t, err)
		require.Len(t, buf, len(cp.Names))
		for j, phy := range buf {
			role := cp.Roles[j]
			var arr arrow.Array
			switch {
			case cc.Scope != common.IntermediateColumnScopeTagged:
				// The plain id: one u64 per entity, not a list.
				b := array.NewUint64Builder(pool)
				for e := range attrsPerEntity {
					b.Append(uint64(e + 1))
				}
				arr = b.NewArray()
			case role == common.ColumnRoleValue:
				lb := array.NewListBuilder(pool, arrow.PrimitiveTypes.Uint64)
				vb := lb.ValueBuilder().(*array.Uint64Builder)
				for e := range attrsPerEntity {
					lb.Append(true)
					vb.AppendValues(pointValues[e], nil)
				}
				arr = lb.NewArray()
			case role == common.ColumnRoleLowCardVerbatim:
				src := pointPaths
				if cc.SectionName == "labels" {
					src = labels
				}
				lb := array.NewListBuilder(pool, arrow.BinaryTypes.Binary)
				vb := lb.ValueBuilder().(*array.BinaryBuilder)
				for e := range attrsPerEntity {
					lb.Append(true)
					for _, s := range src[e] {
						vb.Append([]byte(s))
					}
				}
				arr = lb.NewArray()
			case role == common.ColumnRoleLowCardVerbatimCardinality:
				// Exactly one membership per attribute on this channel.
				lb := array.NewListBuilder(pool, arrow.PrimitiveTypes.Uint64)
				vb := lb.ValueBuilder().(*array.Uint64Builder)
				for e, n := range attrsPerEntity {
					lb.Append(true)
					for range n {
						vb.Append(1)
					}
					_ = e
				}
				arr = lb.NewArray()
			default:
				t.Fatalf("fixture does not know how to build column %s (role %s)", phy.String(), role)
			}
			fields = append(fields, arrow.Field{Name: phy.String(), Type: arr.DataType(), Nullable: false})
			cols = append(cols, arr)
		}
	}
	schema := arrow.NewSchema(fields, nil)
	rec := array.NewRecordBatch(schema, cols, int64(len(attrsPerEntity)))
	defer rec.Release()

	d, err := NewDriver(&tbl, ir, DefaultFormatters())
	require.NoError(t, err)
	sink := NewStructuredOutputRecorder()
	require.NoError(t, d.DriveRecordBatch(sink, rec))
	out := sink.String()

	require.Contains(t, out, `BeginCoSectionGroup("geo")`, "the two sections must merge into one co-group\n%s", out)

	// Every tag frame of the merged tagged value carries the point's path AND
	// the overlay's label: 3 attributes, 2 verbatim tags each.
	frames := strings.Split(out, "BeginTags()")[1:]
	require.Len(t, frames, 3, "one tag frame per merged attribute\n%s", out)
	wantPaths := []string{"/a/0", "/a/1", "/a/2"}
	wantLabels := []string{"lbl0", "lbl1", "lbl2"}
	for i, f := range frames {
		f = strings.SplitN(f, "EndTags()", 2)[0]
		require.Equal(t, 2, strings.Count(f, "AddMembershipVerbatim("), "frame %d must carry one tag per section\n%s", i, f)
		require.Contains(t, f, `AddMembershipVerbatim(lowCard=true,value="`+wantPaths[i]+`")`, "frame %d: point path\n%s", i, f)
		require.Contains(t, f, `AddMembershipVerbatim(lowCard=true,value="`+wantLabels[i]+`")`, "frame %d: overlay label (dropped before the fix)\n%s", i, f)
	}
}
