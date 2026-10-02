---
type: adr
status: proposed
date: 2026-09-30
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0269: App operations — a command/query contract apps declare and agents drive under a task grant

## Context

A conversation with a model should be able to operate the apps a person has
open — read their state and data, change it, run and cancel work, capture what
a window shows — across several apps and several instances of one app, with
every change appearing in the ordinary UI and mixing with the person's own
edits. Play is the first participant.

Observed on 2026-09-30, the runtime's pieces do not add up to this.

- **A model's tools run in the caller's process under the caller's grants**
  ([ADR-0254](./0254-model-inference-as-a-keelson-capability.md) §SD5). Play's
  tool loop is private to play and must end in SQL; the chat app has no tools
  ([ADR-0265](./0265-chat-app-over-retained-model-calls.md), proposed, §SD5).
  No mechanism lets a model act on another app.
- **The bus grants by subject, minted at Mount without a prompt.**
  [ADR-0026](./0026-app-runtime-and-capability-subjects.md) states its threat
  model as "hygiene, not security". Its §SD3 reserves
  `app.{id}.request.{name}`, without an instance token, and no service answers
  on it. A service answering there would act with its own authority on behalf
  of whoever asked.
- **Every imzero2 call runs on one render goroutine**
  ([ADR-0261](./0261-one-render-goroutine-for-every-app.md), proposed). Work
  from elsewhere hands its result to a later frame, with no generic mechanism
  for it, and an in-process bus handler runs on the requester's goroutine. A
  bound widget's value reaches Go state one frame later, and programmatic
  writes to a bound value go through `StateManager.OverrideDatabinding*`
  ([ADR-0267](./0267-imzero2-go-widget-api-contract-immediate-and-semi-retained.md)
  W10).
- **The accessibility tree and the driver serve development agents** — tools
  with a shell ([ADR-0154](./0154-headless-carrier-tree-and-driver.md) and its
  2026-09-18 Update). For a model working in a person's live windows a
  synthetic click carries no revision and no writer, and canvases (map, chart,
  graph, projection) carry no nodes.
- **Results carry a sensitivity label**, ordinary or confined
  ([ADR-0145](./0145-sealed-app-data.md)), and confined content must not reach
  a model endpoint that ADR-0254 §SD3's locality rule refuses. Anything a
  model reads — cells, documents, tool text, pixels — is also content an
  attacker can influence.

Two deputies are involved. An app that runs an agent's request with its own
database access is Hardy's confused deputy. The model is the other: it holds
the person's delegated authority while reading untrusted content, and a
confirmation the model decides on itself misses (OpenAI's ChatGPT agent system
card, 2025: 91% recall of model-decided confirmations on its evaluation set).
Credentials answer only the first.

Operation shape, authority and encoding are decided together because they meet
in one place, the host's dispatcher: an operation's declared effect is what
the grant is checked against, and what the dispatcher stamps is part of the
wire. A reading of play and the runtime, prior art, the options weighed, and
the probes behind the figures here are in
[app-operations-prior-art](../adr-background-work/app-operations-prior-art.md).

## Terms

| Term | Meaning here |
|---|---|
| person | the human at the windows |
| model | the language model; it acts only through tool calls |
| coordinator | an app instance that runs a model's tool loop for one conversation, presents that conversation's grant handle and is the grant's actor — the chat app ([ADR-0265](./0265-chat-app-over-retained-model-calls.md), proposed) |
| agent | a model acting through a coordinator; ADR-0154's agents with a shell are called development agents here |
| caller | any client of the host's `runtime.agent.*` services: a coordinator, or a scene or test holding a test grant (SD6) |
| instance | one window of an app, addressed by its host-minted, numeric instance key |
| task | one unit of delegated work within one conversation: one plan, one grant, one coordinator |
| grant | the host's record of what a task may do (SD6); one per task |
| grant handle | the opaque value a coordinator presents; honoured only from the coordinator's instance and at the grant's current epoch |
| epoch | a counter on the grant; bumping it invalidates the grant handle and every pending call of the task |
| resource | a unit of app state an operation declares it reads or writes — a document, a parameter, a pane binding — with its own revision |
| revision | a counter per resource, bumped by every change to it from any writer |
| dispatcher | the host's check of every call (SD6); distinct from play's endpoint dispatch ([ADR-0141](./0141-play-endpoint-dispatch-seam.md)) |
| label | ADR-0145's sensitivity: ordinary or confined |
| taint | a flag on a conversation, set once untrusted content has entered the model's context (SD7) |
| result reference | a reference to an immutable result the caller may `read` |
| data handle | a reference to content the model may pass as an argument but not read |
| job handle, artifact handle | references to work in progress and to a capture |

## Design space (QOC)

**Q1 — How does an agent act on an app?**

- **O1** — replay input through the accessibility tree.
- **O2** — write resource fields: REST-style updates, or SQL `UPDATE` over
  virtual tables.
- **O3** — commands and queries the app declares and implements.
- **O4** — a sandboxed script executor that calls the app.

Criteria: **C1** a stale write is detected and the person can override;
**C2** reaches canvases and data, not only widgets; **C3** per-app authoring
cost; **C4** does not break silently under a layout change or refactor;
**C5** authority is checkable per call against a declared purpose; **C6** a
reviewer can see what a call will change before approving it.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | +  | +  | +  |
| C2 | −− | +  | +  | +  |
| C3 | ++ | −  | −  | −  |
| C4 | −− | +  | +  | +  |
| C5 | −  | +  | ++ | −  |
| C6 | −− | −  | ++ | −− |

