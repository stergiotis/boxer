---
type: adr
status: accepted
date: 2026-10-11
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-12
---

# ADR-0323: Attention levels — one table, two scales, configurable within a floor

## Context

A person may run several model conversations at once, each producing
events: model calls, tool calls, answers, refusals, and requests that wait
on the person. Every surface that shows those events has to decide how loud
each one is — the chat transcript's per-line mark, which steps of a turn's
trail fold away, a roster of conversations (ADR-0265, accepted), and the
order of the person's decisions (ADR-0324). Decided per surface,
the same event is loud in one place and silent in another.

Instant-messaging and IRC clients, and industrial alarm practice, answered
this with graded levels from one table
([background](../adr-background-work/juggling-many-agent-streams.md) §2,
§3.2). The person asked for the levels to be configurable. Two constraints
come with that:

- Since ADR-0265's update of 2026-10-08, a call that waits on the person
  holds its turn until decided or expired. A level that hides a waiting
  item stalls a conversation without trace — the defect ADR-0269's update
  of 2026-10-04 fixed by making the person's dialogs modal.
- A level that raises an event nothing waits on into the person's queue
  produces an item with nothing to decide.

## Decision

We will classify every event shown to the person by one host-held table into
a level on one of two scales. The event's nature picks the scale; the
person configures the level within it, at three scopes. Nothing can be
configured across scales.

### SD1 — Two scales, picked by the event

| Scale | Levels, low to high | Holds |
|---|---|---|
| attention | data · message · highlight | events nothing waits on |
| waiting | your turn · critical | events a turn or a decision is blocked on until the person acts |

The waiting scale holds: a grant request and a widening (ADR-0269 §SD6), a
confirmation of a consequential command (ADR-0269 §SD5), a moderator's
question (ADR-0300 (proposed) §SD9), and a question an app asks on a
model's behalf — `ask_user` (ADR-0265 §SD7) and the exit approvals of
ADR-0312. Everything else is on the attention scale, a watchbill job's
events among it: a job owes the person no decision, so it never blocks a
turn and never joins the queue (ADR-0324 §SD8); a failure that should reach
the person is a highlight. Which scale an event is on is code, not
configuration.

### SD2 — The floor

An override moves a level within its scale and never across. Nothing that
waits can be set below *your turn*; nothing that does not wait can be set
into the waiting scale. The floor is checked where an override is stored,
and an override that would cross is refused.

### SD3 — Keys

A row's key is the event's **kind**, optionally narrowed by **effect**
(ADR-0269 §SD5) and by **app id**. Kinds are a closed vocabulary: the run
journal's entry kinds (ADR-0308 §SD2) for what a conversation records, and
the host's own (grant request, widening, confirmation, moderator question,
app question) for what the host asks. A kind the table does not list is
*data* on the attention scale, or *your turn* on the waiting scale.

### SD4 — Three scopes, the most specific winning

1. **Defaults**, in code (SD6).
2. **The person's table**, held and persisted by the host, so every surface
   reads the same one.
3. **Per conversation**, keyed by the conversation id (ADR-0277 §SD1).
   "Tell me when this turn ends" is a finished `turn` raised to
   *highlight* for one conversation; marking a conversation urgent is its
   waiting items raised to *critical*. Neither is a mechanism of its own.
   These last as long as the host that holds them; only the person's table
   is kept across restarts.

### SD5 — Who may change what

- The **attention scale**, at any scope, may be set by the person in host
  settings, and per conversation by the app that owns the conversation, on
  the person's action in its own window.
- The **waiting scale**, at any scope, is set only in host chrome, which no
  app or agent can draw or click (ADR-0269 §SD5). A change is recorded on
  the trail with the person as its author, since it decides what reaches the
  person first (ADR-0324). An app's operations catalog offers no
  operation that sets a level.

The person's table is edited as a grid — kinds as rows, levels as columns —
with no rule language.

### SD6 — Defaults

| Event | Scale | Level |
|---|---|---|
| `user`, `request`, `reply`, `answered`; a succeeded `outcome`; an `intent` of effect none or view | attention | data |
| an `intent` of effect document or run; a refused `outcome`; `note`; a `turn` that answered, failed, stopped or was cancelled | attention | message |
| a watchbill attempt that failed and will be retried under the job's policy | attention | data |
| a watchbill job discarded after its last attempt, or abandoned | attention | message |
| — (reached by overrides only) | attention | highlight |
| grant request, widening, app question, exit approval | waiting | your turn |
| confirmation of a consequential command; moderator question | waiting | critical |

An event is classified by its kind first and its effect second, so a
consequential confirmation is one row, not also an `intent` row. A job's
events are keyed by the job's terminal state, not by a journal kind; the
app that owns the job raises them to *highlight* through its own scale
(SD5), as it would any event of its conversations.

### SD7 — What reads the levels

The table is read by the host's queue (ADR-0324) for order and for what
opens at once, and by the chat app for the transcript's per-line mark, the
trail's folding of *data* steps, and a roster's level per conversation (the
highest level reached since the person last looked). The host serves the
effective table to apps on the bus and announces changes; an app does not
keep its own copy of the rules.

