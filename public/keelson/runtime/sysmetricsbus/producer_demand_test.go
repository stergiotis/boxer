package sysmetricsbus

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

type countingSampler struct{ samples atomic.Int32 }

func (inst *countingSampler) Sample(context.Context) (snap sysmsnap.BundleSnapshot, err error) {
	inst.samples.Add(1)
	return
}

func (inst *countingSampler) Close() (err error) { return }

// TestProducer_DemandGatesSampling drives tick directly through the three
// demand states: no demand samples nothing; demand after a pause samples
// once without publishing; continued demand samples and publishes.
func TestProducer_DemandGatesSampling(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	const subject = "sysmetrics.test.bundle"
	pub := bus.NewClient("test.pub", []app.SubjectFilter{{Pattern: subject, Direction: app.CapDirectionPub}})
	sub := bus.NewClient("test.sub", []app.SubjectFilter{{Pattern: subject, Direction: app.CapDirectionSub}})
	var published atomic.Int32
	unsub, err := sub.Subscribe(subject, func(*app.Msg) { published.Add(1) })
	require.NoError(t, err)
	defer unsub()

	sampler := &countingSampler{}
	var want atomic.Bool
	p, err := NewProducer(ProducerOptions{
		Bundle: sampler, Bus: pub, Subject: subject, Codec: NewCBORCodec(),
		Demand: want.Load, Log: zerolog.Nop(),
	})
	require.NoError(t, err)
	ctx := context.Background()

	want.Store(true)
	p.tick(ctx)
	require.EqualValues(t, 1, sampler.samples.Load(), "demand from the start samples")
	require.EqualValues(t, 1, published.Load(), "and publishes: there was no pause to span")

	want.Store(false)
	p.tick(ctx)
	p.tick(ctx)
	require.EqualValues(t, 1, sampler.samples.Load(), "no demand samples nothing")
	require.EqualValues(t, 1, published.Load())

	want.Store(true)
	p.tick(ctx)
	require.EqualValues(t, 2, sampler.samples.Load(), "demand after a pause primes")
	require.EqualValues(t, 1, published.Load(), "a priming sample is not published")

	p.tick(ctx)
	require.EqualValues(t, 3, sampler.samples.Load())
	require.EqualValues(t, 2, published.Load(), "the tick after priming publishes")
}

// TestProducer_NoDemandProbeSamplesEveryTick keeps the fixed cadence for a
// producer constructed without a probe.
func TestProducer_NoDemandProbeSamplesEveryTick(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	const subject = "sysmetrics.test.bundle"
	pub := bus.NewClient("test.pub", []app.SubjectFilter{{Pattern: subject, Direction: app.CapDirectionPub}})
	sampler := &countingSampler{}
	p, err := NewProducer(ProducerOptions{
		Bundle: sampler, Bus: pub, Subject: subject, Codec: NewCBORCodec(), Log: zerolog.Nop(),
	})
	require.NoError(t, err)
	p.tick(context.Background())
	p.tick(context.Background())
	require.EqualValues(t, 2, sampler.samples.Load())
}
