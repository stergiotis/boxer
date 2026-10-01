// Package agentfacts holds the action record of the app operations contract
// (ADR-0269 §SD9) as a `boxer.facts` kind: one row when the dispatcher
// decides a call and one when the call reaches its final phase.
package agentfacts

import (
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
)

// AgentAction is one action-record row. Id is the xxh3 of the natural key,
// task/key/decision, so the row is addressable; Ts is when it was written.
type AgentAction struct {
	_ struct{} `kind:"agentAction"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	// Kind's value is the label; its membership id is what a query filters on.
	Kind string `lw:"runtimeKindAgentAction,symbol"`
	// Task is the grant's task id; Conversation the coordinator's
	// conversation, when it named one.
	Task         string                `lw:"agentActionTask,symbol"`
	Conversation option.Option[string] `lw:"agentActionConversation,symbol"`
	// Actor is the coordinator app and ActorInstance its window.
	Actor         string `lw:"agentActionActor,symbol"`
	ActorInstance uint64 `lw:"agentActionActorInstance,u64Array,unit"`
	// ToolCallId is the caller's key for the call; CallId the dispatcher's.
	ToolCallId string `lw:"agentActionToolCallId,symbol"`
	CallId     string `lw:"agentActionCallId,symbol"`
	// Instance is the window the call addressed and App its app.
	Instance uint64 `lw:"agentActionInstance,u64Array,unit"`
	App      string `lw:"agentActionApp,symbol"`
	// Operation and Effect as the catalog declares them.
	Operation string `lw:"agentActionOperation,symbol"`
	Effect    string `lw:"agentActionEffect,symbol"`
	// ArgsDigest is a digest of the arguments, never the arguments.
	ArgsDigest string `lw:"agentActionArgsDigest,symbol"`
	// Decision is "dispatch" or "final"; Phase the phase at that point.
	Decision string `lw:"agentActionDecision,symbol"`
	Phase    string `lw:"agentActionPhase,symbol"`
	// Reason is the phase's reason: one element when there is one.
	Reason []string `lw:"agentActionReason,stringArray"`
	// BudgetLeft is the task's call budget after the call.
	BudgetLeft uint32 `lw:"agentActionBudgetLeft,u32Array,unit"`
	// Test marks a row of a test grant.
	Test bool `lw:"agentActionTest,bool"`
}
