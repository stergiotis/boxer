package mdnotebook

import (
	"errors"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/trivialsql"
)

// ErrNotPresentation is the refusal of a statement that computes, filters
// or orders: a presentation cell picks columns and names them, and a record
// does the rest (ADR-0371 §SD6). So every cell is answered in process, on
// every host, from what is stored.
var ErrNotPresentation = errors.New("not a presentation statement")

// Refusal is a refused statement and what in it was refused. Found is what
// a cell shows in place of a result. It is [ErrNotPresentation] to
// errors.Is.
type Refusal struct {
	Found string
}

func (inst *Refusal) Error() string { return ErrNotPresentation.Error() + ": " + inst.Found }

func (inst *Refusal) Is(target error) bool { return target == ErrNotPresentation }

// whereItBelongs says, for the clauses a writer is most likely to reach
// for, where the notebook wants them instead.
var whereItBelongs = map[string]string{
	"WHERE":    "WHERE: a record filters, a cell selects",
	"ORDER BY": "ORDER BY: a record orders, a cell selects",
	"GROUP BY": "GROUP BY: a record computes, a cell selects",
	"JOIN":     "JOIN: a record joins, a cell selects",
}

// Member is one named relation of a presentation statement's WITH list: a
// panel's further channel (ADR-0371 §SD6).
type Member struct {
	Name   string
	Record arrow.RecordBatch
}

// Result is what a presentation statement answers: its own relation and
// the members of its WITH list, in order.
type Result struct {
	Main    arrow.RecordBatch
	Members []Member
	// Reads names the records the statement read, in the order it first
	// read them: what makes the answer stale when a record is.
	Reads []string
}

// Release releases the result's records.
func (inst *Result) Release() {
	if inst == nil {
		return
	}
	if inst.Main != nil {
		inst.Main.Release()
		inst.Main = nil
	}
	for i := range inst.Members {
		inst.Members[i].Record.Release()
	}
	inst.Members = nil
}

// noTables is the registry a presentation statement is answered against:
// it names no keelson table, so a cell reads the notebook's records and
// nothing of the process it is read in.
var noTables = introspect.NewRegistry()

// Evaluate answers a presentation statement over the notebook's records.
// The statement is one the trivial evaluator answers (ADR-0290, ADR-0371
// §SD7): a SELECT, optionally with a WITH list of further SELECTs, each
// reading one record or a member before it, whose list holds `*`, bare
// columns under optional aliases and constants. The answer shares the
// records' columns; nothing is copied or computed per row.
//
// A statement outside that shape is a [Refusal]. Any other error is a
// statement that is wrong where it stands: a record or a column that does
// not exist, a member named as a record is.
func Evaluate(statement string, records map[string]arrow.RecordBatch) (res *Result, err error) {
	if records == nil {
		records = map[string]arrow.RecordBatch{}
	}
	ans, err := trivialsql.ReadTables(noTables, statement, nil, records)
	if err != nil {
		var r *trivialsql.RefusalError
		switch {
		case errors.As(err, &r):
			found := r.Found
			if hint, known := whereItBelongs[found]; known {
				found = hint
			}
			return nil, &Refusal{Found: found}
		case errors.Is(err, trivialsql.ErrNeedsClickHouse):
			// A statement that does not parse.
			return nil, &Refusal{Found: strings.TrimPrefix(err.Error(), trivialsql.ErrNeedsClickHouse.Error()+": ")}
		}
		return nil, err
	}
	res = &Result{Main: ans.Main, Reads: ans.Tables}
	for _, w := range ans.With {
		res.Members = append(res.Members, Member{Name: w.Name, Record: w.Batch})
	}
	return res, nil
}
