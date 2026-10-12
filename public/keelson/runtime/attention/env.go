package attention

import (
	"github.com/stergiotis/boxer/public/config/env"
)

// EscalateEnv is how long a your-turn item waits unacknowledged before its
// card opens on its own (ADR-0324 §SD4).
var EscalateEnv = env.NewDuration(env.Spec{
	Name:        "BOXER_ATTENTION_ESCALATE",
	Default:     "5m",
	Description: "how long a decision waits in the person's queue unacknowledged before its card opens on its own; an item this close to its expiry is marked; 0 never escalates",
	Category:    env.CategoryDev,
})

// FloodStreamEnv and FloodPersonEnv bound how many waiting items a stream,
// and all streams together, may add to the queue in ten minutes (ADR-0324
// §SD6).
var FloodStreamEnv = env.NewInt(env.Spec{
	Name:        "BOXER_ATTENTION_FLOOD_STREAM",
	Default:     "5",
	Description: "how many decisions one conversation may ask of the person in ten minutes; the next is refused with a reason the model can act on; 0 does not bound",
	Category:    env.CategoryDev,
})

var FloodPersonEnv = env.NewInt(env.Spec{
	Name:        "BOXER_ATTENTION_FLOOD_PERSON",
	Default:     "10",
	Description: "how many decisions all conversations together may ask of the person in ten minutes; the next is refused with a reason the model can act on; 0 does not bound",
	Category:    env.CategoryDev,
})

// AwayIdleEnv is how long the host waits without input before it sets the
// person Away (ADR-0328 §SD1).
var AwayIdleEnv = env.NewDuration(env.Spec{
	Name:        "BOXER_ATTENTION_AWAY_IDLE",
	Default:     "10m",
	Description: "how long the host waits without the person's input before it sets them Away: cards stop opening on their own and an away log is kept, shown when Away ends — at the next input; 0 never",
	Category:    env.CategoryDev,
})
