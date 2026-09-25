package providers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// fakeTrail is an AppTrailReaderI over fixed rows, recording what it was
// asked for.
type fakeTrail struct {
	runs    []factsstore.AppRunRow
	logs    []factsstore.AppLogRow
	audit   []factsstore.AppAuditRow
	err     error
	filters []factsstore.AppTrailFilter
	perApp  uint32
}

func (inst *fakeTrail) AppRuns(ctx context.Context, f factsstore.AppTrailFilter) ([]factsstore.AppRunRow, error) {
	inst.filters = append(inst.filters, f)
	return inst.runs, inst.err
}
func (inst *fakeTrail) AppLogTail(ctx context.Context, f factsstore.AppTrailFilter, perApp uint32) ([]factsstore.AppLogRow, error) {
	inst.filters = append(inst.filters, f)
	inst.perApp = perApp
	return inst.logs, inst.err
}
func (inst *fakeTrail) AppAuditSummary(ctx context.Context, f factsstore.AppTrailFilter) ([]factsstore.AppAuditRow, error) {
	inst.filters = append(inst.filters, f)
	return inst.audit, inst.err
}

// trailFacts is a facts store that also reads the trail, as chstore does.
type trailFacts struct {
	factsstore.FactsStoreI
	*fakeTrail
}

func TestAppTrailTablesRenderRows(t *testing.T) {
	at := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	fake := &fakeTrail{
		runs:  []factsstore.AppRunRow{{RunId: "r1", AppId: "a", InstanceKey: 2, StartedAt: at, StopReason: ""}},
		logs:  []factsstore.AppLogRow{{Ts: at, AppId: "a", InstanceKey: 2, RunId: "r1", Level: "warn", Message: "m", Error: "e"}},
		audit: []factsstore.AppAuditRow{{AppId: "a", Subject: "s", Result: "ok", Requests: 3, FirstAt: at, LastAt: at, MeanLatencyMs: 1.5, MaxLatencyMs: 2}},
	}
	r := introspect.NewRegistry()
	require.NoError(t, RegisterAppTrail(r, trailFacts{FactsStoreI: factsstore.NewInMemoryFactsStore(), fakeTrail: fake}))
	for _, name := range []string{TableAppRuns, TableAppLogs, TableAppAudit} {
		p, ok := r.Lookup(name)
		require.True(t, ok, name)
		rec, err := p.Snapshot(introspect.AllColumns())
		require.NoError(t, err, name)
		assert.EqualValues(t, 1, rec.NumRows(), name)
		rec.Release()
	}
	require.Len(t, fake.filters, 3)
	for _, f := range fake.filters {
		assert.WithinDuration(t, time.Now().Add(-appTrailLookBack), f.Since, time.Minute, "every table reads over the look-back")
		assert.NotZero(t, f.Limit, "and under a cap")
	}
	assert.EqualValues(t, appLogsPerApp, fake.perApp)

	rec := appRunsTable(fake.runs).Build(introspect.AllColumns(), 1)
	defer rec.Release()
	stopped := rec.Column(rec.Schema().FieldIndices("stopped_ms")[0]).(*array.Int64)
	assert.Zero(t, stopped.Value(0), "an open session has no stop, not a 1970 one")
	started := rec.Column(rec.Schema().FieldIndices("started_ms")[0]).(*array.Int64)
	assert.Equal(t, at.UnixMilli(), started.Value(0))
	assert.Empty(t, appLogsTable(nil).Schema().FieldIndices("stack"), "the stack is never a column")
}

func TestAppTrailWithoutAReaderIsEmpty(t *testing.T) {
	r := introspect.NewRegistry()
	require.NoError(t, RegisterAppTrail(r, factsstore.NewInMemoryFactsStore()))
	require.NoError(t, RegisterAppTrail(introspect.NewRegistry(), nil))
	for _, name := range []string{TableAppRuns, TableAppLogs, TableAppAudit} {
		p, ok := r.Lookup(name)
		require.True(t, ok, name)
		rec, err := p.Snapshot(introspect.AllColumns())
		require.NoError(t, err)
		assert.Zero(t, rec.NumRows())
		rec.Release()
	}
}

func TestAppTrailReportsAFailedRead(t *testing.T) {
	r := introspect.NewRegistry()
	require.NoError(t, RegisterAppTrail(r, trailFacts{FactsStoreI: factsstore.NewInMemoryFactsStore(), fakeTrail: &fakeTrail{err: errors.New("down")}}))
	p, _ := r.Lookup(TableAppLogs)
	_, err := p.Snapshot(introspect.AllColumns())
	assert.ErrorContains(t, err, "down")
}
