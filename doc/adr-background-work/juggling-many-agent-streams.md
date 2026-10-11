---
type: explanation
audience: designer of the chat app (ADR-0265), of the agent host chrome (ADR-0269) and of the moderator (ADR-0300, ADR-0302)
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-10-11 to feed an ADR
> that replaces the modal decision dialogs of
> [ADR-0269](../adr/0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
> with a queue, and a roster for the chat app of
> [ADR-0265](../adr/0265-chat-app-over-retained-model-calls.md); nothing here
> is a decision. Every claim about another product is `[gk]`: from general
> knowledge of clients in use between roughly 1996 and 2015, not re-read on
> the compile date, and to be checked before anything is built on it; the
> one exception, the EEMUA flood figure, was checked against public secondary
> sources on the compile date. No third-party source code was read. Claims
> about boxer are dated observations of the tree on the compile date.

# Juggling — one operator, many streams

The question: when one person runs several model conversations at once, each
of which may at any moment need them — an access request, a question, a
confirmation — how should the host and the chat app present the streams, so
that the person neither misses what waits on them nor is pulled away by what
does not?

The viewpoint is the operator's, not the conversation's. A chat window
answers "what is this conversation doing"; the operator needs three other
answers, across all of them: **what needs me, what is moving, what is
stuck**. Instant messaging and IRC clients faced the same shape — many
streams, one reader, attention as the scarce resource — long before agents,
and industrial alarm management faced it with higher stakes. Their answers
converged on a small set of mechanisms (§3).

Caveats first. The precedents are humans talking to humans, or plants
talking to operators; an agent differs in two ways that matter. It does not
get bored and stop asking, so a client's social brakes on interruption are
absent and a flood limit has to be mechanical (§3.9). And a call that waits
on the person holds its turn — `ask_user` by ADR-0265 §SD7, grant requests,
widenings and confirmations since ADR-0265's update of 2026-10-08 — until it
is decided or the request expires (`BOXER_AGENT_REQUEST_TIMEOUT`, 30 minutes
by default, ADR-0269 update of 2026-10-08). An item missed is not merely
unread: the conversation stalls for that long. The
precedents are selected for transferable mechanism, not surveyed
exhaustively; a client not named here is not judged.

## 1 The operator's problem

A **stream** is anything that produces events over time and may need the
person: a chat conversation and the task it holds, a watchbill job, a
moderator. An **event** is one thing a stream did. The person can attend to
one stream at a time; everything else they know about the others comes from
what the surface shows without being asked.

Two costs pull against each other:

- **Interruption.** Showing an event in the person's way costs their
  attention to whatever they were doing. A modal costs the most: it stops
  all work until answered.
- **Latency.** An event that waits on the person and is not seen costs the
  stream's progress. Under the waiting rule above, it costs the stream all
  progress until seen or until the request expires, and expiry is itself a
  failure the model has to work around.

Every mechanism below trades one against the other, and most of them do it
by **grading** events, so that only the few that warrant interruption get
it, and by giving the person a cheap way to see the rest when they choose.

## 2 Precedents

### 2.1 ICQ (Mirabilis, 1996 to about 2001) `[gk]`

- **The contact list was the application.** A narrow window of people,
  grouped online then offline, each with a status glyph. Conversations were
  short-lived windows opened from it. The person lived in the list.
- **Status shaped the sender's act.** Online, Free for Chat, Away, N/A,
  Occupied, DND, Invisible. Away and N/A were set by idle time. Sending to an
  Occupied contact made the *sender* choose: send as urgent, or to the
  contact list to be read later (whether DND behaved the same or simply
  refused is not confirmed). Each status carried an away message the
  sender read only on request.
- **Messages waited.** An incoming message did not take focus: the contact
  and the tray icon blinked, a sound played, and a double-click on the tray
  opened the oldest pending event; repeated, it walked the queue.
- **Delivery did not need the recipient.** The server held messages for an
  offline contact until they connected.
- **Consent before presence.** Adding a contact could need their
  authorization; until given, the contact sat in a group of its own. Visible,
  invisible and ignore lists set who saw your status. Messages from people
  not on the list arrived under a separate group.
- **Two speeds.** "Message" was a turn-based exchange; "Chat" a separate
  split-pane window in which each side's text appeared as it was typed.
- **Failures.** Numeric identities nobody could remember; a sound per event
  type, unbearable at ten active contacts; a Simple/Advanced mode switch,
  which conceded that the interface had lost its centre; presence leaked
  (early clients exposed addresses, invisibility could be detected); open
  contact by default invited spam.

### 2.2 IRC clients — irssi, WeeChat, mIRC `[gk]`

- **Activity levels.** Each window carries the highest level reached since
  it was last seen: *data* (joins, parts, mode changes), *message* (someone
  spoke), *highlight* (someone said your nick or a watched word). The window
  list colours each window by level.
- **The hotlist.** irssi's `Act: 2,5,7` in the status bar, and a key (Alt-A
  in irssi and WeeChat) that jumps to the active window of **highest** level,
  not the oldest.
