package play

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonsql"
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
	// binding are the ad-hoc aliases the SQL names that the window binds
	// before the run starts, and bindBy when it stops waiting for them.
	binding []string
	bindBy  time.Time
	obo     *app.OnBehalfOf
}

// queryBindWait is how long query waits for the aliases it binds to resolve
// to a live dataset before it refuses.
const queryBindWait = 10 * time.Second

// queryRunsMax bounds the calls remembered; a call the host ended without
// settling (its window closed) leaves its entry behind.
const queryRunsMax = 8

// addQueryOp declares query; playOps' initializer calls it.
func addQueryOp(playOps *appops.Set[*PlayLauncher, opsSnap]) {
	appops.Command(playOps, app.OperationSpec{Name: opQuery, Version: 1,
		Summary: "replace the buffer with SQL, run it under the agent limits and answer with the result's first rows: set_sql, run and sample_rows in one call",
		Effect:  app.OperationEffectRun, Reads: []string{opsResParams, opsResSignals}, Writes: []string{opsResSql, opsResResult},
		Agents: true, Direct: true, Untrusted: true, Async: true,
		Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance,
		Follows: []string{"the buffer holds the SQL afterwards, and the result replaces the main result, as set_sql and run leave them",
			"a run the checks refuse is refused at the call and leaves the buffer as it was",
			"keelson('<alias>') of an ad-hoc dataset not bound in the window binds it first, as bind_dataset does, and runs once it is bound; the task that published it needs no grant entry to read it",
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
			unbound := p.unboundDatasets(in.Sql)
			if err = p.refuseQuery(call.OnBehalfOf, in, len(unbound) == 0); err != nil {
				return out, err
			}
			for _, alias := range unbound {
				if _, err = inst.bindDatasetAlias(alias, ""); err != nil {
					return out, err
				}
			}
			before := snapshotState(p).Result.Id
			if p.queryRuns == nil || len(p.queryRuns) >= queryRunsMax {
				p.queryRuns = make(map[string]queryRun, 1)
			}
			q := queryRun{before: ResultID(before), args: in, obo: call.OnBehalfOf}
			if len(unbound) > 0 {
				q.binding, q.bindBy = unbound, time.Now().Add(queryBindWait)
				p.queryRuns[call.Key] = q
				return out, nil
			}
			p.startQuery(q)
			p.queryRuns[call.Key] = q
			return out, nil
		})
	appops.Settles(playOps, opQuery, func(inst *PlayLauncher, call app.OperationCall) (out QueryResult, settled bool, err error) {
		p := inst.inner
		if p == nil {
			return out, true, app.RefuseOperation("the window has not mounted")
		}
		q := p.queryRuns[call.Key]
		if len(q.binding) > 0 {
			if waiting := p.stillUnbound(q.binding); len(waiting) > 0 {
				if time.Now().Before(q.bindBy) {
					return out, false, nil
				}
				delete(p.queryRuns, call.Key)
				why := ""
				if inst.follower != nil {
					why = inst.follower.Waiting()[waiting[0]]
				}
				reason := "no live dataset under " + strings.Join(waiting, ", ") + " after " + queryBindWait.String() + "; publish it first, or check the alias with list_bundles"
				if why != "" {
					reason += " (" + why + ")"
				}
				return out, true, app.RefuseOperation(reason)
			}
			// Bound: the run is judged now, when the grant check knows which
			// task published each dataset.
			q.binding = nil
			if err = p.refuseQuery(q.obo, q.args, true); err != nil {
				delete(p.queryRuns, call.Key)
				return out, true, err
			}
			p.startQuery(q)
			p.queryRuns[call.Key] = q
			return out, false, nil
		}
		if !p.runLanded() {
			return out, false, nil
		}
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

// startQuery puts the query's SQL in the buffer and asks for the run.
func (inst *PlayApp) startQuery(q queryRun) {
	inst.swapSql(q.args.Sql)
	inst.requestSubquery = false
	inst.requestStatement = int(q.args.Statement)
	inst.RequestRunAs(q.obo)
}

// unboundDatasets are the names sql reads with keelson('<name>') that are
// neither a table of this host nor bound in the window, and could be an
// ad-hoc dataset's alias: query binds them before it runs.
func (inst *PlayApp) unboundDatasets(sql string) (aliases []string) {
	if inst.client == nil {
		return
	}
	bound := inst.client.DatasetAliases()
	for _, name := range keelsonsql.References(sql) {
		if _, table := introspect.Default.Lookup(name); table || slices.Contains(bound, name) || slices.Contains(aliases, name) ||
			adhocdata.IsHandle(name) || !validDatasetIdentifier(name) {
			continue
		}
		aliases = append(aliases, name)
	}
	return
}

// stillUnbound are those of aliases not bound in the window yet.
func (inst *PlayApp) stillUnbound(aliases []string) (waiting []string) {
	bound := inst.client.DatasetAliases()
	for _, a := range aliases {
		if !slices.Contains(bound, a) {
			waiting = append(waiting, a)
		}
	}
	return
}

// refuseQuery is query's checks at the call, judged on the SQL it would
// run before the buffer changes: the statement it picks, the parameters it
// needs, the window's class ceiling and the agent limits, as run judges
// its buffer. limits false leaves out the agent limits, for SQL naming a
// dataset the window binds first: they are judged once it is bound.
func (inst *PlayApp) refuseQuery(obo *app.OnBehalfOf, in QueryArgs, limits bool) (err error) {
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
	if !limits {
		return
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
