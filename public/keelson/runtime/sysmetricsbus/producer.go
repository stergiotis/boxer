package sysmetricsbus

import (
	"context"
	"sync/atomic"
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

// Producer is the publishing half of the metric plane (ADR-0090 SD2/SD5):
// it owns the sysmetrics.Bundle, ticks at its configured cadence, encodes
// each BundleSnapshot, and publishes it. It is the sole sampler — the
// dataflow is one way, so there is no path from a consumer back here; Pause
// and SetInterval are local controls the co-located owner drives directly.
type Producer struct {
	bundle  BundleSampler
	bus     app.BusI
	subject string
	codec   Codec
	log     zerolog.Logger

	intervalNs atomic.Int64
	paused     atomic.Bool

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
		bundle:  opts.Bundle,
		bus:     opts.Bus,
		subject: opts.Subject,
		codec:   opts.Codec,
		log:     opts.Log,
	}
	inst.intervalNs.Store(int64(clampInterval(opts.Interval)))
	return
}

// Start launches the tick loop. The first sample is published immediately,
// then once per Interval until ctx is cancelled or Close is called.
func (inst *Producer) Start(ctx context.Context) {
	runCtx, cancel := context.WithCancel(ctx)
	inst.cancel = cancel
	inst.done = make(chan struct{})
	go inst.loop(runCtx)
}

func (inst *Producer) loop(ctx context.Context) {
	defer close(inst.done)

	inst.tick(ctx)

	cur := time.Duration(inst.intervalNs.Load())
	ticker := time.NewTicker(cur)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			inst.tick(ctx)
			next := time.Duration(inst.intervalNs.Load())
			if next != cur {
				ticker.Reset(next)
				cur = next
			}
		}
	}
}

func (inst *Producer) tick(ctx context.Context) {
	// Pause short-circuits before Sample: the collectors walk /proc, /sys,
	// etc., and a paused producer should spend no CPU (ADR-0020 SD14).
	if inst.paused.Load() {
		return
	}
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

// Pause stops (p=true) or resumes (p=false) sampling. While paused the
// producer emits nothing, so consumers keep their last snapshot.
func (inst *Producer) Pause(p bool) { inst.paused.Store(p) }

// IsPaused reports the current pause state.
func (inst *Producer) IsPaused() (p bool) { return inst.paused.Load() }

// SetInterval changes the tick period, clamped to [MinInterval, MaxInterval].
// The new period takes effect after the current ticker fires.
func (inst *Producer) SetInterval(d time.Duration) {
	inst.intervalNs.Store(int64(clampInterval(d)))
}

// Interval returns the current tick period.
func (inst *Producer) Interval() (d time.Duration) {
	return time.Duration(inst.intervalNs.Load())
}

// IntervalLabel returns the tick period as a short human-readable string.
func (inst *Producer) IntervalLabel() (out string) {
	return time.Duration(inst.intervalNs.Load()).String()
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
