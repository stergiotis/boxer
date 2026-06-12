package repo

import (
	"context"

	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

// Snapshot is a persisted acceleration point: the graggle state (via
// store.EncodeSnapshot) after applying exactly the patches in Applied,
// in that order. Recovery uses it only when Applied is a PREFIX of the
// current applied log; otherwise the snapshot is discarded and the log
// is replayed from empty — correctness never depends on snapshot
// freshness, only on the log and the envelopes.
type Snapshot struct {
	Applied []t.PatchHash
	Graggle []byte
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
//   - ReplaceApplied atomically replaces the whole log (the unrecord
//     path): readers and crash-recovery observe either the old or the
//     new list, never a mixture.
//   - SaveSnapshot atomically replaces the snapshot; LoadSnapshot
//     reports ok=false when none exists. A snapshot is opaque to the
//     storage layer.
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
	ReplaceApplied(ctx context.Context, hs []t.PatchHash) error
	LoadApplied(ctx context.Context) ([]t.PatchHash, error)

	SaveSnapshot(ctx context.Context, snap Snapshot) error
	LoadSnapshot(ctx context.Context) (snap Snapshot, ok bool, err error)

	Close() error
}
