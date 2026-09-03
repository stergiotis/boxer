package play

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

// TestResultIDMovesWithTheServedResult pins the ResultID contract (ADR-0219
// SD8) on the main lane: zero before the first result, a new id for every
// landed run — a failed one included, since it replaces the record with
// nothing — and the same id for every snapshot in between.
func TestResultIDMovesWithTheServedResult(t *testing.T) {
	var fail atomic.Bool
	stream := arrowStreamBytes(t, []int64{10, 20})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("boom"))
			return
		}
		_, _ = w.Write(stream)
	}))
	defer srv.Close()

	store := NewQueryStore(NewClient(ClientConfig{URL: srv.URL}, srv.Client()), memory.NewGoAllocator(), 100, "test")
	snap := func() (id ResultID, hasRec bool, err error) {
		rec, _, _, _, _, _, _, err, id := store.Snapshot()
		if rec != nil {
			rec.Release()
		}
		return id, rec != nil, err
	}

	id0, has, _ := snap()
	require.Equal(t, ResultID(0), id0, "zero before the first result")
	require.False(t, has)

	store.Execute("SELECT n FROM t", nil, "")
	waitNotLoading(t, store)
	id1, has, err := snap()
	require.NoError(t, err)
	require.True(t, has)
	require.NotEqual(t, ResultID(0), id1)
	again, _, _ := snap()
	require.Equal(t, id1, again, "a repeated snapshot carries the same id")

	// The same SQL re-run returns identical bytes and still moves the id: it
	// is a delivery identity, not a content fingerprint.
	store.Execute("SELECT n FROM t", nil, "")
	waitNotLoading(t, store)
	id2, _, _ := snap()
	require.Greater(t, id2, id1)

	fail.Store(true)
	store.Execute("SELECT bad", nil, "")
	waitNotLoading(t, store)
	id3, has, err := snap()
	require.Error(t, err)
	require.False(t, has, "a failed run replaces the record with nothing")
	require.Greater(t, id3, id2, "and that replacement is a new result")
}

// TestResultIDOnNodeLane pins the same contract on a demand-driven lane, and
// that ids from two lanes never coincide — one process-wide sequence mints
// them all.
func TestResultIDOnNodeLane(t *testing.T) {
	srv, _ := arrowServer(t, []int64{7})
	defer srv.Close()
	lane := newNodeLane(clientExecutor{client: NewClient(ClientConfig{URL: srv.URL}, srv.Client())}, memory.NewGoAllocator(), 0)
	defer lane.close()

	v := lane.demand(compiledNode{SQL: "SELECT 1"})
	require.Equal(t, ResultID(0), v.id, "nothing served yet")
	require.Nil(t, v.rec)
	waitLaneReady(t, lane, "SELECT 1")

	v = lane.demand(compiledNode{SQL: "SELECT 1"})
	v.rec.Release()
	id1 := v.id
	require.NotEqual(t, ResultID(0), id1)
	v = lane.demand(compiledNode{SQL: "SELECT 1"})
	v.rec.Release()
	require.Equal(t, id1, v.id, "the memo serves the same result under the same id")

	v = lane.demand(compiledNode{SQL: "SELECT 2"})
	if v.rec != nil {
		v.rec.Release()
	}
	waitLaneReady(t, lane, "SELECT 2")
	v = lane.demand(compiledNode{SQL: "SELECT 2"})
	v.rec.Release()
	require.Greater(t, v.id, id1, "a new SQL lands a new result")

	store := NewQueryStore(NewClient(ClientConfig{URL: srv.URL}, srv.Client()), memory.NewGoAllocator(), 100, "test")
	store.Execute("SELECT n FROM t", nil, "")
	waitNotLoading(t, store)
	rec, _, _, _, _, _, _, _, storeID := store.Snapshot()
	if rec != nil {
		rec.Release()
	}
	require.NotEqual(t, v.id, storeID, "two lanes, two ids")
}
