package example

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

func randomTimestamp(rnd rand.Source) time.Time {
	return time.Unix(int64(rnd.Uint64()), 0)
}

func TestRoundtrip(t *testing.T) {
	dml := NewInEntityTestTable(memory.DefaultAllocator, 128)
	secText := dml.GetSectionText()
	var _ = secText
	secGeo := dml.GetSectionGeo()
	var _ = secGeo
	rnd := rand.NewPCG(rand.Uint64(), rand.Uint64())
	ts := []time.Time{
		randomTimestamp(rnd),
		randomTimestamp(rnd),
		randomTimestamp(rnd),
	}
	const lrBase uint64 = 0
	var err error
	const nRows = 1
	for i := 0; i < nRows; i++ {
		ent := dml.BeginEntity()
		ent.SetId(0)
		ent.SetTimestamp(ts[0], ts[1:3])
		//secText.BeginAttribute("hello world!").
		//	//AddToCoContainers(uint32(len("hello")), "hello").
		//	//AddToCoContainers(uint32(len("world")), "world").
		//	AddMembershipLowCardRef(lrBase+0).
		//	AddMembershipMixedLowCardVerbatim([]byte("verbatim1"), []byte("params1")).
		//	EndAttribute()
		//secGeo.BeginAttribute(12, -3.5, 0x45494, 0x45454543).
		//	AddMembershipMixedLowCardVerbatim([]byte("verbatim2"), []byte("params2")).
		//	AddMembershipLowCardRef(lrBase + 1).
		//	EndAttribute()
		err = ent.CheckErrors()
		require.NoError(t, err)
		err = ent.CommitEntity()
		require.NoError(t, err)
	}

	var records []arrow.Record
	{
		records, err = dml.TransferRecords(nil)
		require.NoError(t, err)
		require.Len(t, records, 1)
		require.Equal(t, nRows, records[0].NumRows())
	}

	ra := NewReadAccessTestTable()
	err = ra.LoadFromRecord(records[0])
	require.NoError(t, err)
}
