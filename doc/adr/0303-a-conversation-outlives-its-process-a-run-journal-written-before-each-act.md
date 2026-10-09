---
type: adr
status: proposed
date: 2026-10-09
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0303: A conversation outlives its process — a run journal written before each act, resumed by the person, with grants asked again

## Context

A conversation in the chat app ([ADR-0265](./0265-chat-app-over-retained-model-calls.md))
that drives other apps holds an agent task
([ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)).
Everything that makes it a conversation — the message history, the grant,
pending approvals, the ceiling, taint — lives in process memory. A host
restart, a crash or closing the window ends it; the next window starts an
empty conversation with a fresh id (ADR-0265 §SD2). That was deliberate for a
first version: ADR-0265 §SD5 defers "resume and a conversation list", and its
Alternatives reject "resume in v1" because the only record, the kept
`llmMessage` rows, is audit that ADR-0264 §SD6 says no app should treat as
state — the *ditch test*: an app must behave the same with the kept bodies
cleared.

The cost shows as soon as a task runs longer than a sitting. ADR-0298 lets a
task run unattended inside a ceiling until its budget or deadline; ADR-0302
watches loops; neither helps when the process goes away mid-task. The person
then cannot tell which commands were applied, cannot continue, and cannot
even read the conversation back in the app.

Three surveys compiled on 2026-10-09 frame the decision
([engines](../adr-background-work/agentic-workflow-engines-survey.md),
[literature](../adr-background-work/agentic-workflow-literature-survey.md),
[security](../adr-background-work/agentic-workflow-security-survey.md)). The
points this ADR rests on:

- **Every system that resumes re-executes something** (engines §3.3,
  observation 1). The choices are the unit that re-runs and whether
  divergence is detected. Exactly-once holds only where an effect and its
  record share a transaction.
- **A model response is a nondeterministic event to record, never to
  regenerate.** Hosted inference is not reproducible even at temperature 0
  (literature §8); log-based recovery records such *determinants* and replays
  them (Elnozahy et al., literature §1).
- **Log before acting.** Without a record written before a tool call leaves,
  a crash cannot distinguish "never sent" from "sent, outcome unknown";
  agents asked to retry without read-back duplicate writes in 56–74 % of
  episodes, and idempotency keys cut duplicates from 28 % to 4 % (literature
  §2). LogAct and Agent libOS make the same point: surface an ambiguous
  outcome, do not replay it blindly.
- **Approvals re-spent by replay.** Retries and crash-resume re-use one human
  approval unless it is bound to its action (CapLease; security §3.3).
- **Applying new code to paused runs strands them** (LangGraph's documented
  behaviour, engines §3.14); the durable engines pin a run to the code it
  started with.
- **The measurement contradicts the docs.** A model-checked resume harness
  found shipping frameworks at-least-once across a kill where their
  documentation implies more (literature §2) — resume semantics have to be
  tested, not read.

The tree constrains the shape (2026-10-09):

- **Grant handles are never stored** and bind to the coordinator's instance
  and epoch; closing the coordinator bumps the epoch (ADR-0269 §SD6). A grant
  cannot outlive the window that asked for it without amending that rule.
- **Instance keys are per-process counters** (ADR-0191 §SD1). Every
  operation target, revision, snapshot and result reference names a live
  instance and dies with the process (ADR-0269 §SD1).
- **The coordinator, the dispatcher and the windows it drives share one
  process.** A crash takes the windows' in-memory state with it; what
  survives is what an app persisted (ADR-0148, ADR-0185) and what left the
  process — egress, published bundles, jobs.
- **No window-less coordinator exists.** `SurfaceHeadless` has no host
  (ADR-0026 M5), a call from instance 0 is unwatched by the moderator
  (ADR-0302 §SD3) and ranked last by the ration queue (ADR-0300 §SD6), and a
  run with no person has no principal (ADR-0277 §SD10).
- **Periodic work needs its own ADR by name** (ADR-0223 §SD6, ADR-0234
  §SD7).

## Design space (QOC)

**Question.** How does a conversation that holds an agent task continue
after the process that ran it is gone?

