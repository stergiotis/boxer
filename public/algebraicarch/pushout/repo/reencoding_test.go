// A store that disclaims ExactEnvelopeBytes: it keeps decoded
// envelopes and re-encodes on read. This is the in-tree instance of
// the row-store shape ADR-0220 admits, and its two gates: the
// conformance suite over decodable fixtures, and the engine's own
// behaviour over it.
package repo_test

import (
	"context"
	"fmt"
	"iter"
	"sync"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/envelope"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/patch"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/types"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo/storagetest"
)

// reencodingStore decorates a filestore: envelopes are decoded on put,
// kept as values in a process-wide table keyed by location (the
// "rows"), and re-encoded with the local wire codec on read. Log,
// snapshot and ledger pass through. It declares what it does.
type reencodingStore struct {
	repo.StorageI
	reg  *envelope.Registry
	wire string
	rows *envelopeRows
}

type envelopeRows struct {
	mu   sync.Mutex
	envs map[t.PatchHash]envelope.EnvelopeV1
}

var (
	reencodingTablesMu sync.Mutex
	reencodingTables   = map[string]*envelopeRows{}
)

func openReencoding(tt testing.TB, location string) *reencodingStore {
	tt.Helper()
	opts := testOptions(tt, location)
	reencodingTablesMu.Lock()
	rows, ok := reencodingTables[location]
	if !ok {
		rows = &envelopeRows{envs: map[t.PatchHash]envelope.EnvelopeV1{}}
		reencodingTables[location] = rows
	}
	reencodingTablesMu.Unlock()
	return &reencodingStore{StorageI: opts.Storage, reg: opts.Codecs, wire: opts.Wire, rows: rows}
}

func (s *reencodingStore) Capabilities() repo.Capabilities {
	c := s.StorageI.Capabilities()
	c.ExactEnvelopeBytes = false
	return c
}

func (s *reencodingStore) PutEnvelope(_ context.Context, h t.PatchHash, framed []byte) error {
	env, _, err := s.reg.Decode(framed)
	if err != nil {
		return err
	}
	s.rows.mu.Lock()
	defer s.rows.mu.Unlock()
	if _, ok := s.rows.envs[h]; !ok { // first write wins, on the logical envelope
		s.rows.envs[h] = env
	}
	return nil
}

func (s *reencodingStore) PutEnvelopes(ctx context.Context, envs []repo.Envelope) error {
	return repo.PutEnvelopesOneByOne(ctx, s, envs)
}

func (s *reencodingStore) GetEnvelope(_ context.Context, h t.PatchHash) ([]byte, error) {
	s.rows.mu.Lock()
	env, ok := s.rows.envs[h]
	s.rows.mu.Unlock()
	if !ok {
		return nil, repo.ErrEnvelopeNotFound
	}
	return s.reg.Encode(s.wire, env)
}

func (s *reencodingStore) LoadEnvelopes(ctx context.Context, hs []t.PatchHash) iter.Seq2[repo.Envelope, error] {
	return repo.LoadEnvelopesOneByOne(ctx, s, hs)
}

func (s *reencodingStore) HasEnvelope(_ context.Context, h t.PatchHash) (bool, error) {
	s.rows.mu.Lock()
	defer s.rows.mu.Unlock()
	_, ok := s.rows.envs[h]
	return ok, nil
}

