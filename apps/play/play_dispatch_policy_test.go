package play

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testBase  = "http://server.invalid:8123/"
	testLocal = "http://127.0.0.1:9999/query"
)

func testKeelsonResolver(local string) keelsonResolver {
	return keelsonResolver{localEndpoint: func() string { return local }}
}

func TestKeelsonResolverRouting(t *testing.T) {
	cases := []struct {
		name       string
		sql        string
		local      string
		wantClass  dispatchClassE
		wantURL    string
		reasonHas  string
		reasonLack string
	}{
		{
			name:      "keelson only goes to the introspection plane",
			sql:       "SELECT name FROM keelson('env')",
			local:     testLocal,
			wantClass: dispatchClassIntrospection,
			wantURL:   testLocal,
			reasonHas: "env",
		},
		{
			name:      "several keelson tables still route",
			sql:       "SELECT * FROM keelson('env') JOIN keelson('apps') ON 1",
			local:     testLocal,
			wantClass: dispatchClassIntrospection,
			wantURL:   testLocal,
			reasonHas: "env, apps",
		},
		{
			name:      "a CTE over keelson data is not a plain table",
			sql:       "WITH e AS (SELECT * FROM keelson('env')) SELECT * FROM e",
			local:     testLocal,
			wantClass: dispatchClassIntrospection,
			wantURL:   testLocal,
		},
		{
			name:      "a subquery over keelson data is not a plain table",
			sql:       "SELECT * FROM (SELECT name FROM keelson('env'))",
			local:     testLocal,
			wantClass: dispatchClassIntrospection,
			wantURL:   testLocal,
		},
		{
			name:      "system.* carries no placement meaning",
			sql:       "SELECT * FROM keelson('env') JOIN system.one ON 1",
			local:     testLocal,
			wantClass: dispatchClassIntrospection,
			wantURL:   testLocal,
		},
		{
			name:      "the keelson database is not the keelson macro",
			sql:       "SELECT * FROM keelson.env",
			local:     testLocal,
			wantClass: dispatchClassManual,
			wantURL:   testBase,
			reasonHas: "no keelson tables",
		},
		{
			name:      "keelson only, but nothing published, stays put and says why",
			sql:       "SELECT name FROM keelson('env')",
			local:     "",
			wantClass: dispatchClassManual,
			wantURL:   testBase,
			reasonHas: "publishes no introspection endpoint",
		},
		{
			name:      "plain only stays on the pinned endpoint",
			sql:       "SELECT * FROM db.events WHERE a > 1",
			local:     testLocal,
			wantClass: dispatchClassManual,
			wantURL:   testBase,
		},
		{
			name:      "no tables at all stays put",
			sql:       "SELECT 1",
			local:     testLocal,
			wantClass: dispatchClassManual,
			wantURL:   testBase,
		},
		{
			name:      "unparseable stays put",
			sql:       "NOT SQL ((",
			local:     testLocal,
			wantClass: dispatchClassManual,
			wantURL:   testBase,
		},
		{
			name:      "mixed refuses, naming both sides",
			sql:       "SELECT * FROM keelson('env') JOIN db.events ON 1",
			local:     testLocal,
			wantClass: dispatchClassRefused,
			wantURL:   "",
			reasonHas: "no endpoint serves both",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec := testKeelsonResolver(tc.local).resolve(tc.sql, testBase, "")
			assert.Equal(t, tc.wantClass, dec.class, "reason=%q", dec.reason)
			assert.Equal(t, tc.wantURL, dec.targetURL)
			assert.NotEmpty(t, dec.reason, "every decision carries a reason")
			if tc.reasonHas != "" {
				assert.Contains(t, dec.reason, tc.reasonHas)
			}
			if tc.reasonLack != "" {
				assert.NotContains(t, dec.reason, tc.reasonLack)
			}
		})
	}
}

// TestKeelsonResolverMixedNamesBothSides pins the message content: a
// refusal that does not say what collided leaves the user with nothing to
// act on.
func TestKeelsonResolverMixedNamesBothSides(t *testing.T) {
	dec := testKeelsonResolver(testLocal).resolve(
		"SELECT * FROM keelson('env') JOIN db.events ON 1", testBase, "")
	require.Equal(t, dispatchClassRefused, dec.class)
	assert.Contains(t, dec.reason, "env")
	assert.Contains(t, dec.reason, "db.events")
}

// TestKeelsonResolverNeverRoutesNonReads is the R5 wall. A mutation
// addresses a host somebody chose; a router must not choose for it, and an
// unknown kind gets the same treatment as a known mutation.
func TestKeelsonResolverNeverRoutesNonReads(t *testing.T) {
	// Each of these names keelson tables, so only the statement kind can
	// keep them from being rerouted.
	for _, sql := range []string{
		"INSERT INTO keelson('env') VALUES (1)",
		"ALTER TABLE t DELETE WHERE 1",
		"KILL QUERY WHERE query_id='x'",
		"OPTIMIZE TABLE t FINAL",
		"SET max_threads=1",
		"GRANT SELECT ON db.* TO u",
		"nonsense keelson('env')",
	} {
		dec := testKeelsonResolver(testLocal).resolve(sql, testBase, "")
		assert.Equal(t, dispatchClassManual, dec.class, "sql=%q", sql)
		assert.Equal(t, testBase, dec.targetURL, "sql=%q", sql)
		assert.Contains(t, dec.reason, "not provably read-only", "sql=%q", sql)
	}
}

func TestPlainTables(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want []string
	}{
		{"bare table", "SELECT * FROM t", []string{"t"}},
		{"qualified", "SELECT * FROM db.t", []string{"db.t"}},
		{"join", "SELECT * FROM a JOIN b ON 1", []string{"a", "b"}},
		{"alias does not duplicate", "SELECT * FROM t AS x", []string{"t"}},
		{"column qualifier is not a table", "SELECT t.id FROM t", []string{"t"}},
		{"cte name excluded, cte body included", "WITH e AS (SELECT * FROM src) SELECT * FROM e", []string{"src"}},
		{"subquery in from", "SELECT * FROM (SELECT * FROM inner_t)", []string{"inner_t"}},
		{"table function excluded", "SELECT * FROM numbers(10)", nil},
		{"keelson macro excluded", "SELECT * FROM keelson('env')", nil},
		{"system ignored", "SELECT * FROM system.tables", nil},
		{"keelson database ignored", "SELECT * FROM keelson.env", nil},
		{"union covers both members", "SELECT * FROM a UNION ALL SELECT * FROM b", []string{"a", "b"}},
		{"deduplicated", "SELECT * FROM t JOIN t AS t2 ON 1", []string{"t"}},
		{"unparseable", "NOT SQL ((", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, plainTables(tc.sql))
		})
	}
}

func TestNameListElides(t *testing.T) {
	assert.Equal(t, "", nameList(nil))
	assert.Equal(t, "a, b, c", nameList([]string{"a", "b", "c"}))
	assert.Equal(t, "a, b, c, …", nameList([]string{"a", "b", "c", "d"}))
}
