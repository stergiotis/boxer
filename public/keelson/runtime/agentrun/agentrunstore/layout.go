package agentrunstore

import (
	"context"

	"github.com/stergiotis/boxer/public/identity/identgen/leased/rowalloc"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// Layout names the database the tables live in. The zero value is the
// runtime's own.
type Layout struct {
	Database string
}

// DatabaseName is the layout's database, defaulting to [DatabaseName].
func (inst Layout) DatabaseName() (db string) {
	db = inst.Database
	if db == "" {
		db = DatabaseName
	}
	return
}

// RunTable is the qualified run table reference; EntryTable the ledger's;
// EntryVaultTable the vault's.
func (inst Layout) RunTable() (name string)   { return inst.DatabaseName() + "." + TableNameRun }
func (inst Layout) EntryTable() (name string) { return inst.DatabaseName() + "." + TableNameEntry }
func (inst Layout) EntryVaultTable() (name string) {
	return inst.DatabaseName() + "." + TableNameEntryVault
}

// Stores is what a consumer holds: the run store, the ledger and its vault,
// and the profile's writer and reader over the two (ADR-0327 §SD4). Every
// generated store is single-goroutine; the SQL store confines them to one
// mutex.
type Stores struct {
	Run     *RunStore
	Entry   *EntryStore
	Vault   *EntryVaultStore
	Vaulted *EntryVaulted
}

// NewStores opens the stores over the layout's tables, the run store
// minting its Key from runKeys (nil: it refuses to mint), the ledger under
// [ChainScheme]; the caller closes.
func NewStores(exec recordstore.ExecutorI, layout Layout, runKeys identifier.IdGeneratorI) (st Stores) {
	st.Run = NewRunStore(exec, nil, RunStoreConfig{Table: layout.RunTable(), Keys: runKeys})
	st.Entry = NewEntryStore(exec, nil, EntryStoreConfig{Table: layout.EntryTable(), Scheme: ChainScheme})
	st.Vault = NewEntryVaultStore(exec, nil, EntryVaultStoreConfig{Table: layout.EntryVaultTable(), DDLTail: VaultDDLTail})
	st.Vaulted = NewEntryVaulted(st.Entry, st.Vault)
	return
}

// NewScratch is the stores with no executor: what computes canonical items,
// prepares and checks rows without a server (ADR-0325 §SD2).
func NewScratch() (st Stores) { return NewStores(nil, Layout{}, nil) }

// Close releases the stores.
func (inst Stores) Close() {
	if inst.Run != nil {
		inst.Run.Close()
	}
	if inst.Vaulted != nil {
		inst.Vaulted.Close()
		return
	}
	if inst.Entry != nil {
		inst.Entry.Close()
	}
	if inst.Vault != nil {
		inst.Vault.Close()
	}
}

// ProvisionIn creates the tables, and the id lease table beside them, and
// checks their shape; idempotent. A table an earlier schema created
// (CREATE TABLE IF NOT EXISTS accepts any shape) fails the check, naming
// the drift, rather than the first insert.
func ProvisionIn(ctx context.Context, exec recordstore.ExecutorI, layout Layout) (err error) {
	// The run and entry ids are leased from the layout database's lease
	// table (ADR-0319 §SD2, ADR-0325 §SD5).
	if err = rowalloc.ProvisionIn(ctx, exec, rowalloc.Layout{Database: layout.Database}); err != nil {
		return eh.Errorf("provision the id lease table: %w", err)
	}
	st := NewStores(exec, layout, nil)
	defer st.Close()
	if err = st.Run.EnsureTable(ctx); err != nil {
		return eh.Errorf("ensure run table: %w", err)
	}
	if err = st.Entry.EnsureTable(ctx); err != nil {
		return eh.Errorf("ensure entry table: %w", err)
	}
	if err = st.Vault.EnsureTable(ctx); err != nil {
		return eh.Errorf("ensure vault table: %w", err)
	}
	if err = st.Run.VerifySchema(ctx); err != nil {
		return eh.Errorf("run table: %w", err)
	}
	if err = st.Entry.VerifySchema(ctx); err != nil {
		return eh.Errorf("entry table: %w", err)
	}
	if err = st.Vault.VerifySchema(ctx); err != nil {
		return eh.Errorf("vault table: %w", err)
	}
	return
}