O3 is chosen; it and O2 both detect stale writes with revisions, and O3 wins on
C5 and C6 because an operation declares its effect where a field write leaves
it implicit. O1 costs nothing per app but a click has no revision, no writer
and no declared meaning, and layout changes retarget it. O2's SQL form also
needs a grammar rule and both statement classifiers changed in one step, or a
parsed `UPDATE` classifies as a read (`ClassifyStatementKind`,
`ClassifyQuerySecurity`; code reading, 2026-09-29), and modelling verbs as
field writes is the trap AIP-216 and Apple's TN2106 describe. O4's authority
is checked per inner call, but no reviewer can see a program's effect before
it runs; its likely engine, wazero in interpreter mode (the mode that runs on
every build target and generates no native code), measured about 100× slower
than the compiler on H3's `BenchmarkLatLngsToCells` (2026-09-29, one run on a
loaded machine: 460 ms against 4.3 ms per 1k points).

**Q2 — Where does an agent's authority come from?**

- **O1** — the bus grants alone: the coordinator may publish to an app's
  subjects, and the app acts with its own access.
- **O2** — a grant the host issues per task and checks on every call; the app
  does agent-caused work under agent limits.
- **O3** — attenuable tokens (macaroon or Biscuit style) passed along the chain.
- **O4** — confirmations the model asks for when it judges an action
  consequential.

Criteria: **C1** closes the app deputy; **C2** bounds the model deputy;
**C3** scoped to instances, operations, destinations, budgets and a deadline;
**C4** revocable at once; **C5** cost in one process.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | ++ | ++ | −  |
| C2 | −− | +  | −− | −  |
| C3 | −  | ++ | ++ | −− |
| C4 | −  | ++ | +  | −  |
| C5 | ++ | +  | −− | +  |

