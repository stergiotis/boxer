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
on-behalf-of context to the client. Every other lane of the window — a pane's
CTE, the Map's raster, an observed intermediate — runs SQL derived from the
same input, so while the window carries a task's mark (SD3) the client checks
those runs against it too. The Diagnostics probe is not checked: `EXPLAIN AST`
parses and resolves nothing. The client checks, on the statement it is about
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
(`RegisterSnippetLibrary`) — or finds them through the Snippets pane's
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

### 2026-10-02 — datasets by alias, and the rewrite without the endpoint

Three gaps in the entry above:

- **A grant named a bound dataset by a name the check never saw.** An
  ad-hoc dataset's alias is rewritten to its ephemeral handle before the
  passes run, and the limits read the residual, so `keelson:chat_turns` did
  not cover `keelson('chat_turns')` and the refusal named the handle. The
  limits now map a bound handle back to its alias: the alias is what a grant
  lists and what a refusal asks for; the handle is still accepted.
- **No operation named the window's datasets.** `list_datasets` lists each
  bound alias with the destination a run needs and whether the grant lists
  it, and the columns of those it lists that are not sealed, read by an
  empty run of the dataset under the agent limits. Every ad-hoc dataset is
  sealed (ADR-0240), and its columns are its content's shape, so they come
  only when one dataset is asked for by alias, and that result is labelled
  confined (ADR-0269, update of this date): a model that may not read
  confined content gets a data handle in its place, and the listing stays
  readable.
- **Without the endpoint the rewrite was all or nothing.** Only the
  late-bound steps read the endpoint's catalog — handles, `LW_GET` and
  `LW_SEL`, `fs()`, the constructor target — and the selection-condition
  rewrite. `validate_sql` and `trace_rewrite` now make the rewrite without
  them (`Client.buildResidualOffline`), report them declined, and name the
  endpoint as what the whole rewrite needs; `trace_rewrite` no longer
  refuses. A statement dispatch sends to the introspection plane does not
  ask for the endpoint at all.

### 2026-10-02 — the trial cited above left the tree

Two updates of this date cite what the agent-operations-play trial's models
did. The protocol and its runs were withdrawn from the tree (ADR-0269,
update of this date). Both decisions stand on the behaviour they describe:
a run refused in the status line went unseen, and schema probes ran through
the person's buffer.

### 2026-10-03 — the Projection pane, for an agent

A chat model asked to cluster a result in play rewrote and reran its query
some thirty times without a picture: nothing it could call started the
Projection pane, read what it drew, or said why it drew nothing. The catalog
gains a resource, `projection`, and three operations:

- `compute_projection` (command, view effect) sets the run's neighbours,
  minimum cluster size and feature set and raises the pane; the pane's next
  draw starts the run over the result it draws, as the Compute button does —
  which now goes through the command (SD6). It refuses while the pane cannot
  draw, while a run is in flight, and below three rows.
- `get_projection` (query) reports the run: status and error, the clusters
  with their sizes and the noise, the pane's status line, whether the layout
  has settled, and a page of points with row, cluster, membership
  probability and position. The layout lives in the widget and moves only
  while the pane is drawn; the snapshot copies it when a run lands, every
  quarter second while it moves, and once more settled.
- `explain_clusters` (query) returns the pane's "why these clusters" (ADR-0235,
  ADR-0238): by features at a rule depth, one tree per cluster or the one
  partition, or by attributes — per cluster the SQL rule, its fit, and what
  sets the cluster apart, the same text the section shows.

The resource's revision moves inside a frame, as the result's does: a run
lands on its own goroutine, and a change seen between frames would be
credited to the person and pause the task. Both queries are marked
untrusted: rules and contrasts quote the data. The
pane's accept step now refuses a result that is not leeway-shaped — the
features come off the leeway card — so `list_panes` says so before a run
would fail, and a run builds the card driver for its own result instead of
relying on the Table or Detail pane to have built it.


### 2026-10-04 — the Diagnostics pane, for an agent

`get_diagnostics` (query) returns the Diagnostics pane's seven sections as
fields, read from what the pane reads on the render goroutine: the
statement's status — parses, outside boxer's grammar, rejected with
ClickHouse's own diagnostic, still being checked, unverified — with the
parser's error; the client-side rewrite's skipped and declined steps with
their errors; the leeway handles that do not resolve, with candidates; the
security class with its witnesses and the tables returned as stored; the
query graph split of the last run; dropped signal emits; and the last run's
full error or summary. It starts nothing the frame would not: the rewrite
trace is demand-driven for the two lazy panes that draw it, so the query
reports it only once one has measured this buffer, and says so;
`validate_sql` reports a statement's failed rewrites without it. Marked
untrusted: server diagnostics quote the statement and its data.


### 2026-10-04 — an agent binds a dataset by its alias

A window binds an ad-hoc dataset only through its launch config (ADR-0240
§SD7), and a coordinator's `open_window` carries no config, so a model could
publish a dataset in one window and not read it in the play window it
opened. `bind_dataset` (command, document effect) follows an alias in this
window as a declared one is followed: resolved off the frame on the
follower's next round — `adhocdata.NewDeferredFollower` and `Follower.Follow`,
since a command runs on the render goroutine — bound to the newest live
dataset, kept in step with republish and retract, and named in the
waiting notice until one is published. A handle is refused; the grant names
the alias. Binding runs nothing: a launch config's alias still reruns the
buffer when it binds, an added one does not, since that run would be the
task's work started without its call. The catalog gains the resource
`datasets`, the bound and the waiting aliases.


