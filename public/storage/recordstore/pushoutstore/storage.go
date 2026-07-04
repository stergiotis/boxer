package pushoutstore

import (
	"context"
	"encoding/hex"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// Storage adapts the generated PushoutStore to pushout's repo.StorageI
// (ADR-0100 S3 — the conformance gate for the recordstore primitive set;
// repo/storagetest verifies the contract).
//
// Mapping (one fact table, string keys as namespaces):
//   - envelopes:  key "env/<hex hash>", immutable; PutEnvelope is
//     first-write-wins via a read-before-insert (StorageI writes are
//     serialized under the engine's locks, so check-then-insert cannot
//     race); GetEnvelope/HasEnvelope ride the batched read-through cache
//     (immutable-by-key — the ideal case), falling back to the uncached
//     Latest for the authoritative absent-vs-error answer.
//   - applied log: key "log", one row per entry; the row's ts is a
//     synthetic per-key sequence (Unix nanos 1,2,3…), so Replay returns
//     append order. ReplaceApplied appends a state-view tombstone
//     followed by the new entries — one Flush, one Arrow insert, so
//     readers observe the old or the new log, never a mixture — and
//     LoadApplied keeps the entries after the last tombstone.
//   - snapshot / retention: keys "snapshot" / "retention", whole value
//     as one row per save, latest-wins via Latest. The retention ledger
//     rides three aligned arrays (hash, index, nanos), one element per
//     entry.
//
// Durability: every mutating method Commits and Flushes before returning
// (one synchronous Arrow insert per operation) — durable-on-return over
// a durable engine, per the StorageI contract. A failed Flush discards
// the operation's rows (DiscardPending), so a failed operation stays
// "never happened" — it must not ship behind a later operation's back.
//
// Concurrency: the embedded store and cache are single-goroutine, so one
// mutex serializes every method — writes are engine-locked anyway, and
// the mutex makes concurrent reads safe as the contract requires.
type Storage struct {
	mu   sync.Mutex
	st   *PushoutStore
	pc   *PushoutCache[struct{}]
	seqs map[string]uint64 // next per-key ts sequence, lazily derived
}

var _ repo.StorageI = (*Storage)(nil)

const (
	logKey       = "log"
	snapshotKey  = "snapshot"
	retentionKey = "retention"
)

// Open builds the adapter over an executor and ensures the table exists.
// Reopening the same location (executor state) resumes durably: the
// per-key sequences re-derive lazily from storage. The attached cache
// view serves the envelope reads (immutable-by-key — the ideal case).
func Open(ctx context.Context, exec recordstore.ExecutorI, alloc memory.Allocator, storeCfg PushoutStoreConfig, cacheCfg PushoutCacheConfig) (inst *Storage, err error) {
	st := NewPushoutStore(exec, alloc, storeCfg)
	err = st.EnsureTable(ctx)
	if err != nil {
		err = eh.Errorf("open pushout storage: %w", err)
		return
	}
	err = st.VerifySchema(ctx)
	if err != nil {
		err = eh.Errorf("open pushout storage: %w", err)
		return
	}
	inst = &Storage{st: st, pc: NewPushoutCache[struct{}](st, cacheCfg), seqs: make(map[string]uint64)}
	return
}

func envKey(h types.PatchHash) string {
	return "env/" + hex.EncodeToString(h[:])
}

func hexOfHash(h types.PatchHash) string {
	return hex.EncodeToString(h[:])
}

func hashFromHex(s string) (h types.PatchHash, err error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		err = eh.Errorf("decode patch hash %q: %w", s, err)
		return
	}
	if len(b) != len(h) {
		err = eh.Errorf("decode patch hash %q: %d bytes, want %d", s, len(b), len(h))
		return
	}
	copy(h[:], b)
	return
}

// nextTs hands out the key's next synthetic order timestamp. On first
// use after Open the sequence derives from the newest stored row's ts —
// never from a row count: failed operations burn sequence numbers
// without leaving rows, and a count-derived restart would reuse a taken
// ts, making the Replay order of the colliding rows ambiguous.
func (inst *Storage) nextTs(ctx context.Context, key string) (ts time.Time, err error) {
	n, ok := inst.seqs[key]
	if !ok {
		var ent *PushoutEntity
		var found bool
		ent, found, err = inst.st.Latest(ctx, key)
		if err != nil {
			err = eh.Errorf("derive sequence for %q: %w", key, err)
			return
		}
		n = 1
		if found {
			n = recordstore.SeqOf(ent.Ts) + 1
		}
	}
	inst.seqs[key] = n + 1
	ts = recordstore.SeqTs(n)
	return
}

