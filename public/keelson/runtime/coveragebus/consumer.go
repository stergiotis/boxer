package coveragebus

import (
	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/planebus"
	"github.com/stergiotis/boxer/public/observability/coverage/covsnap"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Consumer is the subscribing half of the coverage plane: it decodes each
// published update and hands it to Handler. The handler runs on whatever
// goroutine the bus dispatches on — under inprocbus that is the publisher's
// goroutine, synchronously — so it must not block.
type Consumer struct {
	inner *planebus.Consumer[covsnap.Update]
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
	if opts.Handler == nil {
		err = eh.Errorf("coveragebus: consumer needs a Handler")
		return
	}
	var codec planebus.CodecI[covsnap.Update]
	if opts.Codec != nil {
		codec = opts.Codec
	}
	handler := opts.Handler
	inner, err := planebus.NewConsumer(planebus.ConsumerOptions[covsnap.Update]{
		Bus:     opts.Bus,
		Subject: opts.Subject,
		Codec:   codec,
		Handler: func(_ string, upd *covsnap.Update) { handler(upd) },
		Log:     opts.Log,
	})
	if err != nil {
		err = eh.Errorf("coveragebus: %w", err)
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
