package coveragebus

import (
	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/coverage/covsnap"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Consumer is the subscribing half of the coverage plane: it decodes each
// published update and hands it to Handler. The handler runs on whatever
// goroutine the bus dispatches on — under inprocbus that is the publisher's
// goroutine, synchronously — so it must not block.
type Consumer struct {
	bus     app.BusI
	subject string
	codec   Codec
	handler func(upd *covsnap.Update)
	log     zerolog.Logger

	unsubscribe func()
}

// ConsumerOptions configures NewConsumer. Bus, Subject, Codec, and Handler
// are required.
type ConsumerOptions struct {
	Bus     app.BusI
	Subject string
	Codec   Codec
	Handler func(upd *covsnap.Update)
	Log     zerolog.Logger
}

// NewConsumer validates opts and returns a Consumer that is not yet
// subscribed; call Start to subscribe.
func NewConsumer(opts ConsumerOptions) (inst *Consumer, err error) {
	if opts.Bus == nil {
		err = eh.Errorf("coveragebus: consumer needs a Bus")
		return
	}
	if opts.Subject == "" {
		err = eh.Errorf("coveragebus: consumer needs a Subject")
		return
	}
	if opts.Codec == nil {
		err = eh.Errorf("coveragebus: consumer needs a Codec")
		return
	}
	if opts.Handler == nil {
		err = eh.Errorf("coveragebus: consumer needs a Handler")
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
		upd, derr := inst.codec.Decode(msg.Payload)
		if derr != nil {
			inst.log.Warn().Err(derr).Str("subject", msg.Subject).Msg("coveragebus: decode error")
			return
		}
		inst.handler(upd)
	})
	if err != nil {
		err = eh.Errorf("coveragebus: consumer subscribe: %w", err)
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
