---
type: adr
status: proposed
date: 2026-10-09
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0302: A built-in moderator — loop signals and an escalation ladder

## Context

[ADR-0300](./0300-metered-model-calls-and-rules-a-moderator-writes.md)
(proposed) gives a moderator the means to act: rules on the model service's
ledger, cancelling calls, stopping agent tasks and lowering their
ceilings, and asking the person. It leaves out the moderator itself
(§SD10). Nothing drives that surface yet.

Token totals tell a moderator that a window or a task spends a lot; they
do not tell it that the spending is a loop going nowhere. What would:

- the same tool called with the same arguments again and again;
- a turn's rounds piling up;
- the conversation growing round after round towards the model's context;
- refusals and failures in a row.

The data for these exists, but not live. `keelson('agent_actions')` holds
each dispatched call with its task, operation, argument digest and
outcome; the trail holds every model call with its conversation, turn and
round. The model service publishes one event per call (`llm.event.call`)
without the turn or round; the agent dispatcher publishes none per call.
The chat coordinator suppresses an identical call to one just refused, and
nothing notices a successful call repeated.

The repository has no way to run an app without a window:
`app.SurfaceHeadless` is declared and unused. Background bus clients are
host-side services that host boot starts, as watchbill and the metrics
scraper are.

## Decision

A host-side service, `runtime.moderator`, turns live events into signals
and signals into actions, following a fixed escalation ladder. The agent
dispatcher publishes one event per action for it, and the model service's
call event gains the turn and round.

### SD1 — A host-side service

The package `runtime/moderator` holds an engine — signals, the ladder, the
actions it chooses — that knows nothing of the bus, and a service that
feeds it events and carries out its actions through the ADR-0300 clients.

Host boot starts the service when `BOXER_MODERATOR` is `on`, as its own bus
client with the id `runtime.moderator`, and adds that id to the model
service's moderator list, which the agent dispatcher shares. One variable
turns it on; the deployment does not also have to list it.

It is one moderator, not the only one: another app listed in
`BOXER_LLM_MODERATORS` may moderate beside it. Its rules are named
`moderator/…`, so its own and others' do not collide. It makes no model
calls.

### SD2 — Live inputs

- **`runtime.agent.action.recorded`**: the agent dispatcher publishes each
  action record as it records it — the task, its coordinator and window,
  the conversation and turn, the model call and tool call that caused it,
  the window and app operated on, the operation, its effect, the argument
  digest, the decision, the phase, the reason and the budget left; never
  the model's own words (the call's title and reason, its arguments). Four
  tokens, so no `runtime.agent.*` grant reaches it; `agent.ModeratorCaps`
  grants the subscription. It is published from a queue of its own that
  drops the oldest, so a slow subscriber never holds up a dispatch.

  A call is recorded when it is dispatched and again when a reader of its
  status sees it final, as the chat coordinator does when it settles a
  call; a consumer counts a call once, by its key.
- **`llm.event.call`** gains the conversation, turn and round the request
  named.

### SD3 — Signals

A signal is about a *window*: the one whose model calls a loop spends —
for an agent's action, its coordinator's. A coordinator's own model calls
carry no task, so the task is evidence on the signal, not its subject, and
a rule on the task's account would not slow the loop. A call no window
made (instance 0) is not watched. The first set:

| Signal | Raised when | Default |
|---|---|---|
| `repeat` | one task completes the same operation on the same window with the same argument digest again | 3 times within 10 min |
| `rounds` | a turn's model calls reach a round | round 16 |
| `context` | a call's input tokens reach a share of the model's context size, and grew on each of the turn's last rounds | 80 % |
| `errors` | calls on one subject are refused or fail in a row, model calls and actions together | 5 |
| `queue` | a window's calls wait longer than a bound, several times in a row | 30 s, 3 times |

`queue` is recorded and never escalates: contention is not the waiting
window's fault. A refusal by one of the moderator's own rules is not
counted as an error, so slowing a window does not escalate it further.
`context` needs the model's context size, which the moderator asks the
model service for until it is known. The thresholds are constants of the
engine in this cut.

Each signal is an audit event (domain `moderator`) naming its subject and
the calls that raised it, is published on `moderator.event.signal` for a
console to follow, and shows in `keelson('moderator_signals')` with the
subject's level.

### SD4 — The ladder

Each subject has a level. A signal that escalates moves it one step:

1. **Note** — the signal is recorded.
2. **Slow** — a rate rule on the window's account of 6 model calls a
   minute, lapsing after 10 minutes.
