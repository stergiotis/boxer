package appcenter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspectengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonquery"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// statements is every read the window makes, by the table its grant names.
var statements = []struct{ table, sql string }{
	{tableApps, appsSql},
	{tableAdr, adrSql},
	{tableCoderef, coderefSql},
	{tableCoverageStatus, coverageStatusSql},
	{tableCoveragePkgs, coveragePkgsSql},
	{tableClientCaps, capsSql},
	{tableTasks, tasksSql},
	{tableRunEvents, eventsSql},
	{tableAppState, stateSql},
	{llm.TableCalls, llmSql},
	{watchbill.TableJobs, jobsSql},
	{adhocdata.CatalogTableName, datasetsSql},
	{providers.TableAppRuns, runsSql},
	{providers.TableAppLogs, logsSql},
	{providers.TableAppAudit, auditSql},
}

// adhocStub stands in for keelson('adhoc'), which only a running dataset
// service registers. Its columns are the ones datasetsSql reads, spelled as
// the catalog spells them.
type adhocStub struct{}

func (adhocStub) Name() string                         { return adhocdata.CatalogTableName }
func (adhocStub) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (adhocStub) Schema() *arrow.Schema                { return adhocStubTable().Schema() }
func (adhocStub) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	return adhocStubTable().Build(proj, 0), nil
}
func adhocStubTable() *introspect.Table {
	return introspect.NewTable().
		String("alias", func(int) string { return "" }).
		String("handle", func(int) string { return "" }).
		String("publisher", func(int) string { return "" }).
		Uint64("rows", func(int) uint64 { return 0 }).
		Uint64("bytes", func(int) uint64 { return 0 }).
		Uint64("revision", func(int) uint64 { return 0 })
}

// hostRegistry registers every table the window reads the way the host
// does, with no stores behind them.
func hostRegistry(t *testing.T) *introspect.Registry {
	t.Helper()
	r := introspect.NewRegistry()
	require.NoError(t, providers.RegisterStatic(r))
	require.NoError(t, providers.RegisterRunEvents(r, nil, nil))
	require.NoError(t, providers.RegisterAppState(r, nil))
	require.NoError(t, providers.RegisterCoverage(r, nil))
	require.NoError(t, providers.RegisterEffects(r, nil, nil))
	require.NoError(t, watchbill.RegisterIntrospect(r, watchbill.IntrospectDeps{}))
	require.NoError(t, llm.RegisterIntrospect(r, nil))
	require.NoError(t, providers.RegisterAppTrail(r, nil))
	require.NoError(t, r.Register(adhocStub{}))
	return r
}

func TestManifestDeclaresEveryRead(t *testing.T) {
	declared := make(map[string]bool)
	for _, cp := range manifest.Caps {
		declared[cp.Pattern] = true
	}
	require.Len(t, readTables, len(statements))
	for _, s := range statements {
		assert.True(t, declared[keelsonquery.Subject(s.table)], s.table)
	}
	assert.True(t, declared[windowhost.OpenSubject])
	assert.Equal(t, AppId, manifest.Id)
}

// Every statement stays inside the one table its grant names (ADR-0253
// §SD3), and names a table the host registers.
func TestStatementsPassTheGate(t *testing.T) {
	reg := hostRegistry(t)
	for _, s := range statements {
		_, reason := keelsonquery.Gate(reg, s.sql, s.table)
		assert.Empty(t, reason, s.sql)
	}
}

