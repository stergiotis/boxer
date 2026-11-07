package jackstay

import (
	"time"

	jk "github.com/stergiotis/boxer/public/db/clickhouse/jackstay"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/fsmops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opfsm"
)

// The window's operations catalog (ADR-0269): the plan's phase, as the state
// chip draws it — plan_state and plan_machine, the operations every app's
// mounted machine offers — and four bounded queries over the plan the
// window holds: its tables with their verdicts, one table's DDL and notes,
// the last comparison and the last sync. Nothing here moves the plan, and
// there is no command: every step that writes to a server stays behind the
// window's own confirmation (ADR-0259), which is the person's act.

const (
	opGetPlan    = "get_plan"
	opGetTable   = "get_table"
	opGetCompare = "get_compare"
	opGetSync    = "get_sync"
)

// Result bounds: what one inline result may carry.
const (
	opsMaxTables     = 200
	opsDefaultTables = 50
	opsMaxLines      = 16
	opsMaxLineLen    = 512
)

const (
	noPlanReason       = "no plan is open; discover the servers and plan the structure at the window, or open a plan, and get_plan lists its tables"
	noCompareReason    = "no table has been compared; Compare content on the Differences page runs the comparison"
	noSyncReason       = "no sync has run for this plan; Start on the Sync page runs one, behind the window's confirmation"
	unknownTableReason = "no table of that name in the plan; get_plan lists them as database and name"
)

// opsSnap is what queries read: the plan's tables as the window last
// installed them, with the sections a comparison or a sync left, and the
// window's record of the sync. The mounted machine is read on the render
// goroutine and not from here.
type opsSnap struct {
	plan      *planView
	phase     string
	stale     []string
	note      string
	err       string
	syncing   bool
	comparing bool
	syncStart time.Time
	syncEnd   time.Time
	stopped   bool
	rows      int64
	bytes     int64
}

// planView is the plan's content copied for the queries, so a query off
// the render goroutine never reads a plan the frame replaces. installPlan
// replaces the plan whole and raises planRev; the copy is taken once per
// installed plan and shared by every snapshot after.
type planView struct {
	ptr     *jk.Plan
	rev     uint64
	name    string
	created time.Time
	source  string
	target  string
	dbDDL   int
	tables  []jk.PlanTable
}

func (inst *App) opsPlanView() (v *planView) {
	if inst.plan == nil {
		inst.opsPlan = nil
		return nil
	}
	if cur := inst.opsPlan; cur != nil && cur.ptr == inst.plan && cur.rev == inst.planRev {
		return cur
	}
	p := inst.plan
	v = &planView{ptr: p, rev: inst.planRev, name: inst.planName, created: p.CreatedAt,
		source: hostLabel(p.Source.URL), target: hostLabel(p.Target.URL), dbDDL: len(p.DatabaseDDL),
		tables: append([]jk.PlanTable(nil), p.Tables...)}
	inst.opsPlan = v
	return
}

func (inst *App) opsSnapshot() (sn opsSnap) {
	sn.plan = inst.opsPlanView()
	sn.phase = inst.phaseMachine.Current().String()
	sn.stale = inst.stale
	sn.note, sn.err = inst.note, inst.lastError
	sn.syncing, sn.comparing = inst.syncJob.Running(), inst.diffJob.Running()
	sn.syncStart, sn.syncEnd, sn.stopped = inst.syncStarted, inst.syncFinished, inst.syncStopped
	sn.rows, sn.bytes = inst.rows.Load(), inst.bytes.Load()
	return
}

// GetPlanArgs bounds the tables returned.
type GetPlanArgs struct {
	Limit int `json:",omitzero" desc:"tables to return, in the plan's order; default 50, at most 200"`
}

// PlanRow is one table of the plan as the Structure page lists it.
type PlanRow struct {
	Database       string `desc:"the source database"`
	Name           string `desc:"the source table"`
	TargetDatabase string `desc:"where it lands on the target"`
	TargetName     string `desc:"its name on the target"`
	Engine         string `desc:"the source engine"`
	Rows           uint64 `desc:"rows on the source when planned"`
	Bytes          uint64 `desc:"bytes on the source when planned"`
	Verdict        string `desc:"the structure verdict: identical, narrower, create, extend, unsupported or incompatible"`
	DDLPending     bool   `desc:"the target needs DDL before this table can be compared or synced"`
	Filter         string `desc:"the row filter the plan applies, empty for none"`
	Compared       bool   `desc:"a comparison left a result for this table; get_compare has it"`
	Synced         bool   `desc:"a sync left a report for this table; get_sync has it"`
}

