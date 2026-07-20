// Package introspecthttp serves keelson introspection tables as Arrow
// over HTTP, so a clickhouse-local or clickhouse-server can pull them
// via the url() table function and JOIN them with other data (ADR-0094
// §SD3). It is loopback-bound by default; non-loopback exposure (bearer
// token + TLS) is deferred to ADR-0082 and refused at Start until then.
package introspecthttp

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/chhttp"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonsql"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// ListenAddr is the default bind address for the table source (ADR-0094
// §SD3). Loopback by default; a non-loopback host is refused at Start.
var ListenAddr = env.NewString(env.Spec{
	Name:        "KEELSON_INTROSPECT_HTTP_LISTEN",
	Default:     "127.0.0.1:0",
	Description: "bind address for the keelson introspection HTTP table source (url() endpoint); must be a loopback host in v1",
	Category:    env.CategorySystem,
})

// Server serves introspection tables as ArrowStream over HTTP.
type Server struct {
	reg    *introspect.Registry
	log    zerolog.Logger
	addr   string
	runner QueryRunner
	passes *passreg.Registry
	srv    *http.Server
	ln     net.Listener
}

// Config parameterises a Server.
type Config struct {
	// Registry to serve; defaults to introspect.Default.
	Registry *introspect.Registry
	// Addr is the bind address; defaults to ListenAddr (env), else
	// 127.0.0.1:0 (a loopback ephemeral port).
	Addr string
	// Runner, when set, backs POST /query (ADR-0094 §SD4): it runs a SQL
	// statement (FORMAT clause included) against clickhouse-local and
	// returns the raw output. nil disables /query (it answers 503).
	Runner QueryRunner
	// Passes supplies the registered pre-execute rewrites applied to
	// /query SQL (ADR-0108 §SD6); defaults to passreg.Default.
	Passes *passreg.Registry
}

// QueryRunner executes SQL (the FORMAT clause is already part of sql) with
// `{name:Type}` placeholder bindings (ADR-0133 §SD2/§SD3; nil or empty when
// the request carries none) and returns the raw clickhouse-local output.
// Kept an interface so introspecthttp need not import the bus / chlocal
// broker; the runtime supplies a broker-backed implementation.
type QueryRunner interface {
	RunSQL(ctx context.Context, sql string, params map[string]string) (body []byte, err error)
}

// RunnerFunc adapts a function to QueryRunner.
type RunnerFunc func(ctx context.Context, sql string, params map[string]string) ([]byte, error)

// RunSQL implements QueryRunner.
func (f RunnerFunc) RunSQL(ctx context.Context, sql string, params map[string]string) ([]byte, error) {
	return f(ctx, sql, params)
}

// New returns an unstarted Server.
func New(cfg Config, log zerolog.Logger) (s *Server) {
	reg := cfg.Registry
	if reg == nil {
		reg = introspect.Default
	}
	addr := cfg.Addr
	if addr == "" {
		if raw, set := ListenAddr.Lookup(); set && raw != "" {
			addr = raw
		} else {
			addr = "127.0.0.1:0"
		}
	}
	ps := cfg.Passes
	if ps == nil {
		ps = passreg.Default
	}
	s = &Server{reg: reg, log: log, addr: addr, runner: cfg.Runner, passes: ps}
	s.srv = &http.Server{Handler: s.handler(), ReadHeaderTimeout: 5 * time.Second}
	return
}

// Start binds the listener and serves in a background goroutine. It
// refuses a non-loopback bind (ADR-0082 §SD1 bind-gate).
func (s *Server) Start() (err error) {
	host, _, splitErr := net.SplitHostPort(s.addr)
	if splitErr != nil {
		return eh.Errorf("introspecthttp: bad listen addr %q: %w", s.addr, splitErr)
	}
	if !isLoopbackHost(host) {
		return eh.Errorf("introspecthttp: refusing non-loopback bind %q; remote exposure (token+TLS) is deferred to ADR-0082 §SD1", s.addr)
	}
	ln, lnErr := net.Listen("tcp", s.addr)
	if lnErr != nil {
		return eh.Errorf("introspecthttp: listen %q: %w", s.addr, lnErr)
	}
	s.ln = ln
	go func() {
		if serveErr := s.srv.Serve(ln); serveErr != nil && serveErr != http.ErrServerClosed {
			s.log.Warn().Err(serveErr).Msg("introspecthttp: serve")
		}
	}()
	s.log.Info().Str("addr", s.Addr()).Msg("introspecthttp: table source listening")
	return
}

// Addr returns the bound address (with the resolved port once started).
func (s *Server) Addr() (a string) {
	if s.ln != nil {
		return s.ln.Addr().String()
	}
	return s.addr
}

// BaseURL is the http:// origin a url() reference targets.
func (s *Server) BaseURL() string { return "http://" + s.Addr() }

