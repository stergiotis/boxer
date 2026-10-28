---
type: how-to
audience: developer giving an app or a headless binary durable work to do
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-08
---

# How to run a job on watchbill

The task-oriented walk: write a handler for a kind, put a job on the queue
from an app or from a shell, watch it, and act on it. Why it is shaped this
way is [ADR-0223](../adr/0223-watchbill-durable-work-on-facts.md) (the
table and the claim) and
[ADR-0234](../adr/0234-watchbill-client-protocol-and-worker-presence.md)
(the client protocol). If you know River, the verbs are River's.

## 1. Write a handler and register it

A handler runs the jobs of one kind. It receives the job row as claimed and
the keelson task the run is reported through; return `nil` to succeed, an
error to fail the attempt under the job's policy, and return promptly once
`ctx` is done — that is the cancel or the timeout. Delivery is
at-least-once, so a handler interrupted mid-way is run again and must
tolerate it.

```go
package tender

import (
    "context"

    "github.com/stergiotis/boxer/public/keelson/runtime/task"
    "github.com/stergiotis/boxer/public/keelson/runtime/watchbill"
    "github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
)

func init() {
    _ = watchbill.Register(watchbill.HandlerFunc{
        KindName: "tender.download",
        RunFunc: func(ctx context.Context, job watchbillstore.Job, h task.HandleI) error {
            // job.Subject is the id of the row in your own table that says
            // what to download. Report progress on h; check ctx.
            return download(ctx, job.Subject, h)
        },
    })
}
```

Registration is process-wide: every binary that links this package has the
handler, and every worker in such a binary drains the kind. A job of a kind
no running binary links sits `queued` — `keelson('watchbill_worker')` lists
every worker run on the cell with what it serves and whether it is alive,
and the Watchbill window's bottom line shows the live ones.

Prefer `Subject` over args. When there is no row of your own to point at,
attach a generated facts DTO with `watchbill.WithArgs` and read it back with
`watchbill.ArgsOf`; the kind is taken from the codec, so a job cannot claim
a kind its bytes are not.

## 2. Enqueue from an app

An app declares the capability in its manifest and gets the queue through
its bus client. Nothing else is owed: the worker rings its own doorbell and
attributes the job to the sender.

```go
Caps: append(watchbill.ClientCaps(), task.ObserverCaps()...),
```

```go
func (a *App) Mount(ctx app.MountContextI) error {
    a.queue = watchbill.NewClient(ctx.Bus())
    return nil
}

job, err := a.queue.Enqueue(watchbill.Request{
    Kind: "tender.download", Subject: downloadRowId,
    MaxAttempts: 3, Backoff: watchbillstore.BackoffExponential, BackoffBase: 10 * time.Second,
    Timeout: 10 * time.Minute,
})
```

If the enqueue happens inside a command handler that an agent's call is
running, copy the call's context onto the request so the job is attributed
to the agent and runs under the agentic entry frame (section 5):

```go
job, err := a.queue.Enqueue(watchbill.Request{
    Kind: "tender.download", Subject: downloadRowId,
    OnBehalfOf: call.OnBehalfOf, // nil on the person's own path; the worker attests it
})
```

The other verbs are `Cancel(id, note)`, `Retry(id, note)`, `Get(id)` and
`List(states, kinds, limit)`. A running job is also a keelson task with the
job's id, so the task monitor's cancel button and the client's `Cancel` are
the same transition.

From a process that holds the store — a host service, a test over
`watchbill.MemStore` — `watchbill.Submit` is the same enqueue plus wake.

`apps/watchbilldemo` is the in-tree example of this section end to end: a
handler for its own kind, the client on the mount context's bus, and the
task monitor beside the job list.

## 3. Enqueue from a shell, and stand a headless worker

The CLI group targets the ClickHouse server the `CLICKHOUSE_*` variables
name and provisions the tables on first use.

```sh
./boxer.sh watchbill add tender.download dl-77 --max-attempts 3 --backoff exponential
./boxer.sh watchbill list --state queued --state running
./boxer.sh watchbill cancel <id> --note "not needed"
./boxer.sh watchbill retry <id>
```

A machine with no window host stands a worker with the handlers its binary
links. It writes its heartbeat to the facts store on the same server, so a
host's worker on the cell does not take its jobs for abandoned, and it drains
only the queues you name, or every queue when you name none.

```sh
./boxer.sh watchbill run --queue bulk --max-workers 4
```

## 4. Watch it

- **Manage** — the Watchbill window (`apps/watchbill`): the queue across
  kinds, a job's fields and trail, cancel and retry, the worker of the
  process. Another app opens it on a job with a `watchbillLaunch` config
  carrying the id.
- **In flight, failures, one job's timeline, workers** — the `watchbill`
  applet book in sqlapplet, over `keelson('watchbill')`,
  `keelson('watchbill_event')` and `keelson('watchbill_worker')`. It needs
  no external ClickHouse.
- **Live progress and cancel** — the task monitor, since every run is a
  task.
- **From SQL** — the same three tables in play, on the introspection
  endpoint.

## 5. Who caused a job: the delegation and the entry frame

**Every handler runs under one of two named stack frames.** The worker calls
`HandlerI.Run` through `entrypoint.Agentic` when the job row carries an
attested delegation and through `entrypoint.Human` otherwise; the operations
engine does the same for a command's `ApplyCommand`. The frame is on the
stack for the whole call, so any stack trace taken inside your handler — a
panic, `debug.Stack()`, a goroutine dump, a profiler sample — names who
caused the work by one of these two lines:

```text
github.com/stergiotis/boxer/public/keelson/runtime/entrypoint.Agentic(...)
github.com/stergiotis/boxer/public/keelson/runtime/entrypoint.Human(...)
```

Grep a trace for `entrypoint.Agentic`, or call `entrypoint.Current()` in
code and `entrypoint.OfTrace(b)` on a captured trace. The frame marks the
goroutine it is on: a goroutine your handler starts does not inherit it.

**How a job earns the agentic frame.** A handler running an agent's call is
handed `OperationCall.OnBehalfOf`; copying it onto the request (section 2)
puts the task, its epoch and the call on the wire. The worker confirms the
three with the host's dispatcher for the sending window before it writes the
row, records them as `Task`, `TaskEpoch` and `Call`, and refuses a request
it cannot confirm — including every delegated request reaching a headless
worker, which has no dispatcher. A row with a `Task` is `AgentCaused()`.
The rule, with its lane for running such jobs in a process of their own, is
[ADR-0344](../adr/0344-agent-caused-jobs-in-a-process-of-their-own-and-the-entry-frame-every-stack-carries.md).

A table created before the delegation columns needs
[the migration recipe](../migration/2026-10-watchbill-delegation.md) once,
or a drop where its rows are expendable.

## What is not here

Unique jobs, periodic jobs, snooze, per-queue concurrency, workflows. Each
is a River feature ADR-0234 §SD7 keeps out by name; a need for one is its
own ADR.
