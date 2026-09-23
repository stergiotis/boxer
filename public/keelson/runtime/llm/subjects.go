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
	// SubjectAll is the service's subscription pattern.
	SubjectAll = "llm.*"
)

// TableCalls is the introspection table of completions this process has
// answered (ADR-0254 §SD4).
const TableCalls = "llm_calls"

// DefaultTimeout bounds one completion when neither the request's context
// nor Client.Timeout says otherwise. Generous, because local models are
// slow; the run is cancellable throughout.
const DefaultTimeout = 120 * time.Second

// ServiceCaps is what the host's service holds: the requests to serve and
// the inboxes to answer on. The provider itself is reached over the
// network from host-side code, which is where a network capability is
// expected.
func ServiceCaps() (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: SubjectAll, Direction: app.CapDirectionSub, Reason: "llm: serve describe and complete requests"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "llm: reply to inboxes"},
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
