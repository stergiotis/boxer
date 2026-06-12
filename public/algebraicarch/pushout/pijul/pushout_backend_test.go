// Regression tests for the pushout-native backend defects found in the
// 2026-06-11 review: the dependency gate consulting "seen" instead of
// "applied" (phantom state, permanently wedged repo), conflict-mode saves
// dropping clean-cell edits, lossy value quoting, and Unrecord lacking a
// dependent check. Each test is the in-repo port of an executable repro
// that failed before the fix, with the fixed behavior as the expectation.
package pijul

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/qc"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo"
)

func newTestRepo(tt *testing.T, b BackendI, actor string) *PushoutRepo {
	tt.Helper()
	repo := b.NewRepo(actor, tt.TempDir()).(*PushoutRepo)
	if _, err := repo.Init(context.Background()); err != nil {
		tt.Fatal(err)
	}
	return repo
}

func mustRecord(tt *testing.T, repo *PushoutRepo, cells []KVLine, author, msg string) PatchID {
	tt.Helper()
	id, _, err := repo.SetAndRecord(context.Background(), cells, author, msg)
	if err != nil {
		tt.Fatalf("record %q: %v", msg, err)
	}
	return id
}

func mustHash(tt *testing.T, id PatchID) (h t.PatchHash) {
	tt.Helper()
	if err := h.UnmarshalText([]byte(id.Hex)); err != nil {
		tt.Fatalf("parse patch id %q: %v", id.Hex, err)
	}
	return
}

func stateCells(tt *testing.T, repo *PushoutRepo) (cells []KVLine, log []PatchMetadata) {
	tt.Helper()
	cells, log, _, err := repo.State(context.Background())
	if err != nil {
		tt.Fatal(err)
	}
	return
}

func assertRepoInvariants(tt *testing.T, pr *PushoutRepo) {
	tt.Helper()
	eng := pr.Engine()
	if eng == nil {
		return
	}
	err := eng.View(context.Background(), func(v repo.ViewI) error {
		for _, e := range qc.CheckInvariants(v.Graph().(t.InspectableI)) {
			tt.Errorf("invariant violated on %s: %v", pr.Path(), e)
		}
		return nil
	})
	if err != nil {
		tt.Fatal(err)
	}
}

// conflictPair builds alice/bob with a converged base {a,b}, diverging
// edits to cell "a", and bob pulled into conflict.
func conflictPair(tt *testing.T) (alice, bob *PushoutRepo) {
	tt.Helper()
	ctx := context.Background()
	b := NewPushoutBackend()
	alice = newTestRepo(tt, b, "alice")
	mustRecord(tt, alice, []KVLine{{Path: "a", Value: "1"}, {Path: "b", Value: "1"}}, "alice", "base")
	bobI, _, err := b.Clone(ctx, alice, tt.TempDir(), "bob")
	if err != nil {
		tt.Fatal(err)
	}
	bob = bobI.(*PushoutRepo)
	mustRecord(tt, alice, []KVLine{{Path: "a", Value: "alice"}, {Path: "b", Value: "1"}}, "alice", "edit a")
	mustRecord(tt, bob, []KVLine{{Path: "a", Value: "bob"}, {Path: "b", Value: "1"}}, "bob", "edit a")
	_, hadConflict, err := bob.Pull(ctx, alice)
	if err != nil {
		tt.Fatal(err)
	}
	if !hadConflict {
		tt.Fatal("setup: expected a conflict on cell a")
	}
	return
}

