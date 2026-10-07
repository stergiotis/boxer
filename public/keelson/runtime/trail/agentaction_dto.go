package trail

// AgentAction is one row of the action record (ADR-0269 §SD9): one when the
// dispatcher decides a call and one when the call reaches its final phase.
// The coordinator window is the row's [Origin]; task, epoch and the
// dispatcher's call id its [Delegation]; the conversation its
// [Conversation]; the model call that asked for it its [Cause].
type AgentAction struct {
	_ struct{} `kind:"agentAction"`

	Id uint64 `lw:",id"`
	// Kind's value is the label; its membership id is what a query filters on.
	Kind string `lw:"runtimeKindAgentAction,symbol"`
	// Key is the coordinator's key for the call (ADR-0277 §SD6): a repeat
	// under one task returns the first outcome.
	Key string `lw:"agentActionKey,stringArray,unit"`
	// Instance is the window the call addressed and App its app.
	Instance uint64 `lw:"agentActionInstance,u64Array,unit"`
	App      string `lw:"agentActionApp,symbol"`
	// Operation and Effect as the catalog declares them.
	Operation string `lw:"agentActionOperation,symbol"`
	Effect    string `lw:"agentActionEffect,symbol"`
	// ArgsDigest is a digest of the arguments, never the arguments.
	ArgsDigest string `lw:"agentActionArgsDigest,stringArray,unit"`
	// Decision is "dispatch" or "final"; Phase the phase at that point.
	Decision string `lw:"agentActionDecision,symbol"`
	Phase    string `lw:"agentActionPhase,symbol"`
	// Reason is the phase's reason: one element when there is one.
	Reason []string `lw:"agentActionReason,stringArray"`
	// CallTitle and CallReason are the model's one-line title for the call
	// and the reason it gave, bounded, as the person was shown them: one
	// element each when given, on the dispatch row. They are kept whatever
	// BOXER_LLM_RETAIN says, as the grant's plan is; the coordinator states
	// them.
	CallTitle  []string `lw:"agentActionCallTitle,stringArray"`
	CallReason []string `lw:"agentActionCallReason,stringArray"`
	// BudgetLeft is the task's call budget after the call.
	BudgetLeft uint32 `lw:"agentActionBudgetLeft,u32Array,unit"`
	// Test marks a row of a test grant.
	Test bool `lw:"agentActionTest,bool"`
	// Tainted says the conversation had read untrusted content by then;
	// Confined that the outcome carried confined content (ADR-0269 §SD7).
	Tainted  bool `lw:"agentActionTainted,bool"`
	Confined bool `lw:"agentActionConfined,bool"`
	// Consent is the grant destination that admitted a consequential call
	// without the person's confirmation (ADR-0288 (proposed) §SD4): one
	// element when one did.
	Consent []string `lw:"agentActionConsent,stringArray"`
}