A *highlight* is the one attention level that reaches host chrome. An app
reports an event of its conversation that the person has not seen; the host
classifies it with its own table and keeps it only at *highlight*, one per
source, the latest. A source is a conversation, or, for a job, the owning
app and the job id. The watchbill worker reports a job's terminal state
itself, being host-side and the one that settles the row, so a job enqueued
by an app that is no longer open still reaches the person; a job's
highlight points at the watchbill window, where a retry lives. The control
of ADR-0324 counts these after what waits, and lists them apart; they open no card and are not items
of the queue. One goes when the person looks at the conversation's window or
dismisses it. Since an app sets its own conversation's attention scale
(SD5), it can make any of its events a highlight; what that buys it is a
count the person can dismiss, never a card or a place in the queue.

### SD8 — Deferred, recorded

- **Highlight words** in a model's text, IRC's raise-on-a-word. Additive:
  a key, not a new scale.
- **Levels set by a moderator's rule** (ADR-0300). A rule that raised an
  item to *critical* would be an author other than the person on the
  waiting scale; it needs its own decision.
- **Effects per level** — a sound, a window flash, an OS notification.
- **The person's status** (here, busy, away) and what it lets through: its
  own ADR, reading these levels.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Host attention service (`runtime.attention`) | added: the table, its scopes, the floor check, the bus subjects that serve the effective table and announce changes, and the highlights apps report | the queue of ADR-0324, which lives in the same service |
| Run journal kinds (ADR-0308 §SD2) | unchanged; read as keys | a new kind is *data* until the defaults name it |
| Watchbill worker (ADR-0223) | added: reports a job's terminal state to the attention service as a notice whose source is the owning app and the job id | the management window, which the highlight points at |
| Trail | added: an event for a change on the waiting scale | trail views |
| Chat app | reads levels for the transcript mark, trail folding and a roster | ADR-0265 gains a dated update on acceptance |

## Alternatives

- **One flat scale, freely configurable.** A waiting item could be set to
  *data* and stall its conversation unseen — the 2026-10-04 defect by
  configuration.
- **Fixed levels, not configurable.** The person asked for configuration,
  and per-conversation watch and urgency need overrides in any case.
- **Levels declared by each app in its manifest.** An app would grade its own
  requests; every app's questions would be critical.
- **A rule language** (KVIrc's event scripting, background §2.3). The grid
  covers the keys of SD3; rules beyond it belong with the moderator's,
  which are recorded (ADR-0300).

## Consequences

### Positive

- One event is equally loud on every surface.
- Watch and urgency are overrides rather than features.
- No configuration can hide a waiting item.

### Negative

- A host service and a persisted table the chat app depends on; without a
  host (tests, a bare app) the defaults apply.
- Two places to edit: attention per conversation in the app, waiting in host
  chrome.
- A kind added to the journal is silent (*data*) until someone gives it a
  default.

### Neutral

- *highlight* is unreachable without an override.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the attention service.
- **What would fail.** A table test that every crossing override is refused
  (SD2); a golden of the defaults (SD6) beside the journal's kind golden, so
  a new kind shows up as unlisted; a bus test that an app's request to set a
  waiting-scale level is refused (SD5).
- **Gap.** Whether every surface reads the service rather than a copy is
  review, not a lane.

## Status

Accepted 2026-10-12. ADR-0265 gains a dated update naming the transcript,
trail and roster uses (SD7).

Built, except the watchbill worker's report: SD6's job rows and SD7's job
highlights are decided and not built. Until they are, a job's end reaches
the person only in the watchbill window.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## Updates

### 2026-10-12 — Urgent: an app asks, the person confirms in host chrome

SD4's "marking a conversation urgent" has a control. An app asks the host
to make one of its conversations urgent, or to stop (`runtime.attention.urgent`);
the request is an item of the queue, of the host's own kind `urgency`
(*your turn*), drawn as a host card that carries no model text. Only the
person's **Make urgent** in that card sets the conversation's waiting kinds
— grant request, widening, confirmation, question, exit approval — to
*critical*, as the person's change, on the trail (SD5); an app still cannot
set the waiting scale, so it cannot make itself urgent, whoever drives it.
The card opens at once only when the request comes from the active window,
where the person just asked for it; a request from another window waits in
the queue like any item and counts against the flood limit (ADR-0324
§SD6). An urgent conversation's items open at once also while the person is
Busy (ADR-0328 §SD2). The chat's Urgent sits beside Watch.

## References

- [Juggling — one operator, many streams](../adr-background-work/juggling-many-agent-streams.md)
  — the precedents and options this ADR draws on.
- [ADR-0265](./0265-chat-app-over-retained-model-calls.md) — the chat app;
  §SD7 questions, §SD8 turn machine, update of 2026-10-08.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
  — effects, modes, grants, host chrome.
- [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) —
  the conversation id.
- [ADR-0300](./0300-metered-model-calls-and-rules-a-moderator-writes.md)
  (proposed) — moderator rules and the person's question.
- [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md) —
  the journal's entry kinds.
- [ADR-0312](./0312-exits-sinks-from-the-cell-boundary-approval-tiers-and-exit-budgets.md) —
  exit approvals.
- [ADR-0324](./0324-the-persons-decisions-wait-in-a-host-queue-not-in-modals.md)
  — the queue these levels order.
