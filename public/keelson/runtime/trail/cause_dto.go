package trail

import "github.com/stergiotis/boxer/public/functional/option"

// Cause names the model call whose reply asked for a tool call (ADR-0277
// §SD1): the call id (LlmCall.CallId), the provider's id for the tool call
// and its index in that reply. The coordinator states it; the dispatcher
// does not check it against the model service's record.
type Cause struct {
	_ struct{} `kind:"cause"`

	Id        uint64                `lw:",id"`
	ModelCall string                `lw:"trailCauseModelCall,stringArray,unit"`
	ToolCall  option.Option[string] `lw:"trailCauseToolCall,stringArray,unit"`
	ToolIndex uint32                `lw:"trailCauseToolIndex,u32Array,unit"`
}
