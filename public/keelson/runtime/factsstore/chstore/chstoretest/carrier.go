// Package chstoretest holds a reference deployment for tests of facts kept
// in a deployment's own table (ADR-0402): a carrier table whose columns are
// not the facts columns, the insert transform into it, and the view that
// returns the facts columns again.
//
// It is the smallest storage that differs from boxer.facts in the ways the
// design has to survive: every column under another name, a column the facts
// do not have, rows of other tenants beside the facts, and a key of its own.
// It is test support, not a carrier to deploy.
package chstoretest

import (
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// DefaultEngine is the carrier's engine clause when a Carrier names none:
// keyed by the tenant, with an insert deduplication window, so that a
// re-sent transformed batch is folded as it would be in the facts table.
const DefaultEngine = "MergeTree ORDER BY tenant"

// Carrier is one reference deployment. The zero value is not usable; build
// one with [NewCarrier].
type Carrier struct {
	// Table is the carrier table, database-qualified.
	Table string
	// View is the read relation in the facts columns, database-qualified.
	// A deployment may name it boxer.facts.
	View string
	// Tenant is what the transform writes into the carrier's tenant column
	// and what the view selects; rows of another tenant are not facts.
	Tenant string
	// Engine is the carrier's engine clause, without SETTINGS; empty is
	// [DefaultEngine]. FactsColumn names a facts column's carrier name for a
	// clause that keys or partitions by it.
	Engine string
	// InPlace makes the transform answer in-place statements
	// (recordstore.InPlaceTransformI) by rewriting them for the carrier.
	// Without it the transform refuses them, as one that only maps inserts
	// does.
	InPlace bool

	columns []recordstore.ColumnSpec
	names   map[string]string
}

// NewCarrier describes a carrier for the facts columns given — the Columns
// of chstore.SchemaContract. Each facts column is carried as c<i>, in order.
func NewCarrier(columns []recordstore.ColumnSpec, table string, view string, tenant string) (inst *Carrier) {
	inst = &Carrier{Table: table, View: view, Tenant: tenant, columns: columns, names: make(map[string]string, len(columns))}
	for i, col := range columns {
		inst.names[col.Name] = "c" + strconv.Itoa(i)
	}
	return
}

// FactsColumn is the carrier's name for the facts column named, "" when the
// facts have no such column.
func (inst *Carrier) FactsColumn(name string) string {
	return inst.names[name]
}

// SetupStatements creates the carrier's database and table and the view's
// database, one statement each; idempotent. The view is [Carrier.ViewStatement].
func (inst *Carrier) SetupStatements() (stmts []string) {
	cols := make([]string, 0, len(inst.columns)+1)
	cols = append(cols, "tenant String")
	for _, col := range inst.columns {
		cols = append(cols, inst.names[col.Name]+" "+col.Type)
	}
	engine := inst.Engine
	if engine == "" {
		engine = DefaultEngine
	}
	for _, ref := range []string{inst.Table, inst.View} {
		if db, _, qualified := strings.Cut(ref, "."); qualified {
			stmts = append(stmts, "CREATE DATABASE IF NOT EXISTS "+db)
		}
	}
	stmts = append(stmts, "CREATE TABLE IF NOT EXISTS "+inst.Table+" ("+strings.Join(cols, ", ")+") ENGINE = "+engine+
		" SETTINGS allow_suspicious_low_cardinality_types = 1, "+recordstore.DedupWindowSetting+" = "+strconv.Itoa(recordstore.DedupWindowBlocks))
	return
}

// ViewStatement creates or replaces the view back: the tenant's rows under
// the facts columns' names. The view's SELECT closes with the setting the
// facts table is created under, since it returns the table's LowCardinality
// columns over non-string types and a server refuses to produce those
// without it. A facts column named in skip is left out, for
// a test of a view that does not return every column; one named in retype
// is returned as that type instead of its own.
func (inst *Carrier) ViewStatement(skip []string, retype map[string]string) (stmt string) {
	sel := make([]string, 0, len(inst.columns))
next:
	for _, col := range inst.columns {
		for _, s := range skip {
			if s == col.Name {
				continue next
			}
		}
		expr := inst.names[col.Name]
		if typ, ok := retype[col.Name]; ok {
			expr = "CAST(" + expr + ", " + quoteString(typ) + ")"
		}
		sel = append(sel, expr+" AS "+quoteIdent(col.Name))
	}
	return "CREATE OR REPLACE VIEW " + inst.View + " AS SELECT " + strings.Join(sel, ", ") + " FROM " + inst.Table + " WHERE tenant = " + quoteString(inst.Tenant) +
		" SETTINGS allow_suspicious_low_cardinality_types = 1"
}

// Transform is the insert transform into the carrier: each facts column
// under its carrier name, by reference, with the tenant column in front.
// It is a pure function of its input, as recordstore.InsertTransformI asks.
func (inst *Carrier) Transform() recordstore.InsertTransformI {
	insert := inst.insertTransform()
	if inst.InPlace {
		return inPlaceTransform{InsertTransformFunc: insert, carrier: inst}
	}
	return insert
}

// inPlaceTransform is the carrier's transform with the in-place statements
// answered.
type inPlaceTransform struct {
	recordstore.InsertTransformFunc
	carrier *Carrier
}

// TransformInPlace rewrites a mutation of the facts table — ALTER TABLE …
// UPDATE … WHERE … SETTINGS …, composed over the facts columns — into the
// same mutation of the carrier: the carrier's table and column names, and
// the tenant added to the predicate. It is a rewrite by text, good for a
// carrier that only renames; a deployment whose columns differ in more
// than name writes its statement out.
func (inst inPlaceTransform) TransformInPlace(table string, stmt recordstore.InPlaceStatement) (sql string, err error) {
	c := inst.carrier
	head := "ALTER TABLE " + table + " "
	where, settings := strings.Index(stmt.SQL, " WHERE "), strings.LastIndex(stmt.SQL, " SETTINGS ")
	if !strings.HasPrefix(stmt.SQL, head) || where < 0 || settings < where {
		return "", eb.Build().Str("op", stmt.Op).Errorf("chstoretest: not a mutation of the shape this carrier rewrites: %w", recordstore.ErrTransformedTable)
	}
	sql = "ALTER TABLE " + c.Table + " " + stmt.SQL[len(head):where] +
		" WHERE tenant = " + quoteString(c.Tenant) + " AND (" + stmt.SQL[where+len(" WHERE "):settings] + ")" + stmt.SQL[settings:]
	for _, col := range c.columns {
		sql = strings.ReplaceAll(sql, `"`+col.Name+`"`, c.names[col.Name])
		sql = strings.ReplaceAll(sql, "`"+col.Name+"`", c.names[col.Name])
	}
	return
}

func (inst *Carrier) insertTransform() recordstore.InsertTransformFunc {
	alloc := memory.NewGoAllocator()
	return recordstore.InsertTransformFunc(func(table string, records []arrow.RecordBatch) (target string, out []arrow.RecordBatch, err error) {
		out = make([]arrow.RecordBatch, 0, len(records))
		for _, r := range records {
			fields := make([]arrow.Field, 0, r.NumCols()+1)
			arrs := make([]arrow.Array, 0, r.NumCols()+1)
			fields = append(fields, arrow.Field{Name: "tenant", Type: arrow.BinaryTypes.String})
			b := array.NewStringBuilder(alloc)
			for range int(r.NumRows()) {
				b.Append(inst.Tenant)
			}
			tenant := b.NewArray()
			b.Release()
			arrs = append(arrs, tenant)
			for i, f := range r.Schema().Fields() {
				name, ok := inst.names[f.Name]
				if !ok {
					tenant.Release()
					for _, o := range out {
						o.Release()
					}
					return "", nil, eb.Build().Str("table", table).Str("column", f.Name).Errorf("chstoretest: the carrier has no column for this facts column")
				}
				f.Name = name
				fields = append(fields, f)
				arrs = append(arrs, r.Column(i))
			}
			out = append(out, array.NewRecordBatch(arrow.NewSchema(fields, nil), arrs, r.NumRows()))
			tenant.Release()
		}
		return inst.Table, out, nil
	})
}

func quoteIdent(name string) string {
	return "`" + strings.NewReplacer("\\", "\\\\", "`", "\\`").Replace(name) + "`"
}

func quoteString(s string) string {
	return "'" + strings.NewReplacer("\\", "\\\\", "'", "\\'").Replace(s) + "'"
}