### 2026-10-04 — a bind that waits says so, and binds do not conflict

A chat session bound aliases that never bound and could not tell: the
call's outcome was "accepted", `bind_dataset` answered `Bound: false`, and
`list_datasets` listed bound aliases only, so a waiting alias looked like
no alias at all. The follower now keeps, per waiting alias, why it waits —
not asked yet, nothing live under it, its dataset withdrawn, or the service
not answering (`Follower.Waiting`) — and `bind_dataset` and `list_datasets`
report it. `bind_dataset` declared that it writes `datasets`, whose
revision a bind moves a frame later, so a second bind in a row was a
conflict; it now writes `followed_datasets`, the aliases followed, which only
the command changes. Why those binds never bound is not settled.


### 2026-10-04 — the documentation lookup is not bound by the mark

SD2 checks every lane of a window that carries a task's mark, because
those lanes run SQL derived from the task's input. The Docs pane's lookup
does not: its statement is play's own and the looked-up name enters only as
a bound parameter. Checked anyway, it failed under any grant that did not
list the endpoint — an agent working over `keelson('…')` tables lost the
Docs pane. A lane may now declare its statement its own
(`ExecOptions.OwnStatement`); under a mark it is sent without the grant
check and with `readonly = 2`, and an explicit on-behalf-of context is
still checked. The documentation lookup is the one lane that declares it.

### 2026-10-05 — pane outputs, frameless panes and run's checks at the call

An audit of play's panes against the catalog found operations that
reported a change the window then undid or never made. Each is now refused,
or made, at the call:

- **`set_signal` refuses a pane's outputs.** The Timeline writes `tl_from`,
  `tl_to`, `tl_min` and `tl_max` again every frame it draws, and the
  Graphview `gv_selection`; the Map's `vp_*`, the Vector field pane's bounds
  and `gv_hover` are written by their pane and never read back, so a value
  set from outside moved the queries reading it while the picture stayed.
  The signal declaration (`reservedSignals`) now says which of its names
  hold another writer's value (`Shape`), and for the others what to call
  instead (`Instead`): the pane's own command — `set_timeline_window`,
  `set_map_view`, `set_vectorfield_view`, `select_graphview_nodes` — or,
  for the Timeline's extent, the read that reports it. The names are
  fixed here and the commands land with their panes' sections, in later
  updates. The person's Signals section still writes any name.
- **`selection` moves with its companions.** A pane's click writes the
  row through `selectionStamper`, which also writes `selection_node` and,
  from that row, `selection_id` and `selection_key`; `set_signal` wrote the
  row alone, so a detail query following the key read the previous entity.
  It now writes through the stamper, for the result the panels draw or a
  node a pane is bound to (`Node`), and refuses a value that is not a row
  of that result.
- **`bind_pane` refuses the panes that read CTEs by name.** Network,
  Graphview, Sankey and Vector field draw their named CTEs off the split
  and ignore the frame they are handed (`TabSpec.Frameless`); a binding was
  reported applied and listed by `list_panes` while the pane drew what it
  drew before. The refusal names the CTEs the pane reads, and the Graph
  pane no longer offers those panes as fill targets.
- **`run` checks a subquery run and the class ceiling at the call.** The
  entry of 2026-10-02 checked the whole buffer and skipped a subquery run;
  it now checks the narrowed text the run ships. The window's class ceiling
  (ADR-0187 §SD5), which only the run path judged, is checked first, since
  its reason names the parameter that raised the class.
- **The observed node is part of `panes`.** It decides what every unbound
  panel draws and what `list_panes` reports as their node, so the person's
  "observe in panels" now moves the resource.
- **`describe_result` is marked untrusted**: its column names are the
  statement's and the data's.

### 2026-10-05 — reads that describe what the panes draw

The same audit found reads that described something other than what the
person sees. Each now reads what the pane draws, or what a run would ship:

- **`sample_rows` (version 2) keeps NULL apart from empty text.** Cells
  were formatted with the grids' fallback, which writes NULL as empty
  text, so the two could not be told apart (ADR-0269 §SD10). A NULL cell
  stays empty text and its index is listed in `Nulls`. For a leeway
  result, each column also comes with its handle, the name the Table's
  header shows and `describe_table` teaches, and `Fields` accepts handles
  as well as physical names. A field that names no column is refused.
- **`get_diagnostics` (version 2) reports the run the panels draw.** Its
  last run read the main result while the pane reads the active frame, an
  observed intermediate included. It now reads the active frame and so
  declares that it reads `panes`. It adds the buffer's gloss directives
  that did not compile, which only the Table's pager showed. The
  description of `Measured` names `show_pane diagnostics` as the way to
  get a measurement.
- **`get_projection` and `compute_projection` count the rows the pane is
  fed.** They counted the main result's rows. When the pane was bound
  elsewhere, the reading described the wrong result, and a compute could
  be accepted that the pane's draw then dropped without starting.
- **`list_functions` (version 2) searches as the Vocabulary pane does.**
  Every word or pattern must match the name, doc and family taken
  together, through the pane's parser and thesaurus. Before, the whole
  query was one substring, so a multi-word search found nothing. A client
  macro lists the server functions its expansion lacks once the pane's
  probe has answered (`MissingDependencies`). Functions the endpoint
  carries and no roster declares are listed, as the pane lists them. Only
  names that are plain identifiers are listed; the others are counted, so
  the endpoint's text does not enter an operation that is not marked
  untrusted. The summary says that ClickHouse's own functions are not
  listed.
