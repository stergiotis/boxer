---
name: keelson-app
description: "Use when designing or writing an app under apps/ — what makes an app idiomatic in boxer on the keelson runtime: data kept as Arrow and handed to play, an explicit state machine the person and agents can read, work on tasks and watchbill with the standard progress row, an operations catalog and a help book as the agent surface, regex batteries and SQL instead of ad-hoc search, honest analysis, IDS look, progressive disclosure, and the immediate-mode cost model. For how to draw widgets see the imzero2 skill; this skill is about the shape of the app around them."
type: reference
audience: contributor or agent writing or reviewing an app under apps/
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-11-12
---

# Writing a keelson app — the house idiom

An app under `apps/` is a window over data that a person, a person working
with agents, or an agent alone can operate. This skill says what such a
window owes its host, its data and its operators, and which of boxer's
unusual properties it is expected to lean on. It is a router with rules:
each rule names the decision it follows from and the package or app to copy,
and stops there. The [imzero2 skill](../imzero2/SKILL.md) covers how a frame
is built; the [app package explanation](../../../public/keelson/runtime/app/EXPLANATION.md)
covers the `AppI` contract; the ADRs cited are the record and win where
this page disagrees.

The rules are numbered within sections so a review can cite one (`A3`,
`W5`). They are not all equal: a rule marked **must** is enforced or is a
contract the host relies on; the rest describe the idiom.

## 0. What an app is for here

Four of [why-boxer](../../explanation/why-boxer.md)'s premises land on
every app, and most rules below are one of them worked out:

- **P3, one data spine.** A table an app shows is Arrow; the way to read
  it from anywhere else is SQL. An app does not grow a private data layer.
- **P4, the toolkit is its own workload.** An app's state, jobs, datasets
  and operations are rows in introspection tables, so the app can be
  watched with the same tools it offers.
- **P7, interfaces split by task complexity.** A window is the simple
  tier; play is the middle tier where the person composes; the operations
  catalog is the agentic tier. An app serves all three with one model and
  one set of handlers, never three implementations.
- **The agentic layer.** The model never runs inside an app. An app
  declares operations and help; the chat harness drives them under a
  grant the person approved ([agentic-layer](../../explanation/agentic-layer.md),
  [ADR-0318](../../adr/0318-model-inference-is-the-harnesss-apps-expose-operations.md)).

