---
type: adr
status: proposed
date: 2026-10-04
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0283: Chat analytics — the agent surface, and what of it was granted and used

## Context

A chat ([ADR-0265](./0265-chat-app-over-retained-model-calls.md)) whose model
works in windows ([ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)) acts under
three nested limits: the ceiling its settings set
([ADR-0280](./0280-a-ceiling-on-what-a-chats-model-may-do-scored-on-a-ladder.md)),
the grant the person gave a task, and the operations the model then called.
The window shows the task id and the ladder's two pointers; it does not show
which windows and operations those limits cover, which of them the model
used, or what it asked for that it did not have.

The host already holds each part. The catalog (`agent.Client.Describe`) lists
every launchable app's agent-exposed operations with their effects;
`keelson('windows')` the open windows; `keelson('agent_grants')` each task's
entries as `instance:app:mode[:operations]`; `keelson('agent_actions')` every
call, launch and desktop verb with its conversation, phase and reason. The
chat's Statistics panel shows token and answer distributions only.

`keelson('agent_grants')` has no column for a task's launches (the apps it may
open, and how many); the task holds them (`task.launchStrings`) but the
introspection row does not carry them.

## Design space (QOC)

**Q1 — Where does it live?**

- *O1: the Statistics panel, renamed Analytics,* with the token section and a
  new surface section. **Chosen** by the owner.
- *O2: a second panel* beside Statistics. Two toggles in a bar already
  crowded by Settings and the artefact.

**Q2 — How far does the surface reach?**

- *O1: open windows and launchable apps,* plus the desktop verbs. What the
  model could reach in this session. **Chosen** by the owner.
- *O2: open windows only.* Hides what `open_window` could add.

**Q3 — Which records count?**

- *O1: the current conversation:* the tasks it held and the calls carrying
  its id. New conversation resets the view. **Chosen** by the owner.
- *O2: the window's lifetime,* like the token statistics. Grants are per
  task and tasks per conversation; a lifetime view merges limits that never
  held at the same time.

**Q4 — How is it drawn?**

- *O1: a status-grouped table.* Readable, and what a scene asserts on.
- *O2: a graph (`widgets/graphview`)* centred on the conversation. Shows the
  nesting at a glance; painted, so a scene cannot read it.
- *O3: a toggle between the two,* one area of the panel. **Chosen** by the
  owner.

**Q5 — Where is the status of a cell decided?**

- *O1: in the chat,* re-deriving the dispatcher's order. Drifts from it.
- *O2: in `agent`, beside the dispatcher,* as one exported function the
  dispatcher's own order is tested against. **Chosen.**

## Decision

### SD1 — One status per surface cell

A cell is one (window, operation), one launchable app (`open_window`), or one
desktop verb. Its status is the first that holds:

| status | holds when |
| --- | --- |
| `above-ceiling` | the settings' ceiling refuses the effect (or launching, or the desktop) |
| `not-granted` | no task entry names the window, or the entry names other operations |
| `observe-only` | covered, the operation has an effect, and the entry's mode is observe |
| `granted` | covered and allowed; `consequential` and suggest mode still go through the person |

Use is orthogonal: a cell carries its calls in the current conversation,
counted by final phase. A call on a cell that was `not-granted` or
`above-ceiling` when made is shown as asked-without-grant, from its phase
(`input-required`, `refused`, `denied`). The unused granted cells — the
grant's slack — are the `granted` cells with no call.

The order is the dispatcher's (`agent/dispatch.go`, the `case` ladder before
`settle`), exported as `agent.ClassifyCell` over a ceiling, a parsed entry and
an operation's effect. A test runs it and the dispatcher over the same table.

### SD2 — Sources

| what | from |
| --- | --- |
| surface | `Describe` with no filter, `keelson('windows')` |
| grants | `keelson('agent_grants')`, rows whose `task` this conversation held |
| use | `keelson('agent_actions')` where `conversation` is this one — test grants included — one row per task and key: the `final` row, else the latest |
| ceiling | the window's own settings |

The coordinator records each task id it is granted in the conversation, so a
task that was stopped and replaced still counts. The chat's manifest gains
read caps on `agent_grants` and `agent_actions`. `agent_grants` gains a
`launches` column, `app:mode:count` each, from `task.launchStrings`.

The snapshot is fetched off the render thread (a `bgjob`) when the panel
opens, after a turn lands, and on a refresh button; never per frame.

### SD3 — Two views of one model

A surface model (cells with status and counts) is built once per snapshot.
Windows are those open, granted or used; one whose app offers no operation
is drawn only when a grant or a call names it, since raising and placing it
are all the model could do. Both views sit under the task line and the cells
per status. The **list** groups cells by window, then launches, then the
desktop: one line per cell with a status chip, the operation and its effect,
and the call counts. The **graph** is the same cells under `graphview`'s
radial layout: the conversation at the centre, a hub per window, one for
launches and one for the desktop, a leaf per cell; colour by status, a donut
of call outcomes on used cells, `not-granted` and `above-ceiling` faded, one
aura per hub, edge width by call count. Hovering a leaf names its cell under
the graph, and a legend under it names each encoding — the node kinds, the
status colours, the ring's outcomes, size and fading — with what each entry
means on hover; the colours are the ones the graph draws with. A toggle switches between them. *Open in play* publishes the cells as the ad-hoc dataset `chat_surface`
beside `chat_turns` and `chat_calls`.

### SD4 — The panel is renamed

The bar's button and the panel read *Analytics*, the agent surface first and
the token statistics under it; `BOXER_CHAT_ADVANCED` keeps its name and
meaning.

## Alternatives

- **Read use from the trail on `boxer.facts` (ADR-0277).** Durable and
  across sessions, but it needs a store and a SQL read surface the chat does
  not hold; the in-process tables answer for the session the panel shows.
- **A host service that classifies the surface.** One more subject and wire
  type for a function of data the coordinator can already read; the shared
  function in `agent` keeps one definition without it.
- **A window × operation heatmap.** Dense, but the cells are ragged — each
  app has its own operations — and the nesting of window, task and ceiling
  is lost.

## Consequences

- The person can see what a grant left unused and what the model asked for
  outside it, per conversation, without opening play.
- The cell status is defined once, next to the code that enforces it.
- The view is a snapshot of in-process records: `agent_actions` keeps a
  bounded ring (`keepRecords`), so a long session can lose old calls from the
  counts; the trail on `boxer.facts` keeps them, and reading it is deferred.
- The graph is not in the accessibility tree; scenes assert on the table.

## Status

Proposed 2026-10-04, built. Verified: `agent`'s classification against the
dispatcher over the same grants, the chat's surface builder, and the scenes
[chat-analytics](../../apps/chat/scenes/chat-analytics.scene.md) (a test
grant, three operations and a launch) and
[chat-statistics](../../apps/chat/scenes/chat-statistics.scene.md) (no
grant) on the headless host. Not verified: the desktop host, a person's
grant and widening, and a real model.

## Deferred

- Reading use from the trail on `boxer.facts` (ADR-0277) instead of the
  in-process ring, which also gives a view across sessions.
- Sensitivity labels and destinations (egress) as cells.
- Selecting a graph node to scroll the transcript to its calls.

## Updates

None.
