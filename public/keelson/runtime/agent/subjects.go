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
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

// ServiceAppId is the identity the host's service holds on the bus; it is
// the dispatcher the window host accepts operation calls from.
const ServiceAppId app.AppIdT = opwire.DispatcherAppId

// The family (ADR-0269 §SD3): one request/reply subject per service.
const (
	// SubjectPrefix precedes the service name.
	SubjectPrefix = "runtime.agent."
	// SubjectAll is the service's subscription pattern.
	SubjectAll = "runtime.agent.*"
	// SubjectDescribe answers an app's operations, or those matching a
	// search.
	SubjectDescribe = SubjectPrefix + "describe"
	// SubjectRequest asks for a task grant.
	SubjectRequest = SubjectPrefix + "request"
	// SubjectList lists the task's instances.
	SubjectList = SubjectPrefix + "list"
	// SubjectCall calls one command or query.
	SubjectCall = SubjectPrefix + "call"
	// SubjectStatus reads a call's or a job's phase by key.
	SubjectStatus = SubjectPrefix + "status"
	// SubjectCancel withdraws a queued call by key.
	SubjectCancel = SubjectPrefix + "cancel"
	// SubjectRead reads a result reference or an artifact handle.
	SubjectRead = SubjectPrefix + "read"
	// SubjectCapture captures an instance's window.
	SubjectCapture = SubjectPrefix + "capture"
	// SubjectDetach removes one instance from the task.
	SubjectDetach = SubjectPrefix + "detach"
	// SubjectStop ends the task.
	SubjectStop = SubjectPrefix + "stop"
)

// TableGrants is the introspection table of task grants (ADR-0269 §SD6).
const TableGrants = "agent_grants"

// TableActions is the introspection table of action records (ADR-0269
// §SD9).
const TableActions = "agent_actions"

// TableOperations is the introspection table of every registered catalog
// (ADR-0269 §SD2).
const TableOperations = "app_operations"

// TestGrantsEnv asks the host to issue task grants without the person's
// approval, for scenes and tests (ADR-0269 §SD6). The host honours it only
// on the headless host.
var TestGrantsEnv = env.NewBool(env.Spec{
	Name:        "BOXER_AGENT_TEST_GRANTS",
	Default:     "false",
	Description: "issue runtime.agent task grants without the person's approval, for scenes and tests (ADR-0269); honoured only on the headless host",
	Category:    env.CategoryDev,
})

// ServiceCaps is what the host's service holds.
func ServiceCaps() (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: SubjectAll, Direction: app.CapDirectionSub, Reason: "agent: serve runtime.agent requests"},
		{Pattern: opwire.Pattern, Direction: app.CapDirectionPub, Reason: "agent: call operations of instances"},
		{Pattern: app.SubjectInstanceClosed, Direction: app.CapDirectionSub, Reason: "agent: end a task when its coordinator closes"},
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
