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
// parametrized) value fields. A value may now be Option[T], a container []T
// (one attribute, N values, one scalar carrier), or []T,explode (N attributes,
// one value + one carrier each, paired by a []marshalltypes.X slice carrier).
// These drive all three through the reflect codec — write via the recording
// mock, read via per-attribute mocks — plus the explode length-mismatch guard
// and the RowComposer multi-value path. mixedLowCardVerbatim is used because
// the recording DML already implements its AddMembership method.

// --- explode: value slice + slice carrier, paired element-wise. ---

type explodeCarrierDrone struct {
	_          struct{}                             `kind:"ecd"`
	Id         uint64                               `lw:",id"`
	NaturalKey []byte                               `lw:",naturalKey"`
	Tags       []string                             `lw:"sensor,symbol,explode,mixedLowCardVerbatim"`
	TagsC      []marshalltypes.MixedLowCardVerbatim `lw:"sensor,symbol,mixedLowCardVerbatim"`
}

func TestCarrierExplode_Write(t *testing.T) {
	dml := &recordingDML{}
	rows := []explodeCarrierDrone{{
		Id: 1, NaturalKey: []byte("k"),
		Tags: []string{"a", "b"},
		TagsC: []marshalltypes.MixedLowCardVerbatim{
			{Name: []byte("na"), Params: []byte("pa")},
			{Name: []byte("nb"), Params: []byte("pb")},
		},
	}}
	require.NoError(t, marshallreflect.Marshal(dml, rows, marshallreflect.NoLookup{}))
	joined := strings.Join(dml.log, "\n")
	require.Equal(t, 2, strings.Count(joined, "Symbol.BeginAttribute(")) // one attribute per element
	require.Contains(t, joined, `Symbol.BeginAttribute("a")`)
	require.Contains(t, joined, `AddMembershipMixedLowCardVerbatimP("na", "pa")`)
	require.Contains(t, joined, `Symbol.BeginAttribute("b")`)
	require.Contains(t, joined, `AddMembershipMixedLowCardVerbatimP("nb", "pb")`)
}

func TestCarrierExplode_LengthMismatchErrors(t *testing.T) {
	dml := &recordingDML{}
	rows := []explodeCarrierDrone{{
		Id: 1, NaturalKey: []byte("k"),
		Tags:  []string{"a", "b"},
		TagsC: []marshalltypes.MixedLowCardVerbatim{{Name: []byte("na"), Params: []byte("pa")}}, // len 1 != 2
	}}
	err := marshallreflect.Marshal(dml, rows, marshallreflect.NoLookup{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "different lengths")
}

type explodeAttrsMock struct{ vals [][]string }

func (m explodeAttrsMock) GetNumberOfAttributes(e raruntime.EntityIdx) int64 {
	return int64(len(m.vals[int(e)]))
}
func (m explodeAttrsMock) GetAttrValueValue(e raruntime.EntityIdx, a raruntime.AttributeIdx) string {
	return m.vals[int(e)][int(a)]
}

type explodeMembsMock struct{ names, params [][][]byte }

func (m explodeMembsMock) GetMembValueLowCardVerbatimHighCardParams(e raruntime.EntityIdx, a raruntime.AttributeIdx) iter.Seq2[[]byte, []byte] {
	return func(yield func([]byte, []byte) bool) {
		yield(m.names[int(e)][int(a)], m.params[int(e)][int(a)])
	}
}

func TestCarrierExplode_Read(t *testing.T) {
	idArr, nkArr := buildIDNK(t, []uint64{1}, [][]byte{[]byte("k")})
	defer idArr.Release()
	defer nkArr.Release()

	attrs := explodeAttrsMock{vals: [][]string{{"a", "b"}}}
	membs := explodeMembsMock{
		names:  [][][]byte{{[]byte("na"), []byte("nb")}},
		params: [][][]byte{{[]byte("pa"), []byte("pb")}},
	}
	var got []explodeCarrierDrone
	require.NoError(t, marshallreflect.Unmarshal(carrierArgs(idArr, nkArr, attrs, membs), &got, marshallreflect.NoLookup{}))

	require.Len(t, got, 1)
	require.Equal(t, []string{"a", "b"}, got[0].Tags)
	require.Len(t, got[0].TagsC, 2)
	require.Equal(t, []byte("na"), got[0].TagsC[0].Name)
	require.Equal(t, []byte("pa"), got[0].TagsC[0].Params)
	require.Equal(t, []byte("nb"), got[0].TagsC[1].Name)
	require.Equal(t, []byte("pb"), got[0].TagsC[1].Params)
}

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
