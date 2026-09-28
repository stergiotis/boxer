package streamreadaccess

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stretchr/testify/require"
)

// lanesFixture is one entity of a section carrying a scalar, a homogenous
// array and a low-card verbatim membership channel. Each lane is given per
// entity; a test makes lanes disagree or leaves one out of the schema.
type lanesFixture struct {
	scalar  []uint64
	pts     []float32
	length  []uint64
	paths   []string
	pathsCd []uint64
}

func (inst lanesFixture) build(t *testing.T) (tbl common.TableDesc, ir *common.IntermediateTableRepresentation, conv common.NamingConventionI, rec arrow.RecordBatch) {
	t.Helper()
	tbl, ir, conv = buildIRAndSchema(t, func(manip *common.TableManipulator) {
		manip.SetTableName("lanesfix")
		manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
		sec := manip.TaggedValueSection("poly").
			AddSectionMembership(common.MembershipSpecLowCardVerbatim)
		sec.TaggedValueColumn("s", ctabb.U64)
		sec.TaggedValueColumn("pts", ctabb.F32h)
	})
	pool := memory.NewGoAllocator()
	rec = buildDenseBatch(t, ir, conv, 1, func(t *testing.T, cc common.IntermediateColumnContext, role common.ColumnRoleE, phy string) arrow.Array {
		switch {
		case cc.Scope != common.IntermediateColumnScopeTagged:
			b := array.NewUint64Builder(pool)
			b.Append(7)
			return b.NewArray()
		case role == common.ColumnRoleValue && cc.SubType == common.IntermediateColumnsSubTypeScalar:
			return listU64(pool, [][]uint64{inst.scalar})
		case role == common.ColumnRoleValue:
			return listF32(pool, [][]float32{inst.pts})
		case role == common.ColumnRoleLength:
			return listU64(pool, [][]uint64{inst.length})
		case role == common.ColumnRoleLowCardVerbatim:
			return listBin(pool, [][]string{inst.paths})
		case role == common.ColumnRoleLowCardVerbatimCardinality:
			return listU64(pool, [][]uint64{inst.pathsCd})
		}
		t.Fatalf("fixture does not know how to build column %s (role %s)", phy, role)
		return nil
	})
	t.Cleanup(rec.Release)
	return
}

var wellFormedLanes = lanesFixture{
	scalar:  []uint64{10, 11},
	pts:     []float32{1.5, 2.5, 3.5},
	length:  []uint64{2, 1},
	paths:   []string{"/a", "/b", "/c"},
	pathsCd: []uint64{2, 1},
}

// Regression: lanes of one record that disagree in length indexed past the
// shorter lane and panicked inside arrow. The driver promises an error.
func TestLanesThatDisagreeInLengthAreAnErrorNotAPanic(t *testing.T) {
	for name, fx := range map[string]lanesFixture{
		"length lane shorter than the attribute count": func() lanesFixture {
			f := wellFormedLanes
			f.scalar = []uint64{10, 11, 12}
			f.pathsCd = []uint64{1, 1, 1}
			return f
		}(),
		"membership card lane shorter than the attribute count": func() lanesFixture {
			f := wellFormedLanes
			f.pathsCd = []uint64{3}
			return f
		}(),
		"array length exceeds the value lane": func() lanesFixture {
			f := wellFormedLanes
			f.length = []uint64{2, 5}
			return f
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			tbl, ir, _, rec := fx.build(t)
			d, err := NewDriver(&tbl, ir, DefaultFormatters())
			require.NoError(t, err)
			for _, sink := range []SinkI{NewStructuredOutputRecorder(), &viewSink{DebugSink: NewStructuredOutputRecorder(), t: t}} {
				require.NotPanics(t, func() {
					err = d.DriveRecordBatch(sink, rec)
				})
				require.Error(t, err)
			}
		})
	}
	// The well-formed record drives cleanly.
	tbl, ir, _, rec := wellFormedLanes.build(t)
	d, err := NewDriver(&tbl, ir, DefaultFormatters())
	require.NoError(t, err)
	require.NoError(t, d.DriveRecordBatch(NewStructuredOutputRecorder(), rec))
}
