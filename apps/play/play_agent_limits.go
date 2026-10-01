package play

import (
	"net/url"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonsql"
)

// Play's agent limits (ADR-0270 §SD2): a run an agent's work causes is
// checked on the statement about to be sent — the residual, after the
// client-side rewrites — and fails before sending when the grant does not
// cover it. The limits fail a run; they never change it.

// AgentLimitError is a run an agent's work caused that the limits refused.
type AgentLimitError struct {
	Reason string
}

func (inst *AgentLimitError) Error() string { return "agent limit: " + inst.Reason }

// DestinationKeelson is how a grant names an introspection table.
func DestinationKeelson(table string) (name string) { return "keelson:" + table }

// DestinationClickHouse is how a grant names an endpoint, by host.
func DestinationClickHouse(host string) (name string) { return "clickhouse:" + host }

// checkAgentLimits checks a residual against the grant's destinations:
// a plain read, keelson() tables it lists, and an endpoint it lists unless
// the run goes to the host's introspection engine.
func checkAgentLimits(residual string, dec dispatchDecision, obo *app.OnBehalfOf) (err error) {
	pr, perr := nanopass.Parse(residual)
	if perr != nil {
		return &AgentLimitError{Reason: "the statement cannot be classified, so an agent cannot run it"}
	}
	class, witnesses, cerr := analysis.ClassifyQuerySecurity(pr)
	if cerr != nil || class != analysis.QuerySecurityRead {
		why := "the statement is " + class.String() + ", not a plain read"
		if len(witnesses) > 0 && witnesses[0].Name != "" {
			why += " (" + witnesses[0].Name + ")"
		}
		return &AgentLimitError{Reason: why}
	}
	for _, t := range keelsonsql.References(residual) {
		if !slices.Contains(obo.Destinations, DestinationKeelson(t)) {
			return &AgentLimitError{Reason: "the grant does not list " + DestinationKeelson(t)}
		}
	}
	if dec.class == dispatchClassIntrospection {
		return
	}
	host := endpointHost(dec.targetURL)
	if !slices.Contains(obo.Destinations, DestinationClickHouse(host)) {
		return &AgentLimitError{Reason: "the grant does not list " + DestinationClickHouse(host)}
	}
	return
}

// endpointHost is the host:port of an endpoint URL, or the URL itself when
// it does not parse.
func endpointHost(raw string) (host string) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return strings.TrimSpace(raw)
	}
	return u.Host
}
