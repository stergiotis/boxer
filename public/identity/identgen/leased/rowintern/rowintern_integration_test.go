//go:build integration

package rowintern

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/chclient"
	"github.com/stergiotis/boxer/public/identity/identgen/leased"
	"github.com/stergiotis/boxer/public/identity/identgen/leased/rowalloc"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// serverExec is an executor over the ClickHouse server the CLICKHOUSE_*
// variables name, or a skip. The race needs a server: the ordering the
// mapping rule rests on is one server's (ADR-0319 §SD1).
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

// Sixteen generators, each over an executor and a leased sequence of its
// own as a process would have, resolve the same fresh keys at once. Every
// one of them gets the same id for a key, no two keys share an id, and
// exactly one generator was told its id became the key's (ADR-0405,
// Verification plan).
func TestConcurrentGeneratorsAgreeOnEveryKeyOnTheServer(t *testing.T) {
	ctx := context.Background()
	const database = "interntest_it"
	setup := serverExec(t)
	require.NoError(t, setup.Exec(ctx, "DROP DATABASE IF EXISTS "+database+" SYNC"))
	t.Cleanup(func() { _ = setup.Exec(context.Background(), "DROP DATABASE IF EXISTS "+database+" SYNC") })
	layout := Layout{Database: database}
	leases := rowalloc.Layout{Database: database}
	require.NoError(t, rowalloc.ProvisionIn(ctx, setup, leases))
	require.NoError(t, ProvisionIn(ctx, setup, layout))

	const generators, keys = 16, 40
	const tv identifier.TagValue = 46368
	results := make([][]identifier.TaggedId, generators)
	freshCount := make([]int, keys)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for g := range generators {
		wg.Go(func() {
			exec := serverExec(t)
			alloc, err := rowalloc.NewAllocator(exec, leases)
			if err != nil {
				t.Error(err)
				return
			}
			alloc.MaxAttempts = 1000
			seq, err := leased.NewSequence(alloc, tv, 8)
			if err != nil {
				t.Error(err)
				return
			}
			gen, err := New(exec, layout, "race", seq)
			if err != nil {
				t.Error(err)
				return
			}
			got := make([]identifier.TaggedId, keys)
			// Half the generators one key at a time, half in one batch, in
			// opposite orders, so that the first insert of a key races too.
			if g%2 == 0 {
				for k := range keys {
					i := (k + g) % keys
					id, fresh, gerr := gen.GetId(ctx, []byte("key-"+strconv.Itoa(i)))
					if gerr != nil {
						t.Error(gerr)
						return
					}
					got[i] = id
					if fresh {
						mu.Lock()
						freshCount[i]++
						mu.Unlock()
					}
				}
			} else {
				batch := make([][]byte, keys)
				for k := range keys {
					batch[k] = []byte("key-" + strconv.Itoa(keys-1-k))
				}
				ids, fresh, gerr := gen.GetIds(ctx, batch)
				if gerr != nil {
					t.Error(gerr)
					return
				}
				for k := range keys {
					got[keys-1-k] = ids[k]
					if fresh[k] {
						mu.Lock()
						freshCount[keys-1-k]++
						mu.Unlock()
					}
				}
			}
			results[g] = got
		})
	}
	wg.Wait()
	require.False(t, t.Failed())

	distinct := map[identifier.TaggedId]int{}
	for k := range keys {
		want := results[0][k]
		require.Equal(t, tv, want.GetTag().GetValue())
		for g := 1; g < generators; g++ {
			require.Equal(t, want, results[g][k], "generator %d resolved key %d to another id", g, k)
		}
		distinct[want]++
		assert.Equal(t, 1, freshCount[k], "key %d: exactly one generator's id became the key's", k)
	}
	assert.Len(t, distinct, keys, "no two keys share an id")

	// A generator that starts afterwards resolves to the same ids without
	// minting.
	late, err := New(setup, layout, "race", noMint{})
	require.NoError(t, err)
	for k := range keys {
		id, found, lerr := late.Lookup(ctx, []byte("key-"+strconv.Itoa(k)))
		require.NoError(t, lerr)
		require.True(t, found)
		assert.Equal(t, results[0][k], id)
	}
}

// noMint is a generator of the stream that must never be asked for an id.
type noMint struct{}

func (noMint) GetId(context.Context, []byte) (identifier.TaggedId, bool, error) {
	panic("a lookup minted")
}
func (noMint) GetUntaggedId(context.Context, []byte) (identifier.UntaggedId, bool, error) {
	panic("a lookup minted")
}
func (noMint) Release() error           { return nil }
func (noMint) GetTag() identifier.IdTag { return identifier.TagValue(46368).GetTag() }
