package sysmscrape

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/sysmetrics"
)

// StartScraper builds the default collector bundle and runs a sysmetricsbus
// Producer that publishes to bus under the bundle subject at the given cadence;
// it returns a stop func that halts the loop and closes the bundle.
//
// It is the one place the producer (the /proc reader) is wired, so consumers
// like imztop can stay pure subscribers (ADR-0090). Callers supply the
// transport via bus: the standalone sysmetricsd command passes a natsbus
// client; co-located contexts (the carousel host, the screenshot tour, tests)
// pass an inprocbus client minted with a sysmetrics.> publish cap. hostToken
// scopes the subject (BundleSubject); interval ≤ 0 uses
// sysmetricsbus.DefaultInterval.
//
// GPU note: DefaultBundleOptions wires the vendor-build-tag GPU collector, so
// GPU rides here too on a tagged build.
func StartScraper(ctx context.Context, bus app.BusI, hostToken string, interval time.Duration, logg zerolog.Logger) (stop func() (err error), err error) {
	if bus == nil {
		err = eh.Errorf("sysmscrape: scraper needs a bus")
		return
	}
	bopts, boptsErr := sysmetrics.DefaultBundleOptions()
	if boptsErr != nil {
		err = eh.Errorf("sysmscrape: scraper: %w", boptsErr)
		return
	}
	bundle, bErr := sysmetrics.NewBundle(bopts)
	if bErr != nil {
		err = eh.Errorf("sysmscrape: scraper: build bundle: %w", bErr)
		return
	}
	producer, pErr := sysmetricsbus.NewProducer(sysmetricsbus.ProducerOptions{
		Bundle:   bundle,
		Bus:      bus,
		Subject:  sysmetricsbus.BundleSubject(hostToken),
		Codec:    sysmetricsbus.NewCBORCodec(),
		Interval: interval,
		Log:      logg,
	})
	if pErr != nil {
		_ = bundle.Close()
		err = eh.Errorf("sysmscrape: scraper: %w", pErr)
		return
	}
	producer.Start(ctx)
	stop = producer.Close // halts the tick loop and closes the bundle
	return
}
