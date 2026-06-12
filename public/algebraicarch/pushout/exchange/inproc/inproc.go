// Package inproc adapts a local *repo.Repo to the exchange transport
// interfaces — the trivial carrier used by the demo, by tests, and by
// the exchangetest conformance suite. It is also the reference answer
// to "what must my transport do": exactly this, over a wire.
package inproc

import (
	"context"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/exchange"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo"
)

// Endpoint exposes a repo as both sides of the transport seam.
type Endpoint struct {
	r *repo.Repo
}

var (
	_ exchange.PeerI     = (*Endpoint)(nil)
	_ exchange.AcceptorI = (*Endpoint)(nil)
)

// New wraps a repo.
func New(r *repo.Repo) (ep *Endpoint) {
	ep = &Endpoint{r: r}
	return
}

func (inst *Endpoint) Applied(ctx context.Context) ([]t.PatchHash, error) {
	return inst.r.Applied(ctx)
}

func (inst *Endpoint) Envelopes(ctx context.Context, hs []t.PatchHash) (out [][]byte, err error) {
	out = make([][]byte, 0, len(hs))
	for _, h := range hs {
		framed, gerr := inst.r.EncodedEnvelope(ctx, h)
		if gerr != nil {
			err = gerr
			return
		}
		out = append(out, framed)
	}
	return
}

func (inst *Endpoint) ApplyEnvelope(ctx context.Context, framed []byte) (t.PatchHash, bool, error) {
	return inst.r.ApplyEnvelope(ctx, framed)
}
