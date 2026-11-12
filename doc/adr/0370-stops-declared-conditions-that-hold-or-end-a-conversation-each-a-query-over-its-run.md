---
type: adr
status: proposed
date: 2026-11-12
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0370: Stops — declared conditions that hold or end a conversation, each a query over its run

## Context

A chat turn ends in four ways: the model answers, its rounds run out, the
person presses Stop, or a moderator stops its task
([ADR-0302](./0302-a-built-in-moderator-loop-signals-and-an-escalation-ladder.md)).
None of these is a condition someone declared about *this* conversation: a
time box, a size the report may reach, a phrase that marks the work as done, a
tool call that should not be made without a look.

Three things shape where such a condition can live.

- **The moderator cannot read the words.** Its events carry no model words by
  design (ADR-0302 §SD2), which is what kept "a moderator per chatlet"
  deferred in [ADR-0364](./0364-chatlets-the-chat-specialised-by-a-committed-markdown-document.md).
  The coordinator already holds every body of its own run
  ([ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md)).
- **The turn loop has a place for it, and a rule.** Turn stopping is a guard
  combined by maximum ([ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md)
  §SD1–§SD2), and a harness-side guard may only add refusals (§SD3).
- **A chatlet may only narrow** (ADR-0364 §SD2). A condition that can hold or
  end a conversation and can do nothing else fits that rule.

A survey of what other harnesses offer is in
[agent-stop-criteria-survey](../adr-background-work/agent-stop-criteria-survey.md)
(2026-11-12). Budgets on turns, tokens and time are common; a declared
criterion is rare, a criterion over a work product was not found, and no
harness read exposes a query over its own event log. Three of its lessons are
taken here: a criterion that was acknowledged must count from the
acknowledgement, working time is not wall clock, and the point of evaluation
sets the overshoot.

## Decision

A conversation may carry **stops**. A stop is a named query over the
conversation's run, with an action. It is evaluated in the host's local SQL
worker at fixed points of the turn loop. When it returns rows the person has
not acknowledged, its action is taken: the turn is held, the turn is ended,
the conversation is ended, or the window is closed. A stop grants nothing and
is never shown to the model.

### SD1 — A stop is a query; its rows are its evidence

A stop is one `SELECT` over four relations the coordinator hands the worker:

| Relation | Holds |
| --- | --- |
| `journal` | the run's entries, in the columns of `keelson('agent_run_entries')` |
| `toolcalls` | one row per tool call a reply asked for, whether or not it ran: the reply's ordinal and turn, the position, the name, the arguments |
| `artefact` | one row: the artefact's text, its size in bytes and lines, its revision |
| `clock` | one row: the turn, its round, the turn's working time, and the wall clock, all as data |

No rows means the stop is quiet. Any row means it has something to say, and
the rows are what the person is shown as the reason. The statement is the
definition: there is no second implementation of a stop in Go, and a stop
that is in force can be read, and run by hand over a past run, as the SQL it
is.

The clock is a relation and not a function so that a statement's rows depend
only on its inputs. Working time leaves out the time a turn waited on the
person.

### SD2 — Named kinds are spellings of statements

Six kinds, each compiled to a statement at construction; `sql` is the
statement itself.

| Kind | Fires on |
| --- | --- |
| `after` | each period of the turn's working time that has passed |
| `artefact_bytes` | each multiple of the size the artefact has reached |
| `artefact_match` | each match of a pattern in the artefact's text |
| `journal_match` | each entry whose body matches a pattern; by default the model's replies and tool results, never the person's own messages |
| `tool_match` | each tool call whose name matches a pattern and, when given, whose arguments match another |
| `sql` | whatever the statement returns |

Patterns are the engine's (RE2), since the engine evaluates them.

### SD3 — A stop fires on rows nobody acknowledged

A stop that fired and was answered with "go on" would fire again at the next
check, were its rows the same. So the rule is about rows: **a stop fires when
it returns a row that is not among the rows the person has acknowledged.**
The named kinds are written to return rows that change when there is
something new: a period count, a size step, the n-th match, an entry's
ordinal. A stop whose rows belong to one turn carries the turn among them.

