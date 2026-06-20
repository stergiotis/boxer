package natsbus

import (
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// Provider is an app.BusProvider that mints a per-app NATS-backed BusI. Each
// app gets its own connection (ADR-0026 §SD4: per-app NKey/JWT), named after
// its AppId. The host (windowhost.SetBus) holds one Provider for the
// deployment's NATS server; apps consume MountCtx.Bus() and never see the URL.
//
// Lifecycle caveat: NewBusClient opens a connection per call and the host does
// not yet close per-app bus clients on Unmount, so a long session that opens
// and closes many windows would leak connections. Closing on Unmount is part
// of the remaining M4 host work; harmless co-located (inprocbus) and for the
// single-carrier demo where windows are long-lived.
type Provider struct {
	url            string
	requestTimeout time.Duration
	connectOptions []nats.Option
}

var _ app.BusProvider = (*Provider)(nil)

// ProviderOptions configures NewProvider.
type ProviderOptions struct {
	URL            string
	RequestTimeout time.Duration
	ConnectOptions []nats.Option
}

// NewProvider returns a Provider for the given NATS server.
func NewProvider(opts ProviderOptions) (inst *Provider) {
	inst = &Provider{
		url:            opts.URL,
		requestTimeout: opts.RequestTimeout,
		connectOptions: opts.ConnectOptions,
	}
	return
}

// NewBusClient dials a fresh connection for appId. caps are advisory here —
// NATS enforces subject permissions server-side (ADR-0026 §SD4).
func (inst *Provider) NewBusClient(appId app.AppIdT, _ []app.SubjectFilter) (bus app.BusI, err error) {
	c, err := Connect(Options{
		URL:            inst.url,
		AppId:          appId,
		RequestTimeout: inst.requestTimeout,
		ConnectOptions: inst.connectOptions,
	})
	if err != nil {
		return
	}
	bus = c
	return
}
