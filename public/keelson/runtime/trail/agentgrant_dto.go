package trail

import "github.com/stergiotis/boxer/public/functional/option"

// AgentGrant is one event in the life of a task's grant (ADR-0277 §SD2):
// what a coordinator asked of the person, what the person decided, and how
// the task ended. The coordinator window is the row's [Origin], the task its
// [Delegation]; a request not yet granted has no task and carries none.
type AgentGrant struct {
	_ struct{} `kind:"agentGrant"`

	Id uint64 `lw:",id"`
	// Kind's value is the label; its membership id is what a query filters on.
	Kind string `lw:"runtimeKindAgentGrant,symbol"`
	// Event is what happened: see the GrantEvent constants.
	Event string `lw:"agentGrantEvent,symbol"`
	// Request is the coordinator's request the event answers — asked,
	// approved, widened or refused — so a request joins its decision and,
	// through it, the task it started.
	Request option.Option[string] `lw:"agentGrantRequest,stringArray,unit"`
	// Plan is the coordinator's one line and PlanDigest its digest, what
	// the person read when deciding; both empty for an event with no plan.
	Plan       string `lw:"agentGrantPlan,stringArray,unit"`
	PlanDigest string `lw:"agentGrantPlanDigest,stringArray,unit"`
	// Entries are the windows granted, "instance:app:mode[:operations]"
	// each; Launches the apps the task may open, "app:mode:count" each;
	// Destinations what agent-caused work may reach.
	Entries      []string `lw:"agentGrantEntries,stringArray"`
	Launches     []string `lw:"agentGrantLaunches,stringArray"`
	Destinations []string `lw:"agentGrantDestinations,stringArray"`
	// CallsBudget is the task's call budget and DeadlineMs its deadline in
	// Unix milliseconds, as they stood after the event.
	CallsBudget uint32 `lw:"agentGrantCallsBudget,u32Array,unit"`
	DeadlineMs  int64  `lw:"agentGrantDeadlineMs,i64Array,unit"`
	// DecidedBy is who decided: "person", "host" (a test grant, a refusal
	// the host made, a window or coordinator that closed), "coordinator" (a
	// request, a detach, a stop the model asked for, a lifted pause) or
	// "another task" (a pause another task's write caused). A "person"
	// arriving through the coordinator — its Stop button — is the
	// coordinator's claim, as [Cause] is; the dispatcher does not check it.
	DecidedBy string `lw:"agentGrantDecidedBy,symbol"`
	// Reason is the event's reason: one element when there is one.
	Reason []string `lw:"agentGrantReason,stringArray"`
}

// The grant events, the values of AgentGrant.Event.
const (
	// GrantEventRequested is a coordinator asking for a grant or a widening.
	GrantEventRequested = "requested"
	// GrantEventApproved is the decision that starts a task.
	GrantEventApproved = "approved"
	// GrantEventRefused is a request declined, by the person or by the host.
	GrantEventRefused = "refused"
	// GrantEventWidened is the decision that adds to a task's grant.
	GrantEventWidened = "widened"
	// GrantEventConfirmed is the person accepting a proposed command.
	GrantEventConfirmed = "confirmed"
	// GrantEventDeclined is the person rejecting a proposed command.
	GrantEventDeclined = "declined"
	// GrantEventMode is the person changing a task's mode in a window.
	GrantEventMode = "mode-changed"
	// GrantEventCeiling is the coordinator's settings setting or moving the
	// most the model may ask for (ADR-0280).
	GrantEventCeiling = "ceiling"
	// GrantEventDetached is a window leaving the grant: the person or the
	// coordinator detached it, or it closed. Reason names the window.
	GrantEventDetached = "detached"
	// GrantEventEnded is a task's end: stopped, closed or revoked.
	GrantEventEnded = "ended"
	// GrantEventPaused is a window the task had read changing under it: the
	// task's writes there wait for the coordinator's next turn (ADR-0269).
	// Written once per pause, not per change; Reason names the window.
	GrantEventPaused = "paused"
	// GrantEventResumed is the coordinator's turn lifting a pause in a
	// window, having listed the changes to the model.
	GrantEventResumed = "resumed"
)
