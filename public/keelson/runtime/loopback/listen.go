package loopback

import (
	"context"
	"crypto/tls"
	"net"
	"slices"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Gate is the one place a TCP listener is bound (ADR-0082).
//
// A listener binds the address it is given, ":8080" and "0.0.0.0" included:
// which interfaces a process serves is the operator's choice, and a
// deployment that serves other machines is assumed to sit behind a reverse
// proxy that terminates TLS and authenticates. The gate does not check for
// that proxy and adds no authentication of its own. What it does:
//
//   - returns a failed bind instead of logging and dropping it;
//   - lists every listener it opened until it is closed ([OpenListeners]),
//     with what its bind address means: loopback, declared, peer-tls, or
//     undeclared — a non-loopback bind that says nothing about itself;
//   - refuses a non-loopback bind for a listener marked LoopbackOnly;
//   - serves TLS over PeerTLS, after checking that it authenticates every
//     peer.
//
// Codelint CS018 reports a direct net.Listen or http.ListenAndServe outside
// this package, so a listener does not go missing from the list.
type Gate struct {
	// Name names the listener in an error and in the list, e.g. "svgserver".
	Name string
	// LoopbackOnly marks a listener that serves this machine only — a debug
	// surface, a local daemon, a port probe. Any other bind address is
	// refused, whatever else is set.
	LoopbackOnly bool
	// Exposure says whom a listener serves beyond this machine and why. It
	// is what an audit of the listener list reads; a bind proceeds without
	// it. When set, both fields are required.
	Exposure *Exposure
	// PeerTLS is the TLS configuration of a listener that authenticates
	// its peers by client certificate. It must require and verify one
	// (tls.RequireAndVerifyClientCert with ClientCAs, or
	// tls.RequireAnyClientCert with VerifyPeerCertificate) and carry a
	// server certificate. Listen returns a TLS listener over this
	// configuration, so what the gate checked is what every accepted
	// connection goes through.
	PeerTLS *tls.Config
}

// Exposure is a listener's statement that it serves other machines.
type Exposure struct {
	// Serves names whom the listener serves, e.g. "devices on the local
	// network".
	Serves string
	// Reason says why they are served by this process.
	Reason string
}

// ExposureE names what a listener's bind address exposes it to.
type ExposureE uint8

const (
	// ExposureLoopback: the bind address is a loopback host.
	ExposureLoopback ExposureE = iota
	// ExposureUndeclared: a non-loopback bind with no declaration and no
	// peer authentication in this process; the deployment's proxy and
	// network are what stand in front of it.
	ExposureUndeclared
	// ExposureDeclared: a non-loopback bind with an Exposure declaration.
	ExposureDeclared
	// ExposurePeerTLS: a non-loopback bind whose peers are authenticated
	// over TLS.
	ExposurePeerTLS
)

func (inst ExposureE) String() (s string) {
	switch inst {
	case ExposureLoopback:
		return "loopback"
	case ExposureUndeclared:
		return "undeclared"
	case ExposureDeclared:
		return "declared"
	case ExposurePeerTLS:
		return "peer-tls"
	default:
		return "unknown"
	}
}

// Check says whether addr may be bound under this gate, without binding it.
func (inst Gate) Check(addr string) (err error) {
	_, err = inst.check(addr)
	return
}

func (inst Gate) check(addr string) (exposure ExposureE, err error) {
	host, _, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		err = eb.Build().Str("listener", inst.Name).Str("addr", addr).Errorf("bad listen address: %w", splitErr)
		return
	}
	if inst.PeerTLS != nil {
		if err = inst.checkPeerTLS(addr); err != nil {
			return
		}
	}
	if inst.Exposure != nil && (inst.Exposure.Serves == "" || inst.Exposure.Reason == "") {
		err = eb.Build().Str("listener", inst.Name).Str("addr", addr).Errorf("an exposure declaration names whom the listener serves and why; both are required")
		return
	}
	switch {
	case IsHost(host):
		exposure = ExposureLoopback
	case inst.LoopbackOnly:
		err = eb.Build().Str("listener", inst.Name).Str("addr", addr).Errorf("refusing non-loopback bind: this listener serves loopback only")
	case inst.PeerTLS != nil:
		exposure = ExposurePeerTLS
	case inst.Exposure != nil:
		exposure = ExposureDeclared
	default:
		exposure = ExposureUndeclared
	}
	return
}

