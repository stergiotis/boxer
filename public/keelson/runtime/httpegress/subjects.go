// Package httpegress is HTTP egress as a keelson capability (ADR-0262): the
// `net.http.fetch.<destination>` request/reply family a hosted app fetches
// a URL through, the host-wide registry of destinations those subjects
// name, and the host-side service that holds the transports.
//
// A destination is a name for a set of URL prefixes together with the
// transport policy that reaches them — trust, user agent, timeout, body
// cap — configured once in host code. An app declares the destinations it
// uses in its manifest ([ClientCaps]); the bus's publish check is the
// enforcement, and the service matches every URL against the named
// destination's prefixes before anything leaves. An app that fetches
// through this family shows no network capability of its own.
package httpegress

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

// ServiceAppId is the identity the host's service holds on the bus.
const ServiceAppId app.AppIdT = "runtime.http"

// The family (ADR-0262 §SD1): one fetch verb per destination.
const (
	// SubjectPrefix precedes the destination name.
	SubjectPrefix = "net.http.fetch."
	// SubjectAll is the service's subscription pattern.
	SubjectAll = "net.http.fetch.*"
)

// TableCalls is the introspection table of fetches this process answered
// or refused (ADR-0262 §SD5).
const TableCalls = "http_calls"

// TableDestinations is the introspection table of registered destinations
// as the host resolved them (ADR-0262 §SD2).
const TableDestinations = "http_destinations"

// DefaultTimeout bounds one fetch when neither the destination nor the
// caller's context says otherwise.
const DefaultTimeout = 30 * time.Second

// DefaultMaxBodyBytes caps a reply body when the destination names no cap:
// a reply is one bus message, so the body travels whole.
const DefaultMaxBodyBytes int64 = 8 << 20

// Subject is the fetch subject for a destination.
func Subject(destination string) (subject string) { return SubjectPrefix + destination }

// ServiceCaps is what the host's service holds: the requests to serve and
// the inboxes to answer on.
func ServiceCaps() (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: SubjectAll, Direction: app.CapDirectionSub, Reason: "http: serve fetch requests"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "http: reply to inboxes"},
	}
	return
}

// ClientCaps is what an app declares in Manifest.Caps for one destination.
// Sticky (ADR-0262 §SD1): a server the deployment configured is not a
// per-session consent. reason names the app's purpose, e.g. "play: basemap
// tiles".
func ClientCaps(destination string, reason string) (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: Subject(destination), Direction: app.CapDirectionPub, Reason: reason, Sticky: true},
	}
	return
}