**Options.**

- **O1 — No resume.** Keep ADR-0265 as it is; at most show old
  conversations read-only.
- **O2 — Read the trail back.** Rebuild the conversation from the retained
  `llmMessage` bodies and the `agentAction` records.
- **O3 — Checkpoint at turn end.** Persist the coordinator's memory when a
  turn ends; resume from the last completed turn.
- **O4 — A run journal written before each act.** A store owned by the run,
  appended before every model call and every tool call leaves and after each
  returns; resume folds it back; grants are asked again.
- **O5 — Deterministic replay of the turn code.** Run the turn loop as a
  durable workflow on watchbill whose recorded results are replayed, as
  Temporal and Durable Functions do.

**Criteria.**

- **C1 — No duplicated, no silently lost effect.** A command that may have
  left the process is never re-sent without someone deciding; one that did
  not leave is not reported as done.
- **C2 — Consistent with decided ADRs** — the ditch test (ADR-0264 §SD6),
  unstored handles and epochs (ADR-0269 §SD6), per-run instance keys
  (ADR-0191), write-ahead and host-stamped origin (ADR-0277).
- **C3 — Detects code change.** A run resumed under a different prompt or
  tool set is noticed, not continued.
- **C4 — Labels survive.** Taint and confinement continue with the
  conversation (ADR-0269 §SD7, ADR-0264 §SD5).
- **C5 — Cost.** New stores, protocols and rules of discipline for code.
- **C6 — The work continues.** The person can pick the task up where it
  stopped.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 | O5 |
|----|----|----|----|----|----|
| C1 | ++ | −  | −  | +  | +  |
| C2 | ++ | −− | +  | +  | −− |
| C3 | ++ | +  | −  | ++ | +  |
| C4 | +  | −  | +  | ++ | +  |
| C5 | ++ | +  | ++ | −  | −− |
| C6 | −− | +  | +  | ++ | ++ |

O2 fails the ditch by construction, needs `durable` retention with bodies,
and the action records carry only an argument digest. O3 loses exactly the
case that matters — a crash between a command leaving and its outcome
returning — because nothing marks a call as sent before the turn ends. O5
needs the turn loop to be deterministic Go, a grant and window instances
that survive the process, and a coordinator with no window; the last two
contradict ADR-0269 §SD6 and do not exist. O4 costs a store and a service
and wins the criteria that are not cost.

## Decision

We will give each conversation that runs under a coordinator a **run
journal**: an append-only record owned by the run, written before every
model call and tool call leaves and after each returns, separate from the
audit trail. A person resumes a run from a conversation list; the coordinator
folds the journal back into its history and labels, reports every call whose
outcome is unknown instead of re-sending it, asks for its grant again as a
new task in the same conversation, and refuses to resume a run whose prompt
or tool set has changed.

### Subsidiary design decisions

- **SD1 — The journal is the run's state, not its audit.** A generated
  record store in the house layout, on the ADR-0105 D3a pattern ADR-0223
  §SD1 used for watchbill: its own tables, `<db>.agentrun` (one row per run,
  updated in place: conversation id, owner app, coordinator, state,
  fingerprint, settings, labels, created, last entry, closed) and
  `<db>.agentrun_entry` (append-only: run, ordinal, kind, key, body,
  label). It is not a kind on `boxer.facts`: it needs its own retention —
  a run's entries are deleted with the run, never by the trail's expiry — and
  deleting a run must not touch the audit. The ditch test (ADR-0264 §SD6)
  keeps holding because the chat reads its journal, never the kept
  `llmMessage` rows; clearing those leaves resume unchanged.
