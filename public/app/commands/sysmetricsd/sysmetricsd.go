// Package sysmetricsd is the standalone system-metrics scraper service
// (ADR-0090 SD2/P3): the sole /proc reader, sampling the host through the
// sysmetrics collectors and publishing each per-tick BundleSnapshot one-way
// over NATS for any consumer. Running the scrape in its own process lets the
// GUI carrier keep the full ADR-0085 sandbox and hold no system-state
// capability.
//
// With `--tee` it also persists the plane into `boxer.facts` (ADR-0090 P5, as
// ADR-0184 settles it). That is off by default, so §SD4's "no ClickHouse
// instance enters the metric path" holds unless someone asks for history. The
// tee subscribes like any other consumer rather than forking the producer, so
// the scrape behaves identically whether or not it is on.
package sysmetricsd

import (
	"context"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/natsbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmscrape"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmtee"
	"github.com/stergiotis/boxer/public/keelson/runtime/topo"
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
			&cli.BoolFlag{
				Name:  "tee",
				Usage: "also persist the plane into boxer.facts (ADR-0184; off by default, so no ClickHouse enters the metric path unless asked)",
			},
			&cli.DurationFlag{
				Name:  "tee-flush-interval",
				Value: sysmtee.DefaultFlushInterval,
				Usage: "how long a sampled row may wait before becoming durable; also the window lost if the process dies",
			},
		},
		Action: run,
	}
}

// startTee wires the persistence tee onto the same bus the scraper publishes
// through (ADR-0184 §SD8). ClickHouse coordinates come from the CLICKHOUSE_*
// registry entries.
//
// The store is verified against the live table and never provisions it:
// `chstore` owns the boxer.facts DDL, and a schema mismatch is fatal here
// rather than a warning — rows written against a schema the store does not
// agree with decode as absent, which is indistinguishable from having written
// nothing.
func startTee(ctx context.Context, bus app.BusI, host string, flushInterval time.Duration) (stop func() error, err error) {
	cfg := chclient.ConfigFromEnv()
	client := chclient.New(cfg, nil)
	err = client.Ping(ctx)
	if err != nil {
		err = eh.Errorf("sysmetricsd: tee needs ClickHouse at %s: %w", cfg.URL, err)
		return
	}
	exec, err := storeexec.New(client, nil)
	if err != nil {
		err = eh.Errorf("sysmetricsd: tee executor: %w", err)
		return
	}
	store := sysmfacts.NewSysmetricsStore(exec, nil, sysmfacts.SysmetricsStoreConfig{})
	err = store.VerifySchema(ctx)
	if err != nil {
		store.Close()
		err = eh.Errorf("sysmetricsd: tee schema check against %s: %w", sysmfacts.SysmetricsTableName, err)
		return
	}
	tee, err := sysmtee.Start(sysmtee.Options{
		Bus:           bus,
		Store:         store,
		Host:          host,
		FlushInterval: flushInterval,
		Log:           log.Logger,
	})
	if err != nil {
		store.Close()
		err = eh.Errorf("sysmetricsd: %w", err)
		return
	}
	log.Info().Str("table", sysmfacts.SysmetricsTableName).Stringer("flushInterval", flushInterval).
		Msg("sysmetricsd: persistence tee running")
	stop = func() (serr error) {
		serr = tee.Stop()
		st := tee.Stats()
		log.Info().Uint64("bundles", st.Bundles).Uint64("rows", st.Rows).
			Uint64("flushed", st.Flushed).Uint64("dropped", st.Dropped).
			Uint64("flushErrors", st.FlushErrors).Msg("sysmetricsd: tee stopped")
		store.Close()
		return
	}
	return
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

	stopScraper, err := sysmscrape.StartScraper(ctx, client, host, c.Duration("interval"), log.Logger)
	if err != nil {
		_ = client.Close()
		return eh.Errorf("sysmetricsd: %w", err)
	}
	log.Info().Str("subject", sysmetricsbus.BundleSubject(host)).Stringer("interval", c.Duration("interval")).
		Str("component", topo.Self()).
		Msg("sysmetricsd: publishing system metrics over NATS")

	// Started after the scraper so a tee misconfiguration is reported against a
	// plane already known to work, and refused outright rather than leaving the
	// service running with persistence silently off.
	var stopTee func() error
	if c.Bool("tee") {
		stopTee, err = startTee(ctx, client, host, c.Duration("tee-flush-interval"))
		if err != nil {
			_ = stopScraper()
			_ = client.Close()
			return err
		}
	}
	<-ctx.Done()

	log.Info().Msg("sysmetricsd: shutting down")
	// The tee first: it drains and flushes what the scraper has already
	// published, and stopping the scraper under it would only shorten that.
	var serr error
	if stopTee != nil {
		serr = stopTee()
	}
	if scErr := stopScraper(); scErr != nil && serr == nil { // halts the loop and closes the bundle
		serr = scErr
	}
	if clErr := client.Close(); clErr != nil && serr == nil {
		serr = clErr
	}
	return serr
}
