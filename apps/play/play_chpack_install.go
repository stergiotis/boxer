package play

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/semistructured/leeway/chpack"
)

// chpackInstallOnce gates the per-process pack reconcile: every window open
// runs the app factory, but the pack targets one server per process.
var chpackInstallOnce sync.Once

// installChPack reconciles the co/ragged function pack (ADR-0162 §SD5) on
// the configured ClickHouse endpoint, once per process and off the render
// path. Best-effort by design: on failure play still opens, and a query
// using a pack name fails server-side with an unknown-function error — an
// attributable signal — while the warning below records why.
func installChPack(url, user, password string, logger zerolog.Logger) {
	chpackInstallOnce.Do(func() {
		go func() {
			if url == "" {
				url = chclient.Defaults().URL
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			client := chclient.New(chclient.Config{URL: url, User: user, Password: password}, nil)
			err := chpack.Install(ctx, client)
			if err != nil {
				logger.Warn().Err(err).Str("url", url).Msg("play: leeway function pack install failed; co*/ragged* names will be missing on this server")
				return
			}
			logger.Info().Str("url", url).Int("version", chpack.Version).Msg("play: leeway function pack reconciled")
		}()
	})
}
