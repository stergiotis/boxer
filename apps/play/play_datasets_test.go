package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDatasetTestApp(t *testing.T) (*PlayApp, *Client) {
	t.Helper()
	client := NewClient(ClientConfig{URL: "http://example.invalid"}, nil)
	app := NewPlayApp(client, newLiveQueryGraph(client, memory.NewGoAllocator(), 4), "")
	return app, client
}

func TestValidDatasetIdentifier(t *testing.T) {
	for _, ok := range []string{"items", "a", "adhoc_deadbeef01234567", "A9_b"} {
		assert.True(t, validDatasetIdentifier(ok), ok)
	}
	for _, bad := range []string{"", "9lead", "has-dash", "has space", "x'y"} {
		assert.False(t, validDatasetIdentifier(bad), bad)
	}
}

func TestBindDatasetAndRewrite(t *testing.T) {
	app, client := newDatasetTestApp(t)

	require.NoError(t, app.BindDataset("items", "adhoc_deadbeef01234567"))
	require.Error(t, app.BindDataset("bad-alias", "adhoc_x"))
	require.Error(t, app.BindDataset("items", "bad-handle"))

	// The binding drives the client-side rewrite; unbound names pass through.
	got := client.rewriteDatasetAliases("SELECT * FROM keelson('items') ORDER BY x")
	assert.Contains(t, got, "keelson('adhoc_deadbeef01234567')")
	assert.NotContains(t, got, "'items'")
	assert.Equal(t, "SELECT * FROM keelson('env')", client.rewriteDatasetAliases("SELECT * FROM keelson('env')"))
}

func TestBindDatasetNeedsClient(t *testing.T) {
	app := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 4), "")
	require.Error(t, app.BindDataset("items", "adhoc_x"))
}

func TestNotifyDatasetRevisionAndRequestRun(t *testing.T) {
	app, _ := newDatasetTestApp(t)

	// Not live: a revision notification does not schedule a run.
	app.liveMain = false
	app.requestRun = false
	app.NotifyDatasetRevision("items", 2)
	assert.False(t, app.requestRun)

	// Live: it schedules the ordinary run.
	app.liveMain = true
	app.NotifyDatasetRevision("items", 3)
	assert.True(t, app.requestRun)

	// RequestRun always schedules.
	app.requestRun = false
	app.RequestRun()
	assert.True(t, app.requestRun)
}

func TestSetSignalDoesNotPanic(t *testing.T) {
	app, _ := newDatasetTestApp(t)
	app.SetSignal("sel", int64(42))
	app.SetSignal("name", "hello")
}
