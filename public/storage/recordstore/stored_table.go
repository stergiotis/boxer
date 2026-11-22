package recordstore

import (
	"context"
	"iter"
	"slices"
	"strings"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// StoredTable is one table a build keeps records in, declared so that one
// command judges, creates and migrates every table the binary links.
type StoredTable struct {
	// Owner names the subsystem the table belongs to; a schema command
	// selects the tables of one owner by it. Lower-case letters, digits and
	// dashes.
	Owner string
	// Contract is what the build declares about the table.
	Contract TableContract
	// Provision creates the table, and its database, where they are missing,
	// and changes nothing that exists. Nil for a table this declaration does
	// not create: the store that owns it does, when it starts.
	Provision func(ctx context.Context, exec ExecutorI) (err error)
}

// StoredTablesFunc returns the tables of one declaration as they are
// configured when it is called: a table's name may come from the
// environment, so it is not fixed at declaration.
type StoredTablesFunc func() (tables []StoredTable, err error)

var storedTables struct {
	mu  sync.Mutex
	fns []StoredTablesFunc
}

// DeclareStoredTables registers fn among the declarations
// [DeclaredStoredTables] returns. A package calls it from init for the
// tables it stores records in.
func DeclareStoredTables(fn StoredTablesFunc) {
	if fn == nil {
		panic("recordstore: DeclareStoredTables with a nil function")
	}
	storedTables.mu.Lock()
	defer storedTables.mu.Unlock()
	storedTables.fns = append(storedTables.fns, fn)
}

// DeclaredStoredTables returns the tables of every declaration, by owner and
// then by table. It fails when a declaration does, when one names no owner,
// or when two declare the same table under the same kind.
func DeclaredStoredTables() (tables []StoredTable, err error) {
	storedTables.mu.Lock()
	fns := slices.Clone(storedTables.fns)
	storedTables.mu.Unlock()
	for _, fn := range fns {
		var ts []StoredTable
		if ts, err = fn(); err != nil {
			err = eh.Errorf("stored tables: a declaration failed: %w", err)
			return
		}
		tables = append(tables, ts...)
	}
	err = CheckStoredTables(tables)
	if err != nil {
		tables = nil
		return
	}
	slices.SortStableFunc(tables, func(a StoredTable, b StoredTable) int {
		if c := strings.Compare(a.Owner, b.Owner); c != 0 {
			return c
		}
		return strings.Compare(a.Contract.Table, b.Contract.Table)
	})
	return
}

// CheckStoredTables refuses tables without an owner or a table reference,
// and two that declare one table under one kind.
func CheckStoredTables(tables []StoredTable) (err error) {
	seen := make(map[[2]string]string, len(tables))
	for _, t := range tables {
		if !isOwnerName(t.Owner) {
			return eb.Build().Str("owner", t.Owner).Str("table", t.Contract.Table).Errorf("stored tables: an owner is named by lower-case letters, digits and dashes, starting with a letter")
		}
		if err = CheckTableRef(t.Contract.Table); err != nil {
			return eb.Build().Str("owner", t.Owner).Errorf("stored tables: %w", err)
		}
		key := [2]string{t.Contract.Table, t.Contract.Kind}
		if other, dup := seen[key]; dup {
			return eb.Build().Str("table", t.Contract.Table).Str("kind", t.Contract.Kind).Str("owner", t.Owner).Str("other", other).Errorf("stored tables: declared twice")
		}
		seen[key] = t.Owner
	}
	return
}

func isOwnerName(s string) (ok bool) {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' {
			return
		}
	}
	ok = true
	return
}

// ProvisioningSQL returns the statements provision would run, in order,
// without running them: provision is called on an executor that records
// what it is asked to execute. It fails when provision reads or inserts,
// since its statements then depend on what the server holds.
func ProvisioningSQL(ctx context.Context, provision func(ctx context.Context, exec ExecutorI) (err error)) (stmts []string, err error) {
	rec := &recordingExecutor{}
	if err = provision(ctx, rec); err != nil {
		stmts = nil
		return
	}
	stmts = rec.stmts
	return
}

// recordingExecutor keeps the statements it is asked to execute and refuses
// everything else.
type recordingExecutor struct {
	stmts []string
}

var _ ExecutorI = (*recordingExecutor)(nil)

func (inst *recordingExecutor) Exec(_ context.Context, sql string) (err error) {
	inst.stmts = append(inst.stmts, sql)
	return
}

func (inst *recordingExecutor) QueryArrow(_ context.Context, _ string) iter.Seq2[arrow.RecordBatch, error] {
	return func(yield func(arrow.RecordBatch, error) bool) {
		yield(nil, eh.Errorf("provisioning reads the server, so its statements cannot be listed without one"))
	}
}

func (inst *recordingExecutor) InsertArrow(_ context.Context, table string, _ []arrow.RecordBatch) (err error) {
	err = eb.Build().Str("table", table).Errorf("provisioning inserts rows, so its statements cannot be listed without a server")
	return
}