- **SD2 — Written before each act.** The coordinator appends and flushes an
  entry before the act leaves, and appends its result after:

  | Kind | Written | Carries |
  | --- | --- | --- |
  | `user` | when the person sends | the message |
  | `request` | before a model call leaves | the call's key and the messages new since the last request |
  | `reply` | when the model call returns | the call id and the assistant message with its tool calls |
  | `intent` | before a tool call is dispatched | the call key (`modelCall#index`, ADR-0277 §SD6), tool, arguments, the operation's effect class |
  | `outcome` | when the tool call settles | the call key and what the model was told |
  | `asked` / `answered` | around `ask_user`, grant requests, widenings and confirmations | what was asked, and the decision with who made it |
  | `note` | when the coordinator injects a system note | the note |
  | `turn` | when a turn ends | its terminal state (ADR-0265 §SD8) |

  The entry is the unit: a run resumes after its last entry, never in the
  middle of one. A failed write before an act refuses the act, as
  `BOXER_TRAIL_REQUIRED` does for the trail (ADR-0277 §SD3), because an act
  the journal does not know of is the case this ADR exists to prevent.
- **SD3 — What a resume re-executes.** The fold reads the entries in order
  and settles each unfinished act by kind:

  | Last entry for the act | On resume |
  | --- | --- |
  | `request` without `reply` | the model call is made again — it has no effect beyond cost, which is metered as any call |
  | `intent` of a query (effect none or view) without `outcome` | nothing is sent; the model is told the call did not complete and may read again |
  | `intent` of a command (document, run) without `outcome` | **never re-sent.** The model is told `unknown`: the host stopped before its outcome was recorded. The window it addressed is gone; a new command to the same resource needs a fresh read, which the revision rule enforces (a command to an unread resource is `conflict`, ADR-0269 §SD1) |
  | `intent` of a consequential command without `outcome` | never re-sent, and listed to the person in the resume dialog with its arguments, before anything else happens |
  | `asked` without `answered` (`ask_user`) | the question is shown again |
  | `asked` without `answered` (grant, widening, confirmation) | lapsed; see SD4 |

  This is the prepare–dispatch–settle shape of the literature (surveys:
  literature §2) restricted to what boxer can know: a call's effect class is
  declared (`OperationSpec.Effect`), and only an effect that may have left
  the process — consequential — needs the person.
- **SD4 — Grants and approvals do not survive; they are asked again.**
  ADR-0269 §SD6 holds unchanged: no handle is stored, and the epoch dies with
  the process. A resumed run starts a **new task in the same conversation**:
  the coordinator requests access with the old plan, the journal's last grant
  terms as the proposal, and a plan line saying it resumes. The person
  decides as for any request; unattended mode decides within its ceiling as
  it does today (ADR-0298 §SD2). An approval pending at the crash is not
  carried over: approvals bound to an epoch cannot be re-spent by a replay,
  which is the failure CapLease describes (security §3.3), at the price of
  one more question. The two tasks are linked by the conversation id the
  coordinator states on its calls (ADR-0277 §SD1, `Conversation`); no trail
  field is added.
- **SD5 — Windows are opened again, never assumed.** Instance keys do not
  survive (ADR-0191 §SD1), so the journal's references to instances are
  history only. A resumed task reaches apps the way a new one does — launch
  entries in its grant, or windows the person shares — and reads before it
  writes. Journal entries record app ids beside instance keys so the resume
  dialog can say which apps the run used.
- **SD6 — A run is pinned to the code it started with.** At its first
  request the run records a **fingerprint**: the digest of the ordinal-0
  message (the system prompt as sent), the `ToolsDigest` of the fixed tools
  (ADR-0277), and the coordinator's id. Resume compares the fingerprint of the
  code now running; on a mismatch the run becomes `stranded`: its transcript
  stays readable, it does not continue, and the dialog says why. The build id
  (ADR-0277 §SD8) is recorded and shown, not compared — every rebuild would
  strand every run. Operation tools, offered per window when the chat is
  set to, vary by design between calls and are not in the fingerprint; an
  operation whose `OperationSpec.Version` differs from the journal's is
  reported to the model when it is next called. Continuing a stranded
  conversation under new code is a new conversation (deferred, SD9).
