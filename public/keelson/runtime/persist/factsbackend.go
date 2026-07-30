package persist

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// FactsBackend is the StorageBackendI that records app state as facts. Each
// Set lands a boxer.facts row tagged KindState through FactsStoreI, each
// Delete a tombstone, and each Get resolves to the latest non-tombstoned
// row — the append-only shape the store already gives grants, audits,
// launches, and workingsets (ADR-0026 §SD6). Two properties follow from
// using the facts store rather than a private table: app state survives
// process exit wherever the store does, and it is queryable on the same
// table, joining an app's other facts on the app-id column.
//
// The adapter is deliberately thin — no cache, no batching, no key
// validation beyond what the service already applies. It holds only the
// store, so a caller may share one FactsStoreI between this and the host's
// audit / launch writers; the facts table is designed to be written from
// several places at once.
//
// Durability is the store's, not this adapter's. Over chstore.Store the
// rows reach ClickHouse; over factsstore.InMemoryFactsStore they last
// exactly as long as the process — the same best-effort stance the audit
// trail takes when ClickHouse is down, and the reason the carousel chooses
// the backend by the chstore.NewWithFallback verdict and labels the choice
// in the runtime status bar. A reader that sees "persist:facts" is being
// told which code path ran, and the neighbouring "facts:" segment says
// whether that path reaches ClickHouse.
type FactsBackend struct {
	facts factsstore.FactsStoreI
}

var _ StorageBackendI = (*FactsBackend)(nil)

// NewFactsBackend binds the backend to a facts store. A nil store is
// rejected at construction rather than deferred to the first write, so a
// caller that failed to build a store finds out at wiring time instead of
// handing apps a Storage that errors on every call.
func NewFactsBackend(facts factsstore.FactsStoreI) (inst *FactsBackend, err error) {
	if facts == nil {
		err = eh.Errorf("persist: NewFactsBackend: nil facts store")
		return
	}
	inst = &FactsBackend{facts: facts}
	return
}

// Get resolves (app, key) to the latest state value. A key that was never
// written and a key whose most recent row is a tombstone both report
// found=false — the LatestState semantics, which are the ones the persist
// contract already promises. An empty stored value round-trips as
// found=true with a zero-length slice, distinct from absent.
func (inst *FactsBackend) Get(ref StorageRef, key string) (value []byte, found bool, err error) {
	value, found, err = inst.facts.LatestState(ref.StateAppId(), key)
	if err != nil {
		err = eh.Errorf("persist: facts get %s.%s: %w", ref.Alias, key, err)
		return
	}
	return
}

// Set appends a state row. Append-only: a second write for the same
// (app, key) supersedes the first without erasing it, so the row trail is
// the history of that key and the previous value stays queryable.
func (inst *FactsBackend) Set(ref StorageRef, key string, value []byte) (err error) {
	_, err = inst.facts.WriteState(factsstore.StateRow{
		AppId: ref.StateAppId(),
		Key:   key,
		Value: value,
		Ts:    time.Now().UTC(),
	})
	if err != nil {
		err = eh.Errorf("persist: facts set %s.%s: %w", ref.Alias, key, err)
		return
	}
	return
}

// Delete appends a tombstone. Deleting a never-written key is not an
// error, matching MemoryBackend; the tombstone simply becomes the latest
// row for a key that had none.
func (inst *FactsBackend) Delete(ref StorageRef, key string) (err error) {
	err = inst.facts.DeleteState(ref.StateAppId(), key)
	if err != nil {
		err = eh.Errorf("persist: facts delete %s.%s: %w", ref.Alias, key, err)
		return
	}
	return
}
