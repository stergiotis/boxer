//go:build integration

package distsql_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/analytics/stats/distsql"
	"github.com/stergiotis/boxer/public/db/clickhouse/clickhouseenv"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
)

func liveClient(t *testing.T) (client *chclient.Client, ctx context.Context) {
	t.Helper()
	if clickhouseenv.Endpoint.Get() == "" && clickhouseenv.URL.Get() == "" {
		t.Skip("no ClickHouse endpoint configured (CLICKHOUSE_ENDPOINT / CLICKHOUSE_URL); skipping")
	}
	client = chclient.New(chclient.ConfigFromEnv(), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	require.NoError(t, client.Ping(ctx))
	return
}

func query(t *testing.T, ctx context.Context, client *chclient.Client, sql string) (out string) {
	t.Helper()
	body, err := client.Query(ctx, sql)
	require.NoError(t, err, sql)
	defer func() { _ = body.Close() }()
	b, err := io.ReadAll(body)
	require.NoError(t, err, sql)
	out = strings.TrimSpace(string(b))
	return
}

// TestIntegrationExpandDescriptiveStatistics is the ADR-0161 verification
// lane's live half: the expansion executes on a real server, and the
// Hyndman–Fan fixture pins estimator semantics against server upgrades —
// quantilesExactInclusive on [1,2,3,4] must give the type-7 values
// 1.75 / 2.5 / 3.25 at p = 0.25 / 0.5 / 0.75.
func TestIntegrationExpandDescriptiveStatistics(t *testing.T) {
	client, ctx := liveClient(t)

	t.Run("hyndman-fan type 7 fixture", func(t *testing.T) {
		expanded, err := distsql.ExpandDescriptiveStatistics.Run(
			"SELECT descriptiveStatistics('exact', x) FROM (SELECT arrayJoin([1,2,3,4]) AS x)")
		require.NoError(t, err)
		got := query(t, ctx, client,
			"SELECT qs[indexOf(ps, 0.25)], qs[indexOf(ps, 0.5)], qs[indexOf(ps, 0.75)], length(ps), estimator FROM ("+expanded+")")
		require.Equal(t, "1.75\t2.5\t3.25\t87\texact-hf7", got)
	})

	t.Run("tdigest default with grouping", func(t *testing.T) {
		expanded, err := distsql.ExpandDescriptiveStatistics.Run(
			"SELECT descriptiveStatistics(toFloat64(number)) FROM numbers(1000) GROUP BY number % 2")
		require.NoError(t, err)
		got := query(t, ctx, client,
			"SELECT count(), min(n), uniqExact(estimator) FROM ("+expanded+")")
		require.Equal(t, "2\t500\t1", got)
	})
}
