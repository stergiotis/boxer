// Package exchangetest is the executable conformance contract for
// exchange transports. A transport author implements [exchange.PeerI]
// and [exchange.AcceptorI] over their carrier (NATS request-reply,
// HTTP, …) and passes a MakeFunc to Run; the suite drives real repos
// through Pull/Push across the transport and checks convergence,
// idempotence, dependency-rejection passthrough, and partial-failure
// accounting.
//
// The hard requirement beyond plumbing: sentinel classification must
// SURVIVE the carrier. A remote dependency rejection must arrive
// matching repo.ErrMissingDependency via errors.Is — map error codes
// back on the client edge of your transport.
package exchangetest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/envelope"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/exchange"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/patch"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo/filestore"
)

// MakeFunc exposes a repo through the transport under test. The
// returned endpoints must route to exactly this repo.
type MakeFunc func(tt *testing.T, r *repo.Repo) (exchange.PeerI, exchange.AcceptorI)

func newRepo(tt *testing.T, producer string) *repo.Repo {
	tt.Helper()
	st, err := filestore.Open(tt.TempDir())
	if err != nil {
		tt.Fatal(err)
	}
	reg, err := envelope.NewRegistry(envelope.JSONV1{})
	if err != nil {
		tt.Fatal(err)
	}
	tick := time.Unix(1_600_000_000, 0).UTC()
	r, err := repo.Open(context.Background(), repo.Options{
		Storage:  st,
		Codecs:   reg,
		Wire:     envelope.JSONV1Name,
		Producer: producer,
		Clock: func() time.Time {
			tick = tick.Add(time.Minute)
			return tick
		},
	})
	if err != nil {
		tt.Fatal(err)
	}
	tt.Cleanup(func() { _ = r.Close(context.Background()) })
	return r
}

func record(tt *testing.T, r *repo.Repo, anchor t.NodeID, line string) (t.PatchHash, t.NodeID) {
	tt.Helper()
	h, err := r.Record(context.Background(), "x", "add "+line, []patch.Change{{
		Kind:      patch.ChangeKindNewNode,
		NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: 0},
		Content:   []byte(line + "\n"),
		UpContext: []t.NodeID{anchor},
	}})
	if err != nil {
		tt.Fatal(err)
	}
	return h, t.NodeID{Patch: h, Index: 0}
}

func appliedOf(tt *testing.T, r *repo.Repo) []t.PatchHash {
	tt.Helper()
	hs, err := r.Applied(context.Background())
	if err != nil {
		tt.Fatal(err)
	}
	return hs
}

func sameApplied(a, b []t.PatchHash) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// CheckPullConvergence: a three-deep dependency chain pulls over the
// transport in one run and the applied logs converge.
func CheckPullConvergence(tt *testing.T, mk MakeFunc) (err error) {
	ctx := context.Background()
	src := newRepo(tt, "src")
	dst := newRepo(tt, "dst")
	_, n1 := record(tt, src, t.RootNodeID, "one")
	_, n2 := record(tt, src, n1, "two")
	record(tt, src, n2, "three")

	peer, _ := mk(tt, src)
	stats, err := exchange.Pull(ctx, dst, peer)
	if err != nil {
		return fmt.Errorf("pull: %w", err)
	}
	if stats.Missing != 3 || stats.Applied != 3 || stats.Duplicates != 0 {
		return fmt.Errorf("pull stats: %+v", stats)
	}
	if !sameApplied(appliedOf(tt, src), appliedOf(tt, dst)) {
		return fmt.Errorf("applied logs diverged after pull")
	}
	// Re-pull: nothing missing.
	stats, err = exchange.Pull(ctx, dst, peer)
	if err != nil || stats.Missing != 0 || stats.Shipped != 0 {
		return fmt.Errorf("re-pull: %+v, %v", stats, err)
	}
	return
}

// CheckPushConvergence: the symmetric direction.
func CheckPushConvergence(tt *testing.T, mk MakeFunc) (err error) {
	ctx := context.Background()
	src := newRepo(tt, "src")
	dst := newRepo(tt, "dst")
	_, n1 := record(tt, src, t.RootNodeID, "one")
	record(tt, src, n1, "two")

	peer, acc := mk(tt, dst)
	stats, err := exchange.Push(ctx, src, peer, acc)
	if err != nil {
		return fmt.Errorf("push: %w", err)
	}
	if stats.Missing != 2 || stats.Applied != 2 {
		return fmt.Errorf("push stats: %+v", stats)
	}
	if !sameApplied(appliedOf(tt, src), appliedOf(tt, dst)) {
		return fmt.Errorf("applied logs diverged after push")
	}
	return
}