- **A read marker** in each window, at the point last seen: built into
  WeeChat and mIRC, a script (`trackbar`) in irssi.
- **A status window** for server notices, so channels carry conversation
  only. WeeChat's smart filter hides joins and parts by people who have not
  spoken lately.
- **Away with a reason, and an away log**: highlights received while away are
  collected and read on return as one list.

### 2.3 KVIrc `[gk]`

The same model as §2.2, with the parts that concern many streams on one
screen worked further:

- **A tree window list grouped by context**: each server connection, its
  console, and under it its channels and queries.
- **Per-message-type icon, colour and level.** Every output line starts with
  an icon for its type; the type's level feeds the window list's activity
  colouring, from one configuration.
- **An activity indicator** in the tree list showing how busy a window is,
  not only whether it has unread lines. (Exact presentation not re-checked.)
- **A notifier** popup carrying highlighted messages, with an input line to
  answer from it without switching windows.
- **Nick colour by hash**: a stable colour per nick wherever it appears.
- **A registered-users database and a notify list**: per-user properties the
  person keeps, and an alert when a watched user connects.
- **Undockable windows** that leave the main frame and return.
- **Failures.** KVS, its scripting language, with editors for events,
  aliases, popups, toolbars and actions, made the client almost entirely
  reshapeable, and the options dialog grew with it; the defaults are what
  most people meet. The MDI frame put window management inside the client,
  and the tree list is what made it navigable.

### 2.4 Others `[gk]`

- **XMPP.** A person signed in from several resources (devices) gives each a
  priority; a message to the bare address goes to the highest. Chat states
  (XEP-0085: active, composing, paused, inactive, gone) are a small declared
  state machine exchanged on the wire.
- **Slack's Do Not Disturb.** A sender who messages someone in DND sees it and
  may choose *notify anyway* — ICQ's urgent send, with the choice visible.
- **tmux.** Per-window `monitor-activity` and `monitor-silence`: an alert when
  a window produces output, or when it has produced none for N seconds.
- **Alarm management (ANSI/ISA-18.2, EEMUA 191).** Control-room practice
  separates *acknowledging* an alarm (seen) from its *clearing* (resolved);
  allows *shelving* an alarm for a bounded time, recorded; prioritises
  alarms into a few levels with a target distribution; and treats an *alarm
  flood* — EEMUA's figure is more than ten alarms in ten minutes per
  operator — as a design failure to be fixed at the source, not an operator
  failure.
- **Adium.** An event→action table the person edits: for each event, a
  sound, a bounce, a notification, or nothing.

## 3 Recurring mechanisms

Each mechanism with the precedents that show it and what it answers among
*needs me / moving / stuck*.

