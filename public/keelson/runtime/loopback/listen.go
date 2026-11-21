package loopback

import (
	"context"
	"crypto/tls"
	"net"
	"slices"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Gate is one TCP listener's bind policy, the ADR-0082 §SD1 posture made the
// default for every listener boxer opens.
//
// A loopback bind proceeds. Any other bind — an empty host (":8080" binds
// every interface), "0.0.0.0", a routable address, a name — is refused
// unless one of three things lifts the refusal for this listener:
//
//   - the operator opted in (AllowNonLoopback, named by OptIn);
//   - the listener exists to serve other machines and says whom and why
//     (Exposure);
//   - the listener authenticates every peer by its TLS client certificate
//     (PeerTLS), which Listen then enforces.
//
// Every listener in the tree binds through [Gate.Listen]; codelint CS018
// reports a direct net.Listen or http.ListenAndServe outside this package.
// Each listener Listen opens is listed by [OpenListeners] until it is
// closed.
type Gate struct {
	// Name names the listener in a refusal, e.g. "svgserver".
	Name string
	// OptIn names the flag or registered environment variable that sets
	// AllowNonLoopback; a refusal carries it as the optIn field. Empty
	// means the listener has no opt-in.
	OptIn string
	// AllowNonLoopback is the operator's opt-in. It lifts the refusal only;
	// authentication and TLS stay the listener's own business.
	AllowNonLoopback bool
	// Exposure declares a listener whose purpose is to serve other
	// machines. Its non-loopback bind proceeds without an opt-in; the
	// declaration is what an audit reads. Both fields must be set.
	Exposure *Exposure
	// PeerTLS is the TLS configuration of a listener that authenticates
	// its peers by client certificate. It must require and verify one
	// (tls.RequireAndVerifyClientCert with ClientCAs, or
	// tls.RequireAnyClientCert with VerifyPeerCertificate) and carry a
	// server certificate; then a non-loopback bind proceeds, and Listen
	// returns a TLS listener over this configuration, so what the gate
	// checked is what every accepted connection goes through.
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

// ExposureE names what let a listener's bind proceed.
type ExposureE uint8

const (
	// ExposureLoopback: the bind address is a loopback host.
	ExposureLoopback ExposureE = iota
	// ExposureOptIn: the operator's AllowNonLoopback.
	ExposureOptIn
	// ExposureDeclared: the listener's Exposure declaration.
	ExposureDeclared
	// ExposurePeerTLS: the listener authenticates its peers over TLS.
	ExposurePeerTLS
)

func (inst ExposureE) String() (s string) {
	switch inst {
	case ExposureLoopback:
		return "loopback"
	case ExposureOptIn:
		return "opt-in"
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
	case inst.PeerTLS != nil:
		exposure = ExposurePeerTLS
	case inst.Exposure != nil:
		exposure = ExposureDeclared
	case inst.OptIn == "":
		err = eb.Build().Str("listener", inst.Name).Str("addr", addr).Errorf("refusing non-loopback bind: this listener serves loopback only")
	case !inst.AllowNonLoopback:
		err = eb.Build().Str("listener", inst.Name).Str("addr", addr).Str("optIn", inst.OptIn).Errorf("refusing non-loopback bind: bind a loopback host, or set the listener's opt-in (optIn) to expose it beyond this machine")
	default:
		exposure = ExposureOptIn
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

// Listen binds a TCP listener on addr after [Gate.Check] allows it, over
// TLS when PeerTLS is set. A bind failure is returned, never logged and
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
	case ExposureOptIn:
		info.Reason = "operator opt-in " + inst.OptIn
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
	// names the opt-in or the peer authentication that let a bind proceed.
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
