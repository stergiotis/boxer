package jackstay

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	jk "github.com/stergiotis/boxer/public/db/clickhouse/jackstay"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

func query[In any, Out any](t *testing.T, inst *App, name string, in In) (out Out, err error) {
	t.Helper()
	args, encErr := buscodec.Encode(in)
	require.NoError(t, encErr)
	raw, err := inst.Operations().Snapshot().Query(name, args)
	if err != nil {
		return
	}
	out, decErr := buscodec.Decode[Out](raw)
	require.NoError(t, decErr)
	return
}

func refusal(err error) (r *app.OperationRefusal) {
	errors.As(err, &r)
	return
}

// opsPlanApp holds a plan of three tables: one needing DDL, one compared
// and found differing, one synced with a chunk left.
func opsPlanApp() (inst *App, t0 time.Time) {
	t0 = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	ddl := table("default", "a")
	ddl.Verdict = jk.VerdictCreate
	ddl.DDL = []string{"CREATE TABLE default.a (x UInt8) ENGINE = MergeTree ORDER BY x"}
	ddl.Reasons = []string{"the target has no such table"}
	compared := table("default", "b")
	compared.Diff = &jk.TableDiff{ComputedAt: t0, SrcRows: 10, DstRows: 8, Chunks: 4, IdenticalChunks: 3, Differing: []jk.ChunkDiff{{Id: "c1"}}, Missing: 2}
	synced := table("default", "c")
	synced.SyncReport = &jk.TableSyncReport{RunId: "r1", FinishedAt: t0.Add(time.Minute), Copied: 3, Failed: 1, Rows: 300, Bytes: 4096, Problems: []string{"chunk 4: timeout"}}
	inst = phaseApp()
	p := testPlan(ddl, compared, synced)
	inst.installPlan(&p)
	inst.planName = "plan.json"
	return
}

func TestOperationsCatalogRegisters(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	m, ok := app.LookupManifest(AppId)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(AppId))
	for _, name := range []string{opGetPlan, opGetTable, opGetCompare, opGetSync} {
		spec, found := m.Operations.Lookup(name)
		require.True(t, found, name)
		assert.True(t, spec.Agents, name)
		assert.Equal(t, app.OperationClassQuery, spec.Class, name)
	}
	for _, spec := range m.Operations.Operations {
		assert.NotEqual(t, app.OperationClassCommand, spec.Class, "jackstay's writes stay behind the window's confirmation (ADR-0259)")
	}
}

func TestGetPlanIsBoundedAndRefusesWithoutAPlan(t *testing.T) {
	_, err := query[GetPlanArgs, PlanResult](t, phaseApp(), opGetPlan, GetPlanArgs{})
	assert.Equal(t, noPlanReason, refusal(err).Reason)

	inst, _ := opsPlanApp()
	inst.mirrorPhase()
	out, err := query[GetPlanArgs, PlanResult](t, inst, opGetPlan, GetPlanArgs{})
	require.NoError(t, err)
	assert.Equal(t, "plan.json", out.Name)
	assert.Equal(t, 3, out.Matched)
	assert.False(t, out.Truncated)
	assert.Equal(t, "incomplete", out.Phase, "the latest section, the sync with a chunk left, speaks")
	require.Len(t, out.Tables, 3)
	assert.Equal(t, PlanRow{Database: "default", Name: "a", TargetDatabase: "default", TargetName: "a", Verdict: "create", DDLPending: true}, out.Tables[0])
	assert.True(t, out.Tables[1].Compared)
	assert.True(t, out.Tables[2].Synced)

	out, err = query[GetPlanArgs, PlanResult](t, inst, opGetPlan, GetPlanArgs{Limit: 2})
	require.NoError(t, err)
	assert.Len(t, out.Tables, 2)
	assert.True(t, out.Truncated)
	assert.Equal(t, 3, out.Matched, "the limit bounds the rows, not the count")
	_, err = query[GetPlanArgs, PlanResult](t, inst, opGetPlan, GetPlanArgs{Limit: -1})
	assert.NotNil(t, refusal(err))

	many := make([]jk.PlanTable, opsMaxTables+20)
	for i := range many {
		many[i] = table("default", "t")
	}
	p := testPlan(many...)
	inst.installPlan(&p)
	out, err = query[GetPlanArgs, PlanResult](t, inst, opGetPlan, GetPlanArgs{Limit: opsMaxTables + 50})
	require.NoError(t, err)
	assert.Len(t, out.Tables, opsMaxTables)
	assert.Equal(t, opsMaxTables+20, out.Matched)
}

