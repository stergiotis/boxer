---
type: adr
status: accepted
date: 2026-10-11
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-12
---

# ADR-0324: The person's decisions wait in a host queue, not in modals

## Context

The person owes decisions to several sources: an agent task's grant
request, widening or confirmation (ADR-0269 §SD5, §SD6), a moderator's
question (ADR-0300 (proposed) §SD9), and a model's question in a chat
(`ask_user`, ADR-0265 §SD7; exit approvals, ADR-0312). Each holds a turn
until decided or expired (ADR-0265, update of 2026-10-08;
`BOXER_AGENT_REQUEST_TIMEOUT`).

They are shown in two opposite ways. The host's decisions are modals —
ADR-0269's update of 2026-10-04, after a dialog drawn as a window was
covered by an app window and a turn waited on it unseen. A modal cannot be
missed, but it takes the pointer from every window, so the person cannot
inspect the window a request is about before deciding, and decisions arrive
one at a time in arrival order whatever their weight. A chat's questions
are drawn in the chat's own window, which shows nothing when it is behind
others.

With several conversations running, the person is an operator juggling
streams. Messaging clients, IRC clients and alarm practice converged on a
queue graded by level, entered on the person's terms, with acknowledgement
apart from decision and a limit on how fast sources may add to it
([background](../adr-background-work/juggling-many-agent-streams.md) §3).
The levels it orders by are ADR-0323's.

The modal carries three properties a replacement must keep: a waiting item
**cannot be hidden**; a decision is made **on what it showed**, since
nothing else can be clicked while it is open; and it **cannot be spoofed**,
since only the host draws it.

## Design space (QOC)

**Question.** Where does a decision the person owes wait?

- **O1** — Modals, as since 2026-10-04.
- **O2** — Host dialogs as ordinary windows, as before 2026-10-04.
- **O3** — A host queue in the shell's top panel, opening non-modal host
  cards.
- **O4** — Operating-system notifications.

**Criteria.**

- **C1** — Cannot be hidden by an app window.
- **C2** — The person can inspect other windows before deciding.
- **C3** — Cannot be drawn or clicked by an app or agent.
- **C4** — Ordered by weight across many sources.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | −− | +  | +  |
| C2 | −− | ++ | ++ | +  |
| C3 | ++ | +  | +  | −− |
| C4 | −  | −  | ++ | −  |

O3's C1 and C3 are `+`, not `++`: both rest on SD2's requirements on the
shell, which the modal did not need.

## Decision

We will hold every decision the person owes in one queue in the host,
ordered by ADR-0323's waiting levels, shown as a control in the shell's top
panel and opened as non-modal host cards. Nothing is modal. A small question
is answered in its card; an item may be acknowledged or shelved; and the
rate at which a source adds items is bounded.

### SD1 — One queue, in the host

The attention service (`runtime.attention`, ADR-0323) holds the queue.
Host services post to it in process: the agent dispatcher its requests,
widenings and confirmations, a moderator's question its `llm.ration.ask`.
Apps post on the bus under a manifest capability for the subject — not a
task grant — and may post only a **question**: what `ask_user` and an exit
approval ask. An item carries its source (app id, instance, conversation
id), its kind, a summary, its expiry and, for a question, the form.

Order: level (ADR-0323 §SD1, waiting scale), then age. The control shows
"your turn · N" in the tone of the highest level present; **Next** opens
the head.

### SD2 — Cards, not modals; the three properties

Each item opens as a host card: drawn by the window host, outside every
app's window body, over no backdrop. The person may use other windows while
a card is open.

- **Not hidden.** The control lives in the shell's top panel. The window
  host keeps app windows out of the panel's rect and draws cards on a layer
  above every app window. An unacknowledged item escalates (SD4).
- **Decided on what it showed.** A card fixes its content when it opens —
  the request's terms, plan and the window list it offers to share. If what
  it refers to changes while it is open (a listed window closes, the request
  is withdrawn or expires), its decision buttons are disabled and the card
  says why; reopening shows the current state.
- **Not spoofable.** Cards and the control are drawn only on the host's
  layer, framed in the host's style with the source app's display name
  from its manifest. App windows cannot draw on that layer.

### SD3 — What opens at once

A *critical* item opens its card when it arrives, raised and focused but not
modal. A *your turn* item waits for **Next** or a click. With the defaults of
ADR-0323 §SD6, a consequential confirmation and a moderator's question open
at once; a grant request, a widening and a question wait.

### SD4 — Acknowledge, shelve, escalate

- **Acknowledge** marks an item seen, not decided. It stops escalation.
- **Shelve until** a time takes an item out of the count until then. The
  time is bounded by the item's expiry and its task's deadline, and the card
  shows which clock bounds it; shelving extends neither. At the end the item
  returns unacknowledged.
