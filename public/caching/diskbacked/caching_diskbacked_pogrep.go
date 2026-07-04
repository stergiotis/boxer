package diskbacked

import (
	"fmt"
	"os"

	"github.com/akrylysov/pogreb"
	"github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/caching"
)

// Compile-time assertion that PogrebStash satisfies caching.StashBackendI.
var _ caching.StashBackendI[string, []byte] = (*PogrebStash[string, []byte])(nil)

// PogrebStash implements StashBackendI using the Pogreb key-value store.
// It uses CBOR for serialization.
//
// Per the StashBackendI contract the stash is best-effort: storage and
// codec errors degrade into misses (GetAndRemove) or dropped writes (Add).
type PogrebStash[K comparable, V any] struct {
	db      *pogreb.DB
	path    string
	softCap int // A soft limit on the number of items.
}

// NewPogrebStash creates a new disk-backed stash.
//
// path: Directory path for the DB.
// softCap: Approximate maximum number of items. Set to 0 for "unbounded" (disk limited).
// cleanStart: If true, deletes the DB directory on startup (cache reset).
func NewPogrebStash[K comparable, V any](path string, softCap int, cleanStart bool) (*PogrebStash[K, V], error) {
	if cleanStart {
		_ = os.RemoveAll(path)
	}

	opts := &pogreb.Options{
		BackgroundSyncInterval: -1, // Disable periodic fsync for performance (it's a cache)
	}

	db, err := pogreb.Open(path, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open pogreb db at %s: %w", path, err)
	}

	return &PogrebStash[K, V]{
		db:      db,
		path:    path,
		softCap: softCap,
	}, nil
}

// Close closes the underlying DB. Essential to release file locks.
func (s *PogrebStash[K, V]) Close() error {
	return s.db.Close()
}

// GetAndRemove attempts to retrieve a value and remove it from the stash (Promotion).
func (s *PogrebStash[K, V]) GetAndRemove(key K) (value V, stale bool, found bool) {
	// 1. Serialize Key (Canonical CBOR)
	kBytes, err := keyEncMode.Marshal(key)
	if err != nil {
		return value, false, false
	}

	// 2. Get from Pogreb
	vBytes, err := s.db.Get(kBytes)
	if err != nil || vBytes == nil {
		return value, false, false
	}

	// 3. Deserialize the record envelope
	var rec stashRecord[V]
	if err = cbor.Unmarshal(vBytes, &rec); err != nil {
		return value, false, false
	}

	// 4. Remove (Atomic promotion)
	// Pogreb deletes are fast (tombstones).
	_ = s.db.Delete(kBytes)

	return rec.Value, rec.Stale, true
}

// Add inserts a value. If softCap is exceeded by a NEW key, it evicts a
// random item. Updates to an existing key never evict — they don't change
// the count, so there is no reason to drop an unrelated entry.
func (s *PogrebStash[K, V]) Add(key K, value V, stale bool) (evicted bool) {
	// 1. Serialize
	kBytes, err := keyEncMode.Marshal(key)
	if err != nil {
		return false
	}
	vBytes, err := cbor.Marshal(stashRecord[V]{Value: value, Stale: stale})
	if err != nil {
		return false
	}

	// 2. Distinguish update from insert. pogreb.Get on a missing key returns
	// (nil, nil); a real value is non-nil bytes.
	existing, err := s.db.Get(kBytes)
	updating := err == nil && existing != nil

	// 3. Evict only when inserting a brand-new key over the cap, and report
	// an eviction only when one actually happened.
	// Pogreb's Count() is O(1) (cached), so the probe is cheap.
	if !updating && s.softCap > 0 && int(s.db.Count()) >= s.softCap {
		evicted = s.evictOne()
	}

	// 4. Put
	err = s.db.Put(kBytes, vBytes)
	_ = err
	return evicted
}

// evictOne removes a single item to make space, reporting whether a delete
// was actually performed. Since Pogreb is a hash index, iteration order is
// essentially random, which is exactly what we want for "Random Eviction".
func (s *PogrebStash[K, V]) evictOne() bool {
	it := s.db.Items()
	// We only need the first item yielded by the iterator.
	k, _, err := it.Next()
	if err != nil || k == nil {
		return false
	}
	return s.db.Delete(k) == nil
}

func (s *PogrebStash[K, V]) Delete(key K) {
	kBytes, err := keyEncMode.Marshal(key)
	if err == nil {
		_ = s.db.Delete(kBytes)
	}
}

// Clear removes every entry. Keys are collected before deleting — Pogreb's
// iterator does not guarantee stability under concurrent modification.
// Failures leave a partial clear behind (best-effort contract).
func (s *PogrebStash[K, V]) Clear() {
	it := s.db.Items()
	keys := make([][]byte, 0, s.db.Count())
	for {
		k, _, err := it.Next()
		if err != nil || k == nil {
			break
		}
		keys = append(keys, append([]byte(nil), k...))
	}
	for _, k := range keys {
		_ = s.db.Delete(k)
	}
}

func (s *PogrebStash[K, V]) Len() int {
	return int(s.db.Count())
}

func (s *PogrebStash[K, V]) Cap() int {
	return s.softCap
}
