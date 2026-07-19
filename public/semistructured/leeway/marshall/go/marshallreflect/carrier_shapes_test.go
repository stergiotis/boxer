package marshallreflect_test

import (
	"iter"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/marshalltypes"
	raruntime "github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
)

// ADR-0008 OQ#4 lifted the scalar-only restriction on carrier (mixed /
// parametrized) value fields. A value may be Option[T] or a container []T
// (one attribute, N values, one scalar carrier). These drive both through
// the reflect codec — write via the recording mock, read via per-attribute
// mocks — plus the RowComposer multi-value path. mixedLowCardVerbatim is
// used because the recording DML already implements its AddMembership
// method. (The former []T,explode + slice-carrier pairing was removed by
// ADR-0113 D1.)

// --- container: value slice + one scalar carrier. ---

type containerCarrierDrone struct {
	_          struct{}                           `kind:"ccd"`
	Id         uint64                             `lw:",id"`
	NaturalKey []byte                             `lw:",naturalKey"`
	Tags       []string                           `lw:"sensor,symbol,mixedLowCardVerbatim"`
	TagsC      marshalltypes.MixedLowCardVerbatim `lw:"sensor,symbol,mixedLowCardVerbatim"`
}

func TestCarrierContainer_Write(t *testing.T) {
	dml := &recordingDML{}
	rows := []containerCarrierDrone{{
		Id: 1, NaturalKey: []byte("k"),
		Tags:  []string{"a", "b", "c"},
		TagsC: marshalltypes.MixedLowCardVerbatim{Name: []byte("n"), Params: []byte("p")},
	}}
	require.NoError(t, marshallreflect.Marshal(dml, rows, marshallreflect.NoLookup{}))
	joined := strings.Join(dml.log, "\n")
	require.Equal(t, 1, strings.Count(joined, "Symbol.BeginAttribute(")) // single container attribute
	require.Contains(t, joined, "Symbol.BeginAttribute()")               // opened with no value
	require.Contains(t, joined, `AddToContainerP("a")`)
	require.Contains(t, joined, `AddToContainerP("c")`)
	require.Equal(t, 1, strings.Count(joined, "AddMembershipMixedLowCardVerbatimP(")) // one carrier for the container
	require.Contains(t, joined, `AddMembershipMixedLowCardVerbatimP("n", "p")`)
}

type containerAttrsMock struct{ vals [][]string }

func (m containerAttrsMock) GetNumberOfAttributes(raruntime.EntityIdx) int64 { return 1 }
func (m containerAttrsMock) GetAttrValueValue(e raruntime.EntityIdx, _ raruntime.AttributeIdx) iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, v := range m.vals[int(e)] {
			if !yield(v) {
				return
			}
		}
	}
}

func TestCarrierContainer_Read(t *testing.T) {
	idArr, nkArr := buildIDNK(t, []uint64{1}, [][]byte{[]byte("k")})
	defer idArr.Release()
	defer nkArr.Release()

	attrs := containerAttrsMock{vals: [][]string{{"a", "b", "c"}}}
	membs := mvMembsMock{names: [][]byte{[]byte("n")}, params: [][]byte{[]byte("p")}}
	var got []containerCarrierDrone
	require.NoError(t, marshallreflect.Unmarshal(carrierArgs(idArr, nkArr, attrs, membs), &got, marshallreflect.NoLookup{}))

	require.Len(t, got, 1)
	require.Equal(t, []string{"a", "b", "c"}, got[0].Tags)
	require.Equal(t, []byte("n"), got[0].TagsC.Name)
	require.Equal(t, []byte("p"), got[0].TagsC.Params)
}

// --- option: present + absent rows, scalar carrier per row. ---

type optionCarrierDrone struct {
	_          struct{}                           `kind:"ocd"`
	Id         uint64                             `lw:",id"`
	NaturalKey []byte                             `lw:",naturalKey"`
	Tag        option.Option[string]              `lw:"sensor,symbol,mixedLowCardVerbatim"`
	TagC       marshalltypes.MixedLowCardVerbatim `lw:"sensor,symbol,mixedLowCardVerbatim"`
}

func TestCarrierOption_WritePresentAbsent(t *testing.T) {
	dml := &recordingDML{}
	rows := []optionCarrierDrone{
		{Id: 1, NaturalKey: []byte("k1"), Tag: option.Option[string]{Val: "a", Has: true}, TagC: marshalltypes.MixedLowCardVerbatim{Name: []byte("n"), Params: []byte("p")}},
		{Id: 2, NaturalKey: []byte("k2")}, // Has=false → no attribute emitted
	}
	require.NoError(t, marshallreflect.Marshal(dml, rows, marshallreflect.NoLookup{}))
	joined := strings.Join(dml.log, "\n")
	require.Equal(t, 1, strings.Count(joined, "Symbol.BeginAttribute(")) // only the present row emits
	require.Contains(t, joined, `Symbol.BeginAttribute("a")`)
	require.Contains(t, joined, `AddMembershipMixedLowCardVerbatimP("n", "p")`)
}

// --- RowComposer: a multi-value container carrier via AddMultiValueAttributes.
// The plain owner declares no sections, so the container carrier reaches the
// wire exactly once, through the multi-value-filtered path (the third marshal
// entry point besides Marshal and the emitter). ---

type plainOnlyDrone struct {
	_          struct{} `kind:"pod"`
	Id         uint64   `lw:",id"`
	NaturalKey []byte   `lw:",naturalKey"`
}

func TestCarrierContainer_RowComposerMultiValue(t *testing.T) {
	dml := &recordingDML{}
	comp := marshallreflect.NewRowComposer(dml, marshallreflect.NoLookup{})
	row := containerCarrierDrone{
		Id: 1, NaturalKey: []byte("k"),
		Tags:  []string{"a", "b"}, // runtime length > 1 → multi-value pass
		TagsC: marshalltypes.MixedLowCardVerbatim{Name: []byte("n"), Params: []byte("p")},
	}
	require.NoError(t, comp.BeginRow(plainOnlyDrone{Id: 1, NaturalKey: []byte("k")}))
	require.NoError(t, comp.AddMultiValueAttributes(row))
	require.NoError(t, comp.CommitRow())

	joined := strings.Join(dml.log, "\n")
	require.Contains(t, joined, "Symbol.BeginAttribute()")
	require.Contains(t, joined, `AddToContainerP("a")`)
	require.Contains(t, joined, `AddMembershipMixedLowCardVerbatimP("n", "p")`)
}

// --- shared helpers. ---

func buildIDNK(t *testing.T, ids []uint64, nks [][]byte) (*array.Uint64, *array.Binary) {
	t.Helper()
	mem := memory.NewGoAllocator()
	idB := array.NewUint64Builder(mem)
	for _, id := range ids {
		idB.Append(id)
	}
	nkB := array.NewBinaryBuilder(mem, arrow.BinaryTypes.Binary)
	for _, nk := range nks {
		nkB.Append(nk)
	}
	return idB.NewArray().(*array.Uint64), nkB.NewArray().(*array.Binary)
}

func carrierArgs(idArr, nkArr arrow.Array, attrs, membs any) *marshallreflect.SectionReaders {
	return marshallreflect.NewSectionReaders(idArr.Len()).
		PlainColumn("id", idArr).
		PlainColumn("naturalKey", nkArr).
		Section("symbol", attrs, membs)
}
