package play

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	passregdefaults "github.com/stergiotis/boxer/public/keelson/data/passreg/defaults"
)

// Two leeway schemas as a real endpoint reports them, differing in which
// tagged section the table carries. Which one a handle resolves against is
// therefore observable in the SQL that ships.
var (
	schemaWithSymbol = []string{
		"id:id:u64:47::0:",
		"id:naturalKey:y:4::0:",
		"tv:symbol:value:val:s:124::I:0::data",
		"tv:symbol:hr:hr:u64:47:::0::data",
		"tv:symbol:lr:lr:u64:1247:::0::data",
		"tv:symbol:lmr:lmr:u64:1247:::0::data",
		"tv:symbol:mrhp:mrhp:y:4:::0::data",
		"tv:symbol:hrcard:hrcard:u64:4E:::0::data",
		"tv:symbol:lrcard:lrcard:u64:4E:::0::data",
		"tv:symbol:lmrcard:lmrcard:u64:4E:::0::data",
	}
	schemaWithForeignKey = []string{
		"id:id:u64:47::0:",
		"id:naturalKey:y:4::0:",
		"tv:foreignKey:value:val:u64:4:M::0::foreignKey",
		"tv:foreignKey:lr:lr:u64:1247:M::0::foreignKey",
		"tv:foreignKey:lrcard:lrcard:u64:4E:M::0::foreignKey",
	}
)

// schemaEndpoint serves the system.columns probe with one fixed column list and
// counts how often it was asked.
func schemaEndpoint(t *testing.T, names []string) (url string, probes *atomic.Int64) {
	t.Helper()
	probes = &atomic.Int64{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		probes.Add(1)
		_, _ = w.Write([]byte(strings.Join(names, "\n") + "\n"))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, probes
}

// schemaSwitchClient is a Client with the standard pre-execute set in its own
// registry (the host wires passreg.Default at startup, which a test does not)
// and the leeway resolver installed against its live endpoint.
func schemaSwitchClient(t *testing.T, url string) *Client {
	t.Helper()
	reg := passreg.NewRegistry()
	require.NoError(t, passregdefaults.RegisterStandard(reg))
	client := NewClient(ClientConfig{URL: url}, nil)
	client.passes = reg
	installLeewayNameResolution(client)
	return client
}

// TestSetURLClearsSchemaCaches: the endpoint switcher repoints the client, and
// the schema knowledge cached for the endpoint it left has to go with it.
// Nothing else invalidates it — lwsql.Resolver's derived index has no expiry —
// so before this a retargeted session kept resolving handles against the
// previous server, which is a wrong answer rather than an error.
func TestSetURLClearsSchemaCaches(t *testing.T) {
	urlA, probesA := schemaEndpoint(t, schemaWithSymbol)
	urlB, probesB := schemaEndpoint(t, schemaWithForeignKey)

	client := schemaSwitchClient(t, urlA)

	resolves := func(handle string) bool {
		t.Helper()
		out, _ := client.buildResidual("SELECT `" + handle + "` FROM facts")
		return !strings.Contains(out, "`"+handle+"`")
	}

	// On A the symbol section exists and foreignKey does not.
	require.True(t, resolves("symbol:value"), "A carries symbol")
	require.False(t, resolves("foreignKey:value"), "A does not carry foreignKey")
	require.Positive(t, probesA.Load(), "A should have been probed")
	require.Zero(t, probesB.Load(), "B must not be probed while A is the target")

	// Re-pinning the same endpoint is not a switch: the caches stay warm.
	probesBefore := probesA.Load()
	client.SetURL(urlA)
	require.True(t, resolves("symbol:value"))
	require.Equal(t, probesBefore, probesA.Load(), "re-pinning the current target must not re-probe")

	// The switch. Both verdicts must flip, which they can only do if the
	// derived index AND the column list it was built from were dropped.
	client.SetURL(urlB)
	require.True(t, resolves("foreignKey:value"), "after the switch, B's sections resolve")
	require.False(t, resolves("symbol:value"), "after the switch, A's sections must not")
	require.Positive(t, probesB.Load(), "B should have been probed after the switch")

	// And back, to pin that this is not a one-way flush.
	client.SetURL(urlA)
	require.True(t, resolves("symbol:value"))
	require.False(t, resolves("foreignKey:value"))
}

// TestSetURLEmptyKeepsSchemaCaches: an ignored SetURL must not flush either —
// the target did not change, so neither did what the caches describe.
func TestSetURLEmptyKeepsSchemaCaches(t *testing.T) {
	urlA, probesA := schemaEndpoint(t, schemaWithSymbol)
	client := schemaSwitchClient(t, urlA)

	_, _ = client.buildResidual("SELECT `symbol:value` FROM facts")
	before := probesA.Load()
	require.Positive(t, before)

	client.SetURL("")
	_, _ = client.buildResidual("SELECT `symbol:value` FROM facts")
	require.Equal(t, before, probesA.Load(), "an ignored SetURL must not drop the caches")
}
