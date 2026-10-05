//go:build js || wasip1

package chserver

import (
	"net/http"

	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

// newProgressClient returns nil under wasm: the live-progress transport
// speaks HTTP/1.1 over a raw socket to read X-ClickHouse-Progress headers
// as they stream, and a wasm module has no sockets — its HTTP is a host
// import that returns whole responses (ADR-0077 SD9). The caller keeps its
// stock client and a run reports no progress until it completes.
func newProgressClient(_ string, _ func(p runstream.Progress)) (client *http.Client) {
	return nil
}
