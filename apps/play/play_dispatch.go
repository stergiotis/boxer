package play

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// play_dispatch.go — the E2 dispatch seam of
// doc/explanation/query-system-requirements.md.
//
// Which engine executes a query is a placement decision, and until now it
// lived in the user's head: whatever the endpoint switcher last pointed at
// was where everything went. This file names that decision instead of
// leaving it implicit. A resolver is consulted once per run with the
// finalized outgoing SQL; the decision it returns rides that one request
// and nothing else.
//
// Two properties are the point of the seam, and both are enforced by the
// types rather than by convention:
//
//   - Every issuer of a request takes a decision as a REQUIRED parameter,
//     so the compiler enumerates them. A path that reads the endpoint
//     without resolving cannot compile.
//   - The zero decision is invalid. Forgetting to resolve fails the run
//     loudly instead of silently falling back to some default endpoint.
//
// Boxer ships the seam and a resolver covering its own two endpoints; a
// system that needs placement maps, cluster rosters, or balancing replaces
// the resolver and publishes its data as introspection tables (E5). Site
// policy stays out of this file.

// dispatchClassE labels what kind of endpoint a decision names — the E9
// vocabulary, play-internal until a second consumer needs to share it.
//
// The zero value is dispatchClassUnknown, which pairs with the empty
// targetURL of an unresolved decision.
type dispatchClassE uint8

const (
	dispatchClassUnknown dispatchClassE = iota
	// dispatchClassManual is the endpoint the user pinned — whatever it
	// points at. Boxer does not inspect it.
	dispatchClassManual
	// dispatchClassIntrospection is this process's own loopback
	// introspection plane (ADR-0094). Its locality is a hard wall (R2):
	// live in-process state exists nowhere else.
	dispatchClassIntrospection
	// dispatchClassRefused means no endpoint may serve the statement, and
	// the reason says why. The run does not happen.
	dispatchClassRefused
)

var allDispatchClasses = []dispatchClassE{
	dispatchClassUnknown, dispatchClassManual, dispatchClassIntrospection, dispatchClassRefused,
}

func (inst dispatchClassE) String() (name string) {
	switch inst {
	case dispatchClassManual:
		name = "manual"
	case dispatchClassIntrospection:
		name = "introspection"
	case dispatchClassRefused:
		name = "refused"
	default:
		name = "unresolved"
	}
	return
}

// dispatchDecision is a resolver's answer for one run: where it executes,
// under what label, and a reason a human can read. The reason is not
// decoration — it is what the toolbar shows and what an audit of the run
// records (R12), so it should name the evidence, not restate the outcome.
type dispatchDecision struct {
	targetURL string
	class     dispatchClassE
	reason    string
	// member is the concrete member of the placement the run went to, when
	// the placement has members to choose between. Empty for boxer's own
	// endpoints, which have none: there the target IS the member.
	//
	// It is recorded because cancellation needs it. `KILL QUERY` reaches
	// only the member that ran the query (R11), and a decision that
	// remembered a cluster address instead would address the kill to
	// whichever member happened to answer — which is to say, usually not
	// the right one. An audit of the run wants the same fact (R12).
	//
	// A site resolver fills it, typically from
	// [queryengine.SelectMember](placement, affinity), which is the
	// deterministic (placement, generation) choice R4 asks for.
	member string
	// sensitivity is what the run touches (ADR-0145 §SD3), carried so the
	// engine can refuse independently of whatever placed the run, and so an
	// audit of the run records it (R12). Derived above the resolver, not by
	// it — see confine.
	sensitivity queryengine.SensitivityE
}

// killTarget names what a cancellation for this run must be addressed to:
// the resolved member where the placement had one, and the target endpoint
// otherwise.
//
// It exists so the "which host do I kill on" question has one answer rather
// than a convention every caller re-derives, and it is deliberately usable
// before anything in boxer calls it — the engine's ControlI is built and
// tested (queryengine/chserver), and the connection holder here still
// supersedes by run id instead, which is cheaper and needs no second
// request.
func (inst dispatchDecision) killTarget() (endpoint string, err error) {
	if inst.member != "" {
		endpoint = inst.member
		return
	}
	endpoint, err = inst.target()
	return
}