// PlanResult is get_plan's result.
type PlanResult struct {
	Name               string    `desc:"the plan file in the window's data area; empty before the first save"`
	CreatedAt          time.Time `desc:"when the plan was created"`
	Source             string    `desc:"the source server, as the window labels it"`
	Target             string    `desc:"the target server, as the window labels it"`
	Phase              string    `desc:"the plan's phase, as plan_state reports it"`
	Matched            int       `desc:"tables in the plan"`
	Tables             []PlanRow `desc:"the first limit tables, in the plan's order"`
	Truncated          bool      `desc:"tables were left out by the limit"`
	DatabaseDDLPending int       `desc:"database statements the target needs"`
	Stale              []string  `desc:"what moved under the plan, when the servers did; empty otherwise"`
	Note               string    `desc:"the status line's last outcome"`
	Error              string    `desc:"the status line's last error, empty when none"`
}

// GetTableArgs names one table of the plan by its source.
type GetTableArgs struct {
	Database string `desc:"the source database, as get_plan lists it"`
	Name     string `desc:"the source table, as get_plan lists it"`
}

// TableResult is get_table's result: the Structure page's detail of one
// table.
type TableResult struct {
	Table        PlanRow     `desc:"the table as get_plan lists it"`
	SortingKey   string      `desc:"the source sorting key"`
	PartitionKey string      `desc:"the source partition key"`
	TargetEngine string      `desc:"the target's engine when it exists, empty otherwise"`
	Reasons      []string    `desc:"why the verdict, at most 16 lines"`
	Notes        []string    `desc:"the planner's notes, at most 16 lines"`
	DDL          []string    `desc:"the statements that would bring the target in line, at most 16; the window runs them behind a confirmation, not this catalog"`
	Leeway       bool        `desc:"the table is a leeway facts table"`
	Compare      *CompareRow `desc:"the last comparison of this table, when one ran"`
	Sync         *SyncRow    `desc:"the last sync report of this table, when one ran"`
}

// GetCompareArgs bounds the tables returned.
type GetCompareArgs struct {
	Limit int `json:",omitzero" desc:"compared tables to return, in the plan's order; default 50, at most 200"`
}

// CompareRow is one table's result of the content diff.
type CompareRow struct {
	Database        string    `desc:"the source database"`
	Name            string    `desc:"the source table"`
	ComputedAt      time.Time `desc:"when the comparison finished"`
	Identical       bool      `desc:"no chunk differed"`
	Final           bool      `desc:"the differing leaves were resolved row by row"`
	MaybeSpurious   bool      `desc:"the sides were read at different times and the difference may be a write in between"`
	SourceRows      uint64    `desc:"rows on the source"`
	TargetRows      uint64    `desc:"rows on the target"`
	Chunks          uint64    `desc:"chunks compared"`
	IdenticalChunks uint64    `desc:"of them, identical by digest"`
	DifferingChunks int       `desc:"chunks whose digests differed"`
	Missing         uint64    `desc:"rows the target lacks, where resolved"`
	Extra           uint64    `desc:"rows the target has and the source lacks, where resolved"`
	Changed         uint64    `desc:"rows that differ, where resolved"`
}

// CompareResult is get_compare's result.
type CompareResult struct {
	ComputedAt time.Time    `desc:"the latest comparison's time"`
	Running    bool         `desc:"a comparison runs now; the rows are the previous one's"`
	Compared   int          `desc:"tables with a comparison"`
	Differing  int          `desc:"of them, differing"`
	Tables     []CompareRow `desc:"the first limit compared tables, in the plan's order"`
	Truncated  bool         `desc:"tables were left out by the limit"`
	Skipped    []string     `desc:"tables the last comparison or sync left out, with the reason, at most 16"`
}

