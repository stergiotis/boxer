// Package introspecthttp serves keelson introspection tables as Arrow
// over HTTP, so a clickhouse-local or clickhouse-server can pull them
// via the url() table function and JOIN them with other data (ADR-0094
// §SD3). It is loopback-bound by default; non-loopback exposure (bearer
// token + TLS) is deferred to ADR-0082 and refused at Start until then.
package introspecthttp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/chhttp"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonsql"
	"github.com/stergiotis/boxer/public/keelson/runtime/loopback"
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
	routes http.Handler
	ln     net.Listener
	// probes holds the outstanding E6 reachability nonces (probe.go). The
	// zero value is ready to use.
	probes probeStore
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

// MacroRunnerI is a QueryRunner that resolves the keelson() macros itself:
// the handler hands it the statement with the macros intact instead of
// rewriting them to url() first. The in-process engine is one (ADR-0253
// §SD5): an ordinary table is snapshotted and projected in-process, and
// only a sealed dataset goes back through this server's /table route.
// An unknown table is the runner's error to report.
type MacroRunnerI interface {
	QueryRunner
	// ResolvesMacros marks the type; it carries no behaviour.
	ResolvesMacros()
}

// SettingsRunnerI is a QueryRunner that is also handed the request's
// query-string settings — every key that is not the statement, a param_* or
// query_id, with its last value — and answers for them itself: a runner
// that answers without ClickHouse must refuse a setting that would change
// its result rather than drop it, as the dialect's tolerance does for a
// runner that is not one (ADR-0290 §SD4). It returns the format it encoded
// the body in, a ClickHouse format name, which the response's Content-Type
// follows. The trivial evaluator's Runner is one.
type SettingsRunnerI interface {
	QueryRunner
	RunSQLSettings(ctx context.Context, sql string, params map[string]string, settings map[string]string) (body []byte, format string, err error)
}

// MacroRunnerFunc adapts a function to MacroRunnerI.
type MacroRunnerFunc func(ctx context.Context, sql string, params map[string]string) ([]byte, error)

// RunSQL implements QueryRunner.
func (f MacroRunnerFunc) RunSQL(ctx context.Context, sql string, params map[string]string) ([]byte, error) {
	return f(ctx, sql, params)
}

// ResolvesMacros implements MacroRunnerI.
func (MacroRunnerFunc) ResolvesMacros() {}

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
	s.routes = s.handler()
	s.srv = &http.Server{Handler: s.hostGuard(s.routes), ReadHeaderTimeout: 5 * time.Second}
	return
}

