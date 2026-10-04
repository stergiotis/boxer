---
type: adr
status: accepted
date: 2026-10-04
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-04
---

# ADR-0280: A ceiling on what a chat's model may do, scored on a ladder

## Context

A chat with Apps on runs its model as a coordinator
([ADR-0265](./0265-chat-app-over-retained-model-calls.md) §SD6): the model
asks for windows, the person decides in the host's dialog, and the dispatcher
checks every call against the resulting grant
([ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) §SD6).
Two things were missing.

- **Nothing bounds what the model may ask for.** Each request is judged on
  its own, when it arrives. A person who wants a conversation in which the
  model only reads has no way to say so once; they decline each widening.
- **Nothing shows how far a chat can go.** The grant is a list — windows,
  modes, operations, launches, destinations. The chat's bar shows a task id
  and two badges. Whether this conversation can run queries against a
  server, or only read a window, is not visible without reading the list.

- **Nothing paces the model.** Its changes land as fast as it makes them,
  which is faster than a person can read. The safeguards that rest on the
  person watching — the pause when they edit, Stop task, a conflict on a
  stale write — assume they can see a change before the next one lands.

The chat's options had also accreted in its bar: Keep and Apps as
checkboxes, operation tools behind an environment variable.

## Design space (QOC)

**Q1 — What is scored?**

- *O1: the settings alone.* A static number; it does not move when a task
  is granted or stopped.
- *O2: the live grant alone.* Shows what is, not what could be; the person
  has nothing to set.
- *O3: both, on one scale.* What the settings allow and what the task
  holds, as two markers. **Chosen** by the owner.

**Q2 — How is a score formed?**

- *O1: a weighted sum of everything allowed.* Fine-grained, and two very
  different permission sets can land on one number; the weights need
  defending.
- *O2: a ladder.* The most powerful thing allowed picks the level; the rest
  moves the position inside the level's band. Every position has a name.
  **Chosen** by the owner.

**Q3 — Where does the limit bind?**

- *O1: in the chat.* The coordinator restrains itself: it clamps what it
  asks for. Contained, and not a boundary — the host would grant more if
  asked.
- *O2: in the host.* The coordinator relays the settings and the dispatcher
  refuses above them. **Chosen** by the owner.

**Q4 — Where do the options live?**

- *O1: a window of their own.* An app may not open windows in this host
  (the window host owns them), so it would be a second app window and a
  channel between the two.
- *O2: a panel beside the transcript.* **Chosen** by the owner.

## Decision

A coordinator's settings are a **ceiling** on what its model may do. The
coordinator relays it; the dispatcher enforces it. The ceiling and the
task's grant are scored on one **ladder** and drawn as two markers on one
scale, in a Settings panel that holds every option of the chat.

### SD1 — The ceiling

A ceiling is one value: the highest mode a window may be shared in, the
highest effect an operation may have (ADR-0269 §SD5), whether the model may
open windows and arrange the desktop
([ADR-0276](./0276-agents-read-and-arrange-windows.md) §SD4), and how far its
work may reach — this host's tables, the data endpoints, the network — and
whether it may work unpaced (SD6). The zero value shares no window: the
model only talks.

It travels with every grant request and is set on a task through a service
of its own, which only the task's coordinator may call. A coordinator that
sends none has none, and its tasks behave as before.

### SD2 — The dispatcher refuses above it

- A **request** above the ceiling — a mode, a launch, the desktop, a
  destination — is refused before the person is asked, with what the
  settings allow as the reason.
- A **call** to an operation whose effect is above the ceiling is refused,
  whatever the grant holds. It is not a widening to ask for.
- A window's mode is its grant's and **no higher than the ceiling's**: under
  a ceiling of suggest, a window granted act takes proposals.
- A **launch**, an **arrangement** and a **destination** are checked the
  same way where each is already checked.
- **Moving the ceiling binds at once.** Calls are checked as they arrive;
  proposals waiting on the person that it no longer allows end.

The ceiling restrains the model, not the coordinator app: the app is what
relays it. That is the same trust the host already places in a registered
coordinator.

