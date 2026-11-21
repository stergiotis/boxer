package lowlevel

import (
	"slices"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	raruntime "github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
	"github.com/stretchr/testify/require"
)

// TestAmbientHighCardRefMembership proves ADR-0112 M1: a HighCardRef membership
// pushed on the entity is replayed onto every attribute written while it is in
// scope — landing in the HighCardRef lane alongside the attribute's own
// LowCardRef membership — and is gone once popped. It writes two entities (one
// with an ambient id pushed around its attribute, one control) straight through
// the generated DML and reads the memberships back through the read-access
// classes, no ClickHouse in the loop.
func TestAmbientHighCardRefMembership(t *testing.T) {
	alloc := memory.NewGoAllocator()
	dml := NewInEntityDeviceTable(alloc, 4)
	defer InEntityDeviceTableReleaseBuilder(dml)

	ts := time.Unix(0, 1).UTC()

	// entity 0: ambient HighCardRef(42) pushed around one symbol attribute.
	dml.beginEntity().setId(1).setTimestamp(ts).setLifecycle(0)
	dml.PushMembershipHighCardRef(42)
	dml.GetSectionSymbol().BeginAttribute("alpha").AddMembershipLowCardRef(7).EndAttribute().EndSection()
	dml.PopMembershipsHighCardRef(1)
	require.NoError(t, dml.commitEntity())

	// entity 1: nothing pushed — the control.
	dml.beginEntity().setId(2).setTimestamp(ts).setLifecycle(0)
	dml.GetSectionSymbol().BeginAttribute("beta").AddMembershipLowCardRef(7).EndAttribute().EndSection()
	require.NoError(t, dml.commitEntity())

	recs, err := dml.transferRecords(nil)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	rec := recs[0]
	defer rec.Release()
	require.Equal(t, int64(2), rec.NumRows())

	symR := NewReadAccessDeviceTableTaggedSymbol()
	defer symR.Release()
	require.NoError(t, symR.LoadFromRecord(rec))
	membs := symR.GetMemberships()

	e0, e1 := raruntime.EntityIdx(0), raruntime.EntityIdx(1)
	a0 := raruntime.AttributeIdx(0)

	// entity 0: the ambient id lands in the HighCardRef lane; the codec's own
	// LowCardRef membership is untouched.
	require.Equal(t, []uint64{42}, slices.Collect(membs.GetMembValueHighCardRef(e0, a0)))
	require.Equal(t, []uint64{7}, slices.Collect(membs.GetMembValueLowCardRef(e0, a0)))

	// entity 1: no ambient → HighCardRef lane empty; LowCardRef unchanged.
	require.Empty(t, slices.Collect(membs.GetMembValueHighCardRef(e1, a0)))
	require.Equal(t, []uint64{7}, slices.Collect(membs.GetMembValueLowCardRef(e1, a0)))
}
