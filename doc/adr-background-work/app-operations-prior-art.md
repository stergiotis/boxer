---
type: explanation
audience: designers of the app operations contract (ADR-0269) and its reviewers
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# App operations: play, prior art and the options weighed

Background for [ADR-0269](../adr/0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
(proposed). §1–§3 state what the contract is for and read play — its first
participant — and the runtime under it. §4–§9 collect what other systems do
when an agent, a script or a second program operates a live application.
§10 records the design options weighed and where ADR-0269 took each element
from. §11 lists the probes run in this tree while the contract was drawn.

**Provenance.** §2 and §3 are readings of the tree on 2026-09-29 and
2026-09-30, from source and play's help corpus; the app was not run. The
surveys are clean-room: public specifications, documentation, blog posts and
papers only; no product's source code was read. Retrieved 2026-09-29 and
2026-09-30. A claim marked **[excerpt]** rests on a search-result excerpt
whose page was not opened; **[not re-checked]** marks a claim from background
knowledge that was not verified against a page. Every other external claim
rests on a page listed in §12.

## 1. Requirements

- R1 — A model operates an app interactively, in the same session as the
  person.
- R2 — Every model action shows where it lands, and the person can override
  it through the ordinary controls.
- R3 — The model can read the data, not only the UI.
- R4 — The model can capture a view as PNG or SVG.
- R5 — One conversation coordinates several apps and several instances of one
  app.

R5 places the conversation above any one app: closing one play window must
not end it.

## 2. Play

A ClickHouse SQL workbench with linked views. Its loop is SQL and parameters →
query execution → result views → interaction signals → parameters. The SQL
buffer is the authoring surface; the query graph is derived from it, never
authored separately. [Play's architecture overview](../explanation/play-architecture.md)
and [the tab registry](../../apps/play/play_tabs.go) are the orientation; some
help pages lag the code.

### 2.1 Queries and endpoints

- **Scope of a Run.** In a `;`-separated buffer, Run ships the statement under
  the caret with the leading `SET` prelude. Ctrl+Shift+Enter ships the
  innermost query at the caret — a subquery, a CTE body, a set-operation
  branch — with the WITH items it needs; the Subquery toggle draws that
  scope ([play_statements.go](../../apps/play/play_statements.go)).
- **Pre-execute rewrites** ([play_passes.go](../../apps/play/play_passes.go),
  ADR-0108): canonical form, then client-side macros — `keelson('…')`,
  `LW_*`, `gloss(…)`, `docsearch` — and the splice of SQL-valued parameters.
  The `ts*` family never ships; play computes it over a sub-query's rows. The
  authored buffer is therefore not always standalone ClickHouse SQL.
- **Query graph** ([ADR-0097](../adr/0097-play-reactive-query-graph.md)). The
  buffer's CTEs are nodes with the final `SELECT` as sink; demand-driven,
  memoised, with early cutoff on a content fingerprint. A pane can be bound to
  one node; bindings survive Runs by CTE name.
- **Result state** ([play_querystate.go](../../apps/play/play_querystate.go)):
  idle → running → rows, empty or failed, each with a stale twin entered when
  the buffer, a parameter or a referenced signal moves. Client-side
  cancellation does not prove the server stopped.
- **Endpoints** ([ADR-0141](../adr/0141-play-endpoint-dispatch-seam.md),
  [play_dispatch_policy.go](../../apps/play/play_dispatch_policy.go)). A base
  endpoint, optionally **Auto**: a read naming only `keelson()` tables goes to
  the in-process introspection engine, a mixed read is refused rather than
  guessed, and mutations are never moved. Every request issuer takes a
  dispatch decision as a required parameter. A foreign engine must fetch a
  single-use loopback URL before serving a confined run
  ([play_dispatch_reach.go](../../apps/play/play_dispatch_reach.go), ADR-0145
  §SD5) — stated in the source as a misconfiguration wall, not a security
  boundary.
- **Writes.** `INSERT … SELECT` is the one write a Run sends, gated on
  `BOXER_PLAY_ALLOW_WRITES`.

### 2.2 Parameters and signals

- A `{name:Type}` slot is **pinned** — a `SET param_<name> = …` line in the
  buffer, which shadows any signal of that name — or **live**, read from the
  shared signal store. Pin and unpin move a value between the two. `Expr` and
  `ExprList` are spliced into the text by play and pinned by
  `-- play: expr` (ADR-0187).
- One signal value per name across queries and panes. Panes write them as the
  person interacts: row selection, stable keys, map viewport, Timeline extent,
  vector-field time and view ([play_signal_decl.go](../../apps/play/play_signal_decl.go)).
  The store records each value's writer and a revision; the Graph pane shows
  them.
- **Live** reruns on a referenced signal's move, not on buffer edits, and
  switches itself off when a result keeps moving its own input.
- Row selection is scoped to a node; a row ordinal is not an identity across
  results, and a result without a key may leave the previous key in place
  ([play_bindings.go](../../apps/play/play_bindings.go)).

### 2.3 Tool panes

Docs, Preview (canonical and as-sent SQL), Flow (one statement's dataflow,
`EXPLAIN` views, column lineage), Passes, Diagnostics, Snippets, Model,
Vocabulary, Completion, Glosses and Experiments. Each is a function of the
buffer or an existing probe, so each is a natural query for a model.
Completion ([play_completion_panel.go](../../apps/play/play_completion_panel.go))
reads the call and argument at the caret and answers from component kinds,
introspection tables, session datasets and endpoint catalogs; an unanswered
probe reads "waiting for the endpoint", never as an empty list. The Model pane
([play_model_panel.go](https://github.com/stergiotis/boxer/blob/d1010551e0413e63590417b50b9bcfcc0abea278/apps/play/play_model_panel.go), ADR-0254 §SD6) is
an explain, fix and ask workflow whose tool loop over introspection reads ends
in SQL; it is not a general controller.

### 2.4 Result panes

Each pane declares typed input channels; a dispatcher offers each channel a
node's result, and a pane that rejects it says why
([play_panel_dispatch.go](../../apps/play/play_panel_dispatch.go)). Families:
records (Table, Detail, Schema), numeric (Chart, Distribution), time (Series,
Timeline), hierarchy and flow (Icicle, Treemap, Sankey), data graphs (Network,
Graphview), geography (World, Map, Vector field), collections (Kanban, Cards,
Chat, Files) and Projection. Inputs are not uniform: the graph panes read
named CTEs such as `edges` and `vertices`, Vector field reads `vector_field`,
and Map runs its own query. A pane binding does not retarget those private inputs.

### 2.5 Pins, writes and persistence

"Pin" names three operations: a parameter (§2.2), the base endpoint, and a
result. A result pin (ADR-0115 S4; removed 2026-10-02 by ADR-0270)
wrote the Arrow batch into a content-addressed `boxer.pin_<fingerprint>`
table plus a row in `boxer.resultsets` — a database write, not a frozen
screenshot. Persistence has separate scopes: session history restores SQL and
the signal values a run shipped; the workingset
([play_workingset.go](../../apps/play/play_workingset.go), ADR-0148) carries
authored buffers and some settings, not results; recorded runs depend on
capture being configured; pins are durable; ad-hoc datasets
([ADR-0240](../adr/0240-adhoc-datasets-v2-sealed-store-owned-capability.md))
are session-scoped.

### 2.6 What an operations contract has to account for

- The exported programmatic surface was about a dozen delivery methods
  (`ReplaceSql`, `ActivateTab`, `SetSignal`, `RequestRun`, `BindTab`, …). A
  draft declaration of everything a person can see or change came to 55
  resources (with 59 tables for their verbs, the 114 tables of §11). About
  twenty human-changeable states had no setter — map and graph cameras, graph
  selections, the treemap drill, the Files directory — and the dock's active
  tab lived on the Rust side only.
- One parameter write has two destinations: the SQL text when pinned, the
  signal store when live.
- Result pinning and Series adjudication
  ([play_series_labels.go](../../apps/play/play_series_labels.go)) write
  through the client's raw query path to the base endpoint — not necessarily
  where Auto sent the last run — without consulting
  `BOXER_PLAY_ALLOW_WRITES`, which gates only the run path.
- Play's run path sends `readonly = 2` when writes are not allowed
  ([play_client.go](../../apps/play/play_client.go)).
- Both statement classifiers (`ClassifyStatementKind`, `ClassifyQuerySecurity`
  in [the nanopass analysis package](../../public/db/clickhouse/dsl/nanopass/analysis))
  treat a statement grammar1 parses as a read, with the `INSERT … SELECT`
  wrapper as the one exception; a grammar rule for `UPDATE` would therefore
  read as a read until both change. `ClassifyQuerySecurity` has a class for
  reads that reach beyond the endpoint (egress table functions), and its
  documentation says views, dictionaries, table engines and functions can
  reach further.
- Canvases — map, chart, graph, projection — have no accessibility nodes.
- Reading the SQL and a result's metadata separately can race; `MainSnapshot`
  and `ResultID` ([play_resultid.go](../../apps/play/play_resultid.go)) are
  starting points, and the main result alone does not describe every pane.
- Glosses render values, including masking ones; a gloss is not a disclosure
  policy. A maximum over a sample is not the table's maximum, so a bounded
  read has to say it was sampled or truncated.
- A lazily drawn tab that is not raised has drawn nothing; capturing it means
  raising it, which the person sees.

## 3. The runtime underneath

- **Bus audit.** Every `Client.Request` is audited ([the audit package](../../public/keelson/runtime/audit));
  `Publish` is not. A subject in an audit row cannot say what happened inside
  a request.
- **Grants.** Manifest capabilities are minted at Mount without a prompt;
  [ADR-0026](../adr/0026-app-runtime-and-capability-subjects.md) calls its
  threat model "hygiene, not security", and its §SD3 `request` reservation
  had no service behind it. `RemoveCap` removes filters whose pattern equals
  the one given ([inprocbus](../../public/keelson/runtime/inprocbus/client.go)),
  so an overlapping wildcard grant survives; removing a grant does not cancel
  work already running.
- **Deputies.** [ADR-0254](../adr/0254-model-inference-as-a-keelson-capability.md)
  §SD5 runs a model's tools under the caller's own grants, and its Q2 declined
  to invent a delegation primitive. In-process checks govern cooperating code,
  not hostile native code. Sender attribution differs between transports, so
  an external adapter has to authenticate rather than assume the in-process
  envelope.
- **Sealed data.** Sealed ad-hoc storage protects its stated disk and
  lifetime threat model; it is not isolation per reader, and a reference id is
  not an authorization token.
- **Model service and chat.** The model service is request/reply; the
  streaming reply channel of [ADR-0143](../adr/0143-bus-streaming-reply-channel.md)
  was accepted and not built. The chat app
  ([ADR-0265](../adr/0265-chat-app-over-retained-model-calls.md), then proposed)
  has no tools.
- **Capture.** The host owns windows, so capture is uniform:
  `RequestScreenshotRect` for PNG and `ExportSvgWindow` for SVG, both completing
  at frame end. On 2026-09-27 the tables demo measured about 2.2k tokens as
  PNG against 35.7k as merged SVG (15.2k without fonts). SVG fidelity depends
  on the rendering path: cached textures are embedded, uncached ones skipped,
  paint callbacks need their own handling
  ([svgexport.rs](../../rust/imzero2/src/imzero2/svgexport.rs)).

## 4. State as tables, verbs as something else

Several systems expose a live system's state relationally and let a second
program change it.

- **Dolt** keeps state in read-only system tables and makes a table writable
  only where editing a row has one meaning: deleting a `dolt_conflicts_<t>`
  row resolves the conflict, flipping `dolt_workspace_<t>.staged` stages a
  row, an interactive rebase is a plan table edited with UPDATE/DELETE and
  applied by `CALL dolt_rebase('--continue')`. Its docs state the split:
  procedures "for all imperative CLI commands", system tables for inspecting
  state. Dolt moved `SELECT DOLT_COMMIT()` to `CALL DOLT_COMMIT()` in 2022 on
  the grounds that a SELECT should not change anything, and its Postgres
  sibling brought functions back in 2024 because Postgres procedures cannot
  return rows — a verb must return a result the caller can read.
- **PostgreSQL `pg_settings`** accepts UPDATE as the equivalent of SET;
  **MySQL `performance_schema`** setup tables are configured by UPDATE. MySQL
  also shows the trap: some instruments accept the UPDATE and change nothing
  until a restart. **osquery** tables accept mutation verbs and ignore them;
  its `curl` table issues an HTTP request when selected and was shown to
  pivot to cloud credentials — a side effect inside SELECT turned into a
  hole.
- **SQLite virtual tables** route an UPDATE through a scan and then one
  `xUpdate` call per row, keyed by a single-column key; PostgreSQL foreign
  data wrappers add row-identity columns for the same reason.
- **AppleScript** models an application "as a form of object-oriented
  database" with nouns and verbs; Apple's TN2106 draws the line between them:
  "when an action is initiated, use a command; when an attribute changes, use
  a property" (`play the movie`, not `set playing to true`). Cook's HOPL III
  paper records the cost: hand-written dictionaries were hard to get right
  and scripting support was dropped under schedule pressure.
- **Plan 9 acme** exposes each window as files (`ctl`, `addr`, `body`,
  `event`); small external programs and the person work on one live editor,
  and `nomark` groups a program's changes into one undo step.
- **Riffle** (Litt et al., 2022 essay and UIST 2023) kept UI state — sort,
  search, selection, scroll — in SQLite and edited a running app's UI from a
  generic SQL editor. Its costs: a round trip per keystroke, tree-shaped and
  tagged-union state that relations fit badly, and a persisted "playing" state
  that started playback at the next launch.

**Lessons.** Nouns as tables and verbs as commands; a table is writable only
where a row edit has one meaning; every write reports what happened; no side
effects inside a read; a surface maintained beside the application rots.

## 5. Agents and live applications

| System | Mechanism |
|---|---|
| WebMCP (W3C community group draft) | a page registers tools that act on its own state; annotations `readOnlyHint`, `consequentialHint`, `untrustedContentHint`; best practice: register tools only when usable in the page's state, and update the interface after a tool completes |
| Android AppFunctions | apps expose functions to agents; the caller needs a platform-gated permission; the operating system is the only dispatcher |
| Apple App Intents | entities found through queries are the nouns, intents the verbs; per-intent authentication policy that may only be made stricter; `requestConfirmation` |
| CopilotKit / AG-UI | readable state plus actions; shared state patched with JSON Patch, including `test`, applied all-or-none |
| MCP (2025-06-18 revision) | resources are application-driven, tools model-controlled; hosts "SHOULD" keep a human in the loop and show when tools run |
| Blender and Figma MCP servers | structured reads, code or edit tools, and a viewport screenshot; Figma orders observation from a sparse outline to full context to a screenshot |
| Microsoft UFO² | "API-first" hybrid of application APIs and GUI actions: +6.1 (GPT-4o) and +8.2 (o1) points of success over GUI actions alone on OSWorld office tasks, with fewer steps |
| Declarative Model Interface (2025) | declarative state primitives lifted 27 Office tasks from 44.4% to 74.1% success |

Observation: on OSWorld (2024, GPT-4 family) an accessibility tree scored
12.24% and a screenshot alone 5.26%; a 1080p screenshot costs about
1.5k–2.7k tokens for a Claude model. Code as the action language: CodeAct
reports up to 20% higher success than JSON actions; Cloudflare's "Code Mode"
covered a whole API with two tools in about 1,000 tokens against about 1.17M
as individual tools.

**Lessons.** Declared, domain-level operations beat GUI replay; structured
observation first, pixels for questions about what the view looks like; the
tool list should be discoverable rather than preloaded.

## 6. Protocol precedents

- **HTTP method semantics** already encode command–query separation: safe
  GET, idempotent PUT and DELETE, POST made repeatable by an idempotency key,
  preconditions with `If-Match` [not re-checked].
- **Google's API Improvement Proposals** state the rules as a set:
  long-running operations return an operation object (AIP-151); an etag
  mismatch "must send an ABORTED error" (AIP-154); a repeated request id
  returns the first response (AIP-155); `validate_only` returns what the real
  call would (AIP-163); field behaviours such as OUTPUT_ONLY and IMMUTABLE
  (AIP-203); lifecycle state is output-only, never set directly (AIP-216).
  Stripe saves the first result for an idempotency key "regardless of whether
  it succeeds or fails". Kubernetes separates desired `spec` from observed
  `status`, rejects a stale `resourceVersion` with 409, and reports
  `observedGeneration`.
- **LSP** exchanges edits against per-document versions; the server's
  `workspace/applyEdit` asks the client to apply edits the client may refuse;
  capabilities are agreed at `initialize`; requests can be cancelled
  [not re-checked].
- **Chrome DevTools Protocol → WebDriver BiDi, Playwright**: attach to a
  target to get a session, commands grouped in domains, events after
  subscribing. CDP grew large and unstable; BiDi standardises a small core
  [not re-checked].
- **Elm and Redux**: `update : Msg → Model → (Model, Cmd Msg)`, the view a
  function of the model, actions as data that a devtool can dispatch from
  outside; the communities' advice is to keep ephemeral view state local
  [not re-checked].
- **Web workers**: one thread owns the page; others post messages; large data
  moves as transferable buffers with an owner; RPC proxies that make messages
  look like local calls hide latency and failure [not re-checked].
- **Figma multiplayer**: the server referees and the last writer wins per
  property, so conflicts are rare because the unit is small [not re-checked].

## 7. CQS and CQRS

Fowler: "for most systems CQRS adds risky complexity", and it "should only be
used on specific portions of a system … and not the system as a whole".
Meyer's command–query separation is the method-level rule [not re-checked]; CQRS adds separate
read and write models. The contract takes the first and not the second.

## 8. Authority and delegation

**Two deputies.** Hardy (1988): a compiler with a licence to write its home
files wrote output to a file the user named, because "the compiler runs with
authority stemming from two sources" and "had no way of expressing these
intents"; the fix was to pass a capability that "both identifies the file and
authorizes the compiler to write there". An app that runs an agent's request
with its own access is this deputy. The model is a second one: it holds the
user's delegated authority while reading content an attacker can influence;
tokens do not help there, deterministic checks outside the model do.

**Delegation standards.**

- OAuth Token Exchange (RFC 8693): delegation, not impersonation — the
  callee sees subject and actor; nested `act` claims form the chain.
- Rich Authorization Requests (RFC 9396): a grant as typed entries
  (locations, actions, data types); the issuer defines "subset".
- GNAP (RFC 9635): a continuable grant separate from per-use credentials.
- Transaction Tokens (IETF draft): an immutable call-chain context lasting
  minutes; a replacement "MUST NOT" expand scope.
- MCP authorization (2026-07-28 revision): tokens bound to their audience; no passthrough of a
  received token; step-up through `insufficient_scope`; "a handle is a name,
  not a capability" unless checked on every call; a call may return
  `input_required` and resume.
- AAuth (IETF draft): a *mission* — a task description the person approves,
  hashed and copied unchanged into every token — with a log and supervision
  of each request.
- Entra Agent ID and AWS AgentCore Identity: per-agent identities; the
  platform, not agent code, holds credentials. Entra's template-inherited
  permissions show how pooled grants reach every instance.

**Capability tokens.** Macaroons attenuate by caveats, with third-party
caveats discharged by another service (Fly.io used one to plug in an
approval step); Biscuit attenuates offline with Datalog checks, the verifier
supplying the facts; UCAN separates cacheable delegation from unique,
replay-tracked invocation; Fuchsia and seL4 let rights only shrink on copy
and revoke along the derivation tree. In one process a token's cryptography
protects nothing — any code can read the key — so their value arrives with a
process boundary.

**Platforms.**

| System | Durable grant | Per task | Per action |
|---|---|---|---|
| MCP | client × server scopes | step-up | human in the loop (SHOULD), elicitation |
| Android AppFunctions | platform-vetted caller, per-function switch | session target set, growth approved [excerpt] | — |
| Apple App Intents | per-intent policy, stricter-only overrides | — | confirmation from static metadata and runtime state |
| Windows agent features | agent account and workspace; file grants per host | — | sensitive steps approved |
| Chrome agentic browsing | — | read-only vs read-write origin sets, grown through a gate that sees no untrusted content | confirmations for sensitive classes |
| OpenAI agent | — | watch mode | confirmations the model decides (91% recall, ChatGPT agent system card, 2025) |
| Claude in Chrome | per site | — | high-risk confirmations; attack success 11.2% with mitigations against 23.6% without (autonomous mode, Anthropic's red-team set, pilot) |

Windows' per-host file grants are the counter-example: a grant pooled at the
host became authority for every tool that host ran.

**Research.** CaMeL attaches capabilities to values and checks policy at each
tool call (77% of AgentDojo tasks solved with provable security, against 84%
undefended); FIDES labels values with integrity and confidentiality, hides
values that would raise the planner's label, and checks "consequential only
from trusted inputs" and "egress only to permitted readers" before each call;
Progent narrows privileges automatically and widens them only with approval;
the design-patterns paper (2025) concludes that once an agent has read
untrusted input it must be impossible for that input to trigger consequential
actions; Willison's "lethal trifecta" and Meta's "rule of two" limit an agent
to two of untrusted input, private data, and the ability to change state or
communicate.

**Convergence.** A small durable grant; a task-scoped set that grows only
through a gate; confirmation for a narrow consequential class, triggered by
rules rather than by the model; consent surfaces the agent cannot reach; one
audit record per decision.

## 9. Human factors, control and evaluation

Lessons from practice outside the surveys above.

- **Automation surprises and mode confusion** (Sarter and Woods, cockpit
  studies): a system with modes invites "what is it doing now?". Mode changes
  must be announced, never silent [not re-checked]. **Levels of automation**
  (Parasuraman, Sheridan and Wickens, 2000) treat how much a system decides
  and acts as a scale rather than a switch [not re-checked].
- **Industrial control** answers the two-writer problem with a hand–off–auto
  switch, "local control wins", interlocks that block an action until a
  condition holds, and bumpless transfer between manual and automatic
  [not re-checked]. Suggest and act modes are that switch; a confirmation is
  an interlock; turning queued commands into proposals when leaving act mode
  is the bumpless part.
- **Plan approval.** AAuth's mission (§8) and Progent's task policy approve
  what an agent intends once, so later steps inside it need no prompt;
  CaMeL's authors list user fatigue among their design's costs.
- **Marking untrusted content.** Delimiting and attributing untrusted text
  before it enters a model's context — Microsoft's "spotlighting" — lowers
  injection rates without preventing injection [not re-checked]; Apple's
  `.historyTransform` delimits tool output for the same reason.
- **Schemas on demand.** Loading tool schemas when a model asks, instead of
  all at once, keeps the context small: Cloudflare's search-and-execute pair
  (§5); a tool-search tool in model APIs [not re-checked].
- **Measuring the agent.** CaMeL and FIDES report task success and attack
  success on the AgentDojo benchmark; a contract's tests say nothing about how
  well a model uses it.
- **Telemetry.** OpenTelemetry's GenAI conventions name conversation and
  tool-call ids and agent and tool operations, and have no on-behalf-of field.

## 10. Options weighed

ADR-0269's QOC tables and Alternatives carry the options it rejected and why.
This section keeps the comparison behind them.

### 10.1 Where the model acts

| Option | Outcome |
|---|---|
| Synthetic input through the accessibility tree and the driver | kept as a read-only view of any app, including one with no operations; for writes a click carries no revision or writer, a layout change retargets it silently, and canvases have no nodes (§2.6) |
| Code the model writes, run in a sandbox (wazero's interpreter) | the code would still need the same operations and the same authority, so it adds a language without removing a check; the interpreter ran about 100× slower than the compiler (§11) |
| Read-only tables plus writable virtual tables (SQL `UPDATE`) | a row edit has one meaning only for some state (§4); both classifiers would read `UPDATE` as a read (§2.6); kept: one declaration from which tables and schemas derive |
| Typed operations the app owns, over the bus | chosen |

### 10.2 Three shapes of the operation surface

| Axis | Curated operations plus a state read | Semantic operations with descriptors | Resources with field writes plus verbs |
|---|---|---|---|
| API shape | a short list per app, plus the whole workingset | operations declaring arguments, effects, preconditions, retry and undo | every visible state a resource; verbs beside it |
| Model's language | JSON tools per attached instance | JSON tools | SQL reads and writes |
| Encoding | not fixed | CBOR on the bus, Arrow by reference, JSON Schema at the model | Arrow for every body |
| Lifecycle | ok, conflict, refused, pending | accepted → applied → rendered → completed | operation state plus the frame drawn |
| The person intervenes | a summary of their changes at each model turn | the task pauses; dependent queued actions are invalidated | a revision check per write |
| Undo | a workingset checkpoint per turn | compensate only fields whose values still match | grouped by writer |
| Authority | the call runs inside the target app under its own grants; consent at attach | host-issued task capabilities; delegation designed explicitly | a grant per method; a switch per window |

All three put typed operations the app owns on the bus, tag the model as a
writer, check revisions, keep the queue and the capture with the host, label
every result, address instances, and keep pinning, endpoint changes, writes
and publishing behind a separate approval.

### 10.3 API style

| Style | Place |
|---|---|
| Typed request/reply over the bus | the transport: request/reply, subscriptions and capability checks exist |
| REST | a possible gateway for outside callers; indirect for frame acknowledgements |
| GraphQL | flexible reads; leaves mutation semantics, authority and concurrency open |
| MCP | an adapter over the same operations for outside agents, not a second implementation |
| Accessibility and clicks | tests and a read-only view (§10.1) |

### 10.4 Encodings

| Content | Encoding |
|---|---|
| model tools and compact observations | JSON with JSON Schema; 64-bit integers as strings, bytes as hex, enums by name |
| requests, replies and events on the bus | `buscodec` CBOR with explicit operation versions |
| action records | the facts codecs |
| tables | Arrow IPC through references, never inline in model context |
| captures | PNG or SVG bytes with type, size, provenance and label |

Missing, null and empty stay distinct; unknown fields in a command and
unsupported versions are refused; decode depth, payload, image size and row
counts are bounded. `buscodec`'s `json:` tags do not name CBOR fields, and
versioning is a payload concern.

### 10.5 Smaller choices

- **Where the conversation lives.** In an app of its own rather than a play
  pane, because of R5.
- **Cross-app work.** A task is a sequence with explicit dependencies, not a
  distributed transaction: partial completion is reported, and compensation
  happens only where an operation offers it. Hand-offs use what exists —
  ad-hoc datasets, launch requests, app events — and pass references, which
  distinguish a fixed snapshot from a reference that follows its source. A
  transfer between apps does not imply the data may reach the model.
- **The model's database activity** goes through the window's ordinary run
  path, where the person sees it. A separate inspection lane that leaves the person's main query in
  place, with its SQL, endpoint and an "open in editor", is left to play's
  catalog.
- **Outside agents.** An MCP bridge would be a thin adapter over the same
  operations — ADR-0254 §SD7's deferred external surface, generalised.
- **An end-to-end check.** "Plot these values, inspect an unusual point and
  capture the view", while the person changes the time range through the
  ordinary picker: the task keeps the person's choice and its dependent queued
  commands do not apply. The same run exercises stale reads, a retry after a
  timeout, an instance closing, partial cross-app completion, capture fidelity
  and confined data.

### 10.6 What ADR-0269 took from where

| ADR-0269 element | Source |
|---|---|
| command and query shape, lifecycle phases | §10.2, semantic operations |
| the summary of other writers' changes at each model turn | §10.2, curated operations |
| one declaration from which schemas and tables derive | §10.1, the table option |
| the task grant, labels and taint | §8 |
| plan approval, announced modes, schemas on demand, marking, OpenTelemetry names, the trial | §9 |

## 11. Probes in this tree

Each probe: one run on a loaded handheld machine; ClickHouse probes used
clickhouse-local 26.8. The Values and Arrow round trips were small Go programs
over the named packages, not kept; the statements of the settings probes
follow the table, and the wazero row is
`go test -run '^$' -bench 'BenchmarkLatLngsToCells$' -benchtime=200ms ./public/science/geo/h3/`.

| Date | Probe | Result |
|---|---|---|
| 2026-09-29 | wazero engines on H3's `BenchmarkLatLngsToCells` | interpreter about 100× slower than the compiler (1k points: 460 ms against 4.3 ms), with about 390 allocations per point against none |
| 2026-09-29 | ClickHouse `FORMAT Values` output → [`github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling`](../../public/db/clickhouse/dsl/marshalling) parse and serialize → `FORMAT Values` input | identical after writing each row as `(…)` instead of `tuple(…)`; integers typed by value (Int64 `2` reads as u64); timestamps and enums arrive as strings; non-UTF-8 bytes, which ClickHouse writes raw, are refused |
| 2026-09-29 | ArrowStream → [`github.com/stergiotis/boxer/public/db/clickhouse/chrows`](../../public/db/clickhouse/chrows) `DecodeRowsBytes` → `EncodeStream` → typed table | identical, including bytes `00 ff`, the uint64 maximum, the int64 minimum, nanosecond timestamps and NULL; Enum8 is written to Arrow as Int8; arrays are encoded as `Array(Nullable(…))`; no Tuple or Map columns |
| 2026-09-29 | generated DDL for a draft surface (114 tables) | loads once every column name is quoted: a column named `projection` starts a PROJECTION clause |
| 2026-09-29 | `output_format_json_quote_64bit_integers` | defaults to 0: `18446744073709551615` is written unquoted |
| 2026-09-30 | `readonly = 1` | refuses INSERT, changing any other setting, `url()` and `file()`; cannot be lowered; `SET param_x` still binds a parameter |
| 2026-09-30 | `readonly = 2` | allows changing settings; did not refuse the `url()` read |

```sql
SELECT toUInt64(18446744073709551615) AS u FORMAT JSONEachRow;  -- written unquoted
SET readonly = 1; INSERT INTO t VALUES (1);                     -- refused
SET readonly = 1; SET readonly = 0;                             -- refused
SET readonly = 1; SET max_threads = 2;                          -- refused
SET readonly = 1; SET param_x = '5'; SELECT {x:UInt8};          -- returns 5
SET readonly = 1; SELECT * FROM url('http://127.0.0.1:9/x', 'LineAsString');  -- refused
SET readonly = 1; SELECT * FROM file('nope.csv', 'CSV');        -- refused
SET readonly = 2; SET max_threads = 2; SELECT 2;                -- returns 2
SET readonly = 2; SELECT * FROM url('http://127.0.0.1:9/x', 'LineAsString');  -- not refused by readonly
```

## 12. Sources

**Opened.**

- Dolt: https://www.dolthub.com/docs/sql-reference/version-control/dolt-system-tables, https://www.dolthub.com/docs/sql-reference/version-control/dolt-sql-procedures/, https://www.dolthub.com/blog/2022-04-29-select-isnt-sensible/, https://www.dolthub.com/blog/2024-07-30-re-introducing-dolt-functions/, https://www.dolthub.com/blog/2024-01-03-announcing-dolt-rebase/
- Databases: https://www.sqlite.org/vtab.html, https://www.postgresql.org/docs/current/view-pg-settings.html, https://www.postgresql.org/docs/current/fdw-callbacks.html, https://www.postgresql.org/docs/current/sql-createview.html, https://dev.mysql.com/doc/refman/8.4/en/performance-schema-setup-tables.html, https://dev.mysql.com/doc/refman/8.4/en/performance-schema-setup-instruments-table.html, https://osquery.readthedocs.io/en/stable/introduction/sql/, https://www.tenchisecurity.com/en/insights-news/abusing-the-osquery-curl-table-for-pivoting-into-cloud-environments, https://steampipe.io/docs/guides/key-columns, https://docs.postgrest.org/en/stable/, https://clickhouse.com/docs/sql-reference/statements/kill, https://clickhouse.com/docs/reference/statements/update, https://clickhouse.com/docs/engines/table-engines/special/url, https://clickhouse.com/docs/operations/system-tables/mutations
- Applications as object models: https://developer.apple.com/library/archive/technotes/tn2002/tn2106.html, https://www.cs.utexas.edu/~wcook/Drafts/2006/ashopl.pdf, https://plan9.io/sys/doc/acme/acme.html, https://9fans.github.io/plan9port/man/man4/acme.html, https://riffle.systems/essays/prelude/, https://groups.csail.mit.edu/sdg/pubs/2023/riffle-uist-23.pdf
- Agents and applications: https://mcp.so/servers/blender-mcp, https://developer.apple.com/videos/play/wwdc2026/343/, https://developer.apple.com/videos/play/wwdc2026/347/, https://webmachinelearning.github.io/webmcp/, https://developer.chrome.com/docs/ai/webmcp/best-practices, https://developer.android.com/ai/appfunctions, https://developer.apple.com/documentation/appintents/entitypropertyquery, https://docs.ag-ui.com/concepts/state, https://modelcontextprotocol.io/specification/2025-06-18/server/tools, https://developers.figma.com/docs/figma-mcp-server/tools-and-prompts/, https://arxiv.org/html/2504.14603v2, https://arxiv.org/html/2510.04607, https://arxiv.org/html/2404.07972, https://arxiv.org/abs/2402.01030, https://blog.cloudflare.com/code-mode-mcp/, https://www.anthropic.com/engineering/code-execution-with-mcp, https://platform.claude.com/docs/en/build-with-claude/vision
- Protocols and API rules: https://google.aip.dev/151, https://google.aip.dev/154, https://google.aip.dev/155, https://google.aip.dev/163, https://google.aip.dev/203, https://google.aip.dev/216, https://docs.stripe.com/api/idempotent_requests, https://kubernetes.io/docs/concepts/overview/working-with-objects/, https://kubernetes.io/docs/reference/using-api/api-concepts/, https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/deployment-v1/, https://martinfowler.com/bliki/CQRS.html
- Authority and delegation: https://www.rfc-editor.org/rfc/rfc8693.html, https://www.rfc-editor.org/rfc/rfc9396.html, https://www.rfc-editor.org/rfc/rfc9635.html, https://datatracker.ietf.org/doc/html/draft-ietf-oauth-transaction-tokens-08, https://datatracker.ietf.org/doc/html/draft-hardt-oauth-aauth-protocol-11, https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization, https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization/security-considerations, https://modelcontextprotocol.io/specification/2026-07-28/server/tools, https://learn.microsoft.com/en-us/entra/agent-id/agent-on-behalf-of-oauth-flow, https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/understanding-agent-identities.html, https://developer.apple.com/tutorials/data/documentation/appintents/confirmationconditions.json, https://learn.microsoft.com/en-us/windows/security/book/operating-system-agentic-security, https://learn.microsoft.com/en-us/windows/ai/mcp/servers/mcp-containment, https://blog.google/security/architecting-security-for-agentic/, https://cdn.openai.com/pdf/839e66fc-602c-48bf-81d3-b21eacc3459d/chatgpt_agent_system_card.pdf, https://claude.com/blog/claude-for-chrome, http://web.cs.wpi.edu/~cs557/f14/papers/confused_deputy-hardy.pdf, https://research.google/pubs/macaroons-cookies-with-contextual-caveats-for-decentralized-authorization-in-the-cloud/, https://fly.io/blog/macaroons-escalated-quickly/, https://doc.biscuitsec.org/reference/specifications, https://ucan.xyz/specification/, https://fuchsia.dev/fuchsia-src/concepts/kernel/rights, https://docs.sel4.systems/Tutorials/capabilities.html, https://arxiv.org/abs/2503.18813, https://arxiv.org/abs/2505.23643, https://arxiv.org/abs/2504.11703, https://arxiv.org/abs/2506.08837, https://simonwillison.net/2025/Jun/16/the-lethal-trifecta/, https://ai.meta.com/blog/practical-ai-agent-security/
- Telemetry: https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/

**Excerpt only.** https://developer.android.com/reference/kotlin/android/app/AppInteractionSession.UpdateParams.Builder
