package ladingschema

import (
	"fmt"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// FinishStatements is everything a CREATE TABLE cannot express, as one
// idempotent script: the tree columns over the natural key, the path
// constraint, the directory skip index, the snapshot table and the view that
// fills it.
//
// One statement per element, because the ClickHouse HTTP interface rejects a
// multi-statement body and the executor contract is one statement per Exec.
//
// # Why the tree columns are hand-written
//
// They are MATERIALIZED expressions over a leeway plain, and nothing generates
// those from a leeway schema — the read surface records the gap, and the
// alternative it names is exactly this: physical names inlined by hand. They
// are inlined *once*, here, from [PhysicalPlainName], so a rename in
// factsschema moves them rather than leaving a clause over a column that is no
// longer there.
//
// # What each of them is for
//
//   - `name` and `dir` split the path, and `dir` is what ReadDir is: one
//     equality with a bloom filter behind it, rather than a scan of the
//     mount. Measured at M0: 197 granules to 12 on a synthetic tree.
//   - `depth` is what a bounded walk and a `du` rollup range over.
//   - `ext` groups a store by file type. It reads a leading dot as part of the
//     name, so `.gitignore` has no extension and `.hidden.txt` has `.txt`.
//     The first draft of this expression gave the root row an extension of
//     `.` and read `.gitignore` as being entirely one; both are fixed here
//     (ADR-0198 `## Updates` 2026-08-19).
//
// # The constraint
//
// `io/fs` names are unrooted, `/`-separated and carry no empty, `.` or `..`
// element; the root alone is `.`. A row breaking that would read back as a
// path the adapter cannot produce and cannot address, so it is refused at
// insert rather than found later.
func FinishStatements(p Profile) (stmts []string, err error) {
	naturalKey, err := PhysicalPlainName("naturalKey")
	if err != nil {
		return
	}
	meta := DatabaseName + "." + TableNameMeta
	snap := DatabaseName + "." + TableNameSnap

	stmts = []string{
		// The tree columns. `dir` depends on `name`, which ClickHouse allows
		// and computes in order — verified at M0 rather than assumed.
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS name String MATERIALIZED splitByChar('/', %s)[-1]`,
			meta, naturalKey),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS dir String MATERIALIZED multiIf(%s = '.', '', position(%s, '/') = 0, '.', substring(%s, 1, length(%s) - length(name) - 1))`,
			meta, naturalKey, naturalKey, naturalKey, naturalKey),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS depth UInt16 MATERIALIZED if(%s = '.', 0, length(splitByChar('/', %s)))`,
			meta, naturalKey, naturalKey),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS ext LowCardinality(String) MATERIALIZED if(position(substring(name, 2), '.') = 0, '', concat('.', splitByChar('.', name)[-1]))`,
			meta),

		fmt.Sprintf(`ALTER TABLE %s ADD CONSTRAINT IF NOT EXISTS valid_path CHECK %s = '.' OR NOT hasAny(splitByChar('/', %s), ['', '.', '..'])`,
			meta, naturalKey, naturalKey),
		fmt.Sprintf(`ALTER TABLE %s ADD INDEX IF NOT EXISTS ix_dir dir TYPE bloom_filter GRANULARITY 4`, meta),
	}

	// The snapshot index. It is a table like the other two rather than a view
	// over fsmeta, because "the newest complete snapshot of this mount" is a
	// query the fs() expansion runs on every read, and answering it from the
	// entry table would scan every path of every snapshot to find the root
	// rows.
	snapDDL, err := composeSnapTable(p)
	if err != nil {
		return
	}
	stmts = append(stmts, snapDDL)

	// The view. Its predicate is the commit rule (ADR-0198 §SD6): a root row
	// exists exactly when a walk finished, because the walker writes it last
	// and in a later insert than the batch's other rows.
	//
	// A key-range equality rather than a test for the snapshot component,
	// which would be the structural spelling. This predicate runs on every
	// insert block into fsmeta, and a path equality is the cheapest thing it
	// can be; the two agree because a root row is written once, carrying both
	// components, after the rest of the walk is durable. That is the walker's
	// contract, so it is the walker's test to keep (M2) — what is checked
	// here is that a root row written that way arrives in fssnap with its
	// commit record intact.
	stmts = append(stmts, fmt.Sprintf(
		`CREATE MATERIALIZED VIEW IF NOT EXISTS %s.%s_mv TO %s AS SELECT * FROM %s WHERE %s = '.'`,
		DatabaseName, TableNameSnap, snap, meta, naturalKey))
	return
}

// composeSnapTable renders `fssnap`'s CREATE TABLE.
//
// It cannot come from a generated store's EnsureTable: nothing writes to
// `fssnap` directly, so no store is generated for it, and a store that existed
// only to provision a table would carry ingest verbs nothing may call. The
// columns are the same descriptor the other two use, so the view's `SELECT *`
// lines up column for column.
func composeSnapTable(p Profile) (sql string, err error) {
	td, err := TableDesc(TableNameSnap)
	if err != nil {
		return
	}
	sql, err = composeCreateTable(DatabaseName+"."+TableNameSnap, td, SnapTableOptions(p))
	if err != nil {
		err = eh.Errorf("ladingschema: compose %s: %w", TableNameSnap, err)
	}
	return
}