O2 is chosen. Its C2 rating holds only together with SD7's labels and taint,
which a grant makes checkable at the dispatcher; tokens carry no label and do
nothing for the model deputy, and inside one process any code can read their
key, so O3 pays for portability the runtime lacks. O4 misses measurably (the
Context's 91%) and is a layer, never the gate.

**Q3 — How are messages encoded?** JSON on the bus is excluded by
[ADR-0036](./0036-runtime-buscodec.md). Arrow for every body puts a schema in
each one-row message, and the Arrow path in this tree (`chrows`, 2026-09-29)
carries no Tuple or Map columns, so nested arguments would be flattened. SQL
`Values` text refuses non-UTF-8 bytes and types integers by value (probes,
2026-09-29). Chosen: CBOR messages, Arrow for tables by reference, JSON at the
model edge (SD10).

## Decision

We will give apps an optional interface of commands and queries, declared in
the app's manifest and discoverable before any window opens, served per
instance on the bus, applied on the render goroutine after each frame's
widget values are written back, and reachable by agents only through a host
dispatcher that checks a task grant the person approved.

```text
 model ⇄ coordinator (tool loop; holds a grant handle, never the grant)
           │ runtime.agent.call {handle, instance, operation, args,
           │                     expects, key, reason}
           ↓
 ┌─────────────────────────────── host ───────────────────────────────┐
 │ dispatcher: handle → grant · entry covers instance and operation?  │
 │   mode allows the effect? budget, deadline, epoch, busy?           │
 │   confirmation due? → stamp on-behalf-of context  → action record  │
 └────────────────┬───────────────────────────────────↑───────────────┘
                  │ app.{id}.{instance}.op.{name}       │ outcome; data by
                  ↓                                     │ reference, labelled
 ┌─────────── app instance, on its render goroutine ────┴───────────────┐
 │ frame: widgets draw; the person's input arrives                      │
 │ end:   values written back → queued commands applied → snapshot      │
 └──────────────────────────────────────────────────────────────────────┘
```

### SD1 — Commands and queries, one model

The contract separates commands from queries (Meyer's command–query
separation). It does not adopt CQRS's separate read and write models or event
sourcing: the app's state is the one model, and the read side is a snapshot of
it.

| Class | Rule |
|---|---|
| Query | no effect; a function of its arguments and a snapshot. The outcome names the snapshot (`as_of`) and the revisions of the resources it read. Result data comes back as a result reference, read through the host (SD3). |
| External read | reads outside the app through a fixed, bounded probe the app owns — a documentation lookup, a function listing — never a statement the caller writes; declared as such because it is not a function of the app's state. |
| Command | changes state and returns an outcome — phase, new revisions, a job handle, a result reference — never data. |

- A statement the caller writes is never a query. Running one is a command
  whose outcome is a result reference, which the caller then reads.
- A query never acts. Capturing a pane that is not raised is refused (SD11);
  a derived result never computed reads as "not computed".
- Every call carries a caller-minted key, scoped to its task and kept for the
  task's life. A repeat returns the first outcome. Commands set values and
  never toggle them, so a repeat that finds the value already set reports
  `applied`.
- A command carries the revisions it expects of the resources it writes,
  taken from the outcomes the model read; when it carries none, the host uses
  the revisions of the task's last read of those resources, and a command to
  a resource the task has not read is a `conflict` that asks it to read first.
- Revisions are kept per resource, never one per instance, so an animation
  cannot invalidate a document edit. A selection names the result it indexes.
- The snapshot kept per instance is the latest; asking for an older `as_of`
  returns `expired`. A result lives while the task or the instance that
  produced it lives.
- The app's query view is built only for a frame in which someone can read
  it: a task is attached, or a query waits. A query that finds no view (the
  first after an attachment) waits for the next frame, bounded above the
  host's idle heartbeat, rather than being refused; an instance no task works
  in pays for its revisions only.

### SD2 — The catalog is a property of the app

`app.Manifest` gains an optional operations catalog. Each operation declares:

- name, version, class (SD1), and effect (SD5);
- the resources it reads and, for a command, writes;
- argument and result types as Go values — records may nest — with scalar
  columns of tables in canonical types, and which arguments are references;
- effects that follow from it (a parameter write reruns a Live query);
- whether agents may call it — off unless declared — and whether its output
  may carry untrusted content;
- the UI gesture that does the same, or that there is none.

The app implements the operations behind an interface the window host calls:
commands on the render goroutine, queries as functions of the latest snapshot
off it. The catalog requires per-instance registration (`RegisterFactory`), as
a workingset does, and a catalog on a singleton registration fails validation.
A catalog that fails validation is withdrawn with a diagnostic and the app
still registers. The host may withdraw
an operation from agents or declare it consequential; it never loosens a
catalog. A `keelson('app_operations')` table lists every registered catalog,
so a caller discovers what an app can do before any window is open; the app
center ([ADR-0260](./0260-app-center-one-page-per-app.md), proposed) shows the
list to people. What a running instance adds is availability — whether an
operation can run now, and why not — through `describe` (SD3).

CBOR messages use `buscodec`'s reflection over the Go types, adding no code
generation step (ADR-0036). Tool schemas for the model and the documentation
are derived from the catalog by reflection; there is no second,
hand-maintained description.

### SD3 — Addressing and host services

Operations are addressed to an instance: `app.{id}.{instance}.op.{name}`,
ADR-0026's notation. The family replaces §SD3's `request` reservation; instance
keys are numeric, so it cannot collide with the `app.{id}.event.{name}` and
`app.{id}.request.{name}` families. Only the host publishes on it; a handler
accepts a message only from the host's identity; registration rejects any
other app's capability whose pattern can match an operation subject. Apps
report changes to the host directly, and the host sends each coordinator events
for its task on `runtime.agent.event.{task}`: kind, instance, resource, a job
handle for work the app started on the task's behalf, and a per-instance
sequence number, never content. A coordinator that misses events
reads state again.

Callers reach operations through the host's services. The services answer off
the requester's goroutine and refuse a request made from a render goroutine.
The check reads the goroutine id as ADR-0261 SD2's does; it is always on here,
where SD2's is opt-in for its cost per FFFI message. A coordinator's tool loop
runs off the render goroutine, as the chat's turns do.

| Service | Needs a grant | Does |
|---|---|---|
| `describe` | no | an app's operations, or those matching a search, with schemas and effects; with a grant, one instance's availability |
| `request` | no; a widening presents the grant handle | start a task or widen its grant: a plan, the operations per app, entries, destinations; returns a key and stays pending until the person decides |
| `turn` | yes | start a model turn: the changes by the person and other tasks since the previous `turn`, labelled as reads are (SD7); lifts the task's pauses |
| `list` | yes | the task's instances, their apps and titles, how far each has loaded, and which task holds each (titles and mount errors are marked untrusted; titles are withheld for a confined window) |
| `launch` | yes | open a window of an app the grant names, with a launch request ([ADR-0135](./0135-app-launch-requests.md)); returns the instance and how far it has loaded |
| `call` | yes | one command or query, with its key, expected revisions and a one-line reason |
| `status` | yes; for a `request` key, no, and only the requesting instance may ask | a call's or a job's phase by key, optionally waiting a bounded time; for a request, the grant handle once approved, or `rejected` or `expired` |
| `cancel` | yes | withdraw a queued call or a proposal by its key, or stop work a job handle names |
| `read` | yes | a result reference as model JSON, or an artifact handle as an image, applying labels, budgets and untrusted marking (SD7, SD10) |
| `capture` | yes | a visible pane or window, as an artifact handle (SD11) |
| `detach` | yes | remove one instance from the task |
| `stop` | yes | end the task |

A window is *opening* until its app's `Mount` has returned — which happens
in the first frame that draws its body — then *ready*, or *failed* with the
mount error. `launch` waits a short bound for ready (and, for an app with a
catalog, for its first snapshot) and then reports the window as it stands,
so a caller never reports a window open that has not drawn, or that failed;
a window still opening shows its load in `list`. The host keeps frames
coming for a bounded time while a window is opening, so the window's first
frame does not wait for the person to move the mouse; it cannot wake an
idle render loop from off it, so the first frame after an open still needs
one already running.

`list`, `capture` and `read` need an entry for the instance they name, in any
mode, observe included; `read` opens only references the task received.
Accepting a proposal, confirming a command, undoing a change, changing a mode
and sharing one of the person's windows are the person's acts in host chrome,
never services a caller can use. Progress of long work is read with
`status` until the streaming reply channel of
[ADR-0143](./0143-bus-streaming-reply-channel.md) (accepted, not built on
2026-09-30) exists. A coordinator gives its model a few fixed tools over these
services and loads an operation's schema when the model asks for it; the tool
loop itself belongs to ADR-0265.

### SD4 — The frame: the person's input first, then commands

The window host keeps one queue per instance. The dispatcher enqueues; the
render goroutine applies, after imzero2 has written the frame's widget values
back. The person's changes of that frame have then bumped their resources'
revisions, so a command expecting the old revision is a conflict: the person
wins a tie. While an instance has a task attached, the host requests a
repaint from inside each frame at a bounded interval, so a queued command
waits at most that long.

```text
 frame N, on the render goroutine
 ├─ body   widgets draw; the person's clicks and keys arrive; a person's
 │         gesture the catalog exposes calls its handler directly
 ├─ end    imzero2 writes bound widget values back; the host records the
 │         person's changes and bumps their revisions
 └─ then   apply queued commands in order: re-check epoch, expected
           revisions, focus, availability and pause; write bound values
           through the widget's programmatic path (ADR-0267 W10); report
           applied; take the snapshot (as_of N). Frame N+1 draws the
           effects: rendered.
```

A command to a resource whose widget has keyboard focus, or received the
person's input in this frame, returns `conflict` with the reason that the
person is editing. The first things M2 demonstrates are bumping the revisions
of bound resources from what the write-back changed — the host compares bound
values before and after it — and where a widget's programmatic path (ADR-0267
W10) takes effect relative to the next write-back.

