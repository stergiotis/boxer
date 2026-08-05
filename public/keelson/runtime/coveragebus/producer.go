package coveragebus

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/coverage/covsnap"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// MinInterval and MaxInterval clamp the producer's tick period. Coverage
// churn is slow and each tick serializes the process's counter arrays, so
// the floor sits well above the metric plane's.
const (
	MinInterval = 1 * time.Second
	MaxInterval = 10 * time.Minute
)

// DefaultInterval is the tick period when ProducerOptions.Interval is unset.
const DefaultInterval = 5 * time.Second

// UpdateSampler is the producer's view of the coverage source: anything
// that folds one sample into a pre-aggregated update and can be closed.
// *coverage.Sampler satisfies it. The interface keeps this package free of
// runtime/coverage imports — the concrete sampler is wired in covscrape.
type UpdateSampler interface {
	Sample() (upd *covsnap.Update, err error)
	Close() (err error)
}

// Producer is the publishing half of the coverage plane (ADR-0169 §SD4):
// it owns the sampler, ticks at its configured cadence, encodes each
// update, and publishes it. Deltas ride fire-and-forget Publish; the
// sampler's periodic full re-statements are what heal a consumer that
// missed one.
type Producer struct {
	sampler  UpdateSampler
	bus      app.BusI
	subject  string
	codec    Codec
	interval time.Duration
	log      zerolog.Logger

	cancel context.CancelFunc
	done   chan struct{}
}

// ProducerOptions configures NewProducer. Sampler, Bus, Subject, and Codec
// are required.
type ProducerOptions struct {
	Sampler  UpdateSampler
	Bus      app.BusI
	Subject  string
	Codec    Codec
	Interval time.Duration
	Log      zerolog.Logger
}

// NewProducer validates opts and returns a stopped Producer; call Start to
// begin ticking.
func NewProducer(opts ProducerOptions) (inst *Producer, err error) {
	if opts.Sampler == nil {
		err = eh.Errorf("coveragebus: producer needs a Sampler")
		return
	}
	if opts.Bus == nil {
		err = eh.Errorf("coveragebus: producer needs a Bus")
		return
	}
	if opts.Subject == "" {
		err = eh.Errorf("coveragebus: producer needs a Subject")
		return
	}
	if opts.Codec == nil {
		err = eh.Errorf("coveragebus: producer needs a Codec")
		return
	}
	if opts.Interval <= 0 {
		opts.Interval = DefaultInterval
	}
	inst = &Producer{
		sampler:  opts.Sampler,
		bus:      opts.Bus,
		subject:  opts.Subject,
		codec:    opts.Codec,
		interval: clampInterval(opts.Interval),
		log:      opts.Log,
	}
	return
}

// Start launches the tick loop. The first sample is published immediately
// (the sampler's first update is a full statement by contract), then once
// per interval until ctx is cancelled or Close is called.
func (inst *Producer) Start(ctx context.Context) {
	runCtx, cancel := context.WithCancel(ctx)
	inst.cancel = cancel
	inst.done = make(chan struct{})
	go inst.loop(runCtx)
}

func (inst *Producer) loop(ctx context.Context) {
	defer close(inst.done)

	inst.tick()

	ticker := time.NewTicker(inst.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			inst.tick()
		}
	}
}

func (inst *Producer) tick() {
	upd, err := inst.sampler.Sample()
	if err != nil {
		inst.log.Warn().Err(err).Msg("coveragebus: sample error")
		return
	}
	payload, err := inst.codec.Encode(upd)
	if err != nil {
		inst.log.Warn().Err(err).Msg("coveragebus: encode error")
		return
	}
	err = inst.bus.Publish(inst.subject, payload)
	if err != nil {
		inst.log.Warn().Err(err).Str("subject", inst.subject).Msg("coveragebus: publish error")
	}
}

// Close stops the tick loop and closes the underlying sampler (the
// producer owns it once handed over via ProducerOptions).
func (inst *Producer) Close() (err error) {
	if inst.cancel != nil {
		inst.cancel()
	}
	if inst.done != nil {
		<-inst.done
	}
	if inst.sampler != nil {
		err = inst.sampler.Close()
	}
	return
}

func clampInterval(d time.Duration) (out time.Duration) {
	out = min(max(d, MinInterval), MaxInterval)
	return
}
