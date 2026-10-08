// Package queryrunsd is the standalone query-run capture service
// (ADR-0115 S1): it serves the loopback /pull endpoint that the
// ClickHouse-owned refreshable materialized view reads every cadence,
// turning terminal system.query_log events into boxer.facts rows of
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
	"github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/clickhouseenv"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryrunsvc"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// NewCliCommand returns the `queryrunsd` subcommand. Each flag is the CLI
// face of a registry entry, so a flag, its environment variable and its
// default resolve in one place and queryrunsvc.New reads them all; --ch-url
// is the shared CLICKHOUSE_ENDPOINT, which credentials accompany through
// CLICKHOUSE_USER / CLICKHOUSE_PASSWORD.
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "queryrunsd",
		Usage: "capture terminal system.query_log events into boxer.facts through the url()-pulled transform endpoint (ADR-0115 queryrunsd service)",
		Flags: []cli.Flag{
			queryrunsvc.ListenAddr.AsCliFlag(),
			clickhouseenv.Endpoint.AsCliFlag(env.WithCliFlagName("ch-url")),
			queryrunsvc.Cadence.AsCliFlag(),
			queryrunsvc.Scope.AsCliFlag(),
			queryrunsvc.Backfill.AsCliFlag(),
		},
		Action: run,
	}
}

func run(_ context.Context, _ *cli.Command) (err error) {
	svc, err := queryrunsvc.New(queryrunsvc.Config{}, log.Logger)
	if err != nil {
		return eh.Errorf("queryrunsd: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	err = svc.Start(ctx)
	if err != nil {
		return eh.Errorf("queryrunsd: %w", err)
	}
	// A dead endpoint ends the process rather than leaving it alive and
	// deaf: the unit's Restart=always is what recovers it.
	select {
	case <-ctx.Done():
	case err = <-svc.Failed():
		return
	}

	log.Info().Msg("queryrunsd: shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = svc.Stop(shutdownCtx)
	return
}
