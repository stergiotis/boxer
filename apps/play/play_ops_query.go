package play

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

// play_ops_query.go: query, which does what set_sql, run and sample_rows do
// one after the other, in one call. The buffer, the run's checks, the
// result and the rows are theirs; only the number of calls differs.

const opQuery = "query"

// QueryArgs is query's argument.
type QueryArgs struct {
	Sql string `desc:"the SQL to run; it replaces the SQL buffer, as set_sql does"`
	// Statement picks one statement when the SQL holds several, since the
	// person's caret says nothing about text the agent just wrote.
	Statement int32    `json:",omitzero" desc:"for SQL of several statements, the one to run, from 1, shipped with the SET prelude; required when there are several"`
	Limit     uint32   `json:",omitzero" desc:"how many of the result's first rows to answer with, at most 50; 50 when left out"`
	Fields    []string `json:",omitzero" desc:"the columns to answer with, by name, by gloss label or, for a leeway result, by handle (section:column); every column when left out"`
}

// QueryResult is query's result.
type QueryResult struct {
	// ResultId is the id a citation and check name, at the top level
	// where the chat reads a result id.
	ResultId uint64      `desc:"the result the run produced, as describe_result and sample_rows report it and check and cite take it; 0 when the run produced none"`
	Result   ResultState `desc:"the main result once the run ended, as run answers: its phase, row count, error, and a notice when the run did not happen"`
	Sample   *SampleRows `json:",omitzero" desc:"the result's first rows, as sample_rows reads them; absent when the run produced no rows"`
	// SampleNote says why rows are absent from a result that has some.
	SampleNote string `json:",omitzero" desc:"why no rows came with a result that has some: a column of fields the result does not have, say"`
}

// queryRun is a query call in flight: the main result before it, which the
// result it settles with must differ from, and the rows it asked for.
type queryRun struct {
	before ResultID
	args   QueryArgs
}

// queryRunsMax bounds the calls remembered; a call the host ended without
// settling (its window closed) leaves its entry behind.
const queryRunsMax = 8

// addQueryOp declares query; playOps' initializer calls it.
func addQueryOp(playOps *appops.Set[*PlayLauncher, opsSnap]) {
	appops.Command(playOps, app.OperationSpec{Name: opQuery, Version: 1,
		Summary: "replace the buffer with SQL, run it under the agent limits and answer with the result's first rows: set_sql, run and sample_rows in one call",
		Effect:  app.OperationEffectRun, Reads: []string{opsResParams, opsResSignals}, Writes: []string{opsResSql, opsResResult},
		Agents: true, Untrusted: true, Async: true,
		Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance,
		Follows: []string{"the buffer holds the SQL afterwards, and the result replaces the main result, as set_sql and run leave them",
			"a run the checks refuse is refused at the call and leaves the buffer as it was",
			"check and cite take result_id; sample_rows reads rows past the first 50, with result_id to make sure they are this result's"}},
		func(inst *PlayLauncher, call app.OperationCall, in QueryArgs) (out QueryResult, err error) {
			p := inst.inner
			if p == nil {
				return out, app.RefuseOperation("the window has not mounted")
			}
			if call.OnBehalfOf == nil {
				return out, app.RefuseOperation("query is an agent's call, and carries its context; the person writes in the editor and runs")
			}
			if strings.TrimSpace(in.Sql) == "" {
				return out, app.RefuseOperation("the SQL is empty")
			}
			if err = p.refuseQuery(call.OnBehalfOf, in); err != nil {
				return out, err
			}
			before := snapshotState(p).Result.Id
			p.swapSql(in.Sql)
			p.requestSubquery = false
			p.requestStatement = int(in.Statement)
			p.RequestRunAs(call.OnBehalfOf)
			if p.queryRuns == nil || len(p.queryRuns) >= queryRunsMax {
				p.queryRuns = make(map[string]queryRun, 1)
			}
			p.queryRuns[call.Key] = queryRun{before: ResultID(before), args: in}
			return out, nil
		})
	appops.Settles(playOps, opQuery, func(inst *PlayLauncher, call app.OperationCall) (out QueryResult, settled bool, err error) {
		p := inst.inner
		if p == nil {
			return out, true, app.RefuseOperation("the window has not mounted")
		}
		if !p.runLanded() {
			return out, false, nil
		}
		q := p.queryRuns[call.Key]
		delete(p.queryRuns, call.Key)
		return p.settleQuery(q), true, nil
	})
	playOps.Available(opQuery, func(sn opsSnap) (bool, string) {
		if sn.mounted && sn.state.Result.Phase == "running" {
			return false, "a run is in flight"
		}
		return true, ""
	})
}

// refuseQuery is query's checks at the call, judged on the SQL it would
// run before the buffer changes: the statement it picks, the parameters it
// needs, the window's class ceiling and the agent limits, as run judges
// its buffer.
func (inst *PlayApp) refuseQuery(obo *app.OnBehalfOf, in QueryArgs) (err error) {
	held := inst.sql
	inst.sql = in.Sql
	defer func() { inst.sql = held }()
	ranges, _ := inst.statementRanges()
	if in.Statement == 0 && len(ranges) > 1 {
		return app.RefuseOperation("the SQL holds " + strconv.Itoa(len(ranges)) + " statements; statement names the one to run")
	}
	runSQL := strings.TrimSpace(in.Sql)
	if in.Statement != 0 {
		if runSQL, err = inst.statementBuffer(int(in.Statement)); err != nil {
			return app.RefuseOperation(err.Error())
		}
	}
	// The parameter slots follow a new buffer after a debounce, so the
	// parameters are judged from a parse of the text itself.
	if _, _, names := inst.resolveRunSignals(runSQL); len(names) > 0 {
		return app.RefuseOperation("parameters need a value first: " + strings.Join(names, ", ") +
			"; bind them with a SET prelude in the SQL, or set_sql, set_param and run")
	}
	return inst.refuseAgentRunCall(obo, false, int(in.Statement))
}

// settleQuery is query's answer once its run ended: the main result's
// state, and its first rows when the run produced a result with rows.
func (inst *PlayApp) settleQuery(q queryRun) (out QueryResult) {
	out.Result = snapshotState(inst).Result
	if ResultID(out.Result.Id) == q.before {
		// The run did not happen: the result is the one held before, and
		// the notice says why.
		return
	}
	out.ResultId = out.Result.Id
	if !strings.HasPrefix(out.Result.Phase, "rows") {
		return
	}
	r := snapshotResults(inst)
	// The main result, whatever node the person observes.
	r.observed = nil
	r.active = r.sink
	sample, err := sampleResult(&r, SampleArgs{Limit: q.args.Limit, Fields: q.args.Fields, ResultId: &out.ResultId})
	if err != nil {
		out.SampleNote = err.Error()
		return
	}
	// The model learns this id now, before a snapshot may have seen the
	// result: remember its provenance here, so a check against it finds it.
	sample.StatementDigest = statementDigest(snapshotCheck(inst).source)
	out.Sample = &sample
	return
}
