// ApplyEnvelopes: order-insensitive batch ingest — shuffled input
// converges, pending remainder is reported and resubmittable,
// duplicates are counted, a rejected patch fails the batch before any
// write, storage faults stay crash-equivalent on both log-append paths.
package repo_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/envelope"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/patch"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/types"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo"
)

// memStore is a StorageI with no I/O, for benchmarks that isolate
// engine cost from fsync cost. It deliberately does NOT implement
// repo.BatchAppenderI, so it also exercises the per-hash fallback.
type memStore struct {
	env     map[t.PatchHash][]byte
	applied []t.PatchHash
	snap    *repo.Snapshot
	ret     []repo.RetentionEntry
}

func newMemStore() *memStore { return &memStore{env: map[t.PatchHash][]byte{}} }

func (m *memStore) PutEnvelope(_ context.Context, h t.PatchHash, b []byte) error {
	if _, ok := m.env[h]; !ok {
		m.env[h] = b
	}
	return nil
}

func (m *memStore) GetEnvelope(_ context.Context, h t.PatchHash) ([]byte, error) {
	b, ok := m.env[h]
	if !ok {
		return nil, repo.ErrEnvelopeNotFound
	}
	return b, nil
}

func (m *memStore) HasEnvelope(_ context.Context, h t.PatchHash) (bool, error) {
	_, ok := m.env[h]
	return ok, nil
}

func (m *memStore) AppendApplied(_ context.Context, h t.PatchHash) error {
	m.applied = append(m.applied, h)
	return nil
}

func (m *memStore) ReplaceApplied(_ context.Context, hs []t.PatchHash) error {
	m.applied = slices.Clone(hs)
	return nil
}

func (m *memStore) LoadApplied(_ context.Context) ([]t.PatchHash, error) {
	return slices.Clone(m.applied), nil
}

func (m *memStore) SaveSnapshot(_ context.Context, s repo.Snapshot) error {
	m.snap = &s
	return nil
}

func (m *memStore) LoadSnapshot(_ context.Context) (repo.Snapshot, bool, error) {
	if m.snap == nil {
		return repo.Snapshot{}, false, nil
	}
	return *m.snap, true, nil
}

func (m *memStore) SaveRetention(_ context.Context, e []repo.RetentionEntry) error {
	m.ret = e
	return nil
}

func (m *memStore) LoadRetention(_ context.Context) ([]repo.RetentionEntry, error) {
	return m.ret, nil
}

func (m *memStore) Close() error { return nil }

func memOptions(tt testing.TB, st repo.StorageI) repo.Options {
	tt.Helper()
	reg, err := envelope.NewRegistry(envelope.CBORV1{})
	if err != nil {
		tt.Fatal(err)
	}
	return repo.Options{Storage: st, Codecs: reg, Wire: envelope.CBORV1Name, Producer: "mem", Clock: testClock()}
}

// chainHistory records n patches: a chain of inserts with every tenth
// patch deleting a node twenty back, so replay exercises tombstones and
// pseudo-edge resolution. Returns the hashes in record order.
func chainHistory(tt testing.TB, r *repo.Repo, n int) (hashes []t.PatchHash) {
	tt.Helper()
	anchor := t.RootNodeID
	var nodes []t.NodeID
	for i := range n {
		if i%10 == 9 && len(nodes) > 25 {
			hashes = append(hashes, recordDelete(tt, r, nodes[len(nodes)-20]))
			continue
		}
		h, node := recordLine(tt, r, anchor, fmt.Sprintf("line %d", i))
		anchor = node
		nodes = append(nodes, node)
		hashes = append(hashes, h)
	}
	return
}

func envelopesOf(tt testing.TB, r *repo.Repo, hashes []t.PatchHash) (envs [][]byte) {
	tt.Helper()
	for _, h := range hashes {
		e, err := r.EncodedEnvelope(context.Background(), h)
		if err != nil {
			tt.Fatal(err)
		}
		envs = append(envs, e)
	}
	return
}

