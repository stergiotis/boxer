package appcenter

import (
	"context"
	"errors"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonquery"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill"
)

// Every lens is one statement over one introspection table (ADR-0260 §SD3),
// read through the `keelson.query.<table>` grant the manifest declares
// (ADR-0253) and decoded into columns (ADR-0257). The gate admits one table
// per statement, so the page is composed here rather than joined in SQL.

// Introspection table names. The providers that own them export most; the
// three below are spelled here because their providers live in packages the
// runtime does not import from an app (effects, coverage, the ADR corpus).
const (
	tableApps           = "apps"
	tableAdr            = "adr"
	tableCoderef        = "coderef"
	tableCoverageStatus = "coverage_status"
	tableCoveragePkgs   = "coverage_pkgs"
	tableClientCaps     = "client_caps"
	tableTasks          = "tasks"
	tableRunEvents      = "runtime_events"
	tableAppState       = providers.TableAppState
)

// readTables is every table the window reads, in the order the manifest
// declares them.
var readTables = []string{
	tableApps, tableAdr, tableCoderef, tableCoverageStatus, tableCoveragePkgs,
	tableClientCaps, tableTasks, tableRunEvents, tableAppState,
	llm.TableCalls, watchbill.TableJobs, adhocdata.CatalogTableName,
	providers.TableAppRuns, providers.TableAppLogs, providers.TableAppAudit,
}

// appCols is keelson('apps'): the list and each page's head.
type appCols struct {
	Id         []string   `ch:"id"`
	Display    []string   `ch:"display"`
	Summary    []string   `ch:"summary"`
	Icon       []string   `ch:"icon"`
	Kind       []string   `ch:"kind"`
	Surface    []string   `ch:"surface"`
	Caps       [][]string `ch:"caps"`
	LaunchKind []string   `ch:"launch_kind"`
}

// adrCols is keelson('adr'), the titles the ADR lens names.
type adrCols struct {
	Num    []int32  `ch:"num"`
	Title  []string `ch:"title"`
	Status []string `ch:"status"`
}

// coderefCols is keelson('coderef') folded to citations per package and ADR.
type coderefCols struct {
	Pkg  []string `ch:"pkg"`
	Num  []int32  `ch:"num"`
	Refs []uint64 `ch:"refs"`
}

// coverageStatusCols is keelson('coverage_status'): whether sampling runs.
type coverageStatusCols struct {
	Mode         []string `ch:"mode"`
	Samples      []uint64 `ch:"samples"`
	CoveredStmts []uint64 `ch:"covered_stmts"`
	TotalStmts   []uint64 `ch:"total_stmts"`
}

// coveragePkgCols is keelson('coverage_pkgs') for one app's package subtree.
type coveragePkgCols struct {
	PkgPath      []string `ch:"pkg_path"`
	CoveredStmts []uint64 `ch:"covered_stmts"`
	TotalStmts   []uint64 `ch:"total_stmts"`
	CoveredFuncs []uint64 `ch:"covered_funcs"`
	TotalFuncs   []uint64 `ch:"total_funcs"`
}

// capCols is keelson('client_caps') for one app: what its windows hold.
type capCols struct {
	InstanceKey []uint64 `ch:"instance_key"`
	Pattern     []string `ch:"pattern"`
	Direction   []string `ch:"direction"`
	Reason      []string `ch:"reason"`
	Declared    []bool   `ch:"declared"`
}

// taskCols is keelson('tasks') for one app.
type taskCols struct {
	Title     []string `ch:"title"`
	Kind      []string `ch:"kind"`
	State     []string `ch:"state"`
	CreatedAt []string `ch:"created_at"`
}

// eventCols is keelson('runtime_events') for one app, newest first.
type eventCols struct {
	TsMs        []int64  `ch:"ts_ms"`
	Kind        []string `ch:"kind"`
	InstanceKey []uint64 `ch:"instance_key"`
	Detail      []string `ch:"detail"`
}

// stateCols is keelson('app_state') for one app.
type stateCols struct {
	Kind         []string `ch:"kind"`
	Key          []string `ch:"key"`
	PayloadBytes []int64  `ch:"payload_bytes"`
	WrittenAt    []string `ch:"written_at"`
}

