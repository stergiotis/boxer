package datacatalog_test

import (
	"context"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

// fixedStamp keeps a run reproducible: Analyze takes the run id and the clock
// from its caller precisely so a test can pin both.
var fixedStamp = time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

// captureExec records the SQL a writer would have sent. The writer's whole
// server surface is one method, which is what makes this possible.
type captureExec struct {
	stmts []string
	err   error
}

func (inst *captureExec) Exec(_ context.Context, sql string) (err error) {
	inst.stmts = append(inst.stmts, sql)
	return inst.err
}

// fakeFetcher hands back a literal slice of snapshots.
type fakeFetcher struct {
	tables []datacatalog.TableSnapshot
	err    error
}

func (inst fakeFetcher) FetchTables(_ context.Context) (tables []datacatalog.TableSnapshot, err error) {
	return inst.tables, inst.err
}

// snapshotOf renders a fixture table's physical columns as a snapshot, so the
// analysis sees what a real system.columns probe would report.
func snapshotOf(t *testing.T, db string, name string, tbl *common.TableDesc, sep string) (snap datacatalog.TableSnapshot) {
	t.Helper()
	names := physicalNames(t, tbl, sep)
	cols := make([]datacatalog.ColumnMeta, 0, len(names))
	for i, n := range names {
		cols = append(cols, datacatalog.ColumnMeta{Name: n, Type: "String", Position: uint64(i + 1)})
	}
	return datacatalog.TableSnapshot{
		Ref:     datacatalog.TableRef{Database: db, Name: name},
		Engine:  "MergeTree",
		Columns: cols,
	}
}

func opaqueSnapshot(db string, name string, cols ...datacatalog.ColumnMeta) (snap datacatalog.TableSnapshot) {
	for i := range cols {
		cols[i].Position = uint64(i + 1)
	}
	return datacatalog.TableSnapshot{
		Ref:     datacatalog.TableRef{Database: db, Name: name},
		Engine:  "MergeTree",
		Columns: cols,
	}
}

// The whole engine on one small instance: two related leeway tables and an
// opaque series-shaped one. This is the unit-lane twin of the integration
// test, and asserts the same four claims without a server.
func TestAnalyze_EndToEnd(t *testing.T) {
	small := buildOneSectionTable(t, "metric", "value")
	large := buildTwoSectionTable(t)
	tables := []datacatalog.TableSnapshot{
		opaqueSnapshot("app", "readings",
			datacatalog.ColumnMeta{Name: "ts", Type: "DateTime64(3)"},
			datacatalog.ColumnMeta{Name: "reading", Type: "Float64"}),
		snapshotOf(t, "app", "lw_large", &large, ":"),
		snapshotOf(t, "app", "lw_small", &small, "_"),
	}

	res, err := datacatalog.Analyze(tables, "run-1", fixedStamp, zerolog.Nop())
	require.NoError(t, err)
	assert.Equal(t, "run-1", res.RunId)
	assert.Equal(t, fixedStamp, res.DiscoveredAt)

	// Inventory: every table, in (database, name) order, both kinds.
	require.Len(t, res.Catalog, 3)
	assert.Equal(t, "lw_large", res.Catalog[0].Ref.Name)
	assert.Equal(t, "lw_small", res.Catalog[1].Ref.Name)
	assert.Equal(t, "readings", res.Catalog[2].Ref.Name)
	assert.Equal(t, datacatalog.KindLeeway, res.Catalog[0].Kind)
	assert.Equal(t, datacatalog.KindLeeway, res.Catalog[1].Kind)
	assert.Equal(t, datacatalog.KindOpaque, res.Catalog[2].Kind)
	// classify_detail means "why this table has no tables_leeway row".
	assert.Empty(t, res.Catalog[0].ClassifyDetail)
	assert.NotEmpty(t, res.Catalog[2].ClassifyDetail)
	// The normalized schema is emitted for both kinds.
	assert.Equal(t, ";ts:DateTime64(3);reading:Float64;", res.Catalog[2].NormalizedSchema)

	// Restoration payload for the leeway tables only.
	require.Len(t, res.Leeway, 2)
	for _, r := range res.Leeway {
		assert.NotZero(t, r.SchemaHash)
		assert.EqualValues(t, len(r.AttrKeys), r.NAttrs)
		assert.NotEmpty(t, r.DescJson)
		// The DTO's own json tags decide the spelling; the catalog does not
		// impose one.
		assert.Contains(t, r.DescJson, `"TaggedValuesSections"`)
	}

	// One pair, and the containment the fixtures were built to produce.
	require.Len(t, res.Pairs, 1)
	assert.Equal(t, common.TableRelationSuperset, res.Pairs[0].Relation)

	// The opaque table satisfies the series shape and nothing that demands
	// named columns it lacks.
	require.Len(t, res.Shapes, 1)
	assert.Equal(t, "series", res.Shapes[0].Shape)
	assert.Equal(t, "readings", res.Shapes[0].Ref.Name)
}

// Shapes are matched against opaque tables only: a leeway table's physical
// names would satisfy patterns for reasons unrelated to what its columns mean.
func TestAnalyze_ShapesSkipLeewayTables(t *testing.T) {
	tbl := buildOneSectionTable(t, "metric", "value")
	res, err := datacatalog.Analyze([]datacatalog.TableSnapshot{
		snapshotOf(t, "app", "lw", &tbl, ":"),
	}, "run", fixedStamp, zerolog.Nop())
	require.NoError(t, err)
	assert.Empty(t, res.Shapes)
}

// A table system.tables reports but system.columns has nothing for is a visible
// opaque row, not an omission.
func TestAnalyze_ZeroColumnTableIsAVisibleRow(t *testing.T) {
	res, err := datacatalog.Analyze([]datacatalog.TableSnapshot{
		{Ref: datacatalog.TableRef{Database: "app", Name: "empty"}, Engine: "View"},
	}, "run", fixedStamp, zerolog.Nop())
	require.NoError(t, err)
	require.Len(t, res.Catalog, 1)
	assert.Equal(t, datacatalog.KindOpaque, res.Catalog[0].Kind)
	assert.EqualValues(t, 0, res.Catalog[0].NColumns)
	assert.Equal(t, ";", res.Catalog[0].NormalizedSchema)
	assert.NotEmpty(t, res.Catalog[0].ClassifyDetail)
	assert.Empty(t, res.Leeway)
}

func TestAnalyze_NoTables(t *testing.T) {
	res, err := datacatalog.Analyze(nil, "run", fixedStamp, zerolog.Nop())
	require.NoError(t, err)
	assert.Empty(t, res.Catalog)
	assert.Empty(t, res.Leeway)
	assert.Empty(t, res.Pairs)
	assert.Empty(t, res.Shapes)
}

func TestApplyDDL(t *testing.T) {
	exec := &captureExec{}
	require.NoError(t, datacatalog.ApplyDDL(context.Background(), exec, ""))
	require.Len(t, exec.stmts, len(datacatalog.DDL()))
	assert.Contains(t, exec.stmts[0], "CREATE DATABASE IF NOT EXISTS boxer")
	for _, table := range datacatalog.AllTables {
		assert.Truef(t, strings.Contains(strings.Join(exec.stmts, "\n"), datacatalog.Qualified(table)),
			"no DDL statement for %s", table)
	}
}

func TestInsert_WritesEveryTable(t *testing.T) {
	res := datacatalog.Result{
		RunId:        "run-1",
		DiscoveredAt: fixedStamp,
		Catalog: []datacatalog.CatalogRow{{
			Ref: datacatalog.TableRef{Database: "app", Name: "t"}, Engine: "MergeTree",
			Kind: datacatalog.KindOpaque, NColumns: 2, NormalizedSchema: ";a:String;b:UInt64;",
			ClassifyDetail: "nope",
		}},
		Leeway: []datacatalog.LeewayRow{{
			Ref: datacatalog.TableRef{Database: "app", Name: "lw"}, TableRowConfig: "multi-attributes-per-row",
			SchemaHash: 42, NAttrs: 2, AttrKeys: []string{"plain/entity-id/id:bh", "tagged/metric/value:u64"},
			DescJson: `{"a":1}`,
		}},
		Pairs: []datacatalog.Pair{{
			A:        datacatalog.TableRef{Database: "app", Name: "a"},
			B:        datacatalog.TableRef{Database: "app", Name: "b"},
			Relation: common.TableRelationSubset, ShapeId: 7, NCommon: 3, Jaccard: 0.5,
		}},
		Shapes: []datacatalog.ShapeRow{{
			Ref: datacatalog.TableRef{Database: "app", Name: "t"}, Shape: "series",
		}},
	}
	exec := &captureExec{}
	require.NoError(t, datacatalog.Insert(context.Background(), exec, "", res))
	require.Len(t, exec.stmts, 4)
	all := strings.Join(exec.stmts, "\n")

	assert.Contains(t, exec.stmts[0], "INSERT INTO "+datacatalog.Qualified(datacatalog.TableCatalog))
	assert.Contains(t, exec.stmts[1], "INSERT INTO "+datacatalog.Qualified(datacatalog.TableLeeway))
	assert.Contains(t, exec.stmts[2], "INSERT INTO "+datacatalog.Qualified(datacatalog.TableCompatibility))
	assert.Contains(t, exec.stmts[3], "INSERT INTO "+datacatalog.Qualified(datacatalog.TableOpaqueShapes))

	// The enum columns go over as their string spellings, which is what
	// ClickHouse wants for an Enum8.
	assert.Contains(t, exec.stmts[0], "'opaque'")
	assert.Contains(t, exec.stmts[2], "'subset'")
	// Arrays render as a bracketed list of quoted elements.
	assert.Contains(t, exec.stmts[1], "['plain/entity-id/id:bh','tagged/metric/value:u64']")
	// The stamp is a Unix second, not a text datetime the server's timezone
	// could reinterpret.
	assert.Contains(t, all, "toDateTime("+strconv.FormatInt(fixedStamp.Unix(), 10)+")")
	assert.Contains(t, all, "'run-1'")
}

// An empty table gets no statement at all: ClickHouse rejects an INSERT with an
// empty VALUES list.
func TestInsert_SkipsEmptyTables(t *testing.T) {
	exec := &captureExec{}
	require.NoError(t, datacatalog.Insert(context.Background(), exec, "", datacatalog.Result{RunId: "r", DiscoveredAt: fixedStamp}))
	assert.Empty(t, exec.stmts)
}

// Quotes and backslashes in a column name must not escape the literal — the
// normalized schema string is user-supplied text as far as the writer knows.
func TestInsert_EscapesLiterals(t *testing.T) {
	exec := &captureExec{}
	err := datacatalog.Insert(context.Background(), exec, "", datacatalog.Result{
		RunId: "r", DiscoveredAt: fixedStamp,
		Catalog: []datacatalog.CatalogRow{{
			Ref:              datacatalog.TableRef{Database: "app", Name: `it's`},
			NormalizedSchema: `;back\slash:String;`,
		}},
	})
	require.NoError(t, err)
	require.Len(t, exec.stmts, 1)
	assert.Contains(t, exec.stmts[0], `'it\'s'`)
	assert.Contains(t, exec.stmts[0], `';back\\slash:String;'`)
}

// More rows than one batch holds still land, split across statements.
func TestInsert_Batches(t *testing.T) {
	rows := make([]datacatalog.ShapeRow, 0, 1200)
	for i := range 1200 {
		rows = append(rows, datacatalog.ShapeRow{
			Ref:   datacatalog.TableRef{Database: "app", Name: "t" + strconv.Itoa(i)},
			Shape: "series",
		})
	}
	exec := &captureExec{}
	require.NoError(t, datacatalog.Insert(context.Background(), exec, "", datacatalog.Result{
		RunId: "r", DiscoveredAt: fixedStamp, Shapes: rows,
	}))
	assert.Len(t, exec.stmts, 3)
	joined := strings.Join(exec.stmts, "")
	assert.Equal(t, 1200, strings.Count(joined, "'series'"))
}

// fakeQuery answers each query with a canned JSONEachRow body, in call order.
type fakeQuery struct {
	bodies []string
	sqls   []string
	err    error
}

func (inst *fakeQuery) Query(_ context.Context, sql string) (body io.ReadCloser, err error) {
	inst.sqls = append(inst.sqls, sql)
	if inst.err != nil {
		return nil, inst.err
	}
	b := inst.bodies[len(inst.sqls)-1]
	return io.NopCloser(strings.NewReader(b)), nil
}

func TestChFetcher_JoinsTablesAndColumns(t *testing.T) {
	q := &fakeQuery{bodies: []string{
		`{"database":"app","name":"t","engine":"MergeTree"}
{"database":"app","name":"u","engine":"View"}
`,
		`{"database":"app","table":"t","name":"a","type":"String","position":1}
{"database":"app","table":"t","name":"b","type":"UInt64","position":2}
{"database":"gone","table":"x","name":"c","type":"String","position":1}
`,
	}}
	tables, err := datacatalog.NewChFetcher(q).FetchTables(context.Background())
	require.NoError(t, err)
	require.Len(t, tables, 2)
	assert.Equal(t, "MergeTree", tables[0].Engine)
	require.Len(t, tables[0].Columns, 2)
	assert.Equal(t, "a", tables[0].Columns[0].Name)
	assert.Equal(t, "UInt64", tables[0].Columns[1].Type)
	assert.EqualValues(t, 2, tables[0].Columns[1].Position)
	// A table with no column rows survives with none; a column whose table is
	// absent is dropped rather than inventing an inventory row.
	assert.Empty(t, tables[1].Columns)

	// Both probes skip the system databases and read columns in position order.
	require.Len(t, q.sqls, 2)
	for _, sql := range q.sqls {
		assert.Contains(t, sql, "database NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema')")
		assert.Contains(t, sql, "FORMAT JSONEachRow")
	}
	assert.Contains(t, q.sqls[1], "ORDER BY database, table, position")
}

func TestChFetcher_QueryFailurePropagates(t *testing.T) {
	q := &fakeQuery{err: io.ErrUnexpectedEOF}
	_, err := datacatalog.NewChFetcher(q).FetchTables(context.Background())
	assert.Error(t, err)
}

func TestRun_DryRunWritesNothing(t *testing.T) {
	exec := &captureExec{}
	res, err := datacatalog.Run(context.Background(), fakeFetcher{tables: []datacatalog.TableSnapshot{
		opaqueSnapshot("app", "t", datacatalog.ColumnMeta{Name: "source", Type: "String"},
			datacatalog.ColumnMeta{Name: "target", Type: "String"}),
	}}, exec, "", true, zerolog.Nop())
	require.NoError(t, err)
	assert.Empty(t, exec.stmts)
	assert.Len(t, res.Catalog, 1)
	assert.Len(t, res.Shapes, 1)
	assert.NotEmpty(t, res.RunId)
}

func TestRun_AppliesDdlThenInserts(t *testing.T) {
	exec := &captureExec{}
	_, err := datacatalog.Run(context.Background(), fakeFetcher{tables: []datacatalog.TableSnapshot{
		opaqueSnapshot("app", "t", datacatalog.ColumnMeta{Name: "x", Type: "String"}),
	}}, exec, "", false, zerolog.Nop())
	require.NoError(t, err)
	require.Greater(t, len(exec.stmts), len(datacatalog.DDL()))
	assert.Contains(t, exec.stmts[0], "CREATE DATABASE")
	assert.Contains(t, exec.stmts[len(datacatalog.DDL())], "INSERT INTO")
}
