package play

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/play/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// The endpoint switcher's "External (reset)" preset, and the invariant that
// makes it mean anything: externalURL names the external server, not whatever
// the window happens to be pointed at.

const (
	testExternalURL = "http://external.invalid:8123/"
	testLocalURL    = "http://127.0.0.1:59999/query"
)

// withLocalQueryEndpoint publishes an in-process introspection /query URL for
// the duration of the test, restoring whatever was there before. The registry
// is process-global, so leaking one would retarget every later test that opens
// with EndpointIntrospection.
func withLocalQueryEndpoint(t *testing.T, url string) {
	t.Helper()
	prev := introspect.LocalQueryEndpoint()
	introspect.SetLocalQueryEndpoint(url)
	t.Cleanup(func() { introspect.SetLocalQueryEndpoint(prev) })
}

// TestMount_IntrospectionLaunchKeepsExternalResetTarget is the regression: a
// window opened on the introspection plane (ADR-0135 §SD7) must still know
// which server "External (reset)" goes back to.
//
// The bug it pins: externalURL was read off the client *after* the retarget,
// so it named the introspection endpoint — the reset button pinned the URL
// already pinned, changed nothing, and left the user on the local plane with
// no way back through the menu. A `system.*` query then answers from
// clickhouse-local's own system tables rather than the server's, which is a
// wrong answer rather than an error.
func TestMount_IntrospectionLaunchKeepsExternalResetTarget(t *testing.T) {
	t.Setenv("CLICKHOUSE_URL", testExternalURL)
	withLocalQueryEndpoint(t, testLocalURL)

	cfg := encodePlayLaunch(t, launchcfg.PlayLaunch{
		Sql:      "SELECT 1",
		Endpoint: launchcfg.EndpointIntrospection,
	})
	inst, err := mountLauncher(t, cfg, mapStorage{})
	require.NoError(t, err)

	// The retarget itself still happens — that part was never broken.
	require.Equal(t, testLocalURL, inst.inner.client.URL(),
		"an EndpointIntrospection open binds the client to the local plane")
	assert.Equal(t, testLocalURL, inst.inner.endpointDraft,
		"the draft reflects where the window actually opened")

	// ...and the way out is still named.
	assert.Equal(t, testExternalURL, inst.inner.externalURL,
		`"External (reset)" must name the external server, not the retarget`)

	// The gesture the user could not perform.
	inst.inner.client.SetURL(inst.inner.externalURL)
	assert.Equal(t, testExternalURL, inst.inner.client.URL())
}

// TestMount_PlainOpenExternalIsTheEnvTarget: with no retarget, the external
// target is simply where the window opened.
func TestMount_PlainOpenExternalIsTheEnvTarget(t *testing.T) {
	t.Setenv("CLICKHOUSE_URL", testExternalURL)
	withLocalQueryEndpoint(t, testLocalURL)

	inst, err := mountLauncher(t, nil, mapStorage{})
	require.NoError(t, err)

	assert.Equal(t, testExternalURL, inst.inner.client.URL())
	assert.Equal(t, testExternalURL, inst.inner.externalURL)
}

// TestMount_IntrospectionLaunchWithoutLocalEndpointDegrades: the launch config
// asks for a plane no one published. Per §SD7 that is a degraded open, not a
// failed one — and the external target is untouched, so the switcher is in the
// same state a plain open leaves it.
func TestMount_IntrospectionLaunchWithoutLocalEndpointDegrades(t *testing.T) {
	t.Setenv("CLICKHOUSE_URL", testExternalURL)
	withLocalQueryEndpoint(t, "")

	cfg := encodePlayLaunch(t, launchcfg.PlayLaunch{
		Sql:      "SELECT 1",
		Endpoint: launchcfg.EndpointIntrospection,
	})
	inst, err := mountLauncher(t, cfg, mapStorage{})
	require.NoError(t, err)

	assert.Equal(t, testExternalURL, inst.inner.client.URL())
	assert.Equal(t, testExternalURL, inst.inner.externalURL)
}

// TestSetEndpointRefusesEmpty: Client.SetURL ignores an empty target, so
// applying one must not turn Auto off around a pin that never happened. The
// guard returns before any frontend override, which is what keeps this
// testable off the render thread.
func TestSetEndpointRefusesEmpty(t *testing.T) {
	inst := &PlayApp{
		client:        NewClient(ClientConfig{URL: testExternalURL}, nil),
		endpointDraft: testExternalURL,
		externalURL:   testExternalURL,
		autoEndpoint:  true,
	}

	inst.setEndpoint("")

	assert.Equal(t, testExternalURL, inst.client.URL(), "target unchanged")
	assert.True(t, inst.autoEndpoint, "Auto survives a pin that did not happen")
}
