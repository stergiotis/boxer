---
type: adr
status: accepted
date: 2026-10-28
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-28
---

# ADR-0345: The runtime's own apps join the operations contract — queries over what the host holds, commands only over the view; the log viewer first

## Context

The runtime ships windows about the host itself: the log viewer over the
process's log tail, the config inspector over the env-var registry, the
help center over the apps' books, the launcher and the app center over the
app registry, and under `apps/` the capability inspector and the app-state
inspector. None declares an operations catalog
([ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)),
so an agent sees each of them in `keelson('windows')` and reads nothing it
shows. The gap matters most for the log viewer: an agent whose command
failed to apply, or whose window failed to mount, has the host's log as the
one place the reason was written, and no way to read it. Log lines reach
`boxer.facts` only when `BOXER_LOG_FACTS` is set; otherwise the sink's
in-memory tail is the only copy, and the window is the only reader.

Three properties of these apps shape how they join the contract:

- **Their state is the host's, not the window's.** The log sink, the env
  registry, the app registry and the help library are process-wide, and
  the window reads them through bounded probes it does not own. Nothing a
  window could write there is its own; the only thing it holds is its
  view — a filter, a selection, an expanded section.
- **Much of it is already a table.** `keelson('apps')`,
  `keelson('app_operations')`, `keelson('windows')` and the capability
  tables answer, under the agent schema
  ([ADR-0313](./0313-agent-schema-generated-relations-an-agent-may-read-in-full.md)),
  what the launcher, the app center and the capability inspector draw. The
  log tail and the env registry's values in force have no table.
- **Some of what they show is privileged.** The config inspector can
  reveal sensitive values; the app-state inspector can clear what an app
  keeps. Both are the person's acts at the window.

## Decision

System apps join ADR-0269 as any app does, with no mechanism of their own,
under three rules: a query reads the host's state through the probe the
window draws from and takes its own arguments; a command writes the view
and nothing else, so every command's effect is `view`; a catalog carries
only what no `keelson()` table carries. The log viewer's catalog is built
with this record; the others are milestones (SD6).

### SD1 — Which apps, and the rule each follows

| App | Reads from | Catalog holds | Not an operation |
| --- | --- | --- | --- |
| log viewer (`keelson/runtime/logviewer`) | the `logbridge.Sink` tail | the tail, filtered by the caller's arguments; a row's detail; the counters; the view | writing the sink |
| config inspector (`keelson/runtime/configview`) | the env-var registry (ADR-0009) | variables, whether each is set, values and defaults unless sensitive; the view: filter, selection, the open sections | sensitive values and the reveal toggle (SD4) |
| help center (`keelson/runtime/helphost`) | the help library | the view: the open book, document and section, the expanded books, the search text, rendered or source | the help text itself: `runtime.agent.help` serves it |
| launcher (`keelson/runtime/launcher`) | the app registry | the view: query, kind toggles, topic chips, the selected row | listing apps: `keelson('apps')`; launching: the host's `launch` |
| app center (`keelson/runtime/appcenter`) | the registry and the keelson tables | the view: which app's page is open, the list's filter | the page's rows: each is a table already |
| capability inspector (`apps/capinspector`) | the registry and the in-process audit tally | the view: the selected capability; the tally's per-capability count and last-minute buckets, which no table carries | the declared subjects and the audit records: `keelson('client_caps')`, `keelson('app_audit')` |
| app-state inspector (`apps/appstate`) | the window's last read of `keelson('app_state')` | entries listed from that read, bounded; the view: the selected app, when the read landed; a refresh that asks for the next read | clearing or forgetting an entry (SD4); a statement of the caller's |

The hand-driven coordinator, `apps/agentconsole`, is not in the table and
declares no catalog: an agent driving a window that calls other windows'
operations would be a second dispatcher path beside the host's.

Two apps hold state that is not the window's. The launcher's filter and
selection belong to the one component every launcher window and the host's
empty-state pane draw (ADR-0214 §SD2), so they are written through one
controlling window as ADR-0329 §SD3 writes imztop's freeze: the first
window to draw claims the role, every other window reports a fixed value
and refuses the commands, and the empty-state pane applies directly. The
app-state inspector reads the store on its own poller, so its `read`
resource is latched at the top of each frame and a read that lands between
frames is attributed to the app; `refresh` asks for the next read and
`get_view` says when it landed.