A call moves through three stages:

```text
 dispatcher (answered by call)
   denied          coordinator not registered, operation not exposed to
                   agents, handle invalid or from another instance
   input_required ──→ accepted | proposed | rejected | expired
                   outside the grant — an instance, operation, mode,
                   destination or budget it does not cover: pending a
                   widening by the person (SD5)
   refused         busy: another task holds the instance
   proposed ──→ accepted | rejected | stale | expired | cancelled
                   suggest mode, or a consequential command; accepting a
                   consequential proposal is its confirmation
   accepted        a command, queued
   completed | failed
                   a query, answered from the latest snapshot off the
                   render goroutine; an external read starts work instead
 queue (read with status)
   accepted ──→ applied ──→ rendered
            ├─→ conflict   an expected revision moved, none is known for a
            │              resource it writes (read first), or the person is
            │              editing it; carries the current value
            ├─→ refused    unavailable, precondition failed, or paused
            ├─→ proposed   the mode lowered to suggest
            ├─→ cancelled  withdrawn by cancel
            └─→ expired    detached, stopped, deadline passed, or the mode
                           lowered to observe
 work (a job handle, or an event for work the app started)
   running ──→ completed | failed | cancelled
                   failed carries the reason, including "agent limit" (SD6)
```

A proposal becomes `stale` when a revision it expects moves before the person
accepts it. After a timeout a caller asks `status` by key before retrying. One
task at a time holds an instance in suggest or act mode; observing is shared.

### SD5 — Effects, modes and when the person is asked

Every operation declares one effect:

| Effect | Meaning |
|---|---|
| none | queries and external reads |
| view | changes what a window shows, not what it holds: a selection, a camera |
| document | changes authored state: text, parameters, signals, pane bindings and options |
| run | executes against a data source under the agent limits (SD6) and produces a result |
| consequential | writes outside the app: pinning, publishing, adjudication, export |

Each grant entry — one task, one instance — carries a mode:

| Mode | none | view, document, run | consequential |
|---|---|---|---|
| observe | yes | input_required: raise the mode | input_required: raise the mode |
| suggest | yes | proposed | proposed; accepting confirms |
| act | yes | applied, undoable in the UI | proposed; confirmed each time |

An instance the task launches starts in the mode its entry names, act unless
the plan says otherwise. One of the person's windows enters a task in the mode
the person picks when sharing it, observe unless the person picks more.

```text
                                        ┌─ attached ──────────────────────┐
 detached ── shared: the mode the ─────→│  observe ⇄ suggest ⇄ act        │
    ↑        person picks; launched:    │  up: the person, from the       │
    │        the entry's mode           │  badge or approving a widening  │
    │                                   │  down: the person, to any       │
    │                                   │  lower mode, at once, announced │
    │                                   └────────────────┬────────────────┘
    └────────── detach · stop · window closes · deadline ┘
 badge flags: paused · tainted · confirmation due · busy (task) · proposals n
```

The person is asked at these moments and no others:

| Moment | Prompt |
|---|---|
| starting a task | once: the model's plan, the operations per app, the entries (instance or app to launch, mode), the endpoints, egress destinations and `keelson()` tables, the budgets; the person picks which of their open windows to share and in which mode; an instance another task holds is shown as busy |
| a consequential command | every time, in suggest and act; in observe the call first asks to raise the mode |
| a widening — another instance, operation, mode, destination or budget | every time; while the conversation is tainted, host-derived facts come first and the model's reason last, as its claim |

Sharing a further window from its badge and changing a mode there are the
person's own acts, not prompts. Proposals are not prompts, except a
consequential one, which opens a confirmation in host chrome and sets
"confirmation due"; accepting it is the confirmation. Other proposals wait on
the badge until the person accepts or rejects them in host chrome. Changes of
mode, pauses, taint and pending confirmations show on the window's badge and
in the coordinator's log; nothing changes mode silently. Lowering a mode to suggest turns queued commands into
proposals; lowering it to observe expires them; raising a mode does not apply
earlier proposals. The model can ask for a mode change; only the person makes
one.

### SD6 — Authority: the task grant

```text
 person ──starts the task──→ grant (a row in the host's table)
                             principal: the person   actor: coordinator#3
                             operations per app · entries: instance · mode
                             destinations · budgets · deadline · epoch
                             plan and its hash
                                │
 coordinator#3 presents the grant handle; honoured only from coordinator#3
    │ call
    ↓
 dispatcher checks, then stamps
   {task, principal, act: person → coordinator#3 → play#2}
    │
    ↓
 play#2 does agent-caused work under the agent limits; its onward requests
    │ carry the context, and host services check it against the grant
    ↓
 outcome ── labels (SD7) ──→ coordinator#3    (confined content: a data handle)
```

- **Coordinators.** Only an app the person has registered as a coordinator in
  host settings may request a grant; the `runtime.agent.*` capability in its
  manifest is necessary and not sufficient.
- **Task and conversation.** A task belongs to one conversation. Starting a
  new conversation, or resuming another, stops the task.
