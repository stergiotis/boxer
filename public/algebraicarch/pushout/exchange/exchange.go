// Package exchange synchronizes repos over a pluggable transport. The
// transport seam is two small interfaces — [PeerI] (remote read side)
// and [AcceptorI] (remote write side) — that carriers map onto their
// own primitives (in-process calls, NATS request-reply, HTTP, …).
// Envelope bytes are opaque framed blobs (see envelope.Frame): the
// transport never decodes them, so wire-codec choice composes freely
// with carrier choice.
//
// v1 protocol: full applied-list exchange. The local side computes the
// set difference, fetches/ships the missing envelopes, and applies them
// as ONE batch through repo.ApplyEnvelopes (ADR-0221): the batch is
// sorted by dependency on the applying side, so the carrier owes no
// delivery order, and the whole batch is one storage write. Duplicates
// are not errors (apply is idempotent). An envelope whose dependency
// the peer never announced is reported pending, and the run ends with
// repo.ErrMissingDependency and Stats describing what landed. Smarter
// reconciliation (frontiers, set sketches) is ADR-0079 OQ-1;
// doc/explanation/pushout-distributed-operation.md maps that design
// space and the deployment topologies.
//
// Transport authors: errors crossing the carrier must preserve sentinel
// classification — at minimum, a dependency rejection on the remote
// side must arrive matching repo.ErrMissingDependency via errors.Is
// (map error codes back on the client edge). exchange/exchangetest is
// the executable contract.
package exchange

import (
	"context"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/types"
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
// ApplyEnvelopes mirrors repo.ApplyEnvelopes: any order, one batch,
// pending reported in the BatchReport rather than as an error — a
// transport must carry the report back intact (Applied, Duplicates and
// Pending with their Missing lists). Push uses the batch verb; the
// single verb remains for transports and callers that ship one at a
// time.
type AcceptorI interface {
	ApplyEnvelope(ctx context.Context, framed []byte) (h t.PatchHash, applied bool, err error)
	ApplyEnvelopes(ctx context.Context, framed [][]byte) (report repo.BatchReport, err error)
}

// Stats describes one sync run.
type Stats struct {
	Missing    int // patches the source had and the destination lacked
	Shipped    int // envelopes transferred before stopping
	Applied    int // envelopes that newly applied
	Duplicates int // envelopes the destination already had
	Pending    int // envelopes left unapplied for want of a dependency the peer never announced
}

// account folds a batch report into stats and turns a pending
// remainder into the dependency-rejection sentinel, so a transport or
// caller that inspected errors.Is(err, repo.ErrMissingDependency)
// before the batch verb still does.
func account(stats *Stats, report repo.BatchReport) (err error) {
	stats.Applied += len(report.Applied)
	stats.Duplicates += len(report.Duplicates)
	stats.Pending += len(report.Pending)
	if len(report.Pending) > 0 {
		first := report.Pending[0]
		b := eb.Build().Stringer("patchHash", first.Hash).Int("pending", len(report.Pending))
		if len(first.Missing) > 0 {
			b = b.Stringer("dep", first.Missing[0])
		}
		err = b.Errorf("the peer shipped a patch whose dependency it never announced: %w", repo.ErrMissingDependency)
	}
	return
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
		err = eb.Build().Int("returned", len(envs)).Int("requested", len(missing)).Errorf("peer returned a different envelope count than requested")
		return
	}
	stats.Shipped = len(envs)
	report, err := into.ApplyEnvelopes(ctx, envs)
	if err != nil {
		return
	}
	err = account(&stats, report)
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
	if len(missing) == 0 {
		return
	}
	envs, err := from.EncodedEnvelopes(ctx, missing)
	if err != nil {
		return
	}
	stats.Shipped = len(envs)
	report, err := acc.ApplyEnvelopes(ctx, envs)
	if err != nil {
		return
	}
	err = account(&stats, report)
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