func (inst *Storage) flush(ctx context.Context) (err error) {
	_, err = inst.st.Flush(ctx)
	if err != nil {
		// Per-op contract: a failed operation "never happened". Drop the
		// buffered rows rather than let a later operation ship them.
		inst.st.DiscardPending()
	}
	return
}

// --- envelopes. ---

func (inst *Storage) PutEnvelope(ctx context.Context, h types.PatchHash, framed []byte) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	key := envKey(h)
	// First write wins: envelopes are immutable, re-putting different
	// bytes for an existing hash is ignored per the contract.
	_, found, err := inst.st.Latest(ctx, key)
	if err != nil {
		err = eh.Errorf("put envelope existence check: %w", err)
		return
	}
	if found {
		return
	}
	// Envelopes are immutable single-row keys: the row always carries
	// sequence 1 (the Latest check above just said the key is empty), so
	// no per-key sequence derivation — and no second query — is needed.
	ts := recordstore.SeqTs(1)
	b := inst.st.Begin(key, ts)
	b.AddEnvelope(Envelope{ID: key, Framed: framed})
	err = b.Commit()
	if err != nil {
		err = eh.Errorf("put envelope commit: %w", err)
		return
	}
	err = inst.flush(ctx)
	if err != nil {
		err = eh.Errorf("put envelope flush: %w", err)
	}
	return
}

// getEnvelopeLocked serves an envelope through the cache view's
// single-lookup read: cached hit, or one immediate batched fetch with
// the fetch error surfaced — the authoritative absent-vs-error answer.
func (inst *Storage) getEnvelopeLocked(ctx context.Context, key string) (ent *PushoutEntity, found bool, err error) {
	return inst.pc.GetFetch(ctx, key)
}

func (inst *Storage) GetEnvelope(ctx context.Context, h types.PatchHash) (framed []byte, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	ent, found, err := inst.getEnvelopeLocked(ctx, envKey(h))
	if err != nil {
		err = eh.Errorf("get envelope: %w", err)
		return
	}
	if !found || !ent.Envelope.Has {
		err = eh.Errorf("get envelope %s: %w", hexOfHash(h), repo.ErrEnvelopeNotFound)
		return
	}
	framed = ent.Envelope.Val.Framed
	return
}

func (inst *Storage) HasEnvelope(ctx context.Context, h types.PatchHash) (ok bool, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	ent, found, err := inst.getEnvelopeLocked(ctx, envKey(h))
	if err != nil {
		err = eh.Errorf("has envelope: %w", err)
		return
	}
	ok = found && ent.Envelope.Has
	return
}

// --- applied log. ---

func (inst *Storage) AppendApplied(ctx context.Context, h types.PatchHash) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	ts, err := inst.nextTs(ctx, logKey)
	if err != nil {
		return
	}
	b := inst.st.Begin(logKey, ts)
	b.AddLogEntry(LogEntry{ID: logKey, Hash: hexOfHash(h)})
	err = b.Commit()
	if err != nil {
		err = eh.Errorf("append applied commit: %w", err)
		return
	}
	err = inst.flush(ctx)
	if err != nil {
		err = eh.Errorf("append applied flush: %w", err)
	}
	return
}

func (inst *Storage) ReplaceApplied(ctx context.Context, hs []types.PatchHash) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	// Multi-commit operation: an error between the tombstone and the last
	// entry leaves earlier rows buffered — flush()'s own discard only
	// covers flush failures, so drop them here (idempotent with it).
	defer func() {
		if err != nil {
			inst.st.DiscardPending()
		}
	}()
	ts, err := inst.nextTs(ctx, logKey)
	if err != nil {
		return
	}
	// The tombstone resets the log; the new entries follow in the same
	// buffered batch, so one Flush makes the whole replacement visible
	// atomically (a single Arrow insert).
	err = inst.st.Delete(logKey, ts)
	if err != nil {
		err = eh.Errorf("replace applied tombstone: %w", err)
		return
	}
	for _, h := range hs {
		ts, err = inst.nextTs(ctx, logKey)
		if err != nil {
			return
		}
		b := inst.st.Begin(logKey, ts)
		b.AddLogEntry(LogEntry{ID: logKey, Hash: hexOfHash(h)})
		err = b.Commit()
		if err != nil {
			err = eh.Errorf("replace applied commit: %w", err)
			return
		}
	}
	err = inst.flush(ctx)
	if err != nil {
		err = eh.Errorf("replace applied flush: %w", err)
	}
	return
}

