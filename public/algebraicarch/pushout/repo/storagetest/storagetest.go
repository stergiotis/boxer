// Package storagetest is the executable conformance contract for
// [repo.StorageI] implementations. Run drives the full suite; each
// requirement is also a Check* function returning an error so suites
// can be meta-tested against deliberately broken stores.
//
// The suite follows the store's declared [repo.Capabilities]: every
// claimed capability gets its positive check and every disclaimed one
// gets the matching negative check, so a declaration that does not
// match behaviour fails either way. Envelope byte-equality is checked
// only for stores that claim ExactEnvelopeBytes; a store that
// re-encodes is exercised through the engine's own tests instead, since
// this suite's envelope fixtures are opaque bytes. The batch verbs
// (PutEnvelopes, LoadEnvelopes, AppendAppliedBatch) are checked against
// their one-by-one counterparts, and the retention ledger is checked as
// what it is since ADR-0221 — a set folded from deltas.
//
// Implementors: the factory you pass to Run must open a store over the
// given location string, and OPENING THE SAME LOCATION AGAIN after
// Close must observe everything previously written — the suite checks
// durability across reopen, which is the property the engine's
// crash-recovery depends on.
package storagetest

import (
	"bytes"
	"context"
	"errors"
	"testing"

	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/types"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// OpenFunc opens (creating on first use) a store at location.
type OpenFunc func(location string) (repo.StorageI, error)

func h(b byte) (out t.PatchHash) {
	for i := range out {
		out[i] = b
	}
	return
}

func nid(b byte, idx uint64) t.NodeID {
	return t.NodeID{Patch: h(b), Index: idx}
}

func retentionSetEqual(got, want []repo.RetentionEntry) bool {
	if len(got) != len(want) {
		return false
	}
	type stamp struct {
		nanos  int64
		purged bool
	}
	m := make(map[t.NodeID]stamp, len(got))
	for _, e := range got {
		m[e.Node] = stamp{e.UnixNano, e.Purged}
	}
	for _, e := range want {
		if v, ok := m[e.Node]; !ok || v != (stamp{e.UnixNano, e.Purged}) {
			return false
		}
	}
	return true
}

// CheckEnvelopes: put/get/has round-trip, idempotent re-put,
// first-write-wins immutability, ErrEnvelopeNotFound on miss. Skipped
// for a store that disclaims ExactEnvelopeBytes: its fixtures would
// have to be decodable envelopes, which this suite does not carry; the
// engine's own tests over such a store are its conformance gate.
func CheckEnvelopes(ctx context.Context, open OpenFunc, location string) (err error) {
	st, err := open(location)
	if err != nil {
		return eh.Errorf("open: %w", err)
	}
	defer st.Close()
	if !st.Capabilities().ExactEnvelopeBytes {
		return
	}

	data := []byte("PXE1\x05opaq1\x00\x01\x02")
	if err = st.PutEnvelope(ctx, h(1), data); err != nil {
		return eh.Errorf("put: %w", err)
	}
	if err = st.PutEnvelope(ctx, h(1), data); err != nil {
		return eh.Errorf("idempotent re-put: %w", err)
	}
	got, err := st.GetEnvelope(ctx, h(1))
	if err != nil {
		return eh.Errorf("get: %w", err)
	}
	if !bytes.Equal(got, data) {
		return eh.Errorf("get returned %q, want %q", got, data)
	}
	ok, err := st.HasEnvelope(ctx, h(1))
	if err != nil || !ok {
		return eh.Errorf("has(present) = %v, %v", ok, err)
	}
	ok, err = st.HasEnvelope(ctx, h(2))
	if err != nil || ok {
		return eh.Errorf("has(absent) = %v, %v", ok, err)
	}
	if _, err2 := st.GetEnvelope(ctx, h(2)); !errors.Is(err2, repo.ErrEnvelopeNotFound) {
		return eh.Errorf("get(absent): want ErrEnvelopeNotFound, got %v", err2)
	}
	// First write wins: a different payload for the same hash must not
	// replace the original (envelopes are immutable).
	if err = st.PutEnvelope(ctx, h(1), []byte("OTHER")); err != nil {
		return eh.Errorf("re-put different bytes: %w", err)
	}
	got, err = st.GetEnvelope(ctx, h(1))
	if err != nil || !bytes.Equal(got, data) {
		return eh.Errorf("envelope mutated by re-put: %q, %v", got, err)
	}
	return
}

// CheckAppliedLog: append order, replace semantics, empty-on-fresh.
func CheckAppliedLog(ctx context.Context, open OpenFunc, location string) (err error) {
	st, err := open(location)
	if err != nil {
		return eh.Errorf("open: %w", err)
	}
	defer st.Close()

	got, err := st.LoadApplied(ctx)
	if err != nil || len(got) != 0 {
		return eh.Errorf("fresh load = %v, %v (want empty, nil)", got, err)
	}
	want := []t.PatchHash{h(1), h(2), h(3)}
	for _, x := range want {
		if err = st.AppendApplied(ctx, x); err != nil {
			return eh.Errorf("append: %w", err)
		}
	}
	got, err = st.LoadApplied(ctx)
	if err != nil {
		return eh.Errorf("load: %w", err)
	}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		return eh.Errorf("load order mismatch: %v", got)
	}
	replaced := []t.PatchHash{h(1), h(3)}
	if !st.Capabilities().ReplaceApplied {
		// Disclaimed: the store must refuse, and the log must be intact.
		if rerr := st.ReplaceApplied(ctx, replaced); !errors.Is(rerr, repo.ErrUnsupported) {
			return eh.Errorf("ReplaceApplied disclaimed but returned %v (want ErrUnsupported)", rerr)
		}
		got, err = st.LoadApplied(ctx)
		if err != nil || len(got) != 3 {
			return eh.Errorf("refused replace changed the log: %v, %v", got, err)
		}
		return
	}
	if err = st.ReplaceApplied(ctx, replaced); err != nil {
		return eh.Errorf("replace: %w", err)
	}
	got, err = st.LoadApplied(ctx)
	if err != nil || len(got) != 2 || got[0] != h(1) || got[1] != h(3) {
		return eh.Errorf("post-replace load = %v, %v", got, err)
	}
	if err = st.ReplaceApplied(ctx, nil); err != nil {
		return eh.Errorf("replace-to-empty: %w", err)
	}
	got, err = st.LoadApplied(ctx)
	if err != nil || len(got) != 0 {
		return eh.Errorf("post-empty load = %v, %v", got, err)
	}
	return
}

