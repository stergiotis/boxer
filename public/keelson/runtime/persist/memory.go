package persist

import (
	"sync"
)

// MemoryBackend is an in-memory StorageBackendI used by M2.4 until the
// runtime.facts-backed implementation lands in M2.5. Values are defensively
// copied on both Get and Set so callers can't mutate the backing store.
type MemoryBackend struct {
	mu   sync.RWMutex
	data map[memoryKey][]byte
}

type memoryKey struct {
	Alias string
	Key   string
}

var _ StorageBackendI = (*MemoryBackend)(nil)

// NewMemoryBackend returns an empty MemoryBackend.
func NewMemoryBackend() (b *MemoryBackend) {
	b = &MemoryBackend{
		data: make(map[memoryKey][]byte, 32),
	}
	return
}

func (inst *MemoryBackend) Get(appAlias string, key string) (value []byte, found bool, err error) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	v, ok := inst.data[memoryKey{Alias: appAlias, Key: key}]
	if !ok {
		return
	}
	value = make([]byte, len(v))
	copy(value, v)
	found = true
	return
}

func (inst *MemoryBackend) Set(appAlias string, key string, value []byte) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	stored := make([]byte, len(value))
	copy(stored, value)
	inst.data[memoryKey{Alias: appAlias, Key: key}] = stored
	return
}

func (inst *MemoryBackend) Delete(appAlias string, key string) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	delete(inst.data, memoryKey{Alias: appAlias, Key: key})
	return
}

// Len returns the number of (alias, key) pairs currently stored. Used by
// tests to assert behaviour; not part of StorageBackendI.
func (inst *MemoryBackend) Len() (n int) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	n = len(inst.data)
	return
}
