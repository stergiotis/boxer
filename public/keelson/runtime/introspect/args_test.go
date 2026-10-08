package introspect

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seqProvider is a table defined by its argument: n rows, v = 1..n, plus a
// label that defaults.
type seqProvider struct{}

func (seqProvider) Name() string              { return "seq" }
func (seqProvider) Freshness() FreshnessClass { return FreshnessLive }
func (seqProvider) Schema() *arrow.Schema     { return seqTable(0, "").Schema() }
func (seqProvider) Snapshot(Projection) (arrow.RecordBatch, error) {
	return nil, assert.AnError
}
func (seqProvider) Args() []ArgSpec {
	return []ArgSpec{
		{Name: "n", Type: ArgTypeUInt64, Required: true},
		{Name: "label", Type: ArgTypeString, Default: "x"},
	}
}
func (seqProvider) SnapshotArgs(proj Projection, args Args) (arrow.RecordBatch, error) {
	n := int(args.UInt64("n"))
	return seqTable(n, args.String("label")).Build(proj, n), nil
}

func seqTable(n int, label string) *Table {
	return NewTable().
		Uint64("v", func(i int) uint64 { return uint64(i + 1) }).
		String("label", func(int) string { return label })
}

func TestResolveArgs_TypesAndDefaults(t *testing.T) {
	args, err := ResolveArgs(seqProvider{}.Args(), map[string]string{"n": "3"})
	require.NoError(t, err)
	assert.Equal(t, uint64(3), args.UInt64("n"))
	assert.Equal(t, "x", args.String("label"))
}

func TestResolveArgs_Refusals(t *testing.T) {
	specs := seqProvider{}.Args()
	for name, raw := range map[string]map[string]string{
		"missing required": {"label": "y"},
		"undeclared":       {"n": "1", "m": "2"},
		"ill-typed":        {"n": "-1"},
		"not a number":     {"n": "three"},
	} {
		_, err := ResolveArgs(specs, raw)
		assert.Error(t, err, name)
	}
	_, err := ResolveArgs([]ArgSpec{{Name: "f", Type: ArgTypeFloat64, Required: true}}, map[string]string{"f": "nan"})
	assert.Error(t, err, "NaN")
}

func TestArgsKey_IsCanonical(t *testing.T) {
	specs := []ArgSpec{
		{Name: "b", Type: ArgTypeFloat64, Required: true},
		{Name: "a", Type: ArgTypeInt64, Required: true},
	}
	k1, err := ResolveArgs(specs, map[string]string{"a": "7", "b": "1.50"})
	require.NoError(t, err)
	k2, err := ResolveArgs(specs, map[string]string{"b": "1.5", "a": "07"})
	require.NoError(t, err)
	assert.Equal(t, k1.Key(), k2.Key(), "same values, different spellings")
	k3, err := ResolveArgs(specs, map[string]string{"a": "8", "b": "1.5"})
	require.NoError(t, err)
	assert.NotEqual(t, k1.Key(), k3.Key())
}

func TestSnapshotCall(t *testing.T) {
	batch, err := SnapshotCall(seqProvider{}, AllColumns(), map[string]string{"n": "4", "label": "z"})
	require.NoError(t, err)
	defer batch.Release()
	assert.Equal(t, int64(4), batch.NumRows())
	assert.Equal(t, uint64(4), batch.Column(0).(*array.Uint64).Value(3))
	assert.Equal(t, "z", batch.Column(1).(*array.String).Value(0))

	_, err = SnapshotCall(seqProvider{}, AllColumns(), map[string]string{})
	assert.Error(t, err, "a required argument is missing")
}

func TestSnapshotCall_ArgumentsToAPlainProviderAreRefused(t *testing.T) {
	_, err := SnapshotCall(&tablesProvider{reg: NewRegistry()}, AllColumns(), map[string]string{"n": "1"})
	assert.Error(t, err)
}
