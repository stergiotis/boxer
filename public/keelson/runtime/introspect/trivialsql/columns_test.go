package trivialsql

import (
	"context"
	"errors"
	"testing"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// Bare columns in a SELECT list (ADR-0290, update of 2026-11-13). The table
// below names, per statement, how the evaluator's answer relates to
// ClickHouse's. This file checks the evaluator's half in the default lane;
// columns_parity_integration_test.go checks both against clickhouse-local.

const (
	t2 = "values('a UInt8, b String', (1, 'x'), (2, 'y'))"
	t3 = "values('a UInt8, b String, c Float64', (1, 'x', 1.5), (2, 'y', 2.5), (3, 'z', 3.5))"
	u2 = "values('a UInt8, b UInt8', (1, 2), (3, 4))"
)

// The classes of a parity case.
const (
	same      = "same"      // both answer, byte for byte
	refused   = "refused"   // ClickHouse answers, the evaluator refuses
	bothError = "bothError" // ClickHouse errors, the evaluator errors without refusing
	refusedCH = "refusedCH" // ClickHouse errors, the evaluator refuses
)

var columnCases = []struct {
	sql  string
	want string
}{
	// Subset, order, twice.
	{"SELECT a, c FROM " + t3, same},
	{"SELECT c, a FROM " + t3, same},
	{"SELECT a AS x, a AS y FROM " + t2, same},
	{"SELECT a, a FROM " + t2, refused},
	{"SELECT a AS x, a AS x FROM " + t2, refused},
	{"SELECT a AS x, b AS x FROM " + t2, refusedCH},
	// An alias that is another column's name.
	{"SELECT a AS b FROM " + t2, same},
	{"SELECT a AS a FROM " + t2, same},
	{"SELECT a AS b, b FROM " + t2, refused},
	{"SELECT b, a AS b FROM " + t2, refused},
	{"SELECT a AS b, b AS a FROM " + t2, refused},
	{"SELECT b AS a, a AS b FROM " + t2, refused},
	{"SELECT a AS b, b AS c, c AS a FROM " + t3, refused},
	{"SELECT a AS x, x FROM " + t2, refused},
	{"SELECT a AS x, x AS y FROM " + t2, refused},
	{"SELECT x AS y, a AS x FROM " + t2, refused},
	{"SELECT a AS a, a AS z FROM " + t2, refused},
	// * beside a column.
	{"SELECT *, a AS a2 FROM " + t2, same},
	{"SELECT a AS a2, * FROM " + t2, same},
	{"SELECT a, * FROM " + t2, refused},
	{"SELECT *, a FROM " + t2, refused},
	{"SELECT *, a AS b FROM " + t2, refusedCH},
	{"SELECT *, a AS b FROM " + u2, refused},
	{"SELECT *, b AS a, a AS z FROM " + u2, refused},
	// A constant beside columns.
	{"SELECT a, 1 AS one FROM " + t2, same},
	{"SELECT 'k' AS k, b, 2.5 AS f, a FROM " + t2, same},
	{"SELECT a, 1 AS a FROM " + t2, refused},
	{"SELECT 1 AS a, a FROM " + t2, refused},
	{"SELECT 1 AS a, b FROM " + t2, same},
	{"SELECT 1 AS a, a AS z FROM " + t2, refused},
	{"SELECT 7 AS k, k AS j, a FROM " + t2, refused},
	{"SELECT true, FALSE AS f, a FROM " + t2, same},
	{"SELECT True, tRuE AS x FROM values('True UInt8', 7)", same},
	{"SELECT `true` FROM values('true UInt8', 7)", same},
	{"SELECT \"true\" AS t FROM values('true UInt8', 7)", same},
	// Quoted names.
	{"SELECT a AS \"p99@gloss/duration;unit=ms\" FROM " + t2, same},
	{"SELECT a AS `p99@gloss/duration;unit=ms` FROM " + t2, same},
	{"SELECT a AS \"p99@gloss/duration;unit=ms\", b AS \"q\"\"q\" FROM " + t2 + " FORMAT JSONEachRow", same},
	{"SELECT a AS \"p99@gloss/duration;unit=ms\", b AS \"x,y\" FROM " + t2 + " FORMAT CSVWithNames", same},
	{"SELECT a AS \"tab\\there\", b AS `back\\`tick`, a AS \"q\"\"q\" FROM " + t2, same},
	{"SELECT a AS \"é\\\\x\" FROM " + t2 + " FORMAT JSONEachRow", same},
	{"SELECT \"a\", `b` FROM " + t2, same},
	{"SELECT a b FROM " + t2, refused},
	{"SELECT (a) FROM " + t2, refused},
	// Qualified.
	{"SELECT t.a FROM " + t2 + " AS t", refused},
	{"SELECT t.a FROM " + t2, refusedCH},
	{"SELECT u.a FROM " + t2 + " AS t", refusedCH},
	{"SELECT a FROM " + t2 + " AS a", same},
	{"SELECT a AS t FROM " + t2 + " AS t", same},
	{"SELECT a.a FROM " + t2 + " AS a", refused},
	{"SELECT t.* FROM " + t2 + " AS t", refused},
	{"WITH t AS (SELECT * FROM " + t2 + ") SELECT t.a FROM t", refused},
	{"WITH t AS (SELECT * FROM " + t2 + ") SELECT t.a FROM t AS u", refused},
	{"WITH 5 AS a SELECT t.a FROM " + t2 + " AS t", refused},
	// WITH queries.
	{"WITH q AS (SELECT a AS x, b FROM " + t2 + "), r AS (SELECT x AS y, b AS c FROM q) SELECT y AS z, c FROM r", same},
	{"WITH q AS (SELECT a AS x, b FROM " + t2 + "), r AS (SELECT x AS y, b AS c FROM q) SELECT * FROM r", same},
	{"WITH q AS (SELECT a AS x, b FROM " + t2 + "), r AS (SELECT x AS y, b AS c FROM q) SELECT a FROM r", bothError},
	{"WITH q AS (SELECT a AS x, b FROM " + t2 + ") SELECT x AS a, b AS x FROM q", refused},
	{"WITH q AS (SELECT * FROM " + t2 + ") SELECT b AS a, a AS z FROM q", refused},
	{"WITH q AS (SELECT * FROM " + t2 + ") SELECT a AS q FROM q", same},
	{"WITH q AS (SELECT * FROM " + t2 + ") SELECT *, a AS z FROM q", same},
	{"WITH q AS (SELECT zz AS y FROM " + t2 + ") SELECT 1 AS zz, y FROM q", refused},
	{"WITH q AS (SELECT zz AS y FROM " + t2 + ") SELECT y FROM q", refusedCH},
	{"WITH q AS (SELECT a AS y FROM " + t2 + ") SELECT 9 AS a, y FROM q", same},
	// A WITH constant named like a column.
	{"WITH 5 AS a SELECT a FROM " + t2, refused},
	{"WITH 5 AS a SELECT a, b FROM " + t2, refused},
	{"WITH 5 AS a SELECT a AS z, b FROM " + t2, refused},
	{"WITH 5 AS a SELECT b FROM " + t2, same},
	{"WITH 5 AS a SELECT * FROM " + t2, same},
	{"WITH 5 AS a SELECT *, a FROM " + t2, refused},
	{"WITH 5 AS k SELECT k, a FROM " + t2, same},
	{"WITH 5 AS k SELECT a AS k FROM " + t2, refusedCH},
	{"WITH 5 AS k SELECT a AS k, k FROM " + t2, refusedCH},
	{"WITH 5 AS k SELECT 6 AS k", refusedCH},
	{"WITH 5 AS k SELECT k AS k", refusedCH},
	{"WITH 5 AS k SELECT 5 AS k", refused},
	{"WITH 5 AS a, q AS (SELECT * FROM " + t2 + ") SELECT a FROM q", refused},
	{"WITH 5 AS a, q AS (SELECT * FROM " + t2 + ") SELECT b FROM q", refused},
	{"WITH 5 AS a, q AS (SELECT a, b FROM " + t2 + ") SELECT b FROM q", refused},
	{"WITH 5 AS a, q AS (SELECT a AS z, b FROM " + t2 + ") SELECT z, b FROM q", refused},
	{"WITH 5 AS k, q AS (SELECT k AS z, a FROM " + t2 + ") SELECT * FROM q", same},
	{"WITH 5 AS k, q AS (SELECT a FROM " + t2 + ") SELECT a, k FROM q", same},
	{"WITH 5 AS k, q AS (SELECT a AS k FROM " + t2 + ") SELECT * FROM q", refused},
	{"WITH 5 AS k, q AS (SELECT a AS k FROM " + t2 + ") SELECT k FROM q", refused},
	// LIMIT and OFFSET.
	{"SELECT c, a FROM " + t3 + " LIMIT 1 OFFSET 1", same},
	{"SELECT b AS q, a FROM " + t3 + " LIMIT 1, 2", same},
	{"SELECT a AS x FROM " + t3 + " LIMIT 0", same},
	{"SELECT a AS x FROM " + t3 + " LIMIT 5 OFFSET 2", same},
	// A column that is not there, and the case of a name.
	{"SELECT nope FROM " + t2, bothError},
	{"SELECT a, nope AS n FROM " + t2, bothError},
	{"SELECT A FROM " + t2, bothError},
	{"SELECT _table FROM " + t2, bothError},
	{"SELECT a, A FROM values('a UInt8, A String', (1, 'x'), (2, 'y'))", same},
	{"SELECT a AS A, A AS a FROM values('a UInt8, A String', (1, 'x'), (2, 'y'))", refused},
	{"SELECT a", refusedCH},
	{"SELECT a AS b", refusedCH},
	// Other sources and types.
	{"SELECT c2 AS two, c1 FROM values((1, 'x'), (2, 'y'))", same},
	{"SELECT a AS n FROM values('a Nullable(UInt8)', NULL, 3)", same},
	{"SELECT a AS n FROM values('a Nullable(UInt8)', NULL, 3) FORMAT JSONEachRow", same},
	// Still refused.
	{"SELECT DISTINCT a FROM " + t2, refused},
	{"SELECT a AS x FROM " + t2 + " ORDER BY x", refused},
	{"SELECT a AS x FROM " + t2 + " WHERE x = 1", refused},
	{"SELECT a + 1 AS x FROM " + t2, refused},
	{"SELECT toString(a) AS x FROM " + t2, refused},
}

// The evaluator's half of a class: it answers, refuses, or errors without
// refusing.
func TestColumns_EvaluatorSide(t *testing.T) {
	for _, c := range columnCases {
		_, err := Run(context.Background(), reg(t), c.sql, nil)
		isRefusal := errors.Is(err, ErrNeedsClickHouse)
		switch c.want {
		case same:
			assert.NoError(t, err, c.sql)
		case refused, refusedCH:
			assert.True(t, isRefusal, "%s: want a refusal, got %v", c.sql, err)
		case bothError:
			assert.True(t, err != nil && !isRefusal, "%s: want an error that is not a refusal, got %v", c.sql, err)
		}
	}
}

// heldProvider hands out one record it keeps, so a test can compare the
// buffers of what a statement returns with the buffers it was given.
type heldProvider struct{ batch arrow.RecordBatch }

func (heldProvider) Name() string                         { return "held" }
func (heldProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessStatic }
func (inst heldProvider) Schema() *arrow.Schema           { return inst.batch.Schema() }
func (inst heldProvider) Snapshot(context.Context, introspect.Projection) (arrow.RecordBatch, error) {
	inst.batch.Retain()
	return inst.batch, nil
}

func bufferAddrs(a arrow.Array) (addrs []uintptr) {
	for _, b := range a.Data().Buffers() {
		if b == nil || b.Len() == 0 {
			addrs = append(addrs, 0)
			continue
		}
		addrs = append(addrs, uintptr(unsafe.Pointer(unsafe.SliceData(b.Bytes()))))
	}
	return
}

// A selected or renamed column is the source's array: the same buffers, no
// copy, also through a WITH query and under LIMIT.
func TestColumns_ShareTheSourceBuffers(t *testing.T) {
	src := seqTable().Build(introspect.AllColumns(), 1000)
	defer src.Release()
	r := introspect.NewRegistry()
	require.NoError(t, r.Register(heldProvider{batch: src}))
	for _, sql := range []string{
		"SELECT w AS word, v FROM keelson('held')",
		"SELECT w AS word, v, 1 AS one FROM keelson('held') LIMIT 10 OFFSET 5",
		"WITH q AS (SELECT w AS x, v FROM keelson('held')), r AS (SELECT x AS y, v AS n FROM q) SELECT y AS word, n AS v FROM r",
	} {
		out, _, err := Read(t.Context(), r, sql, nil)
		require.NoError(t, err, sql)
		assert.Equal(t, "word", out.Schema().Field(0).Name, sql)
		assert.Equal(t, "v", out.Schema().Field(1).Name, sql)
		assert.Equal(t, bufferAddrs(src.Column(1)), bufferAddrs(out.Column(0)), sql)
		assert.Equal(t, bufferAddrs(src.Column(0)), bufferAddrs(out.Column(1)), sql)
		out.Release()
	}
	// Without LIMIT the very array is handed on.
	out, _, err := Read(t.Context(), r, "SELECT w AS word FROM keelson('held')", nil)
	require.NoError(t, err)
	defer out.Release()
	assert.Same(t, src.Column(1).Data(), out.Column(0).Data())
}

// tableCases are statements over one bound table `t` of the rows of t3.
// Each is also a statement ClickHouse can be asked, with t before it as a
// WITH query (boundAsWith); want is the class of the pair.
var tableCases = []struct {
	sql  string
	want string
}{
	{"SELECT * FROM t", same},
	{"SELECT c, a AS x FROM t LIMIT 2", same},
	{"SELECT a AS \"p99@gloss/duration;unit=ms\" FROM t", same},
	{"WITH edges AS (SELECT a AS source, b AS target FROM t), opts AS (SELECT 'hierarchical' AS layout) SELECT * FROM edges", same},
	{"WITH m AS (SELECT a FROM t), n AS (SELECT a AS z FROM m) SELECT z FROM n", same},
	{"SELECT 'k' AS k, 2 AS n", same},
	{"SELECT a FROM t WHERE a = 1", refused},
	{"SELECT a FROM t ORDER BY a", refused},
	{"SELECT a + 1 AS x FROM t", refused},
	{"SELECT count() FROM t", refused},
	{"SELECT * FROM t AS l JOIN t AS r ON l.a = r.a", refused},
	{"SELECT * FROM nope", bothError},
	{"WITH m AS (SELECT * FROM n), n AS (SELECT a FROM t) SELECT * FROM m", refused},
	{"WITH m AS (SELECT * FROM m) SELECT * FROM m", refusedCH},
	{"WITH m AS (SELECT * FROM nope) SELECT * FROM m", refusedCH},
	{"WITH m AS (SELECT * FROM nope) SELECT 1 AS one", refused},
	{"WITH m AS (SELECT * FROM nope) SELECT * FROM t", refused},
	{"WITH t AS (SELECT 1 AS a) SELECT * FROM t", bothError},
	{"SELECT nope FROM t", bothError},
}

func boundTables(t *testing.T) map[string]arrow.RecordBatch {
	t.Helper()
	batch, _, err := Read(t.Context(), reg(t), "SELECT * FROM "+t3, nil)
	require.NoError(t, err)
	t.Cleanup(batch.Release)
	return map[string]arrow.RecordBatch{"t": batch}
}

func TestReadTables_EvaluatorSide(t *testing.T) {
	tables := boundTables(t)
	for _, c := range tableCases {
		ans, err := ReadTables(t.Context(), reg(t), c.sql, nil, tables)
		isRefusal := errors.Is(err, ErrNeedsClickHouse)
		switch c.want {
		case same:
			if assert.NoError(t, err, c.sql) {
				ans.Release()
			}
		case refused, refusedCH:
			assert.True(t, isRefusal, "%s: want a refusal, got %v", c.sql, err)
		case bothError:
			assert.True(t, err != nil && !isRefusal, "%s: want an error that is not a refusal, got %v", c.sql, err)
		}
	}
}

// The answer holds the statement's own relation, every WITH query in order
// and the bound tables it read; a column of it is the bound table's array.
func TestReadTables_ReturnsTheWithQueries(t *testing.T) {
	tables := boundTables(t)
	ans, err := ReadTables(t.Context(), reg(t), "WITH edges AS (SELECT a AS source, b AS target FROM t), opts AS (SELECT 'hierarchical' AS layout) SELECT * FROM edges LIMIT 2", nil, tables)
	require.NoError(t, err)
	defer ans.Release()
	assert.EqualValues(t, 2, ans.Main.NumRows())
	require.Len(t, ans.With, 2)
	assert.Equal(t, "edges", ans.With[0].Name)
	assert.EqualValues(t, 3, ans.With[0].Batch.NumRows(), "a WITH query is read whole, whatever the statement's LIMIT")
	assert.Equal(t, "source", ans.With[0].Batch.Schema().Field(0).Name)
	assert.Same(t, tables["t"].Column(0).Data(), ans.With[0].Batch.Column(0).Data())
	assert.Equal(t, "opts", ans.With[1].Name, "a WITH query nothing names is read too")
	assert.EqualValues(t, 1, ans.With[1].Batch.NumRows())
	assert.Equal(t, []string{"t"}, ans.Tables)

	lit, err := ReadTables(t.Context(), reg(t), "SELECT 1 AS one", nil, tables)
	require.NoError(t, err)
	defer lit.Release()
	assert.Empty(t, lit.Tables)
}

// Read binds no table, and a bare table name stays a refusal there.
func TestRead_ABareTableIsStillRefused(t *testing.T) {
	_, _, err := Read(t.Context(), reg(t), "SELECT * FROM t", nil)
	assert.ErrorIs(t, err, ErrNeedsClickHouse)
}
