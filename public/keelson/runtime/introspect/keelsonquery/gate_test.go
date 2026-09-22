package keelsonquery

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
)

func testRegistry(t *testing.T) *introspect.Registry {
	t.Helper()
	r := introspect.NewRegistry()
	require.NoError(t, providers.RegisterStatic(r))
	return r
}

// The gate admits a statement over the granted table in either spelling,
// through a CTE and a subquery, and refuses everything that reaches past
// the grant — by table, by qualifier, by table function and by kind.
func TestGate(t *testing.T) {
	reg := testRegistry(t)
	admitted := []string{
		"SELECT name FROM keelson('env')",
		"SELECT name FROM env",
		"WITH e AS (SELECT name FROM keelson('env')) SELECT count() FROM e",
		"SELECT * FROM (SELECT name FROM env) WHERE name != ''",
		"SELECT a.name FROM env a JOIN env b ON a.name = b.name",
	}
	for _, sql := range admitted {
		bare, reason := Gate(reg, sql, "env")
		assert.Empty(t, reason, sql)
		assert.NotContains(t, bare, "keelson(", sql)
	}
	refused := map[string]string{
		"SELECT name FROM keelson('build')":                         "grants only env",
		"SELECT name FROM build":                                    "grants only env",
		"SELECT e.name FROM env e JOIN keelson('build') b ON 1 = 1": "grants only env",
		"SELECT name FROM system.tables":                            "grants only env",
		"SELECT * FROM url('http://127.0.0.1:1/x', 'JSONEachRow')":  "table function",
		"SELECT * FROM file('/etc/passwd', 'LineAsString')":         "table function",
		"INSERT INTO env SELECT * FROM env":                         "not provably read-only",
		"SELECT name FROM keelson('no_such_table')":                 "unknown keelson table",
		"SELECT FROM WHERE":                                         "",
	}
	for sql, want := range refused {
		bare, reason := Gate(reg, sql, "env")
		require.NotEmpty(t, reason, sql)
		assert.Empty(t, bare, sql)
		if want != "" {
			assert.Contains(t, reason, want, sql)
		}
	}
}

func TestSubjectsAndCaps(t *testing.T) {
	assert.Equal(t, "keelson.query.app_state", Subject("app_state"))
	caps := ClientCaps("watchbill_event", "watchbill_worker")
	require.Len(t, caps, 2)
	assert.Equal(t, "keelson.query.watchbill_event", caps[0].Pattern)
	assert.True(t, caps[0].Sticky)
	assert.Contains(t, caps[1].Reason, "watchbill_worker")
	svc := ServiceCaps("introspect")
	require.Len(t, svc, 3)
	assert.Equal(t, "ch.local.exec.introspect", svc[2].Pattern)
}
