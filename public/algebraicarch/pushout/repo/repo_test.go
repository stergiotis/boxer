// Engine tests: open/recover round-trips, snapshot-prefix rule,
// crash-equivalent fault injection at the storage seam, retention
// durability across reopen (the compliance property), envelope exchange
// semantics, and the error taxonomy.
package repo_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/envelope"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/patch"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/qc"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo/filestore"
)

func testClock() func() time.Time {
	tick := time.Unix(1_700_000_000, 0).UTC()
	return func() time.Time {
		tick = tick.Add(time.Minute)
		return tick
	}
}

func testOptions(tt testing.TB, dir string) repo.Options {
	tt.Helper()
	st, err := filestore.Open(dir)
	if err != nil {
		tt.Fatal(err)
	}
	reg, err := envelope.NewRegistry(envelope.JSONV1{})
	if err != nil {
		tt.Fatal(err)
	}
	return repo.Options{
		Storage:  st,
		Codecs:   reg,
		Wire:     envelope.JSONV1Name,
		Producer: "tester",
		Clock:    testClock(),
	}
}

func openTest(tt testing.TB, dir string) *repo.Repo {
	tt.Helper()
	r, err := repo.Open(context.Background(), testOptions(tt, dir))
	if err != nil {
		tt.Fatal(err)
	}
	return r
}

// openTestStore is openTest plus the storage handle, so crash-simulation
// tests can release the store's inter-process lock the way a real process
// exit would. A faithful crash drops the filestore lock (the OS frees it
// on exit); a mere in-memory leak does not, which would falsely collide
// with the recovery Open.
func openTestStore(tt testing.TB, dir string) (*repo.Repo, repo.StorageI) {
	tt.Helper()
	opts := testOptions(tt, dir)
	r, err := repo.Open(context.Background(), opts)
	if err != nil {
		tt.Fatal(err)
	}
	return r, opts.Storage
}

// recordLine records one new node anchored at the given context (root
// for the chain head), returning the patch hash and the node id.
func recordLine(tt testing.TB, r *repo.Repo, anchor t.NodeID, line string) (h t.PatchHash, node t.NodeID) {
	tt.Helper()
	changes := []patch.Change{{
		Kind:      patch.ChangeKindNewNode,
		NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: 0},
		Content:   []byte(line + "\n"),
		UpContext: []t.NodeID{anchor},
	}}
	h, err := r.Record(context.Background(), "tester", "add "+line, changes)
	if err != nil {
		tt.Fatal(err)
	}
	node = t.NodeID{Patch: h, Index: 0}
	return
}

func recordDelete(tt testing.TB, r *repo.Repo, node t.NodeID) (h t.PatchHash) {
	tt.Helper()
	h, err := r.Record(context.Background(), "tester", "delete", []patch.Change{{
		Kind: patch.ChangeKindDeleteNode, NodeID: node,
	}})
	if err != nil {
		tt.Fatal(err)
	}
	return
}

// fingerprint captures the observable repo state for equality checks.
func fingerprint(tt testing.TB, r *repo.Repo) string {
	tt.Helper()
	var sb strings.Builder
	err := r.View(context.Background(), func(v repo.ViewI) error {
		for _, h := range v.Applied() {
			fmt.Fprintf(&sb, "applied %s\n", h)
		}
		g := v.Graph()
		for id := range g.AllLiveNodes() {
			fmt.Fprintf(&sb, "live %s %q\n", id, g.NodeContent(id))
		}
		for id := range v.Visualizable().AllDeletedNodes() {
			fmt.Fprintf(&sb, "dead %s status=%d\n", id, g.NodeContentStatus(id))
		}
		return nil
	})
	if err != nil {
		tt.Fatal(err)
	}
	return sb.String()
}

func assertInvariants(tt testing.TB, r *repo.Repo) {
	tt.Helper()
	err := r.View(context.Background(), func(v repo.ViewI) error {
		for _, e := range qc.CheckInvariants(v.Graph().(t.InspectableI)) {
			tt.Errorf("invariant: %v", e)
		}
		return nil
	})
	if err != nil {
		tt.Fatal(err)
	}
}

