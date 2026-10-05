package streamreadaccess

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stretchr/testify/require"
)

// withoutRole projects rec onto every column whose IR role is not role.
func withoutRole(t *testing.T, ir *common.IntermediateTableRepresentation, conv common.NamingConventionI, rec arrow.RecordBatch, role common.ColumnRoleE) arrow.RecordBatch {
	t.Helper()
	drop := make(map[string]bool, 2)
	var buf []common.PhysicalColumnDesc
	var err error
	for cc, cp := range ir.IterateColumnProps() {
		buf, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, buf[:0], common.TableRowConfigMultiAttributesPerRow)
		require.NoError(t, err)
		for j, phy := range buf {
			if cp.Roles[j] == role {
				drop[phy.String()] = true
			}
		}
	}
	require.NotEmpty(t, drop)
	var fields []arrow.Field
	var cols []arrow.Array
	for i, f := range rec.Schema().Fields() {
		if drop[f.Name] {
			continue
		}
		fields = append(fields, f)
		cols = append(cols, rec.Column(i))
	}
	out := array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, rec.NumRows())
	t.Cleanup(out.Release)
	return out
}

// Regression: a projection that carried array or membership columns without
// their count lane was driven with "one element per attribute", so pts
// [1.5 2.5][3.5] came out as [1.5][2.5] and the second membership of the
// first attribute went to the second. The driver is now refused.
func TestSchemaWithoutCountLaneIsRefused(t *testing.T) {
	tbl, ir, conv, rec := wellFormedLanes.build(t)

	_, err := NewDriverFromSchema(&tbl, ir, DefaultFormatters(), rec.Schema(), conv, common.TableRowConfigMultiAttributesPerRow)
	require.NoError(t, err, "the full schema drives")

	for _, role := range []common.ColumnRoleE{common.ColumnRoleLength, common.ColumnRoleLowCardVerbatimCardinality} {
		sub := withoutRole(t, ir, conv, rec, role)
		_, err = NewDriverFromSchema(&tbl, ir, DefaultFormatters(), sub.Schema(), conv, common.TableRowConfigMultiAttributesPerRow)
		require.Errorf(t, err, "a schema without the %s lane must be refused", role)
	}
}

// A channel the schema declares single-instance (ADR-0213) has no
// cardinality column at all; its absence must not be refused.
func TestSchemaWithDeclaredSingleChannelIsNotRefused(t *testing.T) {
	tbl, ir, conv := buildIRAndSchema(t, func(manip *common.TableManipulator) {
		manip.SetTableName("singlefix")
		manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
		sec := manip.TaggedValueSection("poly").
			AddSectionMembership(common.MembershipSpecLowCardVerbatim).
			AddSectionUseAspects(useaspects.AspectSectionSingleMembershipLowCardVerbatim)
		sec.TaggedValueColumn("s", ctabb.U64)
	})
	pool := memory.NewGoAllocator()
	rec := buildDenseBatch(t, ir, conv, 1, func(t *testing.T, cc common.IntermediateColumnContext, role common.ColumnRoleE, phy string) arrow.Array {
		switch {
		case cc.Scope != common.IntermediateColumnScopeTagged:
			b := array.NewUint64Builder(pool)
			b.Append(7)
			return b.NewArray()
		case role == common.ColumnRoleValue:
			return listU64(pool, [][]uint64{{10}})
		case role == common.ColumnRoleLowCardVerbatim:
			return listBin(pool, [][]string{{"/a"}})
		}
		t.Fatalf("fixture does not know how to build column %s (role %s)", phy, role)
		return nil
	})
	defer rec.Release()
	_, err := NewDriverFromSchema(&tbl, ir, DefaultFormatters(), rec.Schema(), conv, common.TableRowConfigMultiAttributesPerRow)
	require.NoError(t, err)
}
