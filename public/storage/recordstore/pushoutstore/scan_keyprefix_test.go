package pushoutstore

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// TestScanKeyPrefix pins recordstore.ScanOpts.KeyPrefix on a string-keyed
// state-view store (ADR-0105 Update 2026-08-15, P3): the prefix selects
// whole keys for both the row-level Scan and the state view's ScanLive,
// and it matches literally — a quote is escaped and a LIKE metacharacter
// is just a character.
func TestScanKeyPrefix(t *testing.T) {
	local, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	st := NewPushoutStore(local, nil, PushoutStoreConfig{})
	require.NoError(t, st.EnsureTable(ctx))

	t0 := time.Unix(1_600_000_000, 0).UTC()
	t1 := t0.Add(time.Hour)
	snap := func(key string, ts time.Time) {
		require.NoError(t, st.Begin(key, ts).AddSnapshot(Snapshot{ID: key, Applied: []string{key}, PushoutGraph: []byte(key)}).Commit())
	}
	for _, k := range []string{"a/1", "a/2", "a/3", "b/1", "b%z", "a'x"} {
		snap(k, t0)
	}
	require.NoError(t, st.Delete("a/3", t1))
	_, err = st.Flush(ctx)
	require.NoError(t, err)

	keys := func(seq func(func(*PushoutEntity, error) bool)) (got []string) {
		for ent, rerr := range seq {
			require.NoError(t, rerr)
			got = append(got, ent.ID)
		}
		return
	}

	require.Equal(t, []string{"a/1", "a/2"},
		keys(st.ScanLiveSnapshot(ctx, recordstore.ScanOpts{KeyPrefix: "a/"})),
		"the live set under the prefix: the deleted a/3 is absent, b/* is out of range")
	require.ElementsMatch(t, []string{"a/1", "a/2", "a/3"},
		keys(st.ScanSnapshot(ctx, recordstore.ScanOpts{KeyPrefix: "a/"})),
		"the row-level scan keeps a/3's superseded row — the prefix narrows, it does not collapse")
	require.Equal(t, []string{"a'x"},
		keys(st.ScanLiveSnapshot(ctx, recordstore.ScanOpts{KeyPrefix: "a'"})),
		"a quote in the prefix is escaped, not SQL")
	require.Equal(t, []string{"b%z"},
		keys(st.ScanLiveSnapshot(ctx, recordstore.ScanOpts{KeyPrefix: "b%"})),
		"the prefix is literal: % is not a wildcard")
	require.Empty(t, keys(st.ScanLiveSnapshot(ctx, recordstore.ScanOpts{KeyPrefix: "c/"})))
}
