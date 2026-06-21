package sysmetricsbus

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// MinInterval and MaxInterval clamp the producer's tick period, matching
// the bounds imztop's Sampler enforced before the bisection.
const (
	MinInterval = 100 * time.Millisecond
	MaxInterval = 60 * time.Second
)

// DefaultInterval is the tick period when ProducerOptions.Interval is unset.
const DefaultInterval = 1 * time.Second

// BundleSampler is the producer's view of the metric source: anything that
// samples a [sysmsnap.BundleSnapshot] and can be closed. *sysmetrics.Bundle
// satisfies it. The interface keeps this package free of collector imports
// (ADR-0090 SD6) — the concrete Bundle is wired in the scraper-side sysmscrape
// package, so a consumer importing sysmetricsbus pulls in no /proc reader.
type BundleSampler interface {
	Sample(ctx context.Context) (snap sysmsnap.BundleSnapshot, err error)
	Close() (err error)
}

// Producer is the publishing half of the metric plane (ADR-0090 SD2/SD5): it
// owns the metric source (a [BundleSampler]), ticks at its configured cadence,
// encodes each BundleSnapshot, and publishes it. It is the sole sampler — the
// dataflow is one way, so there is no path from a consumer back here, and the
// cadence is fixed at construction.
type Producer struct {
	bundle   BundleSampler
	bus      app.BusI
	subject  string
	codec    Codec
	interval time.Duration
	log      zerolog.Logger

	cancel context.CancelFunc
	done   chan struct{}
}

// ProducerOptions configures NewProducer. Bundle, Bus, Subject, and Codec
// are required.
type ProducerOptions struct {
	Bundle   BundleSampler
	Bus      app.BusI
	Subject  string
	Codec    Codec
	Interval time.Duration
	Log      zerolog.Logger
}

// NewProducer validates opts and returns a stopped Producer; call Start to
// begin ticking.
func NewProducer(opts ProducerOptions) (inst *Producer, err error) {
	if opts.Bundle == nil {
		err = eh.Errorf("sysmetricsbus: producer needs a Bundle")
		return
	}
	if opts.Bus == nil {
		err = eh.Errorf("sysmetricsbus: producer needs a Bus")
		return
	}
	if opts.Subject == "" {
		err = eh.Errorf("sysmetricsbus: producer needs a Subject")
		return
	}
	if opts.Codec == nil {
		err = eh.Errorf("sysmetricsbus: producer needs a Codec")
		return
	}
	if opts.Interval <= 0 {
		opts.Interval = DefaultInterval
	}
	inst = &Producer{
		bundle:   opts.Bundle,
		bus:      opts.Bus,
		subject:  opts.Subject,
		codec:    opts.Codec,
		interval: clampInterval(opts.Interval),
		log:      opts.Log,
	}
	return
}

// Start launches the tick loop. The first sample is published immediately,
// then once per interval until ctx is cancelled or Close is called.
func (inst *Producer) Start(ctx context.Context) {
	runCtx, cancel := context.WithCancel(ctx)
	inst.cancel = cancel
	inst.done = make(chan struct{})
	go inst.loop(runCtx)
}

func (inst *Producer) loop(ctx context.Context) {
	defer close(inst.done)

	inst.tick(ctx)

	ticker := time.NewTicker(inst.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			inst.tick(ctx)
		}
	}
}

func (inst *Producer) tick(ctx context.Context) {
	snap, err := inst.bundle.Sample(ctx)
	if err != nil {
		if ctx.Err() == nil {
			inst.log.Warn().Err(err).Msg("sysmetricsbus: bundle sample error")
		}
		return
	}
	payload, err := inst.codec.Encode(&snap)
	if err != nil {
		inst.log.Warn().Err(err).Msg("sysmetricsbus: encode error")
		return
	}
	err = inst.bus.Publish(inst.subject, payload)
	if err != nil {
		inst.log.Warn().Err(err).Str("subject", inst.subject).Msg("sysmetricsbus: publish error")
	}
}

// Close stops the tick loop and closes the underlying Bundle (the producer
// owns it once handed over via ProducerOptions).
func (inst *Producer) Close() (err error) {
	if inst.cancel != nil {
		inst.cancel()
	}
	if inst.done != nil {
		<-inst.done
	}
	if inst.bundle != nil {
		err = inst.bundle.Close()
	}
	return
}

func clampInterval(d time.Duration) (out time.Duration) {
	out = min(max(d, MinInterval), MaxInterval)
	return
}
