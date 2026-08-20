// Package lading is the fs snapshot store (ADR-0198): a walk of an `fs.FS` is
// written once as a snapshot into facts-shaped ClickHouse tables the store
// controls — never updated, never shared, retained by TTL — and read back
// through a snapshot-pinned `io/fs` adapter or queried as SQL.
//
// The name is the ship's paper. A bill of lading is issued once for one
// voyage, lists exactly what was loaded, and is never amended — a later voyage
// gets a later bill. That is this store's contract for a snapshot: written
// once, complete when its root row signs it, superseded rather than edited,
// and expiring on a schedule the paper itself carries.
//
// The kinds live beside this file — [ladingmeta] for entries and the commit
// record, [ladingdata] for blocks, [ladingpolicy] for a mount's declared
// policy on `boxer.facts` — the memberships that tag them are
// [github.com/stergiotis/boxer/public/fs/lading/ladingvocab]'s, and what the
// tables are is [ladingschema]'s. This package is the step between: create the
// tables, add what CREATE TABLE cannot express, and check the result.
//
// # Why provisioning is three steps and not one
//
// A generated store's EnsureTable renders one CREATE TABLE from the leeway
// descriptor plus the ADR-0102 clause seam. Three things the design needs are
// outside that: MATERIALIZED columns over the natural key (a leeway plain, so
// no generator writes them — the read surface records that gap), a CHECK
// constraint, and a materialised view. All three are ALTERs after the fact,
// all three are idempotent, and none of them changes what the store decodes:
// ClickHouse keeps MATERIALIZED columns out of `SELECT *`, which is what the
// positional decode reads.
package lading