### SD3 — The ladder

Six levels, by the most powerful thing allowed:

| Level | What it allows |
| --- | --- |
| talk only | no window is shared |
| read | reading what windows hold and show |
| change the view | a selection, a camera |
| edit | what a window holds: text, parameters |
| run | executing against a data source |
| act outside | publishing, exporting — confirmed by the person each time |

A position on the scale is the level's band plus a place inside it. What
moves the place: changes applying without the person accepting each, opening
windows, arranging the desktop, reaching data endpoints or the network,
working unpaced, and the model being off this machine. No combination of these lifts a level past
the one above — the band is the level's alone — so two positions compare
first by what is the most powerful thing allowed, as a person would.

The dispatcher computes what a task's grant allows *now* in the same shape
as a ceiling — from the windows shared and their modes, the operations their
apps' catalogs offer an agent, the launches left, the desktop mode and the
destinations, each capped by the ceiling — and reports it with the ceiling.
An operation above the ceiling adds nothing to what is granted: it is
refused, not weakened.

### SD4 — The scale, and the widget

The scale is a row of six equal bands with two markers: a hollow one for the
ceiling ("may") and a filled one for the grant ("now"). Each band is named
under the bar and each marker above it, so colour is never the only channel
(ADR-0031 §SD5). The bar carries a compact form with the two levels named
beside it; the Settings panel the full one, with the number and the factors
in words.

It is a widget of its own, `widgets/bandscale`, immediate-mode
([ADR-0267](./0267-imzero2-go-widget-api-contract-immediate-and-semi-retained.md)):
bands with a label and a tone, markers with a position and a label. It knows
nothing of agents.

### SD5 — The Settings panel

A panel beside the transcript, opened from the bar, holds the chat's
options:

- **What the model may do**: the scale, Apps, the highest action, whether
  changes ask first or apply directly, reach, pace, opening windows,
  arranging the desktop, and a tool per operation.
- **Conversation**: Keep.
- **Model**: what the host offers, read-only.

The bar keeps New conversation, the Settings toggle with the compact scale,
Statistics, and what the conversation was started with. Keep and Apps are
still fixed at a conversation's first message; the rest is live.

The defaults let the model do what a conversation with Apps on could do
before there was a ceiling, short of acting outside the app and at a pace
the person can follow.

The model is told what the settings allow on its first turn and whenever
they move, so it does not spend calls finding the limit. The host enforces
the limit either way.

### SD6 — Pace: speed is granted, not assumed

A model's changes are **paced** unless the ceiling says otherwise: the
dispatcher lets a task's next visible change land no sooner than a set time
after its previous one. Working faster than a person can follow is a grant
of its own — orthogonal to the ladder, since speed is not a kind of action —
and the person gives it in the settings; no request asks for it.

- **What is paced** is what the person can see: an operation with an effect,
  a window opened, a window raised, placed or arranged. Reads are not: they
  change nothing on screen.
- **A wait, not a refusal.** The call is held until its turn and then goes
  through, so a model loses time and no calls. The coordinator shows what
  the call is about to do while it waits, which is when the person can stop
  it.
- **Per task, across its windows.** The person watches a desktop, not a
  window.
- **The time** is the host's (`BOXER_AGENT_PACE`), a deployment's judgement
  of what can be followed.
- **In the score** it is one of the factors that move a position inside its
  band, and it reads on the scale in words.

A task whose coordinator sets no ceiling is not paced, as it is not bounded
otherwise: the agent console and a trial's harness keep their speed. Pacing
those too was weighed and left out by the owner (2026-10-04).

### SD7 — Deferred, recorded

- **Persisting the settings.** They live with the window, like the
  conversation (ADR-0265 §SD5).
- **The scale in the host's approval dialog.** The person decides a request
  without seeing where it would put the task on the ladder.
- **A ceiling on budget and time.** The ceiling bounds what, not how much.
- **Pacing what cannot be seen.** A change in a window that is covered or
  minimised is not seen however slowly it lands; pacing does not raise it.
  Reads are unpaced, so how fast a model reads data is unbounded.
