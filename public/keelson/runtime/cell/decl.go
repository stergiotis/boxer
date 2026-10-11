package cell

import (
	"net/url"
	"strings"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/loopback"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// SinkKindE is the kind of edge a sink is (ADR-0312 §SD1).
type SinkKindE uint8

const (
	SinkKindUnspecified SinkKindE = iota
	// SinkKindModelRoute is a remote model route: the one send an agent makes.
	SinkKindModelRoute
	// SinkKindEgress is the outbound half of an HTTP fetch to a destination
	// outside the cell.
	SinkKindEgress
	// SinkKindClipboard is the clipboard out of boxer.
	SinkKindClipboard
	// SinkKindExport is file export and printing.
	SinkKindExport
	// SinkKindPeer is sync to a peer cell, the person's act.
	SinkKindPeer
)

var sinkKindNames = []string{"unspecified", "model-route", "egress", "clipboard", "export", "peer"}

func (inst SinkKindE) String() (s string) {
	if int(inst) < len(sinkKindNames) {
		return sinkKindNames[inst]
	}
	return "unknown"
}

// Sink is one edge that leaves the declared cell: its kind, the edge it
// crosses (a host, a destination name, a peer), its clearance, and the
// wall that checks it.
type Sink struct {
	Kind      SinkKindE
	Edge      string
	Clearance agentrun.TierE
	// Check names the enforcement point: the service or record that
	// refuses above the clearance.
	Check string
}

// Clearances are the default clearances per sink kind.
type Clearances struct {
	ModelRoute agentrun.TierE
	Egress     agentrun.TierE
	Clipboard  agentrun.TierE
	Export     agentrun.TierE
}

// EgressDestination is a registered HTTP egress destination and whether
// its prefixes are loopback.
type EgressDestination struct {
	Name  string
	Local bool
}

// Peer is a peer cell and the clearance of what is sent to it.
type Peer struct {
	Name      string
	Clearance agentrun.TierE
}

// Canary is a planted token (ADR-0312 §SD6): a string that occurs only in
// content of the named scope, at the named tier, so its presence in an
// outbound payload is a leak the exact check finds.
type Canary struct {
	Scope string
	Token string
	Tier  agentrun.TierE
}

// Protected is a body whose verbatim copy must not leave below its tier:
// the shingle check finds a copy of ShingleSize words with no token
// planted. Declared by the host that holds the body; no environment form.
type Protected struct {
	Scope string
	Body  string
	Tier  agentrun.TierE
}

// Declaration is the cell as the deployment declares it.
type Declaration struct {
	// TrustedHosts are model hosts inside the cell by declaration
	// (BOXER_LLM_TRUSTED_HOSTS); ModelHost is the configured route's host
	// and ModelLocal whether it is loopback.
	TrustedHosts []string
	ModelHost    string
	ModelLocal   bool
	Egress       []EgressDestination
	Peers        []Peer
	// Managed says the organisation manages the host (ADR-0307 §SD6).
	Managed bool
	// ConfinedTier is the tier the compile-date confined bit maps to: the
	// deployment default content derived from sealed data carries at every
	// sink (ADR-0311 §SD1), what a harness reads instead of a constant.
	ConfinedTier agentrun.TierE
	Clearances   Clearances
	Approvals    ApprovalPolicy
	// Canaries and Protected are the leak checks every outbound payload is
	// held to (ADR-0312 §SD6); a hit above the sink's clearance refuses the
	// exit.
	Canaries  []Canary
	Protected []Protected
}

// Defaults is the declaration a host without any has: the compile-date
// walls' behaviour, in tiers — confined is AMBER, a remote route or
// destination clears GREEN, nothing is declared trusted.
func Defaults() (d Declaration) {
	return Declaration{ConfinedTier: agentrun.TierAmber,
		Clearances: Clearances{ModelRoute: agentrun.TierGreen, Egress: agentrun.TierGreen, Clipboard: agentrun.TierGreen, Export: agentrun.TierGreen},
		Approvals:  DefaultApprovalPolicy()}
}

// FromEnv reads the declaration from the registry, with the trusted hosts
// the llm service parsed. A variable that is set and does not parse fails
// closed: the tier variables fall to CLEAR, a bad approvals mapping to
// never for every tier, and the log says which (ADR-0312 §SD3: a policy
// that cannot be read must not open anything).
func FromEnv(trustedHosts []string) (d Declaration) {
	return FromEnvLogged(trustedHosts, zerolog.Nop())
}

// FromEnvLogged is FromEnv with the boot log for the values it refused.
func FromEnvLogged(trustedHosts []string, log zerolog.Logger) (d Declaration) {
	d = Defaults()
	d.TrustedHosts = trustedHosts
	d.Managed = ManagedEnv.Get()
	d.ConfinedTier = tierEnv(ConfinedTierEnv, d.ConfinedTier, agentrun.TierRed, log)
	d.Clearances.ModelRoute = tierEnv(RouteClearanceEnv, d.Clearances.ModelRoute, agentrun.TierClear, log)
	d.Clearances.Egress = tierEnv(EgressClearanceEnv, d.Clearances.Egress, agentrun.TierClear, log)
	d.Clearances.Clipboard = tierEnv(ClipboardClearanceEnv, d.Clearances.Clipboard, agentrun.TierClear, log)
	d.Clearances.Export = tierEnv(ExportClearanceEnv, d.Clearances.Export, agentrun.TierClear, log)
	d.Peers = ParsePeers(PeersEnv.Get())
	if raw, set := ApprovalsEnv.Lookup(); set {
		p, err := ParseApprovalPolicy(raw)
		if err != nil {
			log.Error().Err(err).Str("var", ApprovalsEnv.Spec().Name).Msg("cell: the approvals mapping does not parse; every tier above a clearance is never approved")
			p = ApprovalPolicy{}
		}
		d.Approvals = p
	}
	if raw, set := CanariesEnv.Lookup(); set {
		c, err := ParseCanaries(raw)
		if err != nil {
			log.Error().Err(err).Str("var", CanariesEnv.Spec().Name).Msg("cell: the canaries do not parse; none are planted")
			c = nil
		}
		d.Canaries = c
	}
	return
}

// tierEnvI is the read side of a categorial tier variable.
type tierEnvI interface {
	Lookup() (raw string, set bool)
	Spec() env.Spec
}

// tierEnv reads a tier variable case-insensitively; a value that is set
// and names no tier fails closed to closedTo and is logged. Closed is a
// direction, not a tier: a clearance fails low (CLEAR, nothing above it
// leaves) and a content tier fails high (RED, the content never leaves),
// so a typo in either narrows what goes out.
func tierEnv(v tierEnvI, fallback agentrun.TierE, closedTo agentrun.TierE, log zerolog.Logger) agentrun.TierE {
	raw, set := v.Lookup()
	if !set {
		return fallback
	}
	if t, ok := agentrun.ParseTier(strings.TrimSpace(raw)); ok {
		return t
	}
	log.Error().Str("var", v.Spec().Name).Str("value", raw).Msg("cell: not a TLP tier; failing closed to " + closedTo.String())
	return closedTo
}

func tierOf(name string, fallback agentrun.TierE) agentrun.TierE {
	if t, ok := agentrun.ParseTier(name); ok {
		return t
	}
	return fallback
}

// ParseCanaries reads "scope=token@TIER" triples separated by commas; a
// triple without a tier is RED, the most protected reading.
func ParseCanaries(s string) (canaries []Canary, err error) {
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		scope, rest, found := strings.Cut(part, "=")
		if !found || strings.TrimSpace(scope) == "" {
			err = eb.Build().Str("triple", part).Errorf("a canary is scope=token@TIER")
			return
		}
		token, tierName, hasTier := strings.Cut(rest, "@")
		token = strings.TrimSpace(token)
		if token == "" {
			err = eb.Build().Str("triple", part).Errorf("a canary names a token")
			return
		}
		tier := agentrun.TierRed
		if hasTier {
			t, ok := agentrun.ParseTier(strings.TrimSpace(tierName))
			if !ok {
				err = eb.Build().Str("tier", tierName).Errorf("no such TLP tier")
				return
			}
			tier = t
		}
		canaries = append(canaries, Canary{Scope: strings.TrimSpace(scope), Token: token, Tier: tier})
	}
	return
}

