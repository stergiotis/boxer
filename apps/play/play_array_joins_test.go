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

// An agent's statement with several ARRAY JOIN clauses on one FROM is
// classified, not refused for the grammar; the host's pass set sends it
// with its clauses apart and in their order, and that text is what the
// limits judged. A reach-out in a later clause is refused like one anywhere
// else, and a table named only there is named to what places a run.
func TestAnAgentsArrayJoinsMeetTheLimits(t *testing.T) {
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

	sql := "SELECT k, T, tau, count() AS n FROM db.p ARRAY JOIN [1.0, 1.5, 2.0] AS T ARRAY JOIN [0.5, 0.7] AS tau WHERE v > T * {lo:Float64} GROUP BY k, T, tau"
	require.NoError(t, refuseAgentStatement(client, obo, sql))
	residual, _, _ := client.rewriteFor(obo, sql, nil)
	assert.Contains(t, residual, `FROM "db"."p" ARRAY JOIN "array"(1.0, 1.5, 2.0) AS "T" ARRAY JOIN "array"(0.5, 0.7) AS "tau" WHERE`)

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

	for hidden, witness := range map[string]string{
		"SELECT x FROM t ARRAY JOIN a AS x ARRAY JOIN (SELECT groupArray(k) FROM url('http://h/x', 'CSV', 'k UInt8')) AS y":       "url",
		"SELECT x FROM t ARRAY JOIN a AS x LEFT ARRAY JOIN arrayFilter(i -> i IN (SELECT k FROM remote('h', 'db', 'u')), b) AS y": "remote",
		"SELECT x FROM t ARRAY JOIN a AS x ARRAY JOIN b AS y INNER ARRAY JOIN splitByChar(',', file('/etc/hostname')) AS z":       "file",
		"SELECT x FROM t ARRAY JOIN a AS x ARRAY JOIN [generateSerialID('s')] AS y":                                               "generateSerialID",
	} {
		err := refuseAgentStatement(client, obo, hidden)
		require.Error(t, err, hidden)
		assert.Contains(t, err.Error(), "not a plain read ("+witness, hidden)
	}

	assert.Equal(t, []string{"db.p", "db.u"},
		plainTables("SELECT x FROM db.p ARRAY JOIN a AS x ARRAY JOIN arrayFilter(i -> i IN (SELECT k FROM db.u), b) AS y"))
	source := "SELECT region FROM sales.orders ARRAY JOIN tags AS tag ARRAY JOIN (SELECT groupArray(region) FROM sales.regions) AS region"
	assert.NoError(t, checkScope("SELECT count() FROM sales.regions", source))
	assert.Error(t, checkScope("SELECT count() FROM sales.customers", source))
}
