package loopback

import (
	"context"
	"net"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Gate is one TCP listener's bind policy, the ADR-0082 §SD1 posture made the
// default for every listener boxer opens: a loopback bind proceeds; any other
// bind — an empty host (":8080" binds every interface), "0.0.0.0", a routable
// address, a name — is refused unless the operator opted in for this
// listener.
//
// Every listener in the tree binds through [Gate.Listen]; codelint CS018
// reports a direct net.Listen or http.ListenAndServe outside this package.
type Gate struct {
	// Name names the listener in a refusal, e.g. "svgserver".
	Name string
	// OptIn names the flag or registered environment variable that sets
	// AllowNonLoopback; a refusal carries it as the optIn field. Empty
	// means the listener has no opt-in and refuses every non-loopback bind.
	OptIn string
	// AllowNonLoopback is the operator's opt-in. It lifts the refusal only;
	// authentication and TLS stay the listener's own business.
	AllowNonLoopback bool
}

// Check says whether addr may be bound under this gate, without binding it.
func (inst Gate) Check(addr string) (err error) {
	host, _, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		err = eb.Build().Str("listener", inst.Name).Str("addr", addr).Errorf("bad listen address: %w", splitErr)
		return
	}
	if IsHost(host) {
		return
	}
	switch {
	case inst.OptIn == "":
		err = eb.Build().Str("listener", inst.Name).Str("addr", addr).Errorf("refusing non-loopback bind: this listener serves loopback only")
	case !inst.AllowNonLoopback:
		err = eb.Build().Str("listener", inst.Name).Str("addr", addr).Str("optIn", inst.OptIn).Errorf("refusing non-loopback bind: bind a loopback host, or set the listener's opt-in (optIn) to expose it beyond this machine")
	}
	return
}

// Listen binds a TCP listener on addr after [Gate.Check] allows it. A bind
// failure is returned, never logged and dropped, so a caller that cannot
// listen does not run on as if it did.
func (inst Gate) Listen(ctx context.Context, addr string) (ln net.Listener, err error) {
	err = inst.Check(addr)
	if err != nil {
		return
	}
	var lc net.ListenConfig
	ln, err = lc.Listen(ctx, "tcp", addr)
	if err != nil {
		err = eb.Build().Str("listener", inst.Name).Str("addr", addr).Errorf("listen: %w", err)
	}
	return
}
