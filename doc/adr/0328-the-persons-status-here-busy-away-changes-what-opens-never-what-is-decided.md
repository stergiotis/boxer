---
type: adr
status: accepted
date: 2026-10-12
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-12
---

# ADR-0328: The person's status — Here, Busy, Away — changes what opens, never what is decided

## Context

ADR-0324 (proposed) holds every decision the person owes in one host queue
and decides what opens on its own: a *critical* item's card when it
arrives (§SD3), and a *your turn* item's card once it has waited
unacknowledged for `BOXER_ATTENTION_ESCALATE` (§SD4). ADR-0323 (proposed)
adds highlights, such as a watched turn that ended, which the host's control
counts. All of this assumes the person is at the screen and willing to be
interrupted. It deferred the person's status to its own decision
(ADR-0324 §SD8).

Two cases are not served:

- **Busy.** The person is at the screen and does not want a card opening
  on an escalation in the middle of something else. The only way to avoid
  it is to lower levels, which changes the table for every conversation.
- **Away.** A card that opens while nobody is at the screen is seen later,
  with the others, in no order. When the person returns, nothing tells them
  what arrived, what expired and what was decided without them; ADR-0324's
  queue lists only what still waits.

The precedents (the background note, §2.1, §2.2) pair an operator status,
set by hand or by idle time, with an away log read on return. They also
show two things not to copy (§6): a status that other parties read freely,
and a status that changes what the system does on the person's behalf.

## Design space (QOC)

**Question.** What does the person's status change?

- **O1** — Nothing; the person lowers levels when busy.
- **O2** — What opens on its own, and a log of what happened while away.
- **O3** — O2, and Away lets the host decide some items (skip a question,
  refuse a request) on the person's behalf.
- **O4** — O2, and models are told the status, so they can choose to wait.

**Criteria.**

- **C1** — The person is not interrupted against their choice.
- **C2** — Nothing that waits on the person is lost or decided without
  them.
- **C3** — The status is not information about the person that leaves the
  host without a grant.
- **C4** — On return, the person learns what happened while they were away.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −  | ++ | ++ | ++ |
| C2 | ++ | ++ | −− | ++ |
| C3 | ++ | ++ | ++ | −− |
| C4 | −− | ++ | +  | ++ |

O3 makes a presence setting widen what the host does unasked, which is
ADR-0298's decision, with its own bounds. O4 sends a fact about
the person to a model endpoint that may be remote.

## Decision

We will give the person a status — Here, Busy or Away — held by the
attention service and set in host chrome, by hand or, for Away, by idle
time. The status changes which cards open on their own and starts an away
log; it changes nothing that is decided, no clock, and nothing a model is
told.

### SD1 — Three statuses, held by the host

The attention service (`runtime.attention`) holds one status for the
person. The host's control shows it, and the control's menu sets it.

- **Here** is the default at start.
- **Busy** is set by hand and ends by hand.
- **Away** is set by hand, or by the host after
  `BOXER_ATTENTION_AWAY_IDLE` (10 minutes to start with; 0 never) without
  input. Away set by idle ends at the next input and returns to the status
  before it; Away set by hand ends by hand. Input is what the host already
  reads each frame — the pointer, the wheel, zoom, the modifiers, the keys
  the shell watches — and a change of the active window; no frame reports
  every key, so typing with the pointer still is not seen.

A status change is recorded on the trail with the person as author, or the
host for idle, as ADR-0323's waiting-scale changes are: it changes what
reaches the person first.

### SD2 — What each status opens

| | a *critical* item's card when it arrives | an escalated item's card | the control's count and marks |
|---|---|---|---|
| Here | opens | opens | shown |
| Busy | opens | does not open; marked | shown |
| Away | does not open | does not open | shown |

Busy keeps *critical* items breaking through: that is what *critical*
means (ADR-0323 §SD1). To let one conversation through Busy, the person
raises its levels to *critical* for that conversation, an override the
table already holds and the trail records. There is no separate "urgent"
flag.

An item the status holds back does not open later on its own: it is in the
control's list, marked "waiting long" once escalated, and while Away in the
log. Highlights never open anything at any status; they are counted, and
while Away they go to the log.

### SD3 — The away log

From the moment Away starts, the attention service records:

- items that arrived in the queue;
- items that left it without a decision in a host card: "expired" when
  their bound had passed, else "no longer waits" — withdrawn, answered in
  the asking window, decided by unattended mode (ADR-0298); the
  queue sees an item go, not why;
- requests the flood limit refused (ADR-0324 §SD6);
- highlights.

