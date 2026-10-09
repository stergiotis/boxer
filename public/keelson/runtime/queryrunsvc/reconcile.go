package queryrunsvc

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/loopback"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryrunfacts"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
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
	// chstore's own DDL, run through this service's client so it carries
	// the reconcile tag like everything else the service sends.
	stmts, err := chstore.SetupStatements(chstore.Config{Database: s.cfg.Database, Table: s.cfg.Table}, "")
	if err != nil {
		err = eh.Errorf("queryrunsvc: reconcile: %w", err)
		return
	}
	for _, stmt := range stmts {
		err = s.cli.Exec(ctx, stmt)
		if err != nil {
			err = eh.Errorf("queryrunsvc: reconcile: facts ddl: %w", err)
			return
		}
	}
	// SetupTable is CREATE IF NOT EXISTS — it cannot reconcile a table
	// from an older schema generation. Verify the destination actually
	// carries the columns the pipeline references before creating the MV:
	// without this, a drifted table fails later with ClickHouse's
	// misleading "correlated subqueries are not supported" (the missing
	// column inside the anti-join subquery binds to the outer url() scope
	// instead). Migration is deliberately not this service's job.
	err = s.checkDestinationSchema(ctx)
	if err != nil {
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
	err = s.checkSingleOwner(ctx)
	if err != nil {
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

// checkDestinationSchema verifies the destination carries every column
// of the current facts schema. A table from an older schema generation
// (e.g. pre-array-section, z32 timestamps) passes CREATE IF NOT EXISTS
// untouched and then breaks the pipeline downstream with misleading
// errors; failing here names the drift and leaves the decision — a
// leeway migration, or moving the old table aside — to the operator.
func (s *Service) checkDestinationSchema(ctx context.Context) (err error) {
	want, err := queryrunfacts.DdlColumnNames()
	if err != nil {
		return
	}
	body, err := s.cli.Query(ctx, queryrunfacts.DestinationColumnsSql(s.cfg.Database, s.cfg.Table))
	if err != nil {
		err = eh.Errorf("queryrunsvc: reconcile: destination columns: %w", err)
		return
	}
	defer func() { _ = body.Close() }()
	raw, err := io.ReadAll(body)
	if err != nil {
		err = eh.Errorf("queryrunsvc: reconcile: destination columns read: %w", err)
		return
	}
	// Leeway wire names contain only ':' and identifier characters — no
	// TSV unescaping needed (the fetchColumnNames precedent).
	have := make(map[string]bool, len(want))
	for line := range strings.SplitSeq(string(raw), "\n") {
		if line = strings.TrimRight(line, "\r"); line != "" {
			have[line] = true
		}
	}
	missing := make([]string, 0, 4)
	for _, name := range want {
		if !have[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		example := missing[0]
		err = eb.Build().Str("destination", s.FactsTable()).Int("missing", len(missing)).Int("want", len(want)).Str("example", example).
			Errorf("queryrunsvc: the destination exists but lacks some current facts columns — an older schema generation; migrate it or move it aside, this service will not mutate an existing table")
		return
	}
	return
}

// checkSingleOwner refuses to take over a capture view another live
// instance serves. Two daemons against one server would otherwise drop and
// recreate each other's view on every boot, each pointing it at itself.
// The previous owner counts as live when its /healthz answers; a dead
// one's view is taken over, which is the ordinary restart path, and a view
// already pointing at this instance's own URL is this instance's.
func (s *Service) checkSingleOwner(ctx context.Context) (err error) {
	body, err := s.cli.Query(ctx, queryrunfacts.ExistingMvSql(s.cfg.Database))
	if err != nil {
		err = eh.Errorf("queryrunsvc: reconcile: existing view: %w", err)
		return
	}
	defer func() { _ = body.Close() }()
	raw, err := io.ReadAll(body)
	if err != nil {
		err = eh.Errorf("queryrunsvc: reconcile: existing view read: %w", err)
		return
	}
	prev := queryrunfacts.ParseMvPullURL(string(raw))
	if prev == "" || prev == s.PullURL() || !answersHealthz(ctx, prev) {
		return
	}
	err = eb.Build().Str("mv", s.MvName()).Str("owner", prev).
		Errorf("queryrunsvc: another queryrunsd is serving this capture view; stop it first")
	return
}

// answersHealthz says the service behind a pull URL is up. Only a loopback
// URL is asked: the URL comes from the server's stored view, and the
// service never sends requests elsewhere on its say-so.
func answersHealthz(ctx context.Context, pullURL string) (up bool) {
	u, err := url.Parse(pullURL)
	if err != nil || !loopback.IsHost(u.Hostname()) {
		return
	}
	u.Path = strings.TrimSuffix(u.Path, "/pull") + "/healthz"
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// cadenceSeconds rounds the configured cadence up to whole seconds
// (REFRESH EVERY takes integer units; sub-second cadences make no sense
// against a seconds-order query_log flush anyway).
func (s *Service) cadenceSeconds() (out int) {
	out = max(1, int(math.Ceil(s.cfg.Cadence.Seconds())))
	return
}