- **The grant.** One per task: principal, actor instance, the approved plan
  and its hash, entries, destinations, budgets (calls, rows and bytes,
  launches, captures, wall time), a deadline and an epoch.
  - The grant lists the operations the plan needs per app. An entry covers
    an instance, in a mode, for the operations listed for its app. An entry
    may instead name an app the task may launch, with a mode and a count;
    each launch adds an instance entry, and each window the person shares
    adds one in the mode the person picked.
  - Destinations are the endpoints, egress destinations and `keelson()`
    tables agent-caused work may reach.
  - The first `request` of a task creates the grant when the person approves
    it; later requests widen it.
  - The grant is not secret — `keelson('agent_grants')` lists it; the handle
    is never stored in a table, and what protects it is its binding to the
    coordinator's instance and the epoch.
- **The plan.** The dispatcher enforces entries, not steps. The plan and its
  hash are what the person approved and what every action record names; a
  call outside the entries is a widening.
- **The dispatcher decides.** On every call it resolves the handle, checks the
  entry, the operation, the mode against the operation's effect,
  destinations, budgets, deadline, epoch and busy, decides whether a
  confirmation is due, then stamps an on-behalf-of context the callee reads
  and cannot change.
- **Agent-caused work runs under agent limits.** Work is agent-caused when a
  call starts it — a command, or an external read — or when the app starts it
  because of an input a task wrote: a Live rerun after a parameter write, a
  pane fetching for a camera the task moved, a window launched with a
  configuration the task supplied. Work the person starts is the person's;
  text a task wrote stays marked in the window until the person edits or runs
  it. The app carries the on-behalf-of context with agent-caused work and on
  every onward request it makes for it; host services that reach outside —
  the HTTP egress service ([ADR-0262](./0262-http-egress-as-a-keelson-capability.md),
  proposed) and the model service — refuse a destination the grant does not
  list. For runs against a database:
  - the endpoint is one of the grant's destinations;
  - the statement classifies as a read that names nothing outside the
    endpoint (`ClassifyQuerySecurity`'s read class; mutating, egress-reading
    and unclassifiable statements fail before they are sent);
  - every agent run sends `readonly = 2`, whatever `BOXER_PLAY_ALLOW_WRITES`
    says, so the server refuses writes and DDL;
  - `keelson()` and play's other client-side macros read the host's own
    plane; in an agent run they may name only tables the grant lists as
    destinations;
  - views, dictionaries, table engines and functions can still reach beyond
    the endpoint without the statement naming it, as the classifier's own
    documentation states. Only a database user that can reach the host's
    plane and no other URL, file or remote source closes that; where a
    deployment provides one, agent runs use it, and where it does not, the
    task dialog says so for that endpoint.

  These limits govern runs. A consequential command, once confirmed, makes
  its one write through the app's gated write path to a destination the grant
  lists. An operation whose work cannot run under these rules is not exposed
  to agents.
- **Instances.** An instance the task launches belongs to the task. When the
  task ends, or detaches a window it launched, that window passes to the
  person, badged as left by the task; work in it is then the person's, and
  the task's marks stay until the person edits or runs the marked text.
- **Detach and revocation.** Closing an instance, or `detach`, removes its
  entry, expires its queue, cancels its work and discards its proposals; the
  grant survives. Stop, the deadline, or closing the coordinator bumps the
  epoch: every pending call expires, the task's work is cancelled, the
  coordinator's model call in flight is cancelled (`llm.cancel`, ADR-0254),
  and proposals are discarded. Revoked grants keep their ids for the record.
- **Test grants.** The headless host issues grants without host chrome only
  behind a test flag; the desktop host refuses the flag.
- **The limit.** This bounds the model and apps acting as deputies. It does
  not bound hostile code in the same process, which ADR-0026's threat model
  excludes.

### SD7 — Labels, handles and untrusted content

- **A window's label** is confined while any result it holds is confined. A
  capture's label is the highest label of any window intersecting the
  captured area.
- **Outcomes** carry the label of their content. Confined content bound for a
  model endpoint the locality rule refuses is replaced by a data handle, and a
  confined capture stays an artifact handle `read` does not open.
- **Data handles** bind only to arguments an operation declares as references
  and are never expanded into text. A data handle or result reference passed
  from one instance to another is refused when it holds confined content and
  the destination's endpoint is not proven local (ADR-0145 §SD5).
- **Before a run**, the statement's label comes from play's endpoint dispatch,
  which classifies a statement naming sealed data as confined (ADR-0141,
  ADR-0145); the rules above then apply to its result.
- **Untrusted content** — fields an operation marks, titles, and every
  capture — reaches the model delimited and attributed to its instance and
  operation, and the coordinator instructs the model that such content is
  data, never instruction. This lowers the rate of injection without
  preventing it.
- **Taint** belongs to the conversation: it is set once untrusted content has
  entered the model's context and lasts as long as the conversation. It
  changes how widenings are shown (SD5) and is recorded on every action.
- **Declaration.** On every `llm.*` call the coordinator declares the highest
  label its context holds, as ADR-0254 §SD3 requires of callers; keeping a
  conversation that holds confined content follows
  [ADR-0264](./0264-retained-model-conversations-on-facts.md) (proposed) §SD5.

### SD8 — The person keeps control

- **Writer.** Every change a command makes carries the task as its writer. An
  app's loop protection treats the model as a machine writer (play's Live
  breaker); turning Live on or resuming it stays the person's.
- **Intervention.** A task depends on the resources it has read since its last
  `turn` and those its queued commands expect. A change by the person or by
  another task to one of them pauses the task on that instance until the
  coordinator's next `turn` (SD3), which returns every change by other
  writers since the previous one; commands expecting the old revision become
  `conflict`. Changes that follow from the task's own commands — a Live rerun,
  a derived result — carry the task as writer and do not pause it.