// Over the host's arrangement — the chlocal broker and the keelson.query
// service on one bus — every statement answers and decodes into its columns,
// so a misspelt column or a mistyped field fails here.
func TestEveryLensReadsOverTheBus(t *testing.T) {
	if _, err := chlocalpool.LookupBinary(); err != nil {
		t.Skipf("clickhouse not installed: %v", err)
	}
	logger := zerolog.New(zerolog.NewTestWriter(t))
	bus := inprocbus.NewInst(logger)
	bus.SetRequestTimeout(30 * time.Second)
	broker, err := chlocalbroker.NewService(bus, chlocalpool.Config{
		BaseTmpDir: t.TempDir(), MinIdle: 1, MaxConcurrent: 3, SpawnConcurrency: 1,
	}, logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = broker.Stop(ctx)
	})
	svc, err := keelsonquery.NewService(bus, logger, hostRegistry(t), "introspect")
	require.NoError(t, err)
	svc.Timeout = 30 * time.Second
	t.Cleanup(svc.Close)

	r := newBusReader(bus.NewClient(AppId, manifest.Caps))
	r.cli.Timeout = 30 * time.Second
	ctx := context.Background()
	g := readGlobal(ctx, r, global{})
	for name, st := range map[string]struct {
		s    lensStateE
		note string
	}{
		"apps": {g.apps.state, g.apps.note}, "adr": {g.adrs.state, g.adrs.note},
		"coderef": {g.coderefs.state, g.coderefs.note}, "coverage_status": {g.coverage.state, g.coverage.note},
	} {
		assert.Equal(t, lensStateOk, st.s, "%s: %s", name, st.note)
	}
	assert.Contains(t, g.apps.cols.Id, string(AppId), "the window lists itself")

	p := readPage(ctx, r, page{appId: string(AppId)})
	for name, st := range map[string]struct {
		s    lensStateE
		note string
	}{
		"coverage": {p.coverage.state, p.coverage.note}, "caps": {p.caps.state, p.caps.note},
		"tasks": {p.tasks.state, p.tasks.note}, "events": {p.events.state, p.events.note},
		"state": {p.state.state, p.state.note}, "llm": {p.llm.state, p.llm.note},
		"jobs": {p.jobs.state, p.jobs.note}, "datasets": {p.datasets.state, p.datasets.note},
		"runs": {p.runs.state, p.runs.note}, "logs": {p.logs.state, p.logs.note}, "audit": {p.audit.state, p.audit.note},
	} {
		assert.Equal(t, lensStateOk, st.s, "%s: %s", name, st.note)
	}
}

// fixtureReader answers each table from a function, or refuses it.
type fixtureReader struct {
	answers map[string]func(dst any)
	refused map[string]bool
	failed  map[string]bool
	appIds  []string
}

func (inst *fixtureReader) read(ctx context.Context, table string, sql string, appId string, dst any) error {
	if appId != "" {
		inst.appIds = append(inst.appIds, appId)
	}
	if inst.refused[table] {
		return &keelsonquery.RefusedError{Table: table, Reason: "unknown keelson table " + table}
	}
	if inst.failed[table] {
		return errors.New("timeout")
	}
	if f, ok := inst.answers[table]; ok {
		f(dst)
	}
	return nil
}

func TestRefreshLandsLensStates(t *testing.T) {
	const id = "github.com/stergiotis/boxer/apps/demo"
	fx := &fixtureReader{
		answers: map[string]func(dst any){
			tableApps: func(dst any) {
				a := dst.(*appCols)
				*a = appCols{Id: []string{id}, Display: []string{"Demo"}, Summary: []string{""}, Icon: []string{""},
					Kind: []string{"go"}, Surface: []string{"windowed"}, Caps: [][]string{{"x.y [pub]"}}, LaunchKind: []string{""}}
			},
			tableAppState: func(dst any) {
				s := dst.(*stateCols)
				*s = stateCols{Kind: []string{"persist"}, Key: []string{"k"}, PayloadBytes: []int64{3}, WrittenAt: []string{"t"}}
			},
		},
		refused: map[string]bool{llm.TableCalls: true},
		failed:  map[string]bool{watchbill.TableJobs: true},
	}
	inst := newApp()
	inst.reader = fx
	inst.appCtx, inst.cancelApp = context.WithCancel(context.Background())
	defer inst.cancelApp()

	inst.wantGlobal = true
	inst.selectApp(id)
	inst.refresh()
	snap := inst.snapshot()
	require.True(t, snap.reads)
	assert.Equal(t, lensStateOk, snap.global.apps.state)
	assert.Equal(t, []string{id}, snap.global.apps.cols.Id)
	assert.Equal(t, id, snap.page.appId)
	assert.Equal(t, lensStateOk, snap.page.state.state)
	assert.Equal(t, []string{"k"}, snap.page.state.cols.Key)
	assert.Equal(t, lensStateRefused, snap.page.llm.state)
	assert.Contains(t, snap.page.llm.note, "unknown keelson table")
	assert.Equal(t, lensStateFailed, snap.page.jobs.state)
	assert.Equal(t, lensStateOk, snap.page.events.state, "an answered read with no rows is ok, not unread")
	for _, a := range fx.appIds {
		assert.Equal(t, id, a, "every page read binds the selected id")
	}

	// A second pass does not re-read the window-wide tables unless asked.
	fx.answers[tableApps] = func(dst any) { *dst.(*appCols) = appCols{} }
	inst.refresh()
	assert.Equal(t, []string{id}, inst.snapshot().global.apps.cols.Id)

	// A new selection starts unread rather than under the old app's rows.
	inst.selectApp("other")
	delete(fx.answers, tableAppState)
	inst.refresh()
	snap = inst.snapshot()
	assert.Equal(t, "other", snap.page.appId)
	assert.Empty(t, snap.page.state.cols.Key)
}