// ParsePeers reads "name=TIER" pairs; a pair without a tier gets CLEAR,
// an empty name is skipped.
func ParsePeers(s string) (peers []Peer) {
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, tier, _ := strings.Cut(part, "=")
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		peers = append(peers, Peer{Name: name, Clearance: tierOf(strings.TrimSpace(tier), agentrun.TierClear)})
	}
	return
}

// InsideCell says a model host is inside the cell: loopback, or declared
// trusted.
func (inst Declaration) InsideCell(host string) bool {
	h := hostname(host)
	if h == "" {
		return false
	}
	if loopback.IsHost(h) {
		return true
	}
	for _, t := range inst.TrustedHosts {
		if strings.EqualFold(strings.Trim(t, "[]"), h) {
			return true
		}
	}
	return false
}

// hostname is the host of an endpoint, a host:port or a bare name,
// without port or brackets.
func hostname(s string) (h string) {
	if strings.Contains(s, "://") {
		if u, err := url.Parse(s); err == nil {
			return u.Hostname()
		}
	}
	if u, err := url.Parse("http://" + s); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return strings.Trim(s, "[]")
}

// RouteClearance is the clearance of the model route to host: RED (inside
// the cell, nothing is withheld) when the host is inside, the declared
// route clearance otherwise.
func (inst Declaration) RouteClearance(host string) agentrun.TierE {
	if inst.InsideCell(host) {
		return agentrun.TierRed
	}
	return inst.Clearances.ModelRoute
}

