// Package exchange synchronizes repos over a pluggable transport. The
// transport seam is two small interfaces — [PeerI] (remote read side)
// and [AcceptorI] (remote write side) — that carriers map onto their
// own primitives (in-process calls, NATS request-reply, HTTP, …).
// Envelope bytes are opaque framed blobs (see envelope.Frame): the
// transport never decodes them, so wire-codec choice composes freely
// with carrier choice.
//
// v1 protocol: full applied-list exchange. The local side computes the
// set difference, fetches/ships the missing envelopes IN THE SENDER'S
// APPLY ORDER (dependencies precede dependents by the engine's log
// invariant), and applies them one by one. Duplicates are not errors
// (apply is idempotent); the first real error stops the run and the
// returned Stats describe what landed. Smarter reconciliation
// (frontiers, set sketches) is ADR-0079 OQ-1.
//
// Transport authors: errors crossing the carrier must preserve sentinel
// classification — at minimum, a dependency rejection on the remote
// side must arrive matching repo.ErrMissingDependency via errors.Is
// (map error codes back on the client edge). exchange/exchangetest is
// the executable contract.
package exchange

import (
	"context"

	"github.com/stergiotis/boxer/public/observability/eh"

	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo"
)

// PeerI is the read side of a remote repo.
type PeerI interface {
	// Applied returns the remote applied log in apply order.
	Applied(ctx context.Context) ([]t.PatchHash, error)
	// Envelopes returns the framed envelopes for the requested hashes,
	// in request order.
	Envelopes(ctx context.Context, hs []t.PatchHash) ([][]byte, error)
}

// AcceptorI is the write side of a remote repo. ApplyEnvelope mirrors
// repo.ApplyEnvelope semantics: idempotent, dependency-gated.
type AcceptorI interface {
	ApplyEnvelope(ctx context.Context, framed []byte) (h t.PatchHash, applied bool, err error)
}

// Stats describes one sync run.
type Stats struct {
	Missing    int // patches the source had and the destination lacked
	Shipped    int // envelopes transferred before stopping
	Applied    int // envelopes that newly applied
	Duplicates int // envelopes the destination already had
}

// Pull fetches everything from has that into lacks and applies it.
func Pull(ctx context.Context, into *repo.Repo, from PeerI) (stats Stats, err error) {
	theirs, err := from.Applied(ctx)
	if err != nil {
		return
	}
	ours, err := into.Applied(ctx)
	if err != nil {
		return
	}
	missing := minus(theirs, ours)
	stats.Missing = len(missing)
	if len(missing) == 0 {
		return
	}
	envs, err := from.Envelopes(ctx, missing)
	if err != nil {
		return
	}
	if len(envs) != len(missing) {
		err = eh.Errorf("peer returned %d envelopes for %d requested", len(envs), len(missing))
		return
	}
	for _, framed := range envs {
		if err = ctx.Err(); err != nil {
			return
		}
		stats.Shipped++
		_, applied, aerr := into.ApplyEnvelope(ctx, framed)
		if aerr != nil {
			err = aerr
			return
		}
		if applied {
			stats.Applied++
		} else {
			stats.Duplicates++
		}
	}
	return
}

// Push ships everything from has that the peer lacks. The peer's read
// side supplies its applied list; the acceptor takes the envelopes.
func Push(ctx context.Context, from *repo.Repo, peer PeerI, acc AcceptorI) (stats Stats, err error) {
	theirs, err := peer.Applied(ctx)
	if err != nil {
		return
	}
	ours, err := from.Applied(ctx)
	if err != nil {
		return
	}
	missing := minus(ours, theirs)
	stats.Missing = len(missing)
	for _, h := range missing {
		if err = ctx.Err(); err != nil {
			return
		}
		framed, gerr := from.EncodedEnvelope(ctx, h)
		if gerr != nil {
			err = gerr
			return
		}
		stats.Shipped++
		_, applied, aerr := acc.ApplyEnvelope(ctx, framed)
		if aerr != nil {
			err = aerr
			return
		}
		if applied {
			stats.Applied++
		} else {
			stats.Duplicates++
		}
	}
	return
}

// minus returns the elements of a not present in b, preserving a's order.
func minus(a, b []t.PatchHash) (out []t.PatchHash) {
	have := make(map[t.PatchHash]struct{}, len(b))
	for _, h := range b {
		have[h] = struct{}{}
	}
	for _, h := range a {
		if _, ok := have[h]; !ok {
			out = append(out, h)
		}
	}
	return
}
