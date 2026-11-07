---
type: reference
audience: agent operating the window, and the person reading what it did
status: draft
title: Operations
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Operations

The catalog an agent drives under a task grant (ADR-0269). One resource
holds the spawn controls; two queries read the controls and the tasks this
window spawned; a command sets one control, a command spawns a task and
settles when it has started (ADR-0358), and a command cancels a task by
id. The Start buttons call `spawn` as the person's gesture, so a click
and an agent's call run the same handler.

On a host that gives the window no task API, `spawn` and `cancel_task`
are unavailable, with the reason "this host gives the window no task API,
so nothing can be spawned or cancelled here"; the queries still answer.

## Resources

- `controls` — what the next spawn uses: `Unit` (items, bytes or steps),
  `DurationSec` (1 to 60), `SimulateError` and `SlowFrame`. It is
  restorable: the host may put a whole set of controls back, and a set
  with a unit or a duration out of range is refused. The slider and the
  two check boxes write it directly as the person's edits; `set_control`
  and a Start button write it through the catalog.

## Queries

### `get_view`

Reads `Controls`; `Tasks`, whether the host gives the window a task API;
`Inflight` and `History`, the rows in the monitor's two panes, which on a
shared bus include other producers' tasks; and `Spawned`, how many tasks
this window records.

### `list_tasks`

Lists the tasks this window spawned, oldest first. The rows are the
window's own record, which the monitor's panes show for this producer;
the monitor itself keeps its rows private.

- `State` — `running` (starting or running), `finished` (done, error,
  cancelled or not started), or one state exactly; empty keeps every
  task. Any other word is refused naming the states.
- `Limit` — tasks to return; 0 is 50, at most 200. A negative limit is
  refused: "limit is not negative".

The result carries `Matched`, how many tasks match whatever the limit,
`Tasks`, the first `Limit` of them, and `Truncated`, set when the limit
left matches out. A row has the task's `Id` (empty while the task is
starting, or when the spawn failed), `Kind` (`demo.<unit>`), `Title`,
`Unit`, `DurationMs`, `State`, `Current`, `Total`, `Fraction` and, for an
errored task or a failed spawn, the `Reason`, cut at 512 characters. The
output is marked untrusted: a title is text the window composed from a
spawn's arguments.

## Commands

### `set_control`

Sets one control to a value; a view effect on `controls`, and a repeat is
applied, never toggled. `Control` is `unit`, `duration`, `simulate_error`
or `slow_frame`; `Value` is `items`, `bytes` or `steps` for the unit, a
number of seconds from 1 to 60 for the duration, `true` or `false` for
the other two. A value out of range is refused naming the range: "the
duration is between 1 and 60 seconds", "the unit is one of items, bytes
or steps", "the value is true or false"; an unknown control is refused
naming the four. The person's gesture is the slider and the check boxes
under **Spawn**.

### `spawn`

Spawns one fake task of the shape `Unit` names, or of the controls' unit
when the argument is empty, with the controls' duration and error flag;
the shape becomes the controls' unit. Its effect is `run`, and it is
asynchronous: the call is `running` until the worker has spawned the task
on the bus, then settles with the task's `Id`, `Kind` and `Title`. A
spawn the bus refuses settles as a refusal carrying the error. An unknown
unit is refused before anything starts. From there the task runs for its
duration and ends in the monitor's **History** pane; `list_tasks` follows
it by id. The person's gesture is one of the three Start buttons.

### `cancel_task`

Asks a running task of this window to cancel. The cancel is published on
the bus, where every observer of the task sees it, so the effect is
**consequential** and the host asks the person to confirm it every time
an agent calls it. The worker sees the cancel on its next tick, ends the
task, and the monitor lists it as cancelled. An id this window did not
spawn is refused: "no task of that id was spawned by this window;
list_tasks names them"; a task already finished is refused naming its
state. The person's own cancel is the row's **Cancel** button in the
monitor's In-flight pane, which does not pass through the catalog.
