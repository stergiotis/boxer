---
type: adr
status: accepted
date: 2026-11-04
reviewed-by: "p@stergiotis"
reviewed-date: 2026-11-05
reversibility: local-edit
---

# ADR-0358: Asynchronous operations — a pending outcome, and a turn a completion wakes

## Context

A chat turn waits for its tool calls. `settle` polls a call for up to ten seconds
unless the call waits on the person, then hands the model whatever phase it
reached. A query that outruns that wait reads as "queued; read its effect later",
and the model spends rounds polling, or answers before the result exists.

ADR-0315 §SD6 declared the contract for this — a command declares `Async`, and a
separate `completed` entry records the end of its work — and nothing implemented
it. No operation declares `Async`, nothing writes `completed` outside tests, and
ADR-0315 §SD8 names play's `run` as recording queuing for its outcome.

[The Infinity note](../adr-background-work/infinity-async-agent-runtime.md) reads
one project whose turns are not processes: a turn ends instead of waiting, and a
completion is one more input that restarts it. Boxer already has the prerequisite,
since a request is rebuilt from the journal every time (ADR-0308 §SD3). What
differs is authority: a grant dies with its window instance (ADR-0322), so a turn
that outlives its window needs a design this record does not attempt.

## Decision

**We will let a command stay `running` after it applies, record that as a pending
outcome, let the turn end, and start a new turn from the journal when the work
settles, all inside the window that holds the grant.**

### SD1 — An async command stays running until the app says it settled

A command declaring `Async` that applies moves to `running` instead of `applied`,
and the existing `Status` path reports it. The app reports settlement through an
optional `app.OperationsAsyncI`, which the engine asks once per frame for each
running call on the render goroutine. A frame-driven app already looks at its own
state there, so no callback crosses a thread. The result it returns is the call's
result, under the operation's declared type. The call's key is the handle; no
second identifier is introduced.

A window that goes away ends its running calls as `expired`, since no frame will
ever settle them. A receiver that serves an embedded app's catalog settles through
that app (`SettlesRaw`), as bundle views do for `bundle_run`.

### SD2 — Play's `run` is the first

`run` declares `Async` and settles when its result lands or the run fails, with
the `ResultState` that `get_state` would report. A query that finishes within the
wait now answers the model directly, with no follow-up read.

### SD3 — A call still running at the end of the wait is pending

`BOXER_CHAT_CALL_WAIT` bounds the wait, ten seconds by default, and is a profile
limit. A poll of the host does not outlast it, so a value under the host's
five-second poll binds. An `Async` call still running at the end of it gets an outcome in form
`pending`, which tells the model the work continues and that it will be told. A
non-async call is unchanged: it reports its phase at the end of the wait.

### SD4 — The record: `completed` when known, a note when told

A watcher polls each pending call off the turn, bounded by
`BOXER_CHAT_PENDING_MAX`. As soon as the call settles, a `completed` entry records
its phase and content under the content's label, so a late result raises the run's
label on arrival, whether or not the model is in a turn. `completed` is not a
message, so it may land between a reply and its tool results.

The model reads it through a `note` carrying the same key, written at the next
round boundary, where nothing can separate a reply from its results. The note has
no turn, so it survives a failed turn's drop. The gap between the two entries is
how a resume tells what the model was told.

### SD5 — A completion wakes a turn, within limits

When a note is waiting and no turn runs, the window starts a turn with no message
from the person. A turn ends as it does today, with the model's answer, or with
the pending work noted and nothing further to do.

A wake is withheld, and the completion waits for the person's next message, when:

- The person is editing a message they took back.
- The person pressed Stop since they last spoke.
- The task has ended, so the model holds no grant.
- `BOXER_CHAT_WAKES_MAX` wakes, four by default, have run since the person last
  spoke. This bounds a loop nobody asked for; each wake spends model calls without
  the person's asking.

A woken turn cannot be taken back with Edit: there is no message of the person's
to edit.

### SD6 — Pending work and the end of a conversation

Stop does not cancel running work; the app's own cancel operation does, and the
person holds the app's window. A retired conversation ends its watchers, each
writing `completed` in form `unknown`. A resume writes the same for any call the
record leaves pending, and tells the model. `Fold` reports pending calls across a
closed turn, which is the one kind of act a turn entry does not settle, since a
pending call is meant to outlive the turn that started it.

### SD7 — The same check on a tool call's intent

