// Fault injection on the persistence layer: every mutating backend verb
// must be transactional under disk-write failures. The failures are
// injected with permission bits (no VFS seam needed): a read-only
// changes/ directory fails envelope creation; a read-only applied.txt
// fails the log append AFTER the envelope write — exercising both halves
// of the write sequence. In each case the verb must return an error,
// the observable state, applied log, and graggle invariants must be
// unchanged, and the verb must succeed once the fault is lifted.
package pijul

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo"
)

// faultable wraps the chmod fault with restore-on-cleanup so TempDir
// removal works even when an assertion fails mid-test.
func injectFault(tt *testing.T, path string, mode os.FileMode) (restore func()) {
	tt.Helper()
	if os.Geteuid() == 0 {
		tt.Skip("running as root: permission bits do not bind, fault injection is inert")
	}
	info, err := os.Stat(path)
	if err != nil {
		tt.Fatal(err)
	}
	orig := info.Mode().Perm()
	if err := os.Chmod(path, mode); err != nil {
		tt.Fatal(err)
	}
	restored := false
	restore = func() {
		if !restored {
			restored = true
			_ = os.Chmod(path, orig)
		}
	}
	tt.Cleanup(restore)
	return restore
}

func readAppliedFile(tt *testing.T, repo *PushoutRepo) string {
	tt.Helper()
	data, err := os.ReadFile(filepath.Join(repo.Path(), ".pushout", "applied.txt"))
	if errors.Is(err, fs.ErrNotExist) {
		return "" // fresh store: the log file appears on first append
	}
	if err != nil {
		tt.Fatal(err)
	}
	return string(data)
}

func snapshotObservable(tt *testing.T, repo *PushoutRepo) (cells []KVLine, logLen int, applied string) {
	tt.Helper()
	c, l := stateCells(tt, repo)
	return c, len(l), readAppliedFile(tt, repo)
}

func assertUnchanged(tt *testing.T, repo *PushoutRepo, wantCells []KVLine, wantLogLen int, wantApplied string) {
	tt.Helper()
	cells, logLen, applied := snapshotObservable(tt, repo)
	if len(cells) != len(wantCells) {
		tt.Fatalf("cell count changed across failed verb: %d -> %d", len(wantCells), len(cells))
	}
	for i := range cells {
		if cells[i].Path != wantCells[i].Path || cells[i].Value != wantCells[i].Value {
			tt.Fatalf("cell %d changed across failed verb: %+v -> %+v", i, wantCells[i], cells[i])
		}
	}
	if logLen != wantLogLen {
		tt.Fatalf("patch log length changed across failed verb: %d -> %d", wantLogLen, logLen)
	}
	if applied != wantApplied {
		tt.Fatalf("applied.txt changed across failed verb:\nbefore: %q\nafter:  %q", wantApplied, applied)
	}
	assertRepoInvariants(tt, repo)
}

func TestFault_SetAndRecordEnvelopeWriteFails(tt *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(tt, NewPushoutBackend(), "alice")
	mustRecord(tt, repo, []KVLine{{Path: "k", Value: "v1"}}, "alice", "base")
	cells, logLen, applied := snapshotObservable(tt, repo)

	restore := injectFault(tt, filepath.Join(repo.Path(), ".pushout", "changes"), 0o555)
	_, _, err := repo.SetAndRecord(ctx, []KVLine{{Path: "k", Value: "v2"}}, "alice", "blocked")
	if err == nil {
		tt.Fatal("expected envelope write failure to surface")
	}
	assertUnchanged(tt, repo, cells, logLen, applied)

	restore()
	mustRecord(tt, repo, []KVLine{{Path: "k", Value: "v2"}}, "alice", "retry")
	after, _ := stateCells(tt, repo)
	if len(after) != 1 || after[0].Value != "v2" {
		tt.Fatalf("recovery record not visible: %+v", after)
	}
}

func TestFault_SetAndRecordAppliedLogAppendFails(tt *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(tt, NewPushoutBackend(), "alice")
	mustRecord(tt, repo, []KVLine{{Path: "k", Value: "v1"}}, "alice", "base")
	cells, logLen, applied := snapshotObservable(tt, repo)

	// Fail the SECOND write of the sequence: the envelope lands (it is
	// content-addressed; an orphan file is harmless), the log append
	// fails, and no in-memory state may advance.
	restore := injectFault(tt, filepath.Join(repo.Path(), ".pushout", "applied.txt"), 0o444)
	_, _, err := repo.SetAndRecord(ctx, []KVLine{{Path: "k", Value: "v2"}}, "alice", "blocked")
	if err == nil {
		tt.Fatal("expected applied-log append failure to surface")
	}
	assertUnchanged(tt, repo, cells, logLen, applied)

	restore()
	mustRecord(tt, repo, []KVLine{{Path: "k", Value: "v2"}}, "alice", "retry")
	after, _ := stateCells(tt, repo)
	if len(after) != 1 || after[0].Value != "v2" {
		tt.Fatalf("recovery record not visible: %+v", after)
	}
}

