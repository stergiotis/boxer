package play

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	passregdefaults "github.com/stergiotis/boxer/public/keelson/data/passreg/defaults"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// An agent's statement with EXISTS is classified, not refused for the
// grammar; the host's pass set sends it as the call the server holds it as,
// and that text is what the limits judged. A reach-out hidden in the
// predicate's query is refused like one anywhere else.
func TestAnAgentsExistsMeetsTheLimits(t *testing.T) {
	body := emptyArrowStream(t)
	var wire []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wire, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(ClientConfig{URL: srv.URL}, nil)
	reg := passreg.NewRegistry()
	require.NoError(t, passregdefaults.RegisterStandard(reg))
	require.NoError(t, RegisterPasses(reg))
	client.passes = reg
	obo := &app.OnBehalfOf{Task: "t", Destinations: []string{DestinationClickHouse(endpointHost(srv.URL))}}

	sql := "SELECT (SELECT count() FROM db.m AS m1 WHERE flag = 1 AND exists(SELECT 1 FROM db.m AS m2 WHERE m2.flag = 0 AND m2.k = m1.k)) AS same_k, " +
		"count() AS n FROM db.m AS o WHERE NOT EXISTS (SELECT 1 FROM db.x WHERE x.k = o.k AND x.v > {lo:UInt32})"
	require.NoError(t, refuseAgentStatement(client, obo, sql))
	slots, err := ExtractParamSlots(sql)
	require.NoError(t, err)
	require.Len(t, slots, 1, "the slot inside the EXISTS is the statement's parameter")
	assert.Equal(t, "lo", slots[0].Name)
	residual, _, _ := client.rewriteFor(obo, sql, nil)
	assert.Contains(t, residual, `"exists"((SELECT 1 FROM "db"."m" AS "m2" WHERE "m2"."flag" = 0 AND "m2"."k" = "m1"."k"))`)
	assert.Contains(t, residual, `NOT "exists"((SELECT 1 FROM "db"."x" WHERE "x"."k" = "o"."k" AND "x"."v" > {lo:UInt32}))`)

	rdr, closer, _, err := client.ExecuteArrowStream(context.Background(), sql, memory.NewGoAllocator(), nil, nil, client.Dispatch(sql, ""))
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })
	t.Cleanup(rdr.Release)
	assert.Contains(t, string(wire), residual, "the text sent is the text the limits judged")
	pr, err := nanopass.Parse(string(wire))
	require.NoError(t, err)
	class, _, err := analysis.ClassifyQuerySecurity(pr)
	require.NoError(t, err)
	assert.Equal(t, analysis.QuerySecurityRead, class)
	assert.ElementsMatch(t, []analysis.TableRef{{Database: "db", Table: "m"}, {Database: "db", Table: "m"}, {Database: "db", Table: "m"}, {Database: "db", Table: "x"}},
		analysis.ExtractTables(pr), "the text sent reads the tables the authored text reads")

	for hidden, witness := range map[string]string{
		"SELECT count() FROM t WHERE EXISTS (SELECT 1 FROM url('http://h/x', 'CSV', 'L UInt32'))":    "url",
		"SELECT exists(SELECT * FROM remote('h', 'db', 'u')) AS e":                                   "remote",
		"SELECT count() FROM t WHERE NOT exists(SELECT 1 WHERE file('/etc/hostname') != '')":         "file",
		"SELECT exists(SELECT generateSerialID('s'))":                                                "generateSerialID",
		"SELECT exists(SELECT 1 FROM t WHERE EXISTS (SELECT 1 FROM s3('http://h/x', 'CSV'))) FROM t": "s3",
	} {
		err := refuseAgentStatement(client, obo, hidden)
		require.Error(t, err, hidden)
		assert.Contains(t, err.Error(), "not a plain read ("+witness, hidden)
	}
	// Nothing but a query stands in the parentheses.
	for _, write := range []string{
		"SELECT exists(INSERT INTO t SELECT 1)",
		"SELECT exists(SELECT 1 INTO OUTFILE '/tmp/x')",
	} {
		err := refuseAgentStatement(client, obo, write)
		require.Error(t, err, write)
		assert.Contains(t, err.Error(), "boxer's grammar does not parse the statement", write)
	}
}

// A table named only inside an EXISTS is a table the statement names, to
// everything that decides where a run may go: the plain tables the resolver
// places, the keelson tables a grant must list, and the scope of a check.
// `exists(SELECT * FROM db.t)` counts: its words also read as an expression
// that names no table.
func TestATableNamedOnlyInAnExistsIsNamed(t *testing.T) {
	assert.Equal(t, []string{"db.t"}, plainTables("SELECT exists(SELECT * FROM db.t)"))
	assert.Equal(t, []string{"t"}, plainTables("SELECT 1 WHERE EXISTS (SELECT * FROM t)"))
	assert.Equal(t, []string{"db.u", "db.t"}, plainTables("SELECT a FROM db.u AS u WHERE NOT exists(SELECT 1 FROM db.t AS i WHERE i.k = u.k)"))

	// Keelson and plain tables in one statement have no endpoint that
	// serves both, whichever of them the EXISTS names.
	resolver := keelsonResolver{localEndpoint: func() string { return "http://127.0.0.1:1/query" }}
	for _, sql := range []string{
		"SELECT name FROM keelson('apps') WHERE exists(SELECT * FROM db.t)",
		"SELECT a FROM db.t WHERE EXISTS (SELECT 1 FROM keelson('apps'))",
	} {
		dec := resolver.resolve(sql, "http://db.example:8123/", "")
		assert.Equal(t, dispatchClassRefused, dec.class, sql)
	}

	// A keelson table inside an EXISTS needs its grant entry.
	local := dispatchDecision{class: dispatchClassIntrospection}
	narrow := &app.OnBehalfOf{Task: "t", Destinations: []string{DestinationKeelson("apps")}}
	sql := "SELECT name FROM keelson('apps') WHERE exists(SELECT * FROM keelson('windows'))"
	dec := resolver.resolve(sql, "http://db.example:8123/", "")
	assert.Equal(t, dispatchClassIntrospection, dec.class)
	err := checkAgentLimits(sql, local, narrow, nil)
	var limit *AgentLimitError
	require.ErrorAs(t, err, &limit)
	assert.Equal(t, DestinationKeelson("windows"), limit.Destination)

	// A check may name a table the result's statement read only there, and
	// no other.
	source := "SELECT sum(revenue) FROM sales.orders AS o WHERE EXISTS (SELECT 1 FROM sales.regions AS r WHERE r.region = o.region AND r.active)"
	assert.NoError(t, checkScope("SELECT count() FROM sales.regions WHERE active", source))
	assert.Error(t, checkScope("SELECT count() FROM sales.customers", source))
}