- **`validate_sql` and `trace_rewrite` (version 2) default to what `run`
  would ship.** Without `sql` they checked the whole buffer. A run sends
  the caret's statement of a buffer that has several, with the prelude
  (`runBuffer`). The signal values a run puts beside the body are still
  not in `Params`; that part is deferred.
- **The status line shows the ceiling's and the agent-write refusals.**
  It showed a refused run only while inputs were unfilled, so the class
  ceiling's refusal and "an agent's run does not write" were set and never
  drawn. They now stand until the next run.

### 2026-10-05 — list_panes reads what a pane drew; the result reads take a pane or a node

`list_panes` judged a pane by its schema alone, and `describe_result` and
`sample_rows` read only the main result. A pane bound to a node, or the
panels while an intermediate is observed, drew a result neither read could
reach, and a pane whose data broke its contract read as able to draw.

- **`list_panes` (version 2) reports the pane's last draw.** A pane whose
  driver keeps a status line gives it through `TabSpec.Status`, read
  between frames, so it describes what the last draw showed. `Status` is
  reported, tagged with the result it is of (`StatusOf`), only while the
  schema verdict lets the pane draw; otherwise the driver was not reached
  and its state is of an earlier result. When the data, not the schema,
  left the pane with nothing to draw — no column resolving to countries, a
  repeated heatmap cell, a fold reject, an empty hierarchy, a layout that
  failed — `Draws` turns to no with that reason, but only for a draw of the
  last frame, so a hidden pane is not judged on a stale draw. `Visible`,
  `Zone` and `Lazy` say whether the body drew last frame and whether it
  draws only in front (`lazypane.Pane.Live`). The read never draws or
  raises a pane. It is marked untrusted, since a status line quotes values
  and lane errors. Map and Vector field keep their status lines in another
  form and report none; that is deferred.
- **`describe_result` (version 2) and `sample_rows` take `Pane` or
  `Node`.** A pane resolves to the node it is fed; neither resolves to the
  result the unbound panels draw. A node is read when a pane draws it or
  when it is the main result; any other node is refused with `bind_pane`
  as the way to give it a result. A pane that draws CTEs by name is refused
  with the same reason `bind_pane` gives. `sample_rows` keeps version 2,
  which the previous entry introduced on the same day: the additions are
  optional arguments and new result fields.
- **The snapshot copies lanes, not records.** The audit proposed retaining
  each lane's record in the snapshot and releasing it at the next one.
  Queries run on their own goroutines, so that release could free a record
  a query is still reading. Instead each read peeks the lane under the
  lane's own lock and holds the record for the length of the call, as
  `MainSnapshot` does for the main lane. Peeking starts no run. Like the
  main read, it can return a result that landed after the frame was drawn;
  the result id says which.
- **The window's label covers bound and observed lanes (§SD4 extended).**
  A node lane keeps the dispatch label of the run that produced its
  result, and the window is confined while the main result, the observed
  intermediate's or a bound node's result is. The other lanes — the Map's,
  the Chat roster and reactions, Kanban's lanes, the CTE lanes of Network,
  Graphview and Sankey, the Vector field's — still carry no label; that
  part of the audit's finding remains open.
- **What a result read says.** `describe_result` names the node, each
  column's leeway handle and gloss label, whether the result is
  leeway-shaped together with how to read such columns, and whether the
  result is itself a prefix and why (only the main lane carries a
  truncation). `sample_rows` reads named `Rows` as well as a run from an
  offset, returns each row's number, takes a gloss label in `Fields`, says
  which bound cut the sample and where to read on, and repeats the result's
  own truncation. The faces a gloss draws a cell with are not returned;
  that is deferred.

### 2026-10-05 — the Chart, Distribution and Series panes, for an agent

A model asked what a chart showed could learn only that the pane could
draw: the axis it chose, the series a cap dropped, a distribution's
quantiles or a series' sampling interval were pixels. Each of the three
panes gets a read and a command of its own, `get_<pane>` and
`set_<pane>_options`, rather than one generic operation over every pane:
the readings have little in common past a header, and a pane's own name
says to a model what it reads. Later panes follow the same shape.

- **A read is the pane's last draw.** It opens with the result id that
  draw was of, the node feeding the pane, its status line, and why it
  drew nothing when it did not. A pane that has not drawn, or a lazy one
  not in front, is refused with `show_pane`: its fold is missing or of an
  earlier result, and a read neither folds nor raises the pane. The
  snapshot copies a fold once per fold (each driver counts its folds);
  quantiles, band widths and distances are computed in the query from
  the copied grids with the functions the views draw with. The reads are
  marked untrusted: they quote labels and values of the data.
- **What each reads.** `get_chart`: the reading (lanes or grid), the x
  axis's kind and first categories, each series' points, nulls and
  range or the heatmap's keys, the marks offered and drawn, the log axis,
  and the rows and series the caps left out. `get_dist`: per series its
  count, quantiles at five probabilities read off its grid, the DKW band
  width, the Wasserstein-1 distance to the baseline, and on request its
  letter values; which views the result admits and which is drawn.
  `get_series`: the time range, the Δt class with the pane's finding and
  its scaffold SQL, each lane's range, the smoothing and envelope, and
  the score and span overlays with each span's recorded verdict and the
  measured readout. A scaffold is text to adapt and apply with
  `set_sql`; the button that inserts it at the caret stays the person's.
