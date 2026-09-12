//go:build integration

package lwsqlsurface_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/clickhouseenv"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/semistructured/leeway/chviews"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
)

// The installer's Conn is deliberately client-free; this is the one place
// that asserts the intended client satisfies it.
var _ lwsqlsurface.Conn = (*chclient.Client)(nil)

func liveClient(t *testing.T) (client *chclient.Client, ctx context.Context) {
	t.Helper()
	if clickhouseenv.Endpoint.Get() == "" && clickhouseenv.URL.Get() == "" {
		t.Skip("no ClickHouse endpoint configured (CLICKHOUSE_ENDPOINT / CLICKHOUSE_URL); skipping")
	}
	client = chclient.New(chclient.ConfigFromEnv(), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	require.NoError(t, client.Ping(ctx))
	return
}

// TestIntegrationSurfaceInstall is ADR-0171 §SD2's verification lane: the
// handshake on a real server, which is the one thing clickhouse-local
// cannot stand in for — a live server has its own builtins, its own
// user-defined functions from whoever else uses it, and persistent state
// across the test's own runs.
//
// Reconcile runs in report mode only. This lane may target a shared server,
// and dropping there would delete work that is not ours — which is the
// decision §SD2 records, exercised here rather than described.
func TestIntegrationSurfaceInstall(t *testing.T) {
	client, ctx := liveClient(t)

	require.NoError(t, lwsqlsurface.Install(ctx, client))

	t.Run("marker", func(t *testing.T) {
		body, err := client.Query(ctx, "SELECT "+lwsqlsurface.VersionFunctionName+"()")
		require.NoError(t, err)
		defer func() { _ = body.Close() }()
		b := make([]byte, 64)
		n, _ := body.Read(b)
		require.Equal(t, strconv.Itoa(lwsqlsurface.Version), strings.TrimSpace(string(b[:n])))
	})

	t.Run("nothing declared is missing", func(t *testing.T) {
		rep, err := lwsqlsurface.Reconcile(ctx, client, lwsqlsurface.ReconcileReport)
		require.NoError(t, err)
		require.Empty(t, rep.Missing, "install left part of the surface off the server")
		require.Empty(t, rep.MissingViews, "install left part of the view family off the server")
		require.Equal(t, lwsqlsurface.Version, rep.ServerVersion)
		// Undeclared names are NOT asserted empty: a shared server may
		// legitimately carry someone else's LW_ helper, and reporting it is
		// the feature. What would be a defect is dropping it, which report
		// mode cannot do.
		if len(rep.Undeclared) > 0 {
			t.Logf("endpoint carries %d undeclared LW_ function(s): %v", len(rep.Undeclared), rep.Undeclared)
		}
	})

	t.Run("views select", func(t *testing.T) {
		// A view whose body does not analyze is created without complaint
		// and fails at read time, so creating them is not the assertion —
		// selecting from them is. LIMIT 0 keeps this about the body rather
		// than about whatever the endpoint happens to carry.
		for _, name := range chviews.AllViewNames() {
			qualified := chviews.TargetDatabase("").Qualified(name)
			body, err := client.Query(ctx, "SELECT * FROM "+qualified+" LIMIT 0")
			require.NoError(t, err, "view %s does not analyze", qualified)
			_ = body.Close()
		}
	})

	// A view whose stamp is not this build's is the one drift that answers
	// rather than erroring: ClickHouse inlines the function bodies at CREATE
	// time, so a view from an earlier revision serves an older vocabulary
	// with no sign anything is wrong (ADR-0226 §SD4). Faking the stamp is
	// enough to exercise the detection — what makes a view stale is what it
	// claims, and the claim is the only thing a reconciler can check.
	t.Run("a view from another revision reports stale", func(t *testing.T) {
		qualified := chviews.TargetDatabase("").Qualified(chviews.ViewColumns)
		require.NoError(t, client.Exec(ctx, "ALTER TABLE "+qualified+" MODIFY COMMENT 'surface v0'"))

		rep, err := lwsqlsurface.Reconcile(ctx, client, lwsqlsurface.ReconcileReport)
		require.NoError(t, err)
		require.Equal(t, []string{qualified}, rep.StaleViews)
		require.Empty(t, rep.MissingViews, "a stale view is present, not missing")
		require.False(t, rep.InSync(), "a stale view must fail the verdict; it returns data")
	})

	t.Run("idempotent, and it re-stamps what it replaces", func(t *testing.T) {
		require.NoError(t, lwsqlsurface.Install(ctx, client))
		rep, err := lwsqlsurface.Reconcile(ctx, client, lwsqlsurface.ReconcileReport)
		require.NoError(t, err)
		require.Empty(t, rep.Missing)
		require.Empty(t, rep.MissingViews)
		require.Empty(t, rep.StaleViews, "re-installing left a view on the previous revision")
		require.Empty(t, rep.Retired)
	})
}
