---
type: explanation
audience: contributors
status: draft
---

> **Status: draft — pre-human-review.** Rendered by the capability
> inspector for the task.* cap. Refine as the M4 task monitor widget
> lands and a "task:supervisor-on" segment becomes a value of the
> status readout.

# task.* — background task primitive

[ADR-0038](../../../doc/adr/0038-keelson-background-task-primitive.md)
introduces a bus-protocol primitive for long-running, cancellable,
observable work. Apps inside the runtime use the high-level surface:

```go
func (inst *App) Mount(ctx app.MountContextI) error {
    inst.tasks = task.ForApp(ctx)
    ...
}

func (inst *App) someJob() {
    h, _ := inst.tasks.Spawn(callerCtx, task.SpawnOpts{
        Kind:        "demo.export",
        Title:       "Export rows",
        Cancellable: true,
    })
    // ... ticking loop with h.Report / h.Done / h.Error ...
}
```

`task.ForApp(MountCtx)` is the binding: the returned `TaskApiI`
auto-injects `OwnerAppId` / `OwnerTileKey` / `OwnerRunId` on every
spawn, composes the caller's context with the host's mount-cancel
channel so worker goroutines cascade-cancel on window close, and
tags the producer-side logger with `task_id` (on top of the per-app
logger's `run_id` / `app_id` / `instance_id` fields wired by the
windowhost).

**Subject family.** Flat per-task: `task.<id>.<verb>` with verbs
`created` / `progress` / `cancel` / `done` / `error`. A single
`task.>` subscription fans out every event to any observer; the
M3 supervisor adds a `task.list.inflight` request/reply that
returns a buscodec-encoded `task.InflightSnapshotReply` snapshot
of every running task.

**Backend today:**

- `runtime/task` — the producer surface (`HandleI`, `Spawn`,
  `TaskApiI`, `ForApp`) + the observer surface (`WatchAll`,
  `RequestCancel`, `ObserverI`) + the estimator subpackage that
  drives the humanized-change emission gate.
- `runtime/task/supervisor` — the opt-in audit + heartbeat layer.
  Subscribes `task.>`, persists every terminal-grade verb to
  `runtime.facts` via `factsstore.WriteLog` with structured fields
  (`task_id`, `task_kind`, `run_id`, `instance_id`, `duration_ms`,
  `reason`, `error_chain`). Promotes silent in-flight tasks to
  `InflightStateAbandoned` after `HeartbeatThresholdMs` (default
  30s). Serves `task.list.inflight` request/reply.

**Producer caps:** `task.>` Both — covers publishing the lifecycle
verbs and subscribing to per-task cancels. **Observer caps:**
`task.>` Sub. **Canceler caps:** `task.*.cancel` Pub. Helpers:
`task.ProducerCaps()`, `task.ObserverCaps()`, `task.CancelerCaps()`,
`supervisor.Caps()`, `supervisor.RequesterCaps()`.

**Where to look in the code:**

- `runtime/task/api.go` — `TaskApiI`, `BusApi`, `NoopTaskApi`, `ForApp`
- `runtime/task/handle.go` — `HandleI` + emission gate
- `runtime/task/supervisor/supervisor.go` — audit + heartbeat
- `apps/taskdemo/` — first consumer; demonstrates Spawn / Report /
  Cancel + the rolling history of finished tasks

**ADR reference:** ADR-0038 in full; ADR-0026 §SD3/§SD5 for the bus
contract this primitive rides on; ADR-0036 for the canonical CBOR
wire codec every payload uses.