// SyncRow is one table's sync report.
type SyncRow struct {
	Database   string    `desc:"the source database"`
	Name       string    `desc:"the source table"`
	RunId      string    `desc:"the run the report belongs to"`
	FinishedAt time.Time `desc:"when the table's sync finished"`
	Copied     int       `desc:"chunks copied and verified"`
	Done       int       `desc:"chunks the journal already held as done"`
	Identical  int       `desc:"chunks found identical and not copied"`
	Stale      int       `desc:"chunks that moved under the sync and were not copied"`
	Failed     int       `desc:"chunks that failed"`
	Rows       uint64    `desc:"rows copied"`
	Bytes      uint64    `desc:"bytes on the wire"`
	Problems   []string  `desc:"what went wrong, at most 16 lines"`
}

// SyncResult is get_sync's result: the window's record of the last run and
// every table's report.
type SyncResult struct {
	Running    bool      `desc:"the sync runs now; rows and bytes count up"`
	StartedAt  time.Time `desc:"when the last run started"`
	FinishedAt time.Time `desc:"when it ended; zero while it runs"`
	Stopped    bool      `desc:"the last run ended without a result: failed or cancelled"`
	Rows       int64     `desc:"rows landed by the last run"`
	Bytes      int64     `desc:"bytes on the wire in the last run"`
	Unsynced   int       `desc:"chunks the reports count as failed or stale"`
	Reported   int       `desc:"tables with a sync report"`
	Tables     []SyncRow `desc:"the first limit reported tables, in the plan's order"`
	Truncated  bool      `desc:"tables were left out by the limit"`
}

// GetSyncArgs bounds the tables returned.
type GetSyncArgs struct {
	Limit int `json:",omitzero" desc:"reported tables to return, in the plan's order; default 50, at most 200"`
}

func boundedLimit(limit int) (n int, err error) {
	switch {
	case limit < 0:
		err = app.RefuseOperation("limit is not negative")
	case limit == 0:
		n = opsDefaultTables
	case limit > opsMaxTables:
		n = opsMaxTables
	default:
		n = limit
	}
	return
}

func cutLines(lines []string) (out []string) {
	if len(lines) == 0 {
		return nil
	}
	n := min(len(lines), opsMaxLines)
	out = make([]string, 0, n)
	for _, l := range lines[:n] {
		out = append(out, appops.CutText(l, opsMaxLineLen))
	}
	return
}

func planRowOf(t *jk.PlanTable) (r PlanRow) {
	r = PlanRow{Database: t.Source.Database, Name: t.Source.Name, TargetDatabase: t.Target.Database, TargetName: t.Target.Name,
		Engine: t.Engine, Rows: t.Rows, Bytes: t.Bytes, Verdict: t.Verdict.String(), DDLPending: len(t.DDL) > 0,
		Filter: t.Filter, Compared: t.Diff != nil, Synced: t.SyncReport != nil}
	return
}

func compareRowOf(t *jk.PlanTable) (r *CompareRow) {
	d := t.Diff
	if d == nil {
		return nil
	}
	r = &CompareRow{Database: t.Source.Database, Name: t.Source.Name, ComputedAt: d.ComputedAt, Identical: d.IsIdentical(),
		Final: d.Final, MaybeSpurious: d.MaybeSpurious, SourceRows: d.SrcRows, TargetRows: d.DstRows, Chunks: d.Chunks,
		IdenticalChunks: d.IdenticalChunks, DifferingChunks: len(d.Differing), Missing: d.Missing, Extra: d.Extra, Changed: d.Changed}
	return
}

func syncRowOf(t *jk.PlanTable) (r *SyncRow) {
	s := t.SyncReport
	if s == nil {
		return nil
	}
	r = &SyncRow{Database: t.Source.Database, Name: t.Source.Name, RunId: s.RunId, FinishedAt: s.FinishedAt,
		Copied: s.Copied, Done: s.Done, Identical: s.Identical, Stale: s.Stale, Failed: s.Failed, Rows: s.Rows, Bytes: s.Bytes,
		Problems: cutLines(s.Problems)}
	return
}

