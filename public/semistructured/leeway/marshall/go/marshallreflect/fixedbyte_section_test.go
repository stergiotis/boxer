package marshallreflect_test

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/example"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// fixedHashRow targets FixedTable's `hash` section, a yx4 column whose
// generated BeginAttribute takes a [4]byte — the [N]byte field is passed as
// an array, not resliced to []byte.
type fixedHashRow struct {
	_  struct{} `kind:"fixedHashRow"`
	Id uint64   `lw:",id"`
	H  [4]byte  `lw:"m,hash"`
}

func TestMarshal_FixedByteSectionTakesArray(t *testing.T) {
	dml := example.NewInEntityFixedTable(memory.NewGoAllocator(), 1)
	rows := []fixedHashRow{{Id: 1, H: [4]byte{1, 2, 3, 4}}}
	require.NoError(t, marshallreflect.Marshal(dml, rows, marshallreflect.MapLookup{"m": 7}))
	recs, err := dml.TransferRecords(nil)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	defer recs[0].Release()
	require.EqualValues(t, 1, recs[0].NumRows())
	var found bool
	for i, f := range recs[0].Schema().Fields() {
		l, ok := recs[0].Column(i).(*array.List)
		if !ok {
			continue
		}
		fsb, ok := l.ListValues().(*array.FixedSizeBinary)
		if !ok || !strings.Contains(f.Name, "hash") {
			continue
		}
		require.Equal(t, []byte{1, 2, 3, 4}, fsb.Value(0), "column %s", f.Name)
		found = true
	}
	require.True(t, found, "no hash column in FixedTable")
}
