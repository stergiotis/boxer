package queryrunsvc

import (
	"context"
	"math"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryrunfacts"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Reconcile makes the pipeline objects match this process's
// configuration (ADR-0115 SD4): destination DDL, a forced flush so the
// lazily-created system.query_log exists before the first extract, and
// a drop/recreate of the refreshable MV (unconditional — cheaper and
// drift-proof compared to normalizing create_table_query for a
// comparison; the refresh schedule simply restarts).
//
// Start calls this after the listener is bound, because the MV embeds
// the resolved pull URL. It is also callable on its own for a
// dry-run-style setup against a scratch database.
func (s *Service) Reconcile(ctx context.Context) (err error) {
	store, err := chstore.New(chstore.Config{
		URL:      s.cfg.ChURL,
		User:     s.cfg.ChUser,
		Password: s.cfg.Password,
		Database: s.cfg.Database,
		Table:    s.cfg.Table,
	})
	if err != nil {
		err = eh.Errorf("queryrunsvc: reconcile: %w", err)
		return
	}
	err = store.SetupTable(ctx, "")
	if err != nil {
		err = eh.Errorf("queryrunsvc: reconcile: facts ddl: %w", err)
		return
	}
	// The bare form (no table argument) works across ClickHouse
	// versions; our own reconcile queries above guarantee there is
	// something to flush on a fresh server, so system.query_log
	// materializes here at the latest.
	err = s.cli.Exec(ctx, "SYSTEM FLUSH LOGS")
	if err != nil {
		err = eh.Errorf("queryrunsvc: reconcile: flush logs: %w", err)
		return
	}
	// Best-effort: a refresh already in flight against a dead endpoint
	// would make the DROP below wait for it; cancelling first unblocks
	// the boot path. Errors are expected (first boot: no view yet) and
	// deliberately ignored.
	_ = s.cli.Exec(ctx, "SYSTEM CANCEL VIEW "+s.MvName())
	drop, err := queryrunfacts.ComposeDropMvSql(s.MvName())
	if err != nil {
		return
	}
	err = s.cli.Exec(ctx, drop)
	if err != nil {
		err = eh.Errorf("queryrunsvc: reconcile: drop mv: %w", err)
		return
	}
	mv, err := queryrunfacts.ComposeMvSql(s.MvName(), s.FactsTable(), s.PullURL(), s.cadenceSeconds())
	if err != nil {
		return
	}
	err = s.cli.Exec(ctx, mv)
	if err != nil {
		err = eh.Errorf("queryrunsvc: reconcile: create mv: %w", err)
		return
	}
	return
}

// Teardown removes the MV — the integration tests' cleanup; the
// destination table is left alone (it is shared with every other facts
// writer).
func (s *Service) Teardown(ctx context.Context) (err error) {
	drop, err := queryrunfacts.ComposeDropMvSql(s.MvName())
	if err != nil {
		return
	}
	err = s.cli.Exec(ctx, drop)
	return
}

// cadenceSeconds rounds the configured cadence up to whole seconds
// (REFRESH EVERY takes integer units; sub-second cadences make no sense
// against a seconds-order query_log flush anyway).
func (s *Service) cadenceSeconds() (out int) {
	out = max(1, int(math.Ceil(s.cfg.Cadence.Seconds())))
	return
}
