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
	client, err := natsbus.Connect(natsbus.Options{URL: url, AppId: sysmetricsbus.ServiceAppId})
	if err != nil {
		return eh.Errorf("sysmetricsd: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	stopScraper, err := sysmetricsbus.StartScraper(ctx, client, host, c.Duration("interval"), log.Logger)
	if err != nil {
		_ = client.Close()
		return eh.Errorf("sysmetricsd: %w", err)
	}
	log.Info().Str("subject", sysmetricsbus.BundleSubject(host)).Stringer("interval", c.Duration("interval")).
		Msg("sysmetricsd: publishing system metrics over NATS")
	<-ctx.Done()

	log.Info().Msg("sysmetricsd: shutting down")
	serr := stopScraper() // halts the loop and closes the bundle
	if clErr := client.Close(); clErr != nil && serr == nil {
		serr = clErr
	}
	return serr
}