- **Pace tied to the frame.** The wait is a time between changes, not until
  the previous one has finished drawing or animating.
- **A selected segment that reads at a glance.** The selector's highlight is
  faint; it has no tone option.
- **Per-app ceilings.** One ceiling covers every window.

## Surfaces — Tier 1

| Surface | Change |
| --- | --- |
| `runtime.agent.request` wire | an optional ceiling |
| `runtime.agent.authority` | new: read a task's ceiling and what its grant allows; set the ceiling |
| dispatcher checks | request, call, launch, window verb and destination refuse above the ceiling; visible changes of a paced task wait their turn |
| `BOXER_AGENT_PACE` | new: the least time between two visible changes of a paced task |
| grant events ([ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) §SD2) | a `ceiling` event when one is set or moved |
| `widgets/bandscale` | new |
| chat bar | Keep and Apps checkboxes leave for the Settings panel; the scale joins |

## Alternatives

- **A ceiling the model sets through a tool.** It would be the model
  limiting itself, which is what the ceiling exists not to rely on.
- **Reusing the radial gauge.** A dial reads one value against thresholds;
  two markers and six named bands are an ordinal comparison, which reads
  along a line.

## Consequences

### Positive

- A person can say once how far a conversation's model may go, and the host
  holds it to that.
- How far a chat can go, and how far it is, read at a glance and in words.
- The options have one place.

### Negative

- One more bound to reason about: a call can be refused by the grant or by
  the ceiling, and the reasons differ.
- A paced conversation is slower by the pace for every change after the
  first, which a turn of many edits feels.
- The score's position inside a band rests on weights that order the
  factors against each other; they are a judgement, not a measurement.
- The granted level is read from the apps' catalogs, so it is as good as
  the effects those declare.

### Neutral

- A coordinator that sends no ceiling behaves as before.

## Migration — Tier 1

One step: the ceiling and its checks in the dispatcher, the widget, and the
chat's panel and bar. ADR-0265 is proposed and is revised in place; ADR-0269
takes a dated Update.

## Verification plan — Tier 1

- The ladder: the level is the most powerful thing allowed; a position stays
  in its band; the least of a level is above the most of the level below.
- The dispatcher: a request above the ceiling is refused before the person;
  a call above it is refused under a grant that covers it; lowering the
  ceiling refuses the next call and lowers what is reported as granted; a
  paced task's second change waits and its reads do not, and the unpaced
  grant lifts the wait.
- The chat: the settings map onto the ceiling; a model asking for more is
  refused and is told the limit once.
- A scene on the headless host: the scale before a task, after a grant,
  after the ceiling is lowered, and a write the host then refuses.

## Status

Accepted 2026-10-04, built. The verification plan holds: the unit tests of
the ladder, the dispatcher and the chat, and the scene
[chat-settings](../../apps/chat/scenes/chat-settings.scene.md) against a
scripted model under a test grant. Not verified: the desktop host, and a real model's use of the note
that tells it the limit.

## Updates

### 2026-10-04 — a local git repository is the host's reach

`ReachOf` took every destination class it did not know as the network, so
the first app to name a new class — Git Pulse's `rescan`, which asks the
grant for `git:<absolute path>` when a task set the repository — was refused
under the default ceiling (data endpoints) whatever the person granted. A
repository on this host's disk is what the host holds, like its
`keelson:<table>` tables, so `git:<path>` is host reach. The rule for a
class not listed is unchanged: the widest.

## References

- [ADR-0265](./0265-chat-app-over-retained-model-calls.md) — the chat app and its coordinator.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) §SD5, §SD6 — effects, modes, the grant and the dispatcher.
- [ADR-0276](./0276-agents-read-and-arrange-windows.md) §SD4 — the desktop mode.
- [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) §SD2 — grant events.
- [ADR-0267](./0267-imzero2-go-widget-api-contract-immediate-and-semi-retained.md) — the widget contract.
- [ADR-0031](./0031-imzero2-design-system-color.md) §SD5 — colour is never the only channel.
