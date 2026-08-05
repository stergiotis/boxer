// Package covscrape wires the concrete coverage sampler to the bus plane
// (ADR-0169 §SD4) — the sysmscrape analog. It is the sole importer of both
// the acquisition package and coveragebus, so consumers of either stay
// decoupled from the other.
package covscrape

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/coveragebus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
	"github.com/stergiotis/boxer/public/observability/coverage"
)

// DefaultHostToken is the coverage plane's host token — the metric plane's
// rule, reused so one box carries one token across both planes.
func DefaultHostToken() (token string) {
	return sysmetricsbus.DefaultHostToken()
}

// StartCoverageSampler constructs the live sampler and a publishing
// producer on bus, ticking every interval. On a binary without
// -cover -covermode=atomic, construction fails and the caller logs one
// line and idles — no build tag, the ADR-0169 §SD1 contract.
//
// The returned sampler is the constructor-injected source for the live
// introspection tables (§SD5); it stays owned by the producer, and stop
// closes both.
func StartCoverageSampler(ctx context.Context, bus app.BusI, hostToken string, interval time.Duration, logg zerolog.Logger) (sampler *coverage.Sampler, stop func() error, err error) {
	sampler, err = coverage.NewSampler(coverage.SamplerOptions{})
	if err != nil {
		return nil, nil, err
	}
	producer, err := coveragebus.NewProducer(coveragebus.ProducerOptions{
		Sampler:  sampler,
		Bus:      bus,
		Subject:  coveragebus.SampleSubject(hostToken),
		Codec:    coveragebus.NewCBORCodec(),
		Interval: interval,
		Log:      logg,
	})
	if err != nil {
		_ = sampler.Close()
		return nil, nil, err
	}
	producer.Start(ctx)
	logg.Info().Str("subject", coveragebus.SampleSubject(hostToken)).Dur("interval", interval).
		Msg("covscrape: coverage sampler started")
	stop = producer.Close
	return
}
