// Package stashtest is a conformance suite for caching.StashBackendI
// implementations. Backends in this repository (SliceStash, MapStash,
// diskbacked.PogrebStash, diskbacked.PebbleStash) and external ones run the
// same contract checks: update-in-place Add, stale-flag round-trips,
// removing GetAndRemove, idempotent Delete, honest eviction reporting, and
// Clear.
//
// The suite instantiates backends as [string, int]; the contract is
// type-agnostic, so one instantiation suffices.
package stashtest

import (
	"fmt"
	"testing"

	"github.com/stergiotis/boxer/public/caching"
)

// Factory creates a fresh backend with the given capacity. Implementations
// needing cleanup (disk-backed stashes) should register it via t.Cleanup.
type Factory func(t *testing.T, capacity int) caching.StashBackendI[string, int]

// Opts tunes the suite for backend-specific semantics.
type Opts struct {
	// SupportsUnbounded marks backends where capacity 0 means "no bound"
	// (disk-backed soft caps). Adds an unbounded-mode subtest; bounded
	// backends treat capacity 0 as a construction error instead.
	SupportsUnbounded bool
}

// Run executes the conformance suite against mk-created backends.
func Run(t *testing.T, mk Factory, opts Opts) {
	t.Run("RoundTrip", func(t *testing.T) {
		s := mk(t, 4)
		if evicted := s.Add("a", 1, false); evicted {
			t.Fatalf("Add below capacity must not evict")
		}
		if evicted := s.Add("b", 2, true); evicted {
			t.Fatalf("Add below capacity must not evict")
		}
		if got := s.Len(); got != 2 {
			t.Fatalf("Len = %d, want 2", got)
		}

		v, stale, found := s.GetAndRemove("a")
		if !found || v != 1 || stale {
			t.Fatalf("GetAndRemove(a) = (%d, %v, %v), want (1, false, true)", v, stale, found)
		}
		v, stale, found = s.GetAndRemove("b")
		if !found || v != 2 || !stale {
			t.Fatalf("GetAndRemove(b) = (%d, %v, %v), want (2, true, true) — stale flag must round-trip", v, stale, found)
		}
		if got := s.Len(); got != 0 {
			t.Fatalf("Len after removals = %d, want 0", got)
		}
		if _, _, found = s.GetAndRemove("a"); found {
			t.Fatalf("GetAndRemove must remove: second lookup found the entry")
		}
	})

	t.Run("UpdateInPlace", func(t *testing.T) {
		s := mk(t, 2)
		s.Add("k", 1, false)
		if evicted := s.Add("k", 2, true); evicted {
			t.Fatalf("update must not evict")
		}
		if got := s.Len(); got != 1 {
			t.Fatalf("Len after update = %d, want 1 (no duplicates)", got)
		}
		v, stale, found := s.GetAndRemove("k")
		if !found || v != 2 || !stale {
			t.Fatalf("GetAndRemove = (%d, %v, %v), want the newest (2, true, true)", v, stale, found)
		}

		// Update at capacity must not evict either.
		s.Add("x", 1, false)
		s.Add("y", 2, false)
		if evicted := s.Add("x", 11, false); evicted {
			t.Fatalf("update at capacity must not evict")
		}
		if _, _, found := s.GetAndRemove("y"); !found {
			t.Fatalf("unrelated entry lost by an update")
		}
	})

	t.Run("DeleteIdempotent", func(t *testing.T) {
		s := mk(t, 4)
		s.Add("a", 1, false)
		s.Add("b", 2, false)
		s.Delete("a")
		if got := s.Len(); got != 1 {
			t.Fatalf("Len after Delete = %d, want 1", got)
		}
		s.Delete("a")     // idempotent
		s.Delete("ghost") // missing key is a no-op
		if got := s.Len(); got != 1 {
			t.Fatalf("Len after no-op deletes = %d, want 1", got)
		}
		if _, _, found := s.GetAndRemove("b"); !found {
			t.Fatalf("surviving entry must remain readable")
		}
	})

	t.Run("EvictionHonesty", func(t *testing.T) {
		const capacity = 3
		s := mk(t, capacity)
		for i := 0; i < capacity; i++ {
			if evicted := s.Add(fmt.Sprintf("k%d", i), i, false); evicted {
				t.Fatalf("filling to capacity must not report evictions (i=%d)", i)
			}
		}
		if evicted := s.Add("overflow", 99, false); !evicted {
			t.Fatalf("Add of a new key at capacity must evict and report it")
		}
		if got := s.Len(); got > capacity {
			t.Fatalf("Len = %d exceeds capacity %d after eviction", got, capacity)
		}
		// The new key must be resident; exactly one prior entry is gone.
		if _, _, found := s.GetAndRemove("overflow"); !found {
			t.Fatalf("the newly added key must be resident after eviction")
		}
		survivors := 0
		for i := 0; i < capacity; i++ {
			if _, _, found := s.GetAndRemove(fmt.Sprintf("k%d", i)); found {
				survivors++
			}
		}
		if survivors != capacity-1 {
			t.Fatalf("survivors = %d, want %d (exactly one victim)", survivors, capacity-1)
		}
	})

	t.Run("Clear", func(t *testing.T) {
		s := mk(t, 4)
		s.Add("a", 1, false)
		s.Add("b", 2, true)
		s.Clear()
		if got := s.Len(); got != 0 {
			t.Fatalf("Len after Clear = %d, want 0", got)
		}
		if _, _, found := s.GetAndRemove("a"); found {
			t.Fatalf("entry survived Clear")
		}
		// The stash stays usable after Clear.
		s.Add("c", 3, false)
		if v, _, found := s.GetAndRemove("c"); !found || v != 3 {
			t.Fatalf("stash unusable after Clear")
		}
	})

	if opts.SupportsUnbounded {
		t.Run("Unbounded", func(t *testing.T) {
			s := mk(t, 0)
			if got := s.Cap(); got != 0 {
				t.Fatalf("Cap = %d, want 0 (unbounded)", got)
			}
			for i := 0; i < 16; i++ {
				if evicted := s.Add(fmt.Sprintf("k%d", i), i, false); evicted {
					t.Fatalf("unbounded stash must never evict (i=%d)", i)
				}
			}
			if got := s.Len(); got != 16 {
				t.Fatalf("Len = %d, want 16", got)
			}
		})
	}
}
