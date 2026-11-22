package recordstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore"
)

type sampleInner struct {
	Label string
	N     uint32
}

type sampleRow struct {
	Id       uint64
	Name     string
	On       bool
	Small    uint8
	Ratio    float64
	At       time.Time
	Bytes    []byte
	Keys     []string
	Inner    sampleInner
	Ptr      *sampleInner
	hidden   string
	Fixed    [2]int16
	Duration time.Duration
}

func TestFillSample(t *testing.T) {
	var row sampleRow
	recordstore.FillSample(&row, 41)
	assert.EqualValues(t, 41, row.Id)
	assert.Equal(t, "sample-Name", row.Name)
	assert.True(t, row.On)
	assert.EqualValues(t, 42, row.Small)
	assert.NotZero(t, row.Ratio)
	assert.Equal(t, recordstore.SampleOrder, row.At)
	assert.Len(t, row.Bytes, 1)
	assert.Equal(t, []string{"sample-Keys"}, row.Keys)
	assert.Equal(t, sampleInner{Label: "sample-Label", N: 41}, row.Inner)
	require.NotNil(t, row.Ptr)
	assert.Equal(t, row.Inner, *row.Ptr)
	assert.Empty(t, row.hidden, "an unexported field is left alone")
	assert.NotZero(t, row.Fixed[1])
	assert.NotZero(t, row.Duration)
}

// fakeStore has the shape of a generated store's write side.
type fakeStore struct {
	byTime   []sampleRow
	byNumber []sampleInner
	orders   []any
	flushed  int
	failOn   string
}

func (inst *fakeStore) IngestAlpha(_ context.Context, ts time.Time, rows []sampleRow) error {
	if inst.failOn == "Alpha" {
		return errors.New("refused")
	}
	inst.byTime, inst.orders = append(inst.byTime, rows...), append(inst.orders, ts)
	return nil
}

func (inst *fakeStore) IngestBeta(_ context.Context, ord uint64, rows []sampleInner) error {
	inst.byNumber, inst.orders = append(inst.byNumber, rows...), append(inst.orders, ord)
	return nil
}

// Not an ingest: another shape under the same prefix.
func (inst *fakeStore) IngestNothing(_ context.Context) error { return errors.New("not to be called") }

func (inst *fakeStore) Flush(context.Context) (int, error) { inst.flushed++; return 2, nil }

func TestIngestSamples(t *testing.T) {
	st := &fakeStore{}
	components, err := recordstore.IngestSamples(context.Background(), st, 1000)
	require.NoError(t, err)
	assert.Equal(t, []string{"Alpha", "Beta"}, components)
	require.Len(t, st.byTime, 1)
	require.Len(t, st.byNumber, 1)
	assert.EqualValues(t, 1001, st.byTime[0].Id, "each component's row has a key of its own above the base")
	assert.EqualValues(t, 1002, st.byNumber[0].N)
	assert.Equal(t, []any{recordstore.SampleOrder, uint64(1002)}, st.orders)
	assert.Equal(t, 1, st.flushed)

	_, err = recordstore.IngestSamples(context.Background(), &fakeStore{failOn: "Alpha"}, 0)
	require.ErrorContains(t, err, "refused")
	_, err = recordstore.IngestSamples(context.Background(), &struct{}{}, 0)
	require.Error(t, err, "a value that is no store")
}

func TestRegisterSampleRows(t *testing.T) {
	write := func(context.Context, recordstore.ExecutorI) error { return nil }
	recordstore.RegisterSampleRows("scratch_sample.facts", recordstore.SampleRowWriter{Name: "b", Write: write})
	recordstore.RegisterSampleRows("`scratch_sample`.`facts`", recordstore.SampleRowWriter{Name: "a", Write: write})
	ws := recordstore.SampleRowWriters("scratch_sample.facts")
	require.Len(t, ws, 2)
	assert.Equal(t, "a", ws[0].Name)
	assert.Equal(t, "b", ws[1].Name)
	assert.Empty(t, recordstore.SampleRowWriters("scratch_sample.other"))
	assert.Panics(t, func() {
		recordstore.RegisterSampleRows("scratch_sample.facts", recordstore.SampleRowWriter{Name: "a", Write: write})
	})
	assert.Panics(t, func() { recordstore.RegisterSampleRows("facts", recordstore.SampleRowWriter{Name: "c", Write: write}) })
	assert.Panics(t, func() { recordstore.RegisterSampleRows("scratch_sample.facts", recordstore.SampleRowWriter{Name: "c"}) })
}
