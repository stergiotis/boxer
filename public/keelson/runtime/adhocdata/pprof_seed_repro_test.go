package adhocdata_test

import (
	"bytes"
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/observability/profiling/pprofarrow"
)

var reproSink [][]byte

// TestPprofSeedQueryE2E reproduces the imzrt Profiles flow end to end for
// the instantaneous kinds: capture → pprofarrow.Convert → publish →
// the Explore seed SQL over keelson('<handle>') through the production
// endpoint wiring.
func TestPprofSeedQueryE2E(t *testing.T) {
	svc, query := setupE2E(t)

	for range 2048 {
		reproSink = append(reproSink, make([]byte, 2048))
	}
	runtime.GC()

	for _, kind := range []string{"heap", "allocs", "goroutine"} {
		var buf bytes.Buffer
		require.NoError(t, pprof.Lookup(kind).WriteTo(&buf, 0), kind)
		conv, err := pprofarrow.Convert(bytes.NewReader(buf.Bytes()), pprofarrow.WithKindHint(kind))
		require.NoError(t, err, kind)
		require.Positive(t, conv.Rows, kind)

		res, err := svc.Publish(adhocdata.PublishInput{
			Alias: "pprof_" + kind, Publisher: "test", ArrowIPCStream: conv.IPCStream,
		})
		require.NoError(t, err, kind)

		out := query("SELECT leaf AS fn, pkg, sum(value) AS self\n" +
			"FROM keelson('" + res.Handle + "')\n" +
			"GROUP BY fn, pkg\nORDER BY self DESC\nLIMIT 5")
		t.Logf("%s top rows:\n%s", kind, out)
		require.NotEmpty(t, out, kind)
	}
}

// TestLargeDatasetPartialReadE2E is the regression pin for the live
// failure this file was born from (2026-08-01): ClickHouse's Arrow
// reader SKIPS the buffers of columns a query does not touch by
// re-requesting the source from a later offset, so a query that leaves a
// multi-megabyte column tail unread only works if /table honors HTTP
// range requests. Before range support, this query died with
// HTTP_RANGE_NOT_SATISFIABLE after ~30 s of retries (live symptom: a
// heap-profile dataset's Explore window stuck on "Executing query…").
// The dataset shape mirrors the pprof one: leading columns the query
// reads, a bulky trailing column it does not.
func TestLargeDatasetPartialReadE2E(t *testing.T) {
	svc, query := setupE2E(t)

	const rows = 120_000
	mem := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "s", Type: arrow.BinaryTypes.String},
		{Name: "v", Type: arrow.PrimitiveTypes.Int64},
		{Name: "tail", Type: arrow.BinaryTypes.String},
	}, nil)
	rb := array.NewRecordBuilder(mem, schema)
	defer rb.Release()
	sb := rb.Field(0).(*array.StringBuilder)
	vb := rb.Field(1).(*array.Int64Builder)
	tb := rb.Field(2).(*array.StringBuilder)
	pad := strings.Repeat("t", 64)
	for i := range rows {
		sb.Append("row")
		vb.Append(int64(i))
		tb.Append(pad)
	}
	rec := rb.NewRecordBatch()
	defer rec.Release()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema), ipc.WithAllocator(mem))
	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())

	res, err := svc.Publish(adhocdata.PublishInput{Alias: "skiptail", Publisher: "test", ArrowIPCStream: buf.Bytes()})
	require.NoError(t, err)

	// Reads s and v, leaves the ~7.7 MB tail column unread — the skip
	// that needs a ranged re-request.
	out := query("SELECT count(*), sum(v) FROM keelson('" + res.Handle + "') WHERE s = 'row'")
	require.Equal(t, "120000\t7199940000", out)
}
