package play

import (
	"net/url"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonsql"
)

// Play's agent limits (ADR-0270 §SD2): a run an agent's work causes is
// checked on the statement about to be sent — the residual, after the
// client-side rewrites — and fails before sending when the grant does not
// cover it. The limits fail a run; they never change it.

// AgentLimitError is a run an agent's work caused that the limits refused.
// Destination is set when what was missing is a destination of the grant.
type AgentLimitError struct {
	Reason      string
	Destination string
}

func (inst *AgentLimitError) Error() string { return "agent limit: " + inst.Reason }

// DestinationKeelson is how a grant names an introspection table.
func DestinationKeelson(table string) (name string) { return "keelson:" + table }

// DestinationClickHouse is how a grant names an endpoint, by host.
func DestinationClickHouse(host string) (name string) { return "clickhouse:" + host }

// checkAgentLimits checks a residual against the grant's destinations:
// a plain read, keelson() tables it lists, and an endpoint it lists unless
// the run goes to the host's introspection engine. aliasOf maps a bound
// dataset handle to its alias, which is what the grant lists.
func checkAgentLimits(residual string, dec dispatchDecision, obo *app.OnBehalfOf, aliasOf map[string]string) (err error) {
	pr, perr := nanopass.Parse(residual)
	if perr != nil {
		return &AgentLimitError{Reason: "the statement cannot be classified, so an agent cannot run it"}
	}
	class, witnesses, cerr := analysis.ClassifyQuerySecurity(pr)
	if cerr != nil || class != analysis.QuerySecurityRead {
		why := "the statement is " + class.String() + ", not a plain read"
		if len(witnesses) > 0 && witnesses[0].Name != "" {
			why += " (" + witnesses[0].Name + ": " + witnesses[0].Describe() + ")"
		}
		return &AgentLimitError{Reason: why}
	}
	for _, t := range keelsonsql.References(residual) {
		// A bound dataset reaches the residual as its ephemeral handle; the
		// grant names it by the alias the buffer wrote, which is also the
		// only name a refusal can ask the person for.
		name := t
		if alias, bound := aliasOf[t]; bound {
			name = alias
		}
		if !slices.Contains(obo.Destinations, DestinationKeelson(name)) && !slices.Contains(obo.Destinations, DestinationKeelson(t)) {
			return &AgentLimitError{Reason: "the grant does not list " + DestinationKeelson(name), Destination: DestinationKeelson(name)}
		}
	}
	if dec.class == dispatchClassIntrospection {
		return
	}
	host := endpointHost(dec.targetURL)
	if !slices.Contains(obo.Destinations, DestinationClickHouse(host)) {
		return &AgentLimitError{Reason: "the grant does not list " + DestinationClickHouse(host), Destination: DestinationClickHouse(host)}
	}
	return
}

// refuseAgentRun checks the buffer the way the run will, before the run is
// accepted, so a run the grant does not cover is refused with the
// destination it needs rather than failing after it was applied.
func (inst *PlayApp) refuseAgentRun(obo *app.OnBehalfOf) (err error) {
	if inst.client == nil {
		return
	}
	residual, _, _ := inst.client.rewriteFor(obo, inst.sql, nil)
	lerr := checkAgentLimits(residual, inst.client.previewDispatch(residual, ""), obo, inst.client.datasetAliasOf())
	if limit, ok := lerr.(*AgentLimitError); ok {
		if limit.Destination != "" {
			return app.RefuseForDestinations(lerr.Error(), limit.Destination)
		}
		return app.RefuseOperation(lerr.Error())
	}
	return
}

// rewriteFor is the client-side rewrite an agent's work may make: the whole
// rewrite for the person (obo nil) or under a grant that lists the endpoint,
// and otherwise [Client.buildResidualOffline]. catalog says which was made. validate_sql, trace_rewrite and the run's checks all
// take the rewrite from here, so an agent's work reaches the endpoint only
// under the grant (ADR-0270 §SD2) and what they report is what the run sends.
func (inst *Client) rewriteFor(obo *app.OnBehalfOf, sql string, observe func(passreg.ApplyObservation)) (residual string, params map[string]string, catalog bool) {
	catalog = obo == nil || slices.Contains(obo.Destinations, endpointDestination(inst))
	if catalog {
		residual, params = inst.buildResidualObserved(sql, observe)
	} else {
		residual, params = inst.buildResidualOffline(sql, observe)
	}
	return
}

// dispatchFor is [Client.Dispatch] resolved from the rewrite rewriteFor makes
// for obo, so the decision of an agent's run sends no probe its grant does not
// cover either. A nil obo is Dispatch.
func (inst *Client) dispatchFor(obo *app.OnBehalfOf, sql string, affinity string) (dec dispatchDecision) {
	residual, _, _ := inst.rewriteFor(obo, sql, nil)
	dec = inst.dispatchResidual(residual, affinity)
	return
}

// statementBounds refuses a statement validate_sql and trace_rewrite do not
// take: an empty one, or one past the 16 KiB bound.
func statementBounds(stmt string) (err error) {
	switch {
	case strings.TrimSpace(stmt) == "":
		return app.RefuseOperation("the buffer is empty; pass sql")
	case len(stmt) > schemaMaxStatement:
		return app.RefuseOperation("the statement is longer than 16 KiB")
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