| # | Mechanism | Shown by | Answers |
|---|---|---|---|
| 3.1 | A roster: the streams as the primary view, grouped by context | ICQ, KVIrc tree | all three, at a glance |
| 3.2 | Graded levels, one table from event type to level | IRC, KVIrc, ISA-18.2 | needs me |
| 3.3 | Ordering by level, then age; one "next" action | irssi Alt-A, ICQ tray | needs me |
| 3.4 | Rate, not only unread; silence as a signal | KVIrc indicator, tmux | moving / stuck |
| 3.5 | Read marker | IRC | what changed |
| 3.6 | Operator status, set by hand or by idle, that changes what interrupts | ICQ, Slack, XMPP priority | — (gates the rest) |
| 3.7 | Break-through as a visible, recorded choice of the sender | ICQ urgent, Slack notify-anyway | needs me, now |
| 3.8 | Acknowledge ≠ resolve; shelve with an end time | ISA-18.2 | needs me, later |
| 3.9 | A flood limit that pushes back on the source | ISA-18.2 / EEMUA | — (protects the rest) |
| 3.10 | Answer in place, for small answers | KVIrc notifier | needs me, cheaply |
| 3.11 | A pull-only status line per stream | ICQ away message | moving |
| 3.12 | Stable identity colour across surfaces | KVIrc nick colour | which stream is this |
| 3.13 | Delivery independent of presence; a summary on return | ICQ offline messages, IRC away log | what happened while away |
| 3.14 | Unknown or untrusted sources in a group of their own | ICQ "not in list" | — (trust) |

Two observations across the table. First, the mechanisms are cheap
individually and only work together: levels without ordering is a coloured
list; ordering without acknowledge forces a decision to clear the view;
status without a flood limit moves the flood to the moment of return.
Second, none of the precedents made interruption modal by default. Modality
appears only for the top level, and in alarm practice even that is
acknowledged, not answered.

## 4 Where boxer stands (observed 2026-10-11)

- **Decisions are modal.**
  [`Chrome.RenderDialogs`](../../public/keelson/runtime/agent/chrome.go)
  draws a moderator's question alone when one is open (`Service.AskPerson`,
  the `llm.ration.ask` of ADR-0300 (proposed) §SD9, which any moderator may
  use);
  otherwise the oldest consequential confirmation and the oldest pending
  grant request or widening, each as a modal over every window, both in the
  same frame when both are pending. ADR-0269's update of 2026-10-04 made
  them modal for a stated reason: as windows, an app window could cover
  them, and a turn waited on a dialog nobody saw. The modal carries a second
  property the code states: a grant is decided before anything else is
  clicked, so the windows a request lists cannot change while it is open.
  The cost is the other half: the backdrop dims the host and takes the
  pointer, so the person cannot inspect or operate the window a request is
  about before deciding.
