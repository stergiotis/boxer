// Package browserhost is the Go side of imzero2 in a browser tab (ADR-0263):
// the pieces a wasip1 module needs to be a keelson host inside a Web Worker,
// with the Rust browser host (rust/imzero2/browser) beside it and the
// existing viewer page painting the mesh.
//
// Three things live here, each a seam the tab has and the desktop does not:
//
//   - [Mount] runs a registered keelson app the way the window host does,
//     minus the window and minus the runtime services: a static mount
//     context over an in-process bus with the app's own caps.
//   - [SetMain] and the wasip1 reactor exports. A c-shared wasip1 module runs
//     neither main nor sees argv; the worker writes the arguments into the
//     buffer the module exports, calls setup, and then frame per tick.
//     [StepLoop] is the frame function an [application.Application] yields.
//   - [InstallHostTransport] routes net/http through a host import on
//     wasip1, where there are no sockets; the worker answers it with a
//     synchronous request against the page's origin.
//
// The web/ directory holds the worker and the WASI shim the page loads, and
// scripts/dev/build_tab_bundle.sh assembles a servable directory from all
// of it. public/thestack/cmd/imzero2tab is the binary that links apps in.
package browserhost