func (inst *Storage) LoadApplied(ctx context.Context) (hs []types.PatchHash, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for row, rerr := range inst.st.Replay(ctx, logKey, recordstore.SeqTs(0), recordstore.ReplayOpts{}) {
		if rerr != nil {
			hs = nil
			err = eh.Errorf("load applied: %w", rerr)
			return
		}
		if row.Lifecycle == recordstore.LifecycleTombstone {
			hs = nil // a ReplaceApplied reset — keep only what follows
			continue
		}
		if !row.LogEntry.Has {
			continue
		}
		var h types.PatchHash
		h, err = hashFromHex(row.LogEntry.Val.Hash)
		if err != nil {
			hs = nil
			return
		}
		hs = append(hs, h)
	}
	// Deliberately no sequence refresh here: the row count is NOT the
	// high-water mark once a failed operation has burned a sequence
	// number (nextTs derives from the newest row's ts instead).
	return
}

// --- snapshot. ---

func (inst *Storage) SaveSnapshot(ctx context.Context, snap repo.Snapshot) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	ts, err := inst.nextTs(ctx, snapshotKey)
	if err != nil {
		return
	}
	applied := make([]string, 0, len(snap.Applied))
	for _, h := range snap.Applied {
		applied = append(applied, hexOfHash(h))
	}
	b := inst.st.Begin(snapshotKey, ts)
	b.AddSnapshot(Snapshot{ID: snapshotKey, Applied: applied, Graggle: snap.Graggle})
	err = b.Commit()
	if err != nil {
		err = eh.Errorf("save snapshot commit: %w", err)
		return
	}
	err = inst.flush(ctx)
	if err != nil {
		err = eh.Errorf("save snapshot flush: %w", err)
	}
	return
}

func (inst *Storage) LoadSnapshot(ctx context.Context) (snap repo.Snapshot, ok bool, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	ent, found, err := inst.st.Latest(ctx, snapshotKey)
	if err != nil {
		err = eh.Errorf("load snapshot: %w", err)
		return
	}
	// Graggle is a scalar blob and writes unconditionally (even empty),
	// so a saved snapshot always reads back component-present; only the
	// Applied container is elided when empty (nil round-trips as nil).
	// The Has check stays as a defensive guard.
	if !found || !ent.Snapshot.Has {
		return
	}
	for _, s := range ent.Snapshot.Val.Applied {
		var h types.PatchHash
		h, err = hashFromHex(s)
		if err != nil {
			snap = repo.Snapshot{}
			return
		}
		snap.Applied = append(snap.Applied, h)
	}
	snap.Graggle = ent.Snapshot.Val.Graggle
	ok = true
	return
}

// --- retention ledger. ---

func (inst *Storage) SaveRetention(ctx context.Context, entries []repo.RetentionEntry) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	ts, err := inst.nextTs(ctx, retentionKey)
	if err != nil {
		return
	}
	ret := Retention{
		ID:      retentionKey,
		Hashes:  make([]string, 0, len(entries)),
		Indices: make([]uint64, 0, len(entries)),
		Times:   make([]int64, 0, len(entries)),
	}
	for _, e := range entries {
		ret.Hashes = append(ret.Hashes, hexOfHash(e.Node.Patch))
		ret.Indices = append(ret.Indices, e.Node.Index)
		ret.Times = append(ret.Times, e.UnixNano)
	}
	b := inst.st.Begin(retentionKey, ts)
	b.AddRetention(ret)
	err = b.Commit()
	if err != nil {
		err = eh.Errorf("save retention commit: %w", err)
		return
	}
	err = inst.flush(ctx)
	if err != nil {
		err = eh.Errorf("save retention flush: %w", err)
	}
	return
}

func (inst *Storage) LoadRetention(ctx context.Context) (entries []repo.RetentionEntry, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	ent, found, err := inst.st.Latest(ctx, retentionKey)
	if err != nil {
		err = eh.Errorf("load retention: %w", err)
		return
	}
	if !found || !ent.Retention.Has {
		return
	}
	ret := ent.Retention.Val
	if len(ret.Hashes) != len(ret.Indices) || len(ret.Hashes) != len(ret.Times) {
		err = eh.Errorf("retention ledger arrays misaligned: %d/%d/%d", len(ret.Hashes), len(ret.Indices), len(ret.Times))
		return
	}
	for i := range ret.Hashes {
		var h types.PatchHash
		h, err = hashFromHex(ret.Hashes[i])
		if err != nil {
			entries = nil
			return
		}
		entries = append(entries, repo.RetentionEntry{
			Node:     types.NodeID{Patch: h, Index: ret.Indices[i]},
			UnixNano: ret.Times[i],
		})
	}
	return
}

// Close releases nothing: the executor owns no long-lived resources and
// every mutating method flushed synchronously — durability never depends
// on Close.
func (inst *Storage) Close() error { return nil }
