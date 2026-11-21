package recordstoretest

import (
	"context"
	"iter"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/storage/recordstore"
)

var _ recordstore.ExecutorI = (*HookExecutor)(nil)
var _ recordstore.InsertSettingsI = (*HookExecutor)(nil)

// HookExecutor forwards every executor seam to Inner unless a hook replaces
// it. Set the one hook a fake needs and leave the others nil.
//
// The two insert seams share their hooks: with only OnInsertArrow set,
// InsertArrowWith calls it (the settings are not applied, and applied is
// false); with only OnInsertArrowWith set, InsertArrow calls it with zero
// settings. A hook therefore sees every insert the store makes, whichever
// seam the store chose. Without an insert hook both seams forward to Inner,
// InsertArrowWith through recordstore.InsertArrowWith so Inner's own
// settings support is used.
//
// A nil Inner is an executor that accepts every statement and insert and
// answers every query with no rows.
type HookExecutor struct {
	Inner             recordstore.ExecutorI
	OnExec            func(ctx context.Context, sql string) error
	OnQueryArrow      func(ctx context.Context, sql string) iter.Seq2[arrow.RecordBatch, error]
	OnInsertArrow     func(ctx context.Context, table string, records []arrow.RecordBatch) error
	OnInsertArrowWith func(ctx context.Context, table string, records []arrow.RecordBatch, settings recordstore.InsertSettings) (applied bool, err error)
}

func (inst *HookExecutor) Exec(ctx context.Context, sql string) (err error) {
	switch {
	case inst.OnExec != nil:
		return inst.OnExec(ctx, sql)
	case inst.Inner != nil:
		return inst.Inner.Exec(ctx, sql)
	}
	return
}

func (inst *HookExecutor) QueryArrow(ctx context.Context, sql string) iter.Seq2[arrow.RecordBatch, error] {
	switch {
	case inst.OnQueryArrow != nil:
		return inst.OnQueryArrow(ctx, sql)
	case inst.Inner != nil:
		return inst.Inner.QueryArrow(ctx, sql)
	}
	return func(func(arrow.RecordBatch, error) bool) {}
}

func (inst *HookExecutor) InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) (err error) {
	switch {
	case inst.OnInsertArrow != nil:
		return inst.OnInsertArrow(ctx, table, records)
	case inst.OnInsertArrowWith != nil:
		_, err = inst.OnInsertArrowWith(ctx, table, records, recordstore.InsertSettings{})
		return
	case inst.Inner != nil:
		return inst.Inner.InsertArrow(ctx, table, records)
	}
	return
}

func (inst *HookExecutor) InsertArrowWith(ctx context.Context, table string, records []arrow.RecordBatch, settings recordstore.InsertSettings) (applied bool, err error) {
	switch {
	case inst.OnInsertArrowWith != nil:
		return inst.OnInsertArrowWith(ctx, table, records, settings)
	case inst.OnInsertArrow != nil:
		return settings.IsZero(), inst.OnInsertArrow(ctx, table, records)
	case inst.Inner != nil:
		return recordstore.InsertArrowWith(ctx, inst.Inner, table, records, settings)
	}
	return true, nil
}
