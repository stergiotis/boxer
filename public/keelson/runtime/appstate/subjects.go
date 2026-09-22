// Package appstate is the host side of the app-state manager's delete seam
// (ADR-0185 §SD3/§SD4): the `runtime.appstate.<op>` request/reply family
// and the service that answers it over the state store. Reading is not
// here — it is keelson('app_state'), and the family has no list verb so the
// same read does not live on two surfaces.
//
// Clearing another app's state is what no ordinary app can do, so the
// family is a declared, non-sticky capability the broker prompts for on
// every Mount, and every request is audited — including a refused one —
// because the bus records requests, and these are requests.
package appstate

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/appstaterequest"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

// ServiceAppId is the identity the host's service holds on the bus, the
// shape of the runtime's other services.
const ServiceAppId app.AppIdT = "runtime.appstate"

// The family (ADR-0185 §SD3): `runtime.appstate.<op>`, mutation-only. The
// payload is an appstaterequest.AppStateRequest, the reply an
// appstatereply.AppStateReply, and the op in the subject and in the payload
// must agree.
const (
	SubjectPrefix = "runtime.appstate."
	SubjectAll    = "runtime.appstate.*"
)

// SubjectDelete and SubjectForget are the two request subjects.
var (
	SubjectDelete = Subject(appstaterequest.OpDelete)
	SubjectForget = Subject(appstaterequest.OpForget)
)

// Subject is the request subject for op.
func Subject(op string) (subject string) { return SubjectPrefix + op }

// ServiceCaps is what the host's service holds: the requests to serve and
// the inboxes to answer on.
func ServiceCaps() (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: SubjectAll, Direction: app.CapDirectionSub, Reason: "appstate: serve delete and forget requests"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "appstate: reply to inboxes"},
	}
	return
}

// ClientCaps is what a manager app declares in Manifest.Caps. It is not
// sticky (ADR-0185 §SD3): a remembered, silent grant to clear every app's
// state is precisely what should not exist, and one prompt per session is
// proportionate for an app a user opens deliberately.
func ClientCaps() (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: SubjectAll, Direction: app.CapDirectionPub, Reason: "appstate: clear what other apps have stored — one entry, or everything one app keeps"},
	}
	return
}
