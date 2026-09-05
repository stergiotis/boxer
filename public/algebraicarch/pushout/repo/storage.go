package repo

import (
	"context"

	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/types"
)

// Snapshot is a persisted acceleration point: the pushoutgraph state
// (via store.EncodeSnapshot) after applying exactly the patches in
// Applied, in that order. Since ADR-0220 the purge markers ride the
// retention ledger, so a snapshot holds nothing the log, the envelopes
// and the ledger cannot rebuild. Recovery uses it only when every hash in Applied occurs
// in the current applied log (a subset, not necessarily a prefix — the
// remaining log entries are replayed on top in log order); otherwise the
// snapshot is discarded and the log is replayed from empty. Correctness
// never depends on snapshot freshness, only on the log, the envelopes
// and the ledger; a store may disclaim snapshots (Capabilities.Snapshots)
// and recovery then replays in full.
type Snapshot struct {
	Applied      []t.PatchHash
	PushoutGraph []byte
}

// RetentionEntry is one tombstoned node's replica-local retention
// state: its first-observed-deleted time (unix nanoseconds), the durable
// form of the pushoutgraph's tombstoneAt stamp, and whether a Sweep has
// purged its content. The retention ledger exists because neither fact
// can be rebuilt from envelopes: a full replay would re-stamp every
// tombstone to replay time, and would re-materialise content a sweep
// destroyed (ADR-0079, ADR-0220). Unlike the Snapshot, the ledger is
// never prefix-gated — it is always authoritative — and it is
// replica-local policy, never shipped over the wire.
type RetentionEntry struct {
	Node     t.NodeID
	UnixNano int64
	Purged   bool
}

// Capabilities is a store's declaration of what it persists. The engine
// reads it once at Open, derives the consumer-facing [Guarantees] from
// it together with the retention mode, and never inspects the store's
// dynamic type. A decorator that embeds a StorageI inherits the
// declaration and must override it when it changes the truth.
//
// Every flag is checked by repo/storagetest: a claimed capability gets
// its positive check, a disclaimed one gets the matching negative check.
// A declaration that does not match behaviour is a conformance failure.
type Capabilities struct {
	// Snapshots: SaveSnapshot persists and LoadSnapshot returns the last
	// saved snapshot. When false both are advisory — the engine never
	// calls them — and recovery always replays the log in full.
	Snapshots bool
	// RetentionLedger: SaveRetention persists and LoadRetention returns
	// it, so tombstone stamps and purge markers survive a restart. When
	// false the engine never writes the ledger, stamps reset to replay
	// time on recovery, and a Sweep's purges do not outlive the process.
	RetentionLedger bool
	// ReplaceApplied: the log can be rewritten atomically. When false,
	// Unrecord is refused with ErrUnsupported.
	ReplaceApplied bool
	// ExactEnvelopeBytes: GetEnvelope returns the bytes PutEnvelope was
	// given. When false the store re-encodes on read; identity is
	// unaffected (the hash is over the canonical item, not the frame),
	// the engine's read-side hash check remains the guard, and
	// EncodedEnvelope may ship a different frame than was received.
	ExactEnvelopeBytes bool
}

// AllCapabilities is the declaration of a store that persists
// everything the seam offers — the filestore's shape.
func AllCapabilities() Capabilities {
	return Capabilities{Snapshots: true, RetentionLedger: true, ReplaceApplied: true, ExactEnvelopeBytes: true}
}

// StorageI is the persistence seam of the repo engine. Implementations
// provide atomic, durable primitives; the ENGINE owns operation
// sequencing (envelope before log append before in-memory commit), so a
// crash at any point leaves either "operation never happened" or a
// harmless orphan envelope (content-addressed, never referenced by the
// log).
//
// Contract (verified by repo/storagetest — run it against every
// implementation):
//
//   - PutEnvelope is idempotent for equal (hash, bytes) and atomic: a
//     reader never observes a partial envelope. Envelopes are immutable
//     once written; re-putting different bytes for an existing hash MAY
//     be ignored (first write wins).
//   - GetEnvelope returns bytes equal to those put; a missing hash
//     yields an error matching repo.ErrEnvelopeNotFound via errors.Is.
//   - AppendApplied appends one hash to the log and is durable when it
//     returns. LoadApplied returns the appended hashes in order; an
//     interrupted trailing append (torn tail) is silently dropped — the
//     engine never acknowledged that operation.
//   - AppendAppliedBatch has the contract of consecutive AppendApplied
//     calls: durable when it returns, and a crash mid-batch may persist a
//     proper prefix. The engine hands over batches in dependency order
//     so every prefix is dependency-closed. A store with a single-write
//     path uses it; any other store loops.
//   - ReplaceApplied atomically replaces the whole log (the unrecord
//     path): readers and crash-recovery observe either the old or the
//     new list, never a mixture. A store that cannot returns
//     ErrUnsupported and declares Capabilities.ReplaceApplied false.
//   - SaveSnapshot atomically replaces the snapshot; LoadSnapshot
//     reports ok=false when none exists. A snapshot is opaque to the
//     storage layer. A store that declares Capabilities.Snapshots false
//     may discard saves; its LoadSnapshot always reports none.
//   - SaveRetention atomically replaces the whole retention ledger (not
//     an append); LoadRetention returns the entries in unspecified order
//     (treat as a set) and an empty slice when none exists. The ledger
//     is durable when SaveRetention returns. Entries round-trip their
//     Purged flag.
//   - Capabilities reports the truth about the above and is stable for
//     the life of the store.
//   - All durability promises must hold across Close + reopen of the
//     same location.
//   - Methods are called under the engine's locks; implementations need
//     not add their own ordering guarantees beyond per-call atomicity,
//     but must be safe for concurrent READS (Get/Load/Has).
type StorageI interface {
	PutEnvelope(ctx context.Context, h t.PatchHash, framed []byte) error
	GetEnvelope(ctx context.Context, h t.PatchHash) ([]byte, error)
	HasEnvelope(ctx context.Context, h t.PatchHash) (bool, error)

	AppendApplied(ctx context.Context, h t.PatchHash) error
	AppendAppliedBatch(ctx context.Context, hs []t.PatchHash) error
	ReplaceApplied(ctx context.Context, hs []t.PatchHash) error
	LoadApplied(ctx context.Context) ([]t.PatchHash, error)

	SaveSnapshot(ctx context.Context, snap Snapshot) error
	LoadSnapshot(ctx context.Context) (snap Snapshot, ok bool, err error)

	SaveRetention(ctx context.Context, entries []RetentionEntry) error
	LoadRetention(ctx context.Context) ([]RetentionEntry, error)

	Capabilities() Capabilities
	Close() error
}
