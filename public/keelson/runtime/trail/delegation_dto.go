package trail

import "github.com/stergiotis/boxer/public/functional/option"

// Delegation names the agent task a row is work of (ADR-0277 §SD1): the
// task, its epoch, and the dispatcher's call. On an agent action the call is
// the action's own; on what the action caused — a query run, a launch, a
// fetch, a nested completion — it is the call that caused it, so the two
// join on (Task, Call). The dispatcher stamps it through the on-behalf-of
// context (ADR-0269 §SD6).
type Delegation struct {
	_ struct{} `kind:"delegation"`

	Id    uint64                `lw:",id"`
	Task  string                `lw:"trailTask,stringArray,unit"`
	Epoch uint64                `lw:"trailTaskEpoch,u64Array,unit"`
	Call  option.Option[string] `lw:"trailCall,stringArray,unit"`
}
