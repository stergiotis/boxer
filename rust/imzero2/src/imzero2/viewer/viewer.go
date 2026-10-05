// Package viewer carries the imzero2 browser viewer page to Go hosts. The page
// belongs to the Rust tree — the WebSocket carrier compiles it in with
// include_str! — and go:embed reads only files beside the embedding package,
// so this package sits next to it rather than copying it (ADR-0278 SD3,
// proposed).
package viewer

import _ "embed"

// IndexHTML is the viewer page: the painter for the carrier's mesh lane and,
// with ?worker=, the page a browser tab runs in (ADR-0263).
//
//go:embed index.html
var IndexHTML []byte
