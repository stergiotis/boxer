package watchbillstore

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
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

// updateSettings is what every update of the job table carries: the
// server's sequential mode, stated rather than left to the default's
// dependency analysis. Measured (the background note, 2026-09-09): under
// `async` a twenty-way race doubles most rounds; under `auto` and `sync`
// none in hundreds — as long as no heavyweight mutation runs on the table
// meanwhile, which is why [ExpireSQL] deletes as a lightweight update and
// nothing here issues an ALTER that rewrites parts.
const updateSettings = " SETTINGS update_parallel_mode='sync'"

// deleteSettings makes the expiry a patch-part delete rather than the
// default heavyweight mutation, so it serialises with the updates.
const deleteSettings = " SETTINGS lightweight_delete_mode='lightweight_update'"

// AlterJobTableSettingsSQL puts the lightweight-update settings on a job
// table that exists already.
func AlterJobTableSettingsSQL(layout Layout) (sql string) {
	return fmt.Sprintf(jobTableSettingsAlter, layout.JobTable())
}

// QueueSQL reads the ids of the jobs a worker may take now, oldest-first
// within priority (ADR-0223 §SD2): queued, due, of one of the kinds, at
// most limit. Kinds is the worker's handler set; empty means every kind.
func QueueSQL(layout Layout, kinds []string, now time.Time, limit int) (sql string) {
	var sb strings.Builder
	sb.WriteString("SELECT " + JobColKey + " FROM " + layout.JobTable() +
		" WHERE " + elem("jobState") + " = " + strLit(StateQueued) +
		" AND " + elem("jobRunAfter") + " <= " + timeLit(now))
	if len(kinds) > 0 {
		sb.WriteString(" AND " + elem("jobKind") + " IN (")
		for i, k := range kinds {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(strLit(k))
		}
		sb.WriteString(")")
	}
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
	return "UPDATE " + layout.JobTable() +
		" SET " + col("jobState") + " = " + strArr(StateRunning) +
		", " + col("jobWorkerRun") + " = " + strArr(workerRun) +
		", " + col("jobAttempt") + " = [" + elem("jobAttempt") + " + 1]" +
		" WHERE " + JobColKey + " = " + strLit(id) +
		" AND " + elem("jobState") + " = " + strLit(StateQueued) +
		" AND " + elem("jobRunAfter") + " <= " + timeLit(now) + updateSettings
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
	var sb strings.Builder
	sb.WriteString("UPDATE " + layout.JobTable() + " SET " + col("jobState") + " = " + strArr(t.To))
	if t.SetWorkerRun {
		sb.WriteString(", " + col("jobWorkerRun") + " = " + strArr(t.NewWorkerRun))
	}
	if t.RunAfter != nil {
		sb.WriteString(", " + col("jobRunAfter") + " = " + timeArr(*t.RunAfter))
	}
	if t.FinishedAt != nil {
		sb.WriteString(", " + col("jobFinishedAt") + " = " + timeArr(*t.FinishedAt))
	}
	if t.LastError != nil {
		sb.WriteString(", " + col("jobLastError") + " = " + strArr(*t.LastError))
	}
	if t.ResetAttempts {
		sb.WriteString(", " + col("jobAttempt") + " = " + u32Arr(0))
	}
	sb.WriteString(" WHERE " + JobColKey + " = " + strLit(t.ID))
	if len(t.From) > 0 {
		sb.WriteString(" AND " + elem("jobState") + " IN (")
		for i, s := range t.From {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(strLit(s))
		}
		sb.WriteString(")")
	}
	if t.WorkerRun != "" {
		sb.WriteString(" AND " + elem("jobWorkerRun") + " = " + strLit(t.WorkerRun))
	}
	sb.WriteString(updateSettings)
	return sb.String()
}

// ExpireSQL deletes the rows that left the queue before cutoff (ADR-0223
// §SD4): a lightweight DELETE, since a TTL on a column the update rewrites
// is not a contract worth relying on.
func ExpireSQL(layout Layout, cutoff time.Time) (sql string) {
	var sb strings.Builder
	sb.WriteString("DELETE FROM " + layout.JobTable() + " WHERE " + elem("jobState") + " IN (")
	for i, s := range FinalStates {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(strLit(s))
	}
	sb.WriteString(") AND " + elem("jobFinishedAt") + " < " + timeLit(cutoff) + deleteSettings)
	return sb.String()
}
