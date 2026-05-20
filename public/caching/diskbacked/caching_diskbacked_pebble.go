//go:build llm_generated_opus47

package diskbacked

import (
	"fmt"
	"os"
	"sync/atomic"

	"github.com/cockroachdb/pebble"
	"github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/caching"
)

// Compile-time assertion that PebbleStash satisfies caching.StashBackendI.
// Concrete K, V chosen arbitrarily — the check is parametric.
var _ caching.StashBackendI[string, []byte] = (*PebbleStash[string, []byte])(nil)

// PebbleStash implements StashBackendI using CockroachDB's Pebble.
//
// Capacity is enforced via a soft cap and an atomic item counter. softCap=0
// means "unbounded" (disk-limited only); a positive value triggers
// random-victim eviction inside Add once the counter reaches the cap.
// Pebble's first-row iterator is used to pick the victim, which is
// effectively random (LSM ordering is by sorted key bytes, but for a cache
// of unrelated keys this is a reasonable approximation of uniform).
type PebbleStash[K comparable, V any] struct {
	db      *pebble.DB
	path    string
	softCap int
	count   atomic.Int64
}

// NewPebbleStash opens a disk-backed stash at path.
//
//   - softCap: maximum number of entries before Add starts evicting. Zero
//     disables the bound (disk fills until the OS complains).
//   - cleanStart: if true, removes any existing DB at path before opening.
//     When false, the existing entries are counted to seed the in-memory
//     counter — that scan is O(n) in the live entry count and runs once.
func NewPebbleStash[K comparable, V any](path string, softCap int, cleanStart bool) (*PebbleStash[K, V], error) {
	if cleanStart {
		_ = os.RemoveAll(path)
	}

	opts := &pebble.Options{
		Cache: pebble.NewCache(8 << 20), // 8 MB block cache for Pebble's own use.
	}

	db, err := pebble.Open(path, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open pebble db: %w", err)
	}

	s := &PebbleStash[K, V]{
		db:      db,
		path:    path,
		softCap: softCap,
	}

	// Seed the counter from any pre-existing rows.
	if !cleanStart {
		it, err := db.NewIter(nil)
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to open pebble iterator: %w", err)
		}
		var n int64
		for valid := it.First(); valid; valid = it.Next() {
			n++
		}
		_ = it.Close()
		s.count.Store(n)
	}

	return s, nil
}

func (inst *PebbleStash[K, V]) Close() error {
	return inst.db.Close()
}

func (inst *PebbleStash[K, V]) GetAndRemove(key K) (V, bool) {
	var zero V

	keyBytes, err := keyEncMode.Marshal(key)
	if err != nil {
		return zero, false
	}

	valBytes, closer, err := inst.db.Get(keyBytes)
	if err != nil {
		if err == pebble.ErrNotFound {
			return zero, false
		}
		return zero, false
	}
	defer closer.Close()

	var val V
	if err := cbor.Unmarshal(valBytes, &val); err != nil {
		return zero, false
	}

	if err := inst.db.Delete(keyBytes, pebble.NoSync); err == nil {
		inst.count.Add(-1)
	}

	return val, true
}

func (inst *PebbleStash[K, V]) Add(key K, value V) (evicted bool) {
	kBytes, err := keyEncMode.Marshal(key)
	if err != nil {
		return false
	}
	vBytes, err := cbor.Marshal(value)
	if err != nil {
		return false
	}

	// Updating an existing key does not change the count; check first.
	_, closer, err := inst.db.Get(kBytes)
	updating := err == nil
	if closer != nil {
		_ = closer.Close()
	}

	if !updating && inst.softCap > 0 && int(inst.count.Load()) >= inst.softCap {
		if inst.evictOne() {
			evicted = true
		}
	}

	if err := inst.db.Set(kBytes, vBytes, pebble.NoSync); err == nil && !updating {
		inst.count.Add(1)
	}
	return evicted
}

// evictOne deletes the first key in iteration order, returning true if a
// delete was actually performed. Iteration order is by sorted key bytes,
// which approximates random selection for unrelated cache keys.
func (inst *PebbleStash[K, V]) evictOne() bool {
	it, err := inst.db.NewIter(nil)
	if err != nil {
		return false
	}
	defer it.Close()
	if !it.First() {
		return false
	}
	victim := append([]byte(nil), it.Key()...)
	if err := inst.db.Delete(victim, pebble.NoSync); err == nil {
		inst.count.Add(-1)
		return true
	}
	return false
}

func (inst *PebbleStash[K, V]) Delete(key K) {
	kBytes, err := keyEncMode.Marshal(key)
	if err != nil {
		return
	}
	// Only decrement if the key was actually present.
	_, closer, err := inst.db.Get(kBytes)
	if err != nil {
		return
	}
	_ = closer.Close()
	if err := inst.db.Delete(kBytes, pebble.NoSync); err == nil {
		inst.count.Add(-1)
	}
}

func (inst *PebbleStash[K, V]) Len() int {
	return int(inst.count.Load())
}

func (inst *PebbleStash[K, V]) Cap() int {
	return inst.softCap
}
