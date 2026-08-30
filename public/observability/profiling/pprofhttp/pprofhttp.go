// Package pprofhttp serves net/http/pprof's handlers on a listener of its own:
// --pprofHttpListenAddress starts one in the background and the process keeps
// running.
//
// It is a separate package from its parent (ADR-0212) because importing
// net/http/pprof is not free. It pulls net/http and, through it, the crypto and
// TLS trees — measured at +6.3 MB and +112 packages for a binary that links
// neither otherwise — and it registers /debug/pprof/* on http.DefaultServeMux
// from its own init, which no import of this package can prevent. A host that
// only wants a CPU profile written to a file imports the parent alone and pays
// for none of it.
//
// The handlers are registered on a mux this package owns rather than served off
// the default one, so the listener exposes exactly the pprof surface and not
// whatever else a host or one of its dependencies has registered globally. The
// paths are GET-only, as net/http/pprof itself has made them since Go 1.22.
package pprofhttp

import (
	"net/http"
	"net/http/pprof"

	"github.com/rs/zerolog/log"
	cli "github.com/urfave/cli/v2"
)

const flagNameHttpListenAddress = "pprofHttpListenAddress"

var Flags = []cli.Flag{
	&cli.StringFlag{
		Name:     flagNameHttpListenAddress,
		Category: "profiling",
		Action:   httpServerAddressAction,
	},
}

// NewServeMux returns a mux carrying only net/http/pprof's handlers, for a host
// that wants to mount them on a listener it already runs.
func NewServeMux() (mux *http.ServeMux) {
	mux = http.NewServeMux()
	mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	return
}

func httpServerAddressAction(context *cli.Context, s string) error {
	// No WriteTimeout: /debug/pprof/profile?seconds=N and the delta profiles
	// hold the response open for the requested duration, and any deadline
	// here would truncate exactly the long capture worth taking.
	server := &http.Server{
		Addr:    s,
		Handler: NewServeMux(),
	}
	go func() {
		err := server.ListenAndServe()
		if err != nil {
			log.Error().Str("address", s).Err(err).Msg("unable to start http server, ignoring error")
		}
	}()
	return nil
}