// Stop gracefully shuts the server down.
func (s *Server) Stop(ctx context.Context) error { return s.srv.Shutdown(ctx) }

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tables", s.handleTables)
	mux.HandleFunc("GET /table/{name}", s.handleTable)
	mux.HandleFunc("POST /query", s.handleQuery)
	mux.HandleFunc("GET /query", s.handleQuery)
	return mux
}

// handleTables lists the registered table names, one per line — a cheap
// discovery endpoint for humans and tooling.
func (s *Server) handleTables(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	for _, n := range s.reg.Names() {
		_, _ = w.Write([]byte(n + "\n"))
	}
}

// handleTable serves one table as ArrowStream. An optional ?cols=a,b
// prunes columns (best-effort; ClickHouse does not negotiate columns
// over url(), so this is the only column lever in URL mode — ADR-0094
// §SD3).
func (s *Server) handleTable(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	p, ok := s.reg.Lookup(name)
	if !ok {
		http.Error(w, "unknown introspection table: "+name, http.StatusNotFound)
		return
	}
	// Ad-hoc datasets never ride HTTP: they decrypt only through the
	// in-process engine's broker path, so plaintext cannot leave over the
	// wire (ADR-0134 SD3). Refuse with a clear error rather than a 500
	// from the snapshot attempt.
	if _, enc := p.(introspect.EncryptedDatasetI); enc {
		http.Error(w, "ad-hoc dataset "+name+" is not served over HTTP; query it through the in-process engine (ADR-0134 SD3)", http.StatusForbidden)
		return
	}
	proj := introspect.AllColumns()
	if cols := r.URL.Query().Get("cols"); cols != "" {
		proj = introspect.Columns(splitCols(cols)...)
	}
	b, err := introspect.SnapshotStream(p, proj)
	if err != nil {
		s.log.Warn().Err(err).Str("table", name).Msg("introspecthttp: snapshot failed")
		http.Error(w, "snapshot failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.apache.arrow.stream")
	_, _ = w.Write(b)
}

// maxQueryBytes caps the SQL a /query request may carry.
const maxQueryBytes = 1 << 20

// handleQuery runs a SQL statement against clickhouse-local after
// expanding keelson('<table>') macros to url() references against this
// server (ADR-0094 §SD4). A client (e.g. apps/play) can point at this
// endpoint and query keelson('env') with no external server and no url()
// boilerplate. The SQL carries its own FORMAT clause (clients append
// FORMAT ArrowStream); the response is clickhouse-local's raw output.
//
// The wire dialect — statement extraction, `param_*` binding, settings
// tolerance, the summary header and exception envelope — is chhttp
// (ADR-0133 §SD3); this handler owns only the endpoint policy on top:
// pre-execute passes, the keelson macro rewrite, and the runner.
func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	if s.runner == nil {
		chhttp.WriteException(w, http.StatusServiceUnavailable,
			"introspection /query is not configured (no clickhouse-local runner)")
		return
	}
	req, err := chhttp.ParseRequest(r, maxQueryBytes)
	if err != nil {
		chhttp.WriteException(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.SQL == "" {
		chhttp.WriteException(w, http.StatusBadRequest, "empty query")
		return
	}
	for _, key := range req.Ignored {
		if !chhttp.KnownIgnorableSetting(key) {
			s.log.Debug().Str("key", key).Msg("introspecthttp: /query ignoring unknown query-string key")
		}
	}
	// Registered pre-execute rewrites (ADR-0108 §SD6) — e.g. LW_ID_* macro
	// expansion, which this path needs because chlocal has no LW_ID_* UDFs
	// installed (identsql only emits them for real servers). Best-effort: a
	// failing pass is skipped and the SQL from before it runs instead.
	sql := s.passes.ApplyBestEffort(passreg.StagePreExecute, req.SQL, s.log)
	rewritten, err := keelsonsql.RewriteToURL(s.reg, s.BaseURL(), sql)
	if err != nil {
		// unknown keelson table / malformed macro — a client error.
		chhttp.WriteException(w, http.StatusBadRequest, err.Error())
		return
	}
	start := time.Now()
	body, err := s.runner.RunSQL(r.Context(), rewritten, req.Params)
	if err != nil {
		s.log.Warn().Err(err).Msg("introspecthttp: /query failed")
		// The broker error carries clickhouse-local's stderr, whose
		// `Code: N` the envelope surfaces for probe classification.
		chhttp.WriteException(w, http.StatusBadRequest, err.Error())
		return
	}
	// result_bytes and elapsed_ns only; the chlocal transport surfaces no
	// read counters and the envelope reports honest zeros (ADR-0133 §SD4).
	chhttp.WriteSummary(w, chhttp.Summary{
		ResultBytes: uint64(len(body)),
		Elapsed:     time.Since(start),
	})
	w.Header().Set("Content-Type", chhttp.ContentTypeForStatement(rewritten))
	_, _ = w.Write(body)
}

func splitCols(s string) (out []string) {
	for c := range strings.SplitSeq(s, ",") {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return
}

func isLoopbackHost(host string) (ok bool) {
	if host == "" || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