An acknowledgement is one of two acts, both in the journal: the person
answering a held turn with Continue, or the person sending a message, which
acknowledges every firing before it — they saw what stopped or ended the
turn and spoke again. The acknowledged rows are a fold of the journal, so a
resumed conversation acknowledges what the original did.

A stop may return at most 256 rows. Its rows are its identity, so more than
a check carries is an error and not a truncation.

A statement whose rows differ on every evaluation fires on every check.
Construction refuses the wall-clock and random functions it knows of; it does
not claim to know all of them.

### SD4 — Five actions, combined by maximum

| Action | What it does |
| --- | --- |
| `pause` | holds the turn. A form in the transcript says why and offers Continue and Stop the turn. Nothing is asked elsewhere |
| `ask` | holds the turn and asks the person in the host's queue as well ([ADR-0324](./0324-the-persons-decisions-wait-in-a-host-queue-not-in-modals.md)) |
| `stop` | ends the turn as stopped, with the stop's name and rows as the reason. The conversation stays open |
| `end` | ends the turn and closes the conversation's run. The window stays and shows the record |
| `close` | ends the conversation and closes its window |

When several stops fire at one check the strongest action is taken, and the
record names every stop that fired. A held turn keeps what it has done:
Continue goes on from where it was, and declining, or skipping the form,
ends the turn as stopped. A stopped turn is dropped from what the model is
sent next, as any turn without an answer is
([ADR-0265](./0265-chat-app-over-retained-model-calls.md) §SD3), and a
completion does not start another turn until the person speaks, as after
their own Stop
([ADR-0358](./0358-asynchronous-operations-a-pending-outcome-and-a-turn-a-completion-wakes.md)
§SD5).

A stop that cannot be evaluated — the worker is not there, the statement
fails, the check times out — acts as `stop` with the error as its reason. A
stop that cannot be checked does not read as no stop.

### SD5 — Checked before each request and after each reply

Before a request, a stop sees the results of the round before it and what the
artefact has become. After a reply, it sees the reply and the tool calls it
asks for, before any of them runs. Nothing is checked while a model call or a
tool call is in flight, so a time box is overshot by at most one of them.

A reply without tool calls is the turn's answer. After it there is nothing
left to hold or to stop, so only `end` and `close` act there; a weaker stop
that fires is recorded as noted.

The checks sit beside the turn-stopping guard and are not an element of it.
A guard is a function of a reply; a check calls the worker and may wait on
the person. And a seam's element names are in the run's fingerprint
(ADR-0310 §SD7), so a new element would leave every open conversation
read-only on resume.

### SD6 — Who declares a stop

- A **chatlet** declares stops in its frontmatter (`stops`). They are in
  force in every conversation of that window and the person cannot remove or
  weaken them. A document whose stop does not construct is refused, as one
  with an unknown key is.
- The **person** adds stops of their own in the window, to a plain chat or
  beside a chatlet's. They are kept for the window across its conversations,
  as its Settings are, and are in force from the next turn.
  `BOXER_CHAT_STOPS` seeds a new window with some, for scenes.
- The **model** cannot declare, change or read one.

The stops in force are written to the journal when they change, so the record
says which stops a turn ran under. They are not part of the run's
fingerprint: the model does not read them, and changing a threshold does not
strand a conversation (the rule ADR-0364 §SD6 applies to greetings).

### SD7 — What a statement may be

One `SELECT`, classed as a plain read by the security classifier of
[ADR-0132](./0132-sqlapplet-sql-defined-applets.md) §SD5, naming no table but
the four relations and the names its own `WITH` defines, and carrying no
`SETTINGS` or `FORMAT` clause. The worker is an engine that can read files and fetch
URLs; the classifier is what keeps a stop from doing either. The number of
stops, a statement's length, the rows kept as evidence and the time a check
may take are bounded.

### SD8 — A Stops tab

The window gains a **Stops** tab: each stop in force with its action, where
it came from, its statement, and when it last fired with the rows. The
person's own stops are added and removed there. Settings does not grow.