// CheckAppliedLogBatch: a batch append is observationally equal to the
// same appends one at a time — order kept, interleaving with single
// appends, empty batch a no-op.
func CheckAppliedLogBatch(ctx context.Context, open OpenFunc, location string) (err error) {
	st, err := open(location)
	if err != nil {
		return eh.Errorf("open: %w", err)
	}
	defer st.Close()
	if err = st.AppendApplied(ctx, h(1)); err != nil {
		return eh.Errorf("append: %w", err)
	}
	if err = st.AppendAppliedBatch(ctx, nil); err != nil {
		return eh.Errorf("empty batch: %w", err)
	}
	if err = st.AppendAppliedBatch(ctx, []t.PatchHash{h(2), h(3), h(4)}); err != nil {
		return eh.Errorf("batch: %w", err)
	}
	if err = st.AppendApplied(ctx, h(5)); err != nil {
		return eh.Errorf("append after batch: %w", err)
	}
	got, err := st.LoadApplied(ctx)
	if err != nil {
		return eh.Errorf("load: %w", err)
	}
	want := []t.PatchHash{h(1), h(2), h(3), h(4), h(5)}
	if len(got) != len(want) {
		return eh.Errorf("load = %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			return eh.Errorf("load order mismatch at %d: %v", i, got)
		}
	}
	return
}

