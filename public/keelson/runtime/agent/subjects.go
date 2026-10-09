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
	// SubjectHelp reads the inline help apps ship.
	SubjectHelp = SubjectPrefix + "help"
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
	// SubjectLaunch opens a window of an app the grant names.
	SubjectLaunch = SubjectPrefix + "launch"
	// SubjectArrange arranges windows on the desktop (ADR-0276 §SD3).
	SubjectArrange = SubjectPrefix + "arrange"
	// SubjectRaise brings one of the task's windows to the front.
	SubjectRaise = SubjectPrefix + "raise"
	// SubjectPlace sets the outer rect of one of the task's windows.
	SubjectPlace = SubjectPrefix + "place"
	// SubjectAuthority reads a task's ceiling and what its grant allows
	// under it, and moves the ceiling (ADR-0280).
	SubjectAuthority = SubjectPrefix + "authority"
	// SubjectDisclose records a view of a screenshot's pixels a
	// coordinator's model asked for (ADR-0287 §SD6).
	SubjectDisclose = SubjectPrefix + "disclose"
	// SubjectTurn starts a model turn: the changes by other writers since
	// the previous one, and the task's pauses lifted.
	SubjectTurn = SubjectPrefix + "turn"
	// SubjectEventPrefix precedes a task id: the task's events, without
	// content.
	SubjectEventPrefix = SubjectPrefix + "event."
	// SubjectEvents matches every task's events.
	SubjectEvents = SubjectEventPrefix + "*"
)

// TableGrants is the introspection table of task grants (ADR-0269 §SD6).
const TableGrants = "agent_grants"

// TableActions is the introspection table of action records (ADR-0269
// §SD9).
const TableActions = "agent_actions"

// TableOperations is the introspection table of every registered catalog
// (ADR-0269 §SD2).
const TableOperations = "app_operations"

// ActionsFileEnv names a file the headless host appends the action record
// to, one JSON line per row, so a run can be scored after the host exits
// (ADR-0269 M6). Honoured only on the headless host.
var ActionsFileEnv = env.NewString(env.Spec{
	Name:        "BOXER_AGENT_ACTIONS_FILE",
	Description: "file the headless host appends every runtime.agent action record to, as JSON lines, for trials (ADR-0269); honoured only on the headless host",
	Category:    env.CategoryDev,
})

// DeadlineEnv is how long a task runs before the person is asked for more
// time.
var DeadlineEnv = env.NewDuration(env.Spec{
	Name:        "BOXER_AGENT_DEADLINE",
	Default:     "30m",
	Description: "how long a runtime.agent task runs before its calls wait for the person to give it more time, and how much more an approval gives",
	Category:    env.CategoryDev,
})

// RequestTimeoutEnv is how long a grant request or a widening waits for the
// person before it expires.
var RequestTimeoutEnv = env.NewDuration(env.Spec{
	Name:        "BOXER_AGENT_REQUEST_TIMEOUT",
	Default:     "30m",
	Description: "how long a runtime.agent grant request or widening waits in the person's dialog before it expires; the coordinator's call waits as long",
	Category:    env.CategoryDev,
})

// CallsMinEnv and CallsMaxEnv bound the call budget the person picks for a
// new task in the host's dialog; a budget a coordinator asks for is clamped
// into the range.
var CallsMinEnv = env.NewInt(env.Spec{
	Name:        "BOXER_AGENT_CALLS_MIN",
	Default:     "20",
	Description: "the least call budget the person can give a new runtime.agent task in the host's approval dialog; a budget a coordinator asks for below it is raised to it",
	Category:    env.CategoryDev,
})

var CallsMaxEnv = env.NewInt(env.Spec{
	Name:        "BOXER_AGENT_CALLS_MAX",
	Default:     "1000",
	Description: "the largest call budget the person can give a new runtime.agent task in the host's approval dialog; a budget a coordinator asks for above it is lowered to it",
	Category:    env.CategoryDev,
})

// PaceEnv is the least time between two changes of a paced task that the
// person can see.
var PaceEnv = env.NewDuration(env.Spec{
	Name:        "BOXER_AGENT_PACE",
	Default:     "750ms",
	Description: "the least time between two visible changes — a change to a window, a window opened, an arrangement — of a runtime.agent task whose coordinator's ceiling does not let the model work unpaced",
	Category:    env.CategoryDev,
})

// TestGrantsEnv asks the host to issue task grants without the person's
// approval, for scenes and tests (ADR-0269 §SD6). The host honours it only
// on the headless host.
var TestGrantsEnv = env.NewBool(env.Spec{
	Name:        "BOXER_AGENT_TEST_GRANTS",
	Default:     "false",
	Description: "issue runtime.agent task grants without the person's approval, for scenes and tests (ADR-0269); honoured only on the headless host",
	Category:    env.CategoryDev,
})

// UnattendedEnv turns the unattended mode on (ADR-0298): the host decides
// grant requests, widenings and suggest-mode proposals in the person's
// place, inside the ceiling and the call budget and deadline knobs. It is
// honoured only by a binary built with the boxer_unattended tag
// (Unattended).
var UnattendedEnv = env.NewBool(env.Spec{
	Name:        "BOXER_AGENT_UNATTENDED",
	Default:     "false",
	Description: "turn on the unattended mode (ADR-0298): the host approves runtime.agent grant requests and widenings and accepts suggest-mode proposals in the person's place, within the coordinator's ceiling, BOXER_AGENT_CALLS_MAX and BOXER_AGENT_DEADLINE; a spent budget, a passed deadline and a consequential command still wait for the person; honoured only by a binary built with the boxer_unattended tag",
	Category:    env.CategoryDev,
})

// ServiceCaps is what the host's service holds.
func ServiceCaps() (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: SubjectAll, Direction: app.CapDirectionSub, Reason: "agent: serve runtime.agent requests"},
		{Pattern: opwire.Pattern, Direction: app.CapDirectionPub, Reason: "agent: call operations of instances"},
		{Pattern: app.SubjectInstanceClosed, Direction: app.CapDirectionSub, Reason: "agent: end a task when its coordinator closes"},
		{Pattern: SubjectEvents, Direction: app.CapDirectionPub, Reason: "agent: announce a task's events"},
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
		{Pattern: SubjectEvents, Direction: app.CapDirectionSub, Reason: reason + " (task events, without content)", Sticky: true},
	}
	return
}