// target returns the endpoint the request goes to, or an error explaining
// why it must not be sent. Every issuer calls this rather than reading
// targetURL, so both refusal and the unresolved zero value are handled in
// one place.
func (inst dispatchDecision) target() (targetURL string, err error) {
	if inst.class == dispatchClassRefused {
		err = eb.Build().Str("dispatchReason", inst.reason).Errorf("play: not dispatched: %s", inst.reason)
		return
	}
	if inst.targetURL == "" {
		// A resolver never returns this; an unresolved zero value does.
		err = eh.Errorf("play: dispatch decision names no endpoint")
		return
	}
	targetURL = inst.targetURL
	return
}

// endpointResolverI decides where one run executes.
//
// residual is the finalized outgoing SQL — what the server will actually
// see, after the client-side rewrites. base is the manually pinned endpoint,
// which every resolver must be able to fall back to. affinity is the
// caller's read-consistency token (R4): runs sharing a token belong to one
// evaluation generation and must not be spread across members with
// different replication lag.
//
// Boxer carries the token and does not judge it, because its own endpoints
// have no members to choose between. A resolver that does have members
// judges it with [queryengine.SelectMember], the deterministic (placement,
// generation) function R4 asks for, and records the answer in the
// decision's member field — where cancellation and the audit can find it.
// The roster it selects from is site data and stays out of this repository.
//
// Implementations must be deterministic in their arguments and safe to call
// from any goroutine: the run path and the diagnostics probe resolve
// independently, and they are only guaranteed to agree because the same
// inputs produce the same decision.
type endpointResolverI interface {
	resolve(residual string, base string, affinity string) (dec dispatchDecision)
}

// staticResolver sends everything to the pinned endpoint. It is what play
// did before the seam existed, written down.
type staticResolver struct{}

var _ endpointResolverI = staticResolver{}

func (inst staticResolver) resolve(_ string, base string, _ string) (dec dispatchDecision) {
	dec = dispatchDecision{
		targetURL: base,
		class:     dispatchClassManual,
		reason:    "pinned endpoint",
	}
	return
}

// Dispatch resolves where the statement authored as sql will execute.
//
// It resolves from the residual — the SQL the request will actually carry —
// by running the same client-side rewrites the request runs. That costs one
// extra rewrite per run (the schema probes behind it are cached), and buys
// the property that no resolver ever judges a form the server will not see.
//
// Callers hand the result to ExecuteArrowStream or ProbeStatement. Because
// the decision is a function of the authored buffer alone, two issuers that
// start from the same buffer — a run and the diagnostic probe describing it
// — cannot reach different endpoints.
func (inst *Client) Dispatch(sql string, affinity string) (dec dispatchDecision) {
	residual, _ := inst.buildResidual(sql)
	dec = inst.dispatchResidual(residual, affinity)
	return
}

// dispatchResidual is Dispatch for a caller that already holds the residual
// and must not pay for a second rewrite — or, as with the diagnostics probe,
// must not resolve from the statement it is about to wrap.
func (inst *Client) dispatchResidual(residual string, affinity string) (dec dispatchDecision) {
	inst.mu.RLock()
	r := inst.resolver
	base := inst.targetURL
	inst.mu.RUnlock()
	if r == nil {
		r = staticResolver{}
	}
	dec = confine(residual, r.resolve(residual, base, affinity))
	inst.lastDecision.Store(&dec)
	return
}

