package queryrunfacts

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml"
)

func TestDeterministicIdBandAndStability(t *testing.T) {
	a := DeterministicId("play-map-1234-7", 1752700000000000, "QueryFinish")
	b := DeterministicId("play-map-1234-7", 1752700000000000, "QueryFinish")
	require.Equal(t, a, b, "same event must yield the same id")
	require.NotZero(t, a&IdBand, "capture ids live in the reserved band")

	// A lane's stable query_id reused by a later run is a different event.
	c := DeterministicId("play-map-1234-7", 1752700005000000, "QueryFinish")
	require.NotEqual(t, a, c)
	d := DeterministicId("play-map-1234-7", 1752700000000000, "ExceptionWhileProcessing")
	require.NotEqual(t, a, d)
}

func TestParseStamp(t *testing.T) {
	st, ok := ParseStamp(`{"run_id":"r1","app":"data.play","lane":"map","authored_fp":"a","sent_fp":"s","chain_fp":"c","env_fp":"e"}`)
	require.True(t, ok)
	require.Equal(t, "r1", st.RunId)
	require.Equal(t, "data.play", st.App)
	require.Equal(t, "map", st.Lane)
	require.Equal(t, "a", st.AuthoredFp)
	require.Equal(t, "e", st.EnvFp)

	st, ok = ParseStamp(`{"run_id":"r2"}`)
	require.True(t, ok)
	require.Equal(t, "r2", st.RunId)
	require.Empty(t, st.Lane)

	_, ok = ParseStamp("")
	require.False(t, ok)
	_, ok = ParseStamp("plain comment")
	require.False(t, ok)
	_, ok = ParseStamp(`{"unrelated":"json"}`)
	require.False(t, ok)
}

func TestComposeExtractSql(t *testing.T) {
	sql, err := ComposeExtractSql("boxer.facts", "http://127.0.0.1:8127/pull", ScopeAll, 0)
	require.NoError(t, err)
	require.Contains(t, sql, "FROM system.query_log")
	require.Contains(t, sql, "type != 'QueryStart'")
	require.Contains(t, sql, "'queryrunsd-extract', 'queryrunsd-refresh'")
	require.Contains(t, sql, "position(query, 'http://127.0.0.1:8127/pull') = 0")
	require.Contains(t, sql, "SELECT max("+ColTs+") FROM boxer.facts")
	require.Contains(t, sql, WatermarkOverlap)
	require.Contains(t, sql, "LIMIT 10000")
	require.Contains(t, sql, "log_comment='queryrunsd-extract'")
	require.Contains(t, sql, "FORMAT JSONEachRow")
	require.NotContains(t, sql, "JSONHas")

	sql, err = ComposeExtractSql("boxer.facts", "http://127.0.0.1:8127/pull", ScopeStamped, 500)
	require.NoError(t, err)
	require.Contains(t, sql, "JSONHas(log_comment, 'run_id')")
	require.Contains(t, sql, "LIMIT 500")

	_, err = ComposeExtractSql("boxer.facts", "http://127.0.0.1:8127/pull", ScopeOff, 0)
	require.Error(t, err, "off must not compose an extract")
	_, err = ComposeExtractSql("", "http://127.0.0.1:8127/pull", ScopeAll, 0)
	require.Error(t, err)
}

func TestUrlStructureMatchesBuilderSchema(t *testing.T) {
	structure, err := UrlStructure()
	require.NoError(t, err)
	require.NotContains(t, structure, "CODEC", "codecs must not leak into the structure clause")
	require.Contains(t, structure, "`id:id:u64:2k:0:0:` UInt64")
	require.Contains(t, structure, "`ts:ts:z64:2k:0:0:` DateTime64(9,'UTC')")

	// The structure clause must cover exactly the fields the generated
	// builder emits — this is the wire contract between the /pull
	// response and the MV's url() read.
	ent := dml.NewInEntityFacts(memory.NewGoAllocator(), 1)
	schema := ent.GetSchema()
	require.Equal(t, schema.NumFields(), strings.Count(structure, "`")/2,
		"structure column count must match the Arrow schema")
	for _, f := range schema.Fields() {
		require.Contains(t, structure, "`"+f.Name+"` ", "schema field %q missing from structure", f.Name)
	}
}

