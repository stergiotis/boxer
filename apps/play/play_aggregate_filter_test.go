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

// An agent's statement with an aggregate filter is classified, not refused
// for the grammar; the host's pass set sends it as the If call the server
// reads it as, and that text is what the limits judged. A reach-out hidden
// in the condition is refused like one anywhere else.
func TestAnAgentsAggregateFilterMeetsTheLimits(t *testing.T) {
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

	sql := "SELECT quantile(0.05)(L) FILTER (WHERE L BETWEEN 177 AND 1502) AS p05, count() FILTER (WHERE L > {lo:UInt32}) AS n FROM t"
	require.NoError(t, refuseAgentStatement(client, obo, sql))
	slots, err := ExtractParamSlots(sql)
	require.NoError(t, err)
	require.Len(t, slots, 1, "the condition's slot is the statement's parameter")
	assert.Equal(t, "lo", slots[0].Name)
	residual, _, _ := client.rewriteFor(obo, sql, nil)
	assert.Contains(t, residual, `"quantileIf"(0.05)("L", "L" BETWEEN 177 AND 1502)`)
	assert.Contains(t, residual, `"countIf"("L" > {lo:UInt32})`)

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
		"SELECT count() FILTER (WHERE L IN (SELECT L FROM url('http://h/x', 'CSV', 'L UInt32'))) FROM t": "url",
		"SELECT sum(L) FILTER (WHERE L > (SELECT max(L) FROM remote('h', 'db', 'u'))) OVER () FROM t":    "remote",
		"SELECT avg(L) FILTER (WHERE file('/etc/hostname') != '') FROM t":                                "file",
	} {
		err := refuseAgentStatement(client, obo, hidden)
		require.Error(t, err, hidden)
		assert.Contains(t, err.Error(), "not a plain read ("+witness, hidden)
	}
}

// A table the statement reads only inside an aggregate filter's condition is
// a table it read: a check may name it.
func TestACheckMayNameATableReadInAFilter(t *testing.T) {
	source := "SELECT sum(revenue) FILTER (WHERE region IN (SELECT region FROM sales.regions WHERE active)) FROM sales.orders"
	assert.NoError(t, checkScope("SELECT count() FROM sales.regions WHERE active", source))
	assert.Error(t, checkScope("SELECT count() FROM sales.customers", source))
}
