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
//   - [InstallHostTransport] routes net/http through host imports on
//     wasip1, where there are no sockets; the worker performs the request
//     against the page's origin, asynchronously unless it was made on the
//     goroutine running the current export.
//
// [Serve] serves such a directory during development: its files, `/ch/`
// proxied to ClickHouse so the data plane stays same-origin, and the sinks
// the worker posts to; files the directory lacks may come from assets the
// serving binary embeds. Package web embeds the worker, the WASI shim and
// the viewer page; package tabhost is a tab binary's body as a library
// (ADR-0278, proposed), and public/thestack/cmd/imzero2tab is boxer's tab
// binary over it. scripts/dev/build_tab_bundle.sh assembles a servable
// directory.
package browserhost
