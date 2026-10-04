package trail

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
	// Plan is the coordinator's one line and PlanDigest its digest, what
	// the person read when deciding.
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
	// DecidedBy is who decided: "person", "host" (a test grant, a deadline)
	// or "coordinator" (a request, a stop).
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
	// GrantEventEnded is a task's end: stopped, closed or revoked.
	GrantEventEnded = "ended"
)
