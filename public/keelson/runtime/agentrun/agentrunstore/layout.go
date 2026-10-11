package agentrunstore

import (
	"context"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// Layout names the database the two tables live in. The zero value is the
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

// RunTable is the qualified run table reference; EntryTable the entry
// table's.
func (inst Layout) RunTable() (name string)   { return inst.DatabaseName() + "." + TableNameRun }
func (inst Layout) EntryTable() (name string) { return inst.DatabaseName() + "." + TableNameEntry }

// Stores is the pair a consumer holds. Both are single-goroutine, as every
// generated store is; the SQL store confines them to one mutex.
type Stores struct {
	Run   *RunStore
	Entry *EntryStore
}

// NewStores opens the pair over the layout's tables; the caller closes.
func NewStores(exec recordstore.ExecutorI, layout Layout) (st Stores) {
	st.Run = NewRunStore(exec, nil, RunStoreConfig{Table: layout.RunTable()})
	st.Entry = NewEntryStore(exec, nil, EntryStoreConfig{Table: layout.EntryTable()})
	return
}

// Close releases both stores.
func (inst Stores) Close() {
	if inst.Run != nil {
		inst.Run.Close()
	}
	if inst.Entry != nil {
		inst.Entry.Close()
	}
}

// ProvisionIn creates both tables; idempotent.
func ProvisionIn(ctx context.Context, exec recordstore.ExecutorI, layout Layout) (err error) {
	st := NewStores(exec, layout)
	defer st.Close()
	if err = st.Run.EnsureTable(ctx); err != nil {
		return eh.Errorf("ensure run table: %w", err)
	}
	if err = st.Entry.EnsureTable(ctx); err != nil {
		return eh.Errorf("ensure entry table: %w", err)
	}
	return
}
