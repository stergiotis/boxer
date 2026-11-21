package example

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

// InAttr.EndSection closes the open attribute and the section in one call, so
// BeginAttribute(v).…EndSection() must produce the same record as
// BeginAttribute(v).…EndAttribute().EndSection().
func TestInAttrEndSectionClosesAttribute(t *testing.T) {
	pool := memory.NewGoAllocator()
	ts := time.Unix(1700000000, 0).UTC()
	lmv := []byte("m")

	eA := NewInEntityTesttable(pool, 1)
	eA.BeginEntity().SetId(42).SetTimestamp(ts)
	eA.GetSectionString().BeginAttribute("hello").AddMembershipMixedLowCardVerbatim(lmv, nil).EndAttribute().EndSection()
	require.NoError(t, eA.CommitEntity())
	recsA, err := eA.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(recsA)

	eB := NewInEntityTesttable(pool, 1)
	eB.BeginEntity().SetId(42).SetTimestamp(ts)
	eB.GetSectionString().BeginAttribute("hello").AddMembershipMixedLowCardVerbatim(lmv, nil).EndSection()
	require.NoError(t, eB.CommitEntity())
	recsB, err := eB.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(recsB)

	assertEquivalent(t, recsA, recsB)
}
