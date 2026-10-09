---
type: adr
status: proposed
date: 2026-10-09
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0300: Metered model calls, and rules a moderator writes

## Context

Every model call an app makes goes through one host service, `runtime.llm`
([ADR-0254](./0254-model-inference-as-a-keelson-capability.md)). The service
writes one `trail.LlmCall` row per call
([ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md)). The
row carries the caller's app and instance, stamped by the bus; the agent task
the call was made for; the conversation and turn; and the input and output
tokens. Who spent what can therefore be answered after the fact, in SQL.

Four things are missing:

- **Live totals.** Nothing adds up the trail while calls are running.
- **Limits.** Nothing limits use per caller. The only bounds are per call
  (`BOXER_LLM_MAXTOKENS`, `BOXER_LLM_TIMEOUT`) and per agent task (the call
  budget and pace, [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md),
  [ADR-0280](./0280-a-ceiling-on-what-a-chats-model-may-do-scored-on-a-ladder.md)).
- **A way to change a decision from outside.** Nothing outside the service
  can influence it, so an agent that watches use and throttles or denies it
  (a *moderator*) has nothing to act on.
- **Token detail.** Cached and reasoning tokens are not read from the reply.

The model is usually self-hosted. There the scarce resource is the server's
capacity more than money: a long agent round from one window delays the
prompt the person is typing in another.

LLM gateways (LiteLLM, Bifrost, Envoy AI Gateway, Portkey, Cloudflare and
others) solve the same problem for HTTP traffic. Their common features, and
what fits here, are surveyed in
[llm-gateway-metering-survey](../adr-background-work/llm-gateway-metering-survey.md)
(2026-10-09).

A gateway sees a request and the key that came with it. The host service can
also see:

- the window the call came from, and whether the person is looking at it;
- the agent task that caused the call;
- the purpose the app states;
- the person, through the host's dialogs.

## Design space (QOC)

**Question.** Where is the decision to admit, limit or refuse a call taken?

**Options.**

- **O1** — *A judge on every call.* The service asks the moderator about each
  call and waits for the answer.
- **O2** — *Rules the moderator writes, checked by the host.* The moderator
  writes and changes rules in the background; the service checks each call
  against them synchronously.
- **O3** — *A gateway in front of the endpoint.* Run LiteLLM or Bifrost
  between `runtime.llm` and the model server and use its budgets and keys.
- **O4** — *Apps limit themselves.* Publish usage; each app and coordinator
  slows down on its own.

**Criteria.**

- **C1** — Added latency on the call path.
- **C2** — What happens when the moderator is slow, wrong or absent.
- **C3** — Attribution: window, instance, task, purpose.
- **C4** — What the moderator can express and change.
- **C5** — New components and dependencies.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | ++ | −  | ++ |
| C2 | −− | +  | +  | −− |
| C3 | ++ | ++ | −− | +  |
| C4 | ++ | +  | −  | −− |
| C5 | +  | +  | −− | ++ |