// llmCols is keelson('llm_calls') for one app. The prompt and completion
// columns are never selected (ADR-0260 §SD3): the page reports that calls
// were made and what they cost, not what they said.
type llmCols struct {
	At           []string `ch:"at"`
	Purpose      []string `ch:"purpose"`
	Model        []string `ch:"model"`
	InputTokens  []int64  `ch:"input_tokens"`
	OutputTokens []int64  `ch:"output_tokens"`
	ElapsedMs    []int64  `ch:"elapsed_ms"`
	Refused      []bool   `ch:"refused"`
	Error        []string `ch:"error"`
}

// jobCols is keelson('watchbill') for the jobs one app owns.
type jobCols struct {
	Id         []string `ch:"id"`
	Kind       []string `ch:"kind"`
	State      []string `ch:"state"`
	Attempt    []int64  `ch:"attempt"`
	FinishedAt []string `ch:"finished_at"`
	LastError  []string `ch:"last_error"`
}

// datasetCols is keelson('adhoc') for the datasets one app publishes.
type datasetCols struct {
	Alias    []string `ch:"alias"`
	Handle   []string `ch:"handle"`
	Rows     []uint64 `ch:"rows"`
	Bytes    []uint64 `ch:"bytes"`
	Revision []uint64 `ch:"revision"`
}

// runCols is keelson('app_runs') for one app: its window sessions across
// processes (ADR-0260 §SD5).
type runCols struct {
	RunId       []string `ch:"run_id"`
	InstanceKey []uint64 `ch:"instance_key"`
	StartedMs   []int64  `ch:"started_ms"`
	StoppedMs   []int64  `ch:"stopped_ms"`
	StopReason  []string `ch:"stop_reason"`
}

// logCols is keelson('app_logs') for one app: its newest log rows across
// processes.
type logCols struct {
	TsMs        []int64  `ch:"ts_ms"`
	InstanceKey []uint64 `ch:"instance_key"`
	RunId       []string `ch:"run_id"`
	Level       []string `ch:"level"`
	Caller      []string `ch:"caller"`
	Message     []string `ch:"message"`
	Error       []string `ch:"error"`
}

// auditCols is keelson('app_audit') for one app: its audited requests,
// folded per subject and result.
type auditCols struct {
	Subject       []string  `ch:"subject"`
	Result        []string  `ch:"result"`
	Requests      []uint64  `ch:"requests"`
	FirstMs       []int64   `ch:"first_ms"`
	LastMs        []int64   `ch:"last_ms"`
	MeanLatencyMs []float64 `ch:"mean_latency_ms"`
	MaxLatencyMs  []uint64  `ch:"max_latency_ms"`
}

// runsRead is how many sessions a page reads; the summary says when it hit
// it, since a count of a capped list is not the app's history.
const (
	runsRead    = 100
	runsReadStr = "100"
)

