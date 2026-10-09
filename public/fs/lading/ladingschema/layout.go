package ladingschema

import "github.com/stergiotis/boxer/public/keelson/runtime/factsschema"

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
// Every surface takes this one type — the generated stores through
// lading.NewStores, provisioning and Verify, the adapter's index reads, the
// SFTP head, the ad-hoc publisher, the CLI's `--database`, and the SQL
// macros through
// [github.com/stergiotis/boxer/public/fs/lading/ladingsql.Config] — so there
// is one spelling of where a store lives rather than one per surface.
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

// PolicyTable is the qualified name of the facts table beside the store,
// where a mount's declared policy is recorded (ladingingest.RecordPolicy)
// and a browser reads the mount's name from. It follows the database for the
// same reason the three tables do: a repository that keeps its facts out of
// the default database keeps its policy rows out of it too, and the default
// layout resolves to the baked policy table.
func (inst Layout) PolicyTable() (name string) {
	return inst.DatabaseName() + "." + factsschema.TableName
}