// CheckSnapshot: absent on fresh, save/load round-trip, replace. A
// store that disclaims Snapshots must accept saves and still report
// none.
func CheckSnapshot(ctx context.Context, open OpenFunc, location string) (err error) {
	st, err := open(location)
	if err != nil {
		return eh.Errorf("open: %w", err)
	}
	defer st.Close()

	if _, ok, err2 := st.LoadSnapshot(ctx); err2 != nil || ok {
		return eh.Errorf("fresh snapshot = ok:%v err:%v (want absent)", ok, err2)
	}
	snapA := repo.Snapshot{Applied: []t.PatchHash{h(1), h(2)}, PushoutGraph: []byte("GRG1-bytes-A")}
	if err = st.SaveSnapshot(ctx, snapA); err != nil {
		return eh.Errorf("save: %w", err)
	}
	if !st.Capabilities().Snapshots {
		if _, ok, err2 := st.LoadSnapshot(ctx); err2 != nil || ok {
			return eh.Errorf("Snapshots disclaimed but a save was returned: ok:%v err:%v", ok, err2)
		}
		return
	}
	got, ok, err := st.LoadSnapshot(ctx)
	if err != nil || !ok {
		return eh.Errorf("load: ok:%v err:%v", ok, err)
	}
	if len(got.Applied) != 2 || got.Applied[0] != h(1) || got.Applied[1] != h(2) || !bytes.Equal(got.PushoutGraph, snapA.PushoutGraph) {
		return eh.Errorf("snapshot round-trip mismatch: %+v", got)
	}
	snapB := repo.Snapshot{Applied: nil, PushoutGraph: []byte("GRG1-bytes-B")}
	if err = st.SaveSnapshot(ctx, snapB); err != nil {
		return eh.Errorf("re-save: %w", err)
	}
	got, ok, err = st.LoadSnapshot(ctx)
	if err != nil || !ok || len(got.Applied) != 0 || !bytes.Equal(got.PushoutGraph, snapB.PushoutGraph) {
		return eh.Errorf("snapshot replace mismatch: %+v ok:%v err:%v", got, ok, err)
	}
	return
}

// CheckRetention: absent on fresh; deltas fold (upsert replaces a
// node's entry, removal drops it), the purged flag round-trips, a
// re-applied delta changes nothing, and an empty delta is a no-op. A
// store that disclaims RetentionLedger must accept updates and still
// report none.
func CheckRetention(ctx context.Context, open OpenFunc, location string) (err error) {
	st, err := open(location)
	if err != nil {
		return eh.Errorf("open: %w", err)
	}
	defer st.Close()

	got, err := st.LoadRetention(ctx)
	if err != nil || len(got) != 0 {
		return eh.Errorf("fresh load = %v, %v (want empty, nil)", got, err)
	}
	first := repo.RetentionDelta{Upsert: []repo.RetentionEntry{{Node: nid(1, 0), UnixNano: 100}, {Node: nid(2, 7), UnixNano: 200, Purged: true}}}
	if err = st.UpdateRetention(ctx, first); err != nil {
		return eh.Errorf("update: %w", err)
	}
	if !st.Capabilities().RetentionLedger {
		got, err = st.LoadRetention(ctx)
		if err != nil || len(got) != 0 {
			return eh.Errorf("RetentionLedger disclaimed but an update was returned: %v, %v", got, err)
		}
		return
	}
	got, err = st.LoadRetention(ctx)
	if err != nil {
		return eh.Errorf("load: %w", err)
	}
	if !retentionSetEqual(got, first.Upsert) {
		return eh.Errorf("round-trip mismatch: got %v want %v", got, first.Upsert)
	}
	// Idempotent: the same delta again is a no-op.
	if err = st.UpdateRetention(ctx, first); err != nil {
		return eh.Errorf("re-update: %w", err)
	}
	got, err = st.LoadRetention(ctx)
	if err != nil || !retentionSetEqual(got, first.Upsert) {
		return eh.Errorf("re-applied delta changed the ledger: %v, %v", got, err)
	}
	// Fold: node 1 re-stamped and purged, node 2 removed, node 3 added.
	second := repo.RetentionDelta{
		Upsert: []repo.RetentionEntry{{Node: nid(1, 0), UnixNano: 150, Purged: true}, {Node: nid(3, 0), UnixNano: 300}},
		Remove: []t.NodeID{nid(2, 7)},
	}
	if err = st.UpdateRetention(ctx, second); err != nil {
		return eh.Errorf("second update: %w", err)
	}
	want := []repo.RetentionEntry{{Node: nid(1, 0), UnixNano: 150, Purged: true}, {Node: nid(3, 0), UnixNano: 300}}
	got, err = st.LoadRetention(ctx)
	if err != nil || !retentionSetEqual(got, want) {
		return eh.Errorf("post-fold load = %v, %v (want %v)", got, err, want)
	}
	if err = st.UpdateRetention(ctx, repo.RetentionDelta{}); err != nil {
		return eh.Errorf("empty delta: %w", err)
	}
	got, err = st.LoadRetention(ctx)
	if err != nil || !retentionSetEqual(got, want) {
		return eh.Errorf("empty delta changed the ledger: %v, %v", got, err)
	}
	// Remove everything: an empty set, not an error.
	if err = st.UpdateRetention(ctx, repo.RetentionDelta{Remove: []t.NodeID{nid(1, 0), nid(3, 0)}}); err != nil {
		return eh.Errorf("remove all: %w", err)
	}
	got, err = st.LoadRetention(ctx)
	if err != nil || len(got) != 0 {
		return eh.Errorf("post-remove-all load = %v, %v", got, err)
	}
	return
}

