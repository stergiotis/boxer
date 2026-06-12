package exchange_test

import (
	"context"
	"slices"
	"testing"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/envelope"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/exchange"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/exchange/inproc"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/patch"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo"
)

func fakeHash(b byte) (h t.PatchHash) {
	h[0] = b
	return
}

func TestCompare_Verdicts(tt *testing.T) {
	p, q, r, s := fakeHash(1), fakeHash(2), fakeHash(3), fakeHash(4)
	hs := func(xs ...t.PatchHash) []t.PatchHash { return xs }
	cases := []struct {
		name         string
		a, b         []t.PatchHash
		want         exchange.RelationE
		onlyA, onlyB []t.PatchHash
	}{
		{"BothEmpty", nil, nil, exchange.RelationEqual, nil, nil},
		{"EqualSameOrder", hs(p, q), hs(p, q), exchange.RelationEqual, nil, nil},
		// Commuting peers hold the same set in different apply orders.
		{"EqualDifferentOrder", hs(p, q, r), hs(q, p, r), exchange.RelationEqual, nil, nil},
		{"Ahead", hs(p, q, r), hs(p, q), exchange.RelationAhead, hs(r), nil},
		{"AheadOfEmpty", hs(p), nil, exchange.RelationAhead, hs(p), nil},
		{"Behind", hs(p), hs(p, q), exchange.RelationBehind, nil, hs(q)},
		{"BehindEmpty", nil, hs(p), exchange.RelationBehind, nil, hs(p)},
		{"Diverged", hs(p, q, r), hs(p, q, s), exchange.RelationDiverged, hs(r), hs(s)},
		{"DivergedDisjoint", hs(p), hs(q), exchange.RelationDiverged, hs(p), hs(q)},
	}
	for _, c := range cases {
		tt.Run(c.name, func(tt *testing.T) {
			got := exchange.Compare(c.a, c.b)
			if got.Relation != c.want {
				tt.Fatalf("relation: got %v, want %v", got.Relation, c.want)
			}
			if !slices.Equal(got.OnlyA, c.onlyA) || !slices.Equal(got.OnlyB, c.onlyB) {
				tt.Fatalf("diffs: got %v / %v, want %v / %v", got.OnlyA, got.OnlyB, c.onlyA, c.onlyB)
			}
		})
	}
}

// The diffs come back in the respective input's order: when the inputs
// are applied logs, each side's missing list is dependency-ordered and
// ready to ship as-is.
func TestCompare_DiffsPreserveInputOrder(tt *testing.T) {
	p, x1, x2, y1, y2 := fakeHash(1), fakeHash(2), fakeHash(3), fakeHash(4), fakeHash(5)
	got := exchange.Compare(
		[]t.PatchHash{x1, p, x2},
		[]t.PatchHash{y2, p, y1},
	)
	if got.Relation != exchange.RelationDiverged {
		tt.Fatalf("relation: %v", got.Relation)
	}
	if !slices.Equal(got.OnlyA, []t.PatchHash{x1, x2}) {
		tt.Fatalf("OnlyA order not preserved: %v", got.OnlyA)
	}
	if !slices.Equal(got.OnlyB, []t.PatchHash{y2, y1}) {
		tt.Fatalf("OnlyB order not preserved: %v", got.OnlyB)
	}
}

func TestCompare_RelationString(tt *testing.T) {
	cases := map[exchange.RelationE]string{
		exchange.RelationEqual:    "equal",
		exchange.RelationBehind:   "behind",
		exchange.RelationAhead:    "ahead",
		exchange.RelationDiverged: "diverged",
		exchange.RelationE(99):    "unknown",
	}
	for rel, want := range cases {
		if got := rel.String(); got != want {
			tt.Fatalf("String(%d): got %q, want %q", uint8(rel), got, want)
		}
	}
}

// Compare against live repos through a whole lifecycle: fresh repos are
// Equal; a record makes one side Ahead (Behind with arguments
// swapped); independent records diverge with exactly the two new
// hashes; the join (pull both ways) restores Equal even though the two
// applied logs now order the patches differently; an unrecord makes
// that side strictly less advanced again — later in wall time, behind
// in the version order.
func TestCompare_RepoLifecycle(tt *testing.T) {
	ctx := context.Background()
	alice := openMixed(tt, "alice", envelope.JSONV1Name)
	bob := openMixed(tt, "bob", envelope.JSONV1Name)

	applied := func(r *repo.Repo) []t.PatchHash {
		tt.Helper()
		hs, err := r.Applied(ctx)
		if err != nil {
			tt.Fatal(err)
		}
		return hs
	}
	compare := func() exchange.Comparison {
		tt.Helper()
		return exchange.Compare(applied(alice), applied(bob))
	}
	rec := func(r *repo.Repo, author, line string) t.PatchHash {
		tt.Helper()
		h, err := r.Record(ctx, author, "add "+line, []patch.Change{{
			Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0},
			Content: []byte(line + "\n"), UpContext: []t.NodeID{t.RootNodeID},
		}})
		if err != nil {
			tt.Fatal(err)
		}
		return h
	}

	if c := compare(); c.Relation != exchange.RelationEqual {
		tt.Fatalf("fresh repos: %v", c.Relation)
	}

	hA := rec(alice, "alice", "alpha")
	if c := compare(); c.Relation != exchange.RelationAhead || !slices.Equal(c.OnlyA, []t.PatchHash{hA}) {
		tt.Fatalf("after alice's record: %+v", c)
	}
	if c := exchange.Compare(applied(bob), applied(alice)); c.Relation != exchange.RelationBehind {
		tt.Fatalf("swapped arguments must mirror: %v", c.Relation)
	}

	hB := rec(bob, "bob", "beta")
	c := compare()
	if c.Relation != exchange.RelationDiverged ||
		!slices.Equal(c.OnlyA, []t.PatchHash{hA}) || !slices.Equal(c.OnlyB, []t.PatchHash{hB}) {
		tt.Fatalf("after independent records: %+v", c)
	}

	// The join: pull each other's missing patches. Apply orders now
	// differ (alice holds [hA hB], bob [hB hA]) — the verdict must not
	// care.
	if _, err := exchange.Pull(ctx, alice, inproc.New(bob)); err != nil {
		tt.Fatal(err)
	}
	if _, err := exchange.Pull(ctx, bob, inproc.New(alice)); err != nil {
		tt.Fatal(err)
	}
	if !slices.Equal(applied(alice), []t.PatchHash{hA, hB}) || !slices.Equal(applied(bob), []t.PatchHash{hB, hA}) {
		tt.Fatalf("setup: expected order-divergent logs, got %v / %v", applied(alice), applied(bob))
	}
	if c := compare(); c.Relation != exchange.RelationEqual {
		tt.Fatalf("joined replicas: %+v", c)
	}

	// Unrecord shrinks bob's version: acting later in wall time, yet
	// strictly less advanced.
	if err := bob.Unrecord(ctx, hB); err != nil {
		tt.Fatal(err)
	}
	if c := compare(); c.Relation != exchange.RelationAhead || !slices.Equal(c.OnlyA, []t.PatchHash{hB}) {
		tt.Fatalf("after bob's unrecord: %+v", c)
	}
}
