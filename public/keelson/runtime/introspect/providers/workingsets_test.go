package providers

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// TestWorkingsetsTableRendersRecord drives the table with fixed rows (no
// store), the extbinTable precedent. The config bytes are reported as a size,
// never as a column — they are facts-CBOR only the owning app's codec can
// read (ADR-0148 §SD7).
func TestWorkingsetsTableRendersRecord(t *testing.T) {
	saved := time.Date(2026, 7, 30, 12, 34, 56, 0, time.UTC)
	rows := []factsstore.WorkingsetRow{
		{
			RunId: "run-abc", AppId: "github.com/example/play", Name: "default",
			Kind: "playLaunch", Config: []byte("0123456789"), TileKey: 42,
			Reason: "user-close", Ts: saved,
		},
	}
	rec := workingsetsTable(rows).Build(introspect.AllColumns(), len(rows))
	defer rec.Release()
	require.EqualValues(t, 1, rec.NumRows())
	assert.Equal(t, "github.com/example/play", firstString(t, rec, "app_id"))
	assert.Equal(t, "default", firstString(t, rec, "name"))
	assert.Equal(t, "playLaunch", firstString(t, rec, "kind"))
	assert.Equal(t, "user-close", firstString(t, rec, "reason"))
	assert.Equal(t, "run-abc", firstString(t, rec, "run_id"))
	assert.Equal(t, "2026-07-30T12:34:56Z", firstString(t, rec, "saved_at"))

	sizeIdx := rec.Schema().FieldIndices("config_bytes")
	require.Len(t, sizeIdx, 1)
	assert.EqualValues(t, 10, rec.Column(sizeIdx[0]).(*array.Int64).Value(0))
	tileIdx := rec.Schema().FieldIndices("tile_key")
	require.Len(t, tileIdx, 1)
	assert.EqualValues(t, 42, rec.Column(tileIdx[0]).(*array.Uint64).Value(0))
}

// TestWorkingsetsProviderReadsStore checks the provider serves what the store
// holds, in the store's (app, name) order, and that an AllColumns snapshot
// carries every declared column.
func TestWorkingsetsProviderReadsStore(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	_, err := store.WriteWorkingset(factsstore.WorkingsetRow{
		AppId: "play", Name: "default", Kind: "playLaunch", Config: []byte("sql"),
	})
	require.NoError(t, err)
	_, err = store.WriteWorkingset(factsstore.WorkingsetRow{
		AppId: "imztop", Name: "default", Kind: "imztopLaunch",
	})
	require.NoError(t, err)

	p := workingsetsProvider{facts: store}
	rec, err := p.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec.Release()
	require.EqualValues(t, 2, rec.NumRows())
	assert.EqualValues(t, p.Schema().NumFields(), rec.NumCols(),
		"an AllColumns snapshot must carry every schema column")
	assert.Equal(t, "imztop", firstString(t, rec, "app_id"), "ordered by app id")
}

// TestWorkingsetsProviderNilStore covers the headless / no-facts host: the
// table is empty rather than absent, the keelson('windows') precedent.
func TestWorkingsetsProviderNilStore(t *testing.T) {
	p := workingsetsProvider{}
	rec, err := p.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec.Release()
	assert.EqualValues(t, 0, rec.NumRows())
	assert.EqualValues(t, p.Schema().NumFields(), rec.NumCols())
}

// TestWorkingsetsProviderSurfacesStoreError pins the one place this provider
// differs from its neighbours: a failed read is reported, not rendered as an
// empty table. "No records stored" and "the store did not answer" are
// different claims about restorable state, and only one of them is safe to
// silently report.
func TestWorkingsetsProviderSurfacesStoreError(t *testing.T) {
	p := workingsetsProvider{facts: failingWorkingsetStore{factsstore.NewInMemoryFactsStore()}}
	_, err := p.Snapshot(introspect.AllColumns())
	require.Error(t, err)
}

// failingWorkingsetStore embeds the in-memory store so only the one method
// under test needs an override.
type failingWorkingsetStore struct {
	*factsstore.InMemoryFactsStore
}

func (failingWorkingsetStore) ListWorkingsets() (rows []factsstore.WorkingsetRow, err error) {
	err = eh.Errorf("workingsets: transport is down")
	return
}

func TestRegisterWorkingsets(t *testing.T) {
	r := introspect.NewRegistry()
	require.NoError(t, RegisterWorkingsets(r, factsstore.NewInMemoryFactsStore()))
	assert.Equal(t, []string{"workingsets"}, r.Names())
}
