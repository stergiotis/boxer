package recordstore

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCreateTableColumns(t *testing.T) {
	ddl := "CREATE DATABASE IF NOT EXISTS boxer;\n\nCREATE TABLE IF NOT EXISTS boxer.t (\n" +
		"\t\"id:id:s:4::0:\" String CODEC(ZSTD(3)),\n" +
		"\t\"ts:ts:z64:47::0:\" DateTime64(9,'UTC') CODEC(Delta,ZSTD(3)),\n" +
		"\t\"tv:symbol:value:val:s:24:::0::data\" Array(LowCardinality(String)),\n" +
		"\tplain Enum8('a, b' = 1, 'c' = 2) DEFAULT 'c' COMMENT 'x, y',\n" +
		"\tINDEX idx \"id:id:s:4::0:\" TYPE bloom_filter GRANULARITY 4\n" +
		") ENGINE = MergeTree()\nORDER BY (\"id:id:s:4::0:\")\nSETTINGS allow_suspicious_low_cardinality_types=1\nCOMMENT 'boxer-table/1 kind=t columns=x naming=2'"
	cols, err := ParseCreateTableColumns(ddl)
	require.NoError(t, err)
	assert.Equal(t, []ColumnSpec{
		{Name: "id:id:s:4::0:", Type: "String", Codec: "ZSTD(3)"},
		{Name: "ts:ts:z64:47::0:", Type: "DateTime64(9,'UTC')", Codec: "Delta,ZSTD(3)"},
		{Name: "tv:symbol:value:val:s:24:::0::data", Type: "Array(LowCardinality(String))"},
		{Name: "plain", Type: "Enum8('a, b' = 1, 'c' = 2)"},
	}, cols)

	_, err = ParseCreateTableColumns("SELECT 1")
	require.Error(t, err)
	_, err = ParseCreateTableColumns("CREATE TABLE t (a String")
	require.Error(t, err)
}

// Every DDL golden a store generator wrote parses, and parses to the names
// the CREATE TABLE spells.
func TestParseCreateTableColumnsOverTheGoldens(t *testing.T) {
	n := 0
	err := filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, "_ddl_clickhouse.out.sql") {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		require.NoError(t, rerr)
		cols, perr := ParseCreateTableColumns(string(raw))
		require.NoError(t, perr, path)
		for _, c := range cols {
			assert.Contains(t, string(raw), `"`+c.Name+`" `+c.Type, path)
		}
		n++
		return nil
	})
	require.NoError(t, err)
	assert.Greater(t, n, 10)
}

func TestNormalizeTypeAndCodec(t *testing.T) {
	assert.Equal(t, NormalizeType("DateTime64(9,'UTC')"), NormalizeType("DateTime64(9, 'UTC')"))
	assert.NotEqual(t, NormalizeType("Enum8('a b' = 1)"), NormalizeType("Enum8('ab' = 1)"))
	assert.Equal(t, "Delta,ZSTD(3)", NormalizeCodec("CODEC(Delta, ZSTD(3))"))
	assert.Equal(t, "Delta,ZSTD(3)", NormalizeCodec("Delta,ZSTD(3)"))
	assert.Equal(t, "Array(UInt64)", stripLowCardinality("Array(LowCardinality(UInt64))"))
}

func TestAspectFreeKey(t *testing.T) {
	a, ok := AspectFreeKey("tv:symbol:lr:lr:u64:1247:::0::data")
	require.True(t, ok)
	b, ok := AspectFreeKey("tv:symbol:lr:lr:u64:47:3:1:0::data")
	require.True(t, ok)
	assert.Equal(t, a, b)
	c, ok := AspectFreeKey("tv:symbol:hr:hr:u64:47:::0::data")
	require.True(t, ok)
	assert.NotEqual(t, a, c, "another column of the section is not a rename")
	p1, ok := AspectFreeKey("id:id:s:4::0:")
	require.True(t, ok)
	p2, _ := AspectFreeKey("id:id:s:::0:")
	assert.Equal(t, p1, p2)
	p3, _ := AspectFreeKey("id:id:u64:4::0:")
	assert.NotEqual(t, p1, p3, "a canonical-type change is not an aspect change")
	_, ok = AspectFreeKey("name")
	assert.False(t, ok)
}

