package good

import (
	"net"
	"net/http"
)

// Serving on a listener someone else bound is fine; only the binding calls
// are banned outside package loopback.
func serve(ln net.Listener) error {
	srv := &http.Server{}
	return srv.Serve(ln)
}

// A directive with a reason exempts one call.
func unixSocket(path string) (net.Listener, error) {
	return net.Listen("unix", path) //boxer:lint disable=CS018 reason="a Unix socket is not a network exposure"
}
