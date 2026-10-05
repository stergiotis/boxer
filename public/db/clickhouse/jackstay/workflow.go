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
// implementations of it. Each step that acts on a saved plan rechecks it
// against the servers first and refuses a stale one; the caller only renders
// and decides where the plan is saved.

// DiscoverBoth reads both inventories at once.
func DiscoverBoth(ctx context.Context, src SourceI, dst QueryI) (s Inventory, d Inventory, err error) {
	s, d, err = both(ctx, func(ctx context.Context, side int) (Inventory, error) {
		if side == 0 {
			inv, e := src.discover(ctx)
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
func PlanStructure(ctx context.Context, src SourceI, dst QueryI, srcEp Endpoint, dstEp Endpoint, sel Selection, old *Plan, now time.Time) (plan Plan, err error) {
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
		plan.CarryOver(old, false)
	}
	return
}

// Recheck rediscovers both servers and restates plan (§SD1, §SD3): fresh is
// the plan as the servers now call for it, carrying plan's chunk layouts,
// diffs and sync state; stale lists what moved. A caller must not act on a
// plan whose stale list is not empty.
func Recheck(ctx context.Context, src SourceI, dst QueryI, plan *Plan, now time.Time) (fresh Plan, stale []string, err error) {
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
	fresh.CarryOver(plan, true)
	return
}

// ErrStale is returned by steps that refuse a plan the servers have moved away
// from.
var ErrStale = eh.Errorf("the plan no longer matches the servers")

// ApplyDDLStep is the confirmation of the Structure step: recheck, run the
// pending DDL through ddl (a client carrying [DDLClientConfig]), and judge
// again. It returns the statements that ran and the plan afterwards; a stale
// plan runs nothing. A table the DDL altered loses its diff and sync state,
// which describe the table before; every other table, and the sync run, keep
// theirs.
func ApplyDDLStep(ctx context.Context, src SourceI, dst QueryI, ddl ExecI, plan *Plan, now time.Time) (applied []string, after Plan, stale []string, err error) {
	var pending Plan
	pending, stale, err = Recheck(ctx, src, dst, plan, now)
	if err != nil || len(stale) > 0 {
		return
	}
	applied, err = ApplyStructure(ctx, ddl, &pending)
	if err != nil {
		return
	}
	after, err = PlanStructure(ctx, src, dst, plan.Source, plan.Target, plan.Selection, nil, now)
	if err != nil {
		return
	}
	if plan.Source != after.Source || plan.Target != after.Target {
		return
	}
	// CarryOver keeps a diff whose copied columns and filter are unchanged,
	// which a created table's are; the DDL that ran is what makes it stale.
	after.CarryOver(plan, true)
	for i := range pending.Tables {
		if len(pending.Tables[i].DDL) == 0 {
			continue
		}
		for j := range after.Tables {
			if t := &after.Tables[j]; t.Source == pending.Tables[i].Source {
				t.Diff, t.Sync, t.SyncReport = nil, nil, nil
			}
		}
	}
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

// notDiffable says why a table in scope is not compared or synced.
func notDiffable(pt *PlanTable) (reason string) {
	if pt.Verdict.IsSyncable() {
		return pt.Source.String() + ": verdict " + pt.Verdict.String() + " (apply the DDL first)"
	}
	s := pt.Source.String() + ": verdict " + pt.Verdict.String()
	if len(pt.Reasons) > 0 {
		s += " (" + pt.Reasons[0] + ")"
	}
	return s
}

// DiffStep is the Differences step: recheck the plan, then diff every
// diffable table in scope in place (§SD4). fresh is the plan to save; a
// stale plan is not diffed.
func DiffStep(ctx context.Context, src SourceI, dst QueryI, plan *Plan, opts DiffOptionsAll, now func() time.Time) (fresh Plan, skipped []string, stale []string, err error) {
	fresh, stale, err = Recheck(ctx, src, dst, plan, now())
	if err != nil || len(stale) > 0 {
		return
	}
	todo := make([]*PlanTable, 0, len(fresh.Tables))
	for i := range fresh.Tables {
		pt := &fresh.Tables[i]
		if len(opts.Only) > 0 && !slices.Contains(opts.Only, pt.Source) {
			continue
		}
		if !pt.IsDiffable() {
			if len(opts.Only) > 0 || pt.Verdict.IsSyncable() {
				skipped = append(skipped, notDiffable(pt))
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

// SyncRequest is what the operator asked of the Sync step.
type SyncRequest struct {
	TableSync
	// Only restricts the step to these source tables; empty syncs every
	// table ready for it.
	Only []datacatalog.TableRef
	// Restart begins a new run instead of resuming the plan's.
	Restart  bool
	Chunking ChunkingOptions
	// Headroom is the pre-flight factor over the estimated bytes the target
	// must have free; zero takes 1.5.
	Headroom float64
}

// SyncPrepared is the Sync step before anything moves: the rechecked plan,
// the tables chosen with their chunk layout and settings, the tables left
// out with the reason, and the target's disks against what would land.
type SyncPrepared struct {
	// Request is what the step was asked; [RunSync] takes Restart from it.
	Request SyncRequest
	Plan    Plan
	Stale   []string
	// Chosen points into Plan.Tables.
	Chosen  []*PlanTable
	Skipped []string
	// Disks is empty when nothing was chosen or the plan is stale.
	Disks []PreflightDisk
	// ExpectedRows is what a progress bar counts towards.
	ExpectedRows int64
}

// PrepareSyncStep rechecks the plan, chooses the tables req can take, derives
// the chunk layout of any that lacks one, and reads the target's disks for
// the pre-flight (§SD5, §SD6). Nothing is written to either server.
func PrepareSyncStep(ctx context.Context, src SourceI, dst QueryI, plan *Plan, req SyncRequest, now time.Time) (prep SyncPrepared, err error) {
	prep.Request = req
	prep.Plan, prep.Stale, err = Recheck(ctx, src, dst, plan, now)
	if err != nil || len(prep.Stale) > 0 {
		return
	}
	prep.Chosen, prep.Skipped, err = PrepareSync(ctx, src, &prep.Plan, req.TableSync, req.Only, req.Chunking)
	if err != nil || len(prep.Chosen) == 0 {
		return
	}
	prep.ExpectedRows = ExpectedRows(prep.Chosen)
	refs := make([]datacatalog.TableRef, 0, len(prep.Chosen))
	for _, pt := range prep.Chosen {
		refs = append(refs, pt.Target)
	}
	var rep DiskReport
	rep, err = ReadDisks(ctx, dst, refs)
	if err != nil {
		return
	}
	headroom := req.Headroom
	if headroom <= 0 {
		headroom = 1.5
	}
	prep.Disks = Preflight(prep.Chosen, &rep, headroom)
	return
}

// PrepareSync chooses the tables in scope that can take ts, with a chunk
// layout and ts set on each. A table that cannot is listed in skipped with the
// reason; a repair has nothing to do on a table whose diff is identical.
func PrepareSync(ctx context.Context, src SourceI, plan *Plan, ts TableSync, only []datacatalog.TableRef, chunkOpts ChunkingOptions) (chosen []*PlanTable, skipped []string, err error) {
	chosen = make([]*PlanTable, 0, len(plan.Tables))
	for i := range plan.Tables {
		pt := &plan.Tables[i]
		if len(only) > 0 && !slices.Contains(only, pt.Source) {
			continue
		}
		switch {
		case !pt.IsDiffable():
			if len(only) > 0 || pt.Verdict.IsSyncable() {
				skipped = append(skipped, notDiffable(pt))
			}
			continue
		case ts.Mode == SyncModeRepair && pt.Diff == nil:
			skipped = append(skipped, pt.Source.String()+": repair needs a diff (run the diff first)")
			continue
		case ts.Mode == SyncModeRepair && pt.Diff.IsIdentical():
			continue
		case ts.Mode != SyncModeFull && src.limits().noSubsets:
			skipped = append(skipped, pt.Source.String()+": "+src.limits().what+" holds whole chunks only, so "+ts.Mode.String()+" is not possible (sync in full)")
			continue
		}
		if pt.Chunking == nil {
			var c Chunking
			c, err = src.deriveChunking(ctx, pt, chunkOpts)
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

// SyncOutcome is what a run left behind.
type SyncOutcome struct {
	Run     SyncRun
	Resumed bool
	// Failed counts the chunks not synced: failed and stale ones.
	Failed int
}

// RunSync runs the sync a [PrepareSyncStep] prepared: it names the run (a new
// one when the prepared request asks for a restart), saves the plan at
// planPath (the journal lives beside it), copies each chosen table, and saves
// the plan again after each table and at the end, so a run stopped part-way
// resumes from what it recorded. Chunk failures are in the tables' reports;
// err is a table-level failure, after which the plan is still saved.
func RunSync(ctx context.Context, src SourceI, dst ClientI, prep *SyncPrepared, planPath string, opts SyncOptions, now func() time.Time) (out SyncOutcome, err error) {
	return RunSyncIn(ctx, src, dst, prep, OsFiles{}, planPath, opts, now)
}

// RunSyncIn is [RunSync] with the plan and its journal kept in files under
// planName. When files is a [LockerI], the plan is locked for the run, and a
// second run of it is refused while the first holds it.
func RunSyncIn(ctx context.Context, src SourceI, dst ClientI, prep *SyncPrepared, files FilesI, planName string, opts SyncOptions, now func() time.Time) (out SyncOutcome, err error) {
	if len(prep.Stale) > 0 {
		err = eb.Build().Int("stale", len(prep.Stale)).Errorf("plan is stale: %w", ErrStale)
		return
	}
	if len(prep.Chosen) == 0 {
		return
	}
	if l, ok := files.(LockerI); ok {
		var unlock func() error
		unlock, err = l.Lock(planName)
		if err != nil {
			return
		}
		defer func() {
			if uerr := unlock(); err == nil {
				err = uerr
			}
		}()
	}
	plan := &prep.Plan
	out.Run, out.Resumed, err = BeginRun(plan, prep.Request.Restart, now())
	if err != nil {
		return
	}
	var j *Journal
	j, err = OpenJournalIn(files, JournalPath(planName), out.Run.RunId)
	if err != nil {
		return
	}
	defer func() { _ = j.Close() }()
	// A resumed run keeps the settings it began under. Refused before the
	// plan is saved, so a refused request leaves no trace in it.
	for _, pt := range prep.Chosen {
		if e, started := j.Started(pt.Source.String()); started {
			if err = checkStart(e, pt); err != nil {
				return
			}
		}
	}
	if err = plan.SaveIn(files, planName); err != nil {
		return
	}
	for _, pt := range prep.Chosen {
		if opts.BeforeTable != nil {
			opts.BeforeTable(pt)
		}
		var rep TableSyncReport
		rep, err = SyncTable(ctx, src, dst, pt, j, opts, now)
		if err != nil {
			err = eb.Build().Str("table", pt.Source.String()).Errorf("unable to sync table: %w", err)
			_ = plan.SaveIn(files, planName)
			return
		}
		pt.SyncReport = &rep
		// The diff described the target before the run.
		pt.Diff = nil
		out.Failed += rep.Failed + rep.Stale
		if err = plan.SaveIn(files, planName); err != nil {
			return
		}
		if opts.AfterTable != nil {
			opts.AfterTable(pt)
		}
	}
	return
}

// JournalPath is where the journal of the plan at planPath lives: beside it,
// under the plan's name with a suffix, which is also its name in a [FilesI].
func JournalPath(planPath string) (path string) {
	return planPath + ".journal"
}