func TestRepo_RecordCloseReopen(tt *testing.T) {
	ctx := context.Background()
	dir := tt.TempDir()
	r := openTest(tt, dir)
	hA, nA := recordLine(tt, r, t.RootNodeID, "alpha")
	_, nB := recordLine(tt, r, nA, "beta")
	recordDelete(tt, r, nB)
	want := fingerprint(tt, r)
	assertInvariants(tt, r)
	if err := r.Close(ctx); err != nil {
		tt.Fatal(err)
	}
	if _, err := r.Applied(ctx); !errors.Is(err, repo.ErrClosed) {
		tt.Fatalf("expected ErrClosed after Close, got %v", err)
	}

	var recovered repo.RecoveredEvent
	opts := testOptions(tt, dir)
	opts.Hooks.OnRecovered = func(ev repo.RecoveredEvent) { recovered = ev }
	r2, err := repo.Open(ctx, opts)
	if err != nil {
		tt.Fatal(err)
	}
	defer r2.Close(ctx)
	if got := fingerprint(tt, r2); got != want {
		tt.Fatalf("state diverged across close/reopen:\n got:\n%s\nwant:\n%s", got, want)
	}
	assertInvariants(tt, r2)
	if !recovered.FromSnapshot || recovered.Replayed != 0 || recovered.Applied != 3 {
		tt.Fatalf("expected snapshot-only recovery, got %+v", recovered)
	}
	// PatchInfo for snapshot-covered history loads lazily from storage.
	info, err := r2.PatchInfo(ctx, hA)
	if err != nil || info.Patch.Hash != hA || info.Codec != envelope.JSONV1Name {
		tt.Fatalf("lazy PatchInfo: %+v, %v", info, err)
	}
}

func TestRepo_FullReplayWithoutSnapshot(tt *testing.T) {
	ctx := context.Background()
	dir := tt.TempDir()
	r, st := openTestStore(tt, dir)
	_, nA := recordLine(tt, r, t.RootNodeID, "alpha")
	recordLine(tt, r, nA, "beta")
	want := fingerprint(tt, r)
	// Crash (no Close): the snapshot only exists if a verb wrote one;
	// Record does not. Recovery must replay the full log. Releasing the
	// store models process exit freeing the inter-process lock.
	_ = st.Close()
	var recovered repo.RecoveredEvent
	opts := testOptions(tt, dir)
	opts.Hooks.OnRecovered = func(ev repo.RecoveredEvent) { recovered = ev }
	r2, err := repo.Open(ctx, opts)
	if err != nil {
		tt.Fatal(err)
	}
	defer r2.Close(ctx)
	if got := fingerprint(tt, r2); got != want {
		tt.Fatalf("full replay diverged:\n got:\n%s\nwant:\n%s", got, want)
	}
	if recovered.FromSnapshot || recovered.Replayed != 2 {
		tt.Fatalf("expected full replay of 2, got %+v", recovered)
	}
}