- **Host chrome already draws model text.** The task menu, the request
  dialog and the confirmation show the model's plan and reason, labelled
  "as the model wrote it" and bounded in height (ADR-0269 §SD5: "the
  model's reason last, as its claim"). What host chrome does not do is
  offer choices the model wrote.
- **Questions are in the window.** `ask_user` (ADR-0265 §SD7) and the exit
  approvals of [`chat_approval.go`](../../apps/chat/chat_approval.go) are
  drawn in the chat window's pending bubble. A chat window behind others
  shows nothing outside itself, and the call waits inside the turn.
- **Waiting is bounded.** An undecided grant request or widening expires
  after `BOXER_AGENT_REQUEST_TIMEOUT` (`DefaultRequestTimeout`, 30 minutes)
  and the model reads `expired`; the task's deadline and call budget run
  meanwhile (ADR-0269 §SD6).
- **The turn is a declared machine** (ADR-0265 §SD8), mirrored per window as
  a chip and readable through the app's operations catalog. "Your turn" is
  one of its states. Nothing gathers the states of several windows.
- **Past conversations are a list, not a roster.**
  [`renderRunsPanel`](../../apps/chat/chat_resume.go) lists journal runs with
  state, label, creation time, entry count and id. The title is not a field
  of the run; the model's title call is journaled as an `intent` named
  `title`, so a resume can recover it from the entries. The list does not
  say which runs are live in another window.
- **Events are already typed.** The run journal's entry kinds — those
  ADR-0308 §SD2 names, a closed enum with a golden (`KindE` in
  [`kinds.go`](../../public/keelson/runtime/agentrun/kinds.go)) — and the
  operation effects (ADR-0269 §SD5: none, view, document, run,
  consequential) are the raw material of a level table (§3.2).
- **The host already marks windows a task works in** with a badge in host
  chrome, which is the reverse edge of the roster's tree (§3.1).
- **What runs without the person is decided elsewhere.** Unattended mode
  (ADR-0298, proposed) decides within preset bounds; window-less runs are
  deferred (ADR-0308 §SD11, carrying ADR-0303 §SD9 forward). The built-in
  moderator (ADR-0302, proposed) turns live events into
  signals and tells the person rather than asking them (§SD4).

## 5 Options

Ordered roughly by independence. "Chat" means a change confined to
`apps/chat`; "host" a change to host chrome and an ADR.

### 5.1 Roster in the chat app — chat

The Conversations panel becomes a tree: each conversation with the windows
its task works in as children; groups *in windows*, *resumable — no
window* (journal state open, no live instance), *closed*, *stranded*. Each
line carries its level glyph (§5.2), a short rate gauge of steps per minute
(§3.4), a pull-only status line from the running step (§3.11), and a colour
derived from the conversation id (§3.12), never colour alone.

Needs: the title on the run row, or recovered from the title call's
entries (§4); the live instances and their turn states, read through the
operations catalog or the window tables the chat already queries (ADR-0276)
rather than in-process globals. No host change.

### 5.2 Levels: two scales, configurable within a floor — chat, then host

One table maps an event to a level, and the table drives the transcript's
per-line icon, the roster's level, which trail steps fold (§2.2 smart
filter), the queue's order (§5.3) and what passes Busy (§5.5). Having it in
one place is the point (§2.3).

The levels form two scales, and an event's scale is fixed by what the event
is, never by configuration:

| Scale | Levels | Holds |
|---|---|---|
| attention | data · message · highlight | events nothing waits on |
| waiting | your turn · critical | events a turn is blocked on: `asked` in every form (question, grant, widening, confirmation, exit approval) |

Within its scale an event's level is configurable; across scales it is not.
That is the floor: nothing that blocks a turn can be configured below *your
turn*, so a setting cannot hide a stalled conversation, and nothing that
blocks no turn can be raised into the queue, where it would be an item with
nothing to decide.

**Scopes, the most specific winning:**

1. The shipped default, in code.
2. The person's table, held by the host, so the chat and the queue read the
   same one.
3. Per conversation. "Tell me when this turn ends" (§2.1 online alert, §2.3
   notify list) is raising a finished `turn` to *highlight* for one
   conversation, and an urgency granted to a conversation (§5.5) is raising
   its waiting items to *critical*. Neither needs a mechanism of its own.

**Keys** are entry kind × operation effect, optionally narrowed by app id: a
*document* effect may be *data* in an editor and *message* in play. Entry
kinds the table does not list are *data*.

**Editing** is a grid in Settings — events as rows, levels as columns —
which is Adium's degree of control, not KVIrc's scripting (§6). A change to
the waiting scale is recorded on the trail, since it decides what passes
Busy.

A first sketch of the defaults, for argument rather than adoption:

| Event | Scale | Default |
|---|---|---|
| `request`, `reply`, `answered`, a succeeded `outcome`, an `intent` of effect none or view, `user` | attention | data |
| an `intent` of effect document or run; a refused `outcome`; `note`; a `turn` that answered, failed, stopped or was cancelled | attention | message |
| — (reached only by overrides) | attention | highlight |
| `asked`: a question, a grant request, a widening, an exit approval | waiting | your turn |
| `asked`: a confirmation of a consequential command | waiting | critical |

Precedence: an event is classified by its entry first and its effect
second, so a consequential confirmation is one `asked` row, not also an
`intent` row.

