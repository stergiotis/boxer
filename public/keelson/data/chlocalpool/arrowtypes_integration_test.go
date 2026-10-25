//go:build integration

package chlocalpool

// The Arrow types the engine emits and infers on the worker path. Go code on
// both sides of that path switches on Arrow type IDs, so a release that
// changes a mapping — 26.2 moved `Date` from uint16 to date32, 26.9 made a
// nullable struct infer as `Nullable(Tuple)` — should fail here first. The
// schema is read with arrow-go, not with ClickHouse, so the check is not
// circular.

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runOutput runs one statement on a fresh worker and returns its stdout and
// stderr.
func runOutput(tb testing.TB, sql string, format string) (out []byte, stderr string, err error) {
	tb.Helper()
	w, err := newWorker(context.Background(), m0Config(tb), nil)
	require.NoError(tb, err)
	defer func() { _ = w.Close() }()
	if err = w.WriteSQL(sql, format); err != nil {
		return
	}
	out, err = io.ReadAll(w.Stdout())
	if err != nil {
		return
	}
	err = w.Wait()
	stderr = string(w.StderrTail())
	return
}

// engineVersion returns the worker binary's major and minor release.
func engineVersion(tb testing.TB) (major int, minor int) {
	tb.Helper()
	out, stderr, err := runOutput(tb, "SELECT version()", "TSV")
	require.NoError(tb, err, stderr)
	parts := strings.SplitN(strings.TrimSpace(string(out)), ".", 3)
	require.Len(tb, parts, 3, "version %q", out)
	major, err = strconv.Atoi(parts[0])
	require.NoError(tb, err)
	minor, err = strconv.Atoi(parts[1])
	require.NoError(tb, err)
	return
}

func atLeast(major, minor, wantMajor, wantMinor int) bool {
	return major > wantMajor || (major == wantMajor && minor >= wantMinor)
}

func TestArrow_EmittedTypes(t *testing.T) {
	cases := []struct {
		expr     string
		want     string
		nullable bool
	}{
		{expr: "toDate('2026-01-02')", want: "date32"}, // uint16 before 26.2
		{expr: "toDate32('2026-01-02')", want: "date32"},
		{expr: "toDateTime('2026-01-02 03:04:05', 'UTC')", want: "uint32"}, // not a timestamp
		{expr: "toDateTime64('2026-01-02 03:04:05.123', 3, 'UTC')", want: "timestamp[ms, tz=UTC]"},
		{expr: "(1, 'a')::Tuple(a UInt8, b String)", want: "struct<a: uint8, b: utf8>"},
		{expr: "(1, 'a')::Nullable(Tuple(a UInt8, b String))", want: "struct<a: uint8, b: utf8>", nullable: true},
		{expr: "map('k', 1)", want: "map<utf8, uint8, items_non_nullable>"},
		{expr: "toLowCardinality('x')", want: "utf8"}, // no dictionary unless asked
		{expr: "[1, 2]", want: "list<item: uint8>"},
		{expr: "toUUID('00000000-0000-0000-0000-000000000001')", want: "fixed_size_binary[16]"},
		{expr: "toDecimal64(1.5, 2)", want: "decimal(18, 2)"},
		{expr: "toBool(1)", want: "bool"},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			out, stderr, err := runOutput(t, "SELECT "+c.expr+" AS c", "ArrowStream")
			require.NoError(t, err, stderr)
			r, err := ipc.NewReader(bytes.NewReader(out), ipc.WithAllocator(memory.DefaultAllocator))
			require.NoError(t, err)
			defer r.Release()
			f := r.Schema().Field(0)
			assert.Equal(t, c.want, f.Type.String())
			assert.Equal(t, c.nullable, f.Nullable)
		})
	}
}

// JSON and Dynamic columns fail in Arrow output through 26.9 ("Method
// getDataAt is not supported"); 26.10 writes them as binary by default
// (setting output_format_arrow_unsupported_types).
func TestArrow_JSONColumnOutput(t *testing.T) {
	major, minor := engineVersion(t)
	out, stderr, err := runOutput(t, `SELECT '{"a":1}'::JSON AS j`, "ArrowStream")
	if !atLeast(major, minor, 26, 10) {
		require.Error(t, err)
		assert.Contains(t, stderr, "NOT_IMPLEMENTED")
		return
	}
	require.NoError(t, err, stderr)
	r, err := ipc.NewReader(bytes.NewReader(out), ipc.WithAllocator(memory.DefaultAllocator))
	require.NoError(t, err)
	defer r.Release()
	assert.Equal(t, arrow.BINARY, r.Schema().Field(0).Type.ID())
}

