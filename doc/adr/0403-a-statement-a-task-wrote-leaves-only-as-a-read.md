---
type: adr
status: proposed
date: 2026-11-22
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration. SD1–SD6 are built (2026-11-22) as the sections below describe; what is left out is under *Deferred*. Nothing here has run against a ClickHouse server: the server exit is tested against a stub, the local exit against clickhouse-local.

# ADR-0403: A statement a task wrote leaves only as a read — the task rides to the fairlead

## Context

An agent's work reaches SQL through apps. Play classifies a statement a
task drove before it sends it and sends it with `readonly=2`
([ADR-0270](./0270-play-operations-catalog-and-agent-limits.md) §SD2); tally
and the keelson query gate refuse a statement the statement-kind classifier
does not prove a read. Each of these is a check in the app that sends.

[ADR-0367](./0367-sql-statements-leave-through-one-fairlead-and-each-is-placed-in-a-stratum.md)
gave every statement one opening on its way out of the process, where a
host-installed pass sees it. Its update of 2026-11-20 deferred a pass that
refuses an agent's non-read there: a statement arrived without the task that
caused it. The fairlead's statement had no identity field, the engine request
carried none, and a local run crosses the bus to a broker whose worker leads
its statement under the background context.

Two observations shape the key.

- **"Caused by an agent" is the wrong key.** Agent-caused work makes
  legitimate writes whose text the app composed itself: play's app writes,
  bounded by the grant's destinations, and play's own lookups run on a
  task's behalf with the value bound as a parameter (ADR-0270, update of
  2026-10-04). A pass refusing every non-read under an agent's call would
  refuse those.
- **"Written by the task" is the right one.** The barrier is for text a
  task supplied. Who supplied a statement's text is known to the sender
  and to nobody after it, in the same way that what a run touches is known
  to whoever bound it ([ADR-0145](./0145-sealed-app-data.md) §SD4, the
  `Sensitivity` label on the engine request).

## Decision

We will carry, from the sender to the fairlead, the task whose call
supplied a statement's text, and a host will install a pass that lets such a
statement leave only as a read.

### Subsidiary design decisions

- **SD1 — The fairlead carries the task.** A context mark,
  `fairlead.WithTask` / `fairlead.TaskOf`, and `fairlead.Statement.Task`,
  which `Lead` fills from the context. It is empty for the person's
  statement and for one the process composed itself. An empty task clears a
  mark the context carries, for an exit that sends its own text under a
  caller's context. The fairlead still parses nothing.
- **SD2 — The engine request carries it.** `queryengine.Request.Task`,
  documented as `Sensitivity` is: declared by whoever knows where the text
  came from, riding to the exit so the engine's exit point can hand it to
  the fairlead independently of the checks the caller made. Empty leaves
  whatever mark the context carries. The server engine marks the context it
  hands to `Lead`; its `KILL QUERY` is the adapter's own text and is led
  with the mark cleared.
- **SD3 — A local run carries it across the bus.** The bus hands the
  broker no context, so the task crosses on the request
  (`chlocalbroker.ExecRequest.Task`, an optional field of the wire request
  under the same wire version), taken from the request or else from the
  mark on the caller's context. The broker hands the worker its prelude —
  the `SET param_*` and temporary-table statements it composes — apart from
  the request's statement, and `chlocalpool.Worker.WriteStatement` leads
  the two separately under the broker's request context: the prelude with
  the mark cleared, the statement with the request's task. The worker
  receives the same text as before. Without the split a pass would see a
  script, which the classifier reads closed, and every marked local run with
  an input table or a bound parameter would be refused.
- **SD4 — The pass.** `fairlead/taskread.Pass` refuses a statement with a
  task unless `analysis.ClassifyStatementKind` proves it a read; unparsed,
  unknown and a script are refused with it, and an unmarked statement passes
  untouched. That classifier is the weakest of the per-app checks — tally
  and the keelson query gate apply it, play and the SQL applets require the
  stricter security class — so a statement an app's check let through is
  not expected to be refused here. Play checks the statement before its
  wrapper and format clause are added and the pass judges the text that is
  sent; the wrappers play adds are reads or `EXPLAIN`. The refusal names what was refused and why, and the
  task and the kind travel as fields. It never rewrites and holds no state.
  The pass lives in its own package so that the fairlead, which low-level
  clients import, keeps not linking the parser (ADR-0367 §SD3).
