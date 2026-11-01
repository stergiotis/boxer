---
type: adr
status: proposed
date: 2026-10-31
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0350: A trail viewer — one system app over the trail marts, reading by case and time window, with a verdict line, a previous-state diff and review marks; and the SQL surface it reads

## Context

The trail on `boxer.facts` records every kind of event the runtime
produces — bus requests, egress fetches, publishes, coverage, audit
events in any domain, and on the agentic rows model calls, messages,
actions and grants — under one `Origin` per row, with `Conversation`,
`Delegation` and `Cause` where they apply
([ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md),
[ADR-0296](./0296-audit-events-as-a-trail-component.md),
[ADR-0330](./0330-a-shared-audit-recorder-below-the-runtime-one-sealed-writer-over-a-narrow-store-binding.md)).
The journal is hash-chained and anchorable, and the verifier's vocabulary
— holds, broken at an ordinal for a reason, gap, fork, unanchored — is
served in-process as `keelson('evidence_checks')` and `keelson('anchors')`
([ADR-0327](./0327-evidence-as-a-record-store-profile-chain-vault-marks-seals-and-anchors-any-store-opts-into.md)).
The marts under `trailviews` make every kind readable as columns: one
`dm_trail_*` view per kind, the `dm_trail_timeline` over all of them, and
three agentic aggregates.

Nothing shows this to a person as a trail. Each app shows one kind: the
log viewer log rows, play query runs, chat its own conversation's grants
from in-process rings, the app center a fold of bus requests. A reviewer
who wants everything that shares a run, a window, a subject, a rule or a
task reads it in play over SQL, if at all. The background survey
[audit-trail-visualization-survey](../adr-background-work/audit-trail-visualization-survey.md)
(compiled 2026-10-30) read what regulated review requires, what the
human-factors studies say reviewers do, what the visualization and tracing
literatures scale, what shipped audit logs converged on, and what this tree
has; its §8 maps five shapes onto the tree and its §9 lists eighteen forks.
This record settles them.

Three findings of that survey carry the most weight here. The *form* of a
trail is itself a requirement in the GxP texts and NIST AU-7: sort, search
and export, never a flat file; review is by exception where a validated
rule justifies it, and the review leaves evidence. The reviewer's
bottleneck is context acquisition, not reading: analysts prefer a dense
table, and distrust a highlight that does not show its rule. And the forms
that scale are those that aggregate to the display; per-event forms — a
span tree, a provenance graph, a record diff — serve a selected key only.

Two decisions of 2026-10-31 shape the data this viewer reads. State-changing
audit events follow one convention — an entity reference, the full state
after the event, an optional `from` claim — so the regulated "old and new
value" is the previous event for the same entity, derived at read time
(ADR-0296 Updates). And the recorder writes a `config` event at start and a
`ditch` event for the one mutation the trail performs on itself.

## Design space (QOC)

The survey's §8.2 states the five shapes with their kill-reasons and §8.3
scores them against ten criteria; the table is not repeated. In one line
each: **O1** a system app over the marts; **O2** a pane in chat, which
reaches only rows that carry `Conversation`; **O3** SQL through play, which
has no verdict, no diff and no exception list; **O4** a widget set hosted by
several apps, whose long pole gates every host; **O5** a review ledger alone,
which answers neither reconstruction nor accounting. O1 and O5 compose, and
O2 is O1 opened from chat on one key.

## Decision

We build **one system app, the trail viewer**, under the operations contract
of [ADR-0345](./0345-system-apps-join-the-operations-contract-the-log-viewer-first.md),
reading the trail marts by **case** and **time window** into a windowed
table with a detail pane, a **previous-state diff** derived from the
convention above, a **verdict line** per writer run, a named **exception
list**, and **review marks** written back to the trail as audit events in a
`review` domain. Chat opens it on a conversation; play's marts stay the SQL
route. The widgets it needs that do not exist are built as text first and
painted later. A launch config names the case, and optionally a window, a
tab and a saved view; one the viewer cannot read is ignored with a
diagnostic in the window rather than failing the mount, as the app
center's is.