- **Escalation.** A *your turn* item unacknowledged for
  `BOXER_ATTENTION_ESCALATE` (5 minutes to start with) opens its card once,
  as a *critical* item does. Any item within that interval of its expiry is
  marked on the control.

Acknowledging and shelving are recorded on the trail with the person as
author. The model is told nothing of either: its call is still waiting, and
the turn machine (ADR-0265 §SD8) still says *your turn*.

### SD5 — Quick reply

A question card answers in place when the form is one question, single
choice, two to six options, each label within a bound the attention service
sets; otherwise the card offers **Open**, which raises and focuses the
asking window.

Host chrome already draws the model's text, labelled and bounded (ADR-0269
§SD5). A question card is the first host surface whose **choices** the
model wrote, so a question could be phrased to look like a host decision. A
question card therefore:

- is visibly distinct from a grant card, and uses none of its vocabulary
  (no Approve, Decline, Share);
- heads itself with host-written text: the app's display name, the
  conversation's title as the app reported it, "asks — the model's words";
- carries the conversation's taint line as the request card does;
- draws question and options as the model's text, in the model's style;
- offers Answer, Skip and Open, nothing else.

The answer goes back on the bus to the asking app, which applies it as an
answer given in its window: the same journal entry, the same tool result.
The in-window form stays; the first answer wins, and the other surface
withdraws its form.

### SD6 — The flood limit

The service admits waiting items at a bounded rate, counted over a sliding
ten-minute window:

- **per stream** — a conversation id, or the posting instance when there is
  none: `BOXER_ATTENTION_FLOOD_STREAM`, 5 to start with;
- **per person**: `BOXER_ATTENTION_FLOOD_PERSON`, 10 to start with, after
  EEMUA 191's alarm-flood figure.

The count is kept by stream id, so it survives turns. Items decided by
unattended mode (ADR-0298 (proposed) §SD2) never reach the queue and do not
count; a moderator's question is the host's own and is exempt. An item past
either limit is refused at admission, with a remedy the model can act on:

- a grant request, widening or confirmation returns refused with reason
  `flood`: "N decisions are waiting on the person; wait for one, or ask
  for fewer and broader";
- a question returns, as the tool result, "the person is not taking
  questions now; go on and say what you assumed".

Each refusal publishes an event a moderator may act on.

### SD7 — Headless and test hosts

The headless host has no top panel. Test grants (ADR-0269) decide requests
without chrome as before. A headless host that renders host chrome for the
driver shows the queue as a host window named so a scene can open, answer
and assert on cards.

### SD8 — Deferred, recorded

- **The person's status** — here, busy, away; what passes busy; the away
  log on return: ADR-0328.
- **Watchbill jobs and other non-agent sources** in the queue. A watchbill
  job owes the person no decision; a failure that should reach them would
  be a highlight (ADR-0323), not an item here.
- **Effects** — a sound, a window flash, an OS notification — per level.
- **A roster** of conversations in the chat app: an app change under
  ADR-0265, reading ADR-0323's levels.
- **Multi-select and multi-question forms** in a card.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Agent host chrome (`Chrome.RenderDialogs`) | removed as modals; requests, widenings and confirmations post to the queue and render as cards | ADR-0269 gains a dated update; scenes that wait on the modal |
| Moderator question (`Service.AskPerson`, ADR-0300 §SD9) | posts to the queue as a *critical* item | ADR-0300's §SD9 text, while proposed, edited in place |
| Attention service (`runtime.attention`) | added: the queue, cards, quick reply, flood limit, the post and answer subjects | ADR-0323's table in the same service |
| Window host | added: the control in the top panel, the host card layer, app windows kept out of the panel's rect | `windowhost` layout |
| Chat `ask_user` and exit approvals | also posted to the queue; answered from either surface | ADR-0265 §SD7 gains a dated update |
| Env registry | added: `BOXER_ATTENTION_ESCALATE`, `BOXER_ATTENTION_FLOOD_STREAM`, `BOXER_ATTENTION_FLOOD_PERSON` | `doc/env-vars.md` regenerated |
| Trail | added: acknowledge, shelve and flood-refusal events | trail views |

## Alternatives

O1, O2 and O4 lost on the QOC above. Further:

- **The queue in the chat app.** Grant decisions must be host chrome
  (ADR-0269 §SD5), and the queue holds requests from apps other than the
  chat.
- **Questions answered only in their window.** A question behind other
  windows waits unseen; the queue would list it but every answer would cost
  a window switch for a one-click choice.
- **No flood limit.** The person clears faster or misses items; alarm
  practice treats a flood as a design failure at the source.
