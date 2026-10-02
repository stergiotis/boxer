//go:build integration

package play

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Play's own writes reach a live server through the seam (ADR-0270 §SD7):
// a pin round, its dedup on a second round, and a Series verdict.
func TestLiveAppWritesPinAndVerdict(t *testing.T) {
	cli := NewClient(ClientConfig{URL: liveClickHouseURL(t)}, nil)
	ctx := context.Background()

	b := array.NewRecordBuilder(memory.NewGoAllocator(), arrow.NewSchema([]arrow.Field{
		{Name: "count()", Type: arrow.PrimitiveTypes.Uint64},
		{Name: "note", Type: arrow.BinaryTypes.String},
	}, nil))
	defer b.Release()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	b.Field(0).(*array.Uint64Builder).AppendValues([]uint64{1, 2}, nil)
	b.Field(1).(*array.StringBuilder).AppendValues([]string{"live-test", stamp}, nil)
	rec := b.NewRecordBatch()
	defer rec.Release()

	fp := fingerprintRecord(rec)
	meta := pinMetaRow{Fingerprint: fp, DataTable: pinDataTableName(fp), App: "play-live-test", Lane: "main",
		NumRows: uint64(rec.NumRows()), NumCols: uint64(rec.NumCols())}
	t.Cleanup(func() {
		_, _ = cli.rawTsvQuery(ctx, "DROP TABLE IF EXISTS "+meta.DataTable)
		_, _ = cli.rawTsvQuery(ctx, "DELETE FROM "+pinMetaTable+" WHERE fingerprint = "+strconv.FormatUint(fp, 10))
	})
	d := newPinDriver(cli)
	already, err := d.doPin(ctx, rec, meta, appWriteLabel{})
	require.NoError(t, err)
	assert.False(t, already)
	raw, err := cli.rawTsvQuery(ctx, "SELECT count() FROM "+meta.DataTable+" FORMAT TabSeparated")
	require.NoError(t, err)
	assert.Equal(t, "2", strings.TrimSpace(string(raw)))
	already, err = d.doPin(ctx, rec, meta, appWriteLabel{})
	require.NoError(t, err)
	assert.True(t, already, "the second round finds the first")

	hash := "play-live-test-" + stamp
	t.Cleanup(func() {
		_, _ = cli.rawTsvQuery(ctx, "DELETE FROM "+tsLabelsTable+" WHERE input_hash = '"+hash+"'")
	})
	w := newTsLabelsWriter(cli)
	require.NoError(t, w.doWrite(ctx, tsLabelRow{InputHash: hash, SpanFrom: "2026-01-01 00:00:00.000",
		SpanTo: "2026-01-01 00:05:00.000", Verdict: "confirmed", Detector: "live-test"}, appWriteLabel{}))
	raw, err = cli.rawTsvQuery(ctx, "SELECT count() FROM "+tsLabelsTable+" WHERE input_hash = '"+hash+"' FORMAT TabSeparated")
	require.NoError(t, err)
	assert.Equal(t, "1", strings.TrimSpace(string(raw)))
}
