// Package queryrunsvc is the query-run capture service (ADR-0115 S1):
// a stateless, idempotent, loopback-only HTTP endpoint that a
// ClickHouse-owned refreshable materialized view pulls. GET /pull runs
// the queryrunfacts extract against the same server, encodes the rows
// through the generated runtime.facts DML builders, and streams the
// resulting Arrow IPC back — exactly the bytes chstore would insert, so
// no re-encoding tier exists anywhere in the plane (ADR-0115 SD3).
//
// The service holds no write authority: ClickHouse performs the INSERT
// as part of the refresh, the pipeline is a schema object (drop/create
// reconciled at boot), and its health is a table
// (system.view_refreshes). Statelessness is mandatory: url() reads
// amplify (a pipeline-construction pass and a data pass were measured),
// so /pull must return the same answer for back-to-back reads —
// watermarks derive from the destination, never from having-been-read.
package queryrunsvc

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryrunfacts"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Env registry (ADR-0009): the queryrunsd coordinates.
var (
	ListenAddr = env.NewString(env.Spec{
		Name:        "IMZERO2_QUERYRUNS_LISTEN",
		Default:     "127.0.0.1:8127",
		Description: "bind address for the queryrunsd /pull endpoint (ADR-0115); must be a loopback host — the refreshable MV reads it via url()",
		Category:    env.CategoryObservability,
	})
	ChURL = env.NewString(env.Spec{
		Name:        "IMZERO2_QUERYRUNS_CH_URL",
		Default:     "http://localhost:8123/",
		Description: "ClickHouse HTTP endpoint queryrunsd extracts system.query_log from and reconciles the pipeline objects against (ADR-0115)",
		Category:    env.CategoryObservability,
	})
	Cadence = env.NewDuration(env.Spec{
		Name:        "IMZERO2_QUERYRUNS_CADENCE",
		Default:     "5s",
		Description: "refresh cadence of the capture materialized view (whole seconds, minimum 1s); ClickHouse owns the schedule (ADR-0115 SD2)",
		Category:    env.CategoryObservability,
	})
	Scope = env.NewCategorialString(env.Spec{
		Name:        "IMZERO2_QUERYRUNS_SCOPE",
		Default:     string(queryrunfacts.ScopeAll),
		Description: "capture scope: every terminal query_log event, only boxer-stamped ones, or off (the endpoint serves empty batches)",
		Category:    env.CategoryObservability,
	}, []string{
		string(queryrunfacts.ScopeAll),
		string(queryrunfacts.ScopeStamped),
		string(queryrunfacts.ScopeOff),
	})
)

// Config parameterises a Service. Zero values fall back to the env
// registry (Listen/ChURL/Cadence/Scope) and the conventional
// runtime.facts coordinates; tests point Database/Table at a scratch
// database.
type Config struct {
	Listen   string
	ChURL    string
	ChUser   string
	Password string
	Cadence  time.Duration
	Scope    queryrunfacts.ScopeE
	Database string
	Table    string
	BatchCap int
}

// Service is the running capture endpoint. Construct with New, then
// Start (bind → reconcile → serve); Stop shuts the HTTP server down —
// the pipeline objects stay, refreshing against a dead endpoint until
// the next start catches up (pull-shape degradation, ADR-0115 SD2).
type Service struct {
	cfg Config
	cli *chclient.Client
	log zerolog.Logger
	srv *http.Server
	ln  net.Listener
}

// New fills cfg defaults and constructs the service.
func New(cfg Config, log zerolog.Logger) (s *Service, err error) {
	if cfg.Listen == "" {
		cfg.Listen = ListenAddr.Get()
	}
	if cfg.ChURL == "" {
		cfg.ChURL = ChURL.Get()
	}
	if cfg.Cadence <= 0 {
		cfg.Cadence = Cadence.Get()
	}
	if cfg.Scope == "" {
		cfg.Scope = queryrunfacts.ScopeE(Scope.Get())
	}
	switch cfg.Scope {
	case queryrunfacts.ScopeAll, queryrunfacts.ScopeStamped, queryrunfacts.ScopeOff:
	default:
		err = eh.Errorf("queryrunsvc: unknown scope %q", cfg.Scope)
		return
	}
	if cfg.Database == "" {
		cfg.Database = factsschema.DatabaseName
	}
	if cfg.Table == "" {
		cfg.Table = factsschema.TableName
	}
	if cfg.BatchCap <= 0 {
		cfg.BatchCap = queryrunfacts.DefaultBatchCap
	}
	s = &Service{
		cfg: cfg,
		cli: chclient.New(chclient.Config{URL: cfg.ChURL, User: cfg.ChUser, Password: cfg.Password}, nil),
		log: log,
	}
	s.srv = &http.Server{Handler: s.handler(), ReadHeaderTimeout: 5 * time.Second}
	return
}

// FactsTable is the qualified destination table.
func (s *Service) FactsTable() string { return s.cfg.Database + "." + s.cfg.Table }

// MvName is the qualified materialized-view name, co-located with the
// destination so a scratch-database test tears everything down at once.
func (s *Service) MvName() string { return s.cfg.Database + "." + queryrunfacts.MvBaseName }

// Addr returns the bound address (resolved port once started).
func (s *Service) Addr() (a string) {
	if s.ln != nil {
		return s.ln.Addr().String()
	}
	return s.cfg.Listen
}