func TestComposeMvSql(t *testing.T) {
	sql, err := ComposeMvSql("boxer.mv_queryruns", "boxer.facts", "http://127.0.0.1:8127/pull", 5)
	require.NoError(t, err)
	require.Contains(t, sql, "CREATE MATERIALIZED VIEW IF NOT EXISTS boxer.mv_queryruns")
	require.Contains(t, sql, "REFRESH EVERY 5 SECOND APPEND TO boxer.facts")
	require.Contains(t, sql, "url('http://127.0.0.1:8127/pull', 'ArrowStream', '")
	require.Contains(t, sql, ColId+" NOT IN")
	require.Contains(t, sql, AntiJoinWindow)
	require.Contains(t, sql, "log_comment='queryrunsd-refresh'")
	// DateTime64(9,'UTC') carries single quotes — inside the structure
	// string literal they must arrive doubled.
	require.Contains(t, sql, "DateTime64(9,''UTC'')")

	_, err = ComposeMvSql("", "boxer.facts", "u", 5)
	require.Error(t, err)
	_, err = ComposeMvSql("boxer.mv_queryruns", "boxer.facts", "u", 0)
	require.Error(t, err)

	drop, err := ComposeDropMvSql("boxer.mv_queryruns")
	require.NoError(t, err)
	require.Equal(t, "DROP TABLE IF EXISTS boxer.mv_queryruns", drop)
}

func TestBuildEntitiesEncodesRows(t *testing.T) {
	rows := []Row{
		{
			Type:           "QueryFinish",
			EventUs:        1752700000123456,
			QueryId:        "play-map-1234-7",
			Query:          "SELECT 1",
			NormalizedHash: 18446744073709551615, // full-range u64 must survive
			QueryKind:      "Select",
			DurationMs:     42,
			ReadRows:       100,
			ReadBytes:      2048,
			ResultRows:     1,
			ResultBytes:    64,
			MemoryUsage:    1 << 20,
			ProfileEvents:  map[string]uint64{"SelectedRows": 100, "NetworkSendBytes": 512},
			LogComment:     `{"run_id":"r1","app":"data.play","lane":"map","authored_fp":"af","sent_fp":"sf","chain_fp":"cf","env_fp":"ef"}`,
		},
		{
			Type:          "ExceptionWhileProcessing",
			EventUs:       1752700001000000,
			QueryId:       "adhoc-1",
			Query:         "SELECT broken",
			QueryKind:     "Select",
			ExceptionCode: 47,
			Exception:     "DB::Exception: Missing columns",
		},
	}
	ent := dml.NewInEntityFacts(memory.NewGoAllocator(), len(rows))
	require.NoError(t, BuildEntities(ent, rows))
	records, err := ent.TransferRecords(nil)
	require.NoError(t, err)
	defer func() {
		for _, r := range records {
			r.Release()
		}
	}()
	require.Len(t, records, 1)
	rec := records[0]
	require.EqualValues(t, 2, rec.NumRows())

	idIdx := rec.Schema().FieldIndices("id:id:u64:2k:0:0:")
	require.Len(t, idIdx, 1)
	ids := rec.Column(idIdx[0]).(*array.Uint64)
	require.Equal(t, DeterministicId("play-map-1234-7", 1752700000123456, "QueryFinish"), ids.Value(0))
	require.Equal(t, DeterministicId("adhoc-1", 1752700001000000, "ExceptionWhileProcessing"), ids.Value(1))

	nkIdx := rec.Schema().FieldIndices("id:naturalKey:y:g:0:0:")
	require.Len(t, nkIdx, 1)
	require.Equal(t, []byte("play-map-1234-7"), binaryValue(t, rec.Column(nkIdx[0]), 0))
	require.Equal(t, []byte("adhoc-1"), binaryValue(t, rec.Column(nkIdx[0]), 1))
}

func TestBuildEntitiesEmpty(t *testing.T) {
	ent := dml.NewInEntityFacts(memory.NewGoAllocator(), 0)
	require.NoError(t, BuildEntities(ent, nil))
	records, err := ent.TransferRecords(nil)
	require.NoError(t, err)
	require.Empty(t, records, "no rows → no records; the service serves schema-only")
}

func TestTruncateRuneSafe(t *testing.T) {
	require.Equal(t, "abc", truncateRuneSafe("abc", 8))
	require.Equal(t, "ab", truncateRuneSafe("abcd", 2))
	// 3-byte runes: the cap lands mid-rune, the partial rune must go.
	s := strings.Repeat("€", 3) // 9 bytes
	out := truncateRuneSafe(s, 4)
	require.Equal(t, "€", out)
}

func binaryValue(t *testing.T, col arrow.Array, row int) (out []byte) {
	t.Helper()
	switch c := col.(type) {
	case *array.Binary:
		out = c.Value(row)
	case *array.String:
		out = []byte(c.Value(row))
	case *array.LargeBinary:
		out = c.Value(row)
	default:
		t.Fatalf("unexpected naturalKey column type %T", col)
	}
	return
}
