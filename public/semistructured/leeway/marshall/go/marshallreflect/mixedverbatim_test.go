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

// mixedLowCardVerbatim shares the carrier machinery proven by the anchor
// mixedLowCardRef round-trip; only the carrier value field differs (Name
// []byte, embedded verbatim, vs Id uint64). No boxer schema declares a
// simple single-value section with mixedLowCardVerbatim AND a matching RA
// reader (anchor lacks the channel; the example testtable's verbatim
// sections are multi-sub-column / multi-membership and its string section
// has no reader), so these tests exercise the verbatim-specific halves
// directly against the recording mock (write) and a minimal Seq2 mock (read).

type mixedVerbatimDrone struct {
	_          struct{}                           `kind:"mvd"`
	Id         uint64                             `lw:",id"`
	NaturalKey []byte                             `lw:",naturalKey"`
	Reading    string                             `lw:"sensor,symbol,mixedLowCardVerbatim"`
	ReadingC   marshalltypes.MixedLowCardVerbatim `lw:"sensor,symbol,mixedLowCardVerbatim"`
}

// TestMixedVerbatim_WriteEmitsCarrier confirms Marshal pulls the carrier's
// Name + Params into AddMembershipMixedLowCardVerbatimP (the []byte value
// field, not a uint64 id).
func TestMixedVerbatim_WriteEmitsCarrier(t *testing.T) {
	dml := &recordingDML{}
	rows := []mixedVerbatimDrone{
		{Id: 1, Reading: "alpha", ReadingC: marshalltypes.MixedLowCardVerbatim{Name: []byte("nm-a"), Params: []byte("pa")}},
		{Id: 2, Reading: "beta", ReadingC: marshalltypes.MixedLowCardVerbatim{Name: []byte("nm-b"), Params: []byte("pb")}},
	}
	require.NoError(t, marshallreflect.Marshal(dml, rows, marshallreflect.NoLookup{}))

	joined := strings.Join(dml.log, "\n")
	require.Contains(t, joined, `Symbol.BeginAttribute("alpha")`)
	require.Contains(t, joined, `AddMembershipMixedLowCardVerbatimP("nm-a", "pa")`)
	require.Contains(t, joined, `Symbol.BeginAttribute("beta")`)
	require.Contains(t, joined, `AddMembershipMixedLowCardVerbatimP("nm-b", "pb")`)
}

// --- minimal read-side mock for the symbol section. ---

type mvAttrsMock struct{ vals []string }

func (m mvAttrsMock) GetNumberOfAttributes(raruntime.EntityIdx) int64 { return 1 }
func (m mvAttrsMock) GetAttrValueValue(e raruntime.EntityIdx, _ raruntime.AttributeIdx) string {
	return m.vals[int(e)]
}

type mvMembsMock struct{ names, params [][]byte }

func (m mvMembsMock) GetMembValueLowCardVerbatimHighCardParams(e raruntime.EntityIdx, _ raruntime.AttributeIdx) iter.Seq2[[]byte, []byte] {
	return func(yield func([]byte, []byte) bool) {
		yield(m.names[int(e)], m.params[int(e)])
	}
}

// TestMixedVerbatim_ReadReconstructsCarrier confirms Unmarshal rebuilds the
// value + carrier {Name, Params} from the Seq2 combined accessor, copying
// the verbatim Name out of the (mock) buffer. Fed the same name/params the
// write test emits, so the pair is a logical round-trip.
func TestMixedVerbatim_ReadReconstructsCarrier(t *testing.T) {
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
	membs := mvMembsMock{
		names:  [][]byte{[]byte("nm-a"), []byte("nm-b")},
		params: [][]byte{[]byte("pa"), []byte("pb")},
	}

	args := marshallreflect.NewSectionReaders(2).
		PlainColumn("id", idArr).
		PlainColumn("naturalKey", nkArr).
		Section("symbol", attrs, membs)
	var got []mixedVerbatimDrone
	require.NoError(t, marshallreflect.Unmarshal(args, &got, marshallreflect.NoLookup{}))

	require.Len(t, got, 2)
	require.Equal(t, "alpha", got[0].Reading)
	require.Equal(t, []byte("nm-a"), got[0].ReadingC.Name)
	require.Equal(t, []byte("pa"), got[0].ReadingC.Params)
	require.Equal(t, "beta", got[1].Reading)
	require.Equal(t, []byte("nm-b"), got[1].ReadingC.Name)
	require.Equal(t, []byte("pb"), got[1].ReadingC.Params)
}