func TestJudgeColumns(t *testing.T) {
	want := []ColumnSpec{
		{Name: "id:id:s:4::0:", Type: "String", Codec: "ZSTD(3)"},
		{Name: "ts:ts:z64:47::0:", Type: "DateTime64(9,'UTC')"},
		{Name: "tv:s:lr:lr:u64:1247:::0::data", Type: "Array(LowCardinality(UInt64))"},
	}
	live := func(cols ...LiveColumn) []LiveColumn { return cols }
	id := LiveColumn{Name: "id:id:s:4::0:", Type: "String", Codec: "CODEC(ZSTD(3))"}
	ts := LiveColumn{Name: "ts:ts:z64:47::0:", Type: "DateTime64(9, 'UTC')"}
	lr := LiveColumn{Name: "tv:s:lr:lr:u64:1247:::0::data", Type: "Array(LowCardinality(UInt64))"}

	v := JudgeColumns(want, live(id, ts, lr))
	assert.Equal(t, SchemaVerdictOk, v.Verdict)
	assert.Empty(t, v.Notes)

	t.Run("extra columns are ok and reported", func(t *testing.T) {
		v := JudgeColumns(want, live(id, ts, lr, LiveColumn{Name: "dir", Type: "String", DefaultKind: "MATERIALIZED"}, LiveColumn{Name: "newer", Type: "UInt8"}))
		assert.Equal(t, SchemaVerdictOk, v.Verdict)
		assert.Equal(t, []string{"dir", "newer"}, v.Extra)
		require.NoError(t, v.Err())
	})
	t.Run("missing columns are migratable", func(t *testing.T) {
		v := JudgeColumns(want, live(id, ts))
		assert.Equal(t, SchemaVerdictMigratable, v.Verdict)
		assert.Equal(t, []string{"tv:s:lr:lr:u64:1247:::0::data"}, v.Missing)
		require.Error(t, v.Err())
	})
	t.Run("an aspect-only rename is migratable", func(t *testing.T) {
		old := LiveColumn{Name: "tv:s:lr:lr:u64:47:::0::data", Type: "Array(LowCardinality(UInt64))"}
		v := JudgeColumns(want, live(id, ts, old))
		assert.Equal(t, SchemaVerdictMigratable, v.Verdict)
		assert.Empty(t, v.Missing)
		assert.Empty(t, v.Extra)
		assert.Equal(t, []ColumnRename{{From: old.Name, To: lr.Name}}, v.Renames)
	})
	t.Run("a type change is incompatible", func(t *testing.T) {
		v := JudgeColumns(want, live(id, LiveColumn{Name: ts.Name, Type: "DateTime64(3, 'UTC')"}, lr))
		assert.Equal(t, SchemaVerdictIncompatible, v.Verdict)
		assert.Len(t, v.Reasons, 1)
	})
	t.Run("a computed column the store writes is incompatible", func(t *testing.T) {
		v := JudgeColumns(want, live(id, ts, LiveColumn{Name: lr.Name, Type: lr.Type, DefaultKind: "ALIAS"}))
		assert.Equal(t, SchemaVerdictIncompatible, v.Verdict)
	})
	t.Run("LowCardinality alone is a note", func(t *testing.T) {
		v := JudgeColumns(want, live(id, ts, LiveColumn{Name: lr.Name, Type: "Array(UInt64)"}))
		assert.Equal(t, SchemaVerdictOk, v.Verdict)
		assert.Len(t, v.Notes, 1)
	})
	t.Run("no columns is absent", func(t *testing.T) {
		assert.Equal(t, SchemaVerdictAbsent, JudgeColumns(want, nil).Verdict)
	})
}

func TestJudgeFactsStampAndWindow(t *testing.T) {
	want := []ColumnSpec{{Name: "a", Type: "String"}, {Name: "b", Type: "UInt8"}}
	stamp := TableStamp{Kind: "t", Columns: ColumnsDigest(want), Naming: LeewayNamingVersion}.String()
	c := TableContract{Kind: "t", Table: "db.t", Columns: want, Stamp: stamp}
	cols := []LiveColumn{{Name: "a", Type: "String"}, {Name: "b", Type: "UInt8"}}

	v := JudgeFacts(c, TableFacts{Exists: true, Engine: "MergeTree", Comment: stamp, CreateQuery: "… SETTINGS non_replicated_deduplication_window = 1000", Columns: cols})
	assert.Equal(t, SchemaVerdictOk, v.Verdict)
	assert.Equal(t, StampAgeSame, v.Age)
	assert.True(t, v.DedupWindow)
	assert.Empty(t, v.Notes)

	older := TableStamp{Kind: "t", Columns: "0000", Naming: LeewayNamingVersion}.String()
	v = JudgeFacts(c, TableFacts{Exists: true, Engine: "MergeTree", Comment: older, Columns: cols[:1]})
	assert.Equal(t, SchemaVerdictMigratable, v.Verdict)
	assert.Equal(t, StampAgeOlder, v.Age)
	assert.False(t, v.DedupWindow)
	assert.Contains(t, v.Summary(), "older schema")
	assert.Len(t, v.Notes, 1, "the missing window is noted")

	v = JudgeFacts(c, TableFacts{Exists: true, Engine: "MergeTree", Comment: older, Columns: append(cols, LiveColumn{Name: "c", Type: "UInt8"})})
	assert.Equal(t, StampAgeNewer, v.Age)

	v = JudgeFacts(c, TableFacts{Exists: true, Engine: "ReplicatedMergeTree", Comment: "hand-written", Columns: cols})
	assert.Equal(t, StampAgeUnstamped, v.Age)
	assert.Empty(t, v.Notes, "a replicated table deduplicates by its own window")

	assert.Equal(t, SchemaVerdictAbsent, JudgeFacts(c, TableFacts{}).Verdict)
}

