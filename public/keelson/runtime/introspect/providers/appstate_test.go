package providers

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist/persiststore"
	"github.com/stergiotis/boxer/public/keelson/runtime/statestore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// TestAppStateRowOfEveryKind pins how each kind flattens into the one table
// (ADR-0185 §SD1): its own key, the payload's size but never its bytes, and
// the metadata a user needs to decide whether to clear it.
func TestAppStateRowOfEveryKind(t *testing.T) {
	owner := option.Some(persiststore.Owner{AppId: "play", RunId: "run-1", InstanceKey: 7})
	for _, c := range []struct {
		name string
		ent  persiststore.PersistEntity
		want appStateRow
	}{
		{"persist", persiststore.PersistEntity{ID: "state/play/tabs", Owner: owner,
			State: option.Some(persiststore.State{Key: "tabs", Value: []byte("hello")})},
			appStateRow{kind: "persist", key: "tabs", payloadBytes: 5}},
		{"workingset", persiststore.PersistEntity{ID: "ws/play/default", Owner: owner,
			Workingset: option.Some(persiststore.Workingset{Name: "default", Kind: "playLaunch", Config: make([]byte, 40), Reason: "user-close"})},
			appStateRow{kind: "workingset", key: "default", payloadBytes: 40, detail: "playLaunch · user-close"}},
		{"column width", persiststore.PersistEntity{ID: "cw/play/column//ab12", Owner: owner,
			ColumnWidth: option.Some(persiststore.ColumnWidth{Tier: "column", ColumnKey: "ab12", Points: 137.5, FontSize: 13})},
			appStateRow{kind: "column_width", key: "column//ab12", detail: "137.5 pt @ 13"}},
		// A kind this provider does not know is shown under its store key,
		// never dropped.
		{"unknown", persiststore.PersistEntity{ID: "zz/play/x", Owner: owner},
			appStateRow{kind: "unknown", key: "zz/play/x"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := appStateRowOf(&c.ent)
			c.want.appId, c.want.runId, c.want.instanceKey, c.want.entityId = "play", "run-1", 7, c.ent.ID
			assert.Equal(t, c.want, got)
		})
	}
}

// TestAppStateTableShape pins the columns, so ADR-0185 §SD7's "size, not
// bytes" cut cannot be widened by adding one.
func TestAppStateTableShape(t *testing.T) {
	names := make([]string, 0, 9)
	for _, f := range appStateTable(nil).Schema().Fields() {
		names = append(names, f.Name)
	}
	assert.Equal(t, []string{"kind", "app_id", "key", "entity_id", "payload_bytes", "detail", "written_at", "run_id", "instance_key"}, names)
}

// TestAppStateProviderWithoutAStore: no durable store means an empty table,
// not an absent one — the keelson('windows') precedent.
func TestAppStateProviderWithoutAStore(t *testing.T) {
	p := appStateProvider{}
	rec, err := p.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec.Release()
	assert.EqualValues(t, 0, rec.NumRows())
	assert.EqualValues(t, p.Schema().NumFields(), rec.NumCols())

	r := introspect.NewRegistry()
	require.NoError(t, RegisterAppState(r, nil))
	assert.Equal(t, []string{"app_state"}, r.Names())
}

// TestAppStateProviderReadsTheStore writes through the runtime's own state
// backend over clickhouse-local and reads the table back: every kind of two
// apps appears once, under its own app and kind; a superseded version is not
// a row; a deleted entry is absent — the resurrection the generated live
// scan exists to prevent, checked at the surface a user sees.
func TestAppStateProviderReadsTheStore(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	backend, err := persist.OpenStoreBackend(context.Background(), exec, nil)
	require.NoError(t, err)
	t.Cleanup(backend.Close)

	play := persist.StorageRef{Alias: "play", AppId: "github.com/example/play", InstanceKey: 3}
	require.NoError(t, backend.Set(play, "tabs", []byte("v1")))
	require.NoError(t, backend.Set(play, "tabs", []byte("v2-longer")))
	require.NoError(t, backend.Set(play, "gone", []byte("x")))
	require.NoError(t, backend.Delete(play, "gone"))
	past := time.Now().UTC().Add(-time.Minute)
	require.NoError(t, backend.WriteWorkingset(statestore.WorkingsetRow{
		AppId: "github.com/example/tally", Name: "default", Kind: "tallyLaunch",
		Config: []byte("cfg"), TileKey: 5, Reason: "shutdown", Ts: past,
	}))
	require.NoError(t, backend.WriteColumnWidth(statestore.ColumnWidthRow{
		AppId: "github.com/example/play", InstanceKey: 3, Tier: statestore.ColWidthTierColumn,
		ColumnKey: "ab12", Points: 90, FontSize: 12, Ts: past,
	}))

	rec, err := appStateProvider{persistExec: exec}.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec.Release()

	type seen struct{ kind, app, key, detail string }
	got := make([]seen, 0, rec.NumRows())
	var tabsBytes int64
	for i := 0; i < int(rec.NumRows()); i++ {
		s := seen{
			kind:   stringAt(t, rec, "kind", i),
			app:    stringAt(t, rec, "app_id", i),
			key:    stringAt(t, rec, "key", i),
			detail: stringAt(t, rec, "detail", i),
		}
		if s.key == "tabs" {
			tabsBytes = rec.Column(rec.Schema().FieldIndices("payload_bytes")[0]).(*array.Int64).Value(i)
		}
		got = append(got, s)
	}
	assert.Equal(t, []seen{
		{"column_width", "github.com/example/play", "column//ab12", "90 pt @ 12"},
		{"persist", "github.com/example/play", "tabs", ""},
		{"workingset", "github.com/example/tally", "default", "tallyLaunch · shutdown"},
	}, got, "ordered by app, then kind, then key; the deleted key is absent")
	assert.EqualValues(t, len("v2-longer"), tabsBytes, "the newer version is the live one")
}

func stringAt(t *testing.T, rec arrow.RecordBatch, col string, row int) string {
	t.Helper()
	idx := rec.Schema().FieldIndices(col)
	require.NotEmpty(t, idx, "column %q not found", col)
	return rec.Column(idx[0]).(*array.String).Value(row)
}