// Start binds the listener and serves in a background goroutine. It
// refuses a non-loopback bind (ADR-0082 §SD1 bind-gate).
func (s *Server) Start() (err error) {
	// Loopback only: remote exposure (token+TLS) is deferred to ADR-0082 §SD1.
	ln, lnErr := loopback.Gate{Name: "introspecthttp", LoopbackOnly: true}.Listen(context.Background(), s.addr)
	if lnErr != nil {
		return lnErr
	}
	s.ln = ln
	// The registry learns where it is served, so a dataset exposed through
	// it can say where a reader fetches it (ADR-0407, proposed).
	s.reg.SetServedAt(s.BaseURL())
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
func (s *Server) Stop(ctx context.Context) error {
	if s.ln != nil {
		s.reg.ClearServedAt(s.BaseURL())
	}
	return s.srv.Shutdown(ctx)
}

// Handler is the server's routes, for serving them without a listener — in
// process, as a browser tab does through [InProcess] (ADR-0290 §SD4). It
// carries no Host check: an in-process caller names its own origin.
func (s *Server) Handler() http.Handler { return s.routes }

// hostGuard refuses a request whose Host header is not a loopback host with
// the port this server is bound to. A loopback bind keeps other machines
// out, but not a web page in a local browser: a name the page's site
// controls can be re-pointed at 127.0.0.1 (DNS rebinding), and the browser
// then sends that name as the Host. ClickHouse's url() and every in-tree
// client name the server by the address it is bound to, so they pass.
func (s *Server) hostGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hostAllowed(r.Host) {
			s.log.Warn().Str("host", r.Host).Str("path", r.URL.Path).Msg("introspecthttp: refusing a request for a host other than this loopback listener")
			http.Error(w, "misdirected request: the Host is not this loopback listener", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// hostAllowed says host (a Host header, host[:port]) names this listener.
func (s *Server) hostAllowed(host string) (ok bool) {
	if s.ln == nil {
		return false
	}
	tcp, isTCP := s.ln.Addr().(*net.TCPAddr)
	if !isTCP {
		return false
	}
	h, port, err := net.SplitHostPort(host)
	if err != nil {
		// No port: the client's default, 80.
		h, port = host, "80"
	}
	return loopback.IsHost(h) && port == strconv.Itoa(tcp.Port)
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tables", s.handleTables)
	mux.HandleFunc("GET /table/{name}", s.handleTable)
	mux.HandleFunc("POST /query", s.handleQuery)
	mux.HandleFunc("GET /query", s.handleQuery)
	// The E6 reachability probe (probe.go). No data, no policy: it answers
	// whether a minted nonce was fetched, which is what makes locality
	// provable rather than inferred from an address.
	mux.HandleFunc("GET /probe/{nonce}", s.handleProbe)
	// Exposed datasets (exposed.go, ADR-0407, proposed): by alias and token, never by
	// handle; /table and /query never open one.
	mux.HandleFunc("GET "+ExposedPathPrefix+"{alias}", s.handleExposed)
	mux.HandleFunc("GET "+ExposedPathPrefix+"{alias}/csv", s.handleExposedCSV)
	return mux
}

// handleTables lists the registered table names, one per line — a cheap
// discovery endpoint for humans and tooling. A sealed dataset is left out:
// its handle is what /table opens it by, and a listing open to every local
// process is no place to hand it out (ADR-0145).
func (s *Server) handleTables(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	for _, n := range s.reg.Names() {
		if s.reg.IsSealed(n) {
			continue
		}
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
	// A sealed dataset is served by opening its own record (ADR-0240
	// §SD3): the record holds the key, the plaintext exists only in this
	// process, and this endpoint is loopback-only (non-loopback binds are
	// refused at Start).
	if enc, isEnc := p.(introspect.EncryptedDatasetI); isEnc {
		s.serveEncrypted(w, r, enc)
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
	// ServeContent rather than a plain write: it answers range requests,
	// which ClickHouse's Arrow reader issues to skip column buffers a
	// query does not read (see serveEncrypted).
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(b))
}

// serveEncrypted serves a sealed dataset's plaintext as ArrowStream
// (ADR-0240 §SD3).
//
// It answers through ServeContent over the seekable plaintext because
// range requests are load-bearing here: ClickHouse's Arrow reader skips
// column buffers a query does not touch by re-requesting the source from
// a later offset, so a dataset larger than the client's read buffer is
// unreadable from a stream-only endpoint. The ETag carries the revision the
// reader was opened at — the record hands both out under one lock — so a
// ranged continuation that straddles a republish fails If-Range validation
// instead of splicing two revisions.
//
// A decrypt failure — truncation or authentication — cannot change the
// already-sent status, so it aborts the connection, which the url()
// reader surfaces as a failed fetch: the query fails as a whole rather
// than accepting a silently-truncated result.
func (s *Server) serveEncrypted(w http.ResponseWriter, r *http.Request, enc introspect.EncryptedDatasetI) {
	rc, revision, err := enc.Open()
	if err != nil {
		s.log.Warn().Err(err).Str("table", enc.Name()).Msg("introspecthttp: open sealed dataset")
		http.Error(w, "sealed dataset unavailable", http.StatusServiceUnavailable)
		return
	}
	defer func() { _ = rc.Close() }()
	w.Header().Set("Content-Type", "application/vnd.apache.arrow.stream")
	w.Header().Set("ETag", fmt.Sprintf("%q", fmt.Sprintf("%s-r%d", enc.Name(), revision)))
	tracked := &erroringReadSeeker{rs: rc}
	http.ServeContent(w, r, "", time.Time{}, tracked)
	if tracked.err != nil {
		s.log.Warn().Err(tracked.err).Str("table", enc.Name()).Msg("introspecthttp: sealed dataset stream aborted")
		panic(http.ErrAbortHandler)
	}
}

// erroringReadSeeker records the first read/seek failure so the handler
// can abort the connection after ServeContent returns — ServeContent
// itself swallows copy errors.
type erroringReadSeeker struct {
	rs  io.ReadSeeker
	err error
}

func (inst *erroringReadSeeker) Read(p []byte) (n int, err error) {
	n, err = inst.rs.Read(p)
	if err != nil && err != io.EOF && inst.err == nil {
		inst.err = err
	}
	return
}

func (inst *erroringReadSeeker) Seek(offset int64, whence int) (pos int64, err error) {
	pos, err = inst.rs.Seek(offset, whence)
	if err != nil && inst.err == nil {
		inst.err = err
	}
	return
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
	// Echo the client's run identity, as a server does. This endpoint has no
	// system.processes to register it in — its workers are one-shot and their
	// system tables die with them — so the echo plus the log line below is
	// the whole of what "the id reached here" can mean on this path. Set
	// before anything can fail, so a rejected run is joinable too.
	chhttp.WriteQueryID(w, req.QueryID)
	// Registered pre-execute rewrites (ADR-0108 §SD6) — e.g. LW_ID_* macro
	// expansion, which this path needs because chlocal has no LW_ID_* UDFs
	// installed (identsql only emits them for real servers). Best-effort: a
	// failing pass is skipped and the SQL from before it runs instead.
	sql := s.passes.ApplyBestEffort(passreg.StagePreExecute, req.SQL, s.log)
	rewritten := sql
	if _, resolves := s.runner.(MacroRunnerI); !resolves {
		var rerr error
		rewritten, rerr = keelsonsql.RewriteToURL(s.reg, s.BaseURL(), sql)
		if rerr != nil {
			// unknown keelson table / malformed macro — a client error.
			chhttp.WriteException(w, http.StatusBadRequest, rerr.Error())
			return
		}
	}
	start := time.Now()
	var body []byte
	contentType := chhttp.ContentTypeForStatement(rewritten)
	if sr, takes := s.runner.(SettingsRunnerI); takes {
		settings := make(map[string]string, len(req.Ignored))
		q := r.URL.Query()
		for _, key := range req.Ignored {
			v := q[key]
			settings[key] = v[len(v)-1]
		}
		var format string
		body, format, err = sr.RunSQLSettings(r.Context(), rewritten, req.Params, settings)
		contentType = chhttp.ContentTypeForStatement("FORMAT " + format)
	} else {
		body, err = s.runner.RunSQL(r.Context(), rewritten, req.Params)
	}
	if err != nil {
		s.log.Warn().Err(err).Str("queryId", req.QueryID).Msg("introspecthttp: /query failed")
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
	w.Header().Set("Content-Type", contentType)
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