func TestNoBusSaysSo(t *testing.T) {
	inst := newApp()
	inst.appCtx, inst.cancelApp = context.WithCancel(context.Background())
	defer inst.cancelApp()
	inst.refresh()
	snap := inst.snapshot()
	assert.False(t, snap.reads)
	assert.Contains(t, snap.lastError, "no bus")
}

func TestPackageDir(t *testing.T) {
	pkgs := []string{"apps/play", "apps/play/sub", "play", "public/keelson/runtime/launcher", "apps/appstate/scenes"}
	assert.Equal(t, "apps/play", packageDir("github.com/stergiotis/boxer/apps/play", pkgs),
		"the longest tail wins over a same-named top-level directory")
	assert.Equal(t, "public/keelson/runtime/launcher", packageDir("github.com/stergiotis/boxer/public/keelson/runtime/launcher", pkgs))
	assert.Equal(t, "apps/appstate", packageDir("github.com/stergiotis/boxer/apps/appstate", pkgs),
		"an app whose own directory cites nothing still matches through a subdirectory")
	assert.Equal(t, "", packageDir("github.com/stergiotis/boxer/apps/sqlapplet/weather", pkgs), "an applet has no package")
	assert.Equal(t, "", packageDir("runtime.coverage", pkgs))
	assert.Equal(t, "", packageDir("github.com/stergiotis/boxer/apps/playground", pkgs), "a prefix of a segment is not a match")
}

func TestAdrsFor(t *testing.T) {
	refs := coderefCols{
		Pkg:  []string{"apps/play", "apps/play/sub", "apps/play", "apps/other"},
		Num:  []int32{12, 12, 40, 99},
		Refs: []uint64{2, 3, 1, 7},
	}
	adrs := adrCols{Num: []int32{12, 40, 99}, Title: []string{"twelve", "forty", "ninety-nine"}, Status: []string{"accepted", "proposed", "accepted"}}
	out, dir := adrsFor("github.com/stergiotis/boxer/apps/play", &refs, &adrs)
	assert.Equal(t, "apps/play", dir)
	require.Len(t, out, 2)
	assert.Equal(t, adrRef{num: 12, title: "twelve", status: "accepted", refs: 5}, out[0], "the subtree's citations sum")
	assert.Equal(t, int32(40), out[1].num)

	out, dir = adrsFor("github.com/stergiotis/boxer/apps/sqlapplet/x", &refs, &adrs)
	assert.Empty(t, dir)
	assert.Empty(t, out)
}

func TestSums(t *testing.T) {
	cv := coveragePkgCols{PkgPath: []string{"a", "a/b"}, CoveredStmts: []uint64{1, 2}, TotalStmts: []uint64{4, 4},
		CoveredFuncs: []uint64{1, 0}, TotalFuncs: []uint64{2, 2}}
	tot := sumCoverage(&cv)
	assert.Equal(t, coverageTotal{pkgs: 2, coveredStmts: 3, totalStmts: 8, coveredFuncs: 1, totalFuncs: 4}, tot)
	assert.InDelta(t, 37.5, percent(tot.coveredStmts, tot.totalStmts), 1e-9)
	assert.Zero(t, percent(0, 0))

	assert.False(t, coverageActive(&coverageStatusCols{}))
	assert.True(t, coverageActive(&coverageStatusCols{Mode: []string{"atomic"}}))

	ev := eventCols{Kind: []string{"log", "audit", "log", "lifecycle"}}
	assert.Equal(t, []kindCount{{"log", 2}, {"audit", 1}, {"lifecycle", 1}}, countKinds(&ev))

	lc := llmCols{At: []string{"a", "b"}, InputTokens: []int64{3, 4}, OutputTokens: []int64{1, 1},
		Refused: []bool{false, true}, Error: []string{"", ""}}
	assert.Equal(t, llmTotal{calls: 2, failed: 1, inputTokens: 7, outputTokens: 2}, sumLlm(&lc))
}