import (
	"context"
	"fmt"
	"strings"

	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/stergiotis/boxer/public/fs/lading/ladingdata"
	"github.com/stergiotis/boxer/public/fs/lading/ladingmeta"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// Stores is the pair every consumer of a lading store holds: the entry table
// and the block table. One type rather than one per consumer, so a walker and
// a reader cannot drift on what "the store" is.
//
// Both are single-goroutine, like every generated store, and so is everything
// that takes this.
type Stores struct {
	// Meta takes and serves the entry rows and the commit records.
	Meta *ladingmeta.MetaStore
	// Data takes and serves the block rows. Nil is legal for a reader that
	// only stats, and for a writer whose policy stores no content.
	Data *ladingdata.DataStore
}

// SnapshotIndex binds a store over `fssnap`, the snapshot index.
//
// It is the entry store's own generated code pointed at another table: all
// three tables carry one descriptor, so the rows the materialised view copies
// decode through exactly the same reader. Which is why there is no generated
// store of its own for `fssnap` — nothing writes to it, and nothing needs to.
//
// Reading the index rather than the entry table is the difference between one
// row per snapshot and every path of every snapshot: "the newest snapshot of
// this mount" is a question the entry table can only answer by scanning it.
func SnapshotIndex(exec recordstore.ExecutorI) *ladingmeta.MetaStore {
	return ladingmeta.NewMetaStore(exec, nil, ladingmeta.MetaStoreConfig{
		Table: ladingschema.DatabaseName + "." + ladingschema.TableNameSnap,
	})
}

// Provision creates the store's tables and finishes them: the tree columns,
// the path constraint, the directory skip index and the snapshot view.
//
// It is idempotent — every statement is IF NOT EXISTS — so it may run at every
// start, and it must, because a store that starts against a half-provisioned
// table reads correctly and indexes nothing.
//
// The profile reaches all three CREATE TABLEs, and only their granularity; a
// table that already exists keeps the profile it was created under. Changing a
// store's profile is therefore a migration, not a restart.
//
// The CREATE TABLEs are composed here rather than taken from the generated
// stores' EnsureTable, whose DDL is rendered at code-generation time and so
// carries one fixed granularity — see [ladingschema.CreateTableStatements].
func Provision(ctx context.Context, exec recordstore.ExecutorI, p ladingschema.Profile) (err error) {
	creates, err := ladingschema.CreateTableStatements(p)
	if err != nil {
		return
	}
	for _, sql := range creates {
		err = exec.Exec(ctx, sql)
		if err != nil {
			err = eh.Errorf("create table: %w", err)
			return
		}
	}

	stmts, err := ladingschema.FinishStatements(p)
	if err != nil {
		return
	}
	for _, sql := range stmts {
		err = exec.Exec(ctx, sql)
		if err != nil {
			err = eh.Errorf("finish schema: %w", err)
			return
		}
	}
	return
}

// Verify checks that the live store is the one the code expects, and is meant
// to run at start after [Provision].
//
// It checks more than the two generated VerifySchema verbs, because those
// check less than this store needs. They compare the physical columns a
// positional decode reads, which is exactly the half [Provision] does not add:
// the tree columns are MATERIALIZED and so are hidden from `SELECT *` (which
// is what VerifySchema now skips), `fssnap` is a table no generated store
// creates, and the view that fills it is not a column at all. A store missing
// any of them decodes every row correctly and then fails on the first ReadDir
// — over a pipe rclone has already mounted, in the `sftp-stdio` case, which
// runs Verify precisely so that it does not create anything.
//
// EnsureTable cannot do this either: IF NOT EXISTS succeeds against any older
// shape, and the decode is positional — so drift fails late, or for a
// same-typed column swap, silently.
func Verify(ctx context.Context, exec recordstore.ExecutorI) (err error) {
	meta := ladingmeta.NewMetaStore(exec, nil, ladingmeta.MetaStoreConfig{})
	defer meta.Close()
	data := ladingdata.NewDataStore(exec, nil, ladingdata.DataStoreConfig{})
	defer data.Close()
	snap := SnapshotIndex(exec)
	defer snap.Close()

	for _, t := range []struct {
		name   string
		verify func(context.Context) error
	}{
		{ladingschema.TableNameMeta, meta.VerifySchema},
		{ladingschema.TableNameData, data.VerifySchema},
		// `fssnap` decodes through the entry store pointed at another table,
		// so the same check covers it — and it is the table most able to
		// drift, being created by a different path from the other two.
		{ladingschema.TableNameSnap, snap.VerifySchema},
	} {
		err = t.verify(ctx)
		if err != nil {
			err = eh.Errorf("verify %s: %w", t.name, err)
			return
		}
	}

	err = verifyFinished(ctx, exec)
	return
}

// treeColumns is what [ladingschema.FinishStatements] adds to `fsmeta` and
// what the adapter's ReadDir, the `dir` skip index and the SQL surface's
// `name` / `depth` / `ext` all read. Absent, every query naming one fails with
// "unknown expression or function identifier".
var treeColumns = []string{"name", "dir", "depth", "ext"}

// verifyFinished checks the half of provisioning that is not columns of the
// decode: the materialised tree columns and the view that fills `fssnap`.
//
// It reads `system.columns` and `system.tables` rather than DESCRIBE, because
// what is being asked is whether an ALTER and a CREATE ran, not what a reader
// would see.
func verifyFinished(ctx context.Context, exec recordstore.ExecutorI) (err error) {
	missing, err := scalarStrings(ctx, exec, fmt.Sprintf(
		`SELECT arrayJoin(arrayFilter(c -> NOT has(groupArray(name), c), %s)) FROM system.columns WHERE database = %s AND table = %s AND default_kind = 'MATERIALIZED'`,
		sqlStringArray(treeColumns), ladingschema.QuoteLiteral(ladingschema.DatabaseName), ladingschema.QuoteLiteral(ladingschema.TableNameMeta)))
	if err != nil {
		err = eh.Errorf("read materialized columns of %s: %w", ladingschema.TableNameMeta, err)
		return
	}
	if len(missing) > 0 {
		err = eb.Build().Str("table", ladingschema.TableNameMeta).Str("columns", strings.Join(missing, ", ")).
			Errorf("materialized tree columns are missing; the store was created but never finished — run Provision")
		return
	}

	view := ladingschema.TableNameSnap + "_mv"
	found, err := scalarStrings(ctx, exec, fmt.Sprintf(
		`SELECT name FROM system.tables WHERE database = %s AND name = %s AND engine = 'MaterializedView'`,
		ladingschema.QuoteLiteral(ladingschema.DatabaseName), ladingschema.QuoteLiteral(view)))
	if err != nil {
		err = eh.Errorf("read materialized view %s: %w", view, err)
		return
	}
	if len(found) == 0 {
		err = eb.Build().Str("view", ladingschema.DatabaseName+"."+view).
			Errorf("the snapshot view is missing; nothing would commit a snapshot — run Provision")
	}
	return
}

// scalarStrings runs a query of one String column and collects it.
func scalarStrings(ctx context.Context, exec recordstore.ExecutorI, sql string) (out []string, err error) {
	for rec, rerr := range exec.QueryArrow(ctx, sql) {
		if rerr != nil {
			err = rerr
			return
		}
		if rec.NumCols() < 1 {
			rec.Release()
			err = eh.Errorf("query returned no columns")
			return
		}
		col, ok := rec.Column(0).(*array.String)
		if !ok {
			err = eh.Errorf("column is %s, not a string", rec.Column(0).DataType())
			rec.Release()
			return
		}
		for i := range int(rec.NumRows()) {
			out = append(out, col.Value(i))
		}
		rec.Release()
	}
	return
}

// sqlStringArray renders the column-name list of the check above. The values
// are this package's own constants, never a caller's, but they are quoted
// rather than interpolated so the statement's shape cannot depend on them.
func sqlStringArray(ss []string) string {
	parts := make([]string, 0, len(ss))
	for _, s := range ss {
		parts = append(parts, ladingschema.QuoteLiteral(s))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
