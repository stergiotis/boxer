// Package agent is the host's side of the app operations contract
// (ADR-0269): the `runtime.agent.*` services through which a caller — a
// coordinator running a model's tool loop, or a scene or test holding a test
// grant — discovers what apps can do and calls their operations on the
// person's behalf.
//
// This package holds the service, its wire forms and the client a caller
// uses. The catalog an app declares lives on its manifest
// (app.OperationsCatalog); the typed helper apps declare it with is appops.
package agent

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

// ServiceAppId is the identity the host's service holds on the bus.
const ServiceAppId app.AppIdT = "runtime.agent"

// The family (ADR-0269 §SD3): one request/reply subject per service.
const (
	// SubjectPrefix precedes the service name.
	SubjectPrefix = "runtime.agent."
	// SubjectAll is the service's subscription pattern.
	SubjectAll = "runtime.agent.*"
	// SubjectDescribe answers an app's operations, or those matching a
	// search.
	SubjectDescribe = SubjectPrefix + "describe"
)

// TableOperations is the introspection table of every registered catalog
// (ADR-0269 §SD2).
const TableOperations = "app_operations"

// ServiceCaps is what the host's service holds.
func ServiceCaps() (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: SubjectAll, Direction: app.CapDirectionSub, Reason: "agent: serve runtime.agent requests"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "agent: reply to inboxes"},
	}
	return
}

// ClientCaps is what a caller declares in Manifest.Caps to reach the
// services. The bus check is necessary and not sufficient: every service
// past describe and request checks a task grant (ADR-0269 §SD6).
func ClientCaps(reason string) (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: SubjectAll, Direction: app.CapDirectionPub, Reason: reason, Sticky: true},
	}
	return
}