Outside the Simple view the statement of a stop of the person's own is drawn
in the SQL editor play uses, and is edited there. An edit is a draft: Apply
takes it, a statement SD7 does not admit cannot be applied, and the tab says
why as it is typed. An edited stop keeps its name and action and is from then
on declared by its statement, whatever kind spelled it before. A chatlet's
statement is coloured and read-only (SD6). The named kinds spell their
statements over several lines for that reason. The Simple view keeps the
statement as folded text.

### SD9 — A window may ask its host to close it

`app.WindowCloseI` is an optional capability of the frame context, in the
shape of `app.OperationsGestureI`: the app asks, the host closes the window
at the end of the frame along its ordinary path, with the app's reason on the
lifecycle row, marked as the app's own. A host that has no window to close does not provide it, and
`close` then acts as `end`. So it does in an embedded pane
([ADR-0368](./0368-an-embedded-chat-a-chatlet-drawn-in-another-apps-window-under-the-hosts-governance.md)
(proposed)), whose window is its host's.

It is not a capability subject. An app that closes its own window reaches
nothing that is not its own, and the person can open it again.

### SD10 — Not a security control

A pattern over tool calls is a convenience for a person who wants a look
before something happens. A model that means to evade a pattern can. What a
model may do is decided by the grant and the ceiling
([ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md),
[ADR-0280](./0280-a-ceiling-on-what-a-chats-model-may-do-scored-on-a-ladder.md)),
which a stop neither replaces nor reads.

### Milestones

- **M1 — The host seam.** ✓ `app.WindowCloseI` and the window host's side of it.
- **M2 — The engine.** ✓ Stops, their kinds, construction and its refusals, the
  relations, evaluation in the worker, acknowledgement as a fold.
