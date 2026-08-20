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

	"github.com/stergiotis/boxer/public/fs/lading/ladingdata"
	"github.com/stergiotis/boxer/public/fs/lading/ladingmeta"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/observability/eh"
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
// The profile only reaches the CREATE TABLEs, and only their granularity; a
// table that already exists keeps the profile it was created under. Changing a
// store's profile is therefore a migration, not a restart.
func Provision(ctx context.Context, exec recordstore.ExecutorI, p ladingschema.Profile) (err error) {
	meta := ladingmeta.NewMetaStore(exec, nil, ladingmeta.MetaStoreConfig{})
	defer meta.Close()
	data := ladingdata.NewDataStore(exec, nil, ladingdata.DataStoreConfig{})
	defer data.Close()

	err = meta.EnsureTable(ctx)
	if err != nil {
		err = eh.Errorf("lading: ensure %s: %w", ladingschema.TableNameMeta, err)
		return
	}
	err = data.EnsureTable(ctx)
	if err != nil {
		err = eh.Errorf("lading: ensure %s: %w", ladingschema.TableNameData, err)
		return
	}

	stmts, err := ladingschema.FinishStatements(p)
	if err != nil {
		return
	}
	for _, sql := range stmts {
		err = exec.Exec(ctx, sql)
		if err != nil {
			err = eh.Errorf("lading: finish schema: %w", err)
			return
		}
	}
	return
}

// Verify checks that the live tables still have the shape the generated stores
// decode, and is meant to run at start after [Provision].
//
// EnsureTable cannot do this: IF NOT EXISTS succeeds against any older shape,
// and the decode is positional — so drift fails late, or for a same-typed
// column swap, silently. The materialised columns [Provision] adds are not a
// drift: they are absent from `SELECT *`, which is what VerifySchema skips
// them for.
func Verify(ctx context.Context, exec recordstore.ExecutorI) (err error) {
	meta := ladingmeta.NewMetaStore(exec, nil, ladingmeta.MetaStoreConfig{})
	defer meta.Close()
	data := ladingdata.NewDataStore(exec, nil, ladingdata.DataStoreConfig{})
	defer data.Close()

	err = meta.VerifySchema(ctx)
	if err != nil {
		err = eh.Errorf("lading: verify %s: %w", ladingschema.TableNameMeta, err)
		return
	}
	err = data.VerifySchema(ctx)
	if err != nil {
		err = eh.Errorf("lading: verify %s: %w", ladingschema.TableNameData, err)
		return
	}
	return
}
