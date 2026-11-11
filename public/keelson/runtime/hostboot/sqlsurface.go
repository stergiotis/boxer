package hostboot

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"

	// The trail views and the journal views register as dependents of the
	// surface install (lwsqlsurface.RegisterDependent); linking them here is
	// what makes the boot reconcile create them (ADR-0277 and ADR-0308,
	// updates of 2026-10-13).
	_ "github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore/journalviews"
	_ "github.com/stergiotis/boxer/public/keelson/runtime/trail/trailviews"
)

// sqlSurfaceTimeout bounds the boot reconcile, which on a drifted server is
// getting on for a hundred sequential round trips, as play's reconcile
// (which this matches).
const sqlSurfaceTimeout = 2 * time.Minute

// installSQLSurface reconciles the read surface and its dependents on the
// facts server, in the background: boot does not wait, and a failure leaves
// a warning and the server as it was. No drop is asked for — this runs
// unattended against whatever endpoint is configured.
//
// The function families themselves are already there when this runs: the
// facts store's setup installs them, because its reads need them
// (chstore.Store.EnsureReadSurface). A start against a server that already
// carries this build's surface and dependents replaces nothing
// (lwsqlsurface.InstallIfDrifted).
func installSQLSurface(cfg chstore.Config, logger zerolog.Logger) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), sqlSurfaceTimeout)
		defer cancel()
		client := chclient.New(chclient.Config{URL: cfg.URL, User: cfg.User, Password: cfg.Password}, nil)
		installed, err := lwsqlsurface.InstallIfDrifted(ctx, client)
		if err != nil {
			logger.Warn().Err(err).Str("url", cfg.URL).Msg("hostboot: leeway SQL surface install failed; the trail views may be missing or stale on this server")
			return
		}
		logger.Info().Str("url", cfg.URL).Int("version", lwsqlsurface.Version).Bool("installed", installed).Msg("hostboot: leeway SQL surface and its dependent views reconciled")
	}()
}