- **SD7 — Labels and settings travel with the run.** The run row carries
  taint, confinement, the full ceiling (ADR-0280 §SD2) and the settings fixed
  at the first send (Keep, Apps, Questions; ADR-0265). Resume restores them
  before the first new request: a tainted conversation stays tainted, a
  confined one stays confined, and its bodies are kept only on a local store
  (ADR-0264 §SD5's rule, applied to the journal). This settles ADR-0280
  §SD7's deferred "persisting the settings" for the chat.
- **SD8 — A read surface scoped to the caller.** A host service,
  `runtime.agentrun.*` (append, close, list, get, delete), answers on the
  in-process bus and scopes every call to `Msg.Sender` — the `app_logs`
  shape ADR-0265 §SD5 named. An app sees only its own runs. A run is
  `open`, `closed` (the person ended it), `stranded` (SD6) or `deleted`;
  deletion removes the entries. Two introspection tables, `agent_runs` and
  `agent_run_entries`, serve the same rows to the owner and to `keelson`
  readers granted them.
- **SD9 — What stays out.**
  - *Automatic resume and window-less runs.* Continuing without a person
    needs a host for `SurfaceHeadless`, a principal for a run nobody started,
    and a moderator and ration queue that do not ignore instance 0
    (ADR-0302 §SD3, ADR-0300 §SD6). Watchbill is where such a run would
    execute; it is not used here.
  - *Triggers.* A schedule is periodic work and needs its own ADR by name
    (ADR-0223 §SD6, ADR-0234 §SD7); an event trigger needs a cause on bus
    messages, which `app.Msg` lacks, to avoid acting on events its own runs
    caused. The literature survey's §9.7 lists the questions that ADR must
    answer; who may fire a trigger is first among them.
  - *Seeding a new conversation from a stranded one,* restoring ration rules
    (ADR-0300 §SD10), and resuming a run in a window other than the chat.

### Milestones

- **M1 — The store and the service.** `agentrun` record store, the
  `runtime.agentrun.*` service with sender scoping, the introspection
  tables; no coordinator writes yet.
- **M2 — The chat writes its journal.** Every kind in SD2, written before
  each act; a failed write refuses the act.
- **M3 — The fold and resume.** A conversation list in the chat; resume
  restores history, labels and settings (SD7), settles unfinished acts
  (SD3), and asks for a grant (SD4).
- **M4 — Pinning.** Fingerprint recorded and compared; `stranded` runs shown
  read-only (SD6).
- **M5 — A resume conformance lane.** The properties of the Verification
  plan, run against a host killed at each act boundary.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `<db>.agentrun`, `<db>.agentrun_entry` | added: generated record store and DDL | the store's generator test and its `*.out.go`; the house layout's database placement |
| `runtime.agentrun.*` bus subjects | added: append, close, list, get, delete, sender-scoped | the chat manifest's caps; `hostboot` wiring |
| `keelson('agent_runs')`, `keelson('agent_run_entries')` | added introspection tables | the introspection registry and its doc |
| Chat app (ADR-0265) | reshaped: writes a journal before each act; gains a conversation list and resume | its scenes; ADR-0265 gains an Updates entry pointing here for §SD5's deferred item |
| Agent grant request plan | reshaped by convention only: a resumed task's plan says it resumes and shows the old terms | none — no wire or trail field changes |

## Alternatives

O1, O2, O3 and O5 are weighed in the QOC above. Three narrower choices:

- **Keep grants across the restart by storing the handle.** Rejected: it
  reverses ADR-0269 §SD6, and a stored approval is exactly what a replay
  re-spends.
- **Journal on `boxer.facts` as a new kind.** Rejected: a run's entries must
  be deletable with the run and kept as long as the run, which the shared
  facts table cannot do without a retention of its own; and a kind on facts
  invites reading the audit as state.
- **Re-send commands with their old call key and let the app deduplicate.**
  Rejected: the key's deduplication lives in the dispatcher's memory and in
  the window, both gone (ADR-0269 §SD1); the key would be honoured by
  nothing.

## Consequences

### Positive

- A conversation survives a restart, a crash and a closed window, and the
  person can see what was done, what may have been done, and continue.
- No command that may have left the process is sent twice without a person
  or a fresh read deciding; the literature's most expensive failure,
  duplicated writes under retry, has no automatic path.
- Approvals cannot be replayed; the security of the grant model is
  unchanged.
- A change to the prompt or the fixed tools strands old runs visibly
  instead of steering them with code they did not start with.
- ADR-0265 §SD5's deferred conversation list and ADR-0280 §SD7's deferred
  persisting of settings are settled for the chat.

### Negative

- Every act waits for a store write; at tens of entries per turn this adds
  to latency the model call already dominates, but a slow or failed store
  stops the conversation (SD2).
- A resume asks the person again and reopens windows; unattended mode
  shortens this only within its ceiling.
- The journal duplicates text the trail may also keep when retention is
  `durable`; the two serve different readers and have different lifetimes.
- A command whose outcome is `unknown` leaves the person to find out whether
  it applied; boxer cannot know, because the window that would have known is
  gone.

### Neutral

- Watchbill is not involved; it becomes relevant when a run may continue
  without a person (SD9).
- Other coordinators can adopt the journal through the same service; the
  chat is the first.

## Migration — Tier 1

- **Breaks.** Nothing; the store, service and tables are new, and
  conversations started before the change have no journal and cannot be
  resumed.
- **Path.** None needed.
- **Regeneration.** The new record store's generator test.
- **Old shape.** A conversation without a journal behaves as today.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the fold — a journal in, a coordinator
  state and a list of unfinished acts out — and for the service's sender
  scoping. The integration lane for the store against clickhouse-local. A
  resume conformance lane (M5): a scripted model (`BOXER_LLM_SCRIPT`) on the
  headless host, the host killed at each act boundary in turn, then resumed.
- **What would fail.**
  - A command whose `intent` has no `outcome` is dispatched again after
    resume.
  - A model call whose `reply` is recorded is made again.
  - A resumed task acts before its new grant is decided.
  - A tainted or confined run resumes without its label.
  - A run whose fingerprint differs resumes instead of stranding.
  - Clearing the kept `llmMessage` bodies changes what resume restores (the
    ditch test).
  - An app lists or reads another app's runs.
- **Gap.** Effects outside the process — egress, published bundles, jobs —
  are reported as `unknown`, not reconciled; whether one applied is left to
  the person. The lane kills the process at act boundaries, not inside the
  store's own write.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [ADR-0105](./0105-keelson-adopts-generated-record-stores.md) — generated record stores.
- [ADR-0148](./0148-app-workingsets.md) — workingsets; desktop resume deferred.
- [ADR-0185](./0185-durable-app-state-manager.md) — durable app state.
- [ADR-0191](./0191-runtime-instance-attribution.md) — instance keys per run.
- [ADR-0223](./0223-watchbill-durable-work-on-facts.md) — watchbill; periodic work out of scope.
- [ADR-0234](./0234-watchbill-client-protocol-and-worker-presence.md) — watchbill client protocol.
- [ADR-0254](./0254-model-inference-as-a-keelson-capability.md) — model inference.
- [ADR-0264](./0264-retained-model-conversations-on-facts.md) — retained conversations; the ditch test.
- [ADR-0265](./0265-chat-app-over-retained-model-calls.md) — the chat app; resume deferred.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — operations, grants, epochs, revisions.
- [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) — audit trail, write-ahead, call keys.
- [ADR-0280](./0280-a-ceiling-on-what-a-chats-model-may-do-scored-on-a-ladder.md) — the ceiling.
- [ADR-0298](./0298-an-unattended-mode-the-host-decides-within-preset-bounds.md) — unattended mode.
- [ADR-0300](./0300-metered-model-calls-and-rules-a-moderator-writes.md) — metering and ration.
- [ADR-0302](./0302-a-built-in-moderator-loop-signals-and-an-escalation-ladder.md) — the built-in moderator.
- [Agentic workflow engines — a feature survey](../adr-background-work/agentic-workflow-engines-survey.md)
- [Agentic workflows in the research literature](../adr-background-work/agentic-workflow-literature-survey.md)
- [Agent security — attacks, defenses and shipped controls](../adr-background-work/agentic-workflow-security-survey.md)