// PullURL is the endpoint the materialized view reads.
func (s *Service) PullURL() string { return "http://" + s.Addr() + "/pull" }

// Start binds the listener (refusing non-loopback — the endpoint is
// unauthenticated by loopback containment, the introspecthttp
// precedent), reconciles the pipeline objects, and serves in a
// background goroutine. A failed reconciliation fails Start: under
// systemd Restart=always the unit retries until ClickHouse is up, and
// pull-shape means nothing is lost while it waits.
func (s *Service) Start(ctx context.Context) (err error) {
	host, _, splitErr := net.SplitHostPort(s.cfg.Listen)
	if splitErr != nil {
		err = eh.Errorf("queryrunsvc: bad listen addr %q: %w", s.cfg.Listen, splitErr)
		return
	}
	if !isLoopbackHost(host) {
		err = eh.Errorf("queryrunsvc: refusing non-loopback bind %q; remote exposure (token+TLS) is deferred to ADR-0082 §SD1", s.cfg.Listen)
		return
	}
	ln, lnErr := net.Listen("tcp", s.cfg.Listen)
	if lnErr != nil {
		err = eh.Errorf("queryrunsvc: listen %q: %w", s.cfg.Listen, lnErr)
		return
	}
	s.ln = ln
	err = s.Reconcile(ctx)
	if err != nil {
		_ = ln.Close()
		s.ln = nil
		return
	}
	go func() {
		if serveErr := s.srv.Serve(ln); serveErr != nil && serveErr != http.ErrServerClosed {
			s.log.Warn().Err(serveErr).Msg("queryrunsvc: serve")
		}
	}()
	s.log.Info().Str("addr", s.Addr()).Str("mv", s.MvName()).Str("scope", string(s.cfg.Scope)).
		Msg("queryrunsvc: /pull listening; refreshable MV reconciled")
	return
}

// Stop gracefully shuts the HTTP server down.
func (s *Service) Stop(ctx context.Context) (err error) {
	err = s.srv.Shutdown(ctx)
	return
}

func (s *Service) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /pull", s.handlePull)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	return mux
}

// handleHealthz reports process liveness only — pipeline health is
// deliberately a ClickHouse concern (system.view_refreshes), not a
// self-report.
func (s *Service) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

// handlePull extracts, encodes, and streams. Every failure is a 500
// whose text lands in system.view_refreshes.exception via the refresh
// machinery — the operator reads pipeline errors as data.
func (s *Service) handlePull(w http.ResponseWriter, r *http.Request) {
	rows, err := s.extract(r.Context())
	if err != nil {
		s.log.Warn().Err(err).Msg("queryrunsvc: extract failed")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ent := dml.NewInEntityFacts(memory.NewGoAllocator(), len(rows))
	err = queryrunfacts.BuildEntities(ent, rows)
	if err != nil {
		s.log.Warn().Err(err).Msg("queryrunsvc: encode failed")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	records, err := ent.TransferRecords(nil)
	if err != nil {
		s.log.Warn().Err(err).Msg("queryrunsvc: transfer failed")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() {
		for _, rec := range records {
			rec.Release()
		}
	}()
	// ArrowStream = the IPC stream format (schema + batches; a zero-row
	// response is a valid schema-only stream, which url() reads as an
	// empty table).
	w.Header().Set("Content-Type", "application/vnd.apache.arrow.stream")
	iw := ipc.NewWriter(w, ipc.WithSchema(ent.GetSchema()))
	for _, rec := range records {
		if wErr := iw.Write(rec); wErr != nil {
			s.log.Warn().Err(wErr).Msg("queryrunsvc: stream write failed")
			return
		}
	}
	if cErr := iw.Close(); cErr != nil {
		s.log.Warn().Err(cErr).Msg("queryrunsvc: stream close failed")
		return
	}
	if len(rows) > 0 {
		s.log.Debug().Int("rows", len(rows)).Msg("queryrunsvc: served pull batch")
	}
}

// extract runs the composed extract SELECT and decodes the JSONEachRow
// response. Scope off short-circuits to an empty batch — the pipeline
// keeps ticking, capturing nothing, and flipping the scope back needs
// no DDL.
func (s *Service) extract(ctx context.Context) (rows []queryrunfacts.Row, err error) {
	if s.cfg.Scope == queryrunfacts.ScopeOff {
		return
	}
	sql, err := queryrunfacts.ComposeExtractSql(s.FactsTable(), s.PullURL(), s.cfg.Scope, s.cfg.BatchCap)
	if err != nil {
		return
	}
	body, err := s.cli.Query(ctx, sql)
	if err != nil {
		err = eh.Errorf("queryrunsvc: extract query: %w", err)
		return
	}
	defer func() { _ = body.Close() }()
	dec := json.NewDecoder(body)
	for dec.More() {
		var row queryrunfacts.Row
		if dErr := dec.Decode(&row); dErr != nil {
			err = eh.Errorf("queryrunsvc: decode extract row %d: %w", len(rows), dErr)
			return
		}
		rows = append(rows, row)
	}
	return
}

// isLoopbackHost mirrors the introspecthttp bind-gate (ADR-0082 §SD1).
func isLoopbackHost(host string) (ok bool) {
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
