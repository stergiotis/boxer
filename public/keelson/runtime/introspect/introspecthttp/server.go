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
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
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
	reg  *introspect.Registry
	log  zerolog.Logger
	addr string
	srv  *http.Server
	ln   net.Listener
}

// Config parameterises a Server.
type Config struct {
	// Registry to serve; defaults to introspect.Default.
	Registry *introspect.Registry
	// Addr is the bind address; defaults to ListenAddr (env), else
	// 127.0.0.1:0 (a loopback ephemeral port).
	Addr string
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
	s = &Server{reg: reg, log: log, addr: addr}
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
