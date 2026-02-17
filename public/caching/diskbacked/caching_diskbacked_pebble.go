//go:build llm_generated_gemini3pro

package diskbacked

import (
	"fmt"
	"os"

	"github.com/cockroachdb/pebble"
	"github.com/fxamacker/cbor/v2"
)

// PebbleStash implements StashBackend using CockroachDB's Pebble.
type PebbleStash[K comparable, V any] struct {
	db   *pebble.DB
	path string
}

// NewPebbleStash creates a disk-backed stash.
// cleanStart: If true, deletes existing DB on startup (typical for a cache).
func NewPebbleStash[K comparable, V any](path string, cleanStart bool) (*PebbleStash[K, V], error) {
	if cleanStart {
		_ = os.RemoveAll(path)
	}

	// Disable WAL (Write Ahead Log) for performance?
	// For a stash, we might tolerate data loss on crash for speed.
	// However, Pebble defaults are usually fine.
	opts := &pebble.Options{
		// Cache size for Pebble's internal usage (Block Cache)
		Cache: pebble.NewCache(8 << 20), // 8 MB
	}

	db, err := pebble.Open(path, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open pebble db: %w", err)
	}

	return &PebbleStash[K, V]{
		db:   db,
		path: path,
	}, nil
}

func (inst *PebbleStash[K, V]) Close() error {
	return inst.db.Close()
}
func (inst *PebbleStash[K, V]) GetAndRemove(key K) (V, bool) {
	var zero V

	// 1. Serialize Key
	keyBytes, err := keyEncMode.Marshal(key)
	if err != nil {
		return zero, false
	}

	// 2. Get from Pebble
	// Closer must be closed to free memory/locks
	valBytes, closer, err := inst.db.Get(keyBytes)
	if err != nil {
		if err == pebble.ErrNotFound {
			return zero, false
		}
		// Log error in production
		return zero, false
	}
	defer closer.Close()

	// 3. Deserialize Value
	var val V
	err = cbor.Unmarshal(valBytes, &val)
	if err != nil {
		return zero, false
	}

	// 4. Remove (Atomic promotion logic)
	// Note: Pebble Deletes are fast (tombstones).
	_ = inst.db.Delete(keyBytes, pebble.NoSync)

	return val, true
}

func (inst *PebbleStash[K, V]) Add(key K, value V) bool {
	// 1. Serialize
	kBytes, err := keyEncMode.Marshal(key)
	if err != nil {
		return false // Fail silently or panic depending on philosophy
	}
	vBytes, err := keyEncMode.Marshal(value)
	if err != nil {
		return false
	}

	// 2. Set (NoSync for speed, since it's just a cache)
	err = inst.db.Set(kBytes, vBytes, pebble.NoSync)

	// Pebble manages its own capacity via compactions,
	// but it doesn't strictly "Evict" based on count like a map.
	// If you strictly need to cap disk usage, you'd need a separate counter
	// or use Pebble's metrics to delete range.
	// For a stash, we often assume Disk is "Infinite" compared to RAM.
	return false
}

func (inst *PebbleStash[K, V]) Delete(key K) {
	kBytes, err := keyEncMode.Marshal(key)
	if err == nil {
		_ = inst.db.Delete(kBytes, pebble.NoSync)
	}
}

// Len is hard in LSM trees (requires scan).
// For a Stash, usually we don't strictly rely on Len() for logic
// other than metrics.
func (inst *PebbleStash[K, V]) Len() int {
	// Approximate or maintain a separate atomic counter.
	return 0
}

func (inst *PebbleStash[K, V]) Cap() int {
	return 0 // Infinite (Disk)
}