### SD1 — A case is a correlation key, and the set is fixed here

The viewer's unit of work is a case: one key kind and one value, read over
one time window. The kinds are those the timeline's context columns and the
audit event's references already carry:

| Case | Key | Rows it selects |
| --- | --- | --- |
| run | `run` | every kind of that writer run |
| window | `(run, instance)` | every kind of one window |
| app | `app` | every kind one app wrote, across runs |
| conversation | `conversation` | the agentic rows of one conversation, and audit events that reference it |
| task | `task` | an action and everything it caused, by `(task, call)` |
| subject | `AuditEvent.Subject` | audit events about one data subject |
| principal | `AuditEvent.Principal` | audit events one principal caused |
| entity | one `ref-type:ref-value` | audit events about one entity: a bus subject, a rule, an attention item, a downstream consumer's record |
| destination | `HttpFetch` destination | egress to one host |
| all | — | every row in the window, for accounting |

Every case is narrowed by the time window first; a read is never
unbounded. The set is extended by a dated entry here, not by the viewer
reading mart columns at run time, so a scene and a catalog argument name
a case the same way. A downstream domain that wants a case of its own asks
for the `entity` kind with its reference type.

### SD2 — The SQL surface: three functions, one column, two views

The viewer reads the marts and nothing under them; what it needs that the
marts do not give, the marts gain, so play and a downstream reader see the
same shape.

- **Functions.** `TRAIL_ATTR(keys, values, key)` returns the attribute
  value at `key` or `''`; `TRAIL_REF(types, values, type)` the first
  reference value of `type` or `''`; `TRAIL_REFS(types, values, type)`
  all of them. They are plain array functions over the parallel columns
  `AuditEvent` stores, installed as SQL UDFs by `trailviews` ahead of the
  views in the same statement list, so a view may use them and a reader in
  play may too. `TRAIL_VIEWS_VERSION()` returns `ViewsVersion`, as
  `LW_SURFACE_VERSION()` does for the read surface, so one `SELECT` tells
  a reader which trail revision a server carries. `leeway sqlsurface
  status` does not report it: its reconcile lists only the `LW_`
  namespace and the surface's own views, so the `TRAIL_` names are never
  reported as undeclared, nor as missing, and need no declaration; a stale
  trail view is found by its stamp, as before.
  Play's vocabulary panel lists every non-system function an endpoint
  carries, so play registers the four `TRAIL_` names as a server family,
  *trail views*, from the roster `trailviews.Functions` exports, and an
  endpoint with the trail views lists them under it rather than as
  undeclared. `sqlvocab.Function` cannot mark a family optional per
  endpoint, so an endpoint without the trail views reports them missing,
  because that is a true statement about the endpoint.
- **One column on `dm_trail_audit_events`.** `entity`: the first
  reference as `type:value`, or `''`. The convention of ADR-0296's
  2026-10-31 entry makes the first reference the entity a state-changing
  event changed, and later references its context, so the column is the
  key SD1's entity case and SD5's previous-state read select on; `from` and `from-by` are read with
  `TRAIL_ATTR` where needed and are not columns.
- **`agg_trail_audit_entities`.** One row per `(domain, entity)`: first
  and last timestamp, event count, the last action and outcome, the last
  attribute keys and values, and `facts-ids` — the current state of every
  entity the trail knows, with lineage, as ADR-0051's aggregate class asks.
- **`dm_trail_reviews`.** The `review` domain's rows (SD7) with the marked
  row's id, the mark, the reason, the rule that raised it, and the
  reviewer's principal and principal-by, 1:1 with facts rows.

`ViewsVersion` moves, the goldens regenerate, and `keelsonddl --trail-views`
prints the functions with the views. Nothing is a materialized view and no
index is added: the correlation-index question belongs to the storage
decision ([facts-storage-table-switch-options](../adr-background-work/facts-storage-table-switch-options.md)),
and the viewer's own stamped queries are what will show its cost.

