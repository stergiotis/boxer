package sysmetricsbus

import (
	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/sysmetrics"
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
	bus     app.BusI
	subject string
	codec   Codec
	handler func(snap *sysmetrics.BundleSnapshot)
	log     zerolog.Logger

	unsubscribe func()
}

// ConsumerOptions configures NewConsumer. Bus, Subject, Codec, and Handler
// are required.
type ConsumerOptions struct {
	Bus     app.BusI
	Subject string
	Codec   Codec
	Handler func(snap *sysmetrics.BundleSnapshot)
	Log     zerolog.Logger
}

// NewConsumer validates opts and returns a Consumer that is not yet
// subscribed; call Start to subscribe.
func NewConsumer(opts ConsumerOptions) (inst *Consumer, err error) {
	if opts.Bus == nil {
		err = eh.Errorf("sysmetricsbus: consumer needs a Bus")
		return
	}
	if opts.Subject == "" {
		err = eh.Errorf("sysmetricsbus: consumer needs a Subject")
		return
	}
	if opts.Codec == nil {
		err = eh.Errorf("sysmetricsbus: consumer needs a Codec")
		return
	}
	if opts.Handler == nil {
		err = eh.Errorf("sysmetricsbus: consumer needs a Handler")
		return
	}
	inst = &Consumer{
		bus:     opts.Bus,
		subject: opts.Subject,
		codec:   opts.Codec,
		handler: opts.Handler,
		log:     opts.Log,
	}
	return
}

// Start subscribes to the subject. A decode failure on any message is
// logged and dropped — one corrupt frame must not tear down the stream.
func (inst *Consumer) Start() (err error) {
	unsub, err := inst.bus.Subscribe(inst.subject, func(msg *app.Msg) {
		snap, derr := inst.codec.Decode(msg.Payload)
		if derr != nil {
			inst.log.Warn().Err(derr).Str("subject", msg.Subject).Msg("sysmetricsbus: decode error")
			return
		}
		inst.handler(snap)
	})
	if err != nil {
		err = eh.Errorf("sysmetricsbus: consumer subscribe: %w", err)
		return
	}
	inst.unsubscribe = unsub
	return
}

// Close unsubscribes. Safe to call when never started.
func (inst *Consumer) Close() (err error) {
	if inst.unsubscribe != nil {
		inst.unsubscribe()
		inst.unsubscribe = nil
	}
	return
}
