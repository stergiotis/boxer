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
	"github.com/stergiotis/boxer/public/keelson/runtime/sessiondata"
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
	// KeepAs keeps the statement's result in the task's data session
	// instead of running it as the main result.
	KeepAs string `json:",omitzero" desc:"keep the result as a session data set under this name (an identifier) instead of returning it: later statements read it as session('<name>') and join it with server tables and keelson('<alias>') datasets; the answer describes it and samples its first rows; the same name again redefines it"`
	// OrderBy is the kept table's sort key.
	OrderBy []string `json:",omitzero" desc:"with keep_as: columns of the result to sort the kept table by, which speeds later filters and joins on them"`
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
	// Kept is the data set keep_as defined.
	Kept *KeptDataset `json:",omitzero" desc:"with keep_as: the data set kept — its name, revision, rows, bytes and columns, and the data sets its redefinition left to rebuild; Result and Sample are then its first rows, SELECT * FROM session('<name>') LIMIT n"`
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
	// keep is keep_as's definition in flight, and kept what it made once
	// the window samples it.
	keep *sessionCall[keepResult]
	kept *KeptDataset
	// refresh is the listing of the data session the keep asked for, which
	// the call waits for before it settles.
	refresh uint64
}

// queryBindWait is how long query waits for the aliases it binds to resolve
// to a live dataset before it refuses.
const queryBindWait = 10 * time.Second

// queryRunsMax bounds the calls remembered; a call the host ended without
// settling (its window closed) leaves its entry behind.
const queryRunsMax = 8

// addQueryOp declares query; playOps' initializer calls it.
func addQueryOp(playOps *appops.Set[*PlayLauncher, opsSnap]) {
	appops.Command(playOps, app.OperationSpec{Name: opQuery, Version: 2,
		Summary: "replace the buffer with SQL, run it under the agent limits and answer with the result's first rows: set_sql, run and sample_rows in one call; with keep_as, keep the result as a named session data set",
		Effect:  app.OperationEffectRun, Reads: []string{opsResParams, opsResSignals}, Writes: []string{opsResSql, opsResResult},
		Agents: true, Direct: true, Untrusted: true, Async: true,
		Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance,
		Follows: []string{"the buffer holds the SQL afterwards, and the result replaces the main result, as set_sql and run leave them",
			"a run the checks refuse is refused at the call and leaves the buffer as it was",
			"about to reuse a derivation (a CTE, a filtered or tokenised table) in later statements? keep it once with keep_as: '<name>' and read session('<name>') afterwards, instead of repeating its SQL in every statement",
			"a session data set lives in your task's data session on the server for the task's life; other apps of the task read it too; keep_as with the same name redefines it, and data sets reading it rebuild when next read; list_datasets lists them, drop_dataset drops one",
			"session('<name>') joins server tables and keelson('<alias>') ad-hoc datasets in one statement; keeping a data set needs clickhouse-session:<host:port> in the grant beside clickhouse:<host:port>, the same host and port",
			"with keep_as the answer is the data set and its first rows, SELECT * FROM session('<name>') LIMIT n, which becomes the buffer and the main result",
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
			if in.KeepAs != "" || len(in.OrderBy) > 0 {
				return out, inst.startKeepQuery(call, in)
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
		if q.keep != nil && q.kept == nil {
			res, kErr, done := q.keep.result()
			if !done {
				return out, false, nil
			}
			q.refresh = p.refreshSessionsFor(q.obo)
			if kErr != nil {
				delete(p.queryRuns, call.Key)
				return out, true, sessionRefusal(kErr)
			}
			// Kept: the window samples it as its main run, so the answer
			// has a result_id check and cite rest on.
			q.kept = keptOf(res.res, res.stale)
			q.args.Sql, q.args.Statement = sampleStatement(q.args.KeepAs, q.args.Limit), 0
			q.before = ResultID(snapshotState(p).Result.Id)
			p.startQuery(q)
			p.queryRuns[call.Key] = q
			return out, false, nil
		}
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
		if q.kept != nil && q.refresh != 0 && !p.sessionsShown(q.refresh) {
			// A keep settles once the window's listing shows it, so the
			// revisions it reports are the ones the window holds.
			return out, false, nil
		}
		delete(p.queryRuns, call.Key)
		out = p.settleQuery(q)
		out.Kept = q.kept
		return out, true, nil
	})
	playOps.Available(opQuery, func(sn opsSnap) (bool, string) {
		if sn.mounted && sn.state.Result.Phase == "running" {
			return false, "a run is in flight"
		}
		if sn.keeping {
			return false, "a data set is being kept"
		}
		return true, ""
	})
}

// startKeepQuery is query with keep_as: the checks at the call, then the
// definition off the frame; the call settles once the data set is made and
// sampled.
func (inst *PlayLauncher) startKeepQuery(call app.OperationCall, in QueryArgs) (err error) {
	p := inst.inner
	switch {
	case in.KeepAs == "":
		return app.RefuseOperation("order_by sorts a kept data set; name it with keep_as")
	case !sessiondata.ValidName(in.KeepAs):
		return app.RefuseOperation("keep_as is an identifier of at most 64 bytes: letters, digits and _, not starting with a digit")
	case p.client == nil || p.client.sessionBus() == nil:
		return app.RefuseOperation("the window is not wired to the session data service, so it keeps no data set")
	}
	for _, col := range in.OrderBy {
		if !sessiondata.ValidName(col) {
			return app.RefuseOperation("order_by names columns of the result by identifier")
		}
	}
	if err = p.refuseQuery(call.OnBehalfOf, in, false); err != nil {
		return
	}
	if err = keepGrant(p.client, call.OnBehalfOf); err != nil {
		return
	}
	def, err := p.keepStatement(in)
	if err != nil {
		return
	}
	if p.queryRuns == nil || len(p.queryRuns) >= queryRunsMax {
		p.queryRuns = make(map[string]queryRun, 1)
	}
	p.queryRuns[call.Key] = queryRun{args: in, obo: call.OnBehalfOf, keep: p.startKeep(call.OnBehalfOf, in.KeepAs, def, in.OrderBy)}
	return
}

// keepStatement is the statement keep_as defines a data set by: the one
// statement of the SQL, or the one Statement picks.
func (inst *PlayApp) keepStatement(in QueryArgs) (def string, err error) {
	if in.Statement == 0 {
		return strings.TrimSpace(in.Sql), nil
	}
	held := inst.sql
	inst.sql = in.Sql
	defer func() { inst.sql = held }()
	if def, err = inst.statementBuffer(int(in.Statement)); err != nil {
		return "", app.RefuseOperation(err.Error())
	}
	return
}

// keeping reports whether a keep_as definition is in flight.
func (inst *PlayApp) keeping() (yes bool) {
	for _, q := range inst.queryRuns {
		if q.keep != nil && q.kept == nil {
			if _, _, done := q.keep.result(); !done {
				return true
			}
		}
	}
	return
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
