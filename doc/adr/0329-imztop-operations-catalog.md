---
type: adr
status: proposed
date: 2026-10-13
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0329: imztop's operations catalog — queries over the published snapshot, view commands, and one controlling window for process-wide state

## Context

imztop ([ADR-0020](./0020-imzero2-imztop-resource-monitor.md)) draws the
system-metrics plane ([ADR-0090](./0090-sysmetrics-pubsub-data-plane.md)) and, since
[ADR-0197](./0197-imztop-replay-mode.md), replays stored history from
`boxer.facts`. It declares no operations, so an agent can see an imztop window
in `keelson('windows')` and read nothing it shows. An agent asked "what is
eating the CPU" has to go to SQL over `boxer.facts`, which needs a ClickHouse
destination and lags the live plane, or reach a screenshot.

[ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
is the contract an app joins to be driven: a catalog of queries and commands,
declared resources, a task grant. Three properties of imztop shape how it joins:

- **It changes nothing outside itself.** ADR-0020 SD11 keeps it read-only —
  no kill, renice or signal. Every command it could offer changes what a
  window shows.
- **Its data is already an immutable snapshot.** The sampler publishes a
  `PublishedSnapshot` by atomic pointer and never writes one after
  publishing, so a query can read it off the render goroutine as ADR-0269
  requires, at the cost of a pointer copy.
- **Part of its state is per process, not per window.** The live sampler and
  its freeze, the replay session and its transport speed are singletons
  (ADR-0197 §SD5). An operation addresses one window; a write to a singleton
  changes every imztop window, and each of the others would see its declared
  resource move with no command of its own to attribute it to.

## Decision

**SD1 — Queries read the published snapshot and never the window's view.**
The catalog's snapshot holds the `PublishedSnapshot` pointer the frame drew,
a copy of the window's view state, and copies of the replay status. Queries
take their own arguments: `list_processes` sorts and filters by what the
caller asks, not by what the person last clicked, so a read never depends on
the view. The queries are `get_overview`, `list_processes`, `get_process`,
`get_history`, `get_hardware`, `get_view` and `get_coverage`. Every result is
bounded: `list_processes` returns at most 50 rows, `get_history` at most 120
points per series plus min, mean, max, p95 and last over the in-memory window.
Process names and command lines are attacker-influenceable text, so the
queries that return them are marked `Untrusted`.

**SD2 — Commands change the view, set values, and never toggle.** The
per-window commands are `set_proc_view` (sort key, direction, filter, tree),
`set_procmap_metric`, `set_topology_dim`, `select_interface` and
`show_panel`. Their effect is `view`; what they write is volatile
(ADR-0315 §SD4). `show_panel` activates a dock tab once, so the person's
next click wins; the `panel` resource it writes names the last panel it
asked for, not the tab in front, because the dock layout is Rust-side state
the app does not hold. Arranging windows is the host's (ADR-0276).

**SD3 — Process-wide state is written through one controlling window.** The
first imztop window to draw claims the controller role; it passes to the next
window that draws after it unmounts. The resources `live` (freeze) and
`replay` (session state, window, pause, speed — not the playback
position, which moves on its own) carry their real value in the controller
and a fixed value in every other window, so only the controller ever sees
them move. The controller latches them at the top of its frame, so a change
it did not make itself — a click in another window, an open completing —
lands inside a frame and is attributed to the app rather than to the
person. The commands `set_frozen`, `enter_replay`, `leave_replay`,
`set_replay_paused`, `set_replay_speed` and `set_replay_window` are
unavailable elsewhere, and the refusal names the reason. A person's click
in any window still acts, as it does without a catalog.

**SD4 — `enter_replay` needs the ClickHouse endpoint in the grant.** Opening
a session dials the server holding `boxer.facts`. The queries it then runs are
fixed and bounded (ADR-0197's reader), not a statement the caller writes, so
the class stays `command` with effect `view`; but the reach is the same as an
agent's SQL in play (ADR-0270), so an agent's call is refused with a remedy
naming `clickhouse:<host>` unless the grant lists it. The open is
asynchronous; the caller reads `get_view` for the outcome.

**SD5 — The person's controls go through the same handlers.** The process
table's sort headers and tree toggle, the Proc Map and Topology combos, the
interface combo, Freeze, and the replay transport route through
`appops.Gesture`, and apply directly when the host offers no gesture path
(tour, tests) or the window may not run the command (SD3). Two controls are
exceptions. The filter text box commits on every keystroke, and a revision
per keystroke buys nothing; its writes are attributed to the person by the
frame comparison, and a command to `proc_view` while the person types in it
is a conflict. The availability brush seeks on every drag frame; it applies
directly and the controller's latch attributes the move.

**SD6 — What is not an operation.** Signalling a process (ADR-0020 SD11).
Stepping a paused replay: a relative move, which `set_replay_window` and the
position in `get_view` cover. The screenshot tour. Bulk history: stored
history is SQL over `boxer.facts` in play; publishing the in-memory window as
an ad-hoc dataset is deferred until a task needs more than the summary.

## Alternatives

- **Make replay and freeze per window first.** Rejected for now: two
  replay sessions would mean two folds and two transport goroutines reading
  ClickHouse, and ADR-0197 §SD5 made the session process-wide so that two
  windows could not disagree about one playback. SD3 can be undone by that
  refactor without changing an operation's name.
- **Declare the process-wide resources in every window.** Rejected: every
  window but the one written to would see the resource move and attribute it
  to the person, and a task reading "what changed" would get a change the
  person never made.
- **One `get_snapshot` query returning the whole snapshot.** Rejected: the
  process list alone runs to hundreds of rows, past what an inline result
  carries, and the model would pay for every series to read one number.
- **An ad-hoc dataset per history read.** Deferred (SD6): it makes every
  history read a consequential command under a publish consent, for a
  window ten minutes long that a summary describes.

## Consequences

### Positive

- An agent reads live load, top processes and the last ten minutes without
  a ClickHouse destination.
- The person's controls and an agent's calls share handlers, so the command
  log and undo see both.

### Negative

- With two imztop windows open, an agent sharing the second cannot freeze or
  replay until the first closes or is shared too. The refusal says which.
- The view-state setters change shape: sorting sets a key and a direction
  instead of toggling, and the header click computes the toggle before
  calling the command.

### Neutral

- `get_history` summarises the fold as it is: during replay it reads the
  replayed window, and the result says which mode it came from.
- Sensors and topology have no history (ADR-0197 §SD8); `get_hardware`
  returns the latest reading only.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0020](./0020-imzero2-imztop-resource-monitor.md) — imztop; SD11, read-only.
- [ADR-0197](./0197-imztop-replay-mode.md) — replay; §SD5, the process-wide session.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — the operations contract.
- [ADR-0270](./0270-play-operations-catalog-and-agent-limits.md) — play's catalog; endpoints as grant destinations.
- [ADR-0276](./0276-agents-read-and-arrange-windows.md) — window verbs are the host's.
- [ADR-0315](./0315-coordinator-tools-as-operation-specs-and-a-contract-for-stateful-tools.md) — rebuild classes.