Not proposed: words in the model's text that raise a level (IRC's highlight
words), and moderator rules that set levels. Both could come later without
changing the shape.

A per-conversation **note** — the person's annotation, kept in app storage
rather than the run — stays a separate small feature.

### 5.3 A queue in host chrome, replacing the modals — host, amends ADR-0269

A "your turn · N" control in the host's top panel, opening a list ordered by
level then age, with **Next**. Grant requests, widenings and confirmations
open as non-modal host cards; an `ask_user` item is answered in its card
(§5.4) or raises and focuses its chat window. Per item: **acknowledge**
(seen, not decided) and **shelve until** a time (§3.8).

What host chrome guarantees is unchanged: an agent can neither draw nor
click it. What changes is that the person may act elsewhere while an item
is open. Chat publishes "waiting on the person" items to the host; most of
that is the turn machine's *your turn* state with a key and a summary.

The modal carried three properties, and the ADR has to say how each
survives:

- **Not hidden.** A count no app window can cover replaces a dialog no app
  window can cover. But a count can be seen and ignored as easily as a
  window was covered; the 2026-10-04 defect was a turn waiting on a dialog
  nobody saw. Whether an unacknowledged item escalates with age, and how, is
  open.
- **Decided before anything else is clicked.** A non-modal card lets the
  person share or close windows while a request about them is open, and the
  request's window list is rebuilt each frame. The card has to fix what it
  shows at opening, or refuse a decision on a list that changed.
- **Not spoofable.** The control and its cards need a z-order the host
  guarantees above every app window; otherwise an app can draw a look-alike
  card beside a real one.

Critical items may still open at once (§7, question 2).

### 5.4 Quick reply in host chrome — host, amends ADR-0269 and ADR-0265 §SD7

The queue's card answers a small `ask_user` in place: one question, single
choice, two to six options (§3.10). Anything larger opens the chat window.
The answer travels to the chat over the bus and lands as the tool call's
result, as an answer given in the window does.

Host chrome already draws the model's text, labelled and bounded (§4). The
widening is that the model's text becomes **actionable** there: the model
writes the option labels the person clicks, so a question can be phrased to
look like a host decision ("Approve the widening?"). The ADR has to bound
that explicitly: the card is visibly distinct from a grant card, it carries
the conversation's taint line as the request dialog does, its options are
length-bounded and drawn as the model's, and it offers no action but
Answer, Skip and Open. It also amends ADR-0265 §SD7, whose `ask_user`
"reaches nothing outside the chat window".

### 5.5 Operator status — host

Here / Busy / Away, Away set after an idle interval.

- **Here:** items open as they arrive.
- **Busy:** items wait in the queue; *critical* items break through. Since
  levels are configurable (§5.2), "urgent" is a per-conversation override to
  *critical*, recorded on the trail — ICQ's urgent send and Slack's notify
  anyway as a visible, granted choice (§3.7).
- **Away:** as Busy. On return, an away log (§2.2) lists what landed, what
  expired and what was assumed.

Whether a model learns that the person is away, and since when, is a grant
term, not a default: the per-turn note (`runtime.agent.turn`, ADR-0265
§SD6) reaches a model endpoint that may be remote, and §6 treats presence
as information about the person. Skip stays the person's act (ADR-0265
§SD7): Away does not let a model skip a question on its own, and never
applies to grants, widenings or confirmations.

Not proposed: Away implying unattended mode. That would make a presence
setting widen what agents may do, which is ADR-0298's (proposed) decision to take.

### 5.6 Flood limit — host and moderator

A rate of new waiting items (EEMUA's ten in ten minutes is a starting
point, not a measurement here). Past it, new requests return to the agent
refused with a reason it can act on, and the moderator receives the signal.
The flood is fixed at the source, as alarm practice asks, and not by the
person clearing faster. Points the ADR settles:

- **Counted per what.** A per-person count lets one noisy conversation get a
  quiet one's single request refused; a per-stream count, alongside the
  per-person one, puts the refusal on the source. ADR-0302 §SD3's signals
  are per window.
