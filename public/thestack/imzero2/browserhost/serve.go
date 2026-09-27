package browserhost

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ServeConfig is what Serve needs: the bundle directory, where to listen,
// and where the page's data plane goes.
type ServeConfig struct {
	// Dir is the bundle build_tab_bundle.sh wrote.
	Dir string
	// Listen is the bind address; a port of 0 picks one, and Serve prints
	// the chosen address as `PORT <n>` on stdout either way, which is how
	// the trial's measurement script finds it.
	Listen string
	// ChURL is the ClickHouse HTTP endpoint that `/ch/` proxies to, so the
	// page's data plane stays same-origin (ADR-0077 SD9).
	ChURL string
	// ExitOnReport makes a POST to /report — the trial's browser arms
	// posting their result — end the server after printing the body;
	// off, the body is printed and serving continues.
	ExitOnReport bool
}

// Serve serves a tab bundle: the directory's files with no caching, `/ch/`
// proxied to ClickHouse, `POST /log` (the worker forwarding the module's
// stderr under log=1) to the logger, and `POST /report` to stdout. It is a
// development server — no auth, no TLS, no rate limit — for the shape a
// static host plus a same-origin proxy would take in a deployment. Returns
// when ctx ends or, with ExitOnReport, after the first report.
func Serve(ctx context.Context, cfg ServeConfig, logger zerolog.Logger) (err error) {
	if cfg.Dir == "" {
		return eh.Errorf("browserhost: serve: no directory")
	}
	target, err := url.Parse(cfg.ChURL)
	if err != nil || target.Host == "" {
		return eb.Build().Str("chURL", cfg.ChURL).Errorf("browserhost: serve: ClickHouse endpoint is not an absolute URL")
	}
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return eh.Errorf("browserhost: serve: listen: %w", err)
	}
	reported := make(chan struct{}, 1)

	// /ch/<rest> → ChURL's path joined with <rest>; the query string and
	// the body pass through, the Host header becomes the endpoint's.
	proxy := &httputil.ReverseProxy{
		Director: func(r *http.Request) {
			rest := strings.TrimPrefix(r.URL.Path, "/ch")
			if rest == "" {
				rest = "/"
			}
			r.URL.Scheme = target.Scheme
			r.URL.Host = target.Host
			r.URL.Path = strings.TrimSuffix(target.Path, "/") + rest
			r.Host = target.Host
		},
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) {
		http.Error(w, "proxy: "+e.Error(), http.StatusBadGateway)
	}

	files := http.FileServer(http.Dir(cfg.Dir))
	mux := http.NewServeMux()
	mux.HandleFunc("/ch/", func(w http.ResponseWriter, r *http.Request) { proxy.ServeHTTP(w, r) })
	mux.HandleFunc("/ch", func(w http.ResponseWriter, r *http.Request) { proxy.ServeHTTP(w, r) })
	mux.HandleFunc("POST /log", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		logger.Info().Str("module", strings.TrimSpace(string(b))).Msg("tab")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /report", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 16<<20))
		w.WriteHeader(http.StatusNoContent)
		fmt.Fprintln(os.Stdout, strings.TrimSpace(string(b)))
		if cfg.ExitOnReport {
			select {
			case reported <- struct{}{}:
			default:
			}
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		logger.Debug().Str("method", r.Method).Str("url", r.URL.String()).Msg("tab: request")
		w.Header().Set("Cache-Control", "no-store")
		// The viewer page is opened as /index.html?worker=…; the file
		// server would answer that with a redirect to "/", which a page
		// keeps working through but need not.
		if r.URL.Path == "/index.html" {
			r.URL.Path = "/"
		}
		switch path.Ext(r.URL.Path) {
		case ".wasm":
			w.Header().Set("Content-Type", "application/wasm")
		case ".mjs", ".js":
			w.Header().Set("Content-Type", "text/javascript")
		}
		files.ServeHTTP(w, r)
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	fmt.Fprintf(os.Stdout, "PORT %d\n", ln.Addr().(*net.TCPAddr).Port)
	logger.Info().Str("addr", ln.Addr().String()).Str("dir", cfg.Dir).Str("clickhouse", cfg.ChURL).Msg("tab: serving")
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	select {
	case err = <-done:
	case <-ctx.Done():
	case <-reported:
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return
}