var ops = func() (s *appops.Set[*App, opsSnap]) {
	s = appops.NewSet((*App).opsSnapshot)
	fsmops.Mount(s, "plan", "the plan's phase, from discovering the servers to the sync", func(inst *App) opfsm.SourceI {
		if inst.phaseMachine == nil {
			return nil
		}
		return inst.phaseMachine
	}, fsmops.Options{History: 16})

	appops.Query(s, app.OperationSpec{Name: opGetPlan, Version: 1,
		Summary: "list the plan's tables with their verdicts and whether the target needs DDL, at most 200; no command applies the DDL, which is the person's confirmed click on the Structure page",
		Reads:   []string{"plan_state"}, Agents: true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetPlanArgs) (out PlanResult, err error) {
			limit, err := boundedLimit(in.Limit)
			if err != nil {
				return
			}
			p := sn.plan
			if p == nil {
				err = app.RefuseOperation(noPlanReason)
				return
			}
			out = PlanResult{Name: p.name, CreatedAt: p.created, Source: p.source, Target: p.target, Phase: sn.phase, Matched: len(p.tables),
				DatabaseDDLPending: p.dbDDL, Stale: cutLines(sn.stale), Note: appops.CutText(sn.note, opsMaxLineLen),
				Error: appops.CutText(sn.err, opsMaxLineLen)}
			for i := range p.tables {
				if len(out.Tables) == limit {
					out.Truncated = true
					break
				}
				out.Tables = append(out.Tables, planRowOf(&p.tables[i]))
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetTable, Version: 1,
		Summary: "read one table of the plan by its source database and name: the verdict's reasons, the planner's notes, the DDL the target needs, and its last comparison and sync report; no command runs the DDL (ADR-0259)",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetTableArgs) (out TableResult, err error) {
			p := sn.plan
			if p == nil {
				err = app.RefuseOperation(noPlanReason)
				return
			}
			for i := range p.tables {
				t := &p.tables[i]
				if t.Source.Database != in.Database || t.Source.Name != in.Name {
					continue
				}
				out = TableResult{Table: planRowOf(t), SortingKey: t.SortingKey, PartitionKey: t.PartitionKey, TargetEngine: t.TargetEngine,
					Reasons: cutLines(t.Reasons), Notes: cutLines(t.Notes), DDL: cutLines(t.DDL), Leeway: t.Leeway,
					Compare: compareRowOf(t), Sync: syncRowOf(t)}
				return
			}
			err = app.RefuseOperation(unknownTableReason)
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetCompare, Version: 1,
		Summary: "read the last content comparison: per compared table, the chunks that differed and the rows missing, extra or changed where resolved, at most 200 tables; no command runs a comparison, which is Compare content on the Differences page",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetCompareArgs) (out CompareResult, err error) {
			limit, err := boundedLimit(in.Limit)
			if err != nil {
				return
			}
			p := sn.plan
			if p == nil {
				err = app.RefuseOperation(noPlanReason)
				return
			}
			out.Running = sn.comparing
			for i := range p.tables {
				r := compareRowOf(&p.tables[i])
				if r == nil {
					continue
				}
				out.Compared++
				if !r.Identical {
					out.Differing++
				}
				if r.ComputedAt.After(out.ComputedAt) {
					out.ComputedAt = r.ComputedAt
				}
				if len(out.Tables) == limit {
					out.Truncated = true
					continue
				}
				out.Tables = append(out.Tables, *r)
			}
			if out.Compared == 0 && !out.Running {
				err = app.RefuseOperation(noCompareReason)
				return
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetSync, Version: 1,
		Summary: "read the last sync: when it ran, whether it stopped, the rows and bytes it moved, and every table's report, at most 200; no command starts a sync, which is Start on the Sync page behind the window's confirmation (ADR-0259)",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetSyncArgs) (out SyncResult, err error) {
			limit, err := boundedLimit(in.Limit)
			if err != nil {
				return
			}
			p := sn.plan
			if p == nil {
				err = app.RefuseOperation(noPlanReason)
				return
			}
			out = SyncResult{Running: sn.syncing, StartedAt: sn.syncStart, Stopped: sn.stopped, Rows: sn.rows, Bytes: sn.bytes}
			if !sn.syncing {
				out.FinishedAt = sn.syncEnd
			}
			for i := range p.tables {
				r := syncRowOf(&p.tables[i])
				if r == nil {
					continue
				}
				out.Reported++
				out.Unsynced += r.Failed + r.Stale
				if len(out.Tables) == limit {
					out.Truncated = true
					continue
				}
				out.Tables = append(out.Tables, *r)
			}
			if out.Reported == 0 && !out.Running && out.StartedAt.IsZero() {
				err = app.RefuseOperation(noSyncReason)
				return
			}
			return
		})
	return
}()

// Operations serves the catalog for this window.
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

var _ app.OperationsAppI = (*App)(nil)
