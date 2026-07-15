package introspectengine

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file drives the package_capabilities table (ADR-0120 SD8) end to end
// through real SQL, rather than asserting against the provider in isolation.
// The queries here are the ones the table exists to answer.

// TestQuery_PackageCapsViaKeelsonMacro is the headline: the capability verdicts
// of everything linked into this binary are reachable as SQL.
func TestQuery_PackageCapsViaKeelsonMacro(t *testing.T) {
	e := newEngineWithBroker(t)
	body, _, err := e.Query(context.Background(),
		"SELECT count() FROM keelson('package_capabilities')", "TabSeparated")
	require.NoError(t, err)
	n, err := strconv.Atoi(strings.TrimSpace(string(body)))
	require.NoError(t, err, "body: %q", string(body))
	// This test binary links packageprops declarations transitively; the exact
	// count depends on the link graph, so only non-emptiness is asserted.
	assert.Positive(t, n)
}

// TestQuery_PackageCapsFindsExecutors is the security question the table exists
// for: which linked packages can run an external process? extbin is the tree's
// exec chokepoint (ADR-0118) and is linked here via the introspect providers, so
// it must appear.
func TestQuery_PackageCapsFindsExecutors(t *testing.T) {
	e := newEngineWithBroker(t)
	body, _, err := e.Query(context.Background(),
		"SELECT import_path FROM keelson('package_capabilities') "+
			"WHERE has(caps_direct, 'exec') ORDER BY import_path", "TabSeparated")
	require.NoError(t, err)
	s := string(body)
	assert.Contains(t, s, "github.com/stergiotis/boxer/public/extbin",
		"extbin holds exec directly and is linked into this test binary")
}

// TestQuery_PackageCapsNegativeClaim exercises the reason the reachable closure
// is stored at all (ADR-0120 SD5). As a positive claim it saturates; as a
// negative one it proves a package cannot reach a capability by any path.
func TestQuery_PackageCapsNegativeClaim(t *testing.T) {
	e := newEngineWithBroker(t)
	body, _, err := e.Query(context.Background(),
		"SELECT count() FROM keelson('package_capabilities') "+
			"WHERE surveyed AND NOT has(caps_reachable, 'network')", "TabSeparated")
	require.NoError(t, err)
	n, err := strconv.Atoi(strings.TrimSpace(string(body)))
	require.NoError(t, err, "body: %q", string(body))
	assert.Positive(t, n, "some linked packages must be provably unable to reach the network")
}

// TestQuery_PackageCapsArrayJoinsToLongForm checks the shape decision in
// ADR-0120 SD8: the table is stored wide (one row per package) because arrayJoin
// recovers the long form, while the reverse would lose the row for a package
// with no capabilities.
func TestQuery_PackageCapsArrayJoinsToLongForm(t *testing.T) {
	e := newEngineWithBroker(t)
	body, _, err := e.Query(context.Background(),
		"SELECT capability, count() AS n FROM ("+
			"  SELECT arrayJoin(caps_direct) AS capability FROM keelson('package_capabilities')"+
			") GROUP BY capability ORDER BY n DESC, capability", "TabSeparated")
	require.NoError(t, err)
	s := strings.TrimSpace(string(body))
	require.NotEmpty(t, s)
	assert.Contains(t, s, "safe", "most packages reach nothing privileged directly")
	t.Logf("direct capability histogram over the linked packages:\n%s", s)
}

// TestQuery_PackageCapsSurveyedDistinguishesUnsurveyed guards the ADR-0120 SD4
// encoding across the SQL boundary: an unsurveyed package must not be
// indistinguishable from a safe one, or a query for "safe packages" would
// silently include packages nobody ever looked at.
func TestQuery_PackageCapsSurveyedDistinguishesUnsurveyed(t *testing.T) {
	e := newEngineWithBroker(t)
	body, _, err := e.Query(context.Background(),
		"SELECT count() FROM keelson('package_capabilities') WHERE safe AND NOT surveyed",
		"TabSeparated")
	require.NoError(t, err)
	n, err := strconv.Atoi(strings.TrimSpace(string(body)))
	require.NoError(t, err, "body: %q", string(body))
	assert.Zero(t, n, "safe must imply surveyed — an unsurveyed package asserts nothing")
}