### SD3 — Reads: a host-side probe, stamped, bounded, off the render thread

The viewer is a runtime app in the host process and reads as the log viewer
does: through a Go probe over the facts store's client, not over the bus.
The probe composes SQL over the marts from a case, a window, the filters
and a keyset cursor `(ts, id)`, newest first, under a row cap; every
statement carries a `log_comment` stamp as play's do, so the viewer's reads
land as `QueryRun` rows and the viewer appears in the trail it shows.

A `bgjob` worker runs the read; a frame adopts a finished result and never
waits. The table is refilled when the case, window or filters change and
on an interval the window sets, and extended at the end by the cursor when
the person scrolls past what is held. Selection is by facts id and survives
a refill; the cells key folds the window and cursor, so a replaced record
replays nothing stale. The pattern lives in the viewer's package; it moves
to a shared package when a second host asks for it, not before.

### SD4 — The surface: one overview for every kind, tabs where a chain exists

The window is a case selector and the time range picker
([ADR-0016](./0016-imzero2-time-range-picker.md)) over four regions:

- **A strip** of counts per time bucket over the window, coloured by kind
  and switchable to outcome or status, fed by one `GROUP BY
  toStartOfInterval` over `dm_trail_timeline`. A click on a bucket narrows
  the window to it. With more keys than the qualitative cycle's seven hues,
  the strip draws the five busiest and sums the rest as *other*. Beside the strip, one text line states what it shows —
  rows, buckets, the busiest bucket — and is what a scene asserts on.
- **The table**, a `recordtable` over the timeline's shape — time in the
  picker's zone, kind, headline, status, subject, origin, the agentic
  columns when the case is agentic — with sort, full-row select and
  grouping by `parent-ref` under
  [ADR-0336](./0336-recordtable-row-grouping-a-line-sequence-over-the-permutation.md),
  which gives the intent-level view the agent-trace tools call a
  trajectory. A leading column carries the row's verdict mark (SD6) and a
  second its exception marks (SD7), each as text with a tone.
- **The detail pane**: the row's columns through `fieldview` (the marts
  already decode the components, so `componentview` is not needed), bodies only where kept and the app's grant admits them,
  the identity chain as one breadcrumb line — person › window › task ›
  model call › tool call — built from `Origin`, `Delegation` and `Cause`,
  and the previous-state diff (SD5) for audit events.
- **A facets pane** beside the table: for each faceted column — kind,
  status or outcome, app, domain and action, principal — the values in the
  case and window with their counts, from one `GROUP BY` per column. A
  click on a value includes it, a modified click excludes it, and the
  pane is the filter's visible form: what the table is narrowed by is
  always on screen. This is the histogram, table and facets bound to one
  query that the surveyed log explorers share.
- **Context** for the selected row: the rows immediately before and after
  it in the same writer run, regardless of the filter, in a sub-pane of
  the detail — the "surrounding documents" every surveyed product offers,
  for the case where the filter hid what explains a row.
- **A tree tab**, present only when the case carries a chain (task,
  conversation): the `tree` widget over `parent-ref`, each node its
  headline, elapsed and status as text. It is assertable by a scene. A row
  whose parent is not among the rows held is a root, and the selection is
  the table's. A
  painted span tree with lanes and connectors is deferred (SD10).

What the survey found about overviews is followed: a dense table with
graphics as overlays, not a graphic with a table behind it.

### SD5 — The previous-state diff

For an audit event with an `entity`, the viewer reads the previous event
of the same `(domain, entity)` — `(ts, id)` below the row's, newest first,
one row — within a look-back the window sets, and renders a three-column
table: field, before, after, with added, removed and changed marked in
text and tone. Its verdict on the `from` claim is one of: *matches the
previous event*; *differs from the previous event: …*; *no previous event
within the look-back*, with `from-by` shown when the row carries it; *a
gap row lies between*, when a `trail` domain `audit-gap` row of the same
writer run falls between the two timestamps. The claim matches when it
equals the previous event's value of a field the row changed, or, for a
claim that renders several fields as one line, contains each such value; a
row with a previous event and no `from` states no claim and gets no
verdict. For an `AgentGrant` the same
pane diffs consecutive rows of the task. Nothing is computed from code
defaults: where the baseline is a default the row says so, and the viewer
repeats it.

