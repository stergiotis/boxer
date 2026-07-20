package chlocalbroker

import "sync"

// KeyStore holds per-dataset AEAD keys in process memory (ADR-0134 K2).
// Key custody splits by role: the ad-hoc capability service is the
// policy owner and registers handle→key here at publish (deregistering
// at retract); the broker is the decrypt executor and resolves the key
// by table name when materialising an encrypted input. Keys never ride
// the bus — a request carries only the handle. The zero value is
// unusable; build one with NewKeyStore. Safe for concurrent use.
type KeyStore struct {
	mu   sync.RWMutex
	keys map[string][]byte
}

// NewKeyStore returns an empty KeyStore.
func NewKeyStore() *KeyStore {
	return &KeyStore{keys: make(map[string][]byte)}
}

// RegisterDatasetKey associates name with a copy of key. A republish
// registers a fresh key under the same name, replacing the old one.
func (inst *KeyStore) RegisterDatasetKey(name string, key []byte) {
	cp := make([]byte, len(key))
	copy(cp, key)
	inst.mu.Lock()
	inst.keys[name] = cp
	inst.mu.Unlock()
}

// DeregisterDatasetKey forgets name's key, zeroing the stored copy
// first (best-effort erasure of the buffer this package owns; ADR-0134
// notes runtime-made copies cannot be reached).
func (inst *KeyStore) DeregisterDatasetKey(name string) {
	inst.mu.Lock()
	if k, ok := inst.keys[name]; ok {
		for i := range k {
			k[i] = 0
		}
		delete(inst.keys, name)
	}
	inst.mu.Unlock()
}

// LookupDatasetKey returns a copy of the key registered for name.
func (inst *KeyStore) LookupDatasetKey(name string) (key []byte, ok bool) {
	inst.mu.RLock()
	k, ok := inst.keys[name]
	inst.mu.RUnlock()
	if ok {
		key = make([]byte, len(k))
		copy(key, k)
	}
	return
}

// KeyStoreI is the broker-side key lookup the encrypted-input
// materialiser needs. *KeyStore satisfies it; tests inject fakes.
type KeyStoreI interface {
	LookupDatasetKey(name string) (key []byte, ok bool)
}