// envelopeFixtures builds decodable fixtures: four independent patches
// with an alternate frame per hash that differs only in producer, and
// a logical equality over hash, producer and timestamp.
func envelopeFixtures(tt testing.TB) (fx storagetest.Fixtures) {
	tt.Helper()
	reg, err := envelope.NewRegistry(envelope.CBORV1{})
	if err != nil {
		tt.Fatal(err)
	}
	when := time.Unix(1_700_000_000, 0).UTC()
	for i := range 4 {
		p := patch.NewPatch("fx", fmt.Sprintf("fixture %d", i), nil, []patch.Change{{
			Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0},
			Content: []byte(fmt.Sprintf("line %d\n", i)), UpContext: []t.NodeID{t.RootNodeID},
		}})
		framed, err := reg.Encode(envelope.CBORV1Name, envelope.EnvelopeV1{Patch: p, Producer: "first", Timestamp: when})
		if err != nil {
			tt.Fatal(err)
		}
		alt, err := reg.Encode(envelope.CBORV1Name, envelope.EnvelopeV1{Patch: p, Producer: "second", Timestamp: when})
		if err != nil {
			tt.Fatal(err)
		}
		fx.Envelopes = append(fx.Envelopes, storagetest.FixtureEnvelope{Hash: p.Hash, Framed: framed, Alternate: alt})
	}
	fx.Equal = func(got, want []byte) bool {
		g, _, gerr := reg.Decode(got)
		w, _, werr := reg.Decode(want)
		return gerr == nil && werr == nil && g.Patch.Hash == w.Patch.Hash && g.Producer == w.Producer && g.Timestamp.Equal(w.Timestamp)
	}
	return
}

// Gate 1: the conformance suite over decodable fixtures. Without them
// the envelope checks would be skipped for this store; with them the
// suite checks logical round-trips and first-write-wins on the
// envelope, not the bytes.
func TestReencodingStore_Conformance(tt *testing.T) {
	storagetest.RunWith(tt, func(location string) (repo.StorageI, error) {
		return openReencoding(tt, location), nil
	}, envelopeFixtures(tt))
}

// The default Run skips the envelope checks for this store rather than
// failing them — the documented behaviour for a disclaimed capability.
func TestReencodingStore_DefaultRunSkipsEnvelopes(tt *testing.T) {
	storagetest.Run(tt, func(location string) (repo.StorageI, error) {
		return openReencoding(tt, location), nil
	})
}

// Gate 2: the engine over the store — record, delete, sweep, crash-
// reopen by full replay through re-encoded envelopes, and a batch
// ingest into a second repo from EncodedEnvelopes.
func TestRepo_OverReencodingStore(tt *testing.T) {
	ctx := context.Background()
	dir := tt.TempDir()
	open := func() (*repo.Repo, repo.Options) {
		st := openReencoding(tt, dir)
		opts := repo.Options{Storage: st, Codecs: st.reg, Wire: st.wire, Producer: "tester", Clock: testClock()}
		r, err := repo.Open(ctx, opts)
		if err != nil {
			tt.Fatal(err)
		}
		return r, opts
	}
	r, opts := open()
	if r.Guarantees() != (repo.Guarantees{SweepAllowed: true, RetentionDurable: true, UnrecordSupported: true, Recovery: repo.RecoveryFromSnapshot}) {
		tt.Fatalf("guarantees = %+v", r.Guarantees())
	}
	hashes := chainHistory(tt, r, 40)
	if _, err := r.Sweep(ctx, time.Unix(2_000_000_000, 0), 0); err != nil {
		tt.Fatal(err)
	}
	want := fingerprint(tt, r)
	envs, err := r.EncodedEnvelopes(ctx, hashes)
	if err != nil {
		tt.Fatal(err)
	}
	// Crash-reopen: no Close, so no snapshot — recovery is a full replay
	// of re-encoded envelopes, and the identity check on every one must
	// pass.
	_ = opts.Storage.Close()
	r2, _ := open()
	if got := fingerprint(tt, r2); got != want {
		tt.Fatalf("state after replay through re-encoded envelopes:\n got:\n%s\nwant:\n%s", got, want)
	}
	assertInvariants(tt, r2)

	// The re-encoded envelopes ingest elsewhere as the same patches.
	other := openTest(tt, tt.TempDir())
	report, err := other.ApplyEnvelopes(ctx, envs)
	if err != nil || len(report.Applied) != len(hashes) {
		tt.Fatalf("ingest of re-encoded envelopes: applied=%d err=%v", len(report.Applied), err)
	}
	// Same sweep on the second repo: purge status is replica-local, so
	// only after it do the two materialised states compare equal.
	if _, err := other.Sweep(ctx, time.Unix(2_000_000_000, 0), 0); err != nil {
		tt.Fatal(err)
	}
	if stateFingerprint(tt, other) != stateFingerprint(tt, r2) {
		tt.Fatal("re-encoded envelopes did not reproduce the state elsewhere")
	}
}
