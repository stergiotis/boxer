package watchbillstore

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/storage/recordstore/rowcas"
)

// The claim, the transitions, the queue read and the sweep are hand-written
// SQL over the generated column names (ADR-0223 §SD3): the record store
// generates no update verb, and this package does not ask it to. Every
// statement here is pinned by claim_test.go.
//
// A section holds one attribute, so its value column is an array of one
// element: a guard reads element 1, a change writes a one-element array.
// The physical names are read out of the generated DDL at init, so a
// change of encoding hints regenerates them rather than silently breaking
// the SQL.

// jobValueColumns maps a job section name to the quoted physical name of
// its value column.
var jobValueColumns = valueColumnsOf(watchbillDDLCreate, "job")

var valueColumnPattern = regexp.MustCompile(`"tv:([A-Za-z0-9]+):value:[^"]*"`)

func valueColumnsOf(ddl string, prefix string) (cols map[string]string) {
	cols = make(map[string]string, 24)
	for _, m := range valueColumnPattern.FindAllStringSubmatch(ddl, -1) {
		if strings.HasPrefix(m[1], prefix) {
			cols[m[1]] = m[0]
		}
	}
	return
}

// col is the quoted physical value column of a job section; it panics on
// an unknown section, which only a typo here can cause.
func col(section string) (name string) {
	name, ok := jobValueColumns[section]
	if !ok {
		panic("watchbillstore: no value column for section " + section)
	}
	return
}

// elem is "<column>[1]": the one value the section holds.
func elem(section string) (expr string) { return col(section) + "[1]" }

func strLit(s string) (lit string) { return marshalling.EscapeString(s) }

func strArr(s string) (lit string) { return "[" + strLit(s) + "]" }

func timeLit(t time.Time) (lit string) {
	return "fromUnixTimestamp64Nano(" + strconv.FormatInt(t.UTC().UnixNano(), 10) + ")"
}

func timeArr(t time.Time) (lit string) { return "[" + timeLit(t) + "]" }

func u32Arr(v uint32) (lit string) { return "[" + strconv.FormatUint(uint64(v), 10) + "]" }

// The settings every statement carries, and the conditions they hold
// under, are rowcas's (ADR-0223 §SD3; the model in
// doc/explanation/watchbill-consistency-model.md): the sequential update
// mode stated on the statement, a delete as a lightweight update, the
// block-position columns on the table, and never a heavyweight mutation.
// Measured (the background note, 2026-09-09): under `async` a twenty-way
// race doubles most rounds; under `sync` none in hundreds, as long as no
// mutation rewrites a part meanwhile.

// AlterJobTableSettingsSQL puts the lightweight-update settings on a job
// table that exists already.
func AlterJobTableSettingsSQL(layout Layout) (sql string) {
	return rowcas.AlterTableSettingsSQL(layout.JobTable())
}

// QueueSQL reads the ids of the jobs a worker may take now, oldest-first
// within priority (ADR-0223 §SD2): queued, due, of one of the kinds and on
// one of the queues, at most limit. Kinds is the worker's handler set and
// queues the set it drains (ADR-0234 §SD3); empty means every one.
func QueueSQL(layout Layout, kinds []string, queues []string, now time.Time, limit int) (sql string) {
	var sb strings.Builder
	sb.WriteString("SELECT " + JobColKey + " FROM " + layout.JobTable() +
		" WHERE " + elem("jobState") + " = " + strLit(StateQueued) +
		" AND " + elem("jobRunAfter") + " <= " + timeLit(now))
	sb.WriteString(inClause("jobKind", kinds))
	sb.WriteString(inClause("jobQueue", queues))
	sb.WriteString(" ORDER BY " + elem("jobPriority") + " ASC, " + elem("jobRunAfter") + " ASC, " + JobColOrder + " ASC")
	if limit > 0 {
		sb.WriteString(" LIMIT " + strconv.Itoa(limit))
	}
	return sb.String()
}