### SD2 — The log viewer's catalog

Queries read a copy of the tail the snapshot took with the frame, so a
query and the window agree on what the ring held. Each takes its own
arguments: `list_rows` filters by level floor, app substring and message
substring as the caller asks, not as the person's filter row is set, and
returns the last `limit` matches in chronological order, at most 200,
with the count of all matches. `get_row` returns one row in full — fields,
the structured error chain, the stack — bounded per text and per list, and
is refused when the key names a row that has left the ring; the refusal
says so, since the row may still be in `boxer.facts`. `get_counters`
returns the sink's running totals. `get_view` returns the filter, the
selection, follow, and how many rows the table shows. A row's key is its
timestamp and a digest of its caller and message, which is what the
window itself uses to recognise the selected row across frames.

Log lines are text any code in the process wrote, including content an
agent fetched and an app then logged, so every query that returns a line
or the filter text is marked `Untrusted`. When the host has not installed
a sink, `list_rows`, `get_row` and `select_row` are unavailable and the
reason says why; `get_counters` and `get_view` answer and say the bridge is
not installed.

Commands set the view and never toggle: `set_filter` sets all three filter
fields at once, `set_follow` sets follow, `select_row` selects a row by key
or clears the selection with an empty key, and is refused for a key not in
the ring. The resources are `filter`, `selection` and `follow`; what they
hold is volatile (ADR-0315 §SD4).

### SD3 — The person's controls go through the same handlers

The level combo, the Follow checkbox, a row click and Clear selection
route through `appops.Gesture`, and apply directly where the host serves
no catalog (tour, tests), as ADR-0329 §SD5 does for imztop. The two text
filters commit on every keystroke and apply inside the frame, so their
writes are attributed to the app, and the `filter` resource reports
editing while either box has focus, so an agent's `set_filter` during
typing is a conflict rather than a lost keystroke.

A checkbox is bound through a data binding, which the runtime writes at the
frame's sync, after the app's body returned. A control bound that way
writes a draft field, and the top of the next frame routes a draft that
differs from the committed value as the person's gesture; a command that
sets the value sets the draft with it, so an agent's write is not mistaken
for a click. A control answered from the previous frame's response flags,
such as a button or a segmented selector, routes inside the frame.

### SD4 — What is privileged stays the person's

- **Sensitive values.** The config inspector's reveal is a click at the
  window and nothing else. A query over the registry returns a sensitive
  variable's name and whether it is set, and marks the value withheld,
  whatever the window's reveal state; the window's reveal is not a
  resource, so an agent cannot read it and cannot ask for it.
- **Clearing app state.** It writes the persist store, outside the
  window; it would be consequential, and no task has a reason to do it
  that the person cannot do faster.

### SD5 — Labels: a log line has none of its own

The sink does not label lines; the window reports the host's label, and
an agent's read of the tail carries it. A line may quote content from a
confined result if an app logged it. This record accepts that gap rather
than scanning lines: the untrusted mark, the ceiling
([ADR-0280](./0280-a-ceiling-on-what-a-chats-model-may-do-scored-on-a-ladder.md))
and the person's choice to share the window are what bound it. An app that
logs a confined body is the defect to fix; the labels ADR
([ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md))
is where a per-line label would be declared if one is ever needed.

### SD6 — The other catalogs

Each was built under this record as the table in SD1 describes, with the
log viewer's catalog as the shape. An app whose state fits the table needs
no decision of its own; one that does not — an app wanting a command with
an effect other than `view` — is a new record.

## Alternatives

- **A `log_tail` relation under the agent schema instead of a catalog.**
  Deferred, not rejected: it would serve a window-less run and needs no
  shared window. It was not chosen first because the agent schema reads
  rows in full under a scope, a log line's label is unknown (SD5), and the
  ring moves between two reads with no `as_of`; the catalog's snapshot
  pins what a task read. The two can coexist.