- **SD5 — The host installs it.** `hostboot.Boot` installs the pass once
  per process, before the facts store sends its first statement, and does
  not remove it when a Runtime closes. There is no switch: the pass judges
  only statements a sender marked, and a sender that must send a task's
  non-read has the person adopt the text first (a consequential operation,
  or the person's own Run) rather than mark it.
- **SD6 — Senders declare; the per-app checks stay.** An app marks a
  statement when its text came from a task: play's run path whenever the
  agent limits apply to it — the window's mark, a lane's explicit agent —
  the diagnostics probe of such a run, and `check`; tally's Results query
  set by an agent's `set_query`. Play's own statements under a mark, its
  app writes and its catalog reads stay unmarked. Each app's classification,
  `readonly=2` and the grant checks remain where they are: this is a second
  barrier, not a replacement.

### What the barrier does not cover

- **The mark is the sender's declaration.** An app that runs a task's SQL
  and neither checks nor declares it is not stopped by the pass. The bound
  that does not depend on the client is the server-side user the
  [agentic layer](../explanation/agentic-layer.md) describes.
- **Read versus not-read only.** A read that reaches past the server — the
  table functions `url()`, `s3()`, `remote()`, `file()` — is the security
  class's question, which play and the applets ask before they send, and
  which the server user's missing source privileges bound.
- **Hops that leave the process.** A run play sends to the host's
  introspection endpoint is judged at play's exit; the endpoint receives it
  over HTTP without the task and runs it as its own. A header naming the
  task would be the client's word and is not sent.
- **A broker cache hit sends nothing**, so nothing is led. Only a statement
  that ran before and passed the broker's cacheability gate can hit.
- **A process that does not boot through `hostboot`** installs no pass and
  may install it itself (`taskread.Install`).

### Deferred

- **The journal records no task.** A journal line is one per distinct text
  per exit point (ADR-0367 §SD4) and the report folds on the line; adding
  the task to the line or to its key is a change to that promise.
- **Stored text an agent authored.** An applet buffer an agent wrote
  through the applet creator, and a notebook's record cells set by
  `set_records`, are saved and run later; the store does not record which
  task wrote them, so their runs are not marked. A notebook runs live only
  what classifies a read, and its collection is a consequential operation
  the person confirms.
- **The keelson query service's bus request carries no task.** No app
  forwards a task's text to it; its gate applies the same classifier.
- **`Worker.WriteSQL`** still leads under the background context; only the
  broker's path takes a context, which narrows ADR-0367's deferral of the
  worker's context without closing it.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API: `public/db/fairlead` | `WithTask`, `TaskOf`, `Statement.Task` added | a pass that builds a `Statement` literal without field names |
| Exported Go API: `public/db/fairlead/taskread` | added: `Pass`, `Install` | the host's boot |
| Exported Go API: `queryengine.Request` | `Task` added | the server and local engines |
| Exported Go API: `chlocalbroker.ExecRequest`, `chlocalpool.Worker` | `ExecRequest.Task`, `Worker.WriteStatement` added | the broker's handler |
| Bus wire: the local-exec request | optional `task` field under the same wire version | the broker; broker and clients ship in one binary |
| Host boot | installs the pass | — |
| Fairlead exit sites | `chlocalpool.prelude` added beside `chlocalpool.worker` | journal readers: a local run with a prelude is two lines |

## Alternatives

- **Every statement under an agent call's context is the task's unless
  the app declares it its own.** Fails closed on a forgotten declaration,
  which the chosen direction does not. Not taken: the call's context would
  have to reach every exit, which it does not — lanes run under the
  window's lifetime, a local run crosses the bus, results run off the
  frame — and every legitimate write an app makes on a task's behalf would
  need an inventory and a declaration before the pass could be installed
  without breaking them. Reconsider once operations hand their handlers a
  context that every app's I/O runs under.
- **Key on "caused by an agent".** Refuses the app writes and own
  statements a grant allows (see Context).
- **Classify inside the fairlead.** Every client would link the parser;
  ADR-0367 rejected it for the same reason.
- **Carry the task in the context only, without a request field.** The
  bus to the broker carries no context, so the local engine would need a
  field anyway, and the server engine's caller would have to remember to
  mark a context instead of filling the request it already builds.
- **Judge the script at the local worker by its last statement**, as the
  journal report does. A task's own script ending in a read would pass with
  a write ahead of it, and clickhouse-local runs scripts.

## Consequences

### Positive

- A task's statement that is not a read is refused at the exit whatever
  app check was missed on the way, for every sender that declares.
- The task is visible to any pass, not only this one.
- A local run's statement is led apart from the broker's prelude, so a
  journal line for it no longer carries a temporary directory's path.

### Negative

- A sender that does not declare is not covered, and nothing finds one
  that should: the conformance check of ADR-0367 is about exits, not about
  where text came from.
- A host-installed pass is process-wide state; a test that marks a
  statement in a process where a host booted sees it refused.
- Each marked statement costs one more parse at the exit.

### Neutral

- With no marked statement, every exit behaves as before.
- Play's agent limits refuse first, the pass after them, and the server's
  `readonly=2` last; a statement reaches the pass only when the app's own
  check let it through.

## Migration — Tier 1

Additive: nothing to migrate. A pass that matches statements by site sees
`chlocalpool.prelude` as a new site.

## Verification plan — Tier 1

- **Lane.** Default `go test`: the mark and `Statement.Task` in the
  fairlead; a table of statements through the pass; the server engine
  against an HTTP stub, where a marked write is refused before a request is
  made and an unmarked one is sent; the wire field's round trip and, where
  clickhouse-local is installed, a marked read with a bound parameter and an
  input table running and a marked write refused at the worker; a booted
  host refusing a marked write; play's run path naming the window's task,
  a lane's explicit agent and none for its Docs lookup; tally's `set_query`
  remembering the task.
- **What would fail.** A marked non-read that reaches a server or a
  worker; a prelude judged as the task's; play's own statement marked.
- **Gap.** No ClickHouse server; the local broker tests skip where
  clickhouse-local is absent. The senders' declarations are covered for
  play's run path and tally only.

## Choices for review

- The key is the task that supplied the text, declared by the sender; the
  inversion is recorded and not taken.
- The pass uses the statement-kind classifier, so `SHOW`, `DESCRIBE`,
  `EXISTS` and `EXPLAIN` pass by their leading keyword, and a `SET` prelude
  ahead of a read the grammar parses counts as a read.
- `WithTask(ctx, "")` clears a mark rather than being a no-op.
- `queryengine.Request.Task` wins over the context's mark; an empty one
  leaves the context's mark in place.
- `KILL QUERY` is led with the mark cleared.
- The broker's prelude is led separately, under its own site name, and the
  broker path now leads under the request's context rather than the
  background one.
- The host installs the pass with no switch to turn it off, once per
  process, and never removes it.
- The journal does not record the task.
- Play marks the diagnostics probe of a run; its write path, which the
  renderer refuses for an agent's run, stays unmarked.
- Tally clears the mark when a launch config sets the query, whoever opened
  the window.
- The task does not cross the HTTP hop to the introspection endpoint.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0367](./0367-sql-statements-leave-through-one-fairlead-and-each-is-placed-in-a-stratum.md) — the fairlead, and the deferral this closes.
- [ADR-0145](./0145-sealed-app-data.md) §SD4 — a label derived by the party that knows, riding on the engine request.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) §SD6 — onward requests that carry the task.
- [ADR-0270](./0270-play-operations-catalog-and-agent-limits.md) §SD2 — play's agent limits and the window's mark.
- [ADR-0028](./0028-chlocal-low-latency-sql-cap.md) — the local broker and its wire.
- [ADR-0144](./0144-query-engine-adapters.md) — the engine seam the request belongs to.
- [Agentic layer](../explanation/agentic-layer.md) — what bounds a model's SQL at the server.