// CheckDuplicateIdempotence: shipping an envelope the destination
// already holds reports applied=false through the transport, not an
// error.
func CheckDuplicateIdempotence(tt *testing.T, mk MakeFunc) (err error) {
	ctx := context.Background()
	src := newRepo(tt, "src")
	dst := newRepo(tt, "dst")
	h, _ := record(tt, src, t.RootNodeID, "one")
	framed, err := src.EncodedEnvelope(ctx, h)
	if err != nil {
		return err
	}
	_, acc := mk(tt, dst)
	if _, applied, aerr := acc.ApplyEnvelope(ctx, framed); aerr != nil || !applied {
		return fmt.Errorf("first apply: applied=%v err=%v", applied, aerr)
	}
	if _, applied, aerr := acc.ApplyEnvelope(ctx, framed); aerr != nil || applied {
		return fmt.Errorf("duplicate apply must be (false, nil): applied=%v err=%v", applied, aerr)
	}
	return
}

// CheckDependencyRejectionPassthrough: a dependent shipped before its
// dependency is rejected with repo.ErrMissingDependency — and that
// classification must survive the carrier.
func CheckDependencyRejectionPassthrough(tt *testing.T, mk MakeFunc) (err error) {
	ctx := context.Background()
	src := newRepo(tt, "src")
	dst := newRepo(tt, "dst")
	_, n1 := record(tt, src, t.RootNodeID, "one")
	h2, _ := record(tt, src, n1, "two")
	framed, err := src.EncodedEnvelope(ctx, h2)
	if err != nil {
		return err
	}
	_, acc := mk(tt, dst)
	_, _, aerr := acc.ApplyEnvelope(ctx, framed)
	if !errors.Is(aerr, repo.ErrMissingDependency) {
		return fmt.Errorf("dependency rejection lost its classification across the transport: %v", aerr)
	}
	return
}

// reorderedPeer lies about the apply order — the suite's vehicle for
// partial-failure accounting.
type reorderedPeer struct {
	exchange.PeerI
}

func (p reorderedPeer) Applied(ctx context.Context) (hs []t.PatchHash, err error) {
	hs, err = p.PeerI.Applied(ctx)
	if err != nil || len(hs) < 2 {
		return
	}
	hs = append([]t.PatchHash(nil), hs...)
	hs[0], hs[1] = hs[1], hs[0]
	return
}

func (p reorderedPeer) Envelopes(ctx context.Context, hs []t.PatchHash) ([][]byte, error) {
	return p.PeerI.Envelopes(ctx, hs)
}

// CheckPartialFailureStats: when the stream breaks mid-run (here: a
// peer announcing dependents before dependencies), Pull stops at the
// failure and Stats reflect exactly what landed.
func CheckPartialFailureStats(tt *testing.T, mk MakeFunc) (err error) {
	ctx := context.Background()
	src := newRepo(tt, "src")
	dst := newRepo(tt, "dst")
	_, n1 := record(tt, src, t.RootNodeID, "one")
	record(tt, src, n1, "two")

	peer, _ := mk(tt, src)
	stats, perr := exchange.Pull(ctx, dst, reorderedPeer{PeerI: peer})
	if !errors.Is(perr, repo.ErrMissingDependency) {
		return fmt.Errorf("expected dependency rejection, got %v", perr)
	}
	if stats.Missing != 2 || stats.Shipped != 1 || stats.Applied != 0 {
		return fmt.Errorf("partial-failure stats: %+v", stats)
	}
	// The destination is not wedged: a well-ordered pull completes.
	stats, perr = exchange.Pull(ctx, dst, peer)
	if perr != nil || stats.Applied != 2 {
		return fmt.Errorf("recovery pull: %+v, %v", stats, perr)
	}
	return
}

// Run executes the full conformance suite over the transport.
func Run(tt *testing.T, mk MakeFunc) {
	tt.Helper()
	checks := []struct {
		name  string
		check func(*testing.T, MakeFunc) error
	}{
		{"PullConvergence", CheckPullConvergence},
		{"PushConvergence", CheckPushConvergence},
		{"DuplicateIdempotence", CheckDuplicateIdempotence},
		{"DependencyRejectionPassthrough", CheckDependencyRejectionPassthrough},
		{"PartialFailureStats", CheckPartialFailureStats},
	}
	for _, c := range checks {
		tt.Run(c.name, func(tt *testing.T) {
			if err := c.check(tt, mk); err != nil {
				tt.Fatal(err)
			}
		})
	}
}
