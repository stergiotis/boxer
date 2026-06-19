package marshallreflect_test

import (
	"iter"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/marshalltypes"
	raruntime "github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
)

// The parametrized channels' membership is a single opaque params blob — the
// carrier is marshalltypes.Parametrized{Params} (no id/name), the write takes
// one arg, and the read is a plain Seq[[]byte] (not the mixed channels'
// Seq2). No generated boxer schema declares a parametrized section, so these
// tests exercise the two halves against the recording mock (write) and a
// minimal single-Seq mock (read). mvAttrsMock is shared from
// mixedverbatim_test.go (same package).

type parametrizedDrone struct {
	_          struct{}                   `kind:"pd"`
	Id         uint64                     `lw:",id"`
	NaturalKey []byte                     `lw:",naturalKey"`
	Reading    string                     `lw:"sensor,symbol,lowCardRefParametrized"`
	ReadingC   marshalltypes.Parametrized `lw:"sensor,symbol,lowCardRefParametrized"`
}

// TestParametrized_WriteEmitsParamsOnly confirms Marshal passes only the
// carrier's Params to AddMembershipLowCardRefParametrizedP (one arg — there
// is no membership value field).
func TestParametrized_WriteEmitsParamsOnly(t *testing.T) {
	dml := &recordingDML{}
	rows := []parametrizedDrone{
		{Id: 1, Reading: "alpha", ReadingC: marshalltypes.Parametrized{Params: []byte("blob-a")}},
		{Id: 2, Reading: "beta", ReadingC: marshalltypes.Parametrized{Params: []byte("blob-b")}},
	}
	require.NoError(t, marshallreflect.Marshal(dml, rows, marshallreflect.NoLookup{}))

	joined := strings.Join(dml.log, "\n")
	require.Contains(t, joined, `Symbol.BeginAttribute("alpha")`)
	require.Contains(t, joined, `AddMembershipLowCardRefParametrizedP("blob-a")`)
	require.Contains(t, joined, `Symbol.BeginAttribute("beta")`)
	require.Contains(t, joined, `AddMembershipLowCardRefParametrizedP("blob-b")`)
}

// --- minimal read-side mock: a single Seq of the params blob. ---

type pmMembsMock struct{ blobs [][]byte }

func (m pmMembsMock) GetMembValueLowCardRefParametrized(e raruntime.EntityIdx, _ raruntime.AttributeIdx) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		yield(m.blobs[int(e)])
	}
}

// TestParametrized_ReadReconstructsParams confirms Unmarshal rebuilds the
// value + Parametrized{Params} from the single-value Seq. Fed the same blobs
// the write test emits, so the pair is a logical round-trip.
func TestParametrized_ReadReconstructsParams(t *testing.T) {
	mem := memory.NewGoAllocator()
	idB := array.NewUint64Builder(mem)
	idB.Append(1)
	idB.Append(2)
	idArr := idB.NewArray()
	defer idArr.Release()
	nkB := array.NewBinaryBuilder(mem, arrow.BinaryTypes.Binary)
	nkB.Append([]byte("k1"))
	nkB.Append([]byte("k2"))
	nkArr := nkB.NewArray()
	defer nkArr.Release()

	attrs := mvAttrsMock{vals: []string{"alpha", "beta"}}
	membs := pmMembsMock{blobs: [][]byte{[]byte("blob-a"), []byte("blob-b")}}

	args := marshallreflect.NewSectionReaders(2).
		PlainColumn("id", idArr).
		PlainColumn("naturalKey", nkArr).
		Section("symbol", attrs, membs)
	var got []parametrizedDrone
	require.NoError(t, marshallreflect.Unmarshal(args, &got, marshallreflect.NoLookup{}))

	require.Len(t, got, 2)
	require.Equal(t, "alpha", got[0].Reading)
	require.Equal(t, []byte("blob-a"), got[0].ReadingC.Params)
	require.Equal(t, "beta", got[1].Reading)
	require.Equal(t, []byte("blob-b"), got[1].ReadingC.Params)
}
