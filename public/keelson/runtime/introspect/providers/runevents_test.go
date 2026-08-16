package providers

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// TestRunEventsTableRendersRow drives the table with fixed rows (no store),
// the workingsets precedent.
//
// The timestamp is epoch milliseconds rather than a formatted string, and
// that is the assertion worth making: the consumer plots it, so it needs a
// number it can add a mark width to, not a string it has to parse back.
func TestRunEventsTableRendersRow(t *testing.T) {
	at := time.Date(2026, 8, 16, 12, 34, 56, 0, time.UTC)
	rows := []factsstore.RunEventRow{{
		Ts: at, Kind: "lifecycle", AppId: "github.com/example/play",
		InstanceKey: 3, RunId: "run-abc", Detail: "started",
		Source: factsstore.RunEventSourceFacts, FactId: "17",
	}}
	rec := runEventsTable(rows).Build(introspect.AllColumns(), len(rows))
	defer rec.Release()

	require.EqualValues(t, 1, rec.NumRows())
	assert.Equal(t, "lifecycle", firstString(t, rec, "kind"))
	assert.Equal(t, "github.com/example/play", firstString(t, rec, "app_id"))
	assert.Equal(t, "run-abc", firstString(t, rec, "run_id"))
	assert.Equal(t, "started", firstString(t, rec, "detail"))
	assert.Equal(t, "facts", firstString(t, rec, "source"))
	assert.Equal(t, "17", firstString(t, rec, "fact_id"))

	tsIdx := rec.Schema().FieldIndices("ts_ms")
	require.Len(t, tsIdx, 1)
	tsCol, ok := rec.Column(tsIdx[0]).(*array.Int64)
	require.True(t, ok, "ts_ms must be an integer a consumer can do arithmetic on")
	assert.Equal(t, at.UnixMilli(), tsCol.Value(0))

	keyIdx := rec.Schema().FieldIndices("instance_key")
	require.Len(t, keyIdx, 1)
	keyCol, ok := rec.Column(keyIdx[0]).(*array.Uint64)
	require.True(t, ok)
	assert.Equal(t, uint64(3), keyCol.Value(0))
}

// TestRunEventsProviderWithoutAReader is the degradation contract. A host on
// the in-memory facts store cannot answer this — the flattening is the
// ClickHouse store's — and the table must then be EMPTY rather than absent,
// so the set of keelson table names does not depend on which backend a
// deployment wired.
func TestRunEventsProviderWithoutAReader(t *testing.T) {
	p := runEventsProvider{reader: nil, persistExec: nil}
	rec, err := p.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec.Release()
	assert.EqualValues(t, 0, rec.NumRows())
	assert.NotEmpty(t, rec.Schema().Fields(), "an empty table still declares its shape")
}

// TestRunEventsProviderMergesSourcesByTime pins the merge. The two halves are
// read separately, so without an explicit sort every persist row would follow
// every facts row regardless of when it happened — and a timeline drawn from
// that is not wrong in a way a reader would notice, which is the dangerous
// kind.
func TestRunEventsProviderMergesSourcesByTime(t *testing.T) {
	t0 := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	rows := []factsstore.RunEventRow{
		{Ts: t0.Add(3 * time.Second), Kind: "audit", Source: factsstore.RunEventSourceFacts},
		{Ts: t0.Add(1 * time.Second), Kind: "persist", Source: factsstore.RunEventSourcePersist},
		{Ts: t0.Add(2 * time.Second), Kind: "lifecycle", Source: factsstore.RunEventSourceFacts},
	}
	sortRunEventsByTime(rows)
	assert.Equal(t, []string{"persist", "lifecycle", "audit"},
		[]string{rows[0].Kind, rows[1].Kind, rows[2].Kind})
}

// TestRunEventsRegistered pins the table into the introspection registry, so
// keelson('runtime_events') resolves rather than reporting an unknown table.
// Registered with a nil store on purpose: the name must be there either way.
func TestRunEventsRegistered(t *testing.T) {
	reg := introspect.NewRegistry()
	require.NoError(t, RegisterRunEvents(reg, nil, nil))
	_, ok := reg.Lookup("runtime_events")
	assert.True(t, ok, "keelson('runtime_events') must resolve")
}
