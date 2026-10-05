package example

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

// On the arrow RecordBuilder every list column must gain a row per entity, so
// the SetActiveSections hint must not leave unmarked sections unstarted: the
// record drains, and matches the one written without the hint.
func TestSetActiveSectionsKeepsArrowColumnsAligned(t *testing.T) {
	pool := memory.NewGoAllocator()
	ts := time.Unix(1700000000, 0).UTC()
	write := func(e *InEntityTesttable) {
		for i := range 2 {
			e.BeginEntity().SetId(uint64(i + 1)).SetTimestamp(ts)
			e.GetSectionBool().BeginAttribute(true).AddMembershipMixedLowCardVerbatim([]byte("m"), nil).EndAttribute().EndSection()
			require.NoError(t, e.CommitEntity())
		}
	}

	eA := NewInEntityTesttable(pool, 2)
	write(eA)
	recsA, err := eA.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(recsA)

	eB := NewInEntityTesttable(pool, 2)
	eB.SetActiveSections([]int{InEntityTesttableSectionIndices["bool"]})
	write(eB)
	recsB, err := eB.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(recsB)

	assertEquivalent(t, recsA, recsB)
}
