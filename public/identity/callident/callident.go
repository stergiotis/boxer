// Package callident carries who a call is made for on its context
// (ADR-0295 §SD1): the run, app and window the host vouches for, and the
// principal, purpose and correlation the caller states. It is transport-
// neutral — executors stamp it into ClickHouse's log_comment, an observing
// executor copies it into its events — and it knows nothing about storage.
//
// The vouched/claimed split mirrors trail's Origin (ADR-0277 §SD1). In one
// process it is a convention, not an enforcement: any code holding a context
// can call [WithCallIdentity]. The vouching is as strong as the host's control
// over who builds the contexts its apps receive.
package callident

import "context"

// Origin is the part of a call identity the host vouches for: the run of the
// process, the app, and the app's window. Instance 0 is unattributed — a host
// service's own work, or a transport that carries no window.
type Origin struct {
	Run      string
	App      string
	Instance uint64
}

// Claims is the part of a call identity the caller states and nobody checks.
//
// Principal is opaque: it must be a pseudonymous reference, never a personal
// value such as a name or an email address. It rides log_comment into the
// server's query log, which the caller does not control and cannot erase from.
// Purpose is an opaque code from the consumer's own vocabulary. Correlation is
// free; a consumer uses it to tie the calls of one logical operation together.
type Claims struct {
	Principal   string
	Purpose     string
	Correlation string
}

// CallIdentity is who a call is made for.
type CallIdentity struct {
	Origin Origin
	Claims Claims
}

// IsZero reports whether nothing at all is set.
func (inst CallIdentity) IsZero() bool {
	return inst == CallIdentity{}
}

type ctxKey struct{}

// WithCallIdentity returns ctx carrying ci, replacing any identity already on
// it. It is the host's verb: it sets the origin as well as the claims.
func WithCallIdentity(ctx context.Context, ci CallIdentity) context.Context {
	return context.WithValue(ctx, ctxKey{}, ci)
}

// WithClaims returns ctx carrying c as its claims and keeping the origin
// already on ctx, so a caller stating a purpose cannot overwrite the run or
// the app the host set.
func WithClaims(ctx context.Context, c Claims) context.Context {
	ci, _ := CallIdentityFrom(ctx)
	ci.Claims = c
	return context.WithValue(ctx, ctxKey{}, ci)
}

// CallIdentityFrom returns the identity on ctx. ok is false when there is
// none, or when the one there is zero.
func CallIdentityFrom(ctx context.Context) (ci CallIdentity, ok bool) {
	ci, _ = ctx.Value(ctxKey{}).(CallIdentity)
	ok = !ci.IsZero()
	return
}