- **One catalog the host mounts over every system app.** Rejected: the
  catalog is a property of the app (ADR-0269 §SD2), and these apps share
  no state to mount it over.
- **Sensitive values under a standing consent.** Rejected: a secret shown
  to the model leaves the cell by the model route, no consent class
  covers it, and the person has the window.
- **A `clear_tail` or `set_min_level` command on the sink.** Rejected: the
  sink is process-wide and every window reads it; the viewer never wrote it
  and does not start now.
- **Exposing the launcher's and the app center's rows.** Rejected: each
  row is a `keelson()` table already, and a second copy in a catalog would
  drift from it.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API under `public/`: `logviewer`, `configview`, `helphost`, `launcher`, `appcenter` | added: each catalog's argument and result types and the instance's `Operations` method | nothing; additive |
| `keelson('app_operations')` | gains the seven catalogs' rows | nothing; the table's shape is unchanged |

## Consequences

### Positive

- An agent reads the host's log tail, filtered and bounded, without a
  ClickHouse destination and without `BOXER_LOG_FACTS`.
- The person's controls and an agent's calls share handlers, so the
  command log and undo see both.
- The rule in SD1 keeps the family's catalogs small: what is a table stays
  a table.

### Negative

- The tail is copied once per snapshot while a task is attached, beside
  the copy the render already takes per frame; the config inspector reads
  the registry and every value once per snapshot in the same way.
- With two launcher windows open, an agent sharing the second cannot set
  the filter or the selection until the first closes; the refusal says
  which (ADR-0329 §SD3's cost, inherited).
- A row's key is stable only while the row is in the ring; an agent that
  waits too long between `list_rows` and `get_row` is refused and told to
  look in `boxer.facts`.
- Typing in a filter box is logged as the app's write (SD3), as imztop's
  filter is.

### Neutral

- Nothing in a system app's catalog has an effect above `view`, so the
  ceiling's "change the view" step covers every command here.

## Migration — Tier 1

Nothing to migrate. Nothing is removed or renamed, and the apps register
as before; a catalog that fails validation is withdrawn with a diagnostic
and the app still registers (ADR-0269 §SD2).

## Verification plan — Tier 1

- **Lane.** Default `go test`, in each app's package: the catalog
  validates and registers with `Agents` on every operation and effect
  `view` on every command; a listing query reads its arguments and not the
  view, and is bounded; a key or name no longer held is refused; commands
  set and never toggle; the engine attributes a gesture to the person, a
  checkbox draft to the person on the next frame, and a call to its task.
  The log viewer's lane also covers the view without a sink; the config
  inspector's covers a sensitive value withheld under either reveal state
  and the reveal toggle moving no resource; the launcher's covers the
  controlling window.
- **What would fail.** A query that read the window's filter, a command
  that toggled, a catalog without `Agents` on its operations, a sensitive
  value in a result.
- **Gap.** No headless scene drives the catalog through the chat; the
  chat's own scenes cover the dispatcher, and this app adds no path there.

## Milestones

- **M1 — the log viewer.** ✓
- **M2 — the config inspector.** ✓
- **M3 — the help center, the launcher, the app center.** ✓
- **M4 — the capability inspector and the app-state inspector.** ✓

All four were built alongside the proposal.

## Status

Accepted 2026-10-28. Built alongside the proposal; the seven catalogs land as one commit per app.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — the operations contract.
- [ADR-0329](./0329-imztop-operations-catalog.md) — imztop's catalog; the precedent for read-only state and gestures through handlers.
- [ADR-0270](./0270-play-operations-catalog-and-agent-limits.md) — play's catalog; what is not exposed.
- [ADR-0313](./0313-agent-schema-generated-relations-an-agent-may-read-in-full.md) — the agent schema; the tables a catalog must not duplicate.
- [ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md) — labels; where a per-line label would be declared.
- [ADR-0315](./0315-coordinator-tools-as-operation-specs-and-a-contract-for-stateful-tools.md) — rebuild classes.
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) §SD6 — the log bridge's composition.
- [ADR-0009](./0009-environment-variable-registry.md) — the env-var registry the config inspector reads.