- **Across turns.** A new turn starts without the calls refused before it
  (ADR-0265, 2026-10-08); the count has to survive turns or a refused
  request returns next turn.
- **What counts.** Requests unattended mode decides (ADR-0298 §SD2) never
  reach the person and do not count.
- **A refused question.** Grant, widening and confirmation refusals already
  name their remedy (ADR-0269); a refused `ask_user` needs a defined result,
  such as "the person is not taking questions; go on and say what you
  assumed".

## 6 What not to copy

- **A sound or effect per event type** (ICQ): it scales with the number of
  streams. Effects attach to levels, if at all.
- **A scripting layer for notification policy** (KVIrc): the level grid
  (§5.2) is the configuration; rules beyond it belong with the moderator's
  (ADR-0300), which are recorded and audited.
- **A second window manager** (KVIrc MDI): the roster navigates to the host's
  windows and does not contain them.
- **Modality as the default** (boxer's own 2026-10-04 choice, kept for want
  of a queue): the precedents reserve it for the top level.
- **Presence that leaks**: the operator's status is information about the
  person; which streams may read it is a grant like any other (§5.5).

## 7 Open questions for the ADR

1. The default rows of the level table (§5.2), and which waiting items ship
   as *critical*.
2. Whether any item stays modal: moderator questions (ADR-0300 §SD9),
   *critical* items when the person is Here.
3. Shelving against the clocks that keep running: request expiry (ADR-0269,
   2026-10-08), the task's deadline and call budget (ADR-0269 §SD6; a late
   task's widening is the person's even unattended, ADR-0298 §SD3), and the
   turn waiting on the person (ADR-0265 §SD7). And what, if anything, the
   model is told when an item is acknowledged or shelved.
4. How an unacknowledged item escalates with age (§5.3).
5. The bounds of quick reply (§5.4): sizes, and whether multi-select
   qualifies.
6. Where the operator status lives, and the grant term by which a stream
   may read it (§5.5).
7. The flood count's unit and lifetime (§5.6).
8. Whether watchbill jobs and moderator signals join the same queue, or the
   queue is the agent dispatcher's alone at first.

## References

- [ADR-0265](../adr/0265-chat-app-over-retained-model-calls.md) — the chat
  app; §SD6 coordinator and trail, §SD7 questions, §SD8 turn machine, update
  of 2026-10-08.
- [ADR-0269](../adr/0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
  — operations, grants, host chrome; §SD5 effects and modes; update of
  2026-10-04 on modals.
- [ADR-0276](../adr/0276-agents-read-and-arrange-windows.md) — window tables
  the chat queries.
- [ADR-0298](../adr/0298-an-unattended-mode-the-host-decides-within-preset-bounds.md)
  (proposed) — unattended mode.
- [ADR-0300](../adr/0300-metered-model-calls-and-rules-a-moderator-writes.md)
  and [ADR-0302](../adr/0302-a-built-in-moderator-loop-signals-and-an-escalation-ladder.md)
  (both proposed) — moderator rules, the person's question (ADR-0300 §SD9), and the
  built-in loop.
- [ADR-0308](../adr/0308-the-run-journal-is-the-only-source-of-model-input.md)
  — the run journal; §SD2 entry kinds, §SD11 what stays out. It supersedes
  [ADR-0303](../adr/0303-a-conversation-outlives-its-process-a-run-journal-written-before-each-act.md)
  (superseded), the first proposal.
- [llm-chat-app-requirements-survey.md](./llm-chat-app-requirements-survey.md)
  — the single-conversation requirements this page does not repeat.
- ANSI/ISA-18.2, *Management of Alarm Systems for the Process Industries*;
  EEMUA Publication 191, *Alarm Systems — A Guide to Design, Management and
  Procurement*. `[gk]`, not re-read.
- XMPP Standards Foundation, XEP-0085 *Chat State Notifications*. `[gk]`