// CheckEnvelopeBatch: PutEnvelopes and LoadEnvelopes agree with their
// one-by-one counterparts — batch put then single get, single put then
// batch load in request order, duplicates within a batch, first-write-
// wins across a batch, and a missing hash ending the load with
// ErrEnvelopeNotFound. Skipped for a store that disclaims
// ExactEnvelopeBytes, like CheckEnvelopes.
func CheckEnvelopeBatch(ctx context.Context, open OpenFunc, location string) (err error) {
	st, err := open(location)
	if err != nil {
		return eh.Errorf("open: %w", err)
	}
	defer st.Close()
	if !st.Capabilities().ExactEnvelopeBytes {
		return
	}
	frame := func(b byte) []byte { return []byte("PXE1\x05opaq1" + string([]byte{b})) }
	if err = st.PutEnvelope(ctx, h(1), frame(1)); err != nil {
		return eh.Errorf("put: %w", err)
	}
	if err = st.PutEnvelopes(ctx, nil); err != nil {
		return eh.Errorf("empty batch put: %w", err)
	}
	batch := []repo.Envelope{
		{Hash: h(1), Framed: []byte("OTHER")}, // first write wins across a batch
		{Hash: h(2), Framed: frame(2)},
		{Hash: h(3), Framed: frame(3)},
		{Hash: h(3), Framed: frame(3)}, // duplicate within a batch
	}
	if err = st.PutEnvelopes(ctx, batch); err != nil {
		return eh.Errorf("batch put: %w", err)
	}
	for _, c := range []struct {
		hash t.PatchHash
		want []byte
	}{{h(1), frame(1)}, {h(2), frame(2)}, {h(3), frame(3)}} {
		got, gerr := st.GetEnvelope(ctx, c.hash)
		if gerr != nil || !bytes.Equal(got, c.want) {
			return eh.Errorf("get after batch put: %q, %v (want %q)", got, gerr, c.want)
		}
	}
	// Batch load in request order (not hash order), with a repeat.
	req := []t.PatchHash{h(3), h(1), h(2), h(1)}
	i := 0
	for env, lerr := range st.LoadEnvelopes(ctx, req) {
		if lerr != nil {
			return eh.Errorf("batch load: %w", lerr)
		}
		if i >= len(req) {
			return eh.Errorf("batch load yielded more than requested")
		}
		if env.Hash != req[i] || !bytes.Equal(env.Framed, frame(req[i][0])) {
			return eh.Errorf("batch load position %d: got %v %q, want %v", i, env.Hash, env.Framed, req[i])
		}
		i++
	}
	if i != len(req) {
		return eh.Errorf("batch load yielded %d of %d", i, len(req))
	}
	// Empty request: an empty sequence.
	for range st.LoadEnvelopes(ctx, nil) {
		return eh.Errorf("batch load of nothing yielded something")
	}
	// A missing hash ends the sequence with ErrEnvelopeNotFound, after
	// the hashes before it.
	seen := 0
	var final error
	for env, lerr := range st.LoadEnvelopes(ctx, []t.PatchHash{h(2), h(9), h(3)}) {
		if lerr != nil {
			final = lerr
			break
		}
		if env.Hash != h(2) {
			return eh.Errorf("batch load with a miss: unexpected %v before the miss", env.Hash)
		}
		seen++
	}
	if !errors.Is(final, repo.ErrEnvelopeNotFound) || seen != 1 {
		return eh.Errorf("batch load with a miss: seen=%d err=%v (want 1, ErrEnvelopeNotFound)", seen, final)
	}
	// Early break is allowed.
	for range st.LoadEnvelopes(ctx, []t.PatchHash{h(1), h(2)}) {
		break
	}
	return
}

