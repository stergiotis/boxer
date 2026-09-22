//go:build integration

package persiststore

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// liveDb is a scratch database: the live `boxer.persiststate` holds whatever
// the developer's running desktop stored.
const liveDb = "persiststore_live_test"

// openLive provisions the store on a fresh scratch table through the HTTP
// executor — the transport production uses, and the one that diverges from
// the clickhouse-local executor the default lane runs on (ADR-0105's
// 2026-08-15 entry: a multi-statement EnsureTable passed locally and failed
// here). That divergence is why this lane gates every milestone of the
// state-table work rather than only the last.
func openLive(t *testing.T) (st *PersistStore) {
	t.Helper()
	ctx := context.Background()
	cli := chclient.New(chclient.Defaults(), nil)
	if err := cli.Ping(ctx); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", chclient.Defaults().URL, err)
	}
	require.NoError(t, cli.Exec(ctx, "DROP DATABASE IF EXISTS "+liveDb))
	t.Cleanup(func() { _ = cli.Exec(ctx, "DROP DATABASE IF EXISTS "+liveDb) })
	exec, err := storeexec.New(cli, nil)
	require.NoError(t, err)
	st, err = OpenPersistStore(ctx, exec, nil, PersistStoreConfig{Table: liveDb + "." + TableName})
	require.NoError(t, err)
	t.Cleanup(st.Close)
	return
}

// TestScanLive_LiveCH runs the state view's scan and ScanOpts.KeyPrefix
// over the HTTP executor: the nested newest-row-per-key SELECT and the
// startsWith range read are single statements, and this is where that
// claim is checked against the server rather than clickhouse-local.
func TestScanLive_LiveCH(t *testing.T) {
	st := openLive(t)
	ctx := context.Background()
	t0 := time.Unix(1_700_000_000, 0).UTC()
	put := func(key string, ts time.Time, v string) {
		require.NoError(t, st.Begin(key, ts).
			AddOwner(Owner{ID: key, AppId: "a"}).
			AddState(State{ID: key, Key: key, Value: []byte(v)}).
			Commit())
	}
	put("a/k1", t0, "one")
	put("a/k1", t0.Add(time.Second), "two")
	put("a/k2", t0, "gone")
	put("b/k1", t0, "other")
	require.NoError(t, st.Delete("a/k2", t0.Add(time.Second)))
	_, err := st.Flush(ctx)
	require.NoError(t, err)

	got := map[string]string{}
	for ent, rerr := range st.ScanLiveState(ctx, recordstore.ScanOpts{KeyPrefix: "a/"}) {
		require.NoError(t, rerr)
		got[ent.ID] = string(ent.State.Val.Value)
	}
	require.Equal(t, map[string]string{"a/k1": "two"}, got,
		"newest version only, the deleted key absent, the other prefix out of range")
}