- **Options are document effect, set and never toggled.** Each command
  writes a resource named after its pane (`chart`, `dist`, `series`),
  whose value is the pane's option fields; an option left out keeps the
  person's setting. A value the last fold does not admit — a heatmap on
  a lanes chart, a log axis over a non-positive value, the Shift view
  without a shared grid, a half-width outside the kernel's range — is
  refused with the pane's reason. `compute_projection` keeps its view
  effect; the modes treat the two alike (ADR-0269 §SD5).
- **The panes' option buttons go through the commands (§SD6).** The
  chart's mark chips, the distribution's view buttons and the series'
  smoothing stepper changed driver state inside the frame; they now call
  the command as the person's gesture. The bound boxes (log axis,
  smoothing, envelope) land at the write-back and need no routing.
- **The Distribution baseline is the selection.** It moves with
  `set_signal('selection', row)`, which `get_dist` names. The series
  chips, and a click on a chart or series plot, still write the
  selection from inside the frame as the pane's writer; routing a pane's
  selection through the catalog is left to a later step.
- **Adjudication stays the person's (§SD5 restated).** §SD5 kept Series
  verdicts from agents because no query read the detections a verdict
  names. `get_series` now lists them, so that ground is gone. The
  decision stands on what remains: a verdict is a write outside the
  window, to `boxer.tslabels`, through play's own gated path (§SD7),
  and the readout the person judges a detector by is computed from those
  verdicts. A model recording them would grade the evaluation it is
  asked about. No operation records a verdict.

### 2026-10-05 — the Timeline, Treemap, Icicle, Kanban and Cards panes, for an agent

The five panes get the read and the command of the entry above, plus,
where the pane keeps a window or a pin of its own, one command that sets
it. Names follow the pane ids `list_panes` gives.

- **Timeline.** `get_timeline` reads the mode, the events drawn and the
  rows skipped, the events per `_tl_lane`, the extent (`tl_min`/`tl_max`),
  the brushed window, the view, the selected row and the bands overlay's
  line and bands. `set_timeline_options` sets the now line only; the
  view stays a camera the person moves (ADR-0269 §SD8), and the bands
  query, which runs SQL, is not offered. The pane gained a status line in
  `list_panes`, and a result whose every row was skipped reads as one it
  could not draw.
- **The brushed window is a command (`set_timeline_window`).** The pane
  republishes `tl_from`/`tl_to` every frame it draws, so `set_signal`
  refuses them (entry of this date above). The command moves the widget's
  brush and writes both signals in the same call, so the window holds
  whether or not the pane draws that frame, and the pane's own publish
  finds the values in place. A window takes UTC text, from before to;
  `clear` gives back the unbounded window. The person's brush is taken
  back after the widget commits it and made again through the command.
- **Treemap and Icicle.** `get_treemap` lists a container's children by
  total with share, own value, colour or category and whether it was
  inherited; `get_icicle` lists a frame's children by total, or every
  frame under it by its own value, which is the profile question. Both
  name nodes by their label path, which the selects take back. Both
  report what the build dropped and the drill or pin. Their options are
  the pane's switches, set by name (`set_treemap_options`,
  `set_icicle_options`).
- **Pins are commands, and keep `selection_key` with them.**
  `select_treemap_node` pins a leaf and `select_icicle_frame` a frame;
  each writes the label to `selection_key` in the same call, and the
  pane's click goes through it. The icicle's click also zooms the value
  axis; the command does not, since that axis is the plot's own camera.
  An icicle re-laid out for a new order or prune now keeps its pin by
  path, where it used to keep an index that could name another frame, and
  a prune that removes the pinned frame drops the pin and empties
  `selection_key`.
- **Kanban has a read and no command.** `get_kanban` counts each lane's
  cards and sums its dot tallies over the whole fold, up to the board's
  card cap rather than `sample_rows`' fifty, says which lanes the `lanes`
  CTE declared and whether it failed, and lists one lane's cards on
  request. The board is read-only and has no options; its selection is
  `set_signal('selection')`.
- **Cards reads the page drawn.** `get_cards` gives each card's text as
  its glosses resolved it, the warning facts the pane raised where a
  gloss could not be honoured, and the hero's media type or the reason
  its box gives; hero bytes never travel. Another page is reached by
  turning to it (`set_cards_options` sets page, page size, density and
  aspect), not by reading it off-screen, because the fold goes through
  glosses that only the render goroutine holds. The density and aspect
  buttons and the pager go through the command.
- **Whose write a pane gesture is.** A routed brush or pin is logged as
  the person's operation, while the signals it writes keep the pane as
  their writer, as before. The Live breaker therefore counts them as it
  did; counting a person's brush as a human write is a separate change,
  left for later, as is moving a pin when another writer sets
  `selection_key`.

### 2026-10-05 — the World, Vector field, Network, Graphview and Sankey panes, for an agent

The last five result panes get the read and the command of the two entries
above, and a select command where the pane keeps a pin. Four of them draw
named CTEs on lanes of their own rather than the frame they are handed
(`TabSpec.Frameless`), so their reading opens without a result id: the
frame's result names nothing they drew, and a frame without a result does
not stop them from having drawn.