A call that names a model call must name its intent, with the reference and the
checks of ADR-0357 §SD2 over kind `intent` and the call's key. The rule rests on
`ModelCall` because a coordinator driven by hand, such as `agentconsole`, runs no
model and keeps no journal, and its calls stay legal. A harness that omits
`ModelCall` to escape the check gives up the join to the model on the trail, which
the trail shows. A call a tool makes on its way, keyed beneath the intent's key
(`<key>/<id>`, as a citation's checks are), is covered by that intent.

**Deferred.** Only operation calls are checked. The dispatcher's other verbs —
describe, list, capture, launch, requesting a grant — are read or are decided by
the person, and carry the same cause without the check.

### Deferred

An `Async` command with a consequential effect. None exists, and the endorsement
a person's decision writes (ADR-0311 §SD3) follows a call that completed in its
round, so one would need that written at settlement.

Several outstanding calls in one round (a second tool mode), child threads,
subscriptions, and window-less runs. The first is a follow-up to this record, to be
decided once this is in use. The rest need an answer about authority that outlives
a window, and ADR-0308 §SD11 keeps them out until they have one.

## Alternatives

- **Raise the wait.** Rejected as the fix: it only moves the point where the model
  is told to poll, and it holds a turn open meanwhile. The variable is still useful
  as a limit.
- **The app calls a completer from its own goroutine.** Rejected: the engine's
  state is the render goroutine's, and a frame-driven app sees completion in a
  frame anyway.
- **A separate handle returned by the command.** Rejected: the call's key already
  identifies it to `Status`, `Cancel` and the journal.
- **The `completed` entry as a projected message.** Rejected: it changes the fold
  and its SQL twin (law L4), and a message landing between a reply and its results
  makes a request no route takes.
- **A woken turn as a person's message.** Rejected: it would attribute to the
  person words they did not write, in the one record that says who said what.
- **Slices as the only mode, ending every turn at a tool call.** Rejected for now:
  it would restructure a loop whose synchronous path works, for a benefit that
  shows only on long calls.
- **Waking without a bound.** Rejected: a model that keeps starting async work
  would run indefinitely with nobody asking.

## Consequences

### Positive

- A long query no longer spends rounds on polling, and a short one answers directly.
- The person can talk to the chat while work runs.

### Negative

- A turn can start with no person asking for it, within a bound, which spends
  model calls; the transcript marks such a turn.
- `run`'s phases change for every caller of the catalog, from `rendered` to
  `running` then `completed`.
- A result can arrive after the model has moved on, and it reads as new input.

### Neutral

- The profile fingerprint gains the limits, so conversations started before resume
  read-only, as any prompt or profile change does.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `app.OperationSpec.Async` | enforced: the catalog requires a settler | the declaration's comment |
| `app.OperationsAsyncI`, `appops` | added | handlers declaring `Async` |
| `opengine` | a running stage for commands | `Status`, the log |
| Dispatcher | a running command absorbs its writes; a gone window expires it; intents checked | `agent.CallRequest`, wire |
| Play's `run` | declares `Async`, returns `ResultState` | play's scenes and agent tests |
| `agentrun.Fold` | pending calls survive a turn entry | resume |
| Chat | the watcher, the note, the wake, the transcript mark, the prompt | prompt and profile fingerprints |
| Env | `BOXER_CHAT_CALL_WAIT`, `BOXER_CHAT_PENDING_MAX`, `BOXER_CHAT_WAKES_MAX` | `doc/env-vars.md` |

## Migration — Tier 1

- **Breaks.** A caller reading `run`'s phase as `rendered`. Conversations started
  before this resume read-only.
- **Old shape.** Existing runs have no pending outcomes.

## Verification plan — Tier 1

- **Lane.** Default `go test`: the engine's running stage and expiry; the fold
  across a turn entry; the watcher's entries and labels; the round-boundary note;
  each condition that withholds a wake; retire and resume writing `unknown`; the
  intent check.
- **Gap.** The wake's model behaviour is a judgement a test cannot make: whether a
  model reads a pending outcome and a late note sensibly is for use to show.

## Status

Accepted 2026-11-05, with every decision implemented. Wakes are covered by the
default lane and, since the update of 2026-11-07, by one scene on a scripted
model; the gap in the verification plan stands.

## Updates

### 2026-11-05 — A settled call carries the revisions its work left

The engine asked the running calls whether they settled before it observed
what the app's frame changed. A run whose result landed in that frame
therefore completed with the `result` revision of the result before it, the
frame's end then moved the revision, and the caller's next command writing
`result` was a conflict, "result moved since it was read". A read between
two runs hid it; play's `query` (ADR-0270, update of 2026-11-05), which
writes `result` call after call, met it at once. The frame's end now
observes the app's changes first and settles after, so a completed outcome
carries the revisions its work left. Play's `run` and `query` also wait to
settle until the frame drew the result the panels show, which is what the
`result` resource holds, or until three frames passed for a window whose
frame draws no panels.

### 2026-11-07 — A scene for a wake, and the woken line is the host's

The chat's `chat-wake` scene runs play's `run` on a query that outlasts a
one-second wait, against a scripted model, and checks the pending outcome, the
completion, the woken turn's line and the model's answer on a real host. A
scripted model reads nothing, so the gap above is unchanged.

Writing it showed two faults in how §SD5 was drawn. The woken turn's line was
drawn as a message, which read as part of the model's reply; it is now drawn as
the host's line, as a tool call's is. The status line said a settled result
waited for the person's next message while a wake was about to read it; it now
says that only while a wake is withheld, by the same test that starts one.

## References

- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — operations and phases.
- [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md) — the journal; §SD11 keeps window-less runs out.
- [ADR-0315](./0315-coordinator-tools-as-operation-specs-and-a-contract-for-stateful-tools.md) — §SD6, the contract this implements.
- [ADR-0322](./0322-app-instances-drain-a-lifetime-scope-between-unmount-and-unload.md) — a grant's lifetime.
- [ADR-0357](./0357-a-model-call-names-the-journal-entry-written-before-it.md) — the check §SD7 repeats.
