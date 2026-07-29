package providersgui

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// TestWindowsTableRendersLaunchProvenance drives the table with fixed rows
// (no window host, no GUI): the three windows differ only in where their
// content came from, which is exactly what the launch_reason column exists
// to make queryable (ADR-0148 §SD5).
func TestWindowsTableRendersLaunchProvenance(t *testing.T) {
	ws := []windowhost.WindowInfo{
		{Key: 1, AppId: "test.a", LaunchReason: app.LaunchReasonPlain},
		{
			Key: 2, AppId: "test.b", LaunchReason: app.LaunchReasonCaller,
			ConfigKind: "testLaunch", ConfigBytes: 12,
		},
		{
			Key: 3, AppId: "test.b", LaunchReason: app.LaunchReasonRestore,
			ConfigKind: "testLaunch", ConfigBytes: 34, SharesInstance: true,
		},
	}
	rec := windowsTable(ws).Build(introspect.AllColumns(), len(ws))
	defer rec.Release()
	require.EqualValues(t, 3, rec.NumRows())

	reasons := stringColumn(t, rec, "launch_reason")
	assert.Equal(t, []string{"plain", "caller", "restore"}, reasons)

	kinds := stringColumn(t, rec, "config_kind")
	assert.Empty(t, kinds[0], "a plain open carries no config kind")
	assert.Equal(t, "testLaunch", kinds[1])

	sizes := rec.Column(colIndex(t, rec, "config_bytes")).(*array.Int64)
	assert.EqualValues(t, 0, sizes.Value(0))
	assert.EqualValues(t, 34, sizes.Value(2))

	shared := rec.Column(colIndex(t, rec, "shares_instance")).(*array.Boolean)
	assert.False(t, shared.Value(0))
	assert.True(t, shared.Value(2))
}

func TestWindowsProviderEmptyHost(t *testing.T) {
	// A nil host is the headless/no-window case: an empty table, not an
	// error and not an absent one.
	p := windowsProvider{}
	rec, err := p.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec.Release()
	assert.Zero(t, rec.NumRows())
	assert.EqualValues(t, p.Schema().NumFields(), rec.NumCols())
}

func colIndex(t *testing.T, rec arrow.RecordBatch, col string) int {
	t.Helper()
	idx := rec.Schema().FieldIndices(col)
	require.NotEmpty(t, idx, "column %q not found", col)
	return idx[0]
}

func stringColumn(t *testing.T, rec arrow.RecordBatch, col string) (out []string) {
	t.Helper()
	c := rec.Column(colIndex(t, rec, col)).(*array.String)
	out = make([]string, c.Len())
	for i := range out {
		out[i] = c.Value(i)
	}
	return
}
