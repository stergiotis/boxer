package trail

import "github.com/stergiotis/boxer/public/functional/option"

// Conversation ties a row into a conversation (ADR-0277 §SD1): the app's id
// for the conversation, the turn — one message the person sent and all that
// answering it took — and the round of the turn's tool loop. The app that
// owns the conversation states it; the host records it as stated.
type Conversation struct {
	_ struct{} `kind:"conversation"`

	Id           uint64                `lw:",id"`
	Conversation string                `lw:"trailConversation,stringArray,unit"`
	Turn         option.Option[string] `lw:"trailTurn,stringArray,unit"`
	Round        option.Option[uint32] `lw:"trailRound,u32Array,unit"`
}
