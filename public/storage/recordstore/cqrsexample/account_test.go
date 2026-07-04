package cqrsexample

import (
	"context"
	"testing"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stretchr/testify/require"
)

// TestAccountLifecycle is the ADR-0100 S4 acceptance and the package's
// executable documentation: commands guard invariants and append events,
// rehydration folds the stream (with a snapshot short-circuiting the
// replay — observable via Replayed), the raw event history stays
// readable through Replay, and Scan feeds a cross-aggregate projection.
func TestAccountLifecycle(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	ctx := context.Background()
	st := NewLedgerStore[struct{}](exec, nil, LedgerStoreConfig{CacheCapacity: 8})
	require.NoError(t, st.EnsureTable(ctx))
	svc := NewService(st)
	const id = "acct/1"

	// Commands against a non-existent aggregate are rejected.
	require.ErrorContains(t, svc.Withdraw(ctx, id, 10), "does not exist")

	// Open once; a second Open is rejected by the rehydrated state.
	require.NoError(t, svc.Open(ctx, id, "amara"))
	require.ErrorContains(t, svc.Open(ctx, id, "amara"), "already open")

	// Deposits and a withdrawal; the overdraw guard holds.
	require.NoError(t, svc.Deposit(ctx, id, 100))
	require.NoError(t, svc.Deposit(ctx, id, 50))
	require.NoError(t, svc.Withdraw(ctx, id, 30))
	require.ErrorContains(t, svc.Withdraw(ctx, id, 1000), "balance is 120")

	acct, err := svc.Load(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "amara", acct.Owner)
	require.Equal(t, uint64(120), acct.Balance)
	require.Equal(t, 4, acct.Replayed(), "cold rehydration folds every event")

	// Snapshot, then prove the short-circuit: the next Load restores the
	// state and replays nothing…
	require.NoError(t, svc.Snapshot(ctx, id))
	acct, err = svc.Load(ctx, id)
	require.NoError(t, err)
	require.Equal(t, uint64(120), acct.Balance)
	require.Equal(t, 0, acct.Replayed(), "snapshot short-circuits the replay")

	// …and after one more event, exactly that one event is folded.
	require.NoError(t, svc.Deposit(ctx, id, 5))
	acct, err = svc.Load(ctx, id)
	require.NoError(t, err)
	require.Equal(t, uint64(125), acct.Balance)
	require.Equal(t, 1, acct.Replayed(), "only the post-snapshot event replays")

	// Closing is a domain event; further commands are rejected.
	require.NoError(t, svc.CloseAccount(ctx, id, "done"))
	require.ErrorContains(t, svc.Deposit(ctx, id, 1), "is closed")
	acct, err = svc.Load(ctx, id)
	require.NoError(t, err)
	require.True(t, acct.Closed)
	require.Equal(t, uint64(125), acct.Balance)

	// The full history stays readable: six events in sequence order, the
	// archetype naming the event type.
	history, err := st.Replay(ctx, id, recordstore.SeqTs(0))
	require.NoError(t, err)
	types := make([]string, 0, len(history))
	for _, row := range history {
		archetype := row.Archetype()
		require.Len(t, archetype, 1, "an event row carries exactly one component")
		types = append(types, archetype[0])
	}
	require.Equal(t, []string{"opened", "deposited", "deposited", "withdrawn", "deposited", "closed"}, types)

	// A second aggregate is isolated from the first, and Scan feeds a
	// cross-aggregate projection (the read-model angle): total deposited
	// volume across all accounts, straight off the baked ADR-0066 Filter.
	const id2 = "acct/2"
	require.NoError(t, svc.Open(ctx, id2, "bela"))
	require.NoError(t, svc.Deposit(ctx, id2, 7))
	acct2, err := svc.Load(ctx, id2)
	require.NoError(t, err)
	require.Equal(t, uint64(7), acct2.Balance)

	deposits, err := st.ScanDeposited(ctx, recordstore.ScanOpts{})
	require.NoError(t, err)
	var volume uint64
	for _, row := range deposits {
		require.True(t, row.Deposited.Has)
		volume += row.Deposited.Val.Amount
	}
	require.Len(t, deposits, 4)
	require.Equal(t, uint64(100+50+5+7), volume)
}