func TestTableStampRoundTrip(t *testing.T) {
	s := TableStamp{Kind: "persiststate", Columns: "abc", Naming: "2", Vocab: "def"}
	got, ok := ParseTableStamp(s.String())
	require.True(t, ok)
	assert.Equal(t, s, got)
	_, ok = ParseTableStamp("an operator's note")
	assert.False(t, ok)
	assert.Equal(t, "", VocabDigest(nil))
	assert.Equal(t, VocabDigest(map[string]map[string]uint64{"K": {"a": 1, "b": 2}}), VocabDigest(map[string]map[string]uint64{"K": {"b": 2, "a": 1}}))
}

func TestAppendDDLTail(t *testing.T) {
	stmt := "CREATE TABLE t (a String) ENGINE = MergeTree() ORDER BY a\nSETTINGS x=1" + CommentClause("s")
	assert.Equal(t, "CREATE TABLE t (a String) ENGINE = MergeTree() ORDER BY a\nSETTINGS x=1 , y=2\nCOMMENT 's'", AppendDDLTail(stmt, ", y=2"))
	assert.Equal(t, "CREATE TABLE t (a String) z", AppendDDLTail("CREATE TABLE t (a String)", "z"))
	assert.Equal(t, "x", AppendDDLTail("x", ""))
}

type fakeIndexReader struct{ idx []uint32 }

func (inst *fakeIndexReader) GetColumnIndices() []uint32 { return append([]uint32(nil), inst.idx...) }
func (inst *fakeIndexReader) SetColumnIndices(indices []uint32) []uint32 {
	n := len(inst.idx)
	inst.idx = append([]uint32(nil), indices[:n]...)
	return indices[n:]
}

func TestBindColumnsByName(t *testing.T) {
	field := func(n string) arrow.Field { return arrow.Field{Name: n, Type: arrow.PrimitiveTypes.Uint8} }
	want := arrow.NewSchema([]arrow.Field{field("a"), field("b"), field("c")}, nil)
	got := arrow.NewSchema([]arrow.Field{field("x"), field("c"), field("a"), field("b")}, nil)
	r1 := &fakeIndexReader{idx: []uint32{0}}
	r2 := &fakeIndexReader{idx: []uint32{1, 2}}
	require.NoError(t, BindColumnsByName(want, got, r1, r2))
	assert.Equal(t, []uint32{2}, r1.idx)
	assert.Equal(t, []uint32{3, 1}, r2.idx)

	short := arrow.NewSchema([]arrow.Field{field("a")}, nil)
	err := BindColumnsByName(want, short, &fakeIndexReader{idx: []uint32{0}}, &fakeIndexReader{idx: []uint32{1, 2}})
	require.Error(t, err)
}

func TestSelectList(t *testing.T) {
	s := arrow.NewSchema([]arrow.Field{{Name: "id:id:s:4::0:", Type: arrow.BinaryTypes.String}, {Name: `q"x`, Type: arrow.BinaryTypes.String}}, nil)
	assert.Equal(t, `"id:id:s:4::0:", "q\"x"`, SelectList(s))
}

func TestInsertSettingsDeduplicationToken(t *testing.T) {
	s := InsertSettings{}.WithDeduplicationToken("abc")
	assert.False(t, s.IsZero())
	assert.Equal(t, " SETTINGS insert_deduplication_token='abc'", s.SQL())
	assert.Equal(t, map[string]string{"insert_deduplication_token": "abc"}, s.Map())
}
