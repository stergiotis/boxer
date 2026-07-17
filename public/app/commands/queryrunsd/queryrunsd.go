// Package queryrunsd is the standalone query-run capture service
// (ADR-0115 S1): it serves the loopback /pull endpoint that the
// ClickHouse-owned refreshable materialized view reads every cadence,
// turning terminal system.query_log events into runtime.facts rows of
// kind QueryRun. The process holds no write authority — ClickHouse
// schedules the pull and performs the insert; stopping the daemon
// pauses capture, which catches up from the destination watermark on
// the next start (bounded by the query_log TTL).
package queryrunsd

import (
	"context"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/keelson/runtime/queryrunfacts"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryrunsvc"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// NewCliCommand returns the `queryrunsd` subcommand.
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "queryrunsd",
		Usage: "capture terminal system.query_log events into runtime.facts through the url()-pulled transform endpoint (ADR-0115 queryrunsd service)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "listen",
				Usage: "loopback bind address for /pull (default: $IMZERO2_QUERYRUNS_LISTEN, else 127.0.0.1:8127)",
			},
			&cli.StringFlag{
				Name:  "ch-url",
				Usage: "ClickHouse HTTP endpoint (default: $IMZERO2_QUERYRUNS_CH_URL, else http://localhost:8123/)",
			},
			&cli.DurationFlag{
				Name:  "cadence",
				Usage: "materialized-view refresh cadence, whole seconds (default: $IMZERO2_QUERYRUNS_CADENCE, else 5s)",
			},
			&cli.StringFlag{
				Name:  "scope",
				Usage: "capture scope: all | stamped | off (default: $IMZERO2_QUERYRUNS_SCOPE, else all)",
			},
		},
		Action: run,
	}
}

func run(c *cli.Context) (err error) {
	svc, err := queryrunsvc.New(queryrunsvc.Config{
		Listen:  c.String("listen"),
		ChURL:   c.String("ch-url"),
		Cadence: c.Duration("cadence"),
		Scope:   queryrunfacts.ScopeE(c.String("scope")),
	}, log.Logger)
	if err != nil {
		return eh.Errorf("queryrunsd: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	err = svc.Start(ctx)
	if err != nil {
		return eh.Errorf("queryrunsd: %w", err)
	}
	log.Info().Str("pull", svc.PullURL()).Str("mv", svc.MvName()).
		Msg("queryrunsd: capturing query runs")
	<-ctx.Done()

	log.Info().Msg("queryrunsd: shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = svc.Stop(shutdownCtx)
	return
}
