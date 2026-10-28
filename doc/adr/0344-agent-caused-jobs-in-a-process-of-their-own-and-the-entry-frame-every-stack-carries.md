---
type: adr
status: proposed
date: 2026-10-28
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0344: Agent-caused jobs in a process of their own, and the entry frame every stack carries

## Context

[ADR-0343](./0343-coverage-on-the-trail-what-an-agents-call-executed.md)
(proposed) attributes Go coverage to an agent's command by reading the
process-wide counters before and after the command's handler call. That
works because the call is short, runs alone on the render goroutine, and is
the only app code a command runs. It stated in its §SD5 what it does not
claim: queries, external reads, the next frame's drawing, and jobs.

Jobs are the gap that matters. A watchbill job
([ADR-0223](./0223-watchbill-durable-work-on-facts.md)) is the opposite of
a command on every axis the bracket relies on: the worker launches it on a
goroutine of its own, runs several per kind at once (`DefaultMaxWorkers`
is two), and the primitive is for work measured in seconds to hours. A
counter diff around `runHandler` would contain every frame the host drew
meanwhile, every other job, and the sampler itself — most of the run, not
the job. Go gives no way to isolate a goroutine's counters: the counters
are one shared table, a goroutine cannot be paused from outside, and the
instrumentation has no hook for a context. Time and process are the only
separations available. Commands are short enough to separate by time. Jobs
are not.

