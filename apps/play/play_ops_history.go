package play

// list_history: the History pane's session half for an agent — the runs of
// this window, newest first, each with the SQL it shipped, the buffer it
// came from when that differs, and the signal values it sent. Restoring an
// entry stays set_sql and set_signal, as the pane's Restore button does
// through the same commands (ADR-0270 §SD6). The durable half, the captured
// runs on boxer.facts, is not read here.

import (
	"maps"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

const opListHistory = "list_history"

// Bounds on list_history's reading.
const (
	historyMaxRuns     = 20
	historyMaxSql      = 1 << 10
	historyMaxSignal   = 200
	historyMaxBytes    = 16 << 10
	historyMaxErrBytes = 512
)

// HistoryArgs is list_history's argument.
type HistoryArgs struct {
	Offset int32 `json:",omitzero" desc:"how many of the newest runs to skip; 0 when left out"`
	Limit  int32 `json:",omitzero" desc:"how many runs, at most 20; 20 when left out"`
}

// HistoryRun is one run of the session.
type HistoryRun struct {
	Index     int32  `desc:"its place, 0 for the newest run; offset reads on from it"`
	Executed  string `desc:"when it ran, UTC"`
	ElapsedMs int64  `desc:"how long it took, in milliseconds"`
	Rows      int64  `desc:"the rows it returned"`
	Error     string `json:",omitzero" desc:"its error, when it failed"`
	Sql       string `desc:"the SQL it shipped, cut at 1 KiB"`
	// Buffer is set only when a buffer of several statements ran one.
	Buffer       string            `json:",omitzero" desc:"the buffer it came from, when that was more than the SQL it shipped; set_sql this to restore it"`
	SqlTruncated bool              `json:",omitzero" desc:"true when the cut shortened sql or buffer"`
	Signals      map[string]string `json:",omitzero" desc:"the signal values it sent beside the SQL; set_signal each to restore them"`
}

// HistoryList is list_history's result.
type HistoryList struct {
	Runs      []HistoryRun `desc:"the runs, newest first"`
	Total     int32        `desc:"how many runs the session keeps"`
	More      bool         `desc:"true when older runs follow; offset reads on"`
	Truncated bool         `json:",omitzero" desc:"true when the 16 KiB bound left out runs this page would have held"`
}

func addHistoryOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	// Untrusted: entries quote buffers, signal values and server errors.
	appops.Query(s, app.OperationSpec{Name: opListHistory, Version: 1,
		Summary: "list this window's runs, newest first: when, rows or error, the SQL each shipped and the signal values it sent",
		Reads:   []string{opsResResult}, Agents: true, Untrusted: true,
		Follows: []string{"restoring a run is set_sql with its buffer (or sql) and set_signal for each of its signals, then run"}},
		func(sn opsSnap, in HistoryArgs) (out HistoryList, err error) {
			if !sn.mounted || sn.graph == nil {
				return out, app.RefuseOperation("the window has not mounted")
			}
			return listHistory(sn.graph.MainHistory(), in)
		})
}

func listHistory(hist []HistoryEntry, in HistoryArgs) (out HistoryList, err error) {
	if in.Offset < 0 || in.Limit < 0 {
		err = app.RefuseOperation("offset and limit are counts, 0 or more")
		return
	}
	limit := int(in.Limit)
	if limit == 0 || limit > historyMaxRuns {
		limit = historyMaxRuns
	}
	out.Total = int32(len(hist))
	budget := historyMaxBytes
	for i := int(in.Offset); i < len(hist); i++ {
		if len(out.Runs) == limit {
			out.More = true
			break
		}
		e := hist[len(hist)-1-i]
		r := HistoryRun{Index: int32(i), Executed: e.Executed.UTC().Format(time.DateTime + ".000"),
			ElapsedMs: e.Elapsed.Milliseconds(), Rows: e.NumRows, Error: truncateBytes(e.ErrorText, historyMaxErrBytes),
			Sql: truncateBytes(e.SQL, historyMaxSql), Buffer: truncateBytes(e.Buffer, historyMaxSql)}
		r.SqlTruncated = len(r.Sql) < len(e.SQL) || len(r.Buffer) < len(e.Buffer)
		if len(e.SigParams) > 0 {
			r.Signals = maps.Clone(e.SigParams)
			for k, v := range r.Signals {
				r.Signals[k] = truncateBytes(v, historyMaxSignal)
			}
		}
		size := len(r.Sql) + len(r.Buffer) + len(r.Error) + 96
		for k, v := range r.Signals {
			size += len(k) + len(v) + 8
		}
		if size > budget && len(out.Runs) > 0 {
			out.Truncated, out.More = true, true
			break
		}
		budget -= size
		out.Runs = append(out.Runs, r)
	}
	return
}
