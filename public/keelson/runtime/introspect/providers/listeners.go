package providers

import (
	"context"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/loopback"
)

// listenersProvider exposes the TCP listeners this process opened through
// loopback.Gate and has not closed (ADR-0082) as keelson.listeners: one row
// per listener with its name, bound address, what the address exposes it
// to (loopback, undeclared, declared, peer-tls) and, for a declared
// exposure, whom it serves and why.
type listenersProvider struct{}

func (listenersProvider) Name() string                         { return "listeners" }
func (listenersProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (listenersProvider) Schema() *arrow.Schema                { return listenersTable(nil).Schema() }

func (listenersProvider) Snapshot(ctx context.Context, proj introspect.Projection) (arrow.RecordBatch, error) {
	rows := loopback.OpenListeners()
	return listenersTable(rows).Build(proj, len(rows)), nil
}

func listenersTable(rows []loopback.ListenerInfo) *introspect.Table {
	return introspect.NewTable().
		String("name", func(i int) string { return rows[i].Name }).
		String("addr", func(i int) string { return rows[i].Addr }).
		String("exposure", func(i int) string { return rows[i].Exposure.String() }).
		String("serves", func(i int) string { return rows[i].Serves }).
		String("reason", func(i int) string { return rows[i].Reason })
}
