// The consumer-facing contract of ADR-0220, in executable form: which
// (retention mode × store capabilities) combinations Open accepts, what
// Guarantees each yields, and — the crash matrix — that Guarantees
// predicts what survives a crash-reopen for every store shape and mode.
package repo_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/types"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo/storagetest"
)

// noSnapshotStore is a filestore that has decided not to keep
// snapshots: saves are discarded, loads report none, and it says so.
type noSnapshotStore struct{ repo.StorageI }

func (noSnapshotStore) SaveSnapshot(context.Context, repo.Snapshot) error { return nil }
func (noSnapshotStore) LoadSnapshot(context.Context) (repo.Snapshot, bool, error) {
	return repo.Snapshot{}, false, nil
}
func (s noSnapshotStore) Capabilities() repo.Capabilities {
	c := s.StorageI.Capabilities()
	c.Snapshots = false
	return c
}

// noLedgerStore is a filestore that keeps no retention ledger — the
// shape whose only purge carrier is the snapshot.
type noLedgerStore struct{ repo.StorageI }

func (noLedgerStore) SaveRetention(context.Context, []repo.RetentionEntry) error { return nil }
func (noLedgerStore) LoadRetention(context.Context) ([]repo.RetentionEntry, error) {
	return nil, nil
}
func (s noLedgerStore) Capabilities() repo.Capabilities {
	c := s.StorageI.Capabilities()
	c.RetentionLedger = false
	return c
}

func TestNoLedgerStore_Conformance(tt *testing.T) {
	storagetest.Run(tt, func(location string) (repo.StorageI, error) {
		opts := testOptions(tt, location)
		return noLedgerStore{opts.Storage}, nil
	})
}

// Over a ledger-less store Sweep's one durable write is the snapshot;
// a fault there is crash-equivalent, and the purge is reported as not
// durable because the snapshot is a carrier, not a guarantee.
func TestRepo_SweepSnapshotCarrierFaultIsCrashEquivalent(tt *testing.T) {
	ctx := context.Background()
	dir := tt.TempDir()
	opts := testOptions(tt, dir)
	fault := &faultStore{StorageI: noLedgerStore{opts.Storage}}
	opts.Storage = fault
	r, err := repo.Open(ctx, opts)
	if err != nil {
		tt.Fatal(err)
	}
	if r.Guarantees().RetentionDurable {
		tt.Fatal("ledger-less store reported durable retention")
	}
	chainHistory(tt, r, 40)
	want := fingerprint(tt, r)
	fault.failSnap = true
	if _, serr := r.Sweep(ctx, time.Unix(2_000_000_000, 0), 0); !errors.Is(serr, errInjected) {
		tt.Fatalf("sweep err = %v, want injected", serr)
	}
	if fingerprint(tt, r) != want {
		tt.Fatal("in-memory state changed across failed sweep")
	}
	_ = fault.Close()
	opts2 := testOptions(tt, dir)
	opts2.Storage = noLedgerStore{opts2.Storage}
	r2, err := repo.Open(ctx, opts2)
	if err != nil {
		tt.Fatal(err)
	}
	if fingerprint(tt, r2) != want {
		tt.Fatal("disk state diverged across failed sweep")
	}
	// Without the fault the snapshot carries the purge across a restart,
	// but only a covered snapshot — Durable stays false.
	report, err := r2.Sweep(ctx, time.Unix(2_000_000_000, 0), 0)
	if err != nil || len(report.Purged) == 0 || report.Durable {
		tt.Fatalf("sweep = %+v, %v; want purges and Durable == false", report, err)
	}
}

// The no-snapshot decorator is itself a conformant store: the suite
// runs its negative snapshot checks and the positive rest.
func TestNoSnapshotStore_Conformance(tt *testing.T) {
	storagetest.Run(tt, func(location string) (repo.StorageI, error) {
		opts := testOptions(tt, location)
		return noSnapshotStore{opts.Storage}, nil
	})
}

