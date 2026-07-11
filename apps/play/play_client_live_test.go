package play

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/db/clickhouse/clickhouseenv"
)

// liveClickHouseURL returns the URL of a running ClickHouse HTTP endpoint or
// skips the test when none is configured. Set CLICKHOUSE_URL (e.g.
// http://localhost:8123/) to opt in.
func liveClickHouseURL(t *testing.T) string {
	t.Helper()
	raw, set := clickhouseenv.URL.Lookup()
	if !set {
		t.Skip("CLICKHOUSE_URL unset; skipping live integration test")
	}
	return raw
}

// ClickHouse's URL-param convention is `?param_<name>=<value>` ↔ placeholder
// `{<name>:Type}` — the `param_` prefix on the URL key is the marker, not part
// of the placeholder name. So `SET param_a = 42` resolves into placeholder
// `{a:UInt64}`, not `{param_a:UInt64}`.
func TestLiveExecuteArrowStreamMultiStatementParams(t *testing.T) {
	c := NewClient(ClientConfig{URL: liveClickHouseURL(t)}, nil)
	rdr, body, _, err := c.ExecuteArrowStream(
		context.Background(),
		`SET param_a = 42; SET param_b = 'hello world'; SELECT {a : UInt64} AS a, {b : String} AS b`,
		memory.NewGoAllocator(),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("ExecuteArrowStream: %v", err)
	}
	defer body.Close()
	defer rdr.Release()

	if !rdr.Next() {
		t.Fatalf("no record batch: %v", rdr.Err())
	}
	rec := rdr.RecordBatch()
	if rec.NumCols() != 2 {
		t.Fatalf("NumCols = %d, want 2", rec.NumCols())
	}
	if rec.NumRows() != 1 {
		t.Fatalf("NumRows = %d, want 1", rec.NumRows())
	}
	if got, want := formatCell(rec, 0, 0), "42"; got != want {
		t.Errorf("col a = %q, want %q", got, want)
	}
	if got, want := formatCell(rec, 1, 0), "hello world"; got != want {
		t.Errorf("col b = %q, want %q", got, want)
	}
}

func TestLiveExecuteArrowStreamLargeStringParam(t *testing.T) {
	url := liveClickHouseURL(t)
	c := NewClient(ClientConfig{URL: url}, nil)

	const size = 8000
	blob := make([]byte, size)
	for i := range blob {
		blob[i] = 'x'
	}
	sql := "SET param_blob = '" + string(blob) + "'; SELECT length({blob : String}) AS n"

	rdr, body, _, err := c.ExecuteArrowStream(context.Background(), sql, memory.NewGoAllocator(), nil, nil)
	if err != nil {
		t.Fatalf("ExecuteArrowStream: %v", err)
	}
	defer body.Close()
	defer rdr.Release()

	if !rdr.Next() {
		t.Fatalf("no record batch: %v", rdr.Err())
	}
	rec := rdr.RecordBatch()
	if got, want := formatCell(rec, 0, 0), "8000"; got != want {
		t.Errorf("length = %q, want %q", got, want)
	}
}
