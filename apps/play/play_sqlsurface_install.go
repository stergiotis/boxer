package play

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
)

// surfaceInstallOnce gates the per-process surface reconcile: every window
// open runs the app factory, but the surface targets one server per process.
//
// The Once latches on the attempt, not the outcome, so a failed install is
// not retried for the life of the process. That is deliberate — retrying on
// a timer against an endpoint that just refused would hammer it — and it is
// why the failure has two visible recoveries: the Vocabulary tab shows what
// the endpoint is missing, and `leeway sqlsurface install` fixes it without
// restarting play.
var surfaceInstallOnce sync.Once

// surfaceInstallTimeout bounds the whole reconcile.
//
// ClickHouse's HTTP interface takes one query per request, and an install is
// the declared set plus the retired-name drops plus the probe and verify —
// getting on for a hundred sequential round trips. The 15s this inherited
// from the pack-only installer left under 200ms each and could not finish
// over a slow link; the CLI's own default for the same operation is two
// minutes, and this now matches it. Overshooting costs nothing: the work
// runs off the render path and play opens regardless.
const surfaceInstallTimeout = 2 * time.Minute

// installSQLSurface reconciles leeway's SQL read surface (ADR-0171 §SD2) on
// the configured ClickHouse endpoint, once per process and off the render
// path — all three families and the version marker, not the pack alone.
// Best-effort by design: on failure play still opens, and a query using a
// surface name fails server-side with an unknown-function error — an
// attributable signal — while the warning below records why.
//
// Reconcile is not called here, and deliberately not with ReconcileDrop:
// this runs unattended at startup against whatever endpoint is configured,
// which is the last place that should delete a function nobody asked it to.
// The Vocabulary panel reports the same drift where a human can see it.
func installSQLSurface(url, user, password string, logger zerolog.Logger) {
	surfaceInstallOnce.Do(func() {
		go func() {
			if url == "" {
				url = chclient.Defaults().URL
			}
			ctx, cancel := context.WithTimeout(context.Background(), surfaceInstallTimeout)
			defer cancel()
			client := chclient.New(chclient.Config{URL: url, User: user, Password: password}, nil)
			err := lwsqlsurface.Install(ctx, client)
			if err != nil {
				logger.Warn().Err(err).Str("url", url).Msg("play: leeway SQL surface install failed; LW_* names will be missing on this server")
				return
			}
			logger.Info().Str("url", url).Int("version", lwsqlsurface.Version).Msg("play: leeway SQL surface reconciled")
		}()
	})
}