Each entry carries its source, conversation, kind, level and time. When
Away ends, the log opens as one host card, "While you were away", ordered
by level then time; an entry still in the queue has Open, and the card has
Dismiss. The log holds `MaxAwayLog` entries and counts what it dropped
beyond them. It lasts as long as the host, as the notices do; a new Away
starts a new log.

### SD4 — What the status does not change

- **Nothing is decided.** Skip stays the person's act (ADR-0265 §SD7), and
  Away never refuses, grants or answers anything. Away does not turn on
  unattended mode.
- **No clock moves.** Request expiry, a task's deadline and escalation's
  interval run as before; an item that expires while Away is in the log.
- **Models are not told.** The status is not put in the per-turn note
  (`runtime.agent.turn`) or in any request's reply. A grant term under
  which a task may read it is deferred (SD6).
- **Apps do not read it.** Host chrome reads it in process; a bus subject
  for apps is deferred with the grant term (SD6).

### SD5 — Headless and test hosts

The headless host keeps the status like any other; the control is the
host window ADR-0324 §SD7 names, so a scene can set Busy or Away and
assert on what opens. Driver input counts as input for idle time. Scenes
that do not test idle set `BOXER_ATTENTION_AWAY_IDLE` to 0.

### SD6 — Deferred, recorded

- **A grant term** by which a task, and so its model, may read the
  person's status, and a bus subject by which an app may.
- **Effects per status** — muting a sound or an OS notification — with the
  effects themselves (ADR-0324 §SD8).
- **An away message** shown to anyone. There is no one else to show it to
  on a single-person host.
- **Keeping the status across restarts.** Here at start is the safe side
  of C1's trade.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Attention service (`Presence`) | added: the status, the away log | ADR-0324's queue reads the status before opening a card |
| Window host | added: an input listener, told at each frame that shows input | — |
| Host chrome (`attentionchrome`) | added: the status in the control and its menu; the away log card | scenes that open the control |
| Env registry | added: `BOXER_ATTENTION_AWAY_IDLE` | `doc/env-vars.md` regenerated |
| Trail | added: status changes | trail views |

## Alternatives

O1, O3 and O4 lost on the QOC above. Further:

- **A Do-not-disturb that holds *critical* items too.** It would silence
  exactly the items the person said should reach them; Away already holds
  them when nobody is there to see them.
- **Away set only by hand.** People forget to set it; the away log would be
  empty when it matters most.
- **The away log as a list in the queue.** The queue holds what waits;
  most of the log's entries no longer wait on anything.

## Consequences

### Positive

- The person can work without escalations opening, and still be reached
  by what they marked *critical*.
- On return they see what arrived and what left without them, in one
  place.
- The status reaches no model and decides nothing.

### Negative

- A *critical* item does not open while Away; if the idle interval is long,
  it waits unseen until the person returns.
- Idle time is input to the host's windows; a person reading without
  touching anything, or typing without moving the pointer, turns Away.
- The log names an item that left as "expired" or "no longer waits", not
  who took it away.
- Another host card and another set of trail events.

### Neutral

- A model that waits on the person waits as long under any status; its
  turn still says *your turn* (ADR-0265 §SD8).

## Migration — Tier 1

- **Breaks.** None: Here is the default, and it behaves as ADR-0324 does
  without this decision.
- **Path.** Scenes that run longer than the idle interval set it to 0.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the attention service; a headless scene
  for the control and the log.
- **What would fail.** Busy not opening an escalated card but opening a
  critical one; Away opening neither; idle Away ending at input and
  returning to Busy when Busy was set before; a hand-set Away not ending at
  input; an item that expired while Away appearing in the log; the log
  bounded with a count; no status in the per-turn note.
- **Gap.** Whether 10 minutes suits as a default is a guess until it has
  been used.

## Status

Accepted 2026-10-12. Answers ADR-0324 §SD8's first deferral and the
background note's open question 6, except the grant term.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [Juggling — one operator, many streams](../adr-background-work/juggling-many-agent-streams.md)
  — §2.1, §2.2 precedents, §5.5 the option, §6 what not to copy.
- [ADR-0323](./0323-attention-levels-one-table-two-scales-configurable-within-a-floor.md)
  (proposed) — the levels and highlights.
- [ADR-0324](./0324-the-persons-decisions-wait-in-a-host-queue-not-in-modals.md)
  (proposed) — the queue, what opens at once, escalation.
- [ADR-0298](./0298-an-unattended-mode-the-host-decides-within-preset-bounds.md)
  (proposed) — unattended mode, which the status does not turn on.
- [ADR-0265](./0265-chat-app-over-retained-model-calls.md) — §SD7
  questions, §SD8 turn machine.
