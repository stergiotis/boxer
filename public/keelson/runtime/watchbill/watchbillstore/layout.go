package watchbillstore

import (
	"context"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// Layout names the database the two tables live in (ADR-0223 §SD1). The
// zero value is the runtime's own; a consumer whose facts live elsewhere
// sets Database and both tables follow, as lading's layout does.
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

// JobTable is the qualified job table reference.
func (inst Layout) JobTable() (name string) { return inst.DatabaseName() + "." + TableNameJob }

// EventTable is the qualified event table reference.
func (inst Layout) EventTable() (name string) { return inst.DatabaseName() + "." + TableNameEvent }

// jobTableSettingsTail is what the job table's CREATE gains beyond the
// generator's own settings: the two block-position columns the lightweight
// UPDATE needs (ADR-0223 §SD3). It extends the SETTINGS clause the
// generated DDL ends with.
const jobTableSettingsTail = ", enable_block_number_column=1, enable_block_offset_column=1"

// jobTableSettingsAlter is the same pair as an ALTER, for a table created
// before the tail existed; MODIFY SETTING is idempotent.
const jobTableSettingsAlter = "ALTER TABLE %s MODIFY SETTING enable_block_number_column=1, enable_block_offset_column=1"

// Stores is the pair a consumer holds. Both are single-goroutine, as every
// generated store is; the worker confines them to one goroutine.
type Stores struct {
	Job   *JobStore
	Event *EventStore
}

// NewStores opens the pair over the layout's tables. The stores are the
// caller's to Close.
func NewStores(exec recordstore.ExecutorI, layout Layout) (st Stores) {
	st.Job = NewJobStore(exec, nil, JobStoreConfig{Table: layout.JobTable(), DDLTail: jobTableSettingsTail})
	st.Event = NewEventStore(exec, nil, EventStoreConfig{Table: layout.EventTable()})
	return
}

// Close releases both stores. Rows not yet flushed are lost; the worker
// flushes after every write.
func (inst Stores) Close() {
	if inst.Job != nil {
		inst.Job.Close()
	}
	if inst.Event != nil {
		inst.Event.Close()
	}
}

// ProvisionIn creates both tables and puts the job table's settings in
// place. Idempotent — every statement is IF NOT EXISTS or a MODIFY SETTING
// — so it runs at every start.
func ProvisionIn(ctx context.Context, exec recordstore.ExecutorI, layout Layout) (err error) {
	st := NewStores(exec, layout)
	defer st.Close()
	if err = st.Job.EnsureTable(ctx); err != nil {
		return eh.Errorf("ensure job table: %w", err)
	}
	if err = st.Event.EnsureTable(ctx); err != nil {
		return eh.Errorf("ensure event table: %w", err)
	}
	if err = exec.Exec(ctx, AlterJobTableSettingsSQL(layout)); err != nil {
		return eh.Errorf("job table settings: %w", err)
	}
	return
}
