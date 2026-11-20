package marshallreflect_test

import (
	"fmt"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/eh/eb/ebtest"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// TestSectionReaders_NilReported rejects a nil reader set up front rather than
// dereferencing it mid-row.
func TestSectionReaders_NilReported(t *testing.T) {
	var got []mixedVerbatimDrone
	require.Error(t, marshallreflect.Unmarshal(nil, &got, nil))
}

// TestSectionReaders_MissingCoverageReported confirms Unmarshal reports every
// plain column and section the DTO's Plan declares but the reader set omits —
// in one error, before reading any row (the win over the old func(name) any
// triplet, where a forgotten section returned nil and panicked at row i).
func TestSectionReaders_MissingCoverageReported(t *testing.T) {
	var got []mixedVerbatimDrone // declares id + naturalKey plain + the symbol section
	// Register nothing.
	err := marshallreflect.Unmarshal(marshallreflect.NewSectionReaders(0), &got, marshallreflect.NoLookup{})
	require.Error(t, err)
	require.Contains(t, fieldList(t, err, "missing"), "id")
	require.Contains(t, fieldList(t, err, "missing"), "symbol")
}

// fieldList renders a []string error field as one string, so a test can assert
// on a substring of one entry without pinning the entry's exact wording.
func fieldList(t *testing.T, err error, key string) (s string) {
	t.Helper()
	return fmt.Sprint(ebtest.Fields(t, err)[key])
}

// TestSectionReaders_MistypedPlainColumnReported confirms a plain column of
// the wrong concrete Arrow type is an error before any row is read, not a
// failed type assertion at row 0.
func TestSectionReaders_MistypedPlainColumnReported(t *testing.T) {
	mem := memory.NewGoAllocator()
	idB := array.NewInt64Builder(mem) // the DTO's id is uint64
	idB.Append(1)
	idArr := idB.NewArray()
	defer idArr.Release()
	nkB := array.NewBinaryBuilder(mem, arrow.BinaryTypes.Binary)
	nkB.Append([]byte("k1"))
	nkArr := nkB.NewArray()
	defer nkArr.Release()

	args := marshallreflect.NewSectionReaders(1).
		PlainColumn("id", idArr).
		PlainColumn("naturalKey", nkArr).
		Section("symbol", mvAttrsMock{vals: []string{"alpha"}}, mvMembsMock{names: [][]byte{[]byte("n")}, params: [][]byte{[]byte("p")}})
	var got []mixedVerbatimDrone
	var err error
	require.NotPanics(t, func() { err = marshallreflect.Unmarshal(args, &got, marshallreflect.NoLookup{}) })
	require.Error(t, err)
	require.Contains(t, fieldList(t, err, "mistyped"), "*array.Int64")
	require.Empty(t, got)
}