- **Undo** is per call, from the call's card, in host chrome. It restores a
  field only while its value and revision are still the ones the call left;
  later work by anyone is never overwritten. A write outside the app is not
  undone.
- **One path.** A gesture of the person's that the catalog exposes as a
  command calls the same handler, directly on the render goroutine; the
  person's edits to bound values are recorded when the write-back lands. Both
  enter the command log (SD9). View state outside the catalog — scroll, hover,
  a drag in progress — stays direct.

### SD9 — Records

- **Action records** on `boxer.facts`: one row when the dispatcher decides and
  one at the call's final phase; calls still pending when a task ends close as
  `expired`. A row carries task, conversation and tool-call ids, the act chain,
  instance, operation, a digest of the arguments, decision and rule, phase,
  labels, taint, the confirmation that applied, and budget left. Field names
  follow the OpenTelemetry GenAI conventions where one exists (conversation
  id, tool-call id, operation). Rows join ADR-0254's model-call records by
  call id. The bus audit records requests only and cannot say what happened
  inside one.
- **A command log per instance** holds every writer's commands. It feeds the
  changes `turn` returns, undo and replay in scenes; it is not how an app
  persists.
- **Tables:** `keelson('app_operations')`, `keelson('agent_grants')`,
  `keelson('agent_actions')`.

### SD10 — Encodings

| Boundary | Encoding |
|---|---|
| Calls, outcomes, events | CBOR through `buscodec`, versioned per operation |
| Tables: result rows, lists, derived data | Arrow IPC through [`github.com/stergiotis/boxer/public/db/clickhouse/chrows`](../../public/db/clickhouse/chrows), by reference to an immutable result |
| The model | JSON the host produces from Arrow in `read`, validated against schemas derived from the catalog: 64-bit integers and decimals as strings, bytes as hex, missing, null and empty kept distinct; images from ordinary captures |
| Captures | an artifact handle with media type, size, frame and result identity, and label |

ClickHouse never formats JSON for a model: its JSON output leaves 64-bit
integers unquoted by default, and its Arrow output writes an enum as its
number (probes, 2026-09-29). Enums in results a catalog types are sent by
name; in results of statements a caller wrote they arrive as numbers unless
the statement casts them, and `read` reports each column's ClickHouse type.

### SD11 — Capture and observation

Capture belongs to the host, which owns the windows: PNG by default, SVG when
text or geometry inside a view matters. Only what a window shows can be
captured; a pane that is not raised is refused, so a capture never takes a tab
from the person, and a task that needs another view opens its own window. A
capture waits a bounded time for the view to settle and says whether it did.
`read` returns an ordinary capture to the model as an image. Agents are not
given synthetic input.

### SD12 — Deferred

- An accessibility tree for coordinators on the desktop host, which needs a
  Rust-to-Go tree fetch; development agents keep ADR-0154's driver.
- A wake from outside the render goroutine, replacing SD4's repaint interval.
- A SQL read surface over the catalog and state tables (the
  [ADR-0139](./0139-semantic-layer-text2dsl.md), proposed, grain).
- An MCP adapter over the host services, with the costs ADR-0154's
  2026-09-18 Update recorded against one.
- Operations for pane options beyond a first set, and setters for view state
  that has none.
- Cryptographic tokens, once a process boundary exists (the browser host of
  [ADR-0263](./0263-imzero2-browser-both-modules-in-one-worker-mesh-to-the-painter.md),
  a remote display).
- Policies over argument values beyond destinations; a local model for
  looking at confined content.
- **Separating planning from data** (the CaMeL and FIDES designs): a planner
  that never reads untrusted content, and values it passes without reading.
  Reopened by the first trial run in which injected content leads to a
  consequential command being proposed or applied.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `app.Manifest` in [`github.com/stergiotis/boxer/public/keelson/runtime/app`](../../public/keelson/runtime/app) | added: an optional operations catalog; a catalog failing validation is withdrawn, the app kept | registration; the manifest tests; the capslock app set |
| App contract | added: the interface through which the window host calls an app's operation handlers; apps report changes, a window's label, and agent-caused work to the host | ADR-0261 (proposed), which it extends; ADR-0267 W10 for writes to bound values |
| Capability subjects (ADR-0026 §SD3) | added: `app.{id}.{instance}.op.>`, `runtime.agent.>`, `runtime.agent.event.>`; the `request` reservation withdrawn; overlapping app capabilities rejected | a dated Update on ADR-0026; capinspector's registry, classifier and help |
| Bus wire (`buscodec`) | added: the `runtime.agent.*` requests and replies, operation calls and outcomes, events, and the on-behalf-of context on onward requests | the codec registrations |
| Host services that reach outside | added: a check of the on-behalf-of context against the grant's destinations | ADR-0262 (proposed) `runtime.http`; ADR-0254 `runtime.llm` |
| Window host | added: the dispatcher, a queue per instance applied after the write-back, the repaint interval, host capture, badges, host chrome for sharing, proposals, confirmations, undo and modes, the coordinator registry | ADR-0261 (proposed) |
| Play's run path | added: agent limits for agent-caused work; `readonly = 2` on every agent run | ADR-0141's endpoint seam |
| Deployment | added: an optional database user for agent runs that reaches the host's plane and no other URL, file or remote source | the endpoint configuration |
| Signal writer (ADR-0097) | added: the task as a writer | play's Live breaker |
| `keelson()` tables | added: `app_operations`, `agent_grants`, `agent_actions` | the introspection table docs |
| `boxer.facts` kinds | added: the action record | the runtime vocabulary cohort and its golden |
| ADR-0254 §SD5 | amended: a call from another app runs under the grant and the agent limits — the delegation primitive ADR-0254's Q2 declined to invent | a dated Update on ADR-0254 |
| ADR-0154's 2026-09-18 Update | scoped: the driver is the surface for development agents, not for models in a person's windows | a dated Update on ADR-0154 |
| ADR-0265 (proposed) | revised in place: the chat gains tools and becomes a coordinator; its title changes | ADR-0265 |

