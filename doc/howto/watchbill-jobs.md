---
type: how-to
audience: developer giving an app or a headless binary durable work to do
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-14
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
        Run: func(ctx context.Context, job watchbillstore.Job, h task.HandleI) error {
            // job.Subject is the id of the row in your own table that says
            // what to download. Report progress on h; check ctx.
            return download(ctx, job.Subject, h)
        },
    })
}
```

Registration is process-wide: every binary that links this package has the
handler, and every worker in such a binary drains the kind. A job of a kind
no running binary links sits `queued` — `keelson('watchbill_worker')` shows
what this process serves.

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

The other verbs are `Cancel(id, note)`, `Retry(id, note)`, `Get(id)` and
`List(states, kinds, limit)`. A running job is also a keelson task with the
job's id, so the task monitor's cancel button and the client's `Cancel` are
the same transition.

From a process that holds the store — a host service, a test over
`watchbill.MemStore` — `watchbill.Submit` is the same enqueue plus wake.

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

- **In flight, failures, one job's timeline, workers** — the `watchbill`
  applet book in sqlapplet, over `keelson('watchbill')`,
  `keelson('watchbill_event')` and `keelson('watchbill_worker')`. It needs
  no external ClickHouse.
- **Live progress and cancel** — the task monitor, since every run is a
  task.
- **From SQL** — the same three tables in play, on the introspection
  endpoint.

## What is not here

Unique jobs, periodic jobs, snooze, per-queue concurrency, workflows. Each
is a River feature ADR-0234 §SD7 keeps out by name; a need for one is its
own ADR.
