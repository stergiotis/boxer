package sysmetricsbus

import (
	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/planebus"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// Consumer is the subscribing half of the metric plane (ADR-0090 SD5): it
// subscribes to a subject, decodes each message, and hands the snapshot to
// Handler. It holds no metric state of its own — windowing, smoothing, and
// rendering live in the caller (imztop's Sampler).
//
// The handler runs on whatever goroutine the bus dispatches on. Under
// inprocbus that is the publisher's goroutine (synchronous dispatch), so a
// co-located producer tick runs the handler inline — behaviourally the same
// single-goroutine path imztop had before the bisection.
type Consumer struct {
	inner *planebus.Consumer[sysmsnap.BundleSnapshot]
}

// ConsumerOptions configures NewConsumer. Bus, Subject, Codec, and Handler
// are required.
type ConsumerOptions struct {
	Bus     app.BusI
	Subject string
	Codec   Codec
	Handler func(snap *sysmsnap.BundleSnapshot)
	Log     zerolog.Logger
}

// NewConsumer validates opts and returns a Consumer that is not yet
// subscribed; call Start to subscribe.
func NewConsumer(opts ConsumerOptions) (inst *Consumer, err error) {
	if opts.Handler == nil {
		err = eh.Errorf("sysmetricsbus: consumer needs a Handler")
		return
	}
	var codec planebus.CodecI[sysmsnap.BundleSnapshot]
	if opts.Codec != nil {
		codec = opts.Codec
	}
	handler := opts.Handler
	inner, err := planebus.NewConsumer(planebus.ConsumerOptions[sysmsnap.BundleSnapshot]{
		Bus:     opts.Bus,
		Subject: opts.Subject,
		Codec:   codec,
		Handler: func(_ string, snap *sysmsnap.BundleSnapshot) { handler(snap) },
		Log:     opts.Log,
	})
	if err != nil {
		err = eh.Errorf("sysmetricsbus: %w", err)
		return
	}
	inst = &Consumer{inner: inner}
	return
}

// Start subscribes to the subject. A decode failure on any message is
// logged and dropped — one corrupt frame must not tear down the stream.
func (inst *Consumer) Start() (err error) {
	err = inst.inner.Start()
	return
}

// Close unsubscribes. Safe to call when never started.
func (inst *Consumer) Close() (err error) {
	err = inst.inner.Close()
	return
}
