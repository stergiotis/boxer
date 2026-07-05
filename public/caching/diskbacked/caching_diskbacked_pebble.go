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
//
// Per the StashBackendI contract the stash is best-effort: storage and
// codec errors degrade into misses (GetAndRemove) or dropped writes (Add).
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

	// Pebble takes its own reference on the block cache; release ours after
	// Open so the cache memory dies with the DB instead of leaking.
	blockCache := pebble.NewCache(8 << 20) // 8 MB block cache for Pebble's own use.
	defer blockCache.Unref()
	opts := &pebble.Options{
		Cache: blockCache,
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

func (inst *PebbleStash[K, V]) GetAndRemove(key K) (e caching.StashEntry[V], found bool) {
	keyBytes, err := keyEncMode.Marshal(key)
	if err != nil {
		return e, false
	}

	valBytes, closer, err := inst.db.Get(keyBytes)
	if err != nil {
		return e, false
	}
	defer closer.Close()

	var rec stashRecord[V]
	if err := cbor.Unmarshal(valBytes, &rec); err != nil {
		return e, false
	}

	if err := inst.db.Delete(keyBytes, pebble.NoSync); err == nil {
		inst.count.Add(-1)
	}

	return caching.StashEntry[V]{Value: rec.Value, Ver: rec.Ver, Stamp: rec.Stamp, Stale: rec.Stale}, true
}

func (inst *PebbleStash[K, V]) Add(key K, e caching.StashEntry[V]) (evicted bool) {
	kBytes, err := keyEncMode.Marshal(key)
	if err != nil {
		return false
	}
	vBytes, err := cbor.Marshal(stashRecord[V]{Value: e.Value, Ver: e.Ver, Stamp: e.Stamp, Stale: e.Stale})
	if err != nil {
		return false
	}

	// Updating an existing key does not change the count and never evicts
	// (contract); check first.
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

// Clear removes every entry. Failures leave a partial clear behind
// (best-effort contract); the counter tracks the deletes that succeeded.
func (inst *PebbleStash[K, V]) Clear() {
	it, err := inst.db.NewIter(nil)
	if err != nil {
		return
	}
	defer it.Close()
	for valid := it.First(); valid; valid = it.Next() {
		victim := append([]byte(nil), it.Key()...)
		if err := inst.db.Delete(victim, pebble.NoSync); err == nil {
			inst.count.Add(-1)
		}
	}
}

func (inst *PebbleStash[K, V]) Len() int {
	return int(inst.count.Load())
}

func (inst *PebbleStash[K, V]) Cap() int {
	return inst.softCap
}
