package example

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
	"github.com/stretchr/testify/require"
)

func randomTimestamp(rnd rand.Source) time.Time {
	return time.Unix(int64(rnd.Uint64()), 0)
}

func TestRoundtrip(t *testing.T) {
	dml := NewInEntityTestTable(memory.DefaultAllocator, 128)
	rnd := rand.NewPCG(rand.Uint64(), rand.Uint64())
	ts := []time.Time{
		randomTimestamp(rnd),
		randomTimestamp(rnd),
		randomTimestamp(rnd),
	}
	const lrBase uint64 = 0
	var err error
	const nRows = 3
	{ // dml write
		secText := dml.GetSectionText()
		secGeo := dml.GetSectionGeo()
		for i := 0; i < nRows; i++ {
			ent := dml.BeginEntity()
			ent.SetId(uint64(i))
			ent.SetTimestamp(ts[0], ts[1:3])
			secText.BeginAttribute(fmt.Sprintf("hello world! %d", i)).
				AddToCoContainers(uint32(len("hello")), "hello").
				AddToCoContainers(uint32(len("world")), "world").
				AddMembershipLowCardRef(lrBase+uint64(i)*5).
				AddMembershipLowCardRef(lrBase+uint64(i)*5+1).
				AddMembershipMixedLowCardVerbatim([]byte("verbatim1"), []byte("params1")).
				EndAttribute()
			secText.BeginAttribute(fmt.Sprintf("hallo welt! %d", i)).
				AddToCoContainers(uint32(len("hallo")), "hallo").
				AddToCoContainers(uint32(len("welt")), "welt").
				AddMembershipLowCardRef(lrBase+uint64(i)*5+2).
				AddMembershipLowCardRef(lrBase+uint64(i)*5+3).
				AddMembershipMixedLowCardVerbatim([]byte("wortwörtlich1"), []byte("parameter1")).
				EndAttribute()
			secGeo.BeginAttribute(12.0, -3.5, 0x45494, 0x45454543).
				AddMembershipMixedLowCardVerbatim([]byte("verbatim2"), []byte("params2")).
				AddMembershipLowCardRef(lrBase + uint64(i)*5 + 4).
				EndAttribute()
			err = ent.CheckErrors()
			require.NoError(t, err)
			err = ent.CommitEntity()
			require.NoError(t, err)
		}
	}
	ra := NewReadAccessTestTable()

	{ // transfer
		var records []arrow.Record
		{
			records, err = dml.TransferRecords(nil)
			require.NoError(t, err)
			require.Len(t, records, 1)
			require.EqualValues(t, nRows, records[0].NumRows())
		}
		err = ra.LoadFromRecord(records[0])
		require.NoError(t, err)
	}

	{ // read access
		secText := ra.Text
		secGeo := ra.Geo
		for entityIdx := runtime.EntityIdx(0); entityIdx < nRows; entityIdx++ {
			{
				require.EqualValues(t, entityIdx, ra.EntityId.ValueId.Value(int(entityIdx)))
				//require.EqualValues(t, ts[0], ra.EntityTimestamp.ValueTs.Value(i))
			}
			{
				require.EqualValues(t, 2, secText.Attributes.GetNumberOfAttributes(entityIdx))
				attrIdx := runtime.AttributeIdx(0)
				{
					require.EqualValues(t, fmt.Sprintf("hello world! %d", entityIdx), secText.Attributes.GetAttrValueText(entityIdx, attrIdx))
					require.EqualValues(t, []string{"hello", "world"}, slices.Collect(secText.Attributes.GetAttrValueWords(entityIdx, attrIdx)))
					require.EqualValues(t, []uint64{
						lrBase + uint64(entityIdx)*5,
						lrBase + uint64(entityIdx)*5 + 1,
					}, slices.Collect(secText.Memberships.GetMembValueLowCardRef(entityIdx, attrIdx)))
					require.EqualValues(t, [][]byte{[]byte("verbatim1")}, slices.Collect(secText.Memberships.GetMembValueMixedLowCardVerbatim(entityIdx, attrIdx)))
					require.EqualValues(t, [][]byte{[]byte("params1")}, slices.Collect(secText.Memberships.GetMembValueMixedVerbatimHighCardParameters(entityIdx, attrIdx)))
				}
				attrIdx++
				{
					require.EqualValues(t, fmt.Sprintf("hallo welt! %d", entityIdx), secText.Attributes.GetAttrValueText(entityIdx, attrIdx))
					require.EqualValues(t, []string{"hallo", "welt"}, slices.Collect(secText.Attributes.GetAttrValueWords(entityIdx, attrIdx)))
					require.EqualValues(t, []uint64{
						lrBase + uint64(entityIdx)*5 + 2,
						lrBase + uint64(entityIdx)*5 + 3,
					}, slices.Collect(secText.Memberships.GetMembValueLowCardRef(entityIdx, attrIdx)))
					require.EqualValues(t, [][]byte{[]byte("wortwörtlich1")}, slices.Collect(secText.Memberships.GetMembValueMixedLowCardVerbatim(entityIdx, attrIdx)))
					require.EqualValues(t, [][]byte{[]byte("parameter1")}, slices.Collect(secText.Memberships.GetMembValueMixedVerbatimHighCardParameters(entityIdx, attrIdx)))
				}
			}
			{
				attrIdx := runtime.AttributeIdx(0)
				require.EqualValues(t, 1, secGeo.Attributes.GetNumberOfAttributes(entityIdx))
				require.EqualValues(t, 12.0, secGeo.Attributes.GetAttrValueLat(entityIdx, attrIdx))
				require.EqualValues(t, 0x45494, secGeo.Attributes.GetAttrValueH3Res1(entityIdx, attrIdx))
				require.EqualValues(t, 0x45454543, secGeo.Attributes.GetAttrValueH3Res2(entityIdx, attrIdx))
				require.EqualValues(t, [][]byte{[]byte("verbatim2")}, slices.Collect(secGeo.Memberships.GetMembValueMixedLowCardVerbatim(entityIdx, attrIdx)))
				require.EqualValues(t, [][]byte{[]byte("params2")}, slices.Collect(secGeo.Memberships.GetMembValueMixedVerbatimHighCardParameters(entityIdx, attrIdx)))
				require.EqualValues(t, []uint64{lrBase + uint64(entityIdx)*5 + 4}, slices.Collect(secGeo.Memberships.GetMembValueLowCardRef(entityIdx, attrIdx)))
			}
		}
	}
}
