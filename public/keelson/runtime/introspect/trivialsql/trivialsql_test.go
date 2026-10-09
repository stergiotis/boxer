package trivialsql

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// seqProvider is a table defined by its argument: n rows, v = 1..n, and a
// word per row.
type seqProvider struct{}

func (seqProvider) Name() string                         { return "seq" }
func (seqProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (seqProvider) Schema() *arrow.Schema                { return seqTable().Schema() }
func (seqProvider) Snapshot(introspect.Projection) (arrow.RecordBatch, error) {
	return nil, assert.AnError
}
func (seqProvider) Args() []introspect.ArgSpec {
	return []introspect.ArgSpec{{Name: "n", Type: introspect.ArgTypeUInt64, Required: true}}
}
func (seqProvider) SnapshotArgs(proj introspect.Projection, args introspect.Args) (arrow.RecordBatch, error) {
	return seqTable().Build(proj, int(args.UInt64("n"))), nil
}

func seqTable() *introspect.Table {
	words := []string{"a", "b\tc", "d", "e", "f"}
	return introspect.NewTable().
		Uint64("v", func(i int) uint64 { return uint64(i + 1) }).
		String("w", func(i int) string { return words[i%len(words)] })
}

// plainProvider takes no arguments.
type plainProvider struct{}

func (plainProvider) Name() string                         { return "plain" }
func (plainProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessStatic }
func (plainProvider) Schema() *arrow.Schema                { return seqTable().Schema() }
func (plainProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	return seqTable().Build(proj, 2), nil
}

func reg(t *testing.T) *introspect.Registry {
	t.Helper()
	r := introspect.NewRegistry()
	require.NoError(t, r.Register(seqProvider{}))
	require.NoError(t, r.Register(plainProvider{}))
	return r
}

func run(t *testing.T, sql string, params map[string]string) string {
	t.Helper()
	body, err := Run(context.Background(), reg(t), sql, params)
	require.NoError(t, err, sql)
	return string(body)
}

func TestRun_Shapes(t *testing.T) {
	cases := []struct {
		sql    string
		params map[string]string
		want   string
	}{
		{"SELECT * FROM keelson('plain')", nil, "1\ta\n2\tb\\tc\n"},
		{"SELECT * FROM keelson(plain) AS p FORMAT TabSeparated", nil, "1\ta\n2\tb\\tc\n"},
		{"SELECT * FROM keelson('seq', n = 3)", nil, "1\ta\n2\tb\\tc\n3\td\n"},
		{"SELECT * FROM keelson('seq', n = {k:UInt64}) LIMIT 1", map[string]string{"k": "4"}, "1\ta\n"},
		{"SET param_k = 4; SELECT * FROM keelson('seq', n = {k:UInt64}) LIMIT 1 OFFSET 3", nil, "4\te\n"},
		{"SELECT * FROM keelson('seq', n = 5) LIMIT 1, 2", nil, "2\tb\\tc\n3\td\n"},
		{"WITH vf AS (SELECT * FROM keelson('seq', n = 2)) SELECT * FROM vf", nil, "1\ta\n2\tb\\tc\n"},
		{"SELECT * FROM keelson('seq', n = 9) LIMIT 0", nil, ""},
		{"SELECT * FROM keelson('plain') SETTINGS max_threads = 1 FORMAT JSONEachRow", nil, "{\"v\":1,\"w\":\"a\"}\n{\"v\":2,\"w\":\"b\\tc\"}\n"},
		{"SET max_threads = 1; SELECT * FROM keelson('plain') FORMAT tsv", nil, "1\ta\n2\tb\\tc\n"},
		{"SELECT * FROM keelson('plain') FORMAT TSVWithNames", nil, "v\tw\n1\ta\n2\tb\\tc\n"},
		{"SELECT * FROM keelson('plain') FORMAT CSV", nil, "1,\"a\"\n2,\"b\tc\"\n"},
		{"SELECT * FROM keelson('plain') FORMAT CSVWithNames", nil, "\"v\",\"w\"\n1,\"a\"\n2,\"b\tc\"\n"},
		{"SELECT * FROM keelson('seq', n = 3) LIMIT 1, 9223372036854775807", nil, "2\tb\\tc\n3\td\n"},
		{"SELECT * FROM keelson('seq', n = 3) LIMIT 9223372036854775807 OFFSET 9223372036854775807", nil, ""},
		// Constants: a row of them, beside *, from WITH, as an argument.
		{"SELECT 1 AS a, 'x' AS b", nil, "1\tx\n"},
		{"SELECT 1, -1, 1.50, 1e3, 0x10, 'it''s', TRUE FORMAT TSVWithNames", nil, "1\t-1\t1.5\t1000.\t16\t\\'it\\\\\\'s\\'\ttrue\n1\t-1\t1.5\t1000\t16\tit\\'s\ttrue\n"},
		{"SELECT {k:String} AS k FORMAT JSONEachRow", map[string]string{"k": `a\tb`}, "{\"k\":\"a\\tb\"}\n"},
		{"SELECT *, 'storm' AS src FROM keelson('seq', n = 2)", nil, "1\ta\tstorm\n2\tb\\tc\tstorm\n"},
		{"SELECT 7 AS k, * FROM keelson('plain') LIMIT 1 FORMAT TSVWithNames", nil, "k\tv\tw\n7\t1\ta\n"},
		{"SELECT 0 AS z FROM keelson('seq', n = 3)", nil, "0\n0\n0\n"},
		{"WITH 2 AS k SELECT * FROM keelson('seq', n = k)", nil, "1\ta\n2\tb\\tc\n"},
		{"WITH 2 AS k, k AS j SELECT k, j, k AS kk FORMAT TSVWithNames", nil, "k\tj\tkk\n2\t2\t2\n"},
		{"WITH vf AS (SELECT *, 9 AS nine FROM keelson('seq', n = 1)) SELECT * FROM vf", nil, "1\ta\t9\n"},
		{"SELECT 1 AS a LIMIT 0", nil, ""},
		// Inline tables.
		{"SELECT * FROM values('a UInt8, b String', (1, 'x'), (2, 'y'))", nil, "1\tx\n2\ty\n"},
		{"SELECT * FROM values('a Nullable(UInt8)', NULL, 3, (4))", nil, "\\N\n3\n4\n"},
		{"SELECT * FROM values('a Float32, b Bool, c String, d UInt8', (1, 0, 5, '7'), (2.0, 1, -3, 8.0))", nil, "1\tfalse\t5\t7\n2\ttrue\t-3\t8\n"},
		{"SELECT * FROM values((1, -1.5), (300, 3)) FORMAT TSVWithNames", nil, "c1\tc2\n1\t-1.5\n300\t3\n"},
		{"SELECT * FROM values(255, -1, NULL) FORMAT JSONEachRow", nil, "{\"c1\":255}\n{\"c1\":-1}\n{\"c1\":null}\n"},
		{"WITH 'a UInt8' AS s, 4 AS k SELECT *, 'v' AS t FROM values(s, k, {k:UInt8})", map[string]string{"k": "5"}, "4\tv\n5\tv\n"},
		{"WITH t AS (SELECT * FROM values('a String', 'p', 'q')) SELECT * FROM t LIMIT 1, 1", nil, "q\n"},
		{"SELECT * FROM values('it''s', 'a b')", nil, "it\\'s\na b\n"},
		{"SELECT * FROM values('a UInt8')", nil, "a UInt8\n"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, run(t, c.sql, c.params), c.sql)
	}
}

func TestRun_ArrowStream(t *testing.T) {
	batch, format, err := Read(reg(t), "SELECT * FROM keelson('seq', n = 4) FORMAT ArrowStream", nil)
	require.NoError(t, err)
	defer batch.Release()
	assert.Equal(t, FormatArrowStream, format)
	assert.Equal(t, int64(4), batch.NumRows())
	body, err := Encode(batch, format)
	require.NoError(t, err)
	assert.NotEmpty(t, body)
}

func TestRun_RefusesWhatNeedsClickHouse(t *testing.T) {
	for _, sql := range []string{
		"SELECT now()",
		"SELECT 1 + 1 AS two",
		"SELECT NULL",
		"SELECT {k:UInt64}",
		"SELECT 18446744073709551616 AS big",
		"SELECT *",
		"WITH now() AS t SELECT t",
		"SELECT *, 1 AS v FROM keelson('plain')",
		"WITH 1 AS v SELECT *, v FROM keelson('plain')",
		"SELECT * FROM values('t DateTime', 1)",
		"SELECT * FROM values('hello world', 'x')",
		"SELECT * FROM values(1, 'a')",
		"SELECT * FROM values(18446744073709551615, 1.5)",
		"SELECT * FROM values(NULL, NULL)",
		// Review of bed494954: a number abutting letters, a point-led
		// number, an alias without AS, a parenthesised parameter, and a
		// parameter a SET binds to a value not modelled here.
		"SELECT 0b11",
		"SELECT 1_000",
		"SELECT 1a",
		"SELECT .5",
		"SELECT 1 a",
		"SELECT ({k:UInt64})",
		"SET param_k = [1]; SELECT {k:UInt8} AS k",
		"SET param_k = NULL; SELECT * FROM keelson('seq', n = {k:UInt64})",
		// Review of bed494954, items 7–11.
		"SELECT 'a\\xZZ' AS s",
		"SELECT 'a\\x4' AS s",
		"SELECT * FROM values('a UInt8', 'x')",
		"SELECT * FROM values('a UInt8', '256')",
		"SELECT * FROM values('a Float64', '1_000')",
		"SELECT * FROM values('a Bool', 'maybe')",
		"WITH q AS (SELECT 1 AS a, 2 AS b), 5 AS a SELECT * FROM q",
		"WITH q AS (SELECT * FROM keelson('seq', n = 1)), 5 AS v SELECT * FROM q",
		"WITH 5 AS v SELECT * FROM keelson('seq', n = 1)",
		"WITH now() AS t SELECT * FROM keelson('plain')",
		"WITH {k:Nope} AS x SELECT 1 AS one",
		"WITH 1 SELECT * FROM keelson('plain')",
		"WITH a AS (SELECT 1 AS x), 2 AS a SELECT * FROM a",
		"SELECT * FROM values('a UInt8', 1) SETTINGS database = 'nope'",
		"SELECT * FROM values('`a` String', 'x')",
		"SELECT * FROM values('(a UInt8', 1)",
		"SELECT v FROM keelson('plain')",
		"SELECT * FROM keelson('plain') WHERE v = 1",
		"SELECT * FROM keelson('plain') ORDER BY v",
		"SELECT count() FROM keelson('plain')",
		"SELECT * FROM keelson('plain') JOIN keelson('plain') USING v",
		"SELECT * FROM keelson('plain') UNION ALL SELECT * FROM keelson('plain')",
		"SELECT * FROM (SELECT * FROM keelson('plain'))",
		"SELECT * FROM system.tables",
		"SELECT * FROM url('http://x', 'ArrowStream')",
		"WITH x AS (SELECT v FROM keelson('plain')) SELECT * FROM x",
		"SET limit = 1; SELECT * FROM keelson('plain')",
		"SELECT * FROM keelson('plain') SETTINGS limit = 1",
		"SELECT * FROM keelson('plain') SETTINGS max_result_rows = 1",
		"SELECT * FROM keelson('plain') SETTINGS output_format_json_quote_64bit_integers = 1 FORMAT JSONEachRow",
		"SELECT * FROM (keelson('plain'))",
		"SELECT * FROM keelson('plain') FORMAT Pretty",
		"SELECT * FROM keelson('plain') FORMAT JSON",
		"INSERT INTO t VALUES (1)",
	} {
		_, err := Run(context.Background(), reg(t), sql, nil)
		assert.True(t, errors.Is(err, ErrNeedsClickHouse), "%s: %v", sql, err)
	}
}

func TestRun_CallErrorsAreNotRefusals(t *testing.T) {
	for _, sql := range []string{
		"SELECT * FROM values('a UInt8', 300)",
		"SELECT * FROM values('a Float32', 1e40)",
		"SELECT * FROM values('a Float32', -3.4028236e38)",
		"SELECT * FROM values('a UInt64', 1.8446744073709552e19)",
		"SELECT * FROM values('a Int64', 9.223372036854776e18)",
		"SELECT * FROM values('a UInt8', -1)",
		"SELECT * FROM values('a Int32', 1.5)",
		"SELECT * FROM values('a UInt8', NULL)",
		"SELECT * FROM values('a UInt8, b String', (1))",
		"SELECT * FROM values((1, 'a'), (2))",
		"WITH a AS (SELECT 1 AS x), a AS (SELECT 2 AS x) SELECT * FROM a",
		"WITH 1 AS k, 2 AS k SELECT k",
		"SELECT {k:UInt64} AS k",
		"SELECT * FROM keelson('nope')",
		"SELECT * FROM keelson('seq')",
		"SELECT * FROM keelson('seq', n = {k:UInt64})",
		"SELECT * FROM keelson('plain', n = 1)",
	} {
		_, err := Run(context.Background(), reg(t), sql, nil)
		require.Error(t, err, sql)
		assert.False(t, errors.Is(err, ErrNeedsClickHouse), "%s is a bad call, not a statement for a server: %v", sql, err)
	}
}

func TestRunner_Settings(t *testing.T) {
	r := Runner{Registry: reg(t)}
	body, format, err := r.RunSQLSettings(context.Background(), "SELECT * FROM keelson('plain')", nil,
		map[string]string{"default_format": "JSONEachRow", "readonly": "2", "log_comment": "x"})
	require.NoError(t, err)
	assert.Equal(t, FormatJSONEachRow, format)
	assert.Equal(t, "{\"v\":1,\"w\":\"a\"}\n{\"v\":2,\"w\":\"b\\tc\"}\n", string(body))

	body, format, err = r.RunSQLSettings(context.Background(), "SELECT * FROM keelson('plain') LIMIT 1 FORMAT tsv", nil,
		map[string]string{"default_format": "JSONEachRow"})
	require.NoError(t, err)
	assert.Equal(t, FormatTabSeparated, format, "the statement's FORMAT wins, canonicalised")
	assert.Equal(t, "1\ta\n", string(body))

	for _, s := range []map[string]string{{"limit": "1"}, {"enable_http_compression": "1"}, {"default_format": "Pretty"}, {"database": "nope"}} {
		_, _, err = r.RunSQLSettings(context.Background(), "SELECT * FROM keelson('plain')", nil, s)
		assert.True(t, errors.Is(err, ErrNeedsClickHouse), "%v: %v", s, err)
	}
}

// A type the text formats do not render as ClickHouse does is refused in
// them and carried by ArrowStream.
func TestEncode_RefusesWhatItCannotRenderAsClickHouse(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{{Name: "t", Type: arrow.FixedWidthTypes.Timestamp_us}}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer b.Release()
	b.Field(0).(*array.TimestampBuilder).Append(1)
	batch := b.NewRecordBatch()
	defer batch.Release()
	for _, f := range []string{FormatTabSeparated, FormatCSV, FormatJSONEachRow} {
		_, err := Encode(batch, f)
		assert.True(t, errors.Is(err, ErrNeedsClickHouse), f)
	}
	_, err := Encode(batch, FormatArrowStream)
	assert.NoError(t, err)
}

func TestWriteFloat(t *testing.T) {
	for _, c := range []struct {
		v    float64
		bits int
		want string
	}{
		{0.1, 64, "0.1"}, {math.Copysign(0, -1), 64, "-0"}, {1e20, 64, "100000000000000000000"}, {1e21, 64, "1e21"},
		{1e-6, 64, "0.000001"}, {1e-7, 64, "1e-7"}, {1.5e-10, 64, "1.5e-10"}, {123456.789, 64, "123456.789"},
		{5e-324, 64, "5e-324"}, {-2.5e300, 64, "-2.5e300"}, {float64(float32(0.1)), 32, "0.1"}, {16777217, 32, "16777216"},
	} {
		var buf bytes.Buffer
		writeFloat(&buf, c.v, c.bits, styleTSV)
		assert.Equal(t, c.want, buf.String(), "%v", c.v)
	}
}