- **M3 — The loop.** ✓ The checks, the five actions, the journal entries.
- **M4 — Chatlets.** ✓ The `stops` key.
- **M5 — The Stops tab.** ✓ With the person's own stops.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported API under `public/` | added `app.WindowCloseI`; added `agentrun.EntriesArrow`, the run's entries in the introspection table's columns | the window host installs the close; hosts without windows owe nothing |
| Capability subjects | the chat's manifest gains a publish on one local-worker pool (`chat.StopsCaps`); `chat.EmbedCaps` gains the same | an embedding host's manifest (ADR-0368 §SD2) |
| Chatlet frontmatter | added `stops` | the chatlet parser's known keys; `chat.Preset.Stops` |
| Environment variables | added `BOXER_CHAT_STOPS` | [doc/env-vars.md](../env-vars.md) regenerated |
| Journal | two extension-private entry names: `stops` (what is in force) and `stop` (a firing, a noted firing, the person's answer to a held turn) | resume, which reads them; the projection skips them as it skips every extension-private entry |
| Dock | a fifth tab in the chat's column | the window's default width and the column's share of it; a scene that drags the column's divider by coordinates |

## Alternatives

- **In the moderator.** It is host-side and sees events without words, so it
  could carry time and counts and none of the content kinds. Giving it bodies
  reverses a decision ADR-0302 took on purpose.
- **Kinds evaluated in Go, with `sql` as the escape hatch.** Two semantics for
  one feature, and the named kinds could then not be read or rerun as
  statements. The projection keeps a Go fold beside its SQL because every
  model call pays for it; a check sits beside a model call that costs seconds.
- **Kinds evaluated in Go, held to a SQL twin by test**, as the projection is.
  Twice the code to keep the worker off the path. Not taken before the
  worker's cost on this path is known.
- **Hooks: a program the harness runs at each point.** The most general
  shape and the one several harnesses chose. A program is not reviewable in a
  chatlet document the way a statement over four relations is, and it would
  need a sandbox this repository does not have.
- **A model that judges whether to go on.** A second model call per check,
  under a grant of its own, with its own failure modes. Deferred.
- **Acknowledgement as a watermark** — the stop sees only entries after it.
  Works for the journal and for nothing else: a size, a time and an
  artefact's text have no ordinal. Rows work for all of them.
- **A capability subject for closing one's own window.** A round trip and a
  manifest line for an act that reaches nothing outside the app.
- **Folding the rounds limit into a stop.** It is one in spirit. It is also a
  constant of the profile that the last-round note and `request_access`
  depend on. Left as it is.

## Consequences

### Positive

- A scenario can say when it is over, and a person can box a turn in time or
  size, without a change to the loop.
- Why a conversation stopped is a statement and its rows, on the record.
- A stop can be tried against a past run before it is put in force.

### Negative

- Every check is a round trip to the worker carrying the run's entries. Its
  cost grows with the run and was not measured when this was written.
- The chat needs the local worker where it did not; a host without one
  cannot run a conversation that has stops, and says so.
- A regex is easy to get subtly wrong, and a stop that fires on the wrong
  thing ends work. `pause` and `ask` are the forgiving actions.
- A window that closes itself takes its transcript from view. The roster
  keeps the conversation; a notice says which stop closed it only where the
  person watches the conversation, since notices are the levels' to admit
  ([ADR-0323](./0323-attention-levels-one-table-two-scales-configurable-within-a-floor.md)).
- A hold the person continued leaves no line in the transcript. The Stops tab
  and the journal have it.

### Neutral

- A conversation without stops makes no worker call and writes no new entry.
- The moderator is unchanged and may stop a task a stop would only have
  paused.

## Migration — Tier 1

- **Breaks.** Nothing: an added interface, a key, a tab, entries the
  projection skips.
- **Path.** None.
- **Regeneration.** None.
- **Old shape.** Kept.

## Verification plan — Tier 1

- **Lane.** Default `go test`: construction and each refusal of SD7; each
  kind's statement; the fold of acknowledgements; the loop's five actions
  against a scripted model and a scripted worker; the window host's close;
  the chatlet key. The statements themselves, and the help page's examples,
  run against the engine in tests that skip where no engine is installed, as
  the projection's do. The scene `chat-stops` holds a turn on a stop and
  continues it, with the layout of the Stops tab checked.
- **What would fail.** A stop that fires twice on acknowledged rows; an
  unevaluable stop that lets a turn go on; a statement that names a fifth
  table and constructs; a chatlet stop the person can remove.
- **Gap.** Whether the named kinds are the useful ones is not a test. The
  worker's cost per check is to be measured, not asserted. `end` and `close`
  are covered at the window's methods, not by a scene: no scene watches a
  window close itself.

## Deferred

- **A pause that ends by itself** after a time. Trigger: a scenario that
  wants a cool-off and not a person.
- **A model-judged stop.**
- **Checks while a call is in flight**, to cut a model call or a tool call
  short. Trigger: an overshoot someone minds.
- **Shipping the worker only what a statement reads.** Trigger: the measured
  cost.
- **Telling the model** that a turn is boxed, as it is told its rounds.
- **Budgets in tokens or cost.** The `journal` relation carries no token
  counts, and the ration of
  [ADR-0300](./0300-metered-model-calls-and-rules-a-moderator-writes.md)
  (proposed) is where such a budget belongs.
- **A line in the transcript for a hold that was continued.**

## Status

Proposed — awaiting review by the code owner. M1–M5 are built against this
text, and the verification plan's lanes ran on 2026-11-12; the worker's cost
per check was not measured.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [agent-stop-criteria-survey](../adr-background-work/agent-stop-criteria-survey.md) — what other harnesses offer.
- [ADR-0265](./0265-chat-app-over-retained-model-calls.md), [ADR-0364](./0364-chatlets-the-chat-specialised-by-a-committed-markdown-document.md) — the chat and the chatlet.
- [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md), [ADR-0309](./0309-context-projection-as-sql-over-the-journal-in-a-local-worker.md) — the journal, and SQL over it in a local worker.
- [ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md) — the turn-stopping guard.
- [ADR-0302](./0302-a-built-in-moderator-loop-signals-and-an-escalation-ladder.md) — the moderator.
- [ADR-0324](./0324-the-persons-decisions-wait-in-a-host-queue-not-in-modals.md) — the queue a held turn asks in.
- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md) §SD5 — the security class of a statement.