// checkPeerTLS refuses a PeerTLS configuration that does not authenticate
// every peer or cannot serve.
func (inst Gate) checkPeerTLS(addr string) (err error) {
	c := inst.PeerTLS
	verified := (c.ClientAuth == tls.RequireAndVerifyClientCert && c.ClientCAs != nil) ||
		(c.ClientAuth >= tls.RequireAnyClientCert && c.VerifyPeerCertificate != nil)
	if !verified {
		err = eb.Build().Str("listener", inst.Name).Str("addr", addr).Stringer("clientAuth", c.ClientAuth).Errorf("the peer TLS configuration does not require and verify a client certificate (RequireAndVerifyClientCert with ClientCAs, or RequireAnyClientCert with VerifyPeerCertificate)")
		return
	}
	if len(c.Certificates) == 0 && c.GetCertificate == nil && c.GetConfigForClient == nil {
		err = eb.Build().Str("listener", inst.Name).Str("addr", addr).Errorf("the peer TLS configuration carries no server certificate")
	}
	return
}

// Listen binds a TCP listener on addr after [Gate.Check] passes, over TLS
// when PeerTLS is set. A bind failure is returned, never logged and
// dropped, so a caller that cannot listen does not run on as if it did.
func (inst Gate) Listen(ctx context.Context, addr string) (ln net.Listener, err error) {
	var exposure ExposureE
	exposure, err = inst.check(addr)
	if err != nil {
		return
	}
	var lc net.ListenConfig
	ln, err = lc.Listen(ctx, "tcp", addr)
	if err != nil {
		err = eb.Build().Str("listener", inst.Name).Str("addr", addr).Errorf("listen: %w", err)
		return
	}
	if inst.PeerTLS != nil {
		ln = tls.NewListener(ln, inst.PeerTLS)
	}
	info := ListenerInfo{Name: inst.Name, Addr: ln.Addr().String(), Exposure: exposure}
	switch exposure {
	case ExposureDeclared:
		info.Serves, info.Reason = inst.Exposure.Serves, inst.Exposure.Reason
	case ExposureUndeclared:
		info.Reason = "the operator's bind address; no peer authentication in this process"
	case ExposurePeerTLS:
		info.Reason = "peers authenticated by TLS client certificate"
	}
	ln = track(ln, info)
	return
}

// ListenerInfo is one open listener as [OpenListeners] reports it.
type ListenerInfo struct {
	Name     string
	Addr     string
	Exposure ExposureE
	// Serves and Reason come from an Exposure declaration; Reason also
	// says what an undeclared or peer-authenticated bind rests on.
	Serves string
	Reason string
}

var (
	openMu sync.Mutex
	open   []*trackedListener
)

// OpenListeners lists the listeners Gate.Listen opened in this process and
// that are not closed yet, in the order they were opened.
func OpenListeners() (out []ListenerInfo) {
	openMu.Lock()
	defer openMu.Unlock()
	out = make([]ListenerInfo, 0, len(open))
	for _, t := range open {
		out = append(out, t.info)
	}
	return
}

type trackedListener struct {
	net.Listener
	info ListenerInfo
	once sync.Once
}

func track(ln net.Listener, info ListenerInfo) (out net.Listener) {
	t := &trackedListener{Listener: ln, info: info}
	openMu.Lock()
	open = append(open, t)
	openMu.Unlock()
	return t
}

func (inst *trackedListener) Close() (err error) {
	inst.once.Do(func() {
		openMu.Lock()
		open = slices.DeleteFunc(open, func(t *trackedListener) bool { return t == inst })
		openMu.Unlock()
	})
	return inst.Listener.Close()
}
