package persist

import (
	"context"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist/persiststore"
	"github.com/stergiotis/boxer/public/keelson/runtime/runinfo"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// StoreBackend is the durable StorageBackendI over a generated record
// store (ADR-0105 D3a). It replaced the facts-bound backend: same three
// verbs, same StorageI surface for apps, but the rows land on the
// store-owned `boxer.persiststate` table instead of `boxer.facts`.
//
// What that buys is the reason ADR-0105 exists. The state verbs want a
// latest-wins read over a mutable key. On the append-only facts table
// that read was hand-written leeway-encoded SQL — nested argMax over
// membership lookups, the code class ADR-0105 is deleting. Here the
// generated state view answers it directly: Delete appends a tombstone,
// the newest row for a key wins, and GetLive reads a tombstoned key as
// absent. The history property survives the move — a second Set for the
// same key supersedes the first without erasing it, so the row trail is
// still that key's history.
//
// What it costs is the join. State rows no longer sit on the same table
// as that app's grant, audit and launch facts, so "this app's state
// beside its other facts" is now a two-table query rather than one
// column. ADR-0026 §SD6 argued for the single table on exactly that
// property; ADR-0105 D3a takes the trade knowingly, and the app id is
// carried on the new table so the join is still expressible.
//
// # Durability
//
// Every mutating call Commits and Flushes before returning — one
// synchronous insert per operation, durable-on-return, the posture
// pushoutstore's adapter takes for the same reason. A failed Flush
// discards the operation's buffered rows, so a failed Set stays "never
// happened" rather than shipping behind the next one's back.
//
// # Concurrency
//
// A generated store is single-goroutine (ADR-0100) and StorageBackendI
// promises concurrent safety, so one mutex serializes every method —
// the mutex-guarded wrapper ADR-0105 D4 names as the alternative to a
// single owner goroutine.
type StoreBackend struct {
	mu sync.Mutex
	st *persiststore.PersistStore
	pc *persiststore.PersistCache[struct{}]
	// runId stamps every written row with the process that wrote it
	// (ADR-0191 §SD5). Read once at open rather than per write: it is
	// process-wide and cannot change. Empty on a host that never called
	// runinfo.Init, which writes unstamped rows exactly as before.
	runId string
}

var _ StorageBackendI = (*StoreBackend)(nil)

// OpenStoreBackend builds the backend over an executor and provisions the
// table. Unlike a facts-bound store, this table is the store's own, so
// EnsureTable here is correct rather than a second DDL author.
//
// VerifySchema runs at open: a table left over from an older schema
// answers reads with the wrong columns, and finding that at wiring time
// beats finding it on an app's first Get.
func OpenStoreBackend(ctx context.Context, exec recordstore.ExecutorI, alloc memory.Allocator) (inst *StoreBackend, err error) {
	return OpenStoreBackendAt(ctx, exec, alloc, "")
}

// OpenStoreBackendAt is OpenStoreBackend over a table other than the
// store's baked `boxer.persiststate`: table is the store's runtime
// override (optionally database-qualified; empty selects the baked name).
// The schema is the same, only where the rows land moves — a scratch
// database for an integration test that must not touch the developer's
// live state, or a per-deployment table. Provisioning and the schema check
// run against the override.
func OpenStoreBackendAt(ctx context.Context, exec recordstore.ExecutorI, alloc memory.Allocator, table string) (inst *StoreBackend, err error) {
	if exec == nil {
		err = eh.Errorf("persist: OpenStoreBackend: nil executor")
		return
	}
	if table != "" {
		err = recordstore.CheckTableRef(table)
		if err != nil {
			err = eh.Errorf("persist: OpenStoreBackendAt: %w", err)
			return
		}
	}
	st := persiststore.NewPersistStore(exec, alloc, persiststore.PersistStoreConfig{Table: table})
	err = st.EnsureTable(ctx)
	if err != nil {
		err = eh.Errorf("persist: open store backend: %w", err)
		return
	}
	err = st.VerifySchema(ctx)
	if err != nil {
		err = eh.Errorf("persist: open store backend: %w", err)
		return
	}
	inst = &StoreBackend{
		st: st,
		pc: persiststore.NewPersistCache[struct{}](st, persiststore.PersistCacheConfig{}),
	}
	if ri, riErr := runinfo.Get(); riErr == nil {
		inst.runId = ri.RunId
	}
	return
}

// stateKey is the entity key: the app identity and the app's own key,
// joined the way pushoutstore namespaces its keys. The app id rather
// than the alias, so a key survives an alias change, matching what the
// facts-backed predecessor stamped on its rows.
func stateKey(ref StorageRef, key string) (id string) {
	return string(ref.StateAppId()) + "/" + key
}

// Get resolves (app, key) to the latest value. A key that was never
// written and a key whose newest row is a tombstone both report
// found=false — the semantics the persist contract promises (and the
// facts-bound predecessor answered). An empty stored value round-trips
// as found=true with a zero-length slice, distinct from absent.
//
// The read goes through the cache view: a Set writes through at Commit,
// so the next Get for that key is answered without a round-trip, which
// is what makes a write-then-read pair cheap.
func (inst *StoreBackend) Get(ref StorageRef, key string) (value []byte, found bool, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	ent, found, err := inst.pc.GetFetch(context.Background(), stateKey(ref, key))
	if err != nil {
		err = eh.Errorf("persist: store get %s.%s: %w", ref.Alias, key, err)
		found = false
		return
	}
	if !found || ent.IsTombstone() {
		found = false
		return
	}
	if !ent.State.Has {
		// A live row with no State component is not a shape this backend
		// writes. Reporting absent would silently lose a value someone
		// else's writer put there; saying so names the real condition.
		err = eh.Errorf("persist: store get %s.%s: row carries no state component", ref.Alias, key)
		found = false
		return
	}
	value = ent.State.Val.Value
	return
}

// Set appends a new version. Append-only: the previous row stays, and
// the newest one wins the state view.
func (inst *StoreBackend) Set(ref StorageRef, key string, value []byte) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	id := stateKey(ref, key)
	err = inst.st.Begin(id, time.Now().UTC()).AddState(persiststore.State{
		ID:    id,
		AppId: string(ref.StateAppId()),
		Key:   key,
		Value: value,
		// Provenance (ADR-0191 §SD5), not identity: the id above is still
		// "<appId>/<key>", so a second window writing this key overwrites
		// rather than forks. The tombstone Delete appends carries neither,
		// because the generated Delete writes no component at all.
		RunId:       inst.runId,
		InstanceKey: ref.InstanceKey,
	}).Commit()
	if err != nil {
		inst.st.DiscardPending()
		err = eh.Errorf("persist: store set %s.%s: %w", ref.Alias, key, err)
		return
	}
	err = inst.flushLocked()
	if err != nil {
		err = eh.Errorf("persist: store set %s.%s: %w", ref.Alias, key, err)
	}
	return
}

// Delete appends a tombstone. Deleting a never-written key is not an
// error, matching MemoryBackend and the facts-backed predecessor; the
// tombstone simply becomes the newest row for a key that had none.
func (inst *StoreBackend) Delete(ref StorageRef, key string) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	err = inst.st.Delete(stateKey(ref, key), time.Now().UTC())
	if err != nil {
		inst.st.DiscardPending()
		err = eh.Errorf("persist: store delete %s.%s: %w", ref.Alias, key, err)
		return
	}
	err = inst.flushLocked()
	if err != nil {
		err = eh.Errorf("persist: store delete %s.%s: %w", ref.Alias, key, err)
	}
	return
}

// flushLocked makes the operation's rows durable, discarding them on
// failure so a failed operation cannot ship behind a later one. The
// cache is invalidated on that path too: its write-through entry is
// pinned on a row that will never exist.
func (inst *StoreBackend) flushLocked() (err error) {
	_, err = inst.st.Flush(context.Background())
	if err != nil {
		inst.st.DiscardPending()
		inst.pc.InvalidateAll()
	}
	return
}

// Close releases the store's buffers. The backend is unusable after it.
func (inst *StoreBackend) Close() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.st.Close()
}
