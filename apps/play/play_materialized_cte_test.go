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

// An agent's statement whose common table expressions are marked
// MATERIALIZED is classified, not refused for the grammar; the host's pass
// set sends it with the markers, read-only, and that text is what the
// limits judged. A reach-out in a marked body is refused like one anywhere
// else, and the marker stands only where the server reads it.
func TestAnAgentsMaterializedCTEMeetsTheLimits(t *testing.T) {
	body := emptyArrowStream(t)
	var wire []byte
	var readonly string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wire, _ = io.ReadAll(r.Body)
		readonly = r.URL.Query().Get("readonly")
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

	sql := "WITH a AS MATERIALIZED (SELECT k, v FROM db.m WHERE v > {lo:UInt32}),\n" +
		"b AS materialized (SELECT k, v * 2 AS d FROM a)\n" +
		"SELECT count() AS n, sum(d) AS s FROM b"
	require.NoError(t, refuseAgentStatement(client, obo, sql))
	slots, err := ExtractParamSlots(sql)
	require.NoError(t, err)
	require.Len(t, slots, 1, "the slot in a marked body is the statement's parameter")
	residual, _, _ := client.rewriteFor(obo, sql, nil)
	assert.Contains(t, residual, `WITH "a" AS MATERIALIZED (SELECT "k", "v" FROM "db"."m" WHERE "v" > {lo:UInt32}), "b" AS MATERIALIZED (SELECT "k", "v" * 2 AS "d" FROM "a")`)

	rdr, closer, _, err := client.ExecuteArrowStream(context.Background(), sql, memory.NewGoAllocator(), nil, nil, client.Dispatch(sql, ""))
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })
	t.Cleanup(rdr.Release)
	assert.Contains(t, string(wire), residual, "the text sent is the text the limits judged")
	assert.Equal(t, "2", readonly, "sent read-only like any statement of an agent")
	pr, err := nanopass.Parse(string(wire))
	require.NoError(t, err)
	class, _, err := analysis.ClassifyQuerySecurity(pr)
	require.NoError(t, err)
	assert.Equal(t, analysis.QuerySecurityRead, class)

	for hidden, witness := range map[string]string{
		"WITH a AS MATERIALIZED (SELECT k FROM url('http://h/x', 'CSV', 'k UInt32')) SELECT count() FROM a":                        "url",
		"WITH a AS MATERIALIZED (SELECT k FROM t), b AS MATERIALIZED (SELECT k FROM remote('h', 'db', 'u')) SELECT count() FROM a": "remote",
		"WITH a AS MATERIALIZED (SELECT file('/etc/hostname') AS k) SELECT k FROM a":                                               "file",
		"WITH a AS MATERIALIZED (SELECT generateSerialID('s') AS k) SELECT k FROM a":                                               "generateSerialID",
		"SELECT exists(WITH a AS MATERIALIZED (SELECT k FROM s3('http://h/x', 'CSV')) SELECT k FROM a)":                            "s3",
	} {
		err := refuseAgentStatement(client, obo, hidden)
		require.Error(t, err, hidden)
		assert.Contains(t, err.Error(), "not a plain read ("+witness, hidden)
	}
	for _, refused := range []string{
		"WITH a AS NOT MATERIALIZED (SELECT 1) SELECT * FROM a",
		"WITH a MATERIALIZED AS (SELECT 1) SELECT * FROM a",
		"WITH a AS MATERIALIZED (INSERT INTO t SELECT 1) SELECT 1",
	} {
		err := refuseAgentStatement(client, obo, refused)
		require.Error(t, err, refused)
		assert.Contains(t, err.Error(), "boxer's grammar does not parse the statement", refused)
	}
}

// A table named only in a marked body is named to what places a run, and
// the definitions' own names are not tables.
func TestATableInAMaterializedCTEIsNamed(t *testing.T) {
	assert.Equal(t, []string{"db.t"},
		plainTables("WITH a AS MATERIALIZED (SELECT k FROM db.t), b AS MATERIALIZED (SELECT k FROM a) SELECT count() FROM b"))

	resolver := keelsonResolver{localEndpoint: func() string { return "http://127.0.0.1:1/query" }}
	dec := resolver.resolve("WITH a AS MATERIALIZED (SELECT name FROM keelson('apps')) SELECT a.name FROM a INNER JOIN db.t AS t ON t.name = a.name", "http://db.example:8123/", "")
	assert.Equal(t, dispatchClassRefused, dec.class, "a keelson table in a marked body and a plain table have no endpoint that serves both")

	local := dispatchDecision{class: dispatchClassIntrospection}
	narrow := &app.OnBehalfOf{Task: "t", Destinations: []string{DestinationKeelson("apps")}}
	assert.NoError(t, checkAgentLimits("WITH a AS MATERIALIZED (SELECT name FROM keelson('apps')) SELECT count() FROM a", local, narrow, nil))
	err := checkAgentLimits("WITH a AS MATERIALIZED (SELECT name FROM keelson('windows')) SELECT count() FROM a", local, narrow, nil)
	var limit *AgentLimitError
	require.ErrorAs(t, err, &limit)
	assert.Equal(t, DestinationKeelson("windows"), limit.Destination)

	source := "WITH r AS MATERIALIZED (SELECT region FROM sales.regions WHERE active) SELECT sum(revenue) FROM sales.orders WHERE region IN (SELECT region FROM r)"
	assert.NoError(t, checkScope("SELECT count() FROM sales.regions WHERE active", source))
	assert.Error(t, checkScope("SELECT count() FROM sales.customers", source))
}