func TestGetTableNamesGetPlanWhenItMisses(t *testing.T) {
	inst, t0 := opsPlanApp()
	out, err := query[GetTableArgs, TableResult](t, inst, opGetTable, GetTableArgs{Database: "default", Name: "a"})
	require.NoError(t, err)
	assert.Equal(t, "create", out.Table.Verdict)
	assert.Equal(t, []string{"CREATE TABLE default.a (x UInt8) ENGINE = MergeTree ORDER BY x"}, out.DDL)
	assert.Equal(t, []string{"the target has no such table"}, out.Reasons)
	assert.Nil(t, out.Compare)

	out, err = query[GetTableArgs, TableResult](t, inst, opGetTable, GetTableArgs{Database: "default", Name: "c"})
	require.NoError(t, err)
	require.NotNil(t, out.Sync)
	assert.Equal(t, t0.Add(time.Minute), out.Sync.FinishedAt)
	assert.Equal(t, []string{"chunk 4: timeout"}, out.Sync.Problems)

	_, err = query[GetTableArgs, TableResult](t, inst, opGetTable, GetTableArgs{Database: "default", Name: "zz"})
	r := refusal(err)
	require.NotNil(t, r)
	assert.Contains(t, r.Reason, "get_plan")
}

func TestGetCompareAndGetSyncReadTheLastRun(t *testing.T) {
	inst, t0 := opsPlanApp()
	cmp, err := query[GetCompareArgs, CompareResult](t, inst, opGetCompare, GetCompareArgs{})
	require.NoError(t, err)
	assert.Equal(t, 1, cmp.Compared)
	assert.Equal(t, 1, cmp.Differing)
	assert.Equal(t, t0, cmp.ComputedAt)
	require.Len(t, cmp.Tables, 1)
	assert.Equal(t, CompareRow{Database: "default", Name: "b", ComputedAt: t0, SourceRows: 10, TargetRows: 8, Chunks: 4,
		IdenticalChunks: 3, DifferingChunks: 1, Missing: 2}, cmp.Tables[0])

	sync, err := query[GetSyncArgs, SyncResult](t, inst, opGetSync, GetSyncArgs{})
	require.NoError(t, err)
	assert.Equal(t, 1, sync.Reported)
	assert.Equal(t, 1, sync.Unsynced)
	require.Len(t, sync.Tables, 1)
	assert.Equal(t, "r1", sync.Tables[0].RunId)
	assert.Equal(t, uint64(300), sync.Tables[0].Rows)

	bare := phaseApp()
	p := testPlan(table("default", "a"))
	bare.installPlan(&p)
	_, err = query[GetCompareArgs, CompareResult](t, bare, opGetCompare, GetCompareArgs{})
	assert.Equal(t, noCompareReason, refusal(err).Reason)
	_, err = query[GetSyncArgs, SyncResult](t, bare, opGetSync, GetSyncArgs{})
	assert.Equal(t, noSyncReason, refusal(err).Reason)
}

// The catalog's copy of the plan follows an installed plan and not an
// earlier one.
func TestTheCatalogReadsTheInstalledPlan(t *testing.T) {
	inst, _ := opsPlanApp()
	first := inst.opsPlanView()
	assert.Same(t, first, inst.opsPlanView(), "one copy per installed plan")
	p := testPlan(table("default", "only"))
	inst.installPlan(&p)
	out, err := query[GetPlanArgs, PlanResult](t, inst, opGetPlan, GetPlanArgs{})
	require.NoError(t, err)
	require.Len(t, out.Tables, 1)
	assert.Equal(t, "only", out.Tables[0].Name)
}