// CheckReopenDurability: everything written before Close is observable
// after reopening the same location — the property crash recovery
// stands on.
func CheckReopenDurability(ctx context.Context, open OpenFunc, location string) (err error) {
	st, err := open(location)
	if err != nil {
		return eh.Errorf("open #1: %w", err)
	}
	// The fixture is opaque bytes, which a re-encoding store cannot
	// accept; it is put only where byte-equality is claimed (SD5), and
	// the read below is gated the same way.
	data := []byte("ENVELOPE-BYTES")
	if st.Capabilities().ExactEnvelopeBytes {
		if err = st.PutEnvelope(ctx, h(7), data); err != nil {
			return err
		}
	}
	if err = st.AppendApplied(ctx, h(7)); err != nil {
		return err
	}
	if err = st.SaveSnapshot(ctx, repo.Snapshot{Applied: []t.PatchHash{h(7)}, PushoutGraph: []byte("G")}); err != nil {
		return err
	}
	if err = st.UpdateRetention(ctx, repo.RetentionDelta{Upsert: []repo.RetentionEntry{{Node: nid(7, 3), UnixNano: 999}}}); err != nil {
		return err
	}
	if err = st.Close(); err != nil {
		return eh.Errorf("close: %w", err)
	}

	st2, err := open(location)
	if err != nil {
		return eh.Errorf("open #2: %w", err)
	}
	defer st2.Close()
	caps := st2.Capabilities()
	if caps.ExactEnvelopeBytes {
		got, err := st2.GetEnvelope(ctx, h(7))
		if err != nil || !bytes.Equal(got, data) {
			return eh.Errorf("envelope did not survive reopen: %q, %v", got, err)
		}
	}
	applied, err := st2.LoadApplied(ctx)
	if err != nil || len(applied) != 1 || applied[0] != h(7) {
		return eh.Errorf("applied log did not survive reopen: %v, %v", applied, err)
	}
	snap, ok, err := st2.LoadSnapshot(ctx)
	if caps.Snapshots {
		if err != nil || !ok || len(snap.Applied) != 1 {
			return eh.Errorf("snapshot did not survive reopen: %+v ok:%v err:%v", snap, ok, err)
		}
	} else if err != nil || ok {
		return eh.Errorf("Snapshots disclaimed but a snapshot survived reopen: ok:%v err:%v", ok, err)
	}
	ret, err := st2.LoadRetention(ctx)
	if caps.RetentionLedger {
		if err != nil || len(ret) != 1 || ret[0].Node != nid(7, 3) || ret[0].UnixNano != 999 {
			return eh.Errorf("retention ledger did not survive reopen: %v, %v", ret, err)
		}
	} else if err != nil || len(ret) != 0 {
		return eh.Errorf("RetentionLedger disclaimed but a ledger survived reopen: %v, %v", ret, err)
	}
	return
}

// Run executes the full conformance suite. Each check gets a fresh
// location under t.TempDir().
func Run(tt *testing.T, open OpenFunc) {
	tt.Helper()
	ctx := context.Background()
	checks := []struct {
		name  string
		check func(context.Context, OpenFunc, string) error
	}{
		{"Envelopes", CheckEnvelopes},
		{"EnvelopeBatch", CheckEnvelopeBatch},
		{"AppliedLog", CheckAppliedLog},
		{"AppliedLogBatch", CheckAppliedLogBatch},
		{"Snapshot", CheckSnapshot},
		{"Retention", CheckRetention},
		{"ReopenDurability", CheckReopenDurability},
	}
	for _, c := range checks {
		tt.Run(c.name, func(tt *testing.T) {
			if err := c.check(ctx, open, tt.TempDir()); err != nil {
				tt.Fatal(err)
			}
		})
	}
}