3. **Stop** — every task the window drives is stopped
   (`runtime.agent.moderate.stop` naming the window), and its model calls
   in flight or waiting are cancelled. A window that drives no task loops
   on the model alone; its model calls are denied for 10 minutes instead.

The rules lapse on their own: an ADR-0300 rule gains `Until`, after which
the ledger ignores and drops it, so a moderator that stops leaves nothing
behind.

The moderator acts up to Stop on its own, by the owner's choice. The
person is told rather than asked: the task ends with the reason "stopped by
a moderator (runtime.moderator): <signal>", which the chat shows where a
task's end is shown, and the audit event carries the evidence. A subject
quiet for 10 minutes steps down one level, and its Slow rule is removed.

### SD5 — Its own bounds

The moderator only slows, denies, cancels and stops; it never raises a
limit or a ceiling. Every rule it sets expires. Every action is an audit
event with the signal that caused it.

### SD6 — Deferred, recorded

- **A model-backed policy**, reading the trail and deciding with judgement;
  its calls would be metered under its own account.
- **A console app** showing signals, levels and actions, over
  `moderator.event.signal` and `keelson('moderator_signals')`.
- **Thresholds per deployment**, through the environment or a policy file.
- **Asking the person before Stop**, as an option.
- **Signals from other capabilities**: HTTP fetches, captures.
- **Running as a headless app** once `SurfaceHeadless` has a host.

### Milestones

- **M1 — Inputs.** ✓ `runtime.agent.action.recorded`; conversation, turn
  and round on `llm.event.call`.
- **M2 — Engine.** ✓ Signals and the ladder, tested without a bus.
- **M3 — Service.** ✓ Wiring, actions, audit events, the table, host boot.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Capability subjects | added `runtime.agent.action.recorded`, `moderator.event.signal`; `agent.ModeratorCaps` gains the action subscription | the agent service's `ServiceCaps` |
| `llm.event.call` payload | added conversation, turn, round | `llm.DecodeCallEvent` |
| ADR-0300 rules | added `Until`: a rule lapses | `llm.ration.set` wire, `keelson('llm_rations')`, the rule's audit event |
| Environment variables | added `BOXER_MODERATOR` | [doc/env-vars.md](../env-vars.md) regenerated |
| Audit events on `boxer.facts` | new domain `moderator` | — |
| Introspection | added `keelson('moderator_signals')` | registered by the introspection host, empty without the moderator |

## Alternatives

- **A windowed app.** It moderates only while its window is open.
- **Building the headless app host first.** More infrastructure than the
  moderator needs; deferred (SD6).
- **Polling `keelson('agent_actions')`.** Avoids a new event, at the cost of
  seconds of delay and a query every few seconds; the owner chose the
  event.
- **Signals computed inside the model and agent services.** Spreads policy
  into the services that enforce it; the services publish facts, the
  moderator interprets them.
- **Asking the person before every Stop.** A loop left running while
  nobody answers keeps spending; the owner chose to act and tell.
- **A model-backed moderator first.** Slower, costlier and harder to test
  than a fixed ladder; deferred.

## Consequences

### Positive

- A task that loops is slowed and then stopped without anyone watching.
- Every signal and action is on the record with its evidence.
- The events and the table serve a later console or a smarter moderator.

### Negative

- A fixed threshold stops some legitimate work: a task that repeats a
  call on purpose, a long turn that is making progress.
- The agent dispatcher publishes one more event per action.
- The thresholds cannot be tuned without a rebuild in this cut.

### Neutral

- With `BOXER_MODERATOR` unset nothing changes but the two events.

## Migration — Tier 1

- **Breaks.** Nothing: an added subject, fields, a table and a variable.
- **Path.** None.
- **Regeneration.** `doc/env-vars.md`.
- **Old shape.** Kept.

## Verification plan — Tier 1

- **Lane.** Default `go test`: each signal from a scripted event sequence,
  the ladder up and down (package `moderator`, engine); the service against
  the in-process bus with the model service and the dispatcher — a window
  repeating a call is noted, slowed, then denied, and its next model call
  is refused (package `moderator`, service); the action event's payload
  (package `agent`); a rule that lapses (package `ration`).
- **What would fail.** A repeat that does not escalate; a quiet subject
  that does not step down; an action without its audit event.
- **Gap.** Whether the defaults suit real chats is not tested. Stopping a
  real task through the service is covered by the dispatcher's own tests
  of `runtime.agent.moderate.stop`, not end to end.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0300](./0300-metered-model-calls-and-rules-a-moderator-writes.md) (proposed) — the moderator's surface.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — task grants and action records.
- [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) — the trail.
- [ADR-0296](./0296-audit-events-as-a-trail-component.md) — audit events.
