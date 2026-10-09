// Package llm is model inference as a keelson capability (ADR-0254): the
// `llm.<verb>` request/reply family an app sends text to a model through,
// and the host-side service that answers it over the repository's one
// chat-completion client.
//
// The family exists so that a model call is a declared, prompted, audited
// request with the app as sender, so that the provider is configured once
// per host rather than per app, and so that there is one place — the
// service — where "may this reach a model" is decided (ADR-0254 §SD3).
// An app that talks to a model shows no network capability at all; it
// holds an `llm.*` grant instead.
//
// A model's tool calls come back to the caller unexecuted (§SD5): the loop
// is the app's, and every tool runs through the bus client the app already
// holds, so the service never exercises a grant the app lacks.
package llm

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

// ServiceAppId is the identity the host's service holds on the bus, the
// shape of the runtime's other services.
const ServiceAppId app.AppIdT = "runtime.llm"

// The family (ADR-0254 §SD1): `llm.<verb>`, request/reply.
const (
	// SubjectPrefix precedes the verb.
	SubjectPrefix = "llm."
	// SubjectDescribe asks what the host offers: the model, the endpoint's
	// host, and what the client supports.
	SubjectDescribe = "llm.describe"
	// SubjectComplete is one chat completion.
	SubjectComplete = "llm.complete"
	// SubjectCancel stops a completion in flight: a publish, no reply,
	// naming the cancel key the request carried. Only the sender that
	// made the request can stop it.
	SubjectCancel = "llm.cancel"
	// SubjectAll is the service's subscription pattern.
	SubjectAll = "llm.*"

	// SubjectRetainPrefix precedes the retained verbs (ADR-0264 §SD1). Two
	// tokens, so no `llm.*` grant covers them: an app keeps text only by
	// declaring [RetainCaps], and the bus refuses the subject otherwise.
	SubjectRetainPrefix = "llm.retain."
	// SubjectRetainComplete is llm.complete whose conversation the service
	// keeps where the deployment permits it.
	SubjectRetainComplete = "llm.retain.complete"
	// SubjectRetainAll is the service's subscription pattern for them.
	SubjectRetainAll = "llm.retain.*"

	// SubjectRationPrefix precedes the moderator's verbs (ADR-0300 §SD4,
	// §SD7). Two tokens, so no `llm.*` grant covers them: only an app that
	// declares [ModeratorCaps] and is listed in BOXER_LLM_MODERATORS
	// reaches them.
	SubjectRationPrefix = "llm.ration."
	// SubjectRationSet sets or removes a rule.
	SubjectRationSet = "llm.ration.set"
	// SubjectRationList lists the rules and their state on each account.
	SubjectRationList = "llm.ration.list"
	// SubjectRationCancel cancels the calls in flight of a call id or an
	// account, those waiting in the queue included.
	SubjectRationCancel = "llm.ration.cancel"
	// SubjectRationAsk proposes a rule for the person to decide.
	SubjectRationAsk = "llm.ration.ask"
	// SubjectRationAll is the service's subscription pattern for them.
	SubjectRationAll = "llm.ration.*"

	// SubjectEventPrefix precedes what the service publishes for
	// moderators: one event per call admission decided, and one per soft
	// threshold a call crossed. Publish only; nothing replies.
	SubjectEventPrefix = "llm.event."
	// SubjectEventCall is one call: who it was charged to, how admission
	// decided it, and what it used.
	SubjectEventCall = "llm.event.call"
	// SubjectEventThreshold is a soft threshold a call crossed.
	SubjectEventThreshold = "llm.event.threshold"
	// SubjectEventAll is a moderator's subscription pattern.
	SubjectEventAll = "llm.event.*"
)

// The introspection tables of the ledger (ADR-0300 §SD3).
const (
	// TableUsage is use per account.
	TableUsage = "llm_usage"
	// TableRations is each rule's state on each account it applies to.
	TableRations = "llm_rations"
)

// TableCalls is the introspection table of completions this process has
// answered (ADR-0254 §SD4).
const TableCalls = "llm_calls"

// DefaultTimeout is BOXER_LLM_TIMEOUT's default: the service's bound on
// one completion when its config names none, and a client's when it cannot
// learn the service's. Generous, because local models are slow; the run is
// cancellable throughout.
const DefaultTimeout = 120 * time.Second

// ServiceCaps is what the host's service holds: the requests to serve and
// the inboxes to answer on. The provider itself is reached over the
// network from host-side code, which is where a network capability is
// expected.
func ServiceCaps() (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: SubjectAll, Direction: app.CapDirectionSub, Reason: "llm: serve describe and complete requests"},
		{Pattern: SubjectRetainAll, Direction: app.CapDirectionSub, Reason: "llm: serve retained complete requests"},
		{Pattern: SubjectRationAll, Direction: app.CapDirectionSub, Reason: "llm: serve the moderators' rule requests"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "llm: reply to inboxes"},
		{Pattern: SubjectEventAll, Direction: app.CapDirectionPub, Reason: "llm: publish usage and threshold events to moderators"},
	}
	return
}

// ModeratorCaps is what a moderator declares beside [ClientCaps] (ADR-0300
// §SD7): the rule verbs, and the events. The service also requires the
// app to be listed in BOXER_LLM_MODERATORS. reason names what the
// moderator does, e.g. "budget: throttle runaway agent loops".
func ModeratorCaps(reason string) (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: SubjectRationAll, Direction: app.CapDirectionPub, Reason: reason},
		{Pattern: SubjectEventAll, Direction: app.CapDirectionSub, Reason: reason},
	}
	return
}

// ClientCaps is what an app declares in Manifest.Caps. Not sticky
// (ADR-0254 §SD1): sending text off the box is a per-session consent the
// day a Mount-time prompt exists. reason names the app's purpose, e.g.
// "mdedit: transform the selection through a model".
func ClientCaps(reason string) (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: SubjectAll, Direction: app.CapDirectionPub, Reason: reason},
	}
	return
}

// RetainCaps is the second grant an app declares when it asks the host to
// keep its conversations (ADR-0264 §SD1), beside [ClientCaps]. Not sticky,
// as ClientCaps. Text is kept only where the deployment's ceiling,
// BOXER_LLM_RETAIN, is durable; below it a retained request is served and
// its reply says it was not kept. reason names what is kept and why, e.g.
// "chat: keep conversations on boxer.facts".
func RetainCaps(reason string) (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: SubjectRetainAll, Direction: app.CapDirectionPub, Reason: reason},
	}
	return
}