A second gap sits before the first. A job row carries its owner app and
the run that enqueued it (ADR-0223 §SD1), and nothing about *why*: a job an
agent's command handler enqueued and a job a person's click enqueued are
the same row once claimed. The handler does know — it is handed
`OperationCall.OnBehalfOf`
([ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
§SD6) — and the house pattern for onward work is to carry that context on
the wire and have the receiving service attest it with the dispatcher
before recording it, as the ad-hoc data, HTTP egress and LLM services do
([ADR-0288](./0288-adhoc-bundles-datasets-published-with-the-applet-that-reads-them.md) §SD5,
[ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md)
§SD1). The watchbill wire has no such fields.

A third observation turned into the first decision. Separating the *entry
points* of agent-caused and person-caused execution does not attribute
coverage — a stack is a snapshot of one instant, and coverage is a count
over an interval — but it answers the *who* exactly, for anything that
captures a stack: a panic, a goroutine dump, a profiler, a debugger. The
runtime today calls `ApplyCommand` from three places and `HandlerI.Run`
from one, with nothing on the stack that says which caller it serves. That
is cheap to fix and worth fixing on its own.

The tree already holds every piece the process lane needs. A headless
worker is a run like the host's
([ADR-0234](./0234-watchbill-client-protocol-and-worker-presence.md) §SD6):
it initialises `runinfo`, writes the runtime-start row and heartbeat, and
drains with the handlers its binary registered. The scene runner launches
the running executable as a child through an `extbin.Local` program,
forwards its environment, gives it its own process group and reads its
exit ([ADR-0248](./0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md)
§SD4). The ClickHouse-local pool supervises children behind a
`procpool.SpawnerI` with a release callback and a kill grace
([ADR-0285](./0285-procpool-pre-spawned-workers-behind-a-spawner.md)).
Watchbill's cross-process coordination never touches the bus: claims are
conditional updates on the job table and the lease is the runtime
heartbeat (ADR-0223 §SD2, §SD4); the bus carries the doorbell, the changed
announcements and task reporting, and a headless worker runs without any
of the three.

## Terms

- **Agent-caused** — work whose causal chain holds an agent's call: a
  command the dispatcher queued, or a job a handler enqueued while running
  one, with the dispatcher's attestation.
- **Entry frame** — one of two functions in `keelson/runtime/entrypoint`
  through which the runtime hands control to app code, kept on the stack
  for the whole call, so a captured stack names its cause.
- **The lane** — a reserved watchbill queue only a claim by id drains.
- **Job child** — a process the host starts from its own executable to run
  one job and exit.
- **Driver stamp** — attributes on a run row naming what drove the run.

## Design space (QOC)

**Question A.** How does a stack trace say whether the code it shows ran
because an agent asked?

- **A1 — Two named entry functions, out of line, one per cause** (chosen).
  Every runtime site that calls into a handler goes through one. The frame
  is on every stack, costs one call, and needs nothing from the Go
  toolchain.
- **A2 — Goroutine labels** (`pprof.Do`). Visible to the CPU profiler and
  the goroutine profile, inherited by child goroutines; invisible in a
  panic trace or a debugger, and a label is data the code reads, not a
  frame the reader sees. A later addition over A1, not instead of it.
- **A3 — A field in the trail only.** The action record already says who
  called; it says nothing about a stack captured outside the trail.

**Question B.** How does a job know an agent caused it?

- **B1 — Delegation fields on the enqueue wire and the job row, attested by
  the dispatcher** (chosen). The handler copies its call's on-behalf-of
  onto the request; the worker's enqueue service confirms the call with
  the dispatcher for the envelope's sender and records task, epoch and
  call on the row. Unattested requests are refused and write nothing. The
  shape ADR-0288 §SD5 built.
- **B2 — Infer from the writer of the window at enqueue time.** The worker
  cannot see the window's current writer, and a person keeps editing the
  window a task holds (ADR-0343's rejection of time windows).
- **B3 — Trust the request.** A delegation on a row is a claim about who
  caused the work; the trail records only what was attested (ADR-0277
  §SD1).

**Question C.** Where does an agent-caused job run so its coverage is its
own?

- **C1 — One child process per agent-caused job, claimed by id, exited when
  done** (chosen). The child is a run; the coverage ADR-0343 already writes
  under that run *is* the job's. No new coverage kind, no bracket, no
  concurrency caveat. The ADR-0343 "driver stamp" lands on the child's run
  row to join it to the job and the action record.
- **C2 — A long-lived isolated worker** (`watchbill run` under a unit,
  concurrency one). Pure only while it serves one kind; attribution per job
  needs a bracket row around `runHandler` with a mixture caveat. Kept as
  the lane for a cell that forbids the host from spawning.
- **C3 — Bracket in the host.** Rejected for the reasons in Context: the
  diff is the process's, not the job's.
- **C4 — Context-propagated coverage.** Rejected in ADR-0343; nothing here
  changes that.

**Question D.** Who launches the child?

- **D1 — The host's watchbill service, through `procpool` over an
  `extbin.Local` program naming the running executable** (chosen). The
  scene runner's launch, the ClickHouse-local pool's supervision; CS012
  holds, since extbin resolves and the caller owns the lifecycle, as eleven
  packages already do.
- **D2 — systemd.** The right home for a long-lived worker (C2) and the
  stated posture for the NATS server (ADR-0026 §SD4); not startable per job
  from an unprivileged host, and it gives up the per-job run identity that
  makes attribution free.
- **D3 — The enqueue service spawns at enqueue.** Misses a job a headless
  verb enqueued; the coordinator polls the lane instead, and an enqueue it
  sees is a wake.

**Question E.** What does the child need from the bus?

- **E1 — Nothing** (chosen). It was told its job id, so it needs no
  doorbell; it coordinates through the table and the heartbeat. On the
  in-process bus the host spawns the keelson task itself and folds the
  child's progress from the job events the child writes; it re-announces
  the changed subject on the terminal row or the child's exit.
- **E2 — Require NATS.** Apps are still on the in-process bus until
  ADR-0026 M4 lands; the lane would wait on it. When a NATS URL is
  configured, the child inherits it and reports its task natively — an
  optimisation the lane gains, not a dependency it has.

**Question F.** Where do the child's logs go?

- **F1 — Pipes to the parent, read line by line, passed through to the
  host's log** (chosen). The child's lines already carry its run id, so one
  journal holds both and a reader filters either way. The parent must never
  stop reading (a full pipe blocks the child), lines are the unit, and an
  over-long line is truncated with a marker.
- **F2 — A file per child.** The scene runner's choice, for a reason this
  lane does not share (two writers under gVisor sharing inherited
  descriptors). Kept as an opt-in for a child that dies before its logger
  is up.

## Decision

### SD1 — Two entry frames, and every handler call goes through one

Package `public/keelson/runtime/entrypoint` exports `Agentic(fn)` and
`Human(fn)`, both marked `//go:noinline`, both doing nothing but calling
`fn`. Their fully qualified names are the contract:

```text
github.com/stergiotis/boxer/public/keelson/runtime/entrypoint.Agentic
github.com/stergiotis/boxer/public/keelson/runtime/entrypoint.Human
```

Every place the runtime hands control to app code on a caller's behalf
calls through exactly one of them and never calls the handler directly:

| Site | Frame | Decided by |
| --- | --- | --- |
| `opengine.Engine.apply` — a queued command's `ApplyCommand` | `Agentic` when the call carries an on-behalf-of or its writer is a task; `Human` otherwise | `agentCaused(call)` |
| `opengine.Engine.Gesture` — the person's command through the app's own UI | `Human` | always |
| `watchbill.Worker.execute` — `HandlerI.Run` | `Agentic` when the job row carries an attested delegation; `Human` otherwise | `Job.AgentCaused()` |

The ADR-0343 bracket encloses the frame, not the reverse: `Begin` and
`End` run under no frame, the handler inside runs under one, so the two
counter reads see the same code the frame names.

The frame marks the goroutine it is on. A goroutine the handler starts does
not inherit it; a frame says nothing about what the code did — coverage and
the trail say that. When both frames are present (a handler that calls back
into the apply path), the innermost wins. `entrypoint.Current()` classifies
the calling goroutine, `Of(pcs)` a `runtime.Callers` capture, `OfTrace(b)`
a formatted trace, so a program reads a stack the way a person greps it.

A new site that runs app code for a caller adopts a frame in the same
change, and names which. This is the rule a reviewer checks.

### SD2 — The delegation rides the enqueue and is attested at the door

The watchbill request wire gains `OboTask`, `OboEpoch`, `OboCall`
(vocabulary `wbReqOboTask` 250, `wbReqOboEpoch` 251, `wbReqOboCall` 252 in
the keelson registry). `watchbill.Request` gains `OnBehalfOf
*app.OnBehalfOf`; a handler copies its `OperationCall.OnBehalfOf` there.
`Client.Enqueue` puts it on the wire.

The worker's enqueue service, before writing anything, asks the dispatcher
(`Worker.SetDelegation(app.CallContextI)`, wired in hostboot beside the
egress and LLM services) to confirm `(task, epoch, call)` for the
envelope's sender and instance. Confirmed, the job row records `Task`,
`TaskEpoch`, `Call` under the trail's own memberships (`trailTask`,
`trailTaskEpoch`, `trailCall`), so the join to the action record is on the
same names. Not confirmed, or no dispatcher installed, the request is
refused with `ErrUnattested` and no row is written. A request with no
delegation asks nothing and runs as before.

`Enqueue` on a store handle — the host's own services, tests — takes the
request's delegation as given: the caller is the host.

The three sections are declared last in the job schema, so a table created
before them is migrated by appending columns:
[the migration recipe](../migration/2026-10-watchbill-delegation.md).

### SD3 — The lane: a reserved queue only a claim by id drains

A job the enqueue service records with a delegation lands in the reserved
queue `watchbillstore.QueueLane` (`agent-lane`) when the lane is on, and
the worker's poll never selects that queue in any process — a host with
the feature off included, a worker that names the lane among its queues
included — so no worker runs an agent-caused job in-process by accident.
The exclusion sits in the two queue reads themselves (the SQL and the
memory store), not in a worker's configuration. The reserved queue is
claimable only through `Claim(id)`: `Worker.RunJob(ctx, id)` is that
claim, followed by the run on the calling process, and `watchbill run
--job <id>` is its verb. A request may not name the lane as its queue; a
job parked where no poll looks is refused at the door. A person-caused job
keeps its requested queue and runs in the host as today.

The lane is on when `KEELSON_WATCHBILL_ISOLATE_AGENT_JOBS` is true on the
host (`Config.IsolateAgentJobs`); off, the enqueue service records the
delegation and routes nothing, and the job runs in the host under the
`Agentic` frame. The frame does not depend on the lane. Until the
coordinator of §SD4 lands, a worker with the lane on says so at start, and
a lane row runs when an operator starts `watchbill run --job <id>` for it.

### SD4 — One child per job, from the running executable

The host runs a lane coordinator (`keelson/runtime/watchbill/lane`) when
the lane is on: it reads the reserved queue at the worker's poll interval
and on every changed announcement (an enqueue the service served is one),
and for each queued, due row with no child yet it takes a slot from a
`procpool.Pool` and starts the running executable — `extbin.Local` program
"watchbill job child", path `os.Executable()` — with the host's environment
(the coverage opt-ins included), its own process group, and `watchbill run
--job <id>`. The slot is what the pool counts, ready the moment it is made;
the coordinator starts the process once it holds the slot, so
`KEELSON_WATCHBILL_LANE_CHILDREN` bounds live children through their whole
teardown. A read that finds the pool full stops and tries at the next
read; it never blocks. The host binary must carry the watchbill CLI
command for its children to run; the imzero2 host does from this ADR on.
Close is SIGTERM to the group, then SIGKILL after
`KEELSON_WATCHBILL_LANE_KILL_GRACE`.

`watchbill run --job <id>` initialises `runinfo`, writes the runtime-start
row and heartbeat, claims that one row by id, runs it with the binary's
registered handler under the `Agentic` frame, settles it, and exits. A lost
claim exits at once. A child that dies leaves a running row whose heartbeat
goes stale; any worker's sweep abandons or requeues it (ADR-0223 §SD4).

The child's stdout and stderr are one pipe the coordinator reads line by
line into the host's log (F1): a zerolog JSON line is re-emitted at its
own level with its fields, so the child's run id travels through; any
other line is text; a line past 64 KiB is cut and marked. The reader runs
from before `Start` to the pipe's end of file. The orphan policy is: a
child whose parent is gone dies at its next write to the broken pipe (Go's
default for descriptors 1 and 2), its lease goes stale, and at-least-once
requeues the job. A cell that prefers orphans to finish sets the child to
ignore SIGPIPE; the ADR records the default so the difference is known
before a host crashes.

The host's side of a child (E1): the coordinator spawns the keelson task
under the job's id on the host's bus — the child, with no bus, reports to
no one — notes which child runs it, and ends it from the row the child
left: `Done` on succeeded, `Error` with the row's state otherwise. A
cancel of that task writes a cancel request on the row, which the child
honours at its poll; the coordinator's own stop writes none, since its
children are signalled and their rows are left running for the sweep. A
failed or discarded row tells the attention notices, as the in-process
worker does, and the changed subject is announced. Folding the child's
own progress notes is not done: they go to a handle that reports to no
one, and arrive when E2 does.

A child that exits with its row still running died with the claim; the
task ends in error, and the sweep requeues or abandons the row once the
heartbeat is stale, after which the coordinator sees it queued again. A
child that exits with the row still queued lost its claim or never made
one; the next read starts another.

Cancel: the host's cancel verb writes the row; the child honours it within
its poll as any worker does; the pool's close is the backstop.

### SD5 — The driver stamp, and what the marts join

The run row gains a driver stamp: driver kind (`watchbill-job` here;
`scene`, `headless`, `person` are the same field's other values and land
with their own ADRs), job id, attempt, job kind, and the delegation. The
child writes it with its runtime-start row.

The coverage samples ADR-0343 writes under the child's run are the job's
attribution; no new coverage kind. A mart joins a run's samples through
the stamp to the job and, through the job's delegation, to the action
record. A child whose claim lost ran boot and shutdown only: its set is the
baseline to subtract.

### Deferred

- **Goroutine labels** (A2) under each frame, so profiler samples carry the
  cause too. Small; after the frames prove out.
- **A second lane for person-caused jobs**, for a symmetric comparison; one
  more routing rule.
- **Task progress through a shared bus** once ADR-0026 M4 lands (E2).
- **The watchbill window** showing a job's delegation; the column exists,
  the view does not.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API under `public/` | added: package `keelson/runtime/entrypoint` (`Agentic`, `Human`, `For`, `Current`, `Of`, `OfTrace`, `KindE`, `FuncAgentic`, `FuncHuman`); `watchbill.Request.OnBehalfOf`; `watchbill.Worker.SetDelegation`; `watchbill.ErrUnattested`; `watchbillstore.Job.{Task,TaskEpoch,Call}`, `Job.AgentCaused`. All additive | `opengine.apply`, `opengine.Gesture`, `watchbill.Worker.execute` call through a frame |
| Keelson vocabulary (`keelson/vdd`) | added: `wbReqOboTask` 250, `wbReqOboEpoch` 251, `wbReqOboCall` 252 | the assignments golden; `watchbillrequest.out.go` regenerated |
| `watchbillRequest` wire (ADR-0234 §SD1) | added three optional fields | `Client.Enqueue`, the worker's `opEnqueue` |
| `boxer.watchbill` job table | added three sections, last | DDL golden, job store regenerated; the migration recipe for an existing table |
| hostboot | the worker gets the dispatcher as attester | none |
| CLI `watchbill run` | gains `--job <id>` (SD4, M1) | the how-to |
| Env-var registry | added: `KEELSON_WATCHBILL_ISOLATE_AGENT_JOBS` (SD3, M1); `KEELSON_WATCHBILL_LANE_CHILDREN`, `KEELSON_WATCHBILL_LANE_KILL_GRACE` (SD4, M2) | `doc/env-vars.md` |
| Exported Go API under `public/` (M2) | added: package `keelson/runtime/watchbill/lane` (`Coordinator`, `Config`, `LauncherI`, `ChildI`, `ExecLauncher`, `DefaultChildArgs`, `Caps`, `AppId`); `hostboot.Runtime.WatchbillLane` | hostboot starts it under the lane switch |
| extbin registry (ADR-0118) | added: Local program "watchbill job child" | `keelson.extbin` |
| `imzero2` binary (`public/thestack/cmd/imzero2`) | gains the `watchbill` CLI command | its children run `watchbill run --job` |
| Exported Go API under `public/` (M1) | added: `watchbillstore.QueueLane`, `watchbill.Worker.RunJob`, `watchbill.Config.IsolateAgentJobs`, `watchbill.IsolateAgentJobs` | `QueueSQL` and `MemStore.Queue` exclude the lane |

## Alternatives

- **Bracket jobs in the host** (C3), **a long-lived isolated worker as the
  only lane** (C2), **systemd as the launcher** (D2), **NATS as a
  requirement** (E2), **a log file per child** (F2): each above, with why.
- **Infer the cause at enqueue from the window's writer** (B2) and **trust
  the request's delegation** (B3): above.
- **One entry function with a parameter.** A parameter is not on the stack
  a trace prints; two names are.

## Consequences

### Positive

- Any stack captured under app code — panic, dump, profile, debugger —
  says whether an agent caused it, by one frame's name, in every process
  and on every bus.
- A job's row says which agent call enqueued it, attested, on the names
  the action record uses.
- An agent-caused job's coverage is exact and exhaustive, with no mixture,
  for the cost of a process start per job against work measured in
  seconds to hours.
- A stuck or runaway agent job is a process: visible, killable, its
  external programs dying with it.

### Negative

- One more call on every handler path (two frames deep, not inlined). Not
  measurable against a handler that does anything.
- Three more columns on the job table, and a recipe for tables that
  predate them.
- An agent-caused enqueue through the bus now needs the dispatcher
  installed; a headless worker's service, which has none, refuses one. It
  could not have attested it before either; now it says so.

### Neutral

- Jobs no agent caused run exactly as before, under the `Human` frame.
- The lane off is the enqueue path plus three recorded attributes.

## Migration — Tier 1

- A job table created before SD2: run
  [the recipe](../migration/2026-10-watchbill-delegation.md), or drop the
  table where its rows are expendable (they expire after
  `KEELSON_WATCHBILL_KEEP`, default a week).
- Apps that enqueue from a command handler and want the job attributed:
  copy `call.OnBehalfOf` onto the request. Apps that do not change nothing.

## Verification plan — Tier 1

- `entrypoint`: the frame is on the stack for the whole call, deeper
  callers see it, a child goroutine does not, the innermost wins, a
  formatted trace classifies by the printed name.
- `opengine`: a task's queued command runs under `Agentic` with and without
  an on-behalf-of; a person's queued command and a gesture run under
  `Human`; the bracket's `Begin`/`End` run under no frame.
- `watchbill`: a job with a delegation runs under `Agentic`, one without
  under `Human`, and the row carries the delegation; an enqueue with a
  delegation is refused without a dispatcher and with an unknown call, and
  written with a confirmed one; a plain enqueue asks the dispatcher
  nothing.
- Generators: the vdd assignments golden, `watchbillrequest.out.go`, the
  job store and its DDL golden regenerate to the committed bytes.
- M1–M3 add: a child claims by id and exits; a lost claim exits at once; a
  killed child's row is swept; the lane queue is never polled; the
  coordinator's pipe reader survives a child that writes faster than the
  log drains.

## Milestones

- **M0 — Frames and delegation** (built with this proposal): the
  `entrypoint` package; the three sites call through it; the wire, the
  request, the row, the attestation, the hostboot wiring; tests; the
  migration recipe.
- **M1 — Claim by id** (built): `Worker.RunJob` and `watchbill run --job
  <id>`; the reserved queue the two queue reads never select; the enqueue
  service's routing under `KEELSON_WATCHBILL_ISOLATE_AGENT_JOBS`, and its
  refusal of a request that names the lane.
- **M2 — The coordinator** (built): package `watchbill/lane` — the slot
  pool over `procpool`, the `extbin.Local` program, environment, process
  group, the pipe relay into the host's log; the host-side task, the
  notices and the changed announcement; task cancel to the row; stop with
  the kill grace; the imzero2 binary carries the watchbill verb.
- **M3 — The stamp and the marts.** Driver stamp on the run row; the
  attribution mart over ADR-0343's samples; the baseline from a lost
  claim.

## Status

Proposed — awaiting review by the code owner. M0, M1 and M2 built alongside the proposal.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0343](./0343-coverage-on-the-trail-what-an-agents-call-executed.md) — coverage on the trail; §SD5 the bracket this encloses, its Deferred list this takes the driver stamp from.
- [ADR-0223](./0223-watchbill-durable-work-on-facts.md) — the job table, the claim, the lease.
- [ADR-0234](./0234-watchbill-client-protocol-and-worker-presence.md) — the client wire this extends; §SD6 the headless worker the child is.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — operations; §SD6 the on-behalf-of a handler is handed.
- [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) — the trail; §SD1 "record only what was attested".
- [ADR-0288](./0288-adhoc-bundles-datasets-published-with-the-applet-that-reads-them.md) — §SD5 the attestation shape this copies.
- [ADR-0248](./0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md) — the scene runner's launch of this executable.
- [ADR-0285](./0285-procpool-pre-spawned-workers-behind-a-spawner.md) — the spawner seam the coordinator uses.
- [ADR-0118](./0118-extbin-external-process-chokepoint.md) — resolution through extbin, lifecycle with the caller.
- [ADR-0191](./0191-runtime-instance-attribution.md) — the precedent for appending sections and the ADD COLUMN recipe.
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — §SD4 the bus transports.
- [How to run a job on watchbill](../howto/watchbill-jobs.md) — the delegation and the frames, for a handler author.