// What an input table written by Go becomes once the prelude's
// `CREATE TEMPORARY TABLE … AS SELECT * FROM file(…, 'Arrow')` infers it.
func TestArrow_InferredInputTableTypes(t *testing.T) {
	st := arrow.StructOf(arrow.Field{Name: "a", Type: arrow.PrimitiveTypes.Int32}, arrow.Field{Name: "b", Type: arrow.BinaryTypes.String})
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "d32", Type: arrow.FixedWidthTypes.Date32},
		{Name: "d32n", Type: arrow.FixedWidthTypes.Date32, Nullable: true},
		{Name: "ts", Type: &arrow.TimestampType{Unit: arrow.Millisecond, TimeZone: "UTC"}},
		{Name: "st", Type: st},
		{Name: "stn", Type: st, Nullable: true},
		{Name: "lst", Type: arrow.ListOf(arrow.BinaryTypes.String)},
		{Name: "u16", Type: arrow.PrimitiveTypes.Uint16},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer b.Release()
	b.Field(0).(*array.Date32Builder).Append(arrow.Date32(20455))
	b.Field(1).(*array.Date32Builder).AppendNull()
	b.Field(2).(*array.TimestampBuilder).Append(1)
	for _, i := range []int{3, 4} {
		sb := b.Field(i).(*array.StructBuilder)
		sb.Append(true)
		sb.FieldBuilder(0).(*array.Int32Builder).Append(1)
		sb.FieldBuilder(1).(*array.StringBuilder).Append("x")
	}
	lb := b.Field(5).(*array.ListBuilder)
	lb.Append(true)
	lb.ValueBuilder().(*array.StringBuilder).Append("x")
	b.Field(6).(*array.Uint16Builder).Append(20455)
	rec := b.NewRecord()
	defer rec.Release()

	var buf bytes.Buffer
	fw, err := ipc.NewFileWriter(&buf, ipc.WithSchema(schema))
	require.NoError(t, err)
	require.NoError(t, fw.Write(rec))
	require.NoError(t, fw.Close())
	path := filepath.Join(t.TempDir(), "in.arrow")
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))

	out, stderr, err := runOutput(t,
		"CREATE TEMPORARY TABLE x AS SELECT * FROM file('"+path+"', 'Arrow');\n"+
			"SELECT name, type FROM system.columns WHERE table = 'x' AND database = '' ORDER BY position", "TSVRaw")
	require.NoError(t, err, stderr)
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name, typ, ok := strings.Cut(line, "\t")
		require.True(t, ok, line)
		got[name] = typ
	}
	assert.Equal(t, map[string]string{
		"d32":  "Date32",
		"d32n": "Nullable(Date32)",
		"ts":   "DateTime64(3, 'UTC')",
		"st":   "Tuple(a Int32, b String)",
		"stn":  "Nullable(Tuple(a Int32, b String))", // Tuple(Nullable(...)) before 26.9
		"lst":  "Array(Nullable(String))",            // arrow-go list items are nullable by default
		"u16":  "UInt16",                             // no date: a uint16 day count stays a number
	}, got)
}

// A statement that fails mid-stream still writes a well-formed ArrowStream,
// end-of-stream marker included: the record batches before the failure read
// as a complete result. The worker's exit status (the ClickHouse error code
// modulo 256) is the only failure signal, which is why a broker must not
// reply on the marker before the exit.
func TestArrow_MidStreamErrorReadsAsCompleteStream(t *testing.T) {
	out, stderr, err := runOutput(t,
		"SELECT number, throwIf(number = 50000) AS boom FROM numbers(100000) SETTINGS max_block_size = 1000", "ArrowStream")
	require.Error(t, err)
	assert.Contains(t, stderr, "FUNCTION_THROW_IF_VALUE_IS_NON_ZERO")
	assert.True(t, bytes.HasSuffix(out, []byte{0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0}), "stream ends with the end-of-stream marker")
	r, err := ipc.NewReader(bytes.NewReader(out), ipc.WithAllocator(memory.DefaultAllocator))
	require.NoError(t, err)
	defer r.Release()
	rows := int64(0)
	for r.Next() {
		rows += r.RecordBatch().NumRows()
	}
	require.NoError(t, r.Err())
	assert.Less(t, rows, int64(100000), "a truncated result")
}
