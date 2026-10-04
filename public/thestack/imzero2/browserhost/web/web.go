// Package web is what the page loads beside the two wasm modules: the worker
// that runs them and the WASI shim between them (ADR-0263), plus the viewer
// page itself, gathered so a server can hand all three out of the binary
// that serves the bundle (ADR-0278 SD3, proposed). A page, worker and shim
// taken from one binary come from one boxer version.
package web

import (
	_ "embed"

	"github.com/stergiotis/boxer/rust/imzero2/src/imzero2/viewer"
)

//go:embed worker.mjs
var workerMjs []byte

//go:embed bridge.mjs
var bridgeMjs []byte

// Assets returns the files by the name a bundle serves them under. The map is
// new on each call; the byte slices are shared and must not be written.
func Assets() (files map[string][]byte) {
	return map[string][]byte{
		"index.html": viewer.IndexHTML,
		"worker.mjs": workerMjs,
		"bridge.mjs": bridgeMjs,
	}
}