// The dependency gate must consult the APPLIED set, not the seen-envelope
// cache: after unrecording P and Q, applying Q alone is cleanly rejected,
// the repo state stays untouched (no phantom cells), and a subsequent
// Push redelivers P,Q in order.
func TestApply_DependencyGateUsesAppliedSet(tt *testing.T) {
	ctx := context.Background()
	b := NewPushoutBackend()
	alice := newTestRepo(tt, b, "alice")
	bob := newTestRepo(tt, b, "bob")

	idP := mustRecord(tt, alice, []KVLine{{Path: "k", Value: "v1"}}, "alice", "P")
	idQ := mustRecord(tt, alice, []KVLine{{Path: "k", Value: "v2"}}, "alice", "Q")
	if _, err := alice.Push(ctx, bob); err != nil {
		tt.Fatal(err)
	}
	if _, err := bob.Unrecord(ctx, mustHash(tt, idQ)); err != nil {
		tt.Fatal(err)
	}
	if _, err := bob.Unrecord(ctx, mustHash(tt, idP)); err != nil {
		tt.Fatal(err)
	}

	envQ, _, err := alice.ExportLatest(ctx)
	if err != nil {
		tt.Fatal(err)
	}
	_, aerr := bob.Apply(ctx, envQ)
	if !errors.Is(aerr, repo.ErrMissingDependency) {
		tt.Fatalf("expected ErrMissingDependency (P seen but not applied), got: %v", aerr)
	}

	cells, log := stateCells(tt, bob)
	if len(cells) != 0 || len(log) != 0 {
		tt.Fatalf("rejected Apply left phantom state: cells=%+v log=%d", cells, len(log))
	}
	assertRepoInvariants(tt, bob)

	if _, err := alice.Push(ctx, bob); err != nil {
		tt.Fatalf("repo wedged — redelivery failed: %v", err)
	}
	cells, log = stateCells(tt, bob)
	if len(cells) != 1 || cells[0].Value != "v2" || len(log) != 2 {
		tt.Fatalf("after redelivery: cells=%+v log=%d", cells, len(log))
	}
	assertRepoInvariants(tt, bob)
}

// Saving while conflicted must record edits to clean cells instead of
// silently dropping them, and the conflict must remain resolvable.
func TestSetAndRecord_RecordsCleanEditDuringConflict(tt *testing.T) {
	_, bob := conflictPair(tt)

	cells, _ := stateCells(tt, bob)
	edited := append([]KVLine(nil), cells...)
	touched := false
	for i := range edited {
		if edited[i].Path == "b" {
			edited[i].Value = "2"
			touched = true
		}
	}
	if !touched {
		tt.Fatalf("setup: no clean cell b in state: %+v", cells)
	}
	id := mustRecord(tt, bob, edited, "bob", "edit b during conflict")
	if id.Empty() {
		tt.Fatal("edit to clean cell during conflict was dropped (no patch recorded)")
	}

	after, _ := stateCells(tt, bob)
	for _, c := range after {
		if c.Path == "b" && (c.Conflict != nil || c.Value != "2") {
			tt.Fatalf("clean-cell edit not visible after save: %+v", c)
		}
		if c.Path == "a" && c.Conflict == nil {
			tt.Fatalf("conflict on a vanished without resolution: %+v", c)
		}
	}
	assertRepoInvariants(tt, bob)

	// And the conflict still resolves normally afterwards.
	resolved := append([]KVLine(nil), after...)
	for i := range resolved {
		if resolved[i].Path == "a" {
			resolved[i] = KVLine{Path: "a", Value: "alice"}
		}
	}
	mustRecord(tt, bob, resolved, "bob", "resolve a")
	final, _ := stateCells(tt, bob)
	values := map[string]string{}
	for _, c := range final {
		if c.Conflict != nil {
			tt.Fatalf("unexpected conflict after resolution: %+v", c)
		}
		values[c.Path] = c.Value
	}
	if values["a"] != "alice" || values["b"] != "2" {
		tt.Fatalf("final state wrong: %v", values)
	}
	assertRepoInvariants(tt, bob)
}

// Creating a brand-new cell while conflicted has no reliable anchor and
// must be rejected with a clear error rather than silently ignored.
func TestSetAndRecord_RejectsNewCellDuringConflict(tt *testing.T) {
	_, bob := conflictPair(tt)
	cells, _ := stateCells(tt, bob)
	cells = append(cells, KVLine{Path: "z", Value: "new"})
	_, _, err := bob.SetAndRecord(context.Background(), cells, "bob", "create during conflict")
	if !errors.Is(err, ErrCellCreateWhileConflicted) {
		tt.Fatalf("expected ErrCellCreateWhileConflicted, got: %v", err)
	}
}

