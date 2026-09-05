package repo

import (
	"context"
	"iter"
	"slices"

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

// Envelope is one content-addressed envelope as the batch verbs carry
// it: the patch hash it is filed under and its framed bytes.
type Envelope struct {
	Hash   t.PatchHash
	Framed []byte
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

// RetentionDelta is one change to the retention ledger (ADR-0221): the
// entries to set — replacing whatever the ledger held for those nodes —
// and the nodes to drop (a tombstone resurrected by Unrecord). The
// engine writes the ledger only as deltas, so an append-only store
// appends one row per verb instead of rewriting the set; a store that
// keeps the set whole folds the delta in (ApplyRetentionDelta). A node
// never appears in both lists.
type RetentionDelta struct {
	Upsert []RetentionEntry
	Remove []t.NodeID
}

// IsEmpty reports whether the delta changes nothing.
func (inst RetentionDelta) IsEmpty() bool {
	return len(inst.Upsert) == 0 && len(inst.Remove) == 0
}

// ApplyRetentionDelta folds delta into entries and returns the new set
// (order unspecified). Applying a delta twice yields the same set as
// applying it once, which is what makes a re-driven write after a crash
// safe. A store that keeps the ledger as one value uses it verbatim.
func ApplyRetentionDelta(entries []RetentionEntry, delta RetentionDelta) (out []RetentionEntry) {
	byNode := make(map[t.NodeID]RetentionEntry, len(entries)+len(delta.Upsert))
	for _, e := range entries {
		byNode[e.Node] = e
	}
	for _, e := range delta.Upsert {
		byNode[e.Node] = e
	}
	for _, id := range delta.Remove {
		delete(byNode, id)
	}
	out = make([]RetentionEntry, 0, len(byNode))
	for _, e := range byNode {
		out = append(out, e)
	}
	return
}

// DiffRetention returns the delta that turns the set prev into the set
// next: every entry of next that prev lacks or holds differently is an
// upsert, every node of prev absent from next is a removal. Both lists
// come out in CompareNodeID order, so the delta is deterministic.
func DiffRetention(prev, next []RetentionEntry) (delta RetentionDelta) {
	old := make(map[t.NodeID]RetentionEntry, len(prev))
	for _, e := range prev {
		old[e.Node] = e
	}
	seen := make(map[t.NodeID]struct{}, len(next))
	for _, e := range next {
		seen[e.Node] = struct{}{}
		if o, ok := old[e.Node]; !ok || o != e {
			delta.Upsert = append(delta.Upsert, e)
		}
	}
	for id := range old {
		if _, ok := seen[id]; !ok {
			delta.Remove = append(delta.Remove, id)
		}
	}
	slices.SortFunc(delta.Upsert, func(a, b RetentionEntry) int { return t.CompareNodeID(a.Node, b.Node) })
	slices.SortFunc(delta.Remove, t.CompareNodeID)
	return
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
	// RetentionLedger: UpdateRetention persists and LoadRetention returns
	// the folded ledger, so tombstone stamps and purge markers survive a
	// restart. When false the engine never writes the ledger, stamps
	// reset to replay time on recovery, and a Sweep's purges do not
	// outlive the process.
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
// The seam is written for an append-only store as much as for a
// directory of files (ADR-0221): the batch verbs let one engine verb be
// one storage write, and the ledger is written as deltas. A store with
// a single-write path uses the batch verbs directly; any other store
// loops through the one-by-one helpers (PutEnvelopesOneByOne,
// LoadEnvelopesOneByOne) — that is conformant.
//
// Contract (verified by repo/storagetest — run it against every
// implementation):
//
//   - PutEnvelope is idempotent for equal (hash, bytes) and atomic: a
//     reader never observes a partial envelope. Envelopes are immutable
//     once written; re-putting different bytes for an existing hash MAY
//     be ignored (first write wins).
//   - PutEnvelopes has the contract of consecutive PutEnvelope calls:
//     every envelope is durable when it returns; a crash mid-batch may
//     persist any subset (orphans are harmless, the log is the commit
//     point).
//   - GetEnvelope returns bytes equal to those put; a missing hash
//     yields an error matching repo.ErrEnvelopeNotFound via errors.Is.
//   - LoadEnvelopes yields one Envelope per requested hash, in request
//     order, with the same bytes GetEnvelope would return; a missing
//     hash ends the sequence with an error matching ErrEnvelopeNotFound.
//     The sequence is single-use and ctx must stay valid until it ends;
//     the yielded Framed slices are the caller's to keep.
//   - AppendApplied appends one hash to the log and is durable when it
//     returns. LoadApplied returns the appended hashes in order; an
//     interrupted trailing append (torn tail) is silently dropped — the
//     engine never acknowledged that operation.
//   - AppendAppliedBatch has the contract of consecutive AppendApplied
//     calls: durable when it returns, and a crash mid-batch may persist a
//     proper prefix. The engine hands over batches in dependency order
//     so every prefix is dependency-closed.
//   - ReplaceApplied atomically replaces the whole log (the unrecord
//     path): readers and crash-recovery observe either the old or the
//     new list, never a mixture. A store that cannot returns
//     ErrUnsupported and declares Capabilities.ReplaceApplied false.
//   - SaveSnapshot atomically replaces the snapshot; LoadSnapshot
//     reports ok=false when none exists. A snapshot is opaque to the
//     storage layer. A store that declares Capabilities.Snapshots false
//     may discard saves; its LoadSnapshot always reports none.
//   - UpdateRetention applies one RetentionDelta to the ledger
//     atomically — a reader or a crash-reopen observes the ledger before
//     the delta or after it, never in between — and durably, and is
//     idempotent: applying the same delta again changes nothing.
//     LoadRetention returns the folded ledger in unspecified order
//     (treat as a set; one entry per node) and an empty slice when none
//     exists. Entries round-trip their Purged flag. A store without the
//     ledger declares Capabilities.RetentionLedger false; its
//     LoadRetention always returns empty.
//   - Capabilities reports the truth about the above and is stable for
//     the life of the store.
//   - All durability promises must hold across Close + reopen of the
//     same location.
//   - Methods are called under the engine's locks; implementations need
//     not add their own ordering guarantees beyond per-call atomicity,
//     but must be safe for concurrent READS (Get/Load/Has).
type StorageI interface {
	PutEnvelope(ctx context.Context, h t.PatchHash, framed []byte) error
	PutEnvelopes(ctx context.Context, envs []Envelope) error
	GetEnvelope(ctx context.Context, h t.PatchHash) ([]byte, error)
	LoadEnvelopes(ctx context.Context, hs []t.PatchHash) iter.Seq2[Envelope, error]
	HasEnvelope(ctx context.Context, h t.PatchHash) (bool, error)

	AppendApplied(ctx context.Context, h t.PatchHash) error
	AppendAppliedBatch(ctx context.Context, hs []t.PatchHash) error
	ReplaceApplied(ctx context.Context, hs []t.PatchHash) error
	LoadApplied(ctx context.Context) ([]t.PatchHash, error)

	SaveSnapshot(ctx context.Context, snap Snapshot) error
	LoadSnapshot(ctx context.Context) (snap Snapshot, ok bool, err error)

	UpdateRetention(ctx context.Context, delta RetentionDelta) error
	LoadRetention(ctx context.Context) ([]RetentionEntry, error)

	Capabilities() Capabilities
	Close() error
}

// PutEnvelopesOneByOne is PutEnvelopes for a store whose only write
// path is PutEnvelope: a loop, conformant by the contract's definition.
func PutEnvelopesOneByOne(ctx context.Context, st StorageI, envs []Envelope) (err error) {
	for _, e := range envs {
		if err = ctx.Err(); err != nil {
			return
		}
		if err = st.PutEnvelope(ctx, e.Hash, e.Framed); err != nil {
			return
		}
	}
	return
}

// LoadEnvelopesOneByOne is LoadEnvelopes for a store whose only read
// path is GetEnvelope: one read per hash, yielded in request order.
func LoadEnvelopesOneByOne(ctx context.Context, st StorageI, hs []t.PatchHash) iter.Seq2[Envelope, error] {
	return func(yield func(Envelope, error) bool) {
		for _, h := range hs {
			if err := ctx.Err(); err != nil {
				yield(Envelope{Hash: h}, err)
				return
			}
			framed, err := st.GetEnvelope(ctx, h)
			if err != nil {
				yield(Envelope{Hash: h}, err)
				return
			}
			if !yield(Envelope{Hash: h, Framed: framed}, nil) {
				return
			}
		}
	}
}