// confine applies the ADR-0145 §SD4 locality wall, and does it ABOVE the
// resolver rather than inside one.
//
// That placement is the whole point. R2 says a router must be UNABLE to
// override a hard locality wall, so a check living inside the resolver
// would be bypassed by the static resolver, by a site resolver, and by
// whatever replaces them next. Here, every resolver's answer passes through
// it.
//
// The rule is narrow: a run naming sealed data may only be placed on this
// process's own introspection plane, which is exempt by IDENTITY rather
// than by address — the endpoint string was minted by a server this process
// started, which is not the same act as recognising a loopback address in a
// configured URL. Widening that to an engine which has PROVEN it can reach
// this plane is ADR-0145 §SD5, and is not built yet.
func confine(residual string, in dispatchDecision) (out dispatchDecision) {
	out = in
	if in.class == dispatchClassRefused {
		return
	}
	sealed := sealedNames(residual)
	if len(sealed) == 0 {
		return
	}
	out.sensitivity = queryengine.SensitivityConfined
	local := introspect.LocalQueryEndpoint()
	if local != "" && in.targetURL == local {
		return
	}
	out = dispatchDecision{
		class:       dispatchClassRefused,
		sensitivity: queryengine.SensitivityConfined,
		reason: "names sealed data (" + nameList(sealed) + ") that must not leave this box, and " +
			describeTarget(in.targetURL) + " is not this process's introspection plane",
	}
	return
}

// describeTarget names an endpoint for a refusal reason, or says there was
// none.
func describeTarget(targetURL string) (text string) {
	text = targetURL
	if text == "" {
		text = "an unresolved endpoint"
	}
	return
}

// LastDecision returns the most recently resolved decision, and whether
// anything has been resolved yet. It is what the toolbar reports: where the
// last query actually went and why — a fact about a run that happened, not
// a prediction about the buffer currently being typed. Predicting would
// mean running the client-side rewrites on the render thread every frame,
// and those can reach the network.
func (inst *Client) LastDecision() (dec dispatchDecision, ok bool) {
	p := inst.lastDecision.Load()
	if p == nil {
		return
	}
	dec = *p
	ok = true
	return
}

// describe renders a decision for the toolbar: where it went, and why. The
// member is named only when there was a choice to make — on boxer's own
// endpoints it would repeat the target.
func (inst dispatchDecision) describe() (text string) {
	where := inst.targetURL
	if inst.class == dispatchClassRefused {
		where = "refused"
	}
	if inst.member != "" {
		where += " (" + inst.member + ")"
	}
	text = where + "  · " + inst.reason
	if inst.sensitivity == queryengine.SensitivityConfined {
		text = "confined · " + text
	}
	return
}

// autoResolver is the resolver the Auto preset installs. Built once: the
// toolbar re-installs it every frame (SendRespVal lands a frame late, so
// change detection around it never fires), and boxing a fresh value into
// the interface each time would allocate for nothing.
var autoResolver endpointResolverI = keelsonResolver{}

// runsOnIntrospection reports whether sql would execute on the in-process
// introspection plane, and is what stamps the `endpoint:` frontmatter of a
// saved applet or a reopened playground buffer.
//
// It asks the resolver rather than comparing the client's URL against the
// local endpoint, because under Auto those disagree by design: the pinned
// base is no longer where queries go, so a buffer that in fact runs on the
// introspection plane would be stamped for the default target and come back
// somewhere its bare keelson('…') dialect does not work. Asking the resolver
// gives the same answer as the pinned comparison whenever Auto is off.
//
// MUST NOT be called from the render thread: resolving runs the client-side
// rewrites, whose schema probes can reach the network on a cold cache. Both
// call sites classify inside the goroutine that carries the request.
func (inst *PlayApp) runsOnIntrospection(sql string) (yes bool) {
	if inst.client == nil {
		return
	}
	ep := introspect.LocalQueryEndpoint()
	if ep == "" {
		return
	}
	dec := inst.client.Dispatch(sql, "")
	yes = dec.class == dispatchClassIntrospection || dec.targetURL == ep
	return
}

// SetResolver installs the endpoint resolver. A nil resolver restores the
// static one, which always answers with the pinned endpoint.
func (inst *Client) SetResolver(r endpointResolverI) {
	inst.mu.Lock()
	inst.resolver = r
	inst.mu.Unlock()
}
