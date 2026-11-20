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
	"context"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/rs/zerolog/log"
	cli "github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/keelson/runtime/loopback"
)

const flagNameHttpListenAddress = "pprofHttpListenAddress"
const flagNameHttpAllowNonLoopback = "pprofHttpAllowNonLoopback"

// Flags carries the listener's address and its bind opt-in. The address
// must be a loopback host unless --pprofHttpAllowNonLoopback is set: the
// pprof surface has no authentication, and a profile or a goroutine dump
// shows a process's memory and arguments.
var Flags = []cli.Flag{
	&cli.StringFlag{
		Name:     flagNameHttpListenAddress,
		Category: "profiling",
		Usage:    "serve net/http/pprof on this address (a loopback host unless --" + flagNameHttpAllowNonLoopback + ")",
		Action:   httpServerAddressAction,
	},
	&cli.BoolFlag{
		Name:     flagNameHttpAllowNonLoopback,
		Category: "profiling",
		Usage:    "allow --" + flagNameHttpListenAddress + " to bind a non-loopback address; the pprof surface is unauthenticated",
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

// httpServerAddressAction binds the listener before returning, so a refused
// or failed bind ends the command instead of being logged and ignored.
func httpServerAddressAction(ctx context.Context, cmd *cli.Command, s string) (err error) {
	gate := loopback.Gate{
		Name:             "pprofhttp",
		OptIn:            "--" + flagNameHttpAllowNonLoopback,
		AllowNonLoopback: cmd.Bool(flagNameHttpAllowNonLoopback),
	}
	ln, err := gate.Listen(ctx, s)
	if err != nil {
		return
	}
	// No WriteTimeout: /debug/pprof/profile?seconds=N and the delta profiles
	// hold the response open for the requested duration, and any deadline
	// here would truncate exactly the long capture worth taking.
	server := &http.Server{
		Handler:           NewServeMux(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		serveErr := server.Serve(ln)
		if serveErr != nil && serveErr != http.ErrServerClosed {
			log.Error().Str("address", s).Err(serveErr).Msg("pprof http server stopped")
		}
	}()
	return
}