- **Modal kept for critical items.** It would keep C2's cost exactly where
  the decision is heaviest and the window most worth inspecting.

## Consequences

### Positive

- The person can inspect what a request is about before deciding.
- One place lists everything owed, by weight.
- A chat's question is visible and answerable from any window.
- An agent that floods is refused with a reason, not obeyed by attrition.

### Negative

- Not hidden and not spoofable become requirements on the window host's
  layout and layering, which the modal met by construction.
- A card must track whether what it showed is still true.
- Model-written choices in host chrome are a new surface to keep bounded.
- A turn may wait longer: nothing forces a *your turn* decision until
  escalation.

### Neutral

- The fs Powerbox file dialog stays modal (ADR-0269, update of 2026-10-04):
  the person opened it; nothing waits on them from elsewhere.

## Migration — Tier 1

- **Breaks.** Scenes and tests that wait on or click the request and
  confirmation modals.
- **Path.** They address the card by its name, or, under test grants, are
  unchanged.
- **Old shape.** The modals are removed when the cards land; no setting
  keeps them.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the attention service; a headless scene
  for the cards.
- **What would fail.** Ordering by level then age; shelving bounded by
  expiry and deadline; escalation after the interval; flood refusal per
  stream and per person, surviving a turn; a question card refusing a form
  beyond the quick-reply bounds; the first answer winning between card and
  window. In the scene: two chats asking, Next opening the critical item
  first, a quick reply landing as the tool result.
- **Gap.** That no app window can cover the control or draw on the card
  layer is a property of egui's layering and the window host's constraints;
  a scene can drag a window over the panel and assert the control stays
  clickable, but not prove every path.

## Status

Accepted 2026-10-12. ADR-0269 and ADR-0265 gain dated updates pointing
here.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## Updates

### 2026-10-12 — F8, next by weight

F8 opens the head of the queue — highest level, then oldest — from any
window, as the list's **Next** does; with nothing waiting it raises the
window of the newest highlight (ADR-0323 §SD7) and drops the highlight.
After irssi's Alt-A in the background note (§2.2). It is a host fetcher of
its own, as F1 and F2 are, consumed so no app sees the press. F8 because no
browser binds it bare: the viewer page of the headless host and the
browser tab passes F5, F11 and F12 to the browser and suppresses the rest,
and F1, F3, F6, F7 and F10 have browser meanings a page cannot always
override. A chat window's own unseen level is not among what F8 reaches:
host chrome does not read it.

### 2026-10-12 — a whole form in the card

SD5's bound widens from one single-choice question to a form: up to four
questions, as `ask_user` allows, each a single or a multiple choice among
two to six options. The card shows every question, radio buttons for a
single choice and check boxes for a multiple; Answer is enabled once every
question has a choice, and the host checks the choices against the form
before it settles the question. A form the host had to cut to its bounds,
or one that needs the person's own words, still offers only Open. What the
card is otherwise — host-written heading, the model's text drawn as the
model's, Answer, Skip and Open only — is unchanged.

### 2026-10-13 — the queue is a relation

`keelson('attention_queue')` serves the queue: one row per item its
sources report, open and shelved, with its kind, level, bound, the
person's acknowledgement and shelf, and whether it escalated or nears its
bound. It reads `Queue.Pending`, which classifies and orders the items as
`Snapshot` does but keeps no state and logs nothing to the away log —
`Snapshot` is the chrome's per-frame read and its bookkeeping stays the
chrome's. A shelf that has come due reads as back. On a host without the
agent service the queue is not built and the table is empty.

## References

- [Juggling — one operator, many streams](../adr-background-work/juggling-many-agent-streams.md)
  — precedents, options and the open questions this ADR answers.
- [ADR-0323](./0323-attention-levels-one-table-two-scales-configurable-within-a-floor.md)
  — the levels.
- [ADR-0265](./0265-chat-app-over-retained-model-calls.md) — §SD7 questions,
  §SD8 turn machine, update of 2026-10-08.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
  — §SD5 effects and host chrome, §SD6 grants; updates of 2026-10-04
  (modals) and 2026-10-08 (request expiry).
- [ADR-0298](./0298-an-unattended-mode-the-host-decides-within-preset-bounds.md)
  (proposed) — unattended mode.
- [ADR-0300](./0300-metered-model-calls-and-rules-a-moderator-writes.md)
  (proposed) — §SD9 the moderator's question.
- [ADR-0312](./0312-exits-sinks-from-the-cell-boundary-approval-tiers-and-exit-budgets.md)
  — exit approvals.
- EEMUA Publication 191, *Alarm Systems — A Guide to Design, Management and
  Procurement* — the alarm-flood figure.