## Alternatives

The QOC tables carry the main options. Further options weighed:

- **Synthetic input for models in a person's windows.** Rejected: a click
  carries no revision, writer or declared effect, so none of SD5–SD8 can
  apply to it.
- **Full CQRS with separate read models or event sourcing.** Rejected: one
  frame of lag between a command and a snapshot is explicit (`as_of`, phases),
  and a second model adds complexity no reader needs.
- **Applying commands at the head of a frame.** Rejected: the person's input
  of a frame reaches Go state only at the frame's end, with the write-back, so
  a command applied at the head is checked against revisions that do not yet
  include it.
- **`readonly = 1` for agent runs.** Rejected: it refuses every per-query
  setting, including those play sends with each run; a probe
  (clickhouse-local, 2026-09-30) showed it refusing `url()` and `file()` as
  well, which is why SD6 relies on the statement classifier and a restricted
  database user for egress.
- **A confirmation for every command once the conversation is tainted.**
  Rejected: a task in play reads result cells almost at once, so act mode
  would become suggest mode after the first read; the confirmation stays on
  the consequential set, and taint changes how widenings are shown.
- **Read and operate entries beside the modes.** Rejected: two permission
  settings for one question; the mode on the entry is the one.
- **Tools per instance.** Rejected: the model's tool list would grow with open
  windows; operations take the instance as an argument.
- **The conversation inside a participating app's window** (play's Model tab).
  Rejected: it would end with that one participant and could not span apps;
  the coordinator is an app of its own, and closing it ends its task by
  design.
- **Pooled grants per coordinator app.** Rejected: a grant pooled at a host
  became authority for every tool that host ran (the Windows file grants in
  the survey); a grant binds to one task and one coordinator instance.
- **A workingset checkpoint per model turn as undo.** Rejected: a workingset
  leaves out live parameters, bindings and pane state, so a revert would be
  partial and could overwrite the person's later work.
- **Merging the person's typing with an agent's edit** (a CRDT or operational
  transform). Rejected: a conflict followed by a proposal never overwrites,
  while a merge of two SQL edits can be valid text and a wrong query.

## Consequences

### Positive

- One contract for every participating app; another app costs its catalog and
  handlers.
- A stale write is detected, every change is attributed, and the person's
  change wins within a frame once M2 demonstrates SD4's revision bumping.
- The person is asked at three named moments; a task inside its grant asks
  only for consequential commands.
- A model's reach is bounded by checks outside the model. Agent-caused runs
  cannot write, and cannot reach beyond their endpoint through anything the
  statement names; a restricted database user closes the rest where a
  deployment provides one. A write outside the app happens only as a
  confirmed consequential command.
- Discovery is a query, and scenes and tests use the same operations.

### Negative

- Every participating app writes a catalog and handlers, declares the
  resources its operations touch, and carries the on-behalf-of context with
  agent-caused work.
- The host grows a dispatcher, a grant table, a coordinator registry, host
  chrome for consent and records.
- A command takes effect one frame after the frame it is applied in; the
  render loop keeps repainting while any instance has a task attached.
- A task needs a plan before it can act; marking untrusted content lowers the
  rate of injection without preventing it.
- Without a restricted database user, agent runs can reach beyond the
  endpoint through objects the statement does not name.
- Labels are only as right as their source.
- The grant bounds the model and deputy apps, not hostile code in the same
  process.

### Neutral

- ADR-0154's driver and `imzero2 drive` remain the surface for tests and
  development agents.
- An MCP adapter and a SQL read surface can be added without changing the
  contract.

## Migration — Tier 1

- **Breaks.** Nothing: the contract is additive, and an app without a catalog
  is unchanged. No manifest held a capability that can match an operation
  subject (2026-09-30).
- **Old shape.** ADR-0026 §SD3's `app.{id}.request.{name}` reservation is
  withdrawn in favour of `app.{id}.{instance}.op.{name}`; nothing served it
  (2026-09-30).
- **Path.** Play's Model tab keeps its private tool loop until the
  coordinator's covers it.

## Verification plan — Tier 1

- **Lane: default `go test`.**
  - catalog validation withdraws a bad catalog and keeps the app;
  - dispatcher: each outcome of SD4's first stage from its cause, including
    an operation outside the entry, a mode too low and an exhausted budget
    as `input_required`, busy between two tasks, epoch and detach semantics;
  - a task's own Live rerun does not pause it; a change by the person does;
  - a confirmed consequential command writes once, through the app's gated
    path, to a listed destination;
  - `runtime.agent.*` requests from a render goroutine are refused;
  - a repeated key returns the first outcome;
  - on a test app, a command and a same-frame change by the person to one
    resource end with the person's value and the command in `conflict`; a
    command to a focused widget's resource is `conflict`;
  - mode conversion when a mode is lowered, stale proposals, pause and
    `turn`, undo only while value and revision match;
  - a command to a resource the task has not read is `conflict`;
  - agent limits: mutating, egress-reading and unclassifiable statements fail
    before sending; an agent run sends `readonly = 2` with writes allowed;
    a Live rerun caused by a task's parameter write runs under the limits;
    an onward egress request to a destination outside the grant is refused;
  - capture of a pane that is not raised is refused; a capture's label is the
    highest of the windows it intersects;
  - a widening prompt shows host-derived facts before the model's reason;
  - model JSON: 64-bit integers as strings, bytes as hex, catalog enums by
    name;
  - labels: a confined result, capture or reference yields a handle;
  - a golden of the action record and of the catalog table.