func TestRepo_NonPrefixSnapshotDiscarded(tt *testing.T) {
	ctx := context.Background()
	dir := tt.TempDir()
	r := openTest(tt, dir)
	_, nA := recordLine(tt, r, t.RootNodeID, "alpha")
	recordLine(tt, r, nA, "beta")
	want := fingerprint(tt, r)
	if err := r.Close(ctx); err != nil {
		tt.Fatal(err)
	}
	// Corrupt the snapshot's applied list into a non-prefix.
	st, err := filestore.Open(dir)
	if err != nil {
		tt.Fatal(err)
	}
	bogus := repo.Snapshot{Applied: []t.PatchHash{{0xde, 0xad}}, Graggle: []byte("GRG1garbage")}
	if err := st.SaveSnapshot(ctx, bogus); err != nil {
		tt.Fatal(err)
	}
	_ = st.Close()

	var recovered repo.RecoveredEvent
	opts := testOptions(tt, dir)
	opts.Hooks.OnRecovered = func(ev repo.RecoveredEvent) { recovered = ev }
	r2, err := repo.Open(ctx, opts)
	if err != nil {
		tt.Fatal(err)
	}
	defer r2.Close(ctx)
	if recovered.FromSnapshot {
		tt.Fatal("non-prefix snapshot must be discarded")
	}
	if got := fingerprint(tt, r2); got != want {
		tt.Fatalf("recovery after snapshot discard diverged:\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRepo_CorruptStoreRefusesOpen(tt *testing.T) {
	ctx := context.Background()
	dir := tt.TempDir()
	r, st0 := openTestStore(tt, dir)
	hA, nA := recordLine(tt, r, t.RootNodeID, "alpha")
	recordLine(tt, r, nA, "beta")
	// Crash without snapshot (release the lock as process exit would),
	// then destroy an applied envelope.
	_ = st0.Close()
	st, err := filestore.Open(dir)
	if err != nil {
		tt.Fatal(err)
	}
	if _, err := st.GetEnvelope(ctx, hA); err != nil {
		tt.Fatal(err)
	}
	_ = st.Close()
	hexHash := fmt.Sprintf("%x", hA[:])
	if err := removeEnvelopeFile(dir, hexHash); err != nil {
		tt.Fatal(err)
	}
	_, err = repo.Open(ctx, testOptions(tt, dir))
	if !errors.Is(err, repo.ErrCorruptStore) {
		tt.Fatalf("expected ErrCorruptStore, got %v", err)
	}
	_ = r // crashed; never cleanly closed
}

// faultStore wraps a StorageI and fails selected operations once armed.
type faultStore struct {
	repo.StorageI
	failPut       bool
	failAppend    bool
	failReplace   bool
	failSnap      bool
	failRetention bool
}

var errInjected = errors.New("injected storage fault")

func (f *faultStore) PutEnvelope(ctx context.Context, h t.PatchHash, framed []byte) error {
	if f.failPut {
		return errInjected
	}
	return f.StorageI.PutEnvelope(ctx, h, framed)
}

func (f *faultStore) AppendApplied(ctx context.Context, h t.PatchHash) error {
	if f.failAppend {
		return errInjected
	}
	return f.StorageI.AppendApplied(ctx, h)
}

func (f *faultStore) ReplaceApplied(ctx context.Context, hs []t.PatchHash) error {
	if f.failReplace {
		return errInjected
	}
	return f.StorageI.ReplaceApplied(ctx, hs)
}

func (f *faultStore) SaveSnapshot(ctx context.Context, snap repo.Snapshot) error {
	if f.failSnap {
		return errInjected
	}
	return f.StorageI.SaveSnapshot(ctx, snap)
}

func (f *faultStore) SaveRetention(ctx context.Context, entries []repo.RetentionEntry) error {
	if f.failRetention {
		return errInjected
	}
	return f.StorageI.SaveRetention(ctx, entries)
}

// Every mutating verb, failed at every storage write it performs, must
// (a) surface the error, (b) leave the in-memory state unchanged, and
// (c) leave the DISK in a state from which a crash-reopen reproduces
// exactly the pre-verb state.
func TestRepo_StorageFaultsAreCrashEquivalent(tt *testing.T) {
	ctx := context.Background()
	type step struct {
		name string
		arm  func(f *faultStore)
		act  func(tt *testing.T, r *repo.Repo, hDel t.PatchHash, nB t.NodeID) error
	}
	steps := []step{
		{"Record/PutEnvelope", func(f *faultStore) { f.failPut = true }, func(tt *testing.T, r *repo.Repo, _ t.PatchHash, _ t.NodeID) error {
			_, err := r.Record(ctx, "x", "blocked", []patch.Change{{
				Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0},
				Content: []byte("blocked\n"), UpContext: []t.NodeID{t.RootNodeID},
			}})
			return err
		}},
		{"Record/AppendApplied", func(f *faultStore) { f.failAppend = true }, func(tt *testing.T, r *repo.Repo, _ t.PatchHash, _ t.NodeID) error {
			_, err := r.Record(ctx, "x", "blocked", []patch.Change{{
				Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0},
				Content: []byte("blocked\n"), UpContext: []t.NodeID{t.RootNodeID},
			}})
			return err
		}},
		{"Unrecord/SaveSnapshot", func(f *faultStore) { f.failSnap = true }, func(tt *testing.T, r *repo.Repo, hDel t.PatchHash, _ t.NodeID) error {
			return r.Unrecord(ctx, hDel)
		}},
		{"Unrecord/ReplaceApplied", func(f *faultStore) { f.failReplace = true }, func(tt *testing.T, r *repo.Repo, hDel t.PatchHash, _ t.NodeID) error {
			return r.Unrecord(ctx, hDel)
		}},
		{"Sweep/SaveSnapshot", func(f *faultStore) { f.failSnap = true }, func(tt *testing.T, r *repo.Repo, _ t.PatchHash, _ t.NodeID) error {
			_, err := r.Sweep(ctx, time.Unix(2_000_000_000, 0).UTC(), 0)
			return err
		}},
	}
	for _, s := range steps {
		tt.Run(s.name, func(tt *testing.T) {
			dir := tt.TempDir()
			opts := testOptions(tt, dir)
			fault := &faultStore{StorageI: opts.Storage}
			opts.Storage = fault
			r, err := repo.Open(ctx, opts)
			if err != nil {
				tt.Fatal(err)
			}
			_, nA := recordLine(tt, r, t.RootNodeID, "alpha")
			_, nB := recordLine(tt, r, nA, "beta")
			hDel := recordDelete(tt, r, nB)
			want := fingerprint(tt, r)

			s.arm(fault)
			if err := s.act(tt, r, hDel, nB); !errors.Is(err, errInjected) {
				tt.Fatalf("expected injected fault to surface, got %v", err)
			}
			if got := fingerprint(tt, r); got != want {
				tt.Fatalf("in-memory state changed across failed verb:\n got:\n%s\nwant:\n%s", got, want)
			}
			assertInvariants(tt, r)

			// Crash-reopen from the underlying storage: pre-verb state.
			// Release the lock as process exit would.
			_ = fault.Close()
			r2, err := repo.Open(ctx, testOptions(tt, dir))
			if err != nil {
				tt.Fatal(err)
			}
			defer r2.Close(ctx)
			if got := fingerprint(tt, r2); got != want {
				tt.Fatalf("disk state diverged across failed verb:\n got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

// The compliance property: a sweep's purge markers survive a CRASH (no
// Close) because Sweep snapshots before acking.
func TestRepo_SweepDurableAcrossCrash(tt *testing.T) {
	ctx := context.Background()
	dir := tt.TempDir()
	r, st := openTestStore(tt, dir)
	_, nA := recordLine(tt, r, t.RootNodeID, "alpha")
	hDel := recordDelete(tt, r, nA)
	report, err := r.Sweep(ctx, time.Unix(2_000_000_000, 0).UTC(), 0)
	if err != nil {
		tt.Fatal(err)
	}
	if len(report.Purged) == 0 {
		tt.Fatal("setup: sweep purged nothing")
	}
	// Crash (release the lock as process exit would). Reopen. The
	// tombstone's content must still be purged: unrecording its deleting
	// patch is permanently blocked.
	_ = st.Close()
	r2, err := repo.Open(ctx, testOptions(tt, dir))
	if err != nil {
		tt.Fatal(err)
	}
	defer r2.Close(ctx)
	err = r2.Unrecord(ctx, hDel)
	if !errors.Is(err, repo.ErrRetentionBlocked) {
		tt.Fatalf("expected ErrRetentionBlocked after crash-reopen, got %v", err)
	}
	if !errors.Is(err, patch.ErrRetentionPermanent) {
		tt.Fatalf("expected the patch-level sentinel to remain matchable, got %v", err)
	}
	assertInvariants(tt, r2)
}

// The retention horizon survives full replay — the bug ADR-0079's ledger
// closes. A tombstone stamped at T must still be swept at T+horizon after
// a crash that forces full replay (no snapshot), instead of having its
// horizon reset to reopen time.
func TestRepo_RetentionHorizonSurvivesFullReplay(tt *testing.T) {
	ctx := context.Background()
	dir := tt.TempDir()
	stampedAt := time.Unix(1_700_000_000, 0).UTC()

	opts := testOptions(tt, dir)
	opts.Clock = func() time.Time { return stampedAt }
	st := opts.Storage
	r, err := repo.Open(ctx, opts)
	if err != nil {
		tt.Fatal(err)
	}
	_, nA := recordLine(tt, r, t.RootNodeID, "alpha")
	recordDelete(tt, r, nA)
	// Crash without a snapshot (Record writes none): the next Open
	// full-replays, which re-stamps tombstoneAt to the replay clock.
	_ = st.Close()

	reopenAt := stampedAt.Add(48 * time.Hour)
	opts2 := testOptions(tt, dir)
	opts2.Clock = func() time.Time { return reopenAt }
	var rec repo.RecoveredEvent
	opts2.Hooks.OnRecovered = func(ev repo.RecoveredEvent) { rec = ev }
	r2, err := repo.Open(ctx, opts2)
	if err != nil {
		tt.Fatal(err)
	}
	defer r2.Close(ctx)
	if rec.FromSnapshot {
		tt.Fatalf("expected full replay (the bug path), got %+v", rec)
	}
	// Sweep with a 24h horizon at reopen time. The tombstone was stamped
	// at T (= reopenAt-48h), well past the cutoff, so it must be purged.
	// Without the ledger, replay would have re-stamped it to reopenAt and
	// the sweep would purge nothing.
	report, err := r2.Sweep(ctx, reopenAt, 24*time.Hour)
	if err != nil {
		tt.Fatal(err)
	}
	if len(report.Purged) == 0 {
		tt.Fatal("retention horizon was reset by full replay: tombstone not swept")
	}
}

// A retention-ledger write failure on a delete-bearing Record fails the
// verb before the commit point (AppendApplied), so nothing commits and a
// crash-reopen shows pre-verb state.
func TestRepo_RetentionWriteFaultIsCrashEquivalent(tt *testing.T) {
	ctx := context.Background()
	dir := tt.TempDir()
	opts := testOptions(tt, dir)
	fault := &faultStore{StorageI: opts.Storage}
	opts.Storage = fault
	r, err := repo.Open(ctx, opts)
	if err != nil {
		tt.Fatal(err)
	}
	_, nA := recordLine(tt, r, t.RootNodeID, "alpha")
	want := fingerprint(tt, r)

	fault.failRetention = true
	_, derr := r.Record(ctx, "tester", "del", []patch.Change{{Kind: patch.ChangeKindDeleteNode, NodeID: nA}})
	if !errors.Is(derr, errInjected) {
		tt.Fatalf("expected injected retention fault, got %v", derr)
	}
	if got := fingerprint(tt, r); got != want {
		tt.Fatalf("in-memory state changed across failed retention write:\n got:\n%s\nwant:\n%s", got, want)
	}
	assertInvariants(tt, r)

	// Crash-reopen: the delete never committed.
	_ = fault.Close()
	r2, err := repo.Open(ctx, testOptions(tt, dir))
	if err != nil {
		tt.Fatal(err)
	}
	defer r2.Close(ctx)
	if got := fingerprint(tt, r2); got != want {
		tt.Fatalf("disk state diverged across failed retention write:\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRepo_ExchangeSemantics(tt *testing.T) {
	ctx := context.Background()
	a := openTest(tt, tt.TempDir())
	defer a.Close(ctx)
	b := openTest(tt, tt.TempDir())
	defer b.Close(ctx)

	hA, nA := recordLine(tt, a, t.RootNodeID, "alpha")
	hB, _ := recordLine(tt, a, nA, "beta")

	envA, err := a.EncodedEnvelope(ctx, hA)
	if err != nil {
		tt.Fatal(err)
	}
	envB, err := a.EncodedEnvelope(ctx, hB)
	if err != nil {
		tt.Fatal(err)
	}

	// Dependency gate: beta needs alpha applied first.
	if _, _, err := b.ApplyEnvelope(ctx, envB); !errors.Is(err, repo.ErrMissingDependency) {
		tt.Fatalf("expected ErrMissingDependency, got %v", err)
	}
	if h, applied, err := b.ApplyEnvelope(ctx, envA); err != nil || !applied || h != hA {
		tt.Fatalf("apply alpha: %v %v %v", h, applied, err)
	}
	if h, applied, err := b.ApplyEnvelope(ctx, envB); err != nil || !applied || h != hB {
		tt.Fatalf("apply beta: %v %v %v", h, applied, err)
	}
	// Idempotence.
	if _, applied, err := b.ApplyEnvelope(ctx, envA); err != nil || applied {
		tt.Fatalf("duplicate apply: applied=%v err=%v", applied, err)
	}
	if fingerprint(tt, a) != fingerprint(tt, b) {
		tt.Fatal("repos diverged after full exchange")
	}

	// Unrecord keeps the envelope: re-apply works from local storage.
	if err := b.Unrecord(ctx, hB); err != nil {
		tt.Fatal(err)
	}
	stored, err := b.EncodedEnvelope(ctx, hB)
	if err != nil {
		tt.Fatal(err)
	}
	if h, applied, err := b.ApplyEnvelope(ctx, stored); err != nil || !applied || h != hB {
		tt.Fatalf("re-apply after unrecord: %v %v %v", h, applied, err)
	}
	if fingerprint(tt, a) != fingerprint(tt, b) {
		tt.Fatal("repos diverged after unrecord round-trip")
	}
}

func TestRepo_ErrorTaxonomy(tt *testing.T) {
	ctx := context.Background()
	r := openTest(tt, tt.TempDir())
	defer r.Close(ctx)

	if _, err := r.Record(ctx, "x", "empty", nil); !errors.Is(err, repo.ErrNoChanges) {
		tt.Fatalf("ErrNoChanges: %v", err)
	}
	hA, nA := recordLine(tt, r, t.RootNodeID, "alpha")
	hB, _ := recordLine(tt, r, nA, "beta") // depends on alpha
	if err := r.Unrecord(ctx, hA); !errors.Is(err, repo.ErrDependentExists) {
		tt.Fatalf("ErrDependentExists: %v", err)
	}
	if err := r.Unrecord(ctx, t.PatchHash{0x99}); !errors.Is(err, repo.ErrNotApplied) {
		tt.Fatalf("ErrNotApplied: %v", err)
	}
	if err := r.Unrecord(ctx, hB); err != nil {
		tt.Fatal(err)
	}
	if err := r.Unrecord(ctx, hA); err != nil {
		tt.Fatal(err)
	}
	applied, err := r.Applied(ctx)
	if err != nil || len(applied) != 0 {
		tt.Fatalf("applied after full unwind: %v %v", applied, err)
	}
	assertInvariants(tt, r)
}

// Re-creating identical content after deletion collides with the applied
// patch's identity; Record must disambiguate deterministically.
func TestRepo_IdentityCollisionShift(tt *testing.T) {
	ctx := context.Background()
	r := openTest(tt, tt.TempDir())
	defer r.Close(ctx)

	mk := func() []patch.Change {
		return []patch.Change{{
			Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0},
			Content: []byte("same\n"), UpContext: []t.NodeID{t.RootNodeID},
		}}
	}
	h1, err := r.Record(ctx, "x", "first", mk())
	if err != nil {
		tt.Fatal(err)
	}
	recordDelete(tt, r, t.NodeID{Patch: h1, Index: 0})
	h2, err := r.Record(ctx, "x", "again", mk())
	if err != nil {
		tt.Fatalf("identity collision must be disambiguated: %v", err)
	}
	if h2 == h1 {
		tt.Fatal("expected a fresh identity for the re-creation")
	}
	assertInvariants(tt, r)

	// Determinism: an independent repo performing the same sequence
	// converges on the same shifted identity.
	r2 := openTest(tt, tt.TempDir())
	defer r2.Close(ctx)
	g1, err := r2.Record(ctx, "y", "first", mk())
	if err != nil {
		tt.Fatal(err)
	}
	recordDelete(tt, r2, t.NodeID{Patch: g1, Index: 0})
	g2, err := r2.Record(ctx, "y", "again", mk())
	if err != nil {
		tt.Fatal(err)
	}
	if g1 != h1 || g2 != h2 {
		tt.Fatalf("identity drift across repos: (%s,%s) vs (%s,%s)", h1, h2, g1, g2)
	}
}

func TestRepo_HooksFire(tt *testing.T) {
	ctx := context.Background()
	dir := tt.TempDir()
	var appliedEvents, unrecordEvents, sweptEvents int
	opts := testOptions(tt, dir)
	opts.Hooks = repo.Hooks{
		OnApplied:  func(ev repo.AppliedEvent) { appliedEvents++ },
		OnUnrecord: func(ev repo.UnrecordEvent) { unrecordEvents++ },
		OnSwept:    func(ev repo.SweptEvent) { sweptEvents++ },
	}
	r, err := repo.Open(ctx, opts)
	if err != nil {
		tt.Fatal(err)
	}
	defer r.Close(ctx)
	_, nA := recordLine(tt, r, t.RootNodeID, "alpha")
	hDel := recordDelete(tt, r, nA)
	if _, err := r.Sweep(ctx, time.Unix(2_000_000_000, 0).UTC(), 0); err != nil {
		tt.Fatal(err)
	}
	_ = hDel
	if appliedEvents != 2 || sweptEvents != 1 || unrecordEvents != 0 {
		tt.Fatalf("hook counts: applied=%d swept=%d unrecord=%d", appliedEvents, sweptEvents, unrecordEvents)
	}
}

// removeEnvelopeFile reaches into the filestore layout (test-only).
func removeEnvelopeFile(dir, hexHash string) error {
	return os.Remove(filepath.Join(dir, "changes", hexHash[:2], hexHash))
}