**Governance is the runtime's, not the app's.** What an app may reach,
who approved it, what left the cell and what it cost are properties of the
runtime (the dispatcher, the grants, the labels, the exits, the audit
trail), of the code conventions (the lints, the capability cross-check,
the one-builder rules) and of the build (tags, the license gate, the
reproducible artefact). An app *declares*: capabilities with reasons,
operations with effects, resources with revisions, output that may be
untrusted. It does not enforce: no allowlist of its own, no approval
prompt of its own, no audit row it writes by hand, no budget it counts.
The last mile, the self-serving check an app adds for its own case, is
where governance stops being checkable, so it is a defect here, not
diligence (the authority is the grant, [ADR-0269](../../adr/0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
§SD6; [ADR-0312](../../adr/0312-exits-sinks-from-the-cell-boundary-approval-tiers-and-exit-budgets.md),
[ADR-0317](../../adr/0317-clipboard-and-export-copy-by-reference-and-one-choke-point.md),
[ADR-0324](../../adr/0324-the-persons-decisions-wait-in-a-host-queue-not-in-modals.md)).

**play and chat are regular apps, not system apps.** Both register through
the same factory, declare the same kind of manifest and are subject to the
same host; a consumer may replace either. Chat holds the model grant
because the person registered it as a coordinator, and that there is one
harness is a review convention, not an enforced one
([ADR-0318](../../adr/0318-model-inference-is-the-harnesss-apps-expose-operations.md)).
Play is the destination of every handover (§2) by default, and the role
it fills, the playground a window opens on a dataset or a statement, is a
launch kind the runtime owns: the contract is the leaf
`public/keelson/runtime/playground`, an open names the kind and no app, and
the host resolves it to the launchable app that takes the kind
([ADR-0361](../../adr/0361-the-playground-is-a-launch-kind-the-runtime-owns-and-the-host-resolves.md)).
Play's embed (`NewLivePlayApp`, `PlayApp`) and its operations catalog stay
play's (§SD6), so a consumer that keeps applets still links play and
designates its own app for the kind. An app written against this skill
opens the playground through the leaf, not play, and keeps that import in
one file.

Three operators, then, and each operation an app offers is designed for
one of them before it is written:

| Operator | Reaches the app through | Wants |
| --- | --- | --- |
| a person alone | widgets, the help book, the tethered inspectors | discoverability, direct manipulation, a visible state |
| a person with agents | the same widgets plus the catalog in observe, suggest or act mode | to see what the agent did, to undo it, to stay the tiebreak |
| an agent alone | the catalog, `keelson()` tables, the help service | bounded typed answers, refusals that name the remedy, a state it can read |

## 1. Shape before code

**S1 — A new app is a design dialogue first.** Apps are Tier 2 surfaces in
[CODINGSTANDARDS § What triggers an ADR](../../../CODINGSTANDARDS.md#what-triggers-an-adr):
an app of a new *shape* gets an ADR; another instance of an established
shape is code. Agree on the five decisions below before the first widget,
in a page or in the ADR; each has its section here.

1. **Data.** What tables the app shows, which of them are Arrow records,
   which it publishes, and what play receives on handover (§2).
2. **State.** The phases the window moves through, as a machine the person
   and an agent can read (§3).
3. **Work.** What runs off the frame, as tasks or watchbill jobs, what
   pausing does to it, and how each computation grows with the
   deployment's n (§4, W9).
4. **Operators.** Which operations a person, a team and an agent get, and
   what the help book has to say for the agent to use them (§5).
5. **Method.** For an analytical app, the method, its assumptions and how
   the window states them (§6).

**S2 — Descope the heavy periphery, record the deferral.** Ship the light
cut; a `// deferred:` note or an ADR *Deferred* entry with a trigger ("the
first app that needs it") is the house way to not block on the hardest
tenth ([AGENTS.md § Design before code](../../../AGENTS.md#design-before-code)).

**S3 — Register through a factory, keep state on the struct.** Every app
under `apps/` registers with `app.DefaultRegistry.RegisterFactory`; a
singleton cannot take a launch config, a workingset or a catalog. Per-window
state lives on the `*App` the factory returns, never in package variables.
A process-wide source (a sampler, a watcher) is a documented
`sync.Once` singleton that every window reads, as imztop's and imzrt's
are. Copy the manifest shape from `apps/watchbill/app_register.go` or
`apps/jackstay/app_register.go`: `Summary` is one verb-first clause,
`Topics` is required, `Caps` are composed from the client-caps helpers
(`task.ProducerCaps()`, `keelsonquery.ClientCaps(table)`,
`attention.ClientCaps(reason)`) and every `Reason` says what the app does
with the capability; a helper with a fixed reason (`task.ProducerCaps()`
has one) is the declaration itself and needs no second ([ADR-0026](../../adr/0026-app-runtime-and-capability-subjects.md),
[ADR-0158](../../adr/0158-app-classification-topics-keywords-kind.md)).

**S4 — Parameters arrive as a launch config, and the leaf is importable.**
An app that can be opened *on* something declares `Manifest.LaunchKind` and
a `launchcfg` leaf package: a leeway DTO with a `kind` tag, `AppId` and
`Kind` constants, a `kindcheck.Register` in `register.go`, a golden test.
The leaf exists so another app can open this one without importing its
registering `init()`. Precedence is launch config over environment variable
over persisted state ([ADR-0135](../../adr/0135-app-launch-requests.md) §SD7).
`Mount` decodes the config and returns the error; it does not fall back
silently; opening a plain window instead is a deviation, not a second
shape (decided 2026-11-07). With a launch kind the workingset comes for free
([ADR-0148](../../adr/0148-app-workingsets.md) §SD2). Copy
`apps/watchbill/launchcfg`.

**S5 — Environment variables seed, they do not configure.** An app's
`BOXER_<APP>_*` variables are declared through `env.NewString` beside the
manifest ([ADR-0009](../../adr/0009-environment-variable-registry.md)) and
exist to put the app in a starting state for scenes, demos and headless
runs. Anything a person changes while the window is open is state (§3),
not configuration. The chat harness is the one exception: its ceilings
(rounds, the screenshot budget, pixel limits) are environment variables
by [ADR-0284](../../adr/0284-chat-artefact-screenshots-sealed-under-a-budget.md) §SD4 and [ADR-0280](../../adr/0280-a-ceiling-on-what-a-chats-model-may-do-scored-on-a-ladder.md), because the
ceiling is the runtime's and the harness is where it is read.

## 2. Data: Arrow in the window, SQL at the edge, the playground as the destination

**D1 — A table the window shows is an `arrow.RecordBatch`.** Keep the rows
the app visualises or lets the person manipulate as an Arrow record, and
draw them with `recordtable.RenderGrid` ([recordtable how-to](../../howto/recordtable-grids.md)).
One shape then serves the grid, the sort, the publish to play and an
agent's bounded query. Rows that start as Go structs become a record once
per change, as `apps/watchbill/watchbill_table.go` does; `StringRecord` is
for text-only rows and its numeric columns are marked `NoSort`. A `c.Grid`
over a slice is for a dozen labelled values, not for data.

**D2 — SQL is the read surface, ClickHouse is the engine.** A computation
over rows that ClickHouse can express runs in ClickHouse, over a
`keelson()` table or a published dataset, not in a Go loop in the app. The
app keeps the statement, and the person or an agent can take that statement
to play unchanged. Reading a leeway table is a solved problem
([leeway SQL read surface](../../explanation/leeway-sql-read-surface.md));
a query naming sealed data obeys the placement rule of
[ADR-0145](../../adr/0145-sealed-app-data.md).

**D3 — Publish what the window computed as an ad-hoc dataset.** A result
the person might want to inspect, join or chart is published through one
`adhocdata.NewWindowPublisher` per alias, off the render goroutine; the
publisher reuses its handle so a recomputation replaces the dataset
([ADR-0240](../../adr/0240-adhoc-datasets-v2-sealed-store-owned-capability.md),
[ADR-0288](../../adr/0288-adhoc-bundles-datasets-published-with-the-applet-that-reads-them.md),
[ad-hoc datasets how-to](../../howto/adhoc-datasets.md)). Publish the whole
result, not the visible page: writingstylescope publishes its full cross
matrix and seeds play with SQL that reproduces the on-screen table. A
dataset outlives its window only when it is the app's, not the window's,
as imzrt's profile captures are; say why in the call's comment.

**D4 — Handover to the playground is the export.** The gesture is one
button, labelled *Open in play* or *Send to play*, that publishes (D3) and
then calls `playground.Open` with a launch config naming the alias, never
the handle. The playground is play unless the host registers another (§0),
and the import of the leaf sits in one file of the app
([ADR-0135](../../adr/0135-app-launch-requests.md)). The
request blocks and refuses with a reason; the button says *Opening…* while
busy, reports the refusal, and says what opened ("opened in play — N rows
as <the alias or handle the publisher gave>"). A window with no bus says
so instead of hiding the button. Copy
`apps/writingstylescope/writingstylescope_handover.go`, or the
operations-backed `sendToPlay` in `public/keelson/runtime/trailviewer`.
An app that uploads to facts first and opens play on the row is the mdedit
variant ([ADR-0217](../../adr/0217-mdedit-send-to-play-mddoc-facts.md)).
There is no file export under the app's own name: plaintext leaving the
cell is an exit with a clearance, and export as a broker subject of its
own follows the first consumer ([ADR-0317](../../adr/0317-clipboard-and-export-copy-by-reference-and-one-choke-point.md)
§SD6). A save through the fs broker's dialog subject, where the written
document is the app's work (mdedit's *Save*, [ADR-0178](../../adr/0178-mdedit-markdown-editor.md);
sqlappletcreator's *Export .md*, [ADR-0132](../../adr/0132-sqlapplet-sql-defined-applets.md); jackstay's plan
export), is the Powerbox's exit, recorded by the broker, and not what that
sentence forbids. Do not copy a fire-and-forget `go RequestOpen` that
reports "opened" before the reply.

**D5 — The clipboard is a broker request, labelled.** Copy through
`clipboardbroker.Write` with a `Payload` carrying a label and source, off
the frame, and show the record it returns; never the raw opcode, which
codelint CS014 catches, and never a raw `bus.Request` on
`clipboardbroker.SubjectWrite` or the unlabelled text call, which codelint
CS015 catches at warn. `apps/chat/chat_actions.go`
is the shape.

**D6 — Durable state has three tiers and one store.** Persist keys through
`ctx.Storage()` declared in `Manifest.PersistedKeys`; the workingset the
host saves at close; column widths through the resolver. All three land in
`boxer.persiststate`, browsable and deletable in the app-state manager
([ADR-0185](../../adr/0185-durable-app-state-manager.md)). The rule for
what goes in is *drafts in, caches and chrome out*
([ADR-0148](../../adr/0148-app-workingsets.md) §SD1): buffers, parameters
and the active view; never a recomputable cache, window geometry or a
published artefact. An in-window layout split is chrome and stays out;
column widths are the exception because the resolver owns them
([ADR-0151](../../adr/0151-table-column-width-overrides.md)). An app never holds a facts store and never
writes files under its own name. A lading tree published through the
runtime's store path ([ADR-0222](../../adr/0222-tally-composition-surface.md)) is a publish, not a store
the app holds; a ClickHouse client the app builds from the environment to
write one is. A new kind of durable *fact* is a generated
record store ([facts-bound record stores](../../explanation/facts-bound-record-stores.md)).

**D7 — Published, live and in-flight things are rows.** Datasets, tasks,
jobs, operations, grants, state and frame times each have a `keelson()`
table. An app does not add a second way to see them; a window that wants
to show "what this app has live" opens play on the table, as the app
center's sections do ([ADR-0260](../../adr/0260-app-center-one-page-per-app.md)
§SD4a).

## 3. State: an explicit machine the window mirrors

**T1 — Name the phases as a machine; the window observes it, it does not
drive it.** A window with more than two phases declares them as an enum
and a `fsmview.NewMachine` with rules and edge labels naming the gesture
that causes each edge ("Discover", "Start"). Each frame derives the phase
from the facts (jobs, results, plan) with a pure `observePhase` function
and calls `Mirror`; a mirror the rules cannot reach is a warning, not a
crash ([ADR-0045](../../adr/0045-imzero2-fsmview-widget.md) and its
2026-08-21 update). The machine is documentation the host can check, not
a controller the handlers consult. Failure returns to the phase it
interrupted rather than owning a terminal state. Copy
`apps/jackstay/jackstay_machine.go`; the same comment heads chat's turn
state and play's query state.

**T2 — Show the phase as the chip, with the machine behind it.**
`fsmview.New(..., Options{Tethered: true})` draws the level-1 chip the
person reads at a glance and opens the graph, table and history on click.
Give it provenance (`app.<id>.<thing>.phase`) so the inspector says where
the value came from ([ADR-0046](../../adr/0046-imzero2-value-inspector-infrastructure.md)).
Build the chip in `Mount`, draw it in the status row, not in a panel of
its own. Play, chat, jackstay and watchbill do this.

**T3 — Mount the machine for agents.** `fsmops.Mount(set, name, summary,
source, Options{History})` adds the `name_state` and `name_machine`
queries to the catalog, so an agent reads the current phase, the edges
open from it and the recent history in the same schema every app uses
(ADR-0269's update of 2026-10-02). A machine that tracks a *selected* row
rather than the window mounts without history, as watchbill's does.

**T4 — Three tests per machine.** Table-driven cases for `observePhase`;
every phase reachable from the initial one; the catalog validates and the
two queries decode. `apps/jackstay/jackstay_machine_test.go` has all
three.

**T5 — Snapshot, then draw.** The window keeps one immutable snapshot of
what it shows, swapped in under a mutex by whoever computed it, and the
frame draws the snapshot it finds. Agent queries answer from the latest
snapshot off the render goroutine; commands apply on it after the frame's
widget write-back, so the person's edit of the same frame wins
([ADR-0269](../../adr/0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
§SD4). A resource an agent may write is declared with its revision, and
`Set.Editing` turns an agent write into a conflict while the person is
typing in the bound widget.

## 4. Work: tasks, jobs, the standard progress row, and pausing

**W1 — Nothing blocks the render goroutine; nothing else touches imzero2.**
`Mount`, `Frame`, `Unmount` and every imzero2 call run on the one render
goroutine ([ADR-0261](../../adr/0261-one-render-goroutine-for-every-app.md)).
Work that waits, reads or computes runs elsewhere and hands a result to a
later frame. The budget is `windowhost.SlowFrame`, 8 ms of `Frame` at p95
([ADR-0351](../../adr/0351-what-a-window-costs-beyond-frame-cpu-by-window-label-and-a-slow-window-notice.md)): work that can cross it leaves the frame, and a
capped computation that stays under it on an explicit gesture says so in
a comment. The opt-in check `IMZERO2_RENDER_GOROUTINE_CHECK` panics on
the offending goroutine when a violation is suspected.

**W2 — Short work is a task on `task.ForApp`; the runner is `bgjob`.** A
computation the person waits for runs as a keelson task so it has a
handle, progress, cancel and a row in `keelson('tasks')`
([ADR-0038](../../adr/0038-keelson-background-task-primitive.md)). The
app-side shape is `bgjob.Runner[T]` for a one-shot and `bgjob.Keyed[T]`
for "recompute when this key changes": the worker never calls imzero2, the
frame takes the result once, and a stale result for an old key is
dropped. The handle's `Report` may run in a hot loop; emission is gated by
humanised change. `task.ForApp(ctx)` is the constructor: a
`task.NewBusApi` built from the bus alone compiles and lists tasks but
carries no instance key, mount cancel or lifetime (codelint CS016). Copy
jackstay's runners
and its `takeResults` at the top of `Frame`.

**W3 — Durable or long work is a watchbill job.** Work that must survive
the window, be retried, or run on another machine is a watchbill job with
a registered handler, enqueued through the client on the mount context's
bus ([watchbill how-to](../../howto/watchbill-jobs.md)). Prefer a
`Subject` row over args. Inside a command handler an agent's call is
running, copy `OnBehalfOf` onto the request so the job carries the
delegation and runs under the agentic entry frame
([ADR-0344](../../adr/0344-agent-caused-jobs-in-a-process-of-their-own-and-the-entry-frame-every-stack-carries.md)).
`apps/watchbilldemo` is the end-to-end example: the handler, the monitor,
and an async `enqueue` command that copies the call's `OnBehalfOf` onto
the request, with `TestOnBehalfOfReachesTheRequest` beside it.

**W4 — Progress, cancel and ETA come from the standard row, never a raw
bar.** A running task or job is drawn with `widgets/jobprogress` (fraction,
rate, ETA from the one estimator of
[ADR-0247](../../adr/0247-one-progress-estimator.md), cancel), with
`widgets/bgjobrow` over a `Runner` or `Keyed` snapshot, or with
`widgets/taskmonitor` when the window shows every task of the app. A raw
`c.ProgressBar` is a gauge for a quantity, not a job. `apps/taskdemo`
and `apps/watchbilldemo` show the monitor; mdedit, tally and jackstay use
`bgjobrow`. A query run is a job too: its row is `jobprogress.Render`, as
play draws it.

**W5 — Goroutines are counted into the instance's lifetime scope.** The
host closes the stop channel, retires the scope, unmounts, drains off the
render goroutine for a bounded time, then closes the bus client
([ADR-0322](../../adr/0322-app-instances-drain-a-lifetime-scope-between-unmount-and-unload.md)).
A goroutine the app starts goes through `task.ForApp`, through
`app.LifetimeOf(ctx)` with `scope.Go`, or through a child
`lifetime.New(parent)`; a hand-built cancel-and-WaitGroup still works but
is invisible to the host's drain and to the Running tab. Tails and
drained hooks make no imzero2 calls. A loop selects on both the scope's
context and `ctx.Cancel()`. `apps/servicedemo` and chat's per-coordinator
child scopes are the shape. Codelint CS017 reports a `go` statement under
`apps/` that nothing the host drains owns; a goroutine that is process-wide
on purpose says why in its function's doc comment (`// lifetime: <reason>`).

**W6 — Implement `app.PausableAppI` when you poll.** Pause is the
person's act; the host stops drawing the window and asks the app once,
on the render goroutine, at pause and at resume
([ADR-0354](../../adr/0354-pausing-a-window-the-host-stops-drawing-it-and-asks-the-app-to-stop-its-work.md)).
A poller per window stops reading while paused and reads at once on
resume (`paused atomic.Bool`, a `markDirty` on resume); work driven from
`Frame` needs nothing; a process-wide sampler keeps running because other
windows read it. Tasks are not cancelled by a pause. Commands to a paused
window are refused by the host; queries keep answering from the snapshot.
Test it as `apps/appstate/appstate_pause_test.go` does: nothing is read
while paused, a read lands after resume. A control that freezes a
process-wide sampler for every window is not a pause: label it *Freeze*
and *Go live*, as imztop does, and keep *Pause* for the host's act.

**W7 — Request repaints while something runs, and stop when it settles.**
In reactive cadence a frame is drawn on input or on request
([ADR-0062](../../adr/0062-imzero2-render-cadence.md); the default is
continuous, so a missing request shows only under
`IMZERO2_RENDER_CADENCE=reactive`). A window with a running job calls
`RequestRepaint` while `anyRunning()` so the row advances; a poller that
lands a snapshot requests one repaint so the window shows it before the
next input; nothing requests one unconditionally every frame, which keeps
the loop from idling at all.

**W8 — Report outcomes after the reply, in the window.** A request's
success is shown when its reply arrives, with the detail the reply
carries; a refusal is shown with its reason. Nothing is logged to the
status line before the round trip.

**W9 — Place work by how it grows with n, at the deployment's n.** Apps
here run over tables of billions of rows, fleets of hosts, trees of
millions of files; the fixture a test or a demo uses is three or four
orders of magnitude smaller, and a method that is instant on it can be
hours or out of memory in use. So, for each computation, name n (rows,
series, nodes, hosts, pairs) and the growth in time and in memory, in the
doc comment of the function that does it, beside the method's citation
(F3). The growth then decides where the work runs:

- **Per frame:** proportional to what is on screen, not to n (P1).
  `recordtable` emits only the visible rows; a body that walks every row
  or sorts the whole record each frame leaves the frame.
- **Linear or n log n over the data:** ClickHouse, when it can express
  it (D2); otherwise a task (W2). Pulling n rows into Go to compute an
  aggregate ClickHouse would return is the shape to replace.
- **Quadratic and above** (all pairs, a distance or similarity matrix, a
  naive join of two lists, a layout over every node): never the frame and
  never unbounded. A task with progress and cancel (W2, W4) at minimum, a
  watchbill job when it outlives the window (W3), and an input cap the
  window states.
- **Super-polynomial** (exp(n), n!: every subset, every ordering, exact
  combinatorial optimisation, backtracking search): not on an unbounded
  input at all. Use a polynomial approximation and state what it gives up
  (F3), or bound n explicitly and refuse beyond it with the remedy (F6).
  RE2 batteries (F1) are linear by construction; a backtracking regex
  engine is not.

A cap, a sample or a truncation is part of the method and is shown where
the result is: "first N of M", a `Truncated` flag on a query (A2), a
sampled series named as sampled; a silent cut at a fixture-sized limit is
a wrong answer at the deployment's size. Quadratic memory deserves the
same suspicion as quadratic time: n² cells at n = 10⁵ is 10¹⁰. Check the
growth at two sizes an order of magnitude apart, in a benchmark or a test,
rather than trusting one small run; `keelson('frame_times')` (P5) shows
what reached the frame anyway.

## 5. The operator surface: operations, help, names, attention

**A1 — Every app with state declares an operations catalog.** The catalog
is an `appops.NewSet` built once at package level, with resources, queries
and commands, exposed through `Operations()`
([ADR-0269](../../adr/0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)).
`apps/opsdemo/opsdemo_ops.go` is the template; `apps/mdedit/mdedit_ops.go`
the small exemplar, `apps/watchbill/watchbill_ops.go` the table-shaped
one, `apps/play/play_ops.go` the large one. System-shaped apps follow
[ADR-0345](../../adr/0345-system-apps-join-the-operations-contract-the-log-viewer-first.md).
Two shapes owe none: a coordinator, whose catalog is other windows'
(agentconsole's test asserts `Operations == nil`), and a window whose only
state is which tab is open. A minter that registers many manifests from
documents (sqlapplet) answers this line per minted window, and the embed
seam's forwarding (`BundleViewOps`) is how a minted window serves the
catalog of the play inside it.

**A2 — Choose operations by operator, not by widget.** Decide, per
operation, who it is for (§0). The rules that follow:

- A **query** is a function of its arguments and the snapshot, takes its
  own arguments rather than reading the person's filter row, and returns a
  bounded struct (rows capped, bytes capped, a `Truncated` flag). What a
  `keelson()` table already carries is not an operation; the summary
  points at the table.
- A **command** sets a value and never toggles it, so a repeat is
  `applied`; it returns an outcome, never data. Its effect is declared
  honestly: `view` for selection and camera, `document` for authored
  state, `run` for executing against a data source (a command whose
  queries are fixed and bounded may stay `view` while it dials,
  [ADR-0329](../../adr/0329-imztop-operations-catalog.md) §SD4), `consequential` for anything that writes
  outside the app (publishing, cancelling a job, export). A consequential
  command is confirmed by the person every time, by the host; do not
  soften an effect to skip the prompt. A consequential act a window keeps
  off the catalog on purpose (clearing app state, [ADR-0345](../../adr/0345-system-apps-join-the-operations-contract-the-log-viewer-first.md)
  §SD4; applying DDL, [ADR-0259](../../adr/0259-jackstay-guided-clickhouse-to-clickhouse-sync.md)) is the person's own click
  and takes an inline, non-modal arm-and-confirm (A9), which is the person
  endorsing, not a gate (X4).
- Long work is a command with `Async: true`, settled with
  `appops.Settles`, gated with `Set.Available` while in flight
  ([ADR-0358](../../adr/0358-asynchronous-operations-a-pending-outcome-and-a-turn-a-completion-wakes.md)).
- A **refusal names its remedy** ("the document changed since that
  revision; read it again with get_document") through
  `app.RefuseOperation`; a silent empty result teaches an agent nothing.
- Output that may carry content an agent or a person typed is marked
  `Untrusted`.
- Agent-caused runs against a data source go through the agent limits,
  which fail a run and never change it (`apps/play/play_agent_limits.go`).

**A3 — One path: the person's button calls the same handler.** A gesture
the catalog exposes goes through `appops.Gesture`, with a direct fallback
when no host serves the catalog (tour, tests, headless), so an agent and a
click cannot drift ([ADR-0269](../../adr/0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
§SD8). Declare the `Gesture` text on the spec so a model can tell the
person what to click.

**A4 — Write the help book for the agent as much as the person.** The
`help/` directory with an `overview.md` is embedded through
`help.MustSub` on `Manifest.Help`
([ADR-0047](../../adr/0047-keelson-help-system.md)). F1 opens it for the
person; the `runtime.agent.help` service and `keelson('helpsections')`
serve the same sections to a model without a grant. So the book says what
a model needs to operate the app: the operations by name and when each is
available, the phases of the machine and what moves them, the
preconditions a refusal will name, the datasets and tables the app
publishes or reads, the honesty lines of its method (§6). Diátaxis front
matter, one topic per file, no dialect beyond what the markdown widget
renders. Add the two corpus tests from `apps/mdedit/help_corpus_test.go`.

**A5 — Instructive text lives in the GUI, in a tooltip or an inspector.**
An icon-only control carries a tooltip; a control whose meaning depends
on a method carries the method's caveat as the tooltip
([iconography pattern](../../design-system/patterns/iconography.md)). Keep
the texts as named constants beside the render code, as writingstylescope
does, so a scene can `read` them and the help book can quote them. For
more than a sentence, open a tethered inspector
([ADR-0046](../../adr/0046-imzero2-value-inspector-infrastructure.md)):
a level-1 chip with provenance and a level-2 window on click, sized by
the inspector surface token.

**A6 — Everything a person can point at has a name.** Buttons and check
boxes take their text as the accessible name; a text input takes it from
`AccessibleName`, since its hint is only the placeholder; labels are
anchored by value. A painted mark gets a hit region with a `Label`
(`PaintHitRect`, `PaintHitCircle`, `PaintHitPolygon`) and a painter area an
`AccessibleRegion`, so the tree names it and a tooltip reads it
([imzero2 skill §14.4](../imzero2/SKILL.md), `apps/riskviews/canvas.go`).
A scene that has to click by coordinates is a defect report against the
widget.

**A7 — A scene is the app's maintained proof.** Each app ships at least
one scene under `apps/<app>/scenes` that launches it in a seeded state,
reaches its main surfaces by name, reads a value and asserts it (an
`expect`, or a `wait` matched on `value`), captures, and ends with F1 on
its own help
([ADR-0248](../../adr/0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md),
[imzero2-drive skill](../imzero2-drive/SKILL.md)). An app with a catalog
adds a scene that drives it through agentconsole or a scripted model, as
chat's coordinator scenes do. A sequence of steps an agent found to work is
saved as a scene rather than repeated.

**A8 — A tour entry gives the app a deterministic picture.** Register one
or more `registry.Demo` entries that draw the app in a seeded state without
window chrome ([ADR-0057](../../adr/0057-demo-registry-and-drivers.md)), so
the screenshot driver and the design-system review tier see it. A
whole-app carousel registration is not one: a full-window app (play)
owes a seeded demo entry like any other (decided 2026-11-07).

**A9 — Ask through the attention queue, never with a modal.** A decision
the person owes is posted as a *question* through `attention.Post` under
`attention.ClientCaps`; the host draws it as a card the person can answer,
acknowledge or shelve ([ADR-0324](../../adr/0324-the-persons-decisions-wait-in-a-host-queue-not-in-modals.md)).
Something worth noticing that nothing waits on is a highlight, one per
source. A job's completion owes no decision and stays on the attention
scale ([ADR-0323](../../adr/0323-attention-levels-one-table-two-scales-configurable-within-a-floor.md)).
Process-global input (a chord, a drop) is gated on `app.WindowFocusI`.
An inline arm-and-confirm for the person's own consequential click (A2),
or a two-click guard before discarding their own unsaved work, is neither
a modal nor a question for the queue; the queue is for decisions someone
not at the window owes.

## 6. Filtering and analysis: capable languages, honest methods

**F1 — Filtering is a regex battery or a SQL predicate, not a substring.**
A text filter is a battery: space-separated, case-insensitive RE2 tokens,
all of which must match, parsed with `search.ParseQuery` and typed into
`regexedit` in `ModeTokens` with the hint *Filter (regex, space = AND)*
([ADR-0164](../../adr/0164-documentation-regex-search.md),
[ADR-0015](../../adr/0015-regex-pattern-syntax-highlighting.md)). A token
that does not compile matches literally and the row says so. The battery
is inside the RE2 ∩ hyperscan subset by construction, so the same query
runs in ClickHouse with `multiMatchAllIndices` when the rows are there,
as the documentation search lane does. Over tabular data the filter is a
`WHERE` fragment the person writes and play can take as is, as jackstay's
row filters are ([ADR-0271](../../adr/0271-jackstay-row-filters-and-packs.md)).
Recompute the match set when the query changes, not per frame. Copy
`apps/play/play_snippets.go`'s filter row. `strings.Contains` over a
lower-cased name is the shape to replace, and so is an editor's literal
find-in-document; mdedit's find is a battery.

**F2 — Highlight the pattern; name the gap on hits.** `regexedit` colours
the pattern text; painting the hits inside result rows exists only in
mdedit's find and the regex explorer, not as a shared widget (observed
2026-11-07). An app that needs hit painting reuses the ADR-0130 overlay
rather than inventing a third.

**F3 — Name the method, and state its assumptions where the number is.**
An analytical display says which estimator, test or distance it shows,
with the citation in the doc comment of the function that computes it,
and the assumption that could be violated as the tooltip of the widget
that shows it. writingstylescope's band tooltip is the model: it states
the coverage guarantee and that the observations are not independent, so
the band is a scale reference. The survey behind a choice of method is an
explanation page with a provenance block that says what was run against
boxer data and what is literature
([timeseries-motif-anomaly-survey](../../explanation/timeseries-motif-anomaly-survey.md)).

**F4 — The honesty commitments of ADR-0163 apply to every app.** They are
listed in [play-timeseries-analysis-survey](../../adr-background-work/play-timeseries-analysis-survey.md)
and the ones every app meets are: a distance is not a probability;
interpolation is fabrication, so state data is a step plot and a gap is
a gap ([plots pattern](../../design-system/patterns/plots.md)); a baseline
renders beside the detector by default; a threshold the method does not
supply is not drawn (writingstylescope's "there is no threshold here on
purpose"); uncertainty sits beside the estimate it qualifies, as a band,
a quantile dotplot or a score, never as a decimal place. A bad row
rejects the result loudly; a silently empty plot is a defect.

**F5 — One algorithm per name.** A variant is a new name; a name whose
meaning drifts silently falsifies every artefact that cited it
(play's series registry). The same holds for a column an app publishes.

**F6 — Guide toward the correct procedure, by scaffolding.** When the
input does not satisfy the method, refuse with the reason and offer the
one-click scaffold that writes the correct idiom (play refuses an
irregular grid and offers the resample SQL,
[ADR-0163](../../adr/0163-play-timeseries-workbench.md) §SD2). Order the
controls as the procedure runs: data, then preparation, then the method,
then its diagnostics. A caption under a derived series says what was
applied to what, as play's node captions do.

**F7 — Grade the evidence for a view.** A view built on a published
finding says so, and one built on guidance says that instead
(riskviews' per-view `evidence` line). A prototype says it is one, in the
window.

## 7. Look and feel: calm, dense, familiar

**L1 — Tokens only.** Colour from `styletokens` roles and `Tone`, data
encodings from the sequential, diverging and qualitative ramps, spacing,
rounding, stroke and motion from their ladders; icons from Phosphor through
the `icons` package, brand marks from the Nerd Fonts subset
([ADR-0029](../../adr/0029-imzero2-design-system-and-policy-as-code.md),
[ADR-0044](../../adr/0044-imzero2-design-system-iconography.md)). The
tier-1 lints enforce casing, spacing, rounding, stroke and motion, and the
palette only where the constructor is `color.RGB` or `color.RGBA`
([tier1-mechanical](../../design-system/policy/tier1-mechanical.md)); a
`color.Hex(0x…)` or a bare `uint32` literal is caught by L16, which ships
at warn until its findings reach zero. A `designlint:ignore` carries its
reason, and a missing token is a Tier 3 ADR, not a hex literal. The theme is chosen at launch, so only token reads
follow it. `apps/imztop/imztop_theme.go` derives everything from tokens.

**L2 — Buttons name their role, and there is one primary per view.**
`Kind(c.ButtonKindPrimary)` on the action the view exists for, `Danger` on
what destroys, `Ghost` for the toolbar; the client colours them per theme
([ADR-0273](../../adr/0273-imzero2-ids-button-kinds.md)). Status is a
`badge` with a tone, never a coloured label, and it stays Soft or Ghost so
that it does not read as a button.

**L3 — Familiar vocabulary, consistent verbs.** *Open in play*, *Send to
play*, *Cancel*, *Retry*, *Refresh*, *Pause*, *Resume*, *Freeze*, *Go
live* (W6): use the verb the rest of the desktop uses for the same act. Sentence case on labels, no
trailing periods on controls, units on numbers. The register of in-window
text is the register of the repo's prose: descriptive, no praise, the
caveat first ([DOCUMENTATION_STANDARD § Voice](../../DOCUMENTATION_STANDARD.md#voice-and-tone)).

**L4 — Empty states say which of the three things happened.** Nothing to
show, the read was refused, or there is no bus on this host are different
sentences with different remedies ([empty-states pattern](../../design-system/patterns/empty-states.md),
[ADR-0260](../../adr/0260-app-center-one-page-per-app.md) §SD3). A pane
that is loading says so and names what it waits on.

**L5 — Beginner surface first, the rest behind one fold.** The window
opens on what the method needs: the data, the one or two parameters that
matter, the result. Everything else sits under an *Advanced* collapsing
header, a *Details* section, a tethered inspector on the chip that
summarises it, or an advanced setting that enables a whole tier (chat's
Analytics tab under its Advanced switch). The fold's label says what is
inside, not "more". Parameters that are advanced for a person are still
operations for an agent (A2), so the fold hides controls, never
capabilities.

**L6 — Density over chrome.** Apps here are tables, plots and dense forms;
the design system's thesis is Swiss and scientific, restraint and
hierarchy by weight and spacing rather than ornament
([ADR-0029](../../adr/0029-imzero2-design-system-and-policy-as-code.md)).
No decorative frames, no banner headings, no animation that carries no
information. The data is the figure; the chrome is the margin.

**L7 — The status row is the window's instrument panel.** One row at the
bottom or top carries the phase chip (T2), the running job row (W4), the
dataset chip with its provenance, and the one-line note of the last
outcome (W8). A person or a scene reads the window's state from that row.

## 8. Immediate mode: the cost model, and the guards

**P1 — Every frame rebuilds every open window, hidden tabs included.** Go
runs every body each frame and the Rust side late-culls what is not shown:
a culled widget's arguments are still read and its registers drained, and
a collapsed block's children are still produced into a buffer the host
discards ([imzero2 skill §13](../imzero2/SKILL.md)). Hidden work therefore
costs what visible work costs on the Go side. Design the body so what it
builds per frame is proportional to what is on screen.

**P2 — Gate a measurably heavy body with lazypane.** One `lazypane.New`
per dock tab or region, `pane.Skip()` first thing in its body; the probe
reports whether the region reached the screen last frame
([imzero2 skill §18](../imzero2/SKILL.md)). Gate only what measures: take
an allocation profile hidden and open, and a body that allocates the same
both ways is the one to gate. Cheap bodies stay eager; a body that tethers
a floating window stays eager; one-shot delivery into a hidden tab rides
an activated tab, not a skipped one. play, imztop and tally are the
exemplars.

**P3 — Memoise by key, not by frame.** A derived view is recomputed when
its input changes, keyed by a pointer to the snapshot or a hash of the
query, and held on the struct: imztop's process view memo, play's
"recompute when the query changed", the highlight-job caches keyed by
text (`regexedit.Cache`, [ADR-0130](../../adr/0130-imzero2-textedit-highlight-seam.md)). `.Keep()`
holders for static text and atoms are built once and reused.

**P4 — Fetchers only from `StateManager.Sync()`.** An app never calls a
`Fetcher.*` opcode inline; a widget that needs one reads it at frame end
([imzero2-fetchers skill](../imzero2-fetchers/SKILL.md)). Readbacks lag
one frame by design, and a body that needs last frame's rect says so once.

**P5 — Read the frame-time table before optimising.** `keelson('frame_times')`
has the loop period and each window's `Frame` quantiles; the Running tab
shows them and the host posts a slow-window notice when a window's p95
stays above the signpost ([ADR-0261](../../adr/0261-one-render-goroutine-for-every-app.md)
§SD3, [ADR-0351](../../adr/0351-what-a-window-costs-beyond-frame-cpu-by-window-label-and-a-slow-window-notice.md)).
Goroutines a window starts inherit its pprof labels, so *Measure CPU*
attributes background work too. The slow-frame warning gates on render
plus interpret, not on the vsync wait; chasing the compositor is the
documented mistake ([render troubleshooting](../../howto/imzero2-render-troubleshooting.md)).

**P6 — Ids are stable and scoped.** Multi-child widgets scope their id
stack (`c.IdScope`); row ids come from the vocabulary (`PrepareSeq`,
`PrepareStr` of a stable key), never from a position or a counter joined
into a string; a prefix joined to a stable key
(`PrepareStr("verdict-" + verdict.String())`) is still a stable key. An id
that must survive an edit above it is keyed on content. A mismatched stack
compiles and panics at render ([imzero2 skill §3 and §21](../imzero2/SKILL.md)).

## 9. Extension seams for consumers

Boxer is a library a downstream module composes
([adopting boxer downstream](../../howto/adopting-boxer-downstream.md)).
An app is extended the way the tree already extends things, and only when
there is a witness.

**X1 — A seam waits for its first consumer.** Add an extension point when
a second app, a downstream module or a documented plan needs it; record
the deferral with that trigger otherwise. The tree's own seams each
arrived this way (play's detail renderer, docs source, snippet libraries,
gloss rules, applet books).

**X2 — A seam is a typed slot, registered in Go, at the wiring site.** The
shapes in use: a function-typed field with a setter and the default body
exported so the override can delegate (`PlayApp.SetDetailContent`,
[play-pluggable-detail](../../howto/play-pluggable-detail.md)); a small
interface the host type-asserts off the context, costing hosts without it
nothing ([ADR-0155](../../adr/0155-app-embed-seam.md) §SD1); a `Register*`
function that validates and refuses duplicates (`RegisterSnippetLibrary`,
`sqlapplet.RegisterBook`); a registry built once with composition declared
per seam ([ADR-0310](../../adr/0310-harness-profile-and-registry-composition-declared-per-seam.md)).
Nothing is read from files, the environment or persisted state to decide
what code runs ([play-gloss-rules](../../howto/play-gloss-rules.md)).

**X3 — Not a seam.** Generic hook lists with load-order mutation, runtime
code loading, configuration files that select behaviour, a second render
path "for embedding". An app that another app should open takes a launch
config (S4); an app that another app should embed takes the embed seam.

**X4 — Declare, do not enforce.** The seam a consumer needs most is the
one that lets the runtime govern their app the way it governs play and
chat, and that seam is the manifest: capabilities, effects, resources,
labels. An app that adds its own gate (an id allowlist, a confirmation
dialog of its own, a hand-written audit row, a counted budget) has moved
governance into the last mile where neither the lints nor the trail can
see it (§0). If the runtime lacks a gate the app needs, the gate is a
runtime ADR; the app waits or declares the effect conservatively.

Three things look like gates and are not: a ceiling, approval or journal
entry the chat harness applies, which [ADR-0318](../../adr/0318-model-inference-is-the-harnesss-apps-expose-operations.md)'s update
places with the harness as the runtime's agent ([ADR-0284](../../adr/0284-chat-artefact-screenshots-sealed-under-a-budget.md)
§SD4, [ADR-0312](../../adr/0312-exits-sinks-from-the-cell-boundary-approval-tiers-and-exit-budgets.md) §SD3, [ADR-0342](../../adr/0342-chat-artefact-export-to-lading-and-the-handover-to-mdedit-by-snapshot.md)); a check
through a runtime helper such as `app.RefuseForDestinations` against the
grant the call carries ([ADR-0329](../../adr/0329-imztop-operations-catalog.md) §SD4), which is the app
reading its own declaration; and the person's inline arm-and-confirm of
their own consequential click (A2, A9). One thing is a gate and is
tolerated: play's environment-selected write gates
(`BOXER_PLAY_ALLOW_WRITES`, `BOXER_PLAY_APP_WRITES`,
[ADR-0270](../../adr/0270-play-operations-catalog-and-agent-limits.md) §SD7) exist because the transport has no runtime
gate for a statement with a body; the runtime ADR that supplies one
retires them (§12). A runtime service that lives in an app package under
a `runtime.*` id (sqlapplet's applet store, the moderation gate of
[ADR-0132](../../adr/0132-sqlapplet-sql-defined-applets.md)) is the runtime's code in an app's directory, not
an app gate; it now lives in `public/keelson/runtime/appletstore`, and
sqlapplet fills its `MinterI` slot (ADR-0132's Update of 2026-11-07).

**X5 — The leaf is the contract.** What a consumer imports to drive the
app (its launch config, its operation argument types, its dataset aliases)
lives in a leaf package with no registering `init()`, so importing the
contract does not register the app.

## 10. Review checklist

Run this over an app before calling it done. Each line cites its rule.
It applies to packages that implement `AppI`. A `package main` command
that happens to live under `apps/` (observed 2026-11-07: jsonbench,
svgserver) is outside it, and where such commands belong is a §12 gap. A
headless service ([ADR-0352](../../adr/0352-services-headless-apps-a-service-host-runs-at-boot-under-a-predicate-composed-with-the-launch-limit.md)) answers S3, D6, W5 and X4 and
the rest is n/a. For a minter (sqlapplet) the manifest, leaf and seam
lines are the minter's and the rest are each minted window's.

- [ ] Manifest: factory-registered, summary verb-first, topics, caps with
      reasons, help book with `overview.md` (S3, A4)
- [ ] Launch config leaf with kind check and golden test; precedence
      config > env > persisted (S4, S5)
- [ ] Tables are Arrow records drawn with recordtable; derived results
      publishable; *Open in play* reports the reply (D1, D3, D4)
- [ ] Persist: drafts in, caches out; no files, no facts store (D6)
- [ ] Phases: enum, machine with edge labels, mirrored from facts, chip in
      the status row, mounted for agents, three tests (T1–T4)
- [ ] Off-frame work on `task.ForApp` or watchbill; jobprogress or
      bgjobrow, never a raw bar; goroutines in the lifetime scope;
      `PausableAppI` where there is a poller (W2–W6)
- [ ] Each computation names its n and its growth; per-frame work is
      proportional to the screen, quadratic work runs on a task with a
      stated cap, nothing super-polynomial runs on an unbounded input,
      and caps and samples are shown with the result (W9)
- [ ] Catalog: queries bounded and argument-taking, commands set and never
      toggle, effects honest, async settled, refusals name the remedy,
      gestures on one path, catalog tests (A1–A3)
- [ ] Help written for the agent: operations, phases, preconditions,
      datasets, caveats; corpus tests (A4)
- [ ] Tooltips on icon-only controls and on method-dependent displays;
      painted marks have hit labels (A5, A6)
- [ ] At least one scene with a read and an assertion; a tour entry (A7, A8)
- [ ] Questions through the attention queue; no modals (A9)
- [ ] Filters are regex batteries or SQL predicates with the literal
      fallback shown (F1)
- [ ] Method named, assumptions in the tooltip, uncertainty beside the
      estimate, no fabricated continuity, loud rejection of bad input
      (F3–F6)
- [ ] Tokens only, button kinds, badges for status, three-way empty
      states, one fold for advanced controls (L1–L5)
- [ ] Heavy bodies measured and gated, memos keyed, no inline fetchers,
      ids scoped (P2–P6)
- [ ] Any new seam has a witness and is a typed Go slot (X1–X2)
- [ ] The app declares and never enforces: no allowlist, prompt, audit
      row or budget of its own; the playground is opened through its leaf, from one file (§0, X4)

## 11. Where to copy from

Observed 2026-11-07; the tree moves, so check the app still does what the
row says before copying.

| For | Copy | Not |
| --- | --- | --- |
| manifest, launch config, poller with pause | `apps/watchbill` | |
| state machine mirrored, tethered chip, runners, bgjobrow | `apps/jackstay` | `apps/appstate` as a machine example (it has none) |
| Mount that refuses a bad launch config | `apps/sqlappletcreator`, `apps/play`, `apps/mdedit` | |
| goroutines in the lifetime scope | `apps/servicedemo`, chat's coordinator scopes | a hand-built cancel-and-WaitGroup |
| handover to play with an honest button | `apps/writingstylescope`, `apps/imzrt`, `trailviewer.sendToPlay` | a fire-and-forget `go RequestOpen` |
| method caveats as tooltips, no false threshold | `apps/writingstylescope`, `apps/terrainscope` | a method named only in a doc comment |
| evidence grading per view, hit labels on painted marks | `apps/riskviews` (a `KindDemo` with no catalog, help or tour; copy those two things only) | |
| small catalog with revision conflicts | `apps/mdedit/mdedit_ops.go` | |
| an agent-caused job that carries its delegation | `apps/watchbilldemo/watchbilldemo_ops.go` | |
| async command, agent limits | `apps/play/play_ops.go`, `play_agent_limits.go` | |
| regex battery filter row | `apps/play/play_snippets.go`, `apps/imztop/imztop_panel_proc.go` | `strings.Contains` over a lower-cased name |
| regex and SQL filters over a store | `apps/tally/tally_find.go`, jackstay's row filter | |
| tasks and the monitor | `apps/taskdemo`, `apps/watchbilldemo` | a raw `c.ProgressBar` for a job |
| lazypane and keyed memo | `apps/imztop`, `apps/play` | |
| token-derived theme | `apps/imztop/imztop_theme.go` | a `color.Hex(0x…)` literal (L16) |
| scene that ends on F1 | `apps/capinspector/scenes` (its one coordinate click is a recorded deferral in `archgraph.go`) | scenes with coordinate clicks |
| attention question | `apps/chat/chat_attention.go` | |
| labelled clipboard copy | `apps/chat/chat_actions.go` | a raw `SubjectWrite` request (CS015) |

## 12. What this skill does not decide

Gaps this page names and does not fill; each is its own ADR when a
consumer arrives.

- A runtime write gate for a statement with a body, so play's environment
  gates of ADR-0270 §SD7 can go (X4).
- Graduating L16, CS015, CS016 and CS017 from warn once their findings
  reach zero.
- A shared hit-highlighting widget (F2), a mid-tone background token the
  find overlays suppress a lint for, a hidden-window pause.
- A conformance sweep over catalogs comparable to `widgets/conformance`.
- Where a `package main` command under `apps/` belongs (§10).
- A reason parameter on `task.ProducerCaps()` (S3).

The survey that found most of these is
[keelson-app-adherence-survey](../../adr-background-work/keelson-app-adherence-survey.md).
