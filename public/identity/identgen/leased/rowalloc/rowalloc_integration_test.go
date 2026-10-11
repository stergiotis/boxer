//go:build integration

package rowalloc

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// serverExec is an executor over the ClickHouse server the CLICKHOUSE_*
// variables name, or a skip. The race needs a server: the serialising
// property under test is one server's (ADR-0319 §SD1).
func serverExec(t *testing.T) (exec recordstore.ExecutorI) {
	t.Helper()
	cfg := chclient.ConfigFromEnv()
	client := chclient.New(cfg, nil)
	if err := client.Ping(context.Background()); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", cfg.URL, err)
	}
	exec, err := storeexec.New(client, nil)
	require.NoError(t, err)
	return
}

// Sixteen allocators, each its own executor as a process would be, race one
// fresh tag — so the first insert races too — and lease blocks until each
// holds its share. Every block is disjoint from every other and none holds
// body 0 (ADR-0319, Verification plan).
func TestConcurrentAllocatorsLeaseDisjointBlocksOnTheServer(t *testing.T) {
	ctx := context.Background()
	layout := Layout{Database: "leasetest"}
	setup := serverExec(t)
	require.NoError(t, setup.Exec(ctx, "DROP TABLE IF EXISTS "+layout.Table()))
	require.NoError(t, ProvisionIn(ctx, setup, layout))

	const allocators, leases = 16, 12
	const tv identifier.TagValue = 46368
	var mu sync.Mutex
	var blocks []block
	var wg sync.WaitGroup
	for i := 0; i < allocators; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a, err := NewAllocator(serverExec(t), layout)
			if err != nil {
				t.Error(err)
				return
			}
			a.MaxAttempts = 1000
			for range leases {
				lo, hi, aerr := a.AllocateBlock(ctx, tv, 1, uint64(1+i))
				if aerr != nil {
					t.Error(aerr)
					return
				}
				mu.Lock()
				blocks = append(blocks, block{uint64(lo), uint64(hi)})
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	require.Len(t, blocks, allocators*leases)
	slices.SortFunc(blocks, func(a, b block) int {
		switch {
		case a.lo < b.lo:
			return -1
		case a.lo > b.lo:
			return 1
		}
		return 0
	})
	requireDisjointAscending(t, blocks)
}