// A memStore with settable capabilities is conformant for every
// declaration, which is what makes it usable as the semantics oracle.
func TestMemStore_ConformanceEveryDeclaration(tt *testing.T) {
	for _, caps := range allCapabilityCombos() {
		tt.Run(capsName(caps), func(tt *testing.T) {
			// One store per location: the suite reopens a location and
			// expects what was written to still be there.
			stores := map[string]*memStore{}
			storagetest.Run(tt, func(location string) (repo.StorageI, error) {
				m, ok := stores[location]
				if !ok {
					m = newMemStore()
					m.caps = caps
					stores[location] = m
				}
				return m, nil
			})
		})
	}
}

func allCapabilityCombos() (out []repo.Capabilities) {
	for i := range 16 {
		out = append(out, repo.Capabilities{
			Snapshots:          i&1 != 0,
			RetentionLedger:    i&2 != 0,
			ReplaceApplied:     i&4 != 0,
			ExactEnvelopeBytes: i&8 != 0,
		})
	}
	return
}

func capsName(c repo.Capabilities) string {
	return fmt.Sprintf("snap=%v ledger=%v replace=%v exact=%v", c.Snapshots, c.RetentionLedger, c.ReplaceApplied, c.ExactEnvelopeBytes)
}

// The semantics golden: for every mode and capability set, Open's
// verdict and the Guarantees it derives. Read top to bottom, this table
// is the contract.
func TestRepo_SemanticsGolden(tt *testing.T) {
	ctx := context.Background()
	modes := []repo.RetentionMode{repo.RetentionHygiene, repo.RetentionNone, repo.RetentionCompliance}
	for _, mode := range modes {
		for _, caps := range allCapabilityCombos() {
			tt.Run(mode.String()+"/"+capsName(caps), func(tt *testing.T) {
				m := newMemStore()
				m.caps = caps
				opts := memOptions(tt, m)
				opts.Retention = mode
				r, err := repo.Open(ctx, opts)

				// Open refuses exactly one combination class: Compliance
				// without a ledger.
				if mode == repo.RetentionCompliance && !caps.RetentionLedger {
					if !errors.Is(err, repo.ErrCapability) {
						tt.Fatalf("open = %v, want ErrCapability", err)
					}
					return
				}
				if err != nil {
					tt.Fatal(err)
				}
				want := repo.Guarantees{
					SweepAllowed:      mode != repo.RetentionNone,
					RetentionDurable:  caps.RetentionLedger && mode != repo.RetentionNone,
					UnrecordSupported: caps.ReplaceApplied,
					Recovery:          repo.RecoveryFullReplay,
				}
				if caps.Snapshots {
					want.Recovery = repo.RecoveryFromSnapshot
				}
				if got := r.Guarantees(); got != want {
					tt.Fatalf("guarantees = %+v, want %+v", got, want)
				}

				// The verbs agree with the guarantees.
				hashes := chainHistory(tt, r, 12) // includes a delete
				_, serr := r.Sweep(ctx, time.Unix(2_000_000_000, 0), 0)
				if want.SweepAllowed != (serr == nil) {
					tt.Fatalf("sweep err = %v, SweepAllowed = %v", serr, want.SweepAllowed)
				}
				if !want.SweepAllowed && !errors.Is(serr, repo.ErrUnsupported) {
					tt.Fatalf("refused sweep err = %v, want ErrUnsupported", serr)
				}
				uerr := r.Unrecord(ctx, hashes[len(hashes)-1])
				if want.UnrecordSupported != (uerr == nil) {
					tt.Fatalf("unrecord err = %v, UnrecordSupported = %v", uerr, want.UnrecordSupported)
				}
				if !want.UnrecordSupported && !errors.Is(uerr, repo.ErrUnsupported) {
					tt.Fatalf("refused unrecord err = %v, want ErrUnsupported", uerr)
				}
				// A store without a ledger is never written to; one with a
				// ledger under None is never written to either.
				if !want.RetentionDurable && len(m.ret) != 0 {
					tt.Fatalf("ledger written although RetentionDurable is false: %d entries", len(m.ret))
				}
			})
		}
	}
}

