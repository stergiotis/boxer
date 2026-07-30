package persist

import (
	"sync"
)

// MemoryBackend is an in-memory StorageBackendI: contents last exactly as
// long as the process. It remains the backend when no facts store is
// available and is the one tests use. Values are defensively copied on both
// Get and Set so callers can't mutate the backing store.
//
// It keys on the alias alone and ignores StorageRef.AppId — there is no
// provenance to record when nothing outlives the process, and keying on the
// alias keeps its behaviour identical to the subject namespace apps address.
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

func (inst *MemoryBackend) Get(ref StorageRef, key string) (value []byte, found bool, err error) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	v, ok := inst.data[memoryKey{Alias: ref.Alias, Key: key}]
	if !ok {
		return
	}
	value = make([]byte, len(v))
	copy(value, v)
	found = true
	return
}

func (inst *MemoryBackend) Set(ref StorageRef, key string, value []byte) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	stored := make([]byte, len(value))
	copy(stored, value)
	inst.data[memoryKey{Alias: ref.Alias, Key: key}] = stored
	return
}

func (inst *MemoryBackend) Delete(ref StorageRef, key string) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	delete(inst.data, memoryKey{Alias: ref.Alias, Key: key})
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