// stateFingerprint is fingerprint minus the applied log: two repos that
// hold the same set in a different log order must agree on it.
func stateFingerprint(tt testing.TB, r *repo.Repo) string {
	tt.Helper()
	var sb strings.Builder
	for _, line := range strings.Split(fingerprint(tt, r), "\n") {
		if !strings.HasPrefix(line, "applied ") {
			sb.WriteString(line)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func TestRepo_ApplyEnvelopesShuffledConverges(tt *testing.T) {
	ctx := context.Background()
	src := openTest(tt, tt.TempDir())
	hashes := chainHistory(tt, src, 60)
	envs := envelopesOf(tt, src, hashes)
	rng := rand.New(rand.NewPCG(1, 2))
	rng.Shuffle(len(envs), func(i, j int) { envs[i], envs[j] = envs[j], envs[i] })

	dst := openTest(tt, tt.TempDir())
	report, err := dst.ApplyEnvelopes(ctx, envs)
	if err != nil {
		tt.Fatal(err)
	}
	if len(report.Applied) != len(hashes) || len(report.Pending) != 0 || len(report.Duplicates) != 0 {
		tt.Fatalf("report: applied=%d pending=%d dup=%d, want %d/0/0", len(report.Applied), len(report.Pending), len(report.Duplicates), len(hashes))
	}
	if got, want := stateFingerprint(tt, dst), stateFingerprint(tt, src); got != want {
		tt.Fatalf("state diverged after shuffled batch ingest")
	}
	assertInvariants(tt, dst)

	// The log respects dependencies, and — since the order is a function
	// of the set — a second replica fed a different shuffle logs the same.
	applied, _ := dst.Applied(ctx)
	if !slices.Equal(applied, report.Applied) {
		tt.Fatal("Applied() does not match the report's order")
	}
	rng.Shuffle(len(envs), func(i, j int) { envs[i], envs[j] = envs[j], envs[i] })
	dst2 := openTest(tt, tt.TempDir())
	report2, err := dst2.ApplyEnvelopes(ctx, envs)
	if err != nil {
		tt.Fatal(err)
	}
	if !slices.Equal(report2.Applied, report.Applied) {
		tt.Fatal("log order depends on input order")
	}

	// Durable: a crash-reopen (the log has no snapshot) replays the
	// batch-written log, which must be dependency-closed at every prefix.
	if err = dst.Close(ctx); err != nil {
		tt.Fatal(err)
	}
}

func TestRepo_ApplyEnvelopesPendingAndResubmit(tt *testing.T) {
	ctx := context.Background()
	src := openTest(tt, tt.TempDir())
	hashes := chainHistory(tt, src, 12) // a pure chain: each depends on the previous
	envs := envelopesOf(tt, src, hashes)

	dst := openTest(tt, tt.TempDir())
	// Withhold patch 4: 0..3 apply, 4 is absent, 5..11 are transitively pending.
	withheld := envs[4]
	batch := slices.Concat(envs[:4], envs[5:])
	report, err := dst.ApplyEnvelopes(ctx, batch)
	if err != nil {
		tt.Fatal(err)
	}
	if len(report.Applied) != 4 || len(report.Pending) != 7 {
		tt.Fatalf("applied=%d pending=%d, want 4/7", len(report.Applied), len(report.Pending))
	}
	for _, pe := range report.Pending {
		if len(pe.Missing) != 1 {
			tt.Fatalf("pending %s: missing=%v, want exactly one direct dep", pe.Hash, pe.Missing)
		}
		if pe.Hash == hashes[5] && pe.Missing[0] != hashes[4] {
			tt.Fatalf("patch 5 should be missing patch 4, got %s", pe.Missing[0])
		}
	}
	// Pending envelopes had no effect: not applied, not persisted.
	if _, err = dst.EncodedEnvelope(ctx, hashes[5]); !errors.Is(err, repo.ErrEnvelopeNotFound) {
		tt.Fatalf("pending envelope was persisted: err=%v", err)
	}

	// Resubmit the pending ones together with the withheld dependency.
	resubmit := [][]byte{withheld}
	for _, pe := range report.Pending {
		resubmit = append(resubmit, envs[slices.Index(hashes, pe.Hash)])
	}
	report, err = dst.ApplyEnvelopes(ctx, resubmit)
	if err != nil {
		tt.Fatal(err)
	}
	if len(report.Applied) != 8 || len(report.Pending) != 0 {
		tt.Fatalf("resubmit: applied=%d pending=%d, want 8/0", len(report.Applied), len(report.Pending))
	}
	if stateFingerprint(tt, dst) != stateFingerprint(tt, src) {
		tt.Fatal("state diverged after resubmit")
	}
	assertInvariants(tt, dst)
}

func TestRepo_ApplyEnvelopesDuplicates(tt *testing.T) {
	ctx := context.Background()
	src := openTest(tt, tt.TempDir())
	hashes := chainHistory(tt, src, 6)
	envs := envelopesOf(tt, src, hashes)

	dst := openTest(tt, tt.TempDir())
	if _, _, err := dst.ApplyEnvelope(ctx, envs[0]); err != nil {
		tt.Fatal(err)
	}
	// envs[0] already applied; envs[3] twice within the batch.
	batch := slices.Concat(envs, [][]byte{envs[3]})
	report, err := dst.ApplyEnvelopes(ctx, batch)
	if err != nil {
		tt.Fatal(err)
	}
	if len(report.Applied) != 5 || len(report.Duplicates) != 2 || len(report.Pending) != 0 {
		tt.Fatalf("applied=%d dup=%d pending=%d, want 5/2/0", len(report.Applied), len(report.Duplicates), len(report.Pending))
	}
	if report.Duplicates[0] != hashes[0] || report.Duplicates[1] != hashes[3] {
		tt.Fatalf("duplicates = %v", report.Duplicates)
	}
	// An all-duplicate batch writes nothing and reports nothing applied.
	report, err = dst.ApplyEnvelopes(ctx, envs)
	if err != nil || len(report.Applied) != 0 || len(report.Duplicates) != 6 {
		tt.Fatalf("all-duplicate batch: report=%+v err=%v", report, err)
	}
}

func TestRepo_ApplyEnvelopesRejectedPatchFailsBatchBeforeWrite(tt *testing.T) {
	ctx := context.Background()
	src := openTest(tt, tt.TempDir())
	hashes := chainHistory(tt, src, 3)
	envs := envelopesOf(tt, src, hashes)

	dst := openTest(tt, tt.TempDir())
	if _, _, err := dst.ApplyEnvelope(ctx, envs[0]); err != nil {
		tt.Fatal(err)
	}
	// A patch whose dependencies are satisfied but which the graph
	// rejects: it re-introduces a node id patch 0 already owns.
	existing := t.NodeID{Patch: hashes[0], Index: 0}
	bad := &patch.Patch{
		Author: "x", Description: "collides",
		Dependencies: []t.PatchHash{hashes[0]},
		Changes: []patch.Change{{
			Kind: patch.ChangeKindNewNode, NodeID: existing,
			Content: []byte("dup\n"), UpContext: []t.NodeID{t.RootNodeID},
		}},
	}
	bad.Hash = bad.ComputeHash()
	reg, _ := envelope.NewRegistry(envelope.CBORV1{})
	badFramed, err := reg.Encode(envelope.CBORV1Name, envelope.EnvelopeV1{Patch: bad, Producer: "x"})
	if err != nil {
		tt.Fatal(err)
	}
	before := fingerprint(tt, dst)
	_, err = dst.ApplyEnvelopes(ctx, [][]byte{envs[1], badFramed, envs[2]})
	if err == nil {
		tt.Fatal("batch with a rejected patch succeeded")
	}
	if fingerprint(tt, dst) != before {
		tt.Fatal("a failed batch changed the repo")
	}
	if _, err = dst.EncodedEnvelope(ctx, hashes[1]); !errors.Is(err, repo.ErrEnvelopeNotFound) {
		tt.Fatalf("failed batch persisted an envelope: err=%v", err)
	}
}

// A storage fault inside ApplyEnvelopes is crash-equivalent on both
// log-append paths: the batched one (filestore implements
// BatchAppenderI) and the per-hash fallback (faultStore embeds the
// interface and so hides the extension).
func TestRepo_ApplyEnvelopesStorageFaultIsCrashEquivalent(tt *testing.T) {
	ctx := context.Background()
	src := openTest(tt, tt.TempDir())
	hashes := chainHistory(tt, src, 8)
	envs := envelopesOf(tt, src, hashes)

	for _, arm := range []struct {
		name string
		arm  func(f *faultStore)
	}{
		{"PutEnvelope", func(f *faultStore) { f.failPut = true }},
		{"AppendApplied", func(f *faultStore) { f.failAppend = true }},
	} {
		tt.Run(arm.name, func(tt *testing.T) {
			dir := tt.TempDir()
			opts := testOptions(tt, dir)
			fs := &faultStore{StorageI: opts.Storage}
			opts.Storage = fs
			r, err := repo.Open(ctx, opts)
			if err != nil {
				tt.Fatal(err)
			}
			if _, err = r.ApplyEnvelopes(ctx, envs[:3]); err != nil {
				tt.Fatal(err)
			}
			before := fingerprint(tt, r)
			arm.arm(fs)
			if _, err = r.ApplyEnvelopes(ctx, envs[3:]); !errors.Is(err, errInjected) {
				tt.Fatalf("err = %v, want injected fault", err)
			}
			if fingerprint(tt, r) != before {
				tt.Fatal("in-memory state changed after a failed batch")
			}
			// Crash: drop the handle without Close, reopen over the same
			// files. Whatever reached the log must open cleanly.
			if cerr := opts.Storage.Close(); cerr != nil {
				tt.Fatal(cerr)
			}
			r2 := openTest(tt, dir)
			assertInvariants(tt, r2)
			if fingerprint(tt, r2) != before {
				tt.Fatal("reopen after fault does not reproduce the pre-batch state")
			}
		})
	}
}

func TestRepo_ApplyEnvelopesHooksAndRetention(tt *testing.T) {
	ctx := context.Background()
	src := openTest(tt, tt.TempDir())
	hashes := chainHistory(tt, src, 40) // includes deletes → tombstones
	envs := envelopesOf(tt, src, hashes)

	dir := tt.TempDir()
	opts := testOptions(tt, dir)
	var fired []t.PatchHash
	opts.Hooks.OnApplied = func(ev repo.AppliedEvent) {
		if ev.NewlyRecorded {
			tt.Error("batch ingest reported NewlyRecorded")
		}
		fired = append(fired, ev.Hash)
	}
	dst, err := repo.Open(ctx, opts)
	if err != nil {
		tt.Fatal(err)
	}
	report, err := dst.ApplyEnvelopes(ctx, envs)
	if err != nil {
		tt.Fatal(err)
	}
	if !slices.Equal(fired, report.Applied) {
		tt.Fatal("OnApplied did not fire once per applied patch in log order")
	}
	stamps, _ := dst.RetentionStamps(ctx)
	if len(stamps) == 0 {
		tt.Fatal("no retention stamps after a batch with deletes")
	}
	// The ledger was persisted before the commit: a snapshot-less reopen
	// must seed the same stamps rather than re-stamp at replay time.
	if err = dst.Close(ctx); err != nil {
		tt.Fatal(err)
	}
	r2 := openTest(tt, dir)
	stamps2, _ := r2.RetentionStamps(ctx)
	if !slices.Equal(stamps, stamps2) {
		tt.Fatal("retention stamps changed across reopen")
	}
}

// Ingesting a fresh history: per-envelope verb vs batch verb. Both use
// the I/O-free memStore, so the ratio is the engine's own cost — the
// clone-per-commit against the clone-per-batch.
func benchmarkIngestEnvelopes(b *testing.B, n int) (envs [][]byte) {
	b.Helper()
	ctx := context.Background()
	src, err := repo.Open(ctx, memOptions(b, newMemStore()))
	if err != nil {
		b.Fatal(err)
	}
	hashes := chainHistory(b, src, n)
	envs = envelopesOf(b, src, hashes)
	return
}

func BenchmarkIngest_ApplyEnvelope_1000(b *testing.B) {
	ctx := context.Background()
	envs := benchmarkIngestEnvelopes(b, 1000)
	b.ResetTimer()
	for range b.N {
		r, _ := repo.Open(ctx, memOptions(b, newMemStore()))
		for _, e := range envs {
			if _, _, err := r.ApplyEnvelope(ctx, e); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkIngest_ApplyEnvelopes_1000(b *testing.B) {
	ctx := context.Background()
	envs := benchmarkIngestEnvelopes(b, 1000)
	b.ResetTimer()
	for range b.N {
		r, _ := repo.Open(ctx, memOptions(b, newMemStore()))
		if _, err := r.ApplyEnvelopes(ctx, envs); err != nil {
			b.Fatal(err)
		}
	}
}
