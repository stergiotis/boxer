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

// IsolateAgentJobs routes an agent-caused enqueue into the reserved lane
// (ADR-0344 §SD3), where only a claim by id — a job child — takes it.
var IsolateAgentJobs = env.NewBool(env.Spec{
	Name:        "KEELSON_WATCHBILL_ISOLATE_AGENT_JOBS",
	Description: "watchbill worker: route a job an agent's call enqueued into the reserved lane that only `watchbill run --job <id>` drains, so it runs in a process of its own (ADR-0344)",
	Category:    env.CategorySystem,
	Default:     "false",
})

// LaneChildren bounds the job children the lane coordinator has alive at
// once (ADR-0344 §SD4); the pool's MaxConcurrent. Declared here, beside
// the worker's variables, so the registry's documentation sees it.
var LaneChildren = env.NewInt(env.Spec{
	Name:        "KEELSON_WATCHBILL_LANE_CHILDREN",
	Description: "watchbill lane: how many agent-caused jobs run at once, each in a child process of its own (ADR-0344)",
	Category:    env.CategorySystem,
	Default:     "2",
})

// LaneKillGrace is how long a job child has after SIGTERM before SIGKILL
// when the coordinator stops.
var LaneKillGrace = env.NewDuration(env.Spec{
	Name:        "KEELSON_WATCHBILL_LANE_KILL_GRACE",
	Description: "watchbill lane: time a job child has to exit after SIGTERM before it is killed, when the host stops",
	Category:    env.CategorySystem,
	Default:     "5s",
})
