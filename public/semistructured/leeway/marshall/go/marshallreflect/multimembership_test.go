package marshallreflect_test

import (
	"iter"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
	raruntime "github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
)

// multiMembDrone puts two fields with distinct memberships in one (LowCardRef)
// section, so a single attribute carrying both memberships must feed both —
// the codegen FillFromArrow switch fires both cases.
type multiMembDrone struct {
	_   struct{} `kind:"mmd"`
	Id  uint64   `lw:",id"`
	Foo string   `lw:"foo,symbol"`
	Bar string   `lw:"bar,symbol"`
}

// multiMembMock yields several membership ids for one attribute — the
// multi-membership wire shape no codec writes but a third-party producer can.
type multiMembMock struct{ ids []uint64 }

func (m multiMembMock) GetMembValueLowCardRef(_ raruntime.EntityIdx, _ raruntime.AttributeIdx) iter.Seq[uint64] {
	return func(yield func(uint64) bool) {
		for _, id := range m.ids {
			if !yield(id) {
				return
			}
		}
	}
}

// TestUnmarshal_MultiMembershipFeedsEveryField pins read parity with codegen:
// one attribute tagged with both `foo` and `bar` populates both fields. The old
// first-match dispatch set only Foo, then failed Bar's exactly-one-occurrence
// check.
func TestUnmarshal_MultiMembershipFeedsEveryField(t *testing.T) {
	b := array.NewUint64Builder(memory.NewGoAllocator())
	b.Append(1)
	idArr := b.NewArray().(*array.Uint64)
	defer idArr.Release()

	const fooID, barID = 10, 20
	readers := marshallreflect.NewSectionReaders(1).
		PlainColumn("id", idArr).
		Section("symbol",
			mvAttrsMock{vals: []string{"shared"}},
			multiMembMock{ids: []uint64{fooID, barID}})

	var got []multiMembDrone
	require.NoError(t, marshallreflect.Unmarshal(readers, &got, marshallreflect.MapLookup{"foo": fooID, "bar": barID}))

	require.Len(t, got, 1)
	require.Equal(t, "shared", got[0].Foo)
	require.Equal(t, "shared", got[0].Bar)
}