// Values containing quotes, backslashes, and newlines must round-trip
// byte-exactly through record + State.
func TestSetAndRecord_ValueRoundTrip(tt *testing.T) {
	b := NewPushoutBackend()
	repo := newTestRepo(tt, b, "alice")
	want := map[string]string{
		"len":   `2"`,
		"quote": `a"b\c"`,
		"multi": "line1\nline2",
		"empty": "",
	}
	cells := make([]KVLine, 0, len(want))
	for p, v := range want {
		cells = append(cells, KVLine{Path: p, Value: v})
	}
	mustRecord(tt, repo, cells, "alice", "tricky values")
	got, _ := stateCells(tt, repo)
	if len(got) != len(want) {
		tt.Fatalf("cell count: got %d want %d (%+v)", len(got), len(want), got)
	}
	for _, c := range got {
		if want[c.Path] != c.Value {
			tt.Errorf("path %s: got %q want %q", c.Path, c.Value, want[c.Path])
		}
	}
}

// Paths that cannot survive the line format are rejected up front.
func TestSetAndRecord_RejectsInvalidPath(tt *testing.T) {
	b := NewPushoutBackend()
	repo := newTestRepo(tt, b, "alice")
	for _, bad := range []string{"", "a b", `a"b`, "a\nb"} {
		_, _, err := repo.SetAndRecord(context.Background(), []KVLine{{Path: bad, Value: "v"}}, "alice", "bad path")
		if err == nil || !strings.Contains(err.Error(), "invalid cell path") {
			tt.Fatalf("path %q: expected rejection, got: %v", bad, err)
		}
	}
}

// Unrecord must refuse while an applied patch declares the target as a
// dependency, and work in reverse order.
func TestUnrecord_RefusesWhileDependentsApplied(tt *testing.T) {
	ctx := context.Background()
	b := NewPushoutBackend()
	alice := newTestRepo(tt, b, "alice")
	idP := mustRecord(tt, alice, []KVLine{{Path: "k", Value: "v1"}}, "alice", "P")
	idQ := mustRecord(tt, alice, []KVLine{{Path: "k", Value: "v2"}}, "alice", "Q")

	_, err := alice.Unrecord(ctx, mustHash(tt, idP))
	if !errors.Is(err, repo.ErrDependentExists) {
		tt.Fatalf("expected ErrDependentExists, got: %v", err)
	}
	if _, err := alice.Unrecord(ctx, mustHash(tt, idQ)); err != nil {
		tt.Fatal(err)
	}
	if _, err := alice.Unrecord(ctx, mustHash(tt, idP)); err != nil {
		tt.Fatal(err)
	}
	cells, log := stateCells(tt, alice)
	if len(cells) != 0 || len(log) != 0 {
		tt.Fatalf("expected empty state after unrecording everything: cells=%+v log=%d", cells, len(log))
	}
	assertRepoInvariants(tt, alice)
}

// EXPLANATION.md claim: Unrecord is round-trippable — a subsequent Pull
// from a peer that still has the patch reapplies it cleanly and the
// state converges back.
func TestClaim_UnrecordRoundTripViaPull(tt *testing.T) {
	ctx := context.Background()
	b := NewPushoutBackend()
	alice := newTestRepo(tt, b, "alice")
	bob := newTestRepo(tt, b, "bob")
	mustRecord(tt, alice, []KVLine{{Path: "k", Value: "v1"}}, "alice", "P")
	idQ := mustRecord(tt, alice, []KVLine{{Path: "k", Value: "v2"}}, "alice", "Q")
	if _, err := alice.Push(ctx, bob); err != nil {
		tt.Fatal(err)
	}
	before, beforeLog := stateCells(tt, bob)

	if _, err := bob.Unrecord(ctx, mustHash(tt, idQ)); err != nil {
		tt.Fatal(err)
	}
	if mid, _ := stateCells(tt, bob); len(mid) != 1 || mid[0].Value != "v1" {
		tt.Fatalf("after unrecord: %+v", mid)
	}
	if _, hadConflict, err := bob.Pull(ctx, alice); err != nil || hadConflict {
		tt.Fatalf("pull back: hadConflict=%v err=%v", hadConflict, err)
	}
	after, afterLog := stateCells(tt, bob)
	if len(after) != len(before) || after[0].Value != before[0].Value || len(afterLog) != len(beforeLog) {
		tt.Fatalf("round-trip diverged: before=%+v after=%+v", before, after)
	}
	assertRepoInvariants(tt, bob)
}