**Compare.** Two rows the person selects in the table, of any kind, are
diffed with the same field table over the timeline's columns and, where
both are audit events, their attributes. The base is stated in words
above the table — the older row on the left — as the surveyed compare
pages do, never as a toggle. The second row is the `compare` resource of
SD8, a held row's id or none; `set_tab` reaches the compare tab.

**As of.** For the entity case, the detail pane shows the entity's state
as of the window's end: in each domain the entity has events in, the last
event at or before that instant, read from `dm_trail_audit_events` bounded
by `ts`, which is what `agg_trail_audit_entities` folds per
`(domain, entity)` without the bound. The case names no domain, and one
entity string in two domains is two states. The pane says, per domain,
which event the state comes from and when.

### SD6 — The verdict line

For each writer run the case touches, one line: **holds**; **broken at
ordinal N: reason**, with the break reason's name spelled out; **gap: K
rows no seal names**; **fork: two seals follow one**; **unanchored since
ordinal N**. A run nothing could verify — the trail not durable, the
verification failed — reads **unverified** with the reason, a sixth state
the vocabulary carries. The current run's lines come from `keelson('evidence_checks')`
and `keelson('anchors')` — the anchors as the relation's L32 row reads
them, so the viewer does not read `keelson('anchors')` beside it; another
run's from the recorder's seal verification for that run, read once per
refill. The vocabulary — state,
label text, tone, the law it comes from — is one small package the chat
and the app center may adopt. An unverified state marks the rows it
covers and does not hide them: blocking would hide evidence, and a passive
badge is unseen, so the mark is the table's first column and the line sits
above the table.

### SD7 — Exceptions and review marks

**Exceptions** are named rules the viewer ships as data — a name, a
description, and a SQL predicate over the marts — evaluated for the case
and window and listed with the rule that raised each row: a denied, failed,
timeout or error outcome; a refused model call; a grant widening; an
`audit-gap`, `audit-invalid`, `config` or `ditch` row of the `trail`
domain; a row a seal does not name or a run whose chain is broken; a
`(domain, action)` pair not seen in the look-back before the window; a
`from` claim that differs from the previous event. The rules are the
viewer's, not laws and not moderator rules: they produce a reading list,
not a verdict and not an action. Each rule is visible in the window with
its predicate, since a highlight whose rule is hidden is distrusted.
The rules run over the rows the window holds — its case, window and
filter, under the row cap — not over every row of the window. A run whose
line reads broken, fork or gap raises every held row of the run, since the
verdict names no rows. The `from` claim rule makes the previous-state read
for at most sixteen new rows per refill and remembers what it judged.

**Review marks** are audit events in the `review` domain: actions
`reviewed`, `flagged` and `dismissed`; one reference `trail-row:<id>`; a
`reason` attribute, and `rule` when the mark clears an exception. The
viewer writes them through the host's recorder under the person's
principal, so they sit on the same sealed trail as what they review and
`dm_trail_reviews` lists them. A marked exception leaves the list and shows
its mark in the table. A mark clears every exception of its row, and its
`rule` attribute names them all, comma-separated. The person's principal is
a pseudonymous reference, a hash of the OS user and host names, with
principal-by `os`.

A dismissal requires a reason; reviewed and flagged do not, since the
mark itself is the record.

The mark is the one command of a system app whose effect is not `view`: it
writes outside the window, to the trail, which is adjudication, so its
effect is **consequential** and it is **not offered to agents** — review is
the person's act, and the regulated texts want it so. This is the record
ADR-0345 §SD6 says such a command needs; ADR-0345 gains a dated entry
saying so.

### SD8 — The catalog

