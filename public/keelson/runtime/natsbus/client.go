package natsbus

import (
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// DefaultRequestTimeout bounds Request when Options.RequestTimeout is unset.
// Mirrors inprocbus.DefaultRequestTimeout.
const DefaultRequestTimeout = 5 * time.Second

// Client implements app.BusI over a NATS core connection. See the package
// doc for the authorization posture (server-side, not enforced here).
type Client struct {
	nc             *nats.Conn
	requestTimeout time.Duration
}

var _ app.BusI = (*Client)(nil)

// Close is the ADR-0188 §SD1 per-instance release: a host closes the client
// it minted for a window after that window's Unmount, and on this transport
// closing the connection drops every subscription with it.
var _ app.BusCloserI = (*Client)(nil)

// Options configures Connect.
type Options struct {
	// URL is the NATS server URL (e.g. "nats://127.0.0.1:4222"). Empty uses
	// nats.DefaultURL.
	URL string
	// AppId is the bus identity; carried as the NATS connection name so the
	// server and monitoring can attribute traffic to it.
	AppId app.AppIdT
	// RequestTimeout bounds Request; 0 uses DefaultRequestTimeout.
	RequestTimeout time.Duration
	// ConnectOptions pass through to nats.Connect (TLS, credentials, reconnect
	// tuning, …). Applied after the connection-name option.
	ConnectOptions []nats.Option
}

// Connect dials the NATS server and returns a Client.
func Connect(opts Options) (inst *Client, err error) {
	if opts.URL == "" {
		opts.URL = nats.DefaultURL
	}
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = DefaultRequestTimeout
	}
	natsOpts := make([]nats.Option, 0, len(opts.ConnectOptions)+1)
	natsOpts = append(natsOpts, nats.Name(string(opts.AppId)))
	natsOpts = append(natsOpts, opts.ConnectOptions...)
	nc, cerr := nats.Connect(opts.URL, natsOpts...)
	if cerr != nil {
		err = eb.Build().Str("url", opts.URL).Errorf("natsbus: connect: %w", cerr)
		return
	}
	inst = &Client{nc: nc, requestTimeout: opts.RequestTimeout}
	return
}

func (inst *Client) Publish(subject string, payload []byte) (err error) {
	err = inst.nc.Publish(subject, payload)
	if err != nil {
		err = eb.Build().Str("subject", subject).Errorf("natsbus: publish: %w", err)
	}
	return
}

func (inst *Client) Subscribe(subject string, handler app.MsgHandlerFunc) (unsubscribe func(), err error) {
	// nats.go delivers a given async subscription's messages serially on one
	// goroutine, so a single subscription's handler stays a single writer —
	// the same invariant inprocbus's synchronous dispatch gives.
	sub, serr := inst.nc.Subscribe(subject, func(m *nats.Msg) {
		handler(&app.Msg{
			Subject: m.Subject,
			Reply:   m.Reply,
			Payload: m.Data,
		})
	})
	if serr != nil {
		err = eb.Build().Str("subject", subject).Errorf("natsbus: subscribe: %w", serr)
		return
	}
	unsubscribe = func() { _ = sub.Unsubscribe() }
	return
}

func (inst *Client) Request(subject string, payload []byte) (reply []byte, err error) {
	return inst.RequestWithTimeout(subject, payload, 0)
}

// RequestWithTimeout waits d rather than the connection's configured default.
// A non-positive d means "the default", which is what Request passes.
func (inst *Client) RequestWithTimeout(subject string, payload []byte, d time.Duration) (reply []byte, err error) {
	if d <= 0 {
		d = inst.requestTimeout
	}
	m, rerr := inst.nc.Request(subject, payload, d)
	if rerr != nil {
		err = eb.Build().Str("subject", subject).Errorf("natsbus: request: %w", rerr)
		return
	}
	reply = m.Data
	return
}

// Flush blocks until the server has processed all buffered publishes and
// subscriptions. Not part of app.BusI; it makes a Subscribe visible before a
// subsequent Publish (NATS core drops messages that arrive with no
// subscriber), which co-located inprocbus never needs.
func (inst *Client) Flush() (err error) {
	err = inst.nc.Flush()
	if err != nil {
		err = eh.Errorf("natsbus: flush: %w", err)
	}
	return
}

// Close flushes buffered messages and closes the connection.
func (inst *Client) Close() (err error) {
	if inst.nc == nil || inst.nc.IsClosed() {
		return
	}
	ferr := inst.nc.Flush()
	inst.nc.Close()
	if ferr != nil {
		err = eh.Errorf("natsbus: flush on close: %w", ferr)
	}
	return
}