// The statements. The per-app ones bind the app id as {app:String}, so an
// id is never spliced into SQL.
const (
	appsSql = "SELECT id, display, summary, icon, kind, surface, caps, launch_kind FROM keelson('apps') ORDER BY id"
	adrSql  = "SELECT num, title, status FROM keelson('adr') ORDER BY num"
	// coderefSql folds the citation index once per window: a few thousand
	// rows become one per (package, ADR), which every page matches in Go.
	coderefSql        = "SELECT pkg, num, count() AS refs FROM keelson('coderef') GROUP BY pkg, num ORDER BY pkg, num"
	coverageStatusSql = "SELECT mode, samples, covered_stmts, total_stmts FROM keelson('coverage_status')"

	coveragePkgsSql = "SELECT pkg_path, covered_stmts, total_stmts, covered_funcs, total_funcs FROM keelson('coverage_pkgs') " +
		"WHERE pkg_path = {app:String} OR startsWith(pkg_path, concat({app:String}, '/')) ORDER BY pkg_path"
	capsSql = "SELECT instance_key, pattern, direction, reason, declared FROM keelson('client_caps') " +
		"WHERE app_id = {app:String} ORDER BY instance_key, pattern"
	tasksSql = "SELECT title, kind, state, created_at FROM keelson('tasks') " +
		"WHERE owner_app_id = {app:String} ORDER BY created_at DESC"
	eventsSql = "SELECT ts_ms, kind, instance_key, detail FROM keelson('runtime_events') " +
		"WHERE app_id = {app:String} ORDER BY ts_ms DESC LIMIT 500"
	stateSql = "SELECT kind, key, payload_bytes, written_at FROM keelson('app_state') " +
		"WHERE app_id = {app:String} ORDER BY kind, key"
	llmSql = "SELECT at, purpose, model, input_tokens, output_tokens, elapsed_ms, refused, error FROM keelson('llm_calls') " +
		"WHERE app_id = {app:String} ORDER BY at DESC LIMIT 100"
	jobsSql = "SELECT id, kind, state, attempt, finished_at, last_error FROM keelson('watchbill') " +
		"WHERE owner_app_id = {app:String} ORDER BY run_after DESC LIMIT 100"
	datasetsSql = "SELECT alias, handle, rows, bytes, revision FROM keelson('adhoc') " +
		"WHERE publisher = {app:String} ORDER BY alias"
	runsSql = "SELECT run_id, instance_key, started_ms, stopped_ms, stop_reason FROM keelson('app_runs') " +
		"WHERE app_id = {app:String} ORDER BY greatest(started_ms, stopped_ms) DESC LIMIT " + runsReadStr
	logsSql = "SELECT ts_ms, instance_key, run_id, level, caller, message, error FROM keelson('app_logs') " +
		"WHERE app_id = {app:String} ORDER BY ts_ms DESC"
	auditSql = "SELECT subject, result, requests, first_ms, last_ms, mean_latency_ms, max_latency_ms FROM keelson('app_audit') " +
		"WHERE app_id = {app:String} ORDER BY requests DESC, subject, result"
)

// lensStateE says what a lens holds, so an empty section can say why.
type lensStateE uint8

const (
	// lensStateUnread: no read has come back yet.
	lensStateUnread lensStateE = iota
	// lensStateOk: the read answered; the rows may be none.
	lensStateOk
	// lensStateRefused: the service declined — most often a table this host
	// does not serve (no facts store, no model, no watchbill).
	lensStateRefused
	// lensStateFailed: transport or decode failure.
	lensStateFailed
)

// lens is one section's rows and how they came to be.
type lens[T any] struct {
	cols  T
	state lensStateE
	note  string
}

// readInto runs one read and records its outcome on l. A failure keeps the
// rows l had: a transient timeout should not blank a section.
func readInto[T any](ctx context.Context, r readerI, table string, sql string, appId string, l *lens[T]) {
	var cols T
	err := r.read(ctx, table, sql, appId, &cols)
	var refused *keelsonquery.RefusedError
	switch {
	case err == nil:
		l.cols, l.state, l.note = cols, lensStateOk, ""
	case errors.As(err, &refused):
		l.cols, l.state, l.note = cols, lensStateRefused, refused.Reason
	default:
		l.state, l.note = lensStateFailed, err.Error()
	}
}

// readerI is what the poller reads through: the tables over the bus in the
// window, a fixture in tests. appId is bound as {app:String}; "" binds none.
type readerI interface {
	read(ctx context.Context, table string, sql string, appId string, dst any) (err error)
}

// busReader reads over the bus.
type busReader struct {
	cli *keelsonquery.Client
}

// newBusReader returns nil when bus is nil — the host minted no bus for the
// window — so the window can say so rather than fail each read.
func newBusReader(bus app.BusI) (inst *busReader) {
	if bus == nil {
		return nil
	}
	inst = &busReader{cli: keelsonquery.NewClient(bus)}
	return
}

func (inst *busReader) read(ctx context.Context, table string, sql string, appId string, dst any) (err error) {
	r := keelsonquery.Request{Table: table, Sql: sql}
	if appId != "" {
		r.Params = map[string]string{"app": appId}
	}
	_, err = keelsonquery.ColumnsWith(ctx, inst.cli, r, dst)
	return
}
