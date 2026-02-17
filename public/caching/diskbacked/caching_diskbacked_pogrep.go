//go:build llm_generated_gemini3pro
package diskbacked

import (
	"fmt"
	"os"

	"github.com/akrylysov/pogreb"
	"github.com/fxamacker/cbor/v2"
)


// PogrebStash implements StashBackend using the Pogreb key-value store.
// It uses CBOR for serialization.
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
func (s *PogrebStash[K, V]) GetAndRemove(key K) (V, bool) {
	var zero V

	// 1. Serialize Key (Canonical CBOR)
	kBytes, err := keyEncMode.Marshal(key)
	if err != nil {
		// In production, you might want to log this serialization error
		return zero, false
	}

	// 2. Get from Pogreb
	vBytes, err := s.db.Get(kBytes)
	if err != nil || vBytes == nil {
		return zero, false
	}

	// 3. Deserialize Value (Standard CBOR)
	// We unmarshal directly into zero to avoid allocating if possible (though interface overhead applies)
	err = cbor.Unmarshal(vBytes, &zero)
	if err != nil {
		return zero, false
	}

	// 4. Remove (Atomic promotion)
	// Pogreb deletes are fast (tombstones).
	_ = s.db.Delete(kBytes)

	return zero, true
}

// Add inserts a value. If softCap is exceeded, it evicts a random item.
func (s *PogrebStash[K, V]) Add(key K, value V) (evicted bool) {
	// 1. Serialize
	kBytes, err := keyEncMode.Marshal(key)
	if err != nil {
		return false
	}
	vBytes, err := cbor.Marshal(value)
	if err != nil {
		return false
	}

	// 2. Check Eviction (Soft Cap)
	// Pogreb doesn't have auto-eviction. We must manually check count.
	// This check is somewhat expensive (atomic load), so we might want to sample it,
	// but Pogreb's Count() is generally O(1) (cached).
	if s.softCap > 0 && int(s.db.Count()) >= s.softCap {
		s.evictOne()
		evicted = true
	}

	// 3. Put
	err = s.db.Put(kBytes, vBytes)
	return evicted
}

// evictOne removes a single item to make space.
// Since Pogreb is a hash index, iteration order is essentially random,
// which is exactly what we want for "Random Eviction".
func (s *PogrebStash[K, V]) evictOne() {
	it := s.db.Items()
	// We only need the first item yielded by the iterator
	k, _, err := it.Next()
	if err == nil && k != nil {
		_ = s.db.Delete(k)
	}
	// We don't need to close the iterator explicitly in Pogreb,
	// but we should stop iterating.
}

func (s *PogrebStash[K, V]) Delete(key K) {
	kBytes, err := keyEncMode.Marshal(key)
	if err == nil {
		_ = s.db.Delete(kBytes)
	}
}

func (s *PogrebStash[K, V]) Len() int {
	return int(s.db.Count())
}

func (s *PogrebStash[K, V]) Cap() int {
	return s.softCap
}
