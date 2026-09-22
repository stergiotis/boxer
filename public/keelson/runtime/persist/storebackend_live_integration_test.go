//go:build integration

package persist

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist/persiststore"
	"github.com/stergiotis/boxer/public/keelson/runtime/statestore"
	"github.com/stergiotis/boxer/public/keelson/runtime/statestore/statestoretest"
)

// liveStateDb is a scratch database; the live `boxer.persiststate` holds
// whatever the developer's running desktop stored.
const liveStateDb = "persist_state_live_test"

// TestStoreBackendStateConformance_LiveCH runs the shared state-store
// contract through the HTTP executor the runtime wires, against a live
// server. The default lane runs the same cases on clickhouse-local, which
// accepts shapes the HTTP interface refuses — the divergence ADR-0105's
// 2026-08-15 entry records — so this lane is the one that can see a
// generated statement that works locally and not in production.
func TestStoreBackendStateConformance_LiveCH(t *testing.T) {
	ctx := context.Background()
	cli := chclient.New(chclient.Defaults(), nil)
	if err := cli.Ping(ctx); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", chclient.Defaults().URL, err)
	}
	t.Cleanup(func() { _ = cli.Exec(ctx, "DROP DATABASE IF EXISTS "+liveStateDb) })
	statestoretest.Run(t, func(t *testing.T) statestore.StoreI {
		require.NoError(t, cli.Exec(ctx, "DROP DATABASE IF EXISTS "+liveStateDb))
		exec, err := storeexec.New(cli, nil)
		require.NoError(t, err)
		b, err := OpenStoreBackendAt(ctx, exec, nil, liveStateDb+"."+persiststore.TableName)
		require.NoError(t, err)
		t.Cleanup(b.Close)
		return b
	})
}
