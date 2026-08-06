// Package datacatalog answers, as data, what a live ClickHouse instance holds:
// which tables are leeway-mapped, what leeway schema each one carries, which
// tables contain one another, and which of the remaining opaque tables a play
// panel could render (ADR-0170).
//
// It owns no grammar of its own. A table is leeway iff the leeway naming
// convention can rebuild a [common.TableDesc] from its physical column names
// ([Classify]); two leeway tables are compared by
// [common.TableOperations.Relate]; an opaque table is described by the
// normalized schema string of [NormalizedSchema] and matched against the
// pattern batteries in
// [github.com/stergiotis/boxer/public/gov/datacatalog/panelshapes]. Everything
// here is derived from the physical schema and rebuildable from it, which is
// why the catalog tables are replaced whole per run rather than appended to.
//
// The package is split so that nothing but the writer needs a server: the
// discovery input is a [FetcherI], which the CLI satisfies against a live
// endpoint and a test satisfies with a literal slice.
package datacatalog

import (
	"context"
	"slices"
	"strings"
)

// SystemDatabases are the databases a catalog run skips: ClickHouse's own
// metadata, which is not a subject of the catalog and would swamp it. The
// INFORMATION_SCHEMA spelling exists twice on the server, so both are listed.
var SystemDatabases = []string{"system", "INFORMATION_SCHEMA", "information_schema"}

// IsSystemDatabase reports whether db is one of [SystemDatabases].
func IsSystemDatabase(db string) (is bool) {
	return slices.Contains(SystemDatabases, db)
}

// TableRef is a table's ClickHouse coordinate. It is the catalog's row key
// everywhere and, ordered lexicographically by (database, name), the canonical
// (a) < (b) ordering of the pair matrix.
type TableRef struct {
	Database string
	Name     string
}

// String renders the qualified name, `database.name`. It is what the book
// chapters display and what a Sankey node is keyed by; it is not quoted, so it
// is a label rather than something to splice into SQL.
func (inst TableRef) String() (s string) {
	return inst.Database + "." + inst.Name
}

// Compare orders two refs by database then name, the ordering the pair matrix
// and the catalog tables' ORDER BY both use.
func (inst TableRef) Compare(other TableRef) (r int) {
	r = strings.Compare(inst.Database, other.Database)
	if r != 0 {
		return
	}
	return strings.Compare(inst.Name, other.Name)
}

// ColumnMeta is one physical column as system.columns reports it. Type is the
// server's spelling, verbatim — [NormalizedSchema] does the normalizing, so
// the raw form stays available to a caller that wants it.
type ColumnMeta struct {
	Name     string
	Type     string
	Position uint64
}

// TableSnapshot is one discovered table with its columns in
// system.columns.position order. Position order matters: the leeway naming
// grammar is order-insensitive, but the normalized schema string of an opaque
// table is not, and the shape batteries are written against it.
type TableSnapshot struct {
	Ref     TableRef
	Engine  string
	Columns []ColumnMeta
}

// ColumnNames returns the column names in position order, the input
// [Classify] takes.
func (inst TableSnapshot) ColumnNames() (names []string) {
	names = make([]string, 0, len(inst.Columns))
	for _, c := range inst.Columns {
		names = append(names, c.Name)
	}
	return
}

// FetcherI is where a catalog run gets its input. One call returns every
// non-system table with its columns already attached, so the two-query shape a
// live implementation uses (system.tables, then system.columns) stays that
// implementation's business and a test can hand over a literal slice.
//
// The returned tables need not be sorted; a run sorts them itself.
type FetcherI interface {
	FetchTables(ctx context.Context) (tables []TableSnapshot, err error)
}
