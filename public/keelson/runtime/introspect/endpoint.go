package introspect

import "sync/atomic"

// localQueryEndpoint holds the loopback `/query` URL of the introspection
// HTTP table source running in *this* OS process, or nil when none runs.
// It is the discovery hook a co-resident app (e.g. apps/play) reads to
// target the in-process server without a hard-coded port — the server binds
// an ephemeral port by default (ADR-0094 §SD3), so the address is only known
// at run time. Lock-free: one server per process is expected, and a second
// publisher simply overwrites the first (last-writer-wins).
var localQueryEndpoint atomic.Pointer[string]

// SetLocalQueryEndpoint records the in-process introspection `/query` URL.
// The host start hook calls this once the HTTP table source is listening;
// an empty string clears it (server stopped). See [LocalQueryEndpoint].
func SetLocalQueryEndpoint(url string) {
	localQueryEndpoint.Store(&url)
}

// LocalQueryEndpoint returns the `/query` URL of the introspection HTTP
// table source running in this process, or "" if none is running. A
// co-resident app reads it to offer "query keelson tables here" without
// knowing the bound port — see ADR-0094 §SD3/§SD6 and apps/play's endpoint
// switcher.
func LocalQueryEndpoint() (url string) {
	if p := localQueryEndpoint.Load(); p != nil {
		url = *p
	}
	return
}