// Concurrent verbs on shared repos: correctness of outcomes is covered
// elsewhere; this exists to give the race detector real interleavings
// over the lock discipline (missingOn cloning, clone-and-swap).
func TestConcurrentPushPullRecord(tt *testing.T) {
	ctx := context.Background()
	b := NewPushoutBackend()
	alice := newTestRepo(tt, b, "alice")
	bob := newTestRepo(tt, b, "bob")
	mustRecord(tt, alice, []KVLine{{Path: "k", Value: "v0"}}, "alice", "base")

	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(3)
		go func() {
			defer wg.Done()
			_, _ = alice.Push(ctx, bob)
		}()
		go func() {
			defer wg.Done()
			_, _, _ = bob.Pull(ctx, alice)
		}()
		go func(n int) {
			defer wg.Done()
			_, _, _ = alice.SetAndRecord(ctx, []KVLine{{Path: "k", Value: string(rune('a' + n))}}, "alice", "concurrent edit")
		}(i)
		wg.Wait()
	}
	// Converge and verify both ends are structurally sound.
	if _, err := alice.Push(ctx, bob); err != nil {
		tt.Fatal(err)
	}
	assertRepoInvariants(tt, alice)
	assertRepoInvariants(tt, bob)
}

// Re-creating a previously deleted cell with identical content used to
// reproduce the identical {deps, changes} and therefore the same patch
// hash as the applied-but-tombstoned original — which can never re-apply
// ("node already exists"). SetAndRecord must disambiguate node identity
// deterministically. Found by the rapid state machine.
func TestSetAndRecord_RecreateDeletedCell(tt *testing.T) {
	ctx := context.Background()
	b := NewPushoutBackend()
	alice := newTestRepo(tt, b, "alice")
	mustRecord(tt, alice, []KVLine{{Path: "k", Value: "v"}}, "alice", "create")
	mustRecord(tt, alice, nil, "alice", "delete k")
	mustRecord(tt, alice, []KVLine{{Path: "k", Value: "v"}}, "alice", "recreate")

	cells, log := stateCells(tt, alice)
	if len(cells) != 1 || cells[0].Value != "v" {
		tt.Fatalf("recreated cell missing: %+v", cells)
	}
	if len(log) != 3 {
		tt.Fatalf("expected 3 patches, got %d", len(log))
	}
	assertRepoInvariants(tt, alice)

	// And the disambiguated patch must ship cleanly.
	bob := newTestRepo(tt, b, "bob")
	if _, err := alice.Push(ctx, bob); err != nil {
		tt.Fatal(err)
	}
	bcells, _ := stateCells(tt, bob)
	if len(bcells) != 1 || bcells[0].Value != "v" {
		tt.Fatalf("bob state after push: %+v", bcells)
	}
	assertRepoInvariants(tt, bob)
}

// EXPLANATION.md claim: Push ships envelopes in apply-log order, so
// dependencies always precede dependents — a fresh repo receives a
// three-deep dependency chain in one Push without rejections.
func TestClaim_PushShipsDepsFirst(tt *testing.T) {
	ctx := context.Background()
	b := NewPushoutBackend()
	alice := newTestRepo(tt, b, "alice")
	mustRecord(tt, alice, []KVLine{{Path: "k", Value: "v1"}}, "alice", "P")
	mustRecord(tt, alice, []KVLine{{Path: "k", Value: "v2"}}, "alice", "Q")
	mustRecord(tt, alice, []KVLine{{Path: "k", Value: "v3"}}, "alice", "R")

	bob := newTestRepo(tt, b, "bob")
	if _, err := alice.Push(ctx, bob); err != nil {
		tt.Fatalf("push of dependency chain failed: %v", err)
	}
	_, alog := stateCells(tt, alice)
	bcells, blog := stateCells(tt, bob)
	if len(blog) != len(alog) {
		tt.Fatalf("log length: got %d want %d", len(blog), len(alog))
	}
	for i := range alog {
		if alog[i].ID != blog[i].ID {
			tt.Fatalf("apply order diverged at %d: %s vs %s", i, alog[i].ID.Short(), blog[i].ID.Short())
		}
	}
	if len(bcells) != 1 || bcells[0].Value != "v3" {
		tt.Fatalf("bob final state: %+v", bcells)
	}
}