O1 makes every call wait on a model-backed agent and fails with it; a
gateway that offers this (Portkey's guardrail webhooks) defaults to a 3 s
timeout and lets the call through when the judge fails. O3 sees only the
service's single key, so all attribution is lost, and it adds a deployable.
O4 cannot stop an app that ignores the signal. **O2 is chosen.**

## Decision

The `runtime.llm` service keeps a ledger of use per account. It checks each
call against stored rules before calling the provider. It publishes usage
events. A moderator, an ordinary app on the bus with a privileged
capability, reads the events and writes the rules.

### SD1 — Accounts: who a call is charged to

A call is charged to every account in its chain, and must fit within the
rules on each of them:

| Account | Key | Source |
|---|---|---|
| run | the process run | `trail.Origin.Run` |
| app | `AppIdT` | the bus envelope |
| instance | the window key | the bus envelope (`SenderInstance`); 0 is the service account |
| task | the agent task | the delegation context (`OnBehalfTask`) |
| purpose | the stated purpose | `Request.Purpose`, scoped to the app |

A call an agent causes is charged to its task *and* to the coordinator's
instance that sent it. A task's rules therefore cover every window the task
drives. The causal chain (`ParentCallId`, tool calls) stays on the trail
row; the ledger does not walk it.

### SD2 — Usage is a set of named quantities

Usage is an open set of named numeric quantities, each with a unit and how
it was obtained (*measured*, *estimated*, *apportioned*). It is not a fixed
struct. The first set:

- calls;
- input, output, cached input and reasoning tokens, named after the OTel
  GenAI usage attributes;
- wall time;
- calls in flight.

The provider adapter reads cached and reasoning tokens from the reply's
usage details when the server reports them, and leaves them absent
otherwise. A quantity the server does not report is never filled with 0.

### SD3 — The ledger

- **Counters.** The ledger keeps counters per account and quantity over the
  windows the rules name: rolling windows, or windows aligned to the
  calendar.
- **Reserve, then settle.** Before the provider call it reserves an
  estimate: input tokens from the prompt size, output tokens from
  `max_tokens`. After the call it replaces the estimate with the reported
  figures. Counters live in one process, so the reservation is exact under
  concurrency. A quantity the ledger cannot estimate before the call —
  wall time, the cached and reasoning parts of the tokens — has nothing to
  reserve: a rule on it refuses once its window is spent, and the call
  that spends it overshoots by its own use.
- **Rebuilt on start.** On start, the ledger rebuilds from the trail rows
  inside the longest active window. Facts are the record; the ledger is a
  cache of them. If facts cannot be read, the ledger starts empty, says so
  in the log, and SD5's setting for calls no rule covers applies. The
  rebuild runs before the service subscribes, so no call is admitted
  against counts that are still loading; a slow store holds host start
  for up to its bound (15 s). Moving it off the start path would need
  calls held until it finishes; not done.
- **Bounded.** An account idle past the longest window, with nothing
  reserved, in flight or waiting, is dropped.
- **Exposed** as `keelson('llm_usage')` and as events on `llm.event.*`:
  usage per call, a soft threshold crossed, a refusal, queueing.

### SD4 — Rules

A rule has these parts:

- an account selector (one of SD1's kinds, a key or every key of the kind);
- a quantity;
- a kind: **budget** (a total over a window), **rate** (per minute or per
  hour), or **concurrency** (calls in flight);
- a limit;
- optional soft thresholds that raise an event;
- an optional temporary raise with an expiry;
- the author, and a reason.

Every change to a rule is an audit event on `boxer.facts` (domain
`llm-ration`, [ADR-0296](./0296-audit-events-as-a-trail-component.md)):
who set or removed which rule, with what limit and reason, and whether it
was refused. Who limited whom, when and why is therefore on the record and
in SQL. Rules are written through `llm.ration.set` and listed through
`llm.ration.list` and `keelson('llm_rations')`. A temporary raise never
changes the base limit; it adds to it until it expires, as Bifrost's
overrides and LiteLLM's temporary budget increases do.

The rules themselves live in the process: after a restart the ledger's
counts are rebuilt (SD3) but the rules are not, and the moderator sets them
again. Restoring them from their audit events is deferred (SD10).

No rules ship by default.

### SD5 — Admission

The service checks a call after the delegation check and the sensitivity
wall, and before the write-ahead. The outcomes:

- **admit**;
- **clamp** — `max_tokens` lowered to what the tightest budget leaves;
- **queue** — SD6;
- **refuse** — typed. `RefusedError` gains a kind: *wait* (a rate, or a
  call that reached its deadline in the queue; carries when to retry when
  it can) or *stop* (a budget, a refusal rule, or a call cancelled while it
  waited — a moderator's cancel carries its reason). It also names the
  rule that refused.

The outcome, the rule that decided it and any time spent queued go on the
call's trail row. `Response` carries what remains of the tightest rule on
each account, so a chat's loop can slow down before it is refused.

`BOXER_LLM_RATION_UNRULED` (`allow`, the default, or `refuse`) decides a
call when the ledger could not be rebuilt.

### SD6 — A queue ordered by window state

A call that a concurrency rule would refuse waits in a queue instead,
bounded by the call's own deadline. Its class comes from its instance's
window:

1. the focused window;
2. a shown window;
3. a collapsed window, or one not yet shown;
4. no window (instance 0: services, the CLI).

An agent's call takes the class of its coordinator's window. A singleton
shown in several windows takes the class of its best one. The class is
read once, when the call arrives: focusing a window does not move the
calls it already has waiting. Calls are served
first in, first out within a class. A waiting call moves up one class after
a fixed age, so a background loop is delayed, never starved.

The service reads window state through a reader wired after the window
host starts, in the same way as `SetDelegation`.

### SD7 — The moderator

- **Who.** A moderator is an app listed in `BOXER_LLM_MODERATORS`. Only a
  listed app may hold the `llm.ration.*` capabilities. These subjects have
  three tokens, so the client capability `llm.*` does not reach them.
- **What it does.** It subscribes to `llm.event.*`, reads
  `keelson('llm_usage')` and the trail, and writes rules.
- **Its own calls** are metered and ruled like any other app's.
- **Its own account.** A moderator may tighten a rule that could hold its
  own calls — on the run, its app, its window, its purposes, or a task its
  own calls were charged to — but not loosen or remove one: a higher
  limit, a larger or longer raise, or any other change of shape goes to
  the person (SD9).

### SD8 — Levers on the loop and on calls in flight

A runaway agent loop is better handled at the loop than one call at a time.

- **Cancel.** `llm.ration.cancel` cancels the calls in flight of a call id
  or an account, through the existing cancel path.
- **Stop or lower a task.** Moderators may stop an agent task and lower its
  ceiling through two new dispatcher verbs, `runtime.agent.moderate.stop`
  and `runtime.agent.moderate.authority`. These are admitted against
  `BOXER_LLM_MODERATORS`, not against the task's owner. A verb names a task
  by its id, or names a coordinator's window and reaches every live task
  that window drives: a coordinator's own model calls carry no task, so the
  window is the account a moderator sees a loop charged to. A lowering
  takes the lesser of the ceiling in force and the one named in each part,
  so it can never raise one. The grant record names the moderator as the
  one who decided.
- **Slow a task.** A rate rule on the task's account (SD4) slows it; there
  is no separate pace setting.
- **What stays the person's.** Raising a ceiling or adding calls to a task,
  as in [ADR-0298](./0298-an-unattended-mode-the-host-decides-within-preset-bounds.md)
  (proposed) §SD3.

### SD9 — Asking the person

A moderator may ask the person instead of deciding: `llm.ration.ask`
proposes a rule ("window X has spent its hour; allow N more?") and the host
shows it in a dialog. The answer is written as the rule, with the person as
its author, or as nothing.

The host has no general dialog for this. The question is shown in the
agent dispatcher's dialog, the host's one place for a decision the person
owes, one at a time and ahead of the dispatcher's own requests. A question
nobody answers within a bound expires undecided. Unattended mode does not
answer these questions: they wait and expire, as the person's decisions do
in ADR-0298 §SD3.

### SD10 — Deferred, recorded

- **Device time and energy.** GPU time, energy and server-reported
  prefill/decode times. As of 2026-10-09 no LLM API reports energy per
  call and no standard names a field for it (survey §3). SD2's open set
  takes these quantities without a schema change.
- **Cost.** Weighted cost (cheap cached tokens, dear output tokens) and
  dollar prices per model.
- **Models.** Per-model rules, model allowlists and falling back to a
  cheaper model; there is one endpoint.
- **Testing a rule** against past trail rows before it is set.
- **Suspending** an instance's `llm.*` capability. A refusal rule does the
  same.
- **Deferring a refused call** to a watchbill job that runs when the server
  is idle; watchbill has no model job kind (ADR-0254 §SD7).
- **Showing use to the person**: a usage badge per window, and marking a
  throttled window through inscribe
  ([ADR-0297](./0297-inscribe-an-overlay-agents-annotate-the-desktop-through.md),
  proposed).
- **Telemetry export** in the OTel GenAI shape.
- **Cutting off a streamed answer** when it crosses a budget, once
  [ADR-0286](./0286-streamed-model-answers-over-a-private-inbox.md)
  (proposed) is built.
- **A synchronous policy hook** in Go, for a rule SD4 cannot express.
- **Restoring rules at start** from their audit events (SD4).
- **The moderator app itself.** This decision provides the surface a
  moderator drives; which moderator runs, and what policy it follows, is a
  decision of its own.

### Milestones

- **M1 — Meter.** SD2, SD3 and `keelson('llm_usage')`; cached and reasoning
  tokens on the trail.
- **M2 — Rules and admission.** SD4 and SD5, with the typed refusal and
  what remains.
- **M3 — Queue.** SD6.
- **M4 — Moderator.** SD7 and SD8.
- **M5 — Asking the person.** SD9.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `llm` exported API: `Response`, `RefusedError` | added: what remains; refusal kind, retry time, rule | chat's turn loop reads them |
| `llm` wire reply (CBOR) | added fields | client and service in one commit |
| `trail.LlmCall` (facts schema) | added: cached and reasoning tokens, admission outcome, rule, time queued | runtime vocabulary memberships 304–308; trail store regenerated |
| Audit events on `boxer.facts` | new domain `llm-ration` (rule changes, cancels, asks) | — |
| Capability subjects | added `llm.ration.{set,list,cancel,ask}`, `llm.event.*`, `runtime.agent.moderate.{stop,authority}`; `llm.ModeratorCaps`, `agent.ModeratorCaps` | ADR-0026 subject taxonomy; both services' `ServiceCaps` |
| Environment variables | added `BOXER_LLM_MODERATORS`, `BOXER_LLM_RATION_UNRULED` | [doc/env-vars.md](../env-vars.md) regenerated |
| Introspection | added `keelson('llm_usage')`, `keelson('llm_rations')`; `keelson('llm_calls')` gains the admission and token-part columns | — |
| `openaichat.CompletionResponse` | added: cached and reasoning tokens | — |
| Agent dispatcher | the dialog also shows a moderator's question | host boot wires the model service's asker to it |

## Alternatives

- **A judge on every call (O1).** Every call waits on a model-backed agent
  and fails with it.
- **A gateway in front of the endpoint (O3).** It sees one key and loses
  window, instance and task.
- **Apps limit themselves (O4).** Nothing stops an app that does not.
- **Charge after the call**, as Envoy AI Gateway and Cloudflare do. Their
  counters are shared across replicas, which makes reservation expensive;
  here reservation is cheap. Charging after lets parallel agent rounds
  overshoot together.
- **Virtual keys per app.** The bus already identifies the caller in a way
  apps cannot forge; a key adds something that can be shared or leaked.
- **Budgets in dollars first.** Most calls go to local models, which have no
  price.
- **The moderator as a host component.** It could not be replaced or turned
  off without a rebuild, and would not be metered as an app is.
- **A pace per task for slowing loops.** A rate rule on the task's account
  does the same through one mechanism.

## Consequences

### Positive

- Use per window, app, task and purpose is visible while it happens, not
  only after.
- A moderator can throttle, refuse, cancel and stop without sitting on the
  call path.
- What the person is looking at is served first when the server is busy.
- Every rule and every refusal is on the record with its author.

### Negative

- Every call takes a lock on the ledger and a rule lookup.
- The queue holds calls the person may experience as slowness rather than
  as a refusal.
- Budgets on output tokens are estimated until the call returns; a clamp
  can stop an answer short.
- Two new privileged paths (rule writes, moderator verbs on tasks) widen
  what a listed app can do.
- Calls that bypass the service (the commit-digest CLI calls `openaichat`
  directly) are not metered.

### Neutral

- Without rules and without a moderator, behaviour is unchanged apart from
  the ledger and the events.

## Migration — Tier 1

- **Breaks.** Nothing: every change is an added field, subject, table or
  variable.
- **Path.** None. Trail rows written before the change have no admission
  fields and read as null.
- **Regeneration.** `doc/env-vars.md` from the env registry.
- **Old shape.** Kept; nothing is removed.

## Verification plan — Tier 1

- **Lane.** Default `go test`: reservation and settlement under concurrent
  calls, rule evaluation across nested accounts, queue order by class and
  aging, a refusal's kind (package `ration`); the service's admission,
  events, moderator bounds, cancel and ask, and the ledger's rebuild from
  the trail over clickhouse-local (package `llm`); the moderator's verbs and
  question queue (package `agent`). A headless scene with `BOXER_LLM_SCRIPT`
  and a rule is not written yet.
- **What would fail.** A call admitted past a budget under concurrency; a
  background call served before a focused one; a refusal without its rule.
- **Gap.** Queueing against a real server's capacity is not tested; the
  scripted model has none. The question dialog's rendering is not covered
  by a test; its queue is.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [llm-gateway-metering-survey](../adr-background-work/llm-gateway-metering-survey.md) — the gateway survey and the energy findings.
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — capability subjects.
- [ADR-0254](./0254-model-inference-as-a-keelson-capability.md) — the `llm.*` capability.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — task grants.
- [ADR-0276](./0276-agents-read-and-arrange-windows.md) — window state off the render thread.
- [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) — the trail.
- [ADR-0280](./0280-a-ceiling-on-what-a-chats-model-may-do-scored-on-a-ladder.md) — ceilings.
- [ADR-0298](./0298-an-unattended-mode-the-host-decides-within-preset-bounds.md) (proposed) — unattended mode.
