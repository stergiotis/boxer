// Package sysmetricsd is the standalone system-metrics scraper service
// (ADR-0090 SD2/P3): the sole /proc reader, sampling the host through the
// sysmetrics collectors and publishing each per-tick BundleSnapshot one-way
// over NATS for any consumer (imztop, a future persistence tee, …). Running
// the scrape in its own process lets the GUI carrier keep the full ADR-0085
// sandbox and hold no system-state capability.
//
// Not yet wired into the boxer CLI dispatch (public/app/main.go): expose it
// there with `sysmetricsd.NewCliCommand()` once that file is free of other
// in-flight edits.
package sysmetricsd

import (
	"context"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/natsbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/sysmetrics"
	"github.com/urfave/cli/v2"
)

// NewCliCommand returns the `sysmetricsd` subcommand.
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "sysmetricsd",
		Usage: "scrape system metrics from /proc and publish them over NATS (ADR-0090 scraper service)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "url",
				Usage: "NATS server URL (default: $IMZERO2_SYSMETRICS_NATS_URL, else nats://127.0.0.1:4222)",
			},
			&cli.StringFlag{
				Name:  "host",
				Usage: "host token for the sysmetrics.{host}.bundle subject (default: sanitised hostname)",
			},
			&cli.DurationFlag{
				Name:  "interval",
				Value: time.Second,
				Usage: "sampling/publish cadence",
			},
		},
		Action: run,
	}
}

func run(c *cli.Context) (err error) {
	url := c.String("url")
	if url == "" {
		url = sysmetricsbus.NatsURL.Get() // empty here falls through to nats.DefaultURL in Connect
	}

	host := sysmetricsbus.DefaultHostToken()
	if h := c.String("host"); h != "" {
		host = sysmetricsbus.HostToken(h)
	}
	subject := sysmetricsbus.BundleSubject(host)

	bopts, err := sysmetrics.DefaultBundleOptions()
	if err != nil {
		return eh.Errorf("sysmetricsd: %w", err)
	}
	bundle, err := sysmetrics.NewBundle(bopts)
	if err != nil {
		return eh.Errorf("sysmetricsd: build bundle: %w", err)
	}

	client, err := natsbus.Connect(natsbus.Options{URL: url, AppId: sysmetricsbus.ServiceAppId})
	if err != nil {
		_ = bundle.Close()
		return eh.Errorf("sysmetricsd: %w", err)
	}

	producer, err := sysmetricsbus.NewProducer(sysmetricsbus.ProducerOptions{
		Bundle:   bundle,
		Bus:      client,
		Subject:  subject,
		Codec:    sysmetricsbus.NewCBORCodec(),
		Interval: c.Duration("interval"),
		Log:      log.Logger,
	})
	if err != nil {
		_ = bundle.Close()
		_ = client.Close()
		return eh.Errorf("sysmetricsd: build producer: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Info().Str("subject", subject).Stringer("interval", c.Duration("interval")).
		Msg("sysmetricsd: publishing system metrics over NATS")
	producer.Start(ctx)
	<-ctx.Done()

	log.Info().Msg("sysmetricsd: shutting down")
	cerr := producer.Close() // stops the loop and closes the bundle it owns
	if clErr := client.Close(); clErr != nil && cerr == nil {
		cerr = clErr
	}
	return cerr
}