func TestMatchingApps(t *testing.T) {
	cols := appCols{Id: []string{"x/appstate", "x/play"}, Display: []string{"App state", "Play"}, Summary: []string{"clear", "sql"}}
	assert.Equal(t, []int{0, 1}, matchingApps(&cols, " "))
	assert.Equal(t, []int{1}, matchingApps(&cols, "PLA"))
	assert.Equal(t, []int{0}, matchingApps(&cols, "clear"))
	assert.Equal(t, "play", shortApp("x/play"))
	assert.Equal(t, 1, appIndex(&cols, "x/play"))
	assert.Equal(t, -1, appIndex(&cols, "nope"))
}

var _ app.AppI = newApp()

func TestRunSummary(t *testing.T) {
	rc := runCols{
		RunId:       []string{"r1", "r1", "r2"},
		InstanceKey: []uint64{1, 2, 1},
		StartedMs:   []int64{1000, 5000, 0},
		StoppedMs:   []int64{3000, 0, 9000},
		StopReason:  []string{"user-close", "", "shutdown"},
		RunSeenMs:   []int64{0, 0, 0},
	}
	assert.Equal(t, runSummary{sessions: 3, runs: 2, open: 1, lastMs: 9000}, summarizeRuns(&rc))
	assert.Equal(t, "2s", sessionLength(1000, 3000, 0))
	assert.Equal(t, "≥ 4s", sessionLength(1000, 0, 5000), "no close: the process's last sign of life is a lower bound")
	assert.Empty(t, sessionLength(0, 3000, 0), "a start before the look-back has no length")
	assert.Empty(t, sessionLength(1000, 0, 0), "no close and no later sign of the process")
	end, closed := sessionEnd(1000, 0, 500)
	assert.Zero(t, end, "a heartbeat before the start says nothing about the end")
	assert.False(t, closed)
	assert.Equal(t, []kindCount{{"warn", 2}, {"info", 1}}, countValues([]string{"warn", "info", "warn"}))
	assert.Equal(t, "—", timeOfMs(0))
	assert.Equal(t, "abcdefgh", shortRun("abcdefghij"))
}

func TestPlayQueries(t *testing.T) {
	const id = "github.com/stergiotis/boxer/apps/appstate"
	for _, key := range playSections {
		sql, ok := playQuery(key, id, "apps/appstate")
		require.True(t, ok, key)
		if key == secAdrs {
			assert.Contains(t, sql, "'apps/appstate'", "the ADR statement filters by directory")
		} else {
			assert.Contains(t, sql, "'"+id+"'", key)
		}
	}
	_, ok := playQuery(secAdrs, id, "")
	assert.False(t, ok, "no directory, no ADR statement")
	_, ok = playQuery("nope", id, "")
	assert.False(t, ok)
	sql, _ := playQuery(secLlm, id, "")
	assert.NotContains(t, sql, "prompt", "the model-call statement leaves the text out")
	assert.Equal(t, `'a\'b\\c'`, sqlString(`a'b\c`))
}

// Every statement runs on the engine the introspection endpoint serves
// play's window through, over a registry built the way the host builds it.
func TestPlayQueriesRunOnTheEngine(t *testing.T) {
	if _, err := chlocalpool.LookupBinary(); err != nil {
		t.Skipf("clickhouse not installed: %v", err)
	}
	logger := zerolog.New(zerolog.NewTestWriter(t))
	bus := inprocbus.NewInst(logger)
	bus.SetRequestTimeout(30 * time.Second)
	broker, err := chlocalbroker.NewService(bus, chlocalpool.Config{
		BaseTmpDir: t.TempDir(), MinIdle: 1, MaxConcurrent: 3, SpawnConcurrency: 1,
	}, logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = broker.Stop(ctx)
	})
	e, err := introspectengine.New(introspectengine.Config{
		Registry: hostRegistry(t),
		Bus: bus.NewClient("test.appcenter.play", []app.SubjectFilter{
			{Pattern: chlocalbroker.SubjectExecAll, Direction: app.CapDirectionBoth, Reason: "test"},
		}),
	}, logger)
	require.NoError(t, err)
	for _, key := range playSections {
		sql, ok := playQuery(key, "github.com/stergiotis/boxer/apps/appstate", "apps/appstate")
		require.True(t, ok)
		_, _, err := e.Query(context.Background(), sql, "TabSeparated")
		assert.NoError(t, err, "%s:\n%s", key, sql)
	}
}
