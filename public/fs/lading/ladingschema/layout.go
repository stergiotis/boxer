package ladingschema

// Layout is where one lading store's three tables live. The zero value is
// the default: [DatabaseName], beside the facts table whose shape the tables
// carry. A consuming repository that keeps its own facts in a database of its
// own (ADR-0198 Updates 2026-09-04) sets Database and hands the same value to
// every function that takes a Layout, and the store — provisioning, the
// generated stores, the snapshot index, the adapter's index reads — follows.
//
// The table names are not a degree of freedom: `fsmeta`, `fsdata` and
// `fssnap` are what the SQL surface and every operator instruction call them,
// and a store that renamed them would be a different store. Only the database
// moves.
//
// The SQL surface has its own spelling of the same fact —
// [github.com/stergiotis/boxer/public/fs/lading/ladingsql.Config] takes a
// database — because it is configured where a pass registry is built, not
// where a store is opened.
type Layout struct {
	// Database is the ClickHouse database; empty is [DatabaseName].
	Database string
}

// DatabaseName is the database the layout resolves to.
func (inst Layout) DatabaseName() (db string) {
	db = inst.Database
	if db == "" {
		db = DatabaseName
	}
	return
}

// MetaTable is the qualified name of the entry table.
func (inst Layout) MetaTable() (name string) { return inst.DatabaseName() + "." + TableNameMeta }

// DataTable is the qualified name of the block table.
func (inst Layout) DataTable() (name string) { return inst.DatabaseName() + "." + TableNameData }

// SnapTable is the qualified name of the snapshot index.
func (inst Layout) SnapTable() (name string) { return inst.DatabaseName() + "." + TableNameSnap }

// SnapView is the unqualified name of the materialised view that fills the
// snapshot index; [Layout.DatabaseName] qualifies it.
func (inst Layout) SnapView() (name string) { return TableNameSnap + "_mv" }
