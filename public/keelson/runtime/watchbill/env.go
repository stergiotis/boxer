package watchbill

import "github.com/stergiotis/boxer/public/config/env"

// Poll is how often a worker reads the queue when no doorbell rings.
var Poll = env.NewDuration(env.Spec{
	Name:        "KEELSON_WATCHBILL_POLL",
	Description: "watchbill worker: interval between queue reads when no wake arrives",
	Category:    env.CategorySystem,
	Default:     "5s",
})

// AbandonAfter is how stale a holding run's heartbeat may be before its
// jobs are abandoned (ADR-0223 §SD4). Three runtime heartbeats.
var AbandonAfter = env.NewDuration(env.Spec{
	Name:        "KEELSON_WATCHBILL_ABANDON_AFTER",
	Description: "watchbill worker: a running job whose worker run has no heartbeat this recent is abandoned and re-queued",
	Category:    env.CategorySystem,
	Default:     "90s",
})

// Keep is how long a finished job row stays before the sweep deletes it.
var Keep = env.NewDuration(env.Spec{
	Name:        "KEELSON_WATCHBILL_KEEP",
	Description: "watchbill worker: finished job rows older than this are deleted; the event rows stay",
	Category:    env.CategorySystem,
	Default:     "168h",
})
