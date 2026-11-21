package recordstore

import (
	"context"
	"errors"
	"iter"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// InsertTransformI rewrites one insert into another table's schema (ADR-0402
// §SD1): a deployment that keeps a table's rows in a table of its own
// receives the batches a store built for the table, and returns the table to
// insert into and the batches in that table's schema.
//
// TransformInsert must be a pure function of its input: the same batches give
// the same target and the same output, so a re-sent batch is the same block
// under the same deduplication token. It must not retain or release the
// batches it receives. The batches it returns are released by the caller
// once the insert returned, so one it passes through unchanged is Retained
// first. One insert maps to one insert into one table: there is no fan-out.
type InsertTransformI interface {
	TransformInsert(table string, records []arrow.RecordBatch) (target string, out []arrow.RecordBatch, err error)
}

// ErrTransformedTable is what a statement that would change, empty or
// create a table by its name returns when an insert transform is registered
// for that table (ADR-0402 §SD5): the rows are stored in a deployment's own
// table, which the statement would not reach, so it is refused rather than
// run against a relation that holds none of them. Match with errors.Is.
var ErrTransformedTable = errors.New("the table's inserts are transformed into a deployment's own table, which this statement does not reach")

// InsertTransformFunc adapts a function as an InsertTransformI.
type InsertTransformFunc func(table string, records []arrow.RecordBatch) (target string, out []arrow.RecordBatch, err error)

var _ InsertTransformI = InsertTransformFunc(nil)

func (inst InsertTransformFunc) TransformInsert(table string, records []arrow.RecordBatch) (target string, out []arrow.RecordBatch, err error) {
	return inst(table, records)
}

// insertTransforms is the process-wide registry: table reference → transform.
var insertTransforms struct {
	mu sync.RWMutex
	m  map[string]*registeredTransform
}

// registeredTransform is one registration; its address tells it from a later
// one for the same table, which a func-typed transform could not.
type registeredTransform struct{ t InsertTransformI }

// RegisterInsertTransform registers t for every insert into table, for the
// whole process (ADR-0402 §SD2). A binary registers before it builds any
// store: the executors boxer constructs consult the registry on each insert,
// and a writer that ran before the registration wrote the untransformed
// shape. table is compared as written, so it is the reference the writers
// use — "boxer.facts" for the facts table.
//
// It panics on a malformed reference, a nil t, or a second registration for
// the same table: a registration is wiring, and two transforms for one table
// would make which one ran depend on the order of init. unregister removes
// the registration; a binary does not call it, a test does.
func RegisterInsertTransform(table string, t InsertTransformI) (unregister func()) {
	if err := CheckTableRef(table); err != nil {
		panic("recordstore.RegisterInsertTransform: " + err.Error())
	}
	if t == nil {
		panic("recordstore.RegisterInsertTransform: nil transform")
	}
	insertTransforms.mu.Lock()
	defer insertTransforms.mu.Unlock()
	if _, dup := insertTransforms.m[table]; dup {
		panic("recordstore.RegisterInsertTransform: a transform is already registered for " + table)
	}
	if insertTransforms.m == nil {
		insertTransforms.m = make(map[string]*registeredTransform, 1)
	}
	reg := &registeredTransform{t: t}
	insertTransforms.m[table] = reg
	unregister = func() {
		insertTransforms.mu.Lock()
		defer insertTransforms.mu.Unlock()
		if insertTransforms.m[table] == reg {
			delete(insertTransforms.m, table)
		}
	}
	return
}

// RegisteredInsertTransform is the transform registered for table, if any.
func RegisteredInsertTransform(table string) (t InsertTransformI, ok bool) {
	insertTransforms.mu.RLock()
	defer insertTransforms.mu.RUnlock()
	reg, ok := insertTransforms.m[table]
	if ok {
		t = reg.t
	}
	return
}

// TransformInsert applies t to one insert into table. release must be
// called once the transformed insert returned; it releases the batches t
// returned. A target that is not a table reference, or an error from t, fails
// the insert before anything is sent. An empty records is passed through
// untransformed: it sends nothing.
func TransformInsert(t InsertTransformI, table string, records []arrow.RecordBatch) (target string, out []arrow.RecordBatch, release func(), err error) {
	release = func() {}
	if t == nil || len(records) == 0 {
		return table, records, release, nil
	}
	target, out, err = t.TransformInsert(table, records)
	releaseOut := func() {
		for _, r := range out {
			if r != nil {
				r.Release()
			}
		}
	}
	if err != nil {
		releaseOut()
		err = eb.Build().Str("table", table).Errorf("recordstore: insert transform: %w", err)
		return
	}
	if cerr := CheckTableRef(target); cerr != nil {
		releaseOut()
		err = eb.Build().Str("table", table).Str("target", target).Errorf("recordstore: insert transform returned no usable target: %w", cerr)
		return
	}
	release = releaseOut
	return
}

// ApplyRegisteredTransform is [TransformInsert] with the transform registered
// for table, and a pass-through when there is none. It is what an executor
// that consults the registry calls on each insert.
func ApplyRegisteredTransform(table string, records []arrow.RecordBatch) (target string, out []arrow.RecordBatch, release func(), err error) {
	t, ok := RegisteredInsertTransform(table)
	if !ok {
		return table, records, func() {}, nil
	}
	return TransformInsert(t, table, records)
}

// TransformingExecutor is the decorator [TransformExecutor] and
// [RegisteredTransforms] return: inserts into the table it transforms go
// through the transform, and every other call — statements, queries, inserts
// into other tables — reaches the inner executor untouched. The settings of
// an insert, its deduplication token among them, pass through to the
// transformed insert. It is safe for concurrent use when its inner executor
// and its transform are.
type TransformingExecutor struct {
	inner  ExecutorI
	lookup func(table string) (InsertTransformI, bool)
}

var _ ExecutorI = (*TransformingExecutor)(nil)
var _ InsertSettingsI = (*TransformingExecutor)(nil)

// TransformExecutor decorates inner so that inserts into table go through t.
func TransformExecutor(inner ExecutorI, table string, t InsertTransformI) (inst *TransformingExecutor) {
	if inner == nil || t == nil {
		panic("recordstore.TransformExecutor: nil inner executor or transform")
	}
	return &TransformingExecutor{inner: inner, lookup: func(tbl string) (InsertTransformI, bool) {
		if tbl == table {
			return t, true
		}
		return nil, false
	}}
}

// RegisteredTransforms decorates inner so that each insert goes through the
// transform registered for its table at the time of the insert
// ([RegisterInsertTransform]). It is for an executor that does not consult
// the registry itself.
func RegisteredTransforms(inner ExecutorI) (inst *TransformingExecutor) {
	if inner == nil {
		panic("recordstore.RegisteredTransforms: nil inner executor")
	}
	return &TransformingExecutor{inner: inner, lookup: RegisteredInsertTransform}
}

func (inst *TransformingExecutor) Exec(ctx context.Context, sql string) error {
	return inst.inner.Exec(ctx, sql)
}

func (inst *TransformingExecutor) QueryArrow(ctx context.Context, sql string) iter.Seq2[arrow.RecordBatch, error] {
	return inst.inner.QueryArrow(ctx, sql)
}

func (inst *TransformingExecutor) InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) (err error) {
	_, err = inst.insert(ctx, table, records, InsertSettings{})
	return
}

// InsertArrowWith transforms as InsertArrow does and forwards the settings;
// applied is the inner executor's answer.
func (inst *TransformingExecutor) InsertArrowWith(ctx context.Context, table string, records []arrow.RecordBatch, settings InsertSettings) (applied bool, err error) {
	return inst.insert(ctx, table, records, settings)
}

func (inst *TransformingExecutor) insert(ctx context.Context, table string, records []arrow.RecordBatch, settings InsertSettings) (applied bool, err error) {
	t, ok := inst.lookup(table)
	if !ok {
		return InsertArrowWith(ctx, inst.inner, table, records, settings)
	}
	target, out, release, err := TransformInsert(t, table, records)
	if err != nil {
		return
	}
	defer release()
	applied, err = InsertArrowWith(ctx, inst.inner, target, out, settings)
	if err != nil {
		err = eh.Errorf("recordstore: transformed insert: %w", err)
	}
	return
}