// ClaimSQL is the claim (ADR-0223 §SD3): one conditional update that takes
// a queued, due job for workerRun and counts the attempt. The server
// serialises competing updates of one row, so of N claimants one changes
// the row; the caller reads the row back and holds the job exactly when
// WorkerRun is its own.
func ClaimSQL(layout Layout, id string, workerRun string, now time.Time) (sql string) {
	return rowcas.UpdateSQL(layout.JobTable(),
		[]rowcas.Set{
			{Column: col("jobState"), Expr: strArr(StateRunning)},
			{Column: col("jobWorkerRun"), Expr: strArr(workerRun)},
			{Column: col("jobAttempt"), Expr: "[" + elem("jobAttempt") + " + 1]"},
		},
		JobColKey+" = "+strLit(id)+
			" AND "+elem("jobState")+" = "+strLit(StateQueued)+
			" AND "+elem("jobRunAfter")+" <= "+timeLit(now))
}

// ReadJobPredicate is the ScanOpts.ExtraPredicate that reads one job.
func ReadJobPredicate(id string) (pred string) {
	return JobColKey + " = " + strLit(id)
}

// HeldPredicate is the ScanOpts.ExtraPredicate for the jobs in one of the
// states — running, or running with a cancel requested — held by workerRun;
// an empty run means every holder. What a worker re-reads to notice a
// cancel, and what the sweep reads once liveness has named the stale runs.
func HeldPredicate(states []string, workerRun string) (pred string) {
	var sb strings.Builder
	sb.WriteString(elem("jobState") + " IN (")
	for i, s := range states {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(strLit(s))
	}
	sb.WriteString(")")
	if workerRun != "" {
		sb.WriteString(" AND " + elem("jobWorkerRun") + " = " + strLit(workerRun))
	}
	return sb.String()
}

// Transition is one guarded change of a job row (ADR-0223 §SD2). From is
// the state the row must be in, and WorkerRun — when set — the run it must
// be held by, so a stale actor changes nothing. To is the new state; the
// optional fields are what else the change writes.
type Transition struct {
	ID        string
	From      []string
	WorkerRun string
	To        string
	// SetWorkerRun writes the worker-run column; an empty string with the
	// flag set clears it.
	SetWorkerRun  bool
	NewWorkerRun  string
	RunAfter      *time.Time
	FinishedAt    *time.Time
	LastError     *string
	ResetAttempts bool
}

// TransitionSQL renders t as one conditional update.
func TransitionSQL(layout Layout, t Transition) (sql string) {
	sets := []rowcas.Set{{Column: col("jobState"), Expr: strArr(t.To)}}
	if t.SetWorkerRun {
		sets = append(sets, rowcas.Set{Column: col("jobWorkerRun"), Expr: strArr(t.NewWorkerRun)})
	}
	if t.RunAfter != nil {
		sets = append(sets, rowcas.Set{Column: col("jobRunAfter"), Expr: timeArr(*t.RunAfter)})
	}
	if t.FinishedAt != nil {
		sets = append(sets, rowcas.Set{Column: col("jobFinishedAt"), Expr: timeArr(*t.FinishedAt)})
	}
	if t.LastError != nil {
		sets = append(sets, rowcas.Set{Column: col("jobLastError"), Expr: strArr(*t.LastError)})
	}
	if t.ResetAttempts {
		sets = append(sets, rowcas.Set{Column: col("jobAttempt"), Expr: u32Arr(0)})
	}
	var where strings.Builder
	where.WriteString(JobColKey + " = " + strLit(t.ID))
	where.WriteString(inClause("jobState", t.From))
	if t.WorkerRun != "" {
		where.WriteString(" AND " + elem("jobWorkerRun") + " = " + strLit(t.WorkerRun))
	}
	return rowcas.UpdateSQL(layout.JobTable(), sets, where.String())
}

// ExpireSQL deletes the rows that left the queue before cutoff (ADR-0223
// §SD4): a lightweight DELETE, since a TTL on a column the update rewrites
// is not a contract worth relying on.
func ExpireSQL(layout Layout, cutoff time.Time) (sql string) {
	where := strings.TrimPrefix(inClause("jobState", FinalStates), " AND ") +
		" AND " + elem("jobFinishedAt") + " < " + timeLit(cutoff)
	return rowcas.DeleteSQL(layout.JobTable(), where)
}

// inClause is " AND <section>[1] IN (...)" over values, or nothing when
// there are none — a filter that is absent matches everything.
func inClause(section string, values []string) (clause string) {
	if len(values) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(" AND " + elem(section) + " IN (")
	for i, v := range values {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(strLit(v))
	}
	sb.WriteString(")")
	return sb.String()
}