- **World.** `get_world` reads the last extraction: the column taken for
  countries and the one the fill is shaded by, whether the values had no
  spread and fell back to presence, the legend's range, the rows that
  resolved, repeated or matched nothing, up to twenty of the cells that
  matched nothing (so the spelling can be fixed in SQL), and each country
  with its value and the row that set it. `set_world_options` sets the
  value column by name against the result the pane draws, and the
  projection; the two combos go through it. The pane keeps no pin: a
  country is selected with `set_signal('selection')` and the row the read
  gives. That path does not write `selection_country`, which only the
  pane's own click writes; this is left open.
- **Vector field.** `get_vectorfield` reads what only the pane holds: the
  field's description, the step on display, each step's mean and maximum
  speed inside the settled view, the window and the errors of each phase,
  the sites, and on request the last window statement as a buffer that
  runs on its own. `set_vectorfield_view` (view effect) frames the field
  or a box, shows a step or a time, and sets playback. Its resource holds
  the settled view, the step and playback, not the camera between rests,
  so a pan in progress invalidates nothing. The `vf_*` signals the pane
  writes once the moved view or time rests carry the task as writer, then
  the pane's own again (ADR-0269 §SD8); the lanes they rerun run under the
  task's mark as before. The display options, the time strip's drag and
  "Window query…" stay the person's; the strip is not routed through the
  command, because playback moves the same position on its own and a drag
  moves it every frame. The pane's status line now reaches `list_panes`.
- **Network and Graphview.** `get_network` and `get_graphview` give the
  counts and caps, the lanes' errors, the selection and a page of vertices
  and edges as drawn. Graphview adds each setting together with whether
  the pane or the query's `graph_opts` decided it, the settle state, and
  the encodings the pane could not honour. Its vertices carry up to four
  graph metrics: copied where the pane computed them, computed in the read
  otherwise, on the pane's metric graph, which a rebuild replaces and never
  edits, with an engine of the read's own. The seeded metrics measure from
  the selection as it stands. `set_graphview_options` sets the settings;
  `auto` hands one back to the query, and fit, re-lay-out and settle act
  once. `set_network_options` sets the direction.
  `select_network_node` highlights one vertex and `select_graphview_nodes`
  sets the whole selection (the name `set_signal`'s refusal gave), writing
  `gv_selection` and `selection_key` as the pane would publish them, so
  the pane's next publish finds them in place. A click is routed through
  each: the Graphview widget's selection is taken back after it changes
  and made again through the command, but only when the widget reported a
  select or deselect, so the ids a rebuild drops silently are not logged
  as the person's gesture.
- **Sankey.** `get_sankey` reads the conserved total, which is the outflow
  of the sources rather than the sum of the ribbons, each node's inflow and
  outflow, each ribbon's share, the mode used and why alluvial fell back,
  the unbalanced nodes, and what the build summed, dropped or capped.
  `set_sankey_options` sets the mode only; the fill, gradient and label
  switches change no reading and are not offered. `select_sankey_node`
  pins a node, whose id becomes `selection_key`, or a ribbon, which writes
  an empty key as the pane's click does. A pin used to be a layout index
  that a new diagram could point at another node; it is now carried across
  a re-layout by ids, and a pinned node that leaves the diagram empties
  `selection_key`.

Still open: these panes' CTE lanes carry no confinement label, so the
window label does not yet cover what their reads quote (the 2026-10-05
entry on `list_panes` names the same gap). Vertex positions are not in
`get_graphview`, and the Network pane's layers are not reported.

### 2026-10-05 — the Table, Files, Chat, Map and Experiments panes, for an agent

The remaining panes with a reading of their own get the shape of the three
entries above. The Map and Experiments are tool panes that draw no result,
so their readings, like the frameless panes', open without a result id.

- **Table.** `get_table` reads the sort, the page, the leeway display
  options and raw cells, and each column as the header captions it: its
  leeway handle, its gloss label and media type, or why the gloss is not
  applied. On request it lists the page's rows in the order drawn, each
  cell as its gloss renders it, with the tones that are not neutral and
  NULL cells named apart from empty text. Gloss instances and row-value
  companions are reached only on the render goroutine, so the snapshot
  copies the page once per page, sort and gloss resolution, at most a
  hundred rows of it. The per-attribute grid's exploded rows are not read;
  `sample_rows` reads the record rows. `set_table_options` sets the sort
  by a column's name, handle or label (empty gives back the result's
  order), the page, the page size or the page a record row draws on, raw
  cells and the leeway options. A header click, which cycles the sort, and
  the pager go through it; the option boxes are bound and need no routing.
  Raw cells also governs what Detail, Cards and Chat show, which the
  command's description says.
- **Files.** `get_files` lists a directory of the tree the pane interned,
  or the paths under it a filter matches at any depth, with the row behind
  each entry and the directories the tree made to hold paths no row named.
  The tree is never changed once built, so the snapshot shares it and the
  listing runs in the query. `set_files_options` sets the directory, mode,
  filter, hidden names and order; `select_files_path` selects one path as
  a click does and writes `selection_key`, and `selection` where a row
  named the path. The mode buttons go through the options command. A move,
  a new order or a click made inside the browser is applied by the widget
  first and then made again through the command, which leaves it as it is
  and logs it as the person's.
