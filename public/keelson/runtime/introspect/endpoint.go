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

// localSealed answers whether a table name in this process's introspection
// registry is a sealed dataset (ADR-0145 §SD3), or nil when no plane runs.
//
// It is published beside the endpoint, and for the same reason: a
// co-resident app has to be able to ask a question about the local plane
// that only the plane can answer. The narrower shape is deliberate — the
// registry itself stays private to the host, and what leaves is one
// predicate rather than the ability to enumerate or read anything.
var localSealed atomic.Pointer[func(name string) bool]

// SetLocalSealedPredicate records how to ask whether a local introspection
// table is a sealed dataset. The host start hook sets it alongside
// [SetLocalQueryEndpoint]; nil clears it (server stopped).
func SetLocalSealedPredicate(fn func(name string) (yes bool)) {
	if fn == nil {
		localSealed.Store(nil)
		return
	}
	localSealed.Store(&fn)
}

// IsLocalSealed reports whether name is a sealed dataset on this process's
// introspection plane.
//
// False when no plane runs, which is the safe reading here rather than a
// dangerous one: with no local plane there is no sealed data to confine,
// and a statement naming such a table cannot be served by anything anyway.
func IsLocalSealed(name string) (yes bool) {
	if p := localSealed.Load(); p != nil {
		yes = (*p)(name)
	}
	return
}

// LocalProbeI is the reachability-probe primitive of this process's plane
// (the E6 extension point): mint a single-use nonce URL, and afterwards ask
// whether it was fetched.
//
// A successful check proves that whatever fetched it could reach this
// process's loopback plane at that moment — nothing more. It is not a claim
// about the future, about other engines, or about any other address that
// engine might use. *introspecthttp.Server satisfies it.
type LocalProbeI interface {
	MintProbe() (nonce string, url string, err error)
	CheckProbe(nonce string) (fetched bool)
}

// localProbe holds this process's probe primitive, or nil when no plane
// runs. Published beside the endpoint for the same reason: proving that an
// engine can reach this plane is a question only this plane can pose.
var localProbe atomic.Pointer[LocalProbeI]

// SetLocalProbe records this process's probe primitive. The host start hook
// sets it alongside [SetLocalQueryEndpoint]; nil clears it.
//
// Pass an untyped nil to clear, never a typed one. A `(*Server)(nil)` is not
// == nil once it is inside an interface, so it would be STORED and then
// panic at the first MintProbe — the typed-nil trap the runtime's wiring
// already guards against for the dataset decryptor. Hand over a value only
// when the server actually started.
func SetLocalProbe(p LocalProbeI) {
	if p == nil {
		localProbe.Store(nil)
		return
	}
	localProbe.Store(&p)
}

// LocalProbe returns this process's probe primitive. ok is false when no
// plane runs, and a caller that needs a proof must then treat the engine as
// unproven — there is nothing to prove reachability TO.
func LocalProbe() (p LocalProbeI, ok bool) {
	if ptr := localProbe.Load(); ptr != nil {
		p, ok = *ptr, true
	}
	return
}