Everything the person can see is a query; everything the person can
change is a command, and the control routes through it
([ADR-0345](./0345-system-apps-join-the-operations-contract-the-log-viewer-first.md)
§SD3). Two rules hold throughout. A **query selects by its own
arguments** — the case, range and filter it is about — answers from the
cache the snapshot took with the frame, never from the store or the
window's state in the arguments' place, is bounded, and says when its
arguments name a read the window did not make; it is `Untrusted` where
row-derived text is returned. A **command sets and never toggles**.

Queries: `list_rows`, `get_row`, `get_previous` (SD5's read for the
selected row), `get_context` (the rows around the selected row, from the
same read), `get_compare` (SD5's field table for two held rows),
`get_facets` (one facet's values and counts, over the case and range
whole as the pane counts them), `get_strip` (the buckets under the
colouring set), `get_verdict` (SD6), `get_exceptions` (SD7), `get_tree`
(SD4), `get_matrix`, `get_dotted`, `get_dayhour`, `get_series` (SD11's
aggregates: each answers from the view's last read, made while its tab is
open, for arguments naming exactly the case, range and filter the window
read and the picks the window has, since the view aggregates that read
whole), and `get_view`, which reports every resource's value and the
saved views' names.

Commands of effect `view`: `set_case`, `set_range`, `set_filter`,
`select_row`, `set_tab`, `set_strip`, `set_grouping`, `set_compare` (a
held row's id, 0 for none; the arming of the pick is the window's own),
`set_matrix`, `set_dotted`, `set_series`, `set_side`, `set_tree`
(expand or collapse every node; the resource is the value last set, since
the widget's expansion is per node), `refresh` (writes `read`, which
lands with a later frame) and `open_view` (writes case, range, filter and
tab). Commands of effect `document`, since the saved views are authored
state in the persist store: `save_view`, `delete_view`. Two commands of
effect `consequential`: `send_to_play`, which publishes outside the app
and is offered to agents under the person's confirmation on every call,
as play's publish and watchbill's cancel are; and `mark_row`, person-only
(SD7) — review is the person's act, which is a different reason from the
effect. Resources: `case`, `range`, `filter`, `selection`, `tab`, `strip`,
`grouping`, `compare`, `matrix`, `dotted`, `series`, `side`, `tree`,
`read`, `views`.

`tab` is the window's tab: one of the detail pane's — fields, previous,
context, compare — under which the central area shows the table, or an
analytic view of SD4 and SD11 — tree, matrix, dotted, dayhour, series —
which takes the table's place while the detail pane shows the fields; so
the tree is a value of the one resource, not a second one.

### SD9 — Labels, time, bodies, export, old rows

- **Labels.** The pane carries the highest tier of the rows it draws and
  says so; `confined` and `tainted` are drawn as the unenforced flags the
  views pass through. The agent schema is unchanged: the catalog quotes
  nothing a model's principal could not read in play.
- **Time.** The picker's zone applies to `ts`. Where a row has a second
  time — the journal's `At`, a query run's `event_us`, and once the
  storage decision lands an insertion time — the detail pane shows it,
  labelled; where it has none, the pane says the trail records one time.
- **Bodies.** Three states read differently: *not kept* (the size, and
  that retention was off); *ditched*, with the `ditch` event's time when
  one is in range; *erased*, from the journal's mark.
- **Export.** The table publishes to play as an ad-hoc dataset
  ([ADR-0134](./0134-adhoc-datasets.md)), which keeps sort and search and
  is itself a trail row; *Send to play* publishes the table's record under
  the window's own alias and opens a play window reading it, as chat's
  Analytics does. No file export.
- **Rows of the retired `audit` kind**, written before 2026-10-30, are
  not read.

### SD10 — Deferred, with triggers

- A **painted span tree** with caller-fixed lanes and parent→child
  connectors, on `timeline`'s axis and brush or as a sibling widget; its
  trigger is the text tree of SD4 proving too slow to read on a task of
  hundreds of rows, measured by a scene.
- An **accessibility projection** for painted canvases, so a scene reads
  the strip; until then the strip's text line is the assertion target.
- A **description registry** for downstream domains' action names; until
  a consumer asks, names are shown as written beside outcome and subject.
- **Agents marking rows.** Reopened only with a reason the person cannot
  supply themselves.
- The **shared windowed-fetch package**, when a second host wants it.
- A **driver stamp** so the viewer separates a scene's run from a
  person's: [ADR-0344](./0344-agent-caused-jobs-in-a-process-of-their-own-and-the-entry-frame-every-stack-carries.md)
  owns it.
- **Alignment on a milestone** — the first denied request, the grant, a
  launch — with the before and after mirrored, as EventFlow describes;
  its trigger is a reviewer asking "what happens after X" across many
  cases, which the dotted chart of SD11 does not answer.
- **Sankey of actor → object → outcome**, the **directly-follows graph**,
  the **flame graph**, a **trace diff** of two cases and **session
  replay**: each is legible only after category coarsening or serves one
  selected pair, and none was asked for. The `sankey`, `layeredgraph` and
  `icicle` widgets exist, so the trigger is a request, not a widget.

### SD11 — Analytic views, each an aggregate into a widget that exists

Beyond the strip, the survey's scalable forms are aggregates over the
marts drawn with widgets the tree has; each is a tab of the window with a
text summary line a scene asserts on, and none reads a row the table does
not.

| View | Question | Aggregate | Widget |
| --- | --- | --- | --- |
| actor × object matrix | who touched what: principal, app or window against subject, entity or operation, with counts and the last outcome | one `GROUP BY` pair over the case and window | `recordtable`, sortable both ways |
| dotted chart | when each case was active: a row per run, window, conversation or entity, time on x, a mark per bucket with a count | `GROUP BY (case, bucket)` | `implot` scatter, size by count, binned at the display's resolution |
| day × hour | off-hours activity | `GROUP BY (weekday, hour)` | a 7 × 24 `recordtable` with cell tint by count and the count as text |
| numeric series | tokens, latency, elapsed, bytes over time | `GROUP BY bucket` with `quantileTDigest` and sums | `implot` lines, one point per bucket of the strip's step |
| saved views | return to a case, window, filter and tab by name | the persist store | the window's own list; a launch config names one |

The matrix follows the survey's finding that matrices beat node-link
diagrams past about twenty nodes; the dotted chart is the one per-event
form that needs no cause chain and degrades to a density. Each view
states its bucket size and whether a bucket is saturated.
A principal, a data subject and an entity are an audit event's own
columns, so a matrix with one of them on an axis reads the audit-event
mart and counts the case's audit events only, and says so; *operation* is
the timeline's subject, `domain.action` for an audit event. The dotted
chart bins at the strip's bucket size rather than the display's, so the
two read the same buckets, and sizes its marks in four steps by the
square root of the count.
The day × hour grid groups in the picker's zone (SD9) and tints each cell
through `recordtable`'s cell fill from the design system's sequential
palette, the count staying as text, so colour is never the only channel;
its line states the share of rows outside Monday to Friday 08:00–18:00.
The numeric series is not decimated: its buckets are chosen from the
strip's width, so they never outnumber the pixels, and a selection-based
reducer such as LTTB is the thing play's series panel rejects because it
can drop the one narrow extreme a reader is looking for.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `trailviews` views and `ViewsVersion` | added: `TRAIL_ATTR`, `TRAIL_REF`, `TRAIL_REFS`, `TRAIL_VIEWS_VERSION`, the `entity` column, `agg_trail_audit_entities`, `dm_trail_reviews` | the views golden; deployed servers re-run the views half of `keelsonddl` or a surface install |
| `audit` package constants | added: `ReviewDomain`, its three actions, the `trail-row` reference type, the `reason` and `rule` attribute keys | nothing; values, not memberships |
| Exported Go API under `public/`: `keelson/runtime/trailviewer` | added: the app, its launch config codec, its catalog argument and result types, the verdict vocabulary package | `keelson('app_operations')` gains its rows |
| ADR-0345 | a dated entry: one system-app command with effect `consequential`, person-only | — |
| chat | an *Open in trail viewer* on a conversation in the Analytics tab, launching the viewer with a case (the header's *Open trail*, which opens play on the timeline, predates it and stays) | — |
| keelson vocabulary (`vdd`) | added: the `tvLaunch` cohort, the launch config's seven memberships | the assignments golden |
| env-var registry (ADR-0009) | added: `BOXER_TRAILVIEWER_REFRESH`, `BOXER_TRAILVIEWER_ROWS`, `BOXER_TRAILVIEWER_LOOKBACK` | `doc/env-vars.md` regenerates |
| persist store | added: the viewer's saved views, one JSON document under its app id; two windows saving at once keep the later write | nothing; app-owned entries |

## Alternatives

- **A pane in chat (O2).** Rejected as the shape: it reaches only rows with
  `Conversation`, so never a bus request, a log, a downstream domain, a
  period's account; and chat's render code already writes coordinator
  state, which the harness wants kept narrow. Kept as the entry point.
- **SQL through play alone (O3).** Rejected as the shape: no verdict, no
  previous-state diff, no exception list, and pivoting by composing SQL is
  what every surveyed product avoids. Kept as the route a reviewer with
  the full query language takes, and fed by SD2.
- **A widget set first (O4).** Rejected: the long pole gates every host,
  against the repository's "descope rather than gate".
- **A review ledger alone (O5).** Rejected as the shape, adopted as a pane:
  it answers neither reconstruction nor accounting.
- **Old and new value columns on `AuditEvent`.** Rejected: the store is
  append-only and the previous event is a retained record; a second copy
  of it would be a claim beside a fact. The `from` attribute is the claim
  the convention allows, and SD5 checks it.
- **Window-function views for the previous state.** Rejected for now: a
  `lagInFrame` over the whole table in a `VIEW` either scans everything or
  changes meaning under predicate pushdown; the point read of SD5 on the
  selected row is the per-event form the survey classifies it as.
- **Reads over the bus with `clickhouse:<host>`.** Rejected for a runtime
  app in the host process: the log viewer's probe is the precedent, and the
  stamp lands the queries in the trail either way.
- **Blocking the detail pane on an unverified run.** Rejected: it hides the
  evidence a reviewer needs to see what broke.
- **Rules as laws or moderator rules.** Rejected: a law judges the record,
  a moderator rule acts on a window; an exception rule produces a reading
  list and must be visible beside it.

## Consequences

### Positive

- One surface answers "what happened under this key, in this window, and
  does the record hold" for every kind the trail carries, without SQL.
- The regulated tuple is presented in full: who with how the identity was
  established, what, when, outcome, the before and after, the reason where
  recorded, and the evidence of review.
- The marts gain what play and downstream readers need too; the viewer
  adds no private read path.
- Scenes can assert on the table, the detail pane, the verdict line, the
  exception list and the tree, because each is text.

### Negative

- Reads by conversation, task, entity, subject or principal scan within
  the time window until the storage decision lands; the viewer's stamped
  queries make that cost visible but do not remove it.
- The previous-state read is bounded by a look-back and says so; an
  entity whose last change predates it shows no before-state.
- The novelty rule scans the audit mart over the look-back on every
  refill; its cost is unmeasured and is the first thing to bound if a
  window on a busy host turns slow.
- A `consequential` command in a system app is a precedent, held narrow
  by being person-only.
- The strip is painted and a scene reads only its text line.

### Neutral

- The viewer's own reads and publishes appear in the trail it shows.
- Chat Analytics is unchanged; opening the viewer on a conversation is
  additive.

## Migration — Tier 1

Nothing to migrate. The views and functions are additive and installed by
the paths that install the views today; the `entity` column is appended.
Existing readers of `dm_trail_audit_events` by name see one more column;
positional readers of the marts do not exist by design.

## Verification plan — Tier 1

- **Lane.** Default `go test`: the composer's goldens for the functions and
  the two views, and `ViewsVersion` moved; the `clickhouse-local` lane:
  `TRAIL_ATTR`/`TRAIL_REF`/`TRAIL_REFS` on fixture rows, `entity` and
  `agg_trail_audit_entities` over a fixture with two entities and three
  events each, `dm_trail_reviews` listing a written mark; the viewer's
  package: the catalog validates with `Agents` on every operation but
  `mark_row`, every control routes through its command, each command sets
  and never toggles, `list_rows` and the pane and view queries read their
  arguments and are bounded, a cursor continues without repeating a row,
  the previous-state read returns the four verdicts of SD5 on fixtures
  including a gap row, each exception rule returns exactly its fixture's
  rows, a mark lands as an `AuditEvent` in the `review` domain under the
  person's principal.
- **Scene.** A headless scene launches the viewer on a fixture run,
  asserts the table, the detail pane, the verdict line, an exception and
  its clearing by a mark, and the strip's text line.
- **What would fail.** A query reading the window's filter instead of its
  arguments; an agent able to call `mark_row`; a `from` mismatch not
  reported; a view whose functions are missing on install.
- **Gap.** The strip's drawing is not asserted; the correlation-read cost
  is not bounded by a test.

## Milestones

- **M1 — The SQL surface.** SD2 in `trailviews`, goldens, `keelsonddl`.
- **M2 — The app.** Case, window, strip with its text line, windowed
  table, facets, context, detail pane, previous-state diff, verdict line;
  the `view`-effect catalog.
- **M3 — Review.** Exception rules, review marks, `dm_trail_reviews`,
  compare and as-of, the ADR-0345 entry.
- **M4 — Entry points and analytic views.** Open from chat on a
  conversation; the tree tab; export to play; the matrix, the dotted chart
  and saved views.
- **M5 — The remaining analytic views.** Day × hour and the numeric
  series.
- **M6 — The catalog completed.** A query for every pane and view, a
  command for every control, the saved views and the export (SD8).

## Status

Proposed — awaiting review by the owner. Two choices in this record are
the owner's and are flagged for that review: `mark_row` as a
`consequential`, person-only command (SD7), and the default look-back of
the previous-state read (SD5). The owner asked on 2026-11-01 for the
milestones to be built against this proposal; the build assumes both as
written, with the look-back at 30 days.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates
-->

## References

- [audit-trail-visualization-survey](../adr-background-work/audit-trail-visualization-survey.md) — the design space, criteria and forks this record settles.
- [facts-storage-table-switch-options](../adr-background-work/facts-storage-table-switch-options.md) — the correlation index and insertion time this record waits on.
- [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md), [ADR-0296](./0296-audit-events-as-a-trail-component.md), [ADR-0330](./0330-a-shared-audit-recorder-below-the-runtime-one-sealed-writer-over-a-narrow-store-binding.md) — the trail, the audit event and the recorder.
- [ADR-0327](./0327-evidence-as-a-record-store-profile-chain-vault-marks-seals-and-anchors-any-store-opts-into.md), [ADR-0314](./0314-tamper-evident-records-chain-digests-and-anchored-heads.md) — the verdict vocabulary's source.
- [ADR-0345](./0345-system-apps-join-the-operations-contract-the-log-viewer-first.md), [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — the operations contract and the effects.
- [ADR-0336](./0336-recordtable-row-grouping-a-line-sequence-over-the-permutation.md), [ADR-0016](./0016-imzero2-time-range-picker.md), [ADR-0134](./0134-adhoc-datasets.md) — the grid, the picker, the export.
- [ADR-0135](./0135-app-launch-requests.md), [ADR-0347](./0347-mdedit-links-a-url-scheme-a-location-grammar-and-launch-options.md) — launch configs and launch options.
- [ADR-0171](./0171-leeway-sql-read-surface.md), [leeway-sql-read-surface](../explanation/leeway-sql-read-surface.md) — the surface the functions join.
- [ADR-0051](./0051-query-categorization-provenance.md) — the `dm_` / `agg_` classes.
