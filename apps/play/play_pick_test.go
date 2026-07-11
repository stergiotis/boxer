package play

// Regression for the picker→editor buffer handoff (review finding): inst.sql
// is render-thread-only (the editor binding and the Run path read and write it
// unlocked), so the load goroutine must stash under pickMu and let Render
// consume once per frame (consumePickedSql) instead of assigning inst.sql
// directly. Run with -race: the direct cross-goroutine assignment these tests
// replace raced the frame's reads.

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

func TestConsumePickedSqlAppliesOncePerStash(t *testing.T) {
	inst := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 10), "-- initial")

	inst.consumePickedSql()
	require.Equal(t, "-- initial", inst.sql, "no stash → no change")

	loaded := "SELECT 42"
	inst.pickMu.Lock()
	inst.pickedSql = &loaded
	inst.pickMu.Unlock()
	inst.consumePickedSql()
	require.Equal(t, "SELECT 42", inst.sql)
	inst.pickMu.Lock()
	require.Nil(t, inst.pickedSql, "the stash is consumed exactly once")
	inst.pickMu.Unlock()

	inst.sql = "-- user edit"
	inst.consumePickedSql()
	require.Equal(t, "-- user edit", inst.sql, "a consumed stash never re-applies")
}

func TestPickedSqlHandoffIsRaceFree(t *testing.T) {
	// Concurrent stashers (the load goroutine's shape) against a consuming
	// render loop; -race validates the synchronization discipline.
	inst := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 10), "-- initial")
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			loaded := fmt.Sprintf("SELECT %d", n)
			inst.pickMu.Lock()
			inst.pickedSql = &loaded
			inst.pickErr = ""
			inst.pickMu.Unlock()
		}(i)
		inst.consumePickedSql() // the render thread's per-frame consume
	}
	wg.Wait()
	inst.consumePickedSql()
	require.True(t, strings.HasPrefix(inst.sql, "SELECT "), "one of the stashed buffers landed: %q", inst.sql)
}