// The crash matrix: store shapes × modes, sweep then crash-reopen.
// Guarantees.RetentionDurable predicts whether the purge and the
// stamps survive; the applied state survives regardless.
func TestRepo_CrashMatrix(tt *testing.T) {
	ctx := context.Background()
	type shape struct {
		name string
		open func(tt *testing.T, dir string) repo.Options
	}
	shapes := []shape{
		{"filestore", func(tt *testing.T, dir string) repo.Options { return testOptions(tt, dir) }},
		{"filestore-nosnapshot", func(tt *testing.T, dir string) repo.Options {
			o := testOptions(tt, dir)
			o.Storage = noSnapshotStore{o.Storage}
			return o
		}},
	}
	// The memStore cannot crash-reopen (it has no location), so its
	// shapes are covered by the golden above; here every shape is a
	// real store over a directory.
	modes := []repo.RetentionMode{repo.RetentionHygiene, repo.RetentionCompliance}
	for _, sh := range shapes {
		for _, mode := range modes {
			tt.Run(sh.name+"/"+mode.String(), func(tt *testing.T) {
				dir := tt.TempDir()
				opts := sh.open(tt, dir)
				opts.Retention = mode
				r, err := repo.Open(ctx, opts)
				if err != nil {
					tt.Fatal(err)
				}
				g := r.Guarantees()
				chainHistory(tt, r, 40)
				stampsBefore, _ := r.RetentionStamps(ctx)
				report, err := r.Sweep(ctx, time.Unix(2_000_000_000, 0), 0)
				if err != nil {
					tt.Fatal(err)
				}
				if len(report.Purged) == 0 {
					tt.Fatal("history produced no purge")
				}
				if report.Durable != g.RetentionDurable {
					tt.Fatalf("SweepReport.Durable = %v, Guarantees.RetentionDurable = %v", report.Durable, g.RetentionDurable)
				}
				stateBefore := stateFingerprint(tt, r)
				purgedBefore := countPurged(tt, r)

				// Crash: release the lock as a dying process would, reopen.
				if cerr := opts.Storage.Close(); cerr != nil {
					tt.Fatal(cerr)
				}
				opts2 := sh.open(tt, dir)
				opts2.Retention = mode
				var recovered repo.RecoveredEvent
				opts2.Hooks.OnRecovered = func(ev repo.RecoveredEvent) { recovered = ev }
				r2, err := repo.Open(ctx, opts2)
				if err != nil {
					tt.Fatal(err)
				}
				assertInvariants(tt, r2)
				if g.RetentionDurable {
					if got := countPurged(tt, r2); got != purgedBefore {
						tt.Fatalf("purges after crash = %d, want %d (RetentionDurable)", got, purgedBefore)
					}
					if stateFingerprint(tt, r2) != stateBefore {
						tt.Fatal("state after crash differs although retention is durable")
					}
					stampsAfter, _ := r2.RetentionStamps(ctx)
					if len(stampsAfter) != len(stampsBefore) {
						tt.Fatalf("stamps after crash = %d, want %d", len(stampsAfter), len(stampsBefore))
					}
					if !recovered.FromSnapshot && recovered.PurgesRestored != purgedBefore {
						tt.Fatalf("PurgesRestored = %d after full replay, want %d", recovered.PurgesRestored, purgedBefore)
					}
				}
				// A snapshot is used only where the guarantee allows one; a
				// store that allows one may still have none to use.
				if recovered.FromSnapshot && g.Recovery != repo.RecoveryFromSnapshot {
					tt.Fatalf("recovered from a snapshot although Guarantees.Recovery = %v", g.Recovery)
				}
			})
		}
	}
}

// A decorator that embeds a StorageI inherits its declaration, so a
// wrapper cannot silently change what the engine believes.
func TestRepo_DecoratorInheritsCapabilities(tt *testing.T) {
	opts := testOptions(tt, tt.TempDir())
	wrapped := &faultStore{StorageI: opts.Storage}
	if wrapped.Capabilities() != opts.Storage.Capabilities() {
		tt.Fatal("embedding did not pass the declaration through")
	}
	if (noSnapshotStore{opts.Storage}).Capabilities().Snapshots {
		tt.Fatal("an overriding decorator must be able to change the declaration")
	}
}

func countPurged(tt testing.TB, r *repo.Repo) (n int) {
	tt.Helper()
	err := r.View(context.Background(), func(v repo.ViewI) error {
		for id := range v.Visualizable().AllDeletedNodes() {
			if v.Graph().NodeContentStatus(id) == t.NodeContentStatusPurged {
				n++
			}
		}
		return nil
	})
	if err != nil {
		tt.Fatal(err)
	}
	return
}