func TestFault_PushIntoFaultyPeerLeavesPeerConsistent(tt *testing.T) {
	ctx := context.Background()
	b := NewPushoutBackend()
	alice := newTestRepo(tt, b, "alice")
	bob := newTestRepo(tt, b, "bob")
	mustRecord(tt, alice, []KVLine{{Path: "k", Value: "v1"}}, "alice", "P")
	mustRecord(tt, alice, []KVLine{{Path: "k", Value: "v2"}}, "alice", "Q")
	bobCells, bobLog, bobApplied := snapshotObservable(tt, bob)

	restore := injectFault(tt, filepath.Join(bob.Path(), ".pushout", "changes"), 0o555)
	if _, err := alice.Push(ctx, bob); err == nil {
		tt.Fatal("expected push into faulty peer to fail")
	}
	assertUnchanged(tt, bob, bobCells, bobLog, bobApplied)

	restore()
	if _, err := alice.Push(ctx, bob); err != nil {
		tt.Fatalf("recovery push: %v", err)
	}
	after, log := stateCells(tt, bob)
	if len(after) != 1 || after[0].Value != "v2" || len(log) != 2 {
		tt.Fatalf("recovery push state: cells=%+v log=%d", after, len(log))
	}
	assertRepoInvariants(tt, bob)
}

func TestFault_UnrecordAppliedRewriteFails(tt *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(tt, NewPushoutBackend(), "alice")
	mustRecord(tt, repo, []KVLine{{Path: "k", Value: "v1"}}, "alice", "P")
	idQ := mustRecord(tt, repo, []KVLine{{Path: "k", Value: "v2"}}, "alice", "Q")
	cells, logLen, applied := snapshotObservable(tt, repo)

	restore := injectFault(tt, filepath.Join(repo.Path(), ".pushout"), 0o555)
	if _, err := repo.Unrecord(ctx, mustHash(tt, idQ)); err == nil {
		tt.Fatal("expected snapshot/log rewrite failure to surface")
	}
	assertUnchanged(tt, repo, cells, logLen, applied)

	restore()
	if _, err := repo.Unrecord(ctx, mustHash(tt, idQ)); err != nil {
		tt.Fatalf("recovery unrecord: %v", err)
	}
	after, _ := stateCells(tt, repo)
	if len(after) != 1 || after[0].Value != "v1" {
		tt.Fatalf("recovery unrecord state: %+v", after)
	}
	assertRepoInvariants(tt, repo)
}

// Retention blocking through the backend: after a sweep purges the
// tombstone a patch created, Unrecord of that patch (the sole deleter)
// is rejected with the retention error and leaves the repo untouched.
// Retention is session-local: a peer holding the same history but never
// swept can still unrecord the same patch.
func TestUnrecord_BlockedByRetentionAfterSweep(tt *testing.T) {
	ctx := context.Background()
	base := time.Unix(1_000_000, 0).UTC()
	n := 0
	b := NewPushoutBackendWithClock(func() time.Time { n++; return base.Add(time.Duration(n) * time.Minute) })
	alice := newTestRepo(tt, b, "alice")
	bob := newTestRepo(tt, b, "bob")

	mustRecord(tt, alice, []KVLine{{Path: "k", Value: "v1"}}, "alice", "P")
	idQ := mustRecord(tt, alice, []KVLine{{Path: "k", Value: "v2"}}, "alice", "Q") // deletes P's node
	if _, err := alice.Push(ctx, bob); err != nil {
		tt.Fatal(err)
	}

	// Sweep ONLY alice, far past every tombstone stamp.
	report, err := alice.Sweep(ctx, time.Unix(2_000_000, 0).UTC(), 0)
	if err != nil {
		tt.Fatal(err)
	}
	if len(report.Purged) == 0 {
		tt.Fatal("setup: sweep purged nothing")
	}

	before, beforeLog := stateCells(tt, alice)
	_, err = alice.Unrecord(ctx, mustHash(tt, idQ))
	if !errors.Is(err, repo.ErrRetentionBlocked) {
		tt.Fatalf("expected ErrRetentionBlocked, got: %v", err)
	}
	after, afterLog := stateCells(tt, alice)
	if len(after) != len(before) || after[0].Value != before[0].Value || len(afterLog) != len(beforeLog) {
		tt.Fatalf("rejected unrecord mutated state: %+v -> %+v", before, after)
	}
	assertRepoInvariants(tt, alice)

	// Bob never swept: the same unrecord works there.
	if _, err := bob.Unrecord(ctx, mustHash(tt, idQ)); err != nil {
		tt.Fatalf("retention must be session-local; bob's unrecord failed: %v", err)
	}
	bcells, _ := stateCells(tt, bob)
	if len(bcells) != 1 || bcells[0].Value != "v1" {
		tt.Fatalf("bob state after unrecord: %+v", bcells)
	}
	assertRepoInvariants(tt, bob)
}
