// inproc validated by the transport conformance suite.
package inproc

import (
	"testing"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/exchange"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/exchange/exchangetest"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo"
)

func TestConformance(tt *testing.T) {
	exchangetest.Run(tt, func(tt *testing.T, r *repo.Repo) (exchange.PeerI, exchange.AcceptorI) {
		ep := New(r)
		return ep, ep
	})
}