- **Chat has a read and no command.** `get_chat_pane` reads the folded
  transcript from its tail: time order, names from the roster, replies as
  ordinals, reactions per message, and what the fold left out (rows with
  no time or sender, rows past the cap, reactions naming no message). It
  says whether each optional CTE joined, is absent, running, failed, or
  returned a result that does not fit its contract; that last case was
  painted nowhere, and the status line now names it too. The viewer and
  conversation pickers stay the person's for now; the reading names the
  conversation shown and the others.
- **Map.** `get_map` reads the source, render and colour expression,
  sampling, the raster on screen and its errors, the camera, the selected
  area and the window. `set_map_view` (view effect) centres and zooms or
  frames a box, and asks for one fetch of the new view whether or not the
  person's live switch is on; the `vp_*` signals that fetch writes carry
  the task as writer, as the Vector field's do. `set_map_options` sets the
  time column, render, sampling, ladder, cache and readout, and selects or
  clears the area, which is written at once. The table source and the
  colour expression are SQL fragments the raster template splices in and
  runs on every draw whose viewport moves, long after the task's mark is
  cleared by the person's next edit or Run; checking them at the call
  would not cover those later runs. The command therefore refuses them for
  an agent, and they stay the person's. The render combo, Clear area and a
  drawn box go through the command. The live switch, opacity, basemap and
  device resolution stay the person's.
  The resource holds the settings, the area and the viewport last
  published, not the camera between rests. The Map's status line now
  reaches `list_panes`.
- **Experiments.** `get_experiments` reads the source, the sink, its
  options and the candidate they resolve to, the pane's notice, and the
  sink's output where it is text (the box-drawn tables, the sparks, the
  card-JSON), up to 8 KiB; a sink that draws widgets is reported as a
  picture to capture. `set_experiments` lays options given as text over
  the sink's current ones and resolves the whole through the vizeval
  catalogue, as a launch seed is; the seed's artifact box is a capture
  knob and is not offered. The output is the pane's: it is reported built
  only once the pane has drawn the new candidate.

Still open: the Chat pane's CTE lanes and the Map's raster lane carry no
confinement label in the window's (the gap the entries above name), the
Map's hover readout and its Refresh and Cancel are not operations, and the
person's routed gestures keep the pane as signal writer, as before.

### 2026-10-05 — reads of a statement's structure, the documentation, a row and the glosses

Five reads that belong to no pane's options, and a gloss state per column.
None of them moves a pane; each resolves what it reads itself rather than
quoting what a pane last drew.

- **`sql_flow`** (query) is the Flow pane's two local lenses — the clause
  graph and the column lineage of the SELECT list — for a node of what run
  would ship, or of a statement given as `sql`, with each node's clause
  anchored by byte range in the node's SQL. It parses and runs nothing, so
  it answers under any grant. Untrusted: it quotes the statement.
- **`explain_sql`** (external read) is the pane's remote lenses: the
  server's `EXPLAIN` — syntax tree, plan, pipeline, read estimate or index
  use — of a node fused with the CTEs it reads, as lines and, on request,
  as the graph the pane draws. The wrap is applied to the wire body only,
  as the pane's lanes do, so the statement routes and rewrites as itself.
  An agent's call is checked as a run of the explained statement is
  (§SD2), with the destination it lacks named, before anything is sent,
  and the request carries the call's context. The node's signal reads go
  on the URL as on a run. The outcome is labelled confined when the
  statement reads confined data. An endpoint without `EXPLAIN` (the host's
  introspection plane) is a refusal that says so.
- **`lookup_docs`** (external read) reads one name's documentation through
  the window's documentation source: the kinds it carries, one kind's body
  cut at 6 KiB with the headings it lost, and the body's SQL blocks ready
  for `set_sql`. With no name it reads what the Docs pane shows. The pane's
  lookup polls through a single-slot lane, so a source answers an agent
  through an optional `DocsLookupNowI` beside `DocsSourceI`; a re-user's
  source without it leaves the operation refused. Untrusted: the bodies of
  SQL user-defined functions are `create_query`, written by whoever has DDL
  on the endpoint.
