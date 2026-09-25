package jackstay

import (
	"context"
	"slices"
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

// The workflow: one function per step of ADR-0259 §SD7, shared by the CLI and
// the wizard, so the two front ends are two editors of one plan and never two
// implementations of it. Each takes the servers' clients, reads both, and
// returns a new plan value; the caller decides where it is saved.

// DiscoverBoth reads both inventories at once.
func DiscoverBoth(ctx context.Context, src QueryI, dst QueryI) (s Inventory, d Inventory, err error) {
	s, d, err = both(func(side int) (Inventory, error) {
		if side == 0 {
			inv, e := Discover(ctx, src)
			if e != nil {
				return inv, eh.Errorf("unable to discover the source: %w", e)
			}
			return inv, nil
		}
		inv, e := Discover(ctx, dst)
		if e != nil {
			return inv, eh.Errorf("unable to discover the target: %w", e)
		}
		return inv, nil
	})
	return
}

func newTableOps() (ops *common.TableOperations, err error) {
	ops, err = common.NewTableOperations()
	if err != nil {
		err = eh.Errorf("unable to set up leeway operations: %w", err)
	}
	return
}

// PlanStructure is the Structure step: discover both servers and judge every
// selected table. old, when it names the same servers, lends the new plan its
// chunk layouts (§SD4).
func PlanStructure(ctx context.Context, src QueryI, dst QueryI, srcEp Endpoint, dstEp Endpoint, sel Selection, old *Plan, now time.Time) (plan Plan, err error) {
	var s, d Inventory
	s, d, err = DiscoverBoth(ctx, src, dst)
	if err != nil {
		return
	}
	var ops *common.TableOperations
	ops, err = newTableOps()
	if err != nil {
		return
	}
	plan, err = BuildPlan(ops, srcEp, dstEp, &s, &d, sel, now)
	if err != nil {
		return
	}
	if old != nil && old.Source == plan.Source && old.Target == plan.Target {
		plan.CarryOver(old)
	}
	return
}

// Recheck rediscovers both servers and restates plan (§SD1, §SD3): fresh is
// the plan as the servers now call for it, carrying plan's chunk layouts,
// diffs and sync state; stale lists what moved. A caller must not act on a
// plan whose stale list is not empty.
func Recheck(ctx context.Context, src QueryI, dst QueryI, plan *Plan, now time.Time) (fresh Plan, stale []string, err error) {
	var s, d Inventory
	s, d, err = DiscoverBoth(ctx, src, dst)
	if err != nil {
		return
	}
	var ops *common.TableOperations
	ops, err = newTableOps()
	if err != nil {
		return
	}
	fresh, stale, err = Restate(ops, plan, &s, &d, now)
	if err != nil {
		return
	}
	fresh.CarryOver(plan)
	fresh.CarryDiffs(plan)
	return
}

// ErrStale is returned by steps that refuse a plan the servers have moved away
// from.
var ErrStale = eh.Errorf("the plan no longer matches the servers")

// ApplyDDLStep is the confirmation of the Structure step: recheck, run the
// pending DDL through ddl (a client carrying [DDLClientConfig]), and judge
// again. It returns the statements that ran and the plan afterwards; a stale
// plan runs nothing.
func ApplyDDLStep(ctx context.Context, src QueryI, dst QueryI, ddl ExecI, plan *Plan, now time.Time) (applied []string, after Plan, stale []string, err error) {
	var pending Plan
	pending, stale, err = Recheck(ctx, src, dst, plan, now)
	if err != nil || len(stale) > 0 {
		return
	}
	applied, err = ApplyStructure(ctx, ddl, &pending)
	if err != nil {
		return
	}
	after, err = PlanStructure(ctx, src, dst, plan.Source, plan.Target, plan.Selection, plan, now)
	return
}

// DiffOptionsAll gathers what the Differences step needs.
type DiffOptionsAll struct {
	Final    bool
	Chunking ChunkingOptions
	Diff     DiffOptions
	// Only restricts the step to these source tables; empty diffs every
	// diffable one.
	Only []datacatalog.TableRef
	// Progress, when set, is called before each table and once after the
	// last, with the index of the table about to be diffed.
	Progress func(i int, n int, pt *PlanTable)
}

// DiffStep is the Differences step over a plan that [Recheck] found current:
// every diffable table in scope is diffed in place (§SD4).
func DiffStep(ctx context.Context, src QueryI, dst QueryI, plan *Plan, opts DiffOptionsAll, now func() time.Time) (skipped []string, err error) {
	todo := make([]*PlanTable, 0, len(plan.Tables))
	for i := range plan.Tables {
		pt := &plan.Tables[i]
		if len(opts.Only) > 0 && !slices.Contains(opts.Only, pt.Source) {
			continue
		}
		if !pt.IsDiffable() {
			if pt.Verdict.IsSyncable() {
				skipped = append(skipped, pt.Source.String()+": verdict "+pt.Verdict.String()+" (apply the DDL first)")
			}
			continue
		}
		todo = append(todo, pt)
	}
	for i, pt := range todo {
		if opts.Progress != nil {
			opts.Progress(i, len(todo), pt)
		}
		err = DiffPlanTable(ctx, src, dst, pt, opts.Final, opts.Chunking, opts.Diff, now())
		if err != nil {
			return
		}
	}
	if opts.Progress != nil {
		opts.Progress(len(todo), len(todo), nil)
	}
	return
}

// PrepareSync is the Sync step's choice: the tables in scope that can take ts,
// with a chunk layout and ts set on each. A table that cannot is listed in
// skipped with the reason; a repair has nothing to do on a table whose diff
// is identical.
func PrepareSync(ctx context.Context, src QueryI, plan *Plan, ts TableSync, only []datacatalog.TableRef, chunkOpts ChunkingOptions) (chosen []*PlanTable, skipped []string, err error) {
	chosen = make([]*PlanTable, 0, len(plan.Tables))
	for i := range plan.Tables {
		pt := &plan.Tables[i]
		if len(only) > 0 && !slices.Contains(only, pt.Source) {
			continue
		}
		switch {
		case !pt.IsDiffable():
			if len(only) > 0 || pt.Verdict.IsSyncable() {
				skipped = append(skipped, pt.Source.String()+": verdict "+pt.Verdict.String()+" (apply the DDL first)")
			}
			continue
		case ts.Mode == SyncModeRepair && pt.Diff == nil:
			skipped = append(skipped, pt.Source.String()+": repair needs a diff (run the diff first)")
			continue
		case ts.Mode == SyncModeRepair && pt.Diff.IsIdentical():
			continue
		}
		if pt.Chunking == nil {
			var c Chunking
			c, err = DeriveChunking(ctx, src, pt.Source, pt.SortingKey, pt.PartitionKey, pt.Rows, chunkOpts)
			if err != nil {
				return
			}
			pt.Chunking = &c
		}
		t := ts
		pt.Sync = &t
		chosen = append(chosen, pt)
	}
	return
}

// BeginRun names the sync run: the plan's own when it has one and restart is
// false, a new one otherwise. resumed reports which.
func BeginRun(plan *Plan, restart bool, now time.Time) (run SyncRun, resumed bool, err error) {
	if plan.SyncRun != nil && !restart {
		return *plan.SyncRun, true, nil
	}
	var id string
	id, err = gonanoid.New()
	if err != nil {
		err = eh.Errorf("unable to mint a run id: %w", err)
		return
	}
	run = SyncRun{RunId: id, StartedAt: now.UTC()}
	plan.SyncRun = &run
	return
}

// ExpectedRows is the row count a sync of the chosen tables is expected to
// copy, the total a progress bar counts towards.
func ExpectedRows(chosen []*PlanTable) (rows int64) {
	for _, pt := range chosen {
		rows += int64(float64(pt.Rows) * ExpectedCopyFraction(pt))
	}
	return
}

// SyncHooks let a front end follow [SyncStep] table by table.
type SyncHooks struct {
	BeforeTable func(pt *PlanTable)
	// AfterTable runs once a table's report is stored — the moment to save
	// the plan.
	AfterTable func(pt *PlanTable)
}

// SyncStep syncs the chosen tables in order, storing each table's report in
// it and dropping its diff, which described the target before the run. It
// stops at the first table-level error; chunk failures are in the reports.
func SyncStep(ctx context.Context, src ClientI, dst ClientI, chosen []*PlanTable, j *Journal, opts SyncOptions, now func() time.Time, hooks SyncHooks) (failed int, err error) {
	for _, pt := range chosen {
		if hooks.BeforeTable != nil {
			hooks.BeforeTable(pt)
		}
		var rep TableSyncReport
		rep, err = SyncTable(ctx, src, dst, pt, j, opts, now)
		if err != nil {
			err = eb.Build().Str("table", pt.Source.String()).Errorf("unable to sync table: %w", err)
			return
		}
		pt.SyncReport = &rep
		pt.Diff = nil
		failed += rep.Failed + rep.Stale
		if hooks.AfterTable != nil {
			hooks.AfterTable(pt)
		}
	}
	return
}

// JournalPath is where the journal of the plan at planPath lives.
func JournalPath(planPath string) (path string) {
	return planPath + ".journal"
}