// EgressClearance is the clearance of a destination: RED for a loopback
// one, the declared egress clearance otherwise.
func (inst Declaration) EgressClearance(local bool) agentrun.TierE {
	if local {
		return agentrun.TierRed
	}
	return inst.Clearances.Egress
}

// Sinks derives the registry (ADR-0312 §SD1): every edge boxer mediates
// that leaves the cell, with its clearance and its check.
func (inst Declaration) Sinks() (sinks []Sink) {
	if inst.ModelHost != "" && !inst.ModelLocal && !inst.InsideCell(inst.ModelHost) {
		sinks = append(sinks, Sink{Kind: SinkKindModelRoute, Edge: inst.ModelHost, Clearance: inst.Clearances.ModelRoute, Check: "llm sensitivity wall (ADR-0254 §SD3)"})
	}
	for _, d := range inst.Egress {
		if d.Local {
			continue
		}
		sinks = append(sinks, Sink{Kind: SinkKindEgress, Edge: d.Name, Clearance: inst.Clearances.Egress, Check: "httpegress wall (ADR-0262 §SD4)"})
	}
	sinks = append(sinks,
		Sink{Kind: SinkKindClipboard, Edge: "clipboard", Clearance: inst.Clearances.Clipboard, Check: "clipboard broker (ADR-0317)"},
		Sink{Kind: SinkKindExport, Edge: "export", Clearance: inst.Clearances.Export, Check: "export (ADR-0317 §SD4)"})
	for _, p := range inst.Peers {
		sinks = append(sinks, Sink{Kind: SinkKindPeer, Edge: p.Name, Clearance: p.Clearance, Check: "sync, the person's act (ADR-0307 §SD8)"})
	}
	return
}

// TierOf is the tier a payload carries at a sink: its declared tier, or
// the confined tier when it derives from sealed data and declares less.
func (inst Declaration) TierOf(declared agentrun.TierE, confined bool) agentrun.TierE {
	if confined && declared < inst.ConfinedTier {
		return inst.ConfinedTier
	}
	return declared
}