- **`get_detail`** (query) reads one row as the Detail pane does — the
  selection's row of the node the pane follows, or a named row or node:
  its sections in card order with each attribute's values and its
  memberships split by role, its canonform digest and canonwire
  fingerprint, and its temporal attributes as the time strip draws them; a
  result that is not leeway-shaped reads as the pane's ad-hoc groups. The
  read rebuilds the leeway reading from the result's column names
  (`discoverCardRecipe`, shared with the pane's card driver) and
  drives one row into a sink of its own off the render goroutine, so the
  pane's buffered card is not touched. Values are the driver's text,
  before glosses; typed components are not read (their readers are the
  pane's). A window whose embedder replaced the pane's body says so.
- **`list_glosses`** (query) is the Glosses pane's catalog: each gloss's
  media type, doc, accepted kinds, parameters with their closed values,
  affinities, a sample face, and the alias, directive and call spellings
  that declare it; then the host's rule sets. It is the build's own text
  and is not marked untrusted.
- **`describe_result`** gives each column its gloss as the window resolved
  it — the token, what bound it, and whether it is applied, refused, or
  not applied to the column's values, with why. The window resolves glosses
  for one schema at a time, the one a pane last drew through them; for any
  other result the description says so instead of guessing.

**The documentation lookup's two paths are meant to differ.** The update
of 2026-10-04 exempts the Docs pane's lane from the window's mark, because
the person's reading is the person's work and the looked-up name enters
only as a bound parameter. `lookup_docs` is a call an agent makes, which
§SD2 and ADR-0269 §SD6 count as agent-caused work reaching the endpoint;
it is held to the same rule as `list_tables`: the endpoint must be among
the grant's destinations, and the refusal names it. So under a grant that
lists only `keelson(…)` tables the person's Docs pane keeps working and an
agent's `lookup_docs` is refused. Exempting a fixed, parameter-bound probe
for agents was weighed and not taken: the probe still sends a request on
the agent's behalf to a host the grant does not name, and the remedy —
asking for the endpoint — is one call.

### 2026-10-05 — completion, the endpoint's functions, the parameters' controls, the query graph and the history

The remaining reads the audit named, and one command:

- **`complete_sql`** (query) is the Completion pane's engine (ADR-0190) at
  a byte offset of a statement the caller names — its own draft, or the
  buffer — rather than at the person's caret: the domain, the candidates
  extending what is typed (at most 100), the match state and the silent
  reason. The engine is built per call over the in-process registries and
  over a copy of what the pane's catalog probes and the vocabulary probe
  have already landed. It starts no probe: a table, column or setting
  position no pane has asked about is listed in `NotReady`, and
  `list_tables` and `describe_table` read the catalog under the grant.
  Untrusted, since catalog candidates are the endpoint's text.
- **`validate_sql` (version 3)** adds `Literals`: string literals at an
  argument whose domain the build knows whole — component kinds and
  fields, sections, channels, aspects, glosses and gloss keys,
  introspection tables — that name no member of it, each with
  candidates. They are the editor's red underlines, and they make the
  statement not valid. The check needs no endpoint.
- **`endpoint_functions`** (external read) asks the endpoint itself which
  user-defined functions it carries, under the same rule as `list_tables`:
  the endpoint must be among the grant's destinations. It returns the
  declared server functions the endpoint lacks, the client macros whose
  expansion would fail, the surface revision with the skew line the
  Vocabulary pane shows, and the undeclared and withdrawn names — names
  only, never bodies. It is kept apart from `list_functions` so that the
  endpoint's names do not make every vocabulary lookup untrusted. The
  pane's cached probe is left alone, so the two can disagree for a moment.
  Nothing is provisioned.
- **`get_state` (version 2)** reports each parameter as the parameter
  block draws it (ADR-0274 §SD3): its control (text, enum, range, datetime
  pair, expr), the other half of a folded pair, an enum's options, whether
  a run still needs it, and the prelude value Reset restores with whether
  it moved; the block's near-miss note; each signal the Signals section
  lists, including names the buffer reads that nothing holds, with its
  types, a type conflict, SET shadowing, unfilled, its revision, the pane
  that publishes it and `set_signal`'s refusal for it; and for the result,
  what the status line says in place of its summary (a refused run, the
  write gate, Live switched off by its breaker), the progress of a run in
  flight, and the main result's truncation. For a buffer of several
  statements it says which one `run` ships. Reset stays off the catalog:
  it starts a run outside `run`'s checks, and the defaults are now there
  for an agent to `set_param` back and run.
- **`set_param` (version 2)** refuses an agent's value an enum does not
  offer, naming the options. The person's field is not checked.
- **`get_query_graph`** (query) reads the last run's split — each node's
  kind, what it depends on, the parameters it reads and which of them a
  SET line pins, the channels it can also fill, the panes it feeds and its
  SQL cut at 2 KiB — with the observed node, the bindings, and the system
  graph's edges as text. Untrusted: node SQL quotes the buffer.
- **`observe_node`** (command, document effect, writes `panes`) is the
  Graph pane's "observe in panels": every panel without a binding draws
  the node's result. The button goes through it (§SD6). A node the split
  lacks is a conflict, as for `bind_pane`. For an agent, the statement the
  node's lane would send is checked at the call against the class ceiling
  and the agent limits, as `run` checks the buffer, and the window takes
  the task's mark: a node the sink never reads would otherwise reach the
  endpoint only through the observe. `bind_pane` has the same gap and
  keeps it for now.
- **`list_history`** (query) reads this window's runs newest first: when,
  rows or error, the SQL each shipped, the buffer it came from when that
  was more, and the signal values it sent. Restoring one stays `set_sql`
  and `set_signal`. The durable captured runs are not read.

Not done: `endpoint_functions` reaches the endpoint through the schema
reads' one-off request, which carries no on-behalf-of context of its own;
and `complete_sql` parses the scope over the whole text it is given, so a
buffer of several statements completes with the site alone.

### 2026-10-05 — stopping work, one statement, publishing a projection

An agent that started a long scan or a projection over the wrong features
could only wait; the person had Cancel. And with several statements in the
buffer, `run` shipped whichever one the person's caret sat in.

**Convention for stopping work.** Each stop is a command of play's own, and
a task stops only work it caused. ADR-0269 §SD3 has the host's `cancel`
stop work a job handle names, which would serve every lane at once; the
host's service withdraws queued calls and proposals only, so a job handle
on `run` would need the dispatcher to learn about play's lanes. The main
lane now records the on-behalf-of context of the run in flight, and the
Projection pane the task whose `compute_projection` asked for its run. A
Live rerun after the task's input carries its context, so it counts as the
task's. The person's Cancel stops either, whoever started it, and goes
through the command (§SD6), so a task whose run the person stopped pauses.

- **`cancel_run`** (command, run effect, writes `result`) stops the main
  run in flight, or withdraws a run the task asked for that has not started
  yet. A run the person or another task started is refused. `get_state`'s
  result gains `run_by` (person, or `task:<id>`) while a run is in flight.
- **`cancel_projection`** (command, view effect like `compute_projection`,
  writes `projection`) does the same for the Projection pane, and withdraws
  a compute the pane has not started. `get_projection` (version 2) gains
  `run_by`.
- **`run` (version 2)** takes `statement`: statement *n* of the buffer with
  its SET prelude, as a Run with the caret in it ships. The checks at the
  call — unfilled inputs, the class ceiling, the agent limits — judge that
  text, not the whole buffer, so a buffer whose other statement writes no
  longer blocks a read. `statement` with `subquery` is refused, since one
  picks by the caret and the other without it. A statement the buffer no
  longer holds when the run starts lands in the status line.
- **`publish_projection`** (command, consequential, writes `datasets`) is
  the pane's "publish as dataset": both datasets onto the bus and bound in
  this window as `keelson('projection')` and `keelson('projection_rules')`.
  It is consequential because the datasets leave the window, so the person
  confirms each one. For an agent the scaffold query is not inserted at the
  person's caret; `get_projection` reports the publish in flight, the
  handles and row counts once it lands, or why it failed. It refuses until
  the run's layout has been drawn, since the positions live in the widget.
- **`delete_signal`** (command, document effect, writes `signals`) is the
  Signals section's ×, which goes through it. An agent's delete of a pane
  output is refused as `set_signal` refuses it, and so is a name nothing
  holds. This moves "deleting a signal" out of §SD6's list of gestures no
  command covers.
- **`set_run_options`** (command, document effect, writes a new resource
  `run_options`) turns the conditions rewrite (ADR-0121) on or off, where
  the endpoint offers it. `get_state` reports it as `conditions`. The
  checkbox is a bound value, so the write-back credits it to the person.
- **`list_panes`** rows gain `ops`: the operations that read and drive the
  pane, its `get_<pane>` first. The summary says so, so an agent goes from
  a pane to its reading without a second catalog search.

Not offered: `set_param_tier`, `set_timeline_bands` (it authors SQL that
runs, and would need `run`'s checks and the editing conflict),
`get_model_result` (the Model tab's text carries no label for what its
prompt read), `list_recorded_runs` (an external read of captured texts,
some confined), and a stop for the Vector field's describe and summary
lanes. The Vector field would follow the same convention.

### 2026-10-06 — review corrections to the pane operations

- `validate_sql` and `trace_rewrite` are marked Untrusted: given no
  statement, both quote the person's buffer back, as `get_query_graph` and
  `sql_flow` do.
- `select_icicle_frame` takes a frame number beside the path, and
  `get_icicle` lists each frame's number. The person's click pins by
  number, so a sibling whose label agrees no longer takes the pin; a path
  given with the number must still lead to it.
- A vertex id given to `select_network_node` or `select_graphview_nodes`
  matches a whole id first; a cut id counts only when one id cuts to it.
- `get_chat_pane` lists at most 100 participants and 50 conversations and
  counts the rest; the graph and Sankey pages also stop at about 24 KiB of
  text, with their More counts saying what is past them.

### 2026-10-07 — The Experiments pane's operations go; the Projection pane reads its archetypes

`get_experiments` and `set_experiments` are removed with the Experiments pane
([ADR-0289](./0289-leeway-rows-for-readers-canonical-forms-a-read-model-and-kinds-in-projection.md) §SD6).
The Projection pane gains `get_archetypes`, a query over its run (ADR-0289
§SD4): per cluster its rule, the attributes every row holds with one value,
what it typically holds, its extremes, and the exception rows with their
departures and result rows, under the operations' byte bound. Its views —
graph, archetypes, rows, row — are a display setting, like colour-by, and have
no command. `compute_projection`'s defaults are the panel's new ones:
structure features and a minimum cluster of 5 (ADR-0289 §SD5).

The Detail pane gains `get_canonical` (ADR-0289 §SD2): a row's canonform
digest and pin, its canonwire fingerprint and verdict, and the CBOR items both
were taken over in diagnostic notation, cut at a line boundary under the byte
bound. `get_detail` (version 2) reads the row through the read model
(ADR-0289 §SD3): one list of attributes, each named by its first membership
through the session's registries or by its plain column, with its values
spelled once, its further memberships as labels and the `LW_GET` handle that
reads it; columns the card hides as machine-readable only are counted. The
sections-with-roles shape of version 1 is gone.

## References

- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — the contract this catalog serves.
- [ADR-0097](./0097-play-reactive-query-graph.md) — play's query graph and signal store.
- [ADR-0115](./0115-query-observability-data-plane-strategy.md) — result pinning, and its removal.
- [ADR-0141](./0141-play-endpoint-dispatch-seam.md) — the dispatch seam the run limits and play's own writes sit behind.
- [ADR-0145](./0145-sealed-app-data.md) — the confined label.
- [ADR-0163](./0163-play-timeseries-workbench.md) — Series adjudication and `tslabels`.
- [ADR-0181](./0181-leeway-dql-authoring-surface.md) — `readonly = 2` on play's read path.
