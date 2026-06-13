package chstore

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
)

// NewWithFallback returns the strongest FactsStoreI that responds: a
// live-CH Store when the configured server is reachable within
// pingTimeout, or an InMemoryFactsStore otherwise. The chosen backend
// is logged at Info; transient connectivity issues that flip to memory
// are surfaced at Warn (under the supplied logger's run_id context).
//
// Intended for the runtime entry-point — the carousel calls this once
// at start, hands the result to the DockHost, and never reconfigures.
// If the user wants stricter behaviour (e.g. fail-fast on missing CH),
// they can construct a Store directly and skip this helper.
func NewWithFallback(cfg Config, logger zerolog.Logger, pingTimeout time.Duration) (store factsstore.FactsStoreI, isChStore bool) {
	if cfg.URL == "" {
		cfg = Defaults()
	}
	if pingTimeout <= 0 {
		pingTimeout = 2 * time.Second
	}
	ch, err := New(cfg)
	if err != nil {
		logger.Warn().Err(err).
			Str("url", cfg.URL).
			Msg("factsstore: chstore construction failed; falling back to in-memory")
		store = factsstore.NewInMemoryFactsStore()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()
	pingErr := ch.Ping(ctx)
	if pingErr != nil {
		logger.Warn().Err(pingErr).
			Str("url", cfg.URL).
			Dur("timeout", pingTimeout).
			Msg("factsstore: ClickHouse unreachable; falling back to in-memory")
		store = factsstore.NewInMemoryFactsStore()
		return
	}
	// Reachable — also try to set up the table (best-effort; if DDL
	// fails the writer will fail too, but we surface it here loudly).
	setupErr := ch.SetupTable(ctx, "")
	if setupErr != nil {
		logger.Warn().Err(setupErr).
			Str("url", cfg.URL).
			Msg("factsstore: ClickHouse reachable but DDL setup failed; falling back to in-memory")
		store = factsstore.NewInMemoryFactsStore()
		return
	}
	logger.Info().
		Str("url", cfg.URL).
		Str("db", cfg.Database).
		Str("table", cfg.Table).
		Msg("factsstore: using ClickHouse-backed audit trail")
	store = ch
	isChStore = true
	return
}
