package recordstore

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEngineClause(t *testing.T) {
	l, settings, err := ParseEngineClause("MergeTree() PARTITION BY toYYYYMM(`ts:ts:z64:47::0:`) ORDER BY `ts:ts:z64:47::0:`")
	require.NoError(t, err)
	assert.Equal(t, TableLayout{Engine: "MergeTree", PartitionKey: "toYYYYMM(`ts:ts:z64:47::0:`)", SortingKey: "`ts:ts:z64:47::0:`"}, l)
	assert.Empty(t, settings)

	l, settings, err = ParseEngineClause("ENGINE = ReplacingMergeTree(v)\norder by (\"a\", \"b\")\nTTL ts + INTERVAL 1 DAY SETTINGS index_granularity = 8192")
	require.NoError(t, err)
	assert.Equal(t, TableLayout{Engine: "ReplacingMergeTree", SortingKey: "(\"a\", \"b\")"}, l)
	assert.Equal(t, "index_granularity = 8192", settings)

	// A keyword inside a quoted name or a call is not a clause.
	l, _, err = ParseEngineClause("MergeTree ORDER BY (`order by`, f(x, 'TTL'))")
	require.NoError(t, err)
	assert.Equal(t, "(`order by`, f(x, 'TTL'))", l.SortingKey)

	_, _, err = ParseEngineClause("MergeTree() ORDER BY (a")
	require.Error(t, err)
	_, _, err = ParseEngineClause("ORDER BY a")
	require.Error(t, err)
}

func TestNormalizeKeyExpr(t *testing.T) {
	for in, want := range map[string]string{
		"(\"id:id:u64:47::0:\", \"ts:ts:z64:47::0:\")": "id:id:u64:47::0:,ts:ts:z64:47::0:",
		"`id:id:u64:47::0:`, `ts:ts:z64:47::0:`":       "id:id:u64:47::0:,ts:ts:z64:47::0:",
		"toYYYYMM(\"ts\")":                             "toYYYYMM(ts)",
		"(toYYYYMM(ts))":                               "toYYYYMM(ts)",
		"(a) + (b)":                                    "(a)+(b)",
		"tuple()":                                      "",
		"":                                             "",
		"f(x, 'a b')":                                  "f(x,'a b')",
	} {
		assert.Equal(t, want, NormalizeKeyExpr(in), in)
	}
}

// The server's spelling of a table's keys (system.tables, as ClickHouse
// 26.9 writes them) equals the clause it was created with.
func TestTableLayoutEqualsTheServersSpelling(t *testing.T) {
	declared, _, err := ParseEngineClause("MergeTree() PARTITION BY toYYYYMM(`ts:ts:z64:47::0:`) ORDER BY `ts:ts:z64:47::0:`")
	require.NoError(t, err)
	live := TableLayout{Engine: "MergeTree", PartitionKey: "toYYYYMM(`ts:ts:z64:47::0:`)", SortingKey: "`ts:ts:z64:47::0:`"}
	assert.True(t, declared.Equal(live))
	assert.Equal(t, declared.Digest(), live.Digest())

	composed, _, err := ParseEngineClause("MergeTree()\nORDER BY (\"id:id:u64:47::0:\", \"ts:ts:z64:47::0:\")")
	require.NoError(t, err)
	assert.True(t, composed.Equal(TableLayout{Engine: "MergeTree", SortingKey: "`id:id:u64:47::0:`, `ts:ts:z64:47::0:`"}))
	assert.False(t, composed.Equal(declared))
	assert.NotEqual(t, composed.Digest(), declared.Digest())
}

// A table whose layout is not the contract's stays ok and carries a note
// pointing at the recipe; one whose layout matches carries none.
func TestJudgeFactsLayout(t *testing.T) {
	cols := []ColumnSpec{{Name: "id", Type: "UInt64"}, {Name: "ts", Type: "DateTime64(9,'UTC')"}}
	live := []LiveColumn{{Name: "id", Type: "UInt64"}, {Name: "ts", Type: "DateTime64(9, 'UTC')"}}
	c := TableContract{Kind: "t", Table: "db.t", Columns: cols,
		Layout:       TableLayout{Engine: "MergeTree", PartitionKey: "toYYYYMM(\"ts\")", SortingKey: "\"ts\""},
		LayoutRecipe: "doc/migration/x.md"}

	old := JudgeFacts(c, TableFacts{Exists: true, Engine: "MergeTree", SortingKey: "ts", Columns: live})
	assert.Equal(t, SchemaVerdictOk, old.Verdict)
	assert.True(t, old.LayoutDiffers)
	require.NotEmpty(t, old.Notes)
	assert.Contains(t, strings.Join(old.Notes, "\n"), "layout differs (informational)")
	assert.Contains(t, strings.Join(old.Notes, "\n"), "doc/migration/x.md")
	assert.Contains(t, old.Summary(), "layout differs (informational)")
	require.NoError(t, old.Err(), "a differing layout is not an error")

	same := JudgeFacts(c, TableFacts{Exists: true, Engine: "MergeTree", PartitionKey: "toYYYYMM(`ts`)", SortingKey: "ts", Columns: live})
	assert.False(t, same.LayoutDiffers)
	assert.NotContains(t, same.Summary(), "layout")

	c.Layout = TableLayout{}
	undeclared := JudgeFacts(c, TableFacts{Exists: true, Engine: "MergeTree", SortingKey: "ts", Columns: live})
	assert.False(t, undeclared.LayoutDiffers, "a contract without a layout judges none")
}

func TestTableStampLayoutRoundTrip(t *testing.T) {
	s := TableStamp{Kind: "facts", Columns: "c", Naming: LeewayNamingVersion, Layout: "l"}
	got, ok := ParseTableStamp(s.String())
	require.True(t, ok)
	assert.Equal(t, s, got)
	assert.NotContains(t, TableStamp{Kind: "k", Columns: "c", Naming: "2"}.String(), "layout=")
}
