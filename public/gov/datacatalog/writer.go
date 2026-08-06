package datacatalog

import (
	"context"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// ExecI is the one method the writer needs from a ClickHouse client;
// [github.com/stergiotis/boxer/public/keelson/data/chclient.Client] satisfies
// it. Keeping the surface this narrow is what makes the writer testable with a
// capture and no server anywhere near it.
type ExecI interface {
	Exec(ctx context.Context, sql string) (err error)
}

// insertBatchRows bounds one INSERT's VALUES list. The pair matrix is O(n²) and
// reaches a few thousand rows at a hundred tables; chunking keeps any single
// statement a readable size rather than one multi-megabyte line.
const insertBatchRows = 500

// ApplyDDL runs [TargetDatabase.DDL] in order. It is destructive by design: each statement is
// a CREATE OR REPLACE, so a run replaces the previous run's catalog whole.
func ApplyDDL(ctx context.Context, exec ExecI, target TargetDatabase) (err error) {
	for _, stmt := range target.DDL() {
		err = exec.Exec(ctx, stmt)
		if err != nil {
			err = eh.Errorf("unable to apply ddl statement: %w", err)
			return
		}
	}
	return
}

// Insert writes every row of res into the four catalog tables. It assumes
// [ApplyDDL] has just run — the tables are empty, so this appends rather than
// replaces, and re-running Insert without the DDL would double every row.
//
// Rows go over as SQL literals rather than Arrow batches: at these volumes the
// difference is not measurable, and a literal INSERT keeps the Enum8 columns
// unambiguous (a string literal is exactly what ClickHouse wants for an enum)
// without an Arrow-to-enum mapping to get wrong.
func Insert(ctx context.Context, exec ExecI, target TargetDatabase, res Result) (err error) {
	stamp := stampLiterals(res)

	err = insertRows(ctx, exec, target.Qualified(TableCatalog),
		"database, name, engine, kind, n_columns, normalized_schema, classify_detail, run_id, discovered_at",
		len(res.Catalog), func(i int) string {
			r := res.Catalog[i]
			return tuple(
				marshalling.EscapeString(r.Ref.Database),
				marshalling.EscapeString(r.Ref.Name),
				marshalling.EscapeString(r.Engine),
				marshalling.EscapeString(r.Kind.String()),
				strconv.FormatUint(uint64(r.NColumns), 10),
				marshalling.EscapeString(r.NormalizedSchema),
				marshalling.EscapeString(r.ClassifyDetail),
				stamp.runId, stamp.at)
		})
	if err != nil {
		return
	}

	err = insertRows(ctx, exec, target.Qualified(TableLeeway),
		"database, name, table_row_config, schema_hash, n_attrs, attr_keys, desc_json, run_id, discovered_at",
		len(res.Leeway), func(i int) string {
			r := res.Leeway[i]
			return tuple(
				marshalling.EscapeString(r.Ref.Database),
				marshalling.EscapeString(r.Ref.Name),
				marshalling.EscapeString(r.TableRowConfig),
				strconv.FormatUint(r.SchemaHash, 10),
				strconv.FormatUint(uint64(r.NAttrs), 10),
				stringArrayLiteral(r.AttrKeys),
				marshalling.EscapeString(r.DescJson),
				stamp.runId, stamp.at)
		})
	if err != nil {
		return
	}

	err = insertRows(ctx, exec, target.Qualified(TableCompatibility),
		"database_a, name_a, database_b, name_b, relation, shape_id, n_common, jaccard, run_id, discovered_at",
		len(res.Pairs), func(i int) string {
			r := res.Pairs[i]
			return tuple(
				marshalling.EscapeString(r.A.Database),
				marshalling.EscapeString(r.A.Name),
				marshalling.EscapeString(r.B.Database),
				marshalling.EscapeString(r.B.Name),
				marshalling.EscapeString(r.Relation.String()),
				strconv.FormatUint(r.ShapeId, 10),
				strconv.FormatUint(uint64(r.NCommon), 10),
				strconv.FormatFloat(float64(r.Jaccard), 'g', -1, 32),
				stamp.runId, stamp.at)
		})
	if err != nil {
		return
	}

	return insertRows(ctx, exec, target.Qualified(TableOpaqueShapes),
		"database, name, shape, run_id, discovered_at",
		len(res.Shapes), func(i int) string {
			r := res.Shapes[i]
			return tuple(
				marshalling.EscapeString(r.Ref.Database),
				marshalling.EscapeString(r.Ref.Name),
				marshalling.EscapeString(r.Shape),
				stamp.runId, stamp.at)
		})
}

// stamps are the two literals every row of every table repeats. Rendering them
// once per run rather than per row is the only reason this type exists.
type stamps struct {
	runId string
	at    string
}

func stampLiterals(res Result) (s stamps) {
	// toDateTime over the Unix second, not a quoted datetime string: the
	// server's timezone would otherwise reinterpret the text, and a catalog
	// whose discovered_at drifts by an offset is worse than one with no
	// timezone at all.
	return stamps{
		runId: marshalling.EscapeString(res.RunId),
		at:    "toDateTime(" + strconv.FormatInt(res.DiscoveredAt.Unix(), 10) + ")",
	}
}

// insertRows renders n rows through row(i) and ships them in batches of
// [insertBatchRows]. Zero rows is a no-op rather than an empty INSERT, which
// ClickHouse rejects.
func insertRows(ctx context.Context, exec ExecI, table string, columns string, n int, row func(i int) string) (err error) {
	if n == 0 {
		return
	}
	var b strings.Builder
	for start := 0; start < n; start += insertBatchRows {
		end := min(start+insertBatchRows, n)
		b.Reset()
		b.WriteString("INSERT INTO ")
		b.WriteString(table)
		b.WriteString(" (")
		b.WriteString(columns)
		b.WriteString(") VALUES ")
		for i := start; i < end; i++ {
			if i > start {
				b.WriteByte(',')
			}
			b.WriteString(row(i))
		}
		err = exec.Exec(ctx, b.String())
		if err != nil {
			err = eh.Errorf("unable to insert rows: %w", err)
			return
		}
	}
	return
}

// tuple joins already-rendered literals into one VALUES row.
func tuple(literals ...string) (s string) {
	return "(" + strings.Join(literals, ",") + ")"
}

// stringArrayLiteral renders an Array(String) literal. An empty slice is `[]`,
// which ClickHouse reads as an empty array of the column's element type.
func stringArrayLiteral(values []string) (s string) {
	var b strings.Builder
	b.Grow(2 + 24*len(values))
	b.WriteByte('[')
	for i, v := range values {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(marshalling.EscapeString(v))
	}
	b.WriteByte(']')
	return b.String()
}
