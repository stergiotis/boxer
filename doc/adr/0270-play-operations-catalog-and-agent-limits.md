---
type: adr
status: accepted
date: 2026-10-01
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-02
---

# ADR-0270: Play's operations catalog and its agent limits

## Context

[ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
gives apps a catalog of commands and queries that agents call under a task
grant, and leaves play's catalog to a decision of its own (its M4). Play is
the participant the contract was drawn for and the one where an agent's work
leaves the process: a run sends SQL to a database.

What the catalog has to account for, read from the tree on 2026-10-01:

- **The buffer is Go's.** The editor re-sends the SQL every frame and the
  frontend writes it back only when the person types, so an assignment made
  before the window draws shows in that frame without an override.
- **Parameters have two homes.** A pinned value is a `SET param_…` line in
  the buffer; a live value is in the signal store. The widget writes a draft
  and a sync after the panels draw routes it by tier. Slots are re-derived
  from the buffer on a 300 ms debounce.
- **A run's settings are per client.** `readonly = 2` is sent when
  `BOXER_PLAY_ALLOW_WRITES` is off; nothing marks one run as different from
  another. Client-side macros are rewritten off the render goroutine, and
  `keelson('…')` references survive into the residual.
- **The label of a result is not kept with it.** The dispatch decision that
  classifies a run as confined (ADR-0145) lives on the run's goroutine and as
  the client's last decision across all lanes.
- **Play's own writes skip the run path's gate.** Result pinning and Series
  adjudication write through the client's raw query path to the base
  endpoint, without the dispatch seam or any gate.
- **Most of the person's gestures change state inside play's frame**, where
  the window host attributes a change to the app, and the app's changes never
  pause a task (ADR-0269 §SD8).

## Decision

We will give play a catalog of queries and commands over its buffer,
parameters, signals, runs, results and panes; run every agent-caused
statement under play's agent limits; route the person's gestures that do
what a command does through that command; put play's own writes behind the
dispatch seam and a gate of their own; remove result pinning; and keep
adjudication, endpoint changes and turning Live on away from agents.

### SD1 — Resources and operations

| Resource | Value compared across the write-back |
|---|---|
| `sql` | the buffer |
| `params` | the parameter drafts, by name |
| `signals` | the signal store's revision |
| `result` | the main result's id |
| `panes` | the raised tab and the tab bindings |

| Operation | Class · effect | Does |
|---|---|---|
| `get_state` | query | the buffer, parameters with tier and value, signals with writer, Live, the raised tab, bindings, the main result's phase, the endpoint as a grant names it |
| `describe_result` | query | the main result's id, columns and types, row count, error |
| `sample_rows` | query | up to 50 rows of the main result as cell text, at most 8 KiB, marked untrusted |
| `list_panes` | query | each pane, whether it can draw what it is fed and why not, the node feeding it, the signals it writes; the split's nodes |
| `set_sql` | command · document | replace the buffer |
| `set_param` | command · document | set one parameter, through its draft, in its tier |
| `set_signal` | command · document | set one signal, with the task as writer |
| `run` | command · run | run the buffer, under the agent limits |
| `show_pane` | command · view | raise a pane |
| `bind_pane` | command · document | feed a panel from one split node, or from the main result again |

`get_state` and `sample_rows` are marked untrusted: buffers and cells come
from wherever the person or a dataset got them. A command to `sql` while the
editor holds keyboard focus, or to `params` while a parameter field does, is
a conflict (ADR-0269 §SD4).

`bind_pane` refuses a node the buffer's split does not have. Play's own
`BindTab` keeps such a binding inert until the name comes back, which suits
an embedder; to a caller it would read as a change that did nothing.

### SD2 — Agent limits on runs

A run is agent-caused when an agent's `run` starts it, or when Live reruns it
because of an input a task wrote (SD3). The run carries the call's
on-behalf-of context to the client, which checks, on the statement it is about
to send and before sending:

- the statement classifies as a read that names nothing outside the endpoint
  (`ClassifyQuerySecurity`'s read class); mutating, egress-reading and
  unclassifiable statements fail with "agent limit";
- every `keelson('…')` table it names is a destination of the grant, as
  `keelson:<table>`;
- the endpoint is a destination of the grant, as `clickhouse:<host>`; a
  statement Auto routes to the host's introspection engine needs only its
  tables;
- `readonly = 2` is sent whatever `BOXER_PLAY_ALLOW_WRITES` says.

`get_state` names the endpoint's destination, so a coordinator asks for it in
the grant before a run fails for it; the endpoint itself stays unsettable
(SD5).

The limits fail a run; they do not change it. Views, dictionaries and table
engines can still reach beyond the endpoint without the statement naming
them, which ADR-0269 §SD6 records with the restricted database user that
closes it.

### SD3 — Live reruns after an agent's write

A `set_sql`, `set_param`, `set_signal` or `run` marks the window as
agent-driven by its task. While it is, a Live rerun carries that task's
context and runs under SD2. The person's next edit of the buffer or of a
parameter, or their next run, clears the mark. A signal the task writes
carries the task as writer, so the Live breaker counts it as a machine write;
turning Live on, or resuming it, stays the person's (ADR-0269 §SD8).

### SD4 — The label of a result

The main lane keeps the sensitivity of the dispatch decision that produced
its result, and the window's label is confined while that result is. Queries
and runs carry it (ADR-0269 §SD7).

### SD5 — Not exposed to agents

- **Series adjudication.** The detections a verdict names live in the Series
  pane's fold of the frame, and no query reads them, so an agent could not
  say which one it judges.
- **Endpoint changes.** They move what every later run reaches.
- **Turning Live on.** It is the person's (SD3).

### SD6 — The person's gestures go through the catalog

A play control that does what a command does calls that command's handler
through the window host (ADR-0269 §SD8 "One path"). The change is logged
with the person as writer, and a task that read the resource pauses.

| Gesture | Command |
|---|---|
| Run, Run subquery, Ctrl+Enter, Ctrl+Shift+Enter, and the run Reset starts | `run` |
| a buffer swapped whole: a loaded file, a history entry, a pane's `ReplaceSql` | `set_sql` |
| the Signals section's set and add, and the signals a history entry seeds | `set_signal` |
| the panes menu | `show_pane` |
| a node's fill-tab toggles in the Graph pane, and its clear, one binding at a time | `bind_pane` |

- `run` with the person as writer runs under play's own settings, not SD2's
  limits, and makes the window's work the person's again (SD3).
- `set_signal` with the person as writer stamps the signal with the surface
  it came from, so the Live breaker counts it as a person's write.
- A buffer swapped whole takes its prelude as the parameters' new defaults,
  whoever swapped it.
- Typing in the editor or in a parameter field changes a bound value, which
  the write-back records as the person's (ADR-0269 §SD4).
- Only play's own launcher installs the path. An app that embeds play serves
  its own catalog, and play's gestures there apply directly.

Outside it: the prelude rewrite that follows a parameter edit, logged as the
app's because the edit itself is already the person's; a click on the dock's
tab strip, which play does not see; and gestures no command covers, such as
deleting a signal, resetting the parameters, or a Series verdict.

### SD7 — Play's own writes

A Series verdict (`boxer.tslabels`) is play's bookkeeping, not a statement
the person wrote. Each statement of the write takes a dispatch decision as a
run does, so it lands where the verdicts are read back, and passes a gate of
its own before it is sent:

- `BOXER_PLAY_APP_WRITES=off` stops it; it is on otherwise.
  `BOXER_PLAY_ALLOW_WRITES` keeps governing the person's INSERT and DDL, and
  `ExecuteWrite` checks it itself as well as the renderer.
- A verdict carries the window's label, since it names spans of the result,
  and a confined one goes only to an endpoint that may see sealed plaintext
  (ADR-0145 §SD5).
- A write an agent's work causes needs the endpoint in the grant, as
  `clickhouse:<host>`; none does while adjudication stays away from agents
  (SD5).

The statements travel on play's HTTP transport to the decided target, not
through the engine's `Deliver`: a request there carries no insert body, and
`chserver` refuses in-memory inputs. Moving them needs a body on
`queryengine.Request`, which is deferred.

**Result pinning is removed.** The Table pane's Pin result, the History
pane's pin browser, and their writes (`boxer.resultsets`, one
`boxer.pin_<fingerprint>` table per pin) go. A rebuild is expected over
ClickHouse's own primitives, such as the query result cache, rather than a
table play creates and manages per pin (ADR-0115, Update 2026-10-02).

## Alternatives

- **Range edits of the buffer** (`edit_buffer` with offsets) in place of
  `set_sql`. Deferred: a replace with an expected revision is enough to
  detect a stale write, and offsets into text the model reads as JSON are
  fragile.
- **A side lane for an agent's queries**, leaving the person's result in
  place. Deferred: the run path, its history and its panes are what the
  person sees; a lane of its own would hide database activity in the window.
- **Leaving the person's gestures on their direct paths**, recorded only by
  the write-back and the end of the frame. Rejected: a change made inside
  play's frame is then logged as the app's, so the person pressing Run under
  a task went unnoticed by it.
- **Play's own writes behind `BOXER_PLAY_ALLOW_WRITES`.** Rejected: the
  variable is off by default, so adjudication would stop working by default
  on a writable server, for writes the person did not author as SQL.
- **Play's own writes through the seam with no gate.** Rejected: it leaves
  the confined label unchecked on a path that copies what a result holds to
  an endpoint.
- **Keeping result pinning, routed through SD7.** Rejected: a pin copied a
  result into a table play created and owned on the person's endpoint, which
  ClickHouse's own result cache is the better base for.
- **Classifying on the render goroutine** with the diagnostics driver's
  memoised class. Rejected: it classifies the authored buffer before the
  client-side rewrites, and the residual is what the server receives.

## Consequences

### Positive

- An agent drives play's query loop — edit, parameterise, run, read, bind a
  pane — and every step shows in the window as the person's own would.
- What an agent's run reaches is bounded by the grant, checked where the
  statement is final.
- The person stepping in through play's own controls pauses a task that
  depends on what they changed.
- A verdict lands where it is read back, and a confined one stays on an
  endpoint that may see it.

### Negative

- A statement the classifier cannot parse cannot be run by an agent.
- Parameters set right after `set_sql` are refused until the slot debounce
  has seen the new buffer.
- Pinning a result is gone until it is rebuilt.

### Neutral

- A loaded file and a restored history entry now reset what Reset restores
  to, as a snippet's swap already did.
- Tables earlier builds wrote for pins stay on the endpoint; nothing in the
  tree reads them.
- An embedder's window keeps play's gestures out of its command log.

## Migration — Tier 1

- **Breaks.** Pin result and the pin browser disappear from play.
  `boxer.resultsets` and `boxer.pin_*` are left in place, unread.
- **Path.** Play's Model tab keeps its private tool loop.

## Verification plan — Tier 1

- **Lane: default `go test`.** The catalog registers; each command changes
  what its resource reads; `bind_pane` refuses a tool pane and a node the
  split lacks; the agent-limit check refuses a mutating, an egress-reading
  and an unclassifiable statement, a `keelson()` table and an endpoint
  outside the grant, and passes a read the grant covers; an agent-caused run
  sends `readonly = 2` with writes allowed. Each SD6 gesture, served by a
  host engine, is logged with the person as writer; without one it applies
  directly; the person's `set_signal` keeps its surface's signal writer.
  Play's own writes go to the decided target and are refused with
  `BOXER_PLAY_APP_WRITES=off`, for confined data to an endpoint that may not
  see it, and for an agent whose grant lacks the endpoint; `ExecuteWrite`
  refuses with writes off.
- **Lane: integration.** A verdict lands on a live server and reads back.
- **Lane: headless scene.** The agent console opens play under a test grant,
  replaces the buffer with a read of `keelson('apps')`, runs it, and reads
  the result; the person presses Run, and the task's next command is refused
  as paused until a turn reports the person's run, after which the task
  reads again before writing; a run naming a table outside the grant fails
  with the agent limit.

## Milestones

- **M1.** SD1's queries and `set_sql`, `set_signal`, `show_pane`; SD4.
- **M2.** `run` and `set_param` under SD2 and SD3.
- **M3.** `list_panes`, `bind_pane`.
- **M4.** SD6.
- **M5.** SD7 and the removal of pinning.

## Status

Accepted 2026-10-02. M1–M5 are built.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers.

## Updates

### 2026-10-02 — an uncovered run is refused when asked for

`run` checks the buffer against the agent limits before the command is
accepted, the way the run itself will: a run the grant does not cover is
refused with the destination it needs (ADR-0269's remedy, update of this
date) instead of being applied and failing in the status line, where the
trial's models never looked. The run path keeps its own check, for a
subquery run and for a buffer that changes before the run starts.

### 2026-10-02 — the snippets and the vocabulary, for an agent

Play's catalog gains three queries over what the build carries rather than
what the window holds. `list_snippets` lists the worked queries of every
snippet library — play's own and each one a repository contributes
(`RegisterSnippetLibraryE`) — or finds them through the Snippets pane's
search; `read_snippet` returns one section's text and its SQL blocks, each
ready for `set_sql`. `list_functions` is the Vocabulary pane's corpus: each
function's call template, doc, where it runs (server, client or host),
family and dependencies. Whether the endpoint has a server function is
reported only once the pane's probe has landed it — the query never sends
the probe itself, since an agent's work reaches the endpoint only under the
grant (§SD2). All three are the build's own text and are not marked
untrusted.

### 2026-10-02 — the endpoint's schema, and a check that runs nothing

The agent-operations-play trial's models learned a schema by writing
`SELECT … FROM system.columns` into the person's buffer and running it,
which replaced the buffer and the main result on every probe; a draft was
checked only by running it. Four external reads (ADR-0269 §SD1, update of
this date) take that work off the buffer. They are the Model tab's tools
(ADR-0139 §SD8), moved to where a coordinator reaches them:

- `list_tables` — a database's tables with engine, row count, comment and
  whether their column names carry leeway's encoding, and the endpoint's
  other databases.
- `describe_table` — a plain table's columns; for a leeway table, the
  sections play's resolver reads off the physical names: each value
  column's handle and type, whether it is list-valued, and each tagged
  section's membership channels (verbatim or ref, single, mixed), with a
  paragraph on reading them with handles and `LW_GET`. Physical names are
  left out, since the model is to write handles.
- `validate_sql` — the grammar and canonical form of a statement (or the
  buffer), play's client-side rewrite of it as it would be sent, the handles
  that do not resolve with their candidates, failed rewrite steps, and
  whether `run` would accept it under the grant.
- `trace_rewrite` — the same rewrite step by step: every unit of the
  pre-execute stage and play's own steps around it, in the order they ran,
  each with its kind (play, registered, late-bound), outcome (applied,
  skipped, declined), whether it rewrote the statement, its time and its
  error, optionally with the pass invocations inside it; then the body as
  it would ship, the parameters lifted from the prelude, and where dispatch
  would send it. It is `Client.RewriteTrace`, the trace the Passes tab and
  the Diagnostics pane draw, so it describes the code path a run executes.

The probes reach the pinned endpoint, so an agent's needs it among the
grant's destinations, as a run does (§SD2): the catalog reads and the trace
refuse with the destination as their remedy; `validate_sql` returns the
grammar half without it and names the destination the rest needs. Table and
column comments are marked untrusted.

## References

- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — the contract this catalog serves.
- [ADR-0097](./0097-play-reactive-query-graph.md) — play's query graph and signal store.
- [ADR-0115](./0115-query-observability-data-plane-strategy.md) — result pinning, and its removal.
- [ADR-0141](./0141-play-endpoint-dispatch-seam.md) — the dispatch seam the run limits and play's own writes sit behind.
- [ADR-0145](./0145-sealed-app-data.md) — the confined label.
- [ADR-0163](./0163-play-timeseries-workbench.md) — Series adjudication and `tslabels`.
- [ADR-0181](./0181-leeway-dql-authoring-surface.md) — `readonly = 2` on play's read path.