- **Lane: headless scene.** A coordinator backed by a stub model service,
  under a test grant, edits play's document, runs and captures while the scene
  types into the editor; the scene asserts the conflict path. A test asserts
  the desktop host refuses the test flag.
- **Lane: integration.** Against a ClickHouse server: an agent run is refused
  for `INSERT`, DDL and `url()`; a table over a URL engine passes the
  classifier and is refused only under the restricted database user; play's
  own per-query settings still pass; `keelson()` reads tables the grant lists
  and is refused for others; sealed reads behave as for the person.
- **Lane: capinspector.** Only the host publishes on
  `app.{id}.{instance}.op.>`; no app capability overlaps it.
- **Lane: trial.** A protocol under [doc/trials](../trials/README.md) runs
  tasks over play against a tool-capable model endpoint, some with injected
  instructions in result cells, and reports task success, attack success (a
  consequential command proposed or applied because of injected content) and
  prompts per task.
- **Gap.** Hostile code in the same process; wrong labels at the source;
  reach through database objects where no restricted user exists; and, until
  the trial has run, how well a model uses the contract — no tool-capable
  model had driven an app in this tree by 2026-09-30.

## Milestones

- **M1 — Catalog and discovery.** The manifest field and handler interface,
  validation that withdraws a catalog, `app_operations`, `describe`, schemas
  derived by reflection.
- **M2 — The frame and dispatch.** First SD4's revision bumping from the
  write-back and its focus rule; then subjects and sender checks, the
  per-instance queue and repaint interval, the three stages of phases, keys,
  expected revisions, `status` and `cancel`, the command log, `capture` of
  visible panes, action records; dispatch is checked against test grants,
  behind the headless host's test flag.
- **M3 — Grants and the person's control.** The coordinator registry,
  `request` and plan approval, entries with operations, destinations, modes,
  the badge and its host chrome (sharing, proposals, confirmations, undo, mode
  changes), detach, stop and epochs, labels and data handles, `read`, taint
  and marking, pause and `turn`, the agent limits and the on-behalf-of
  context in host services.
- **M4 — First participants.** Play's catalog under its own decision, after
  its pinning and Series adjudication write through the endpoint seam and its
  gate — until then they are not exposed to agents; and one table-shaped app
  (appstate or watchbill), so the contract is not drawn around play alone.
- **M5 — The coordinator.** ADR-0265 revised: the chat's tool loop over the
  host services, with schemas loaded on demand and its context's label
  declared on every model call.
- **M6 — The trial.** A stub model service for scenes, and the trial
  protocol with its first runs.

## Status

Proposed 2026-09-30 — awaiting review by the code owner. Play's catalog is a
separate decision built on this one; ADR-0265 is revised in place for M5.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers.

## References

- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — the subject taxonomy SD3 extends; the threat model SD6 keeps.
- [ADR-0036](./0036-runtime-buscodec.md) — the bus codec; no new code generation step.
- [ADR-0094](./0094-keelson-introspection-tables.md) — the tables SD2 and SD9 add to.
- [ADR-0097](./0097-play-reactive-query-graph.md) — play's signal store and its writer field.
- [ADR-0135](./0135-app-launch-requests.md) — launch requests behind `launch`.
- [ADR-0139](./0139-semantic-layer-text2dsl.md) (proposed) — the grain of the deferred SQL read surface.
- [ADR-0141](./0141-play-endpoint-dispatch-seam.md) — play's endpoint dispatch, where agent limits attach.
- [ADR-0143](./0143-bus-streaming-reply-channel.md) — accepted, not built on 2026-09-30; progress is read with `status` until it is.
- [ADR-0145](./0145-sealed-app-data.md) — labels, locality, the sealed wall.
- [ADR-0154](./0154-headless-carrier-tree-and-driver.md) — the driver, scoped to development agents.
- [ADR-0254](./0254-model-inference-as-a-keelson-capability.md) — the model service; §SD3 labels, §SD5 amended.
- [ADR-0257](./0257-clickhouse-arrow-results-into-column-structs.md) — `chrows`.
- [ADR-0260](./0260-app-center-one-page-per-app.md) (proposed) — where people see a catalog.
- [ADR-0261](./0261-one-render-goroutine-for-every-app.md) (proposed) — the render goroutine rule SD4 builds on.
- [ADR-0262](./0262-http-egress-as-a-keelson-capability.md) (proposed) — the HTTP egress service that checks destinations.
- [ADR-0263](./0263-imzero2-browser-both-modules-in-one-worker-mesh-to-the-painter.md) — a future process boundary.
- [ADR-0264](./0264-retained-model-conversations-on-facts.md) (proposed) — keeping conversations that hold confined content.
- [ADR-0265](./0265-chat-app-over-retained-model-calls.md) (proposed) — the chat app that becomes the coordinator.
- [ADR-0267](./0267-imzero2-go-widget-api-contract-immediate-and-semi-retained.md) — W10, writes to bound values.
- [app-operations-prior-art](../adr-background-work/app-operations-prior-art.md) — play and the runtime read, surveys, the options weighed, probes, sources for every external claim above (Hardy, Meyer, Fowler, Dolt, AIP-216, TN2106, the OpenAI system card, CaMeL, FIDES, OpenTelemetry).
