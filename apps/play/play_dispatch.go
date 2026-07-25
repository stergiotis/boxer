package play

import (
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
// different replication lag. Boxer carries the token; its own endpoints
// have no members to choose between, so nothing here judges it.
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
	dec = r.resolve(residual, base, affinity)
	return
}

// SetResolver installs the endpoint resolver. A nil resolver restores the
// static one, which always answers with the pinned endpoint.
func (inst *Client) SetResolver(r endpointResolverI) {
	inst.mu.Lock()
	inst.resolver = r
	inst.mu.Unlock()
}
