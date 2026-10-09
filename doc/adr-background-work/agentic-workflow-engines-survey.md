---
type: explanation
audience: whoever designs durable, triggered or multi-step agent runs on top of the chat coordinator (ADR-0265), app operations (ADR-0269) and watchbill (ADR-0223)
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.
> Compiled 2026-10-09 from public documentation, specifications, release
> notes and issue trackers; nothing here is a decision. Provenance per claim:
> **[V]** the primary page, spec or release record was read by the compiling
> pass on that date; **[U]** a search snippet, a secondary source or memory.
> Untagged statements in §6 are the compiler's analysis. Versions, limits and
> product states are single observations of a field that moves monthly:
> re-read the primary page before relying on a row. No source code of any
> surveyed system was read. The research literature on the same dimensions is
> surveyed in [agentic-workflow-literature-survey.md](./agentic-workflow-literature-survey.md).

# Agentic workflow engines — a feature survey

The question: what do the systems people reach for when they want "an agent
that runs a workflow" actually provide, mechanism by mechanism — and where
do they differ in ways that matter to someone building the same capability
on a different substrate? The emphasis is on the parts that are expensive to
get wrong: what happens to a run when the process dies, how a run waits for
a person, how code changes reach runs already in flight, and who is allowed
to do what.

Caveats first. Documentation states intended behaviour; where an issue
tracker contradicts it, both are recorded. Several products renamed
themselves or changed licence during 2025–2026, and three of the surveyed
offerings were retired or archived within the three months before the
compile date (§5). Feature depth is uneven by design: LangGraph, n8n and
Temporal are surveyed deepest as the reference point of their family. Not
surveyed, by scope rather than judgement: Azure Durable Functions and Dapr
Workflows (the literature survey's §1.1 covers their published semantics),
AWS Step Functions and Bedrock AgentCore, Prefect, Airflow and Dagster, and
the hosted "managed agents" products of the model vendors.

## 1 Families

| Family | Members surveyed | What it is |
| --- | --- | --- |
| Stateful agent graphs | LangGraph (+ LangChain 1.x), Microsoft Agent Framework (MAF), Google ADK 2.x, Pydantic AI / pydantic-graph, LlamaIndex Workflows, Mastra workflows | A graph of steps over typed shared state, with a checkpointer that makes a run pausable and resumable. |
| Agent-loop SDKs | OpenAI Agents SDK, Claude Agent SDK, LangChain `create_agent`, smolagents | The model → tool → model loop, plus hooks around it: handoffs, guardrails, permissions, sessions. |
| Visual automation | n8n, Dify, Langflow, Flowise (sunset) | A canvas of nodes over an integrations catalogue, started by triggers; an "AI Agent" is one node kind. |
| Durable execution | Temporal, Restate, Inngest, DBOS, Hatchet, Cloudflare Workflows / Agents, Vercel Workflow | Workflows as ordinary code made crash-proof by replaying a history, a journal or step checkpoints. The framework-to-engine mappings of §3.3 run *on* these. |
| Role-based multi-agent | CrewAI (crews + flows) | Personas with tasks under a sequential or hierarchical process, embedded in an event-decorated flow. |
| Contrast cases | DSPy; memory systems (Mem0, Letta, Zep/Graphiti); observability (Langfuse, Phoenix, Braintrust) | Not orchestrators: they optimise a fixed program, hold memory, or record and score runs. |

LangChain is no longer a peer of LangGraph: since 1.0 its agent is
`create_agent`, a prebuilt LangGraph loop extended by middleware **[V]** [lg-graph].

## 2 Feature matrices

Cells are compressed; §3 has the mechanism names and sources.

### 2.1 Orchestrators and durable runtimes

| | LangGraph | n8n | Temporal | Restate | Inngest | DBOS |
| --- | --- | --- | --- | --- | --- | --- |
| Control flow | cyclic graph, BSP supersteps; or functional `@entrypoint`/`@task` | node DAG over item lists, loops, sub-workflows; branches depth-first in canvas order (`v1`) | deterministic workflow code + activities | handlers on services / virtual objects / workflows | function re-invoked, `step.*` memoised | `@workflow` / `@step` in a library |
| Durable unit | superstep checkpoint + per-task pending writes | per-node execution data in DB | event history | journal entry per `ctx` action | step result by step id | one Postgres write per step |
| On resume, re-runs | the interrupted node from its first line | retry: from stored data, granularity undocumented **[U]** | whole workflow code; completed activities skipped | journal replayed; `ctx.run` bodies not repeated once recorded | all code outside steps; unfinished step | workflow from inputs; checkpointed steps skipped |
| Durable human wait | `interrupt()` + `Command(resume=)` | Wait node (>65 s offloaded to DB), Send-and-Wait, per-tool review | signals / updates; timers of years | awakeables, durable promises | `waitForEvent`, bounded by plan's max run length | `recv(topic, timeout)`, events |
| Fan-out / join | `Send`, list joins, `defer=True` | Loop Over Items; queue-mode workers | child workflows, parallel activities (≤2,000 in flight) | `send`, per-key serialisation | `Promise.all` steps; concurrency keys | durable queues |
| Triggers | server cron (thread-bound or stateless); outbound webhooks | webhook, schedule, polling, chat, form, MCP | schedules | delayed send, no built-in cron; Kafka **[U]** | events, cron **[U]** | scheduled workflows |
| In-flight code change | **latest graph applied to every thread** | retry picks original or saved version | patching or pinned worker versioning | immutable deployments; run stays on its endpoint | matched by step id; renamed id re-runs | patch conditionals, version tags |
| Streaming | 7 stream modes; v3 typed projections (beta) | Response Mode = Streaming | Workflow Streams (preview) | pub/sub SSE; no token streaming | Realtime channels; `step.ai` unstreamed | `write_stream` / `read_stream` |
| MCP | client (`langchain.mcp`, beta); server via `/mcp` | client tool + server trigger | per-op activities (agents integration) | via framework integrations | AgentKit **[U]** | via framework integrations |
| Licence | MIT; Agent Server commercial, licence-keyed | Sustainable Use (fair-code); `.ee.` enterprise | MIT server + SDKs | BSL 1.1 server | SSPL server, Apache SDKs | MIT; Conductor commercial |

### 2.2 Agent SDKs and frameworks

| | OpenAI Agents SDK | Claude Agent SDK | Google ADK 2.x | MAF 1.x | Pydantic AI | Mastra | LlamaIndex WF | CrewAI |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Control flow | loop + handoffs (`transfer_to_*`) | Claude Code's loop; `Agent` and `Workflow` tools | graph of `BaseNode`s (2.0) | executor/edge graph, supersteps | implicit loop; pydantic-graph | typed step combinators | event-typed `@step`s | crews + `@start`/`@listen` flows |
| Resume | serialised `RunState` | transcript session, `SessionStore` | event replay by `invocation_id` | superstep checkpoints incl. pending requests | delegated to Temporal / DBOS / Prefect / Restate | snapshot; suspended step re-runs | `DBOSRuntime` journal | `@persist`; task checkpoints |
| HITL | `needs_approval`, `interruptions` | 6-step permission chain, `defer` hook | `require_confirmation` (not on DB sessions) | `request_info`, approval requests | deferred tools | `suspend()` / `resume()` | `InputRequiredEvent`, `wait_for_event` | `@human_feedback` |
| Authority | guardrails (input/output/tool) | permission modes, scoped allow/deny rules, hooks | callbacks, plugins | middleware | harness guardrails (0.x) | processors, tripwire | — | task guardrails |
| Parallel tools | yes, configurable cap | subagents in background, depth/concurrency caps | `ParallelAgent`, graph fan-out | `Concurrent`, fan-out edges | yes | `.parallel`, `.foreach` | `send_event`, `num_workers` | parallel starts |
| Tracing | own model, on by default, to OpenAI | hooks, OTel **[U]** | OTel / Cloud Trace | OTel | Logfire | built in | OTel | usage metrics |
| Evals | via platform (closing 2026-11-30, §3.13) | none built in | `adk eval` + simulation | integrations | `pydantic-evals` **[U]** | scorers | — | `crewai eval` |
| Licence | MIT | Anthropic commercial terms | Apache-2.0 | MIT | MIT **[U]** | Apache-2.0 + `ee/` | MIT | MIT **[U]** |

## 3 The dimensions

### 3.1 Control flow

Four shapes recur.

- **Bulk-synchronous graphs.** LangGraph and MAF both run Pregel-style
  supersteps: plan, execute in parallel, then commit; "channel updates are
  invisible to actors until the next step" **[V]** [lg-pregel] [maf-ckpt].
  Routing is static edges, conditional edges, or a node returning
  `Command(goto=…)`; LangGraph warns that static edges still fire beside a
  `Command` **[V]** [lg-graph]. ADK 2.0 replaced its hierarchical executor
  (`SequentialAgent`, `ParallelAgent`, `LoopAgent`) with a graph engine and
  deprecates the old agents in TypeScript **[V]** [adk-2].
- **Event-typed steps.** LlamaIndex Workflows infer the graph from the event
  types each `@step` consumes and emits **[V]** [lli-wf]; CrewAI Flows wire
  `@listen` / `@router` labels **[V]** [crew-flows]; Mastra composes typed
  steps with `.then`, `.parallel`, `.branch`, `.dowhile`, `.foreach` **[V]**
  [mastra-flow].
- **Code as workflow.** Temporal, Restate, Inngest, DBOS, Hatchet and Vercel
  Workflow take ordinary functions and make their effects durable. The cost is
  a determinism discipline: Temporal returns a non-deterministic error when a
  replayed command does not match history **[V]** [tmp-def]; DBOS raises
  `DBOSStepNondeterminismError` **[V]** [dbos-wf]; Inngest has no matcher and silently re-runs a renamed step
  **[V]** [inn-ver].
- **Model-driven loop.** OpenAI's and Anthropic's SDKs leave control flow to
  the model: a handoff is a tool call that swaps the active agent **[V]**
  [oai-handoffs]; Claude delegates through an `Agent` tool and runs large
  fan-outs through a `Workflow` tool outside the conversation **[V]**
  [cas-sub].

n8n's model is its own: every node runs once per incoming *item*, with
paired-item lineage, and a handful of nodes do not iterate — a frequent
source of silently partial processing **[V]** [n8n-loop]. Branch order is a
per-workflow setting. Since 1.0 (mid-2023) the default, `v1`, runs each
branch to completion before the next ("depth-first"); the legacy `v0` ran
the first node of every branch, then the second ("breadth-first"), and
workflows created before 1.0 keep it unless switched **[V]** [n8n-v1]
[n8n-settings]. Two consequences:

- **The canvas decides the order.** Branches run top to bottom, leftmost
  first at equal height **[V]**, so dragging a node can reorder side effects
  without changing any logic. The order is deterministic, but it is set by
  where nodes are drawn, not by data.
- **Multi-input nodes changed with it.** Under `v0` a multi-input node ran
  once its *first* input had data, which could pull a branch upstream into
  running — the known symptom is both outputs of an If node firing into a
  Merge **[U]** [n8n-merge]. Under `v1` a node runs only when it receives
  data, and a multi-input node needs data on at least one input **[V]**
  [n8n-v1].

Neither order is parallel: the nodes of one execution run one after another,
and n8n's concurrency is across executions (§3.5). The superstep graphs
above went the other way — breadth-first by construction, made safe by
reducers and a commit per step, which `v0` lacked.

### 3.2 State and merging

LangGraph is the reference: per-key reducers declared as
`Annotated[T, reducer]`, `LastValue` by default, `add_messages` appending by
id, `Overwrite` to bypass a reducer, and `DeltaChannel` (beta, 1.2) storing
deltas reconstructed by replay **[V]** [lg-graph]. Two parallel `Overwrite`s
on one key raise `InvalidUpdateError`; returning `[]` through a merging
reducer does not clear a field; parallel updates "may not be ordered
consistently" **[V]**. Runtime context (`context_schema`) is separate from
state and never checkpointed.

ADK scopes state by key prefix — none (session), `user:`, `app:`, `temp:` —
and applies every write as an event's `state_delta` **[V]** [adk-state].
Restate gives each virtual-object key a K/V store journalled with execution,
single writer per key **[V]** [rst-exec]. OpenAI keeps application state in a
`RunContextWrapper.context` the model never sees **[V]** [oai-handoffs].
n8n's `$getWorkflowStaticData` is saved only after successful production
runs and the docs steer users to Data tables instead **[V]** [n8n-static].

### 3.3 Persistence and what re-executes on resume

The decisive dimension. Every system with resume re-executes *something*; they
differ in the unit and in whether they detect divergence.

| System | Recorded | Re-runs after a crash or resume | Divergence detection |
| --- | --- | --- | --- |
| Temporal | every command and event | whole workflow function; completed activities read from history; in-flight activity retried (at-least-once **[U]**) | strict command matching **[V]** [tmp-def] |
| Restate | journal of `ctx` actions + state | replay; completed steps skipped | journal mismatch, error RT0016 **[V]** [rst-ver] |
| Inngest | step results keyed by id + occurrence | all code outside steps; a step that succeeded before its result was saved | none; order warnings only **[V]** [inn-exec] |
| DBOS | inputs, one write per step outcome | workflow from inputs; checkpointed steps skipped; a committed transaction is never retried | `DBOSStepNondeterminismError` **[V]** [dbos-arch] [dbos-wf] |
| Vercel Workflow | event log | full replay from start; duplicate `step_started` possible, "committed but inert" | seeded random and replay-stable `Date` **[V]** [vwf-es] [vwf-glob] |
| Cloudflare Agents fibers | stash rows | nothing — recovery is the author's hook; "the original closure cannot be replayed" | n/a **[V]** [cf-durable] |
| LangGraph (Graph API) | superstep snapshot + per-task pending writes | the interrupted node from its first line; failed sibling's completed writes reused | none |
| LangGraph (Functional API) | task results | the entrypoint from the start; completed tasks loaded; unfinished task may repeat | none; order changes break replay **[V]** [lg-persist] |
| MAF | executor state, pending messages, pending requests, shared state, per superstep | from the checkpoint; same topology and executor ids required | rehydration fails on topology change **[V]** [maf-ckpt] |
| ADK | session events | sequential/loop agents continue from their counters; parallel re-runs unfinished children; tools at-least-once | none stated **[V]** [adk-resume] |
| Mastra | snapshot on suspend | the suspended step, its `execute` receiving `resumeData`; whether from its first line is not stated | none **[V]** [mastra-sr] |
| CrewAI | flow state; task checkpoints | completed tasks skipped per the docs; the literature survey's §2 (Khan) measured 1.15.2 re-running completed side-effecting methods | none documented; checkpoint writes are best-effort, a failed write is logged and the run continues **[V]** [crew-ckpt] |
| n8n | per-node execution data | retry from stored data; granularity undocumented | n/a **[U]** [n8n-exec] |

LangGraph adds three **durability modes** — `"exit"` (persist only when the
graph exits; a crash loses the run), `"async"` (persist while the next step
runs; the default), `"sync"` (persist before the next step starts) **[V]**
[lg-invoke] — and, in 1.2, `RetryPolicy`,
async-only `TimeoutPolicy` with heartbeats, `error_handler=` for saga
compensation, and `RunControl.request_drain()` **[V]** [lg-fault]. Open
issues record that `"sync"` does not enforce write ordering (#8039), that
`"async"` leaks coroutine chains (#7094), and that on the hosted server a
tool call running past ~180 s is re-dispatched while the original still runs,
doubling or tripling the work (#7417) **[V]** [lg-7417].

The integration pattern of 2026 is a framework *mapped onto* a durable engine:
Pydantic AI makes every model request and tool call its own Temporal activity
or DBOS step, behind a `Durability` capability on an otherwise ordinary agent
**[V]** [pai-durable]; Temporal's OpenAI Agents integration (1.0.0, a package of its own;
streaming marked experimental) redirects `Runner.run` so the loop is workflow
code and the model call an activity, with `activity_as_tool()` for I/O tools
**[V]** [tmp-oai];
LlamaIndex's `DBOSRuntime` journals step completions **[V]** [lli-dbos].
Pydantic's documentation states the corollary plainly: durability is not
storage — chat threads need persistence of their own **[V]**.

### 3.4 Human in the loop

Two properties separate the implementations: whether a wait survives a
restart, and what re-runs when it ends.

- **Durable waits.** LangGraph's `interrupt(value)` raises, the checkpoint is
  kept, and `Command(resume=…)` on the same thread continues; parallel
  interrupts resume by id map, matching is "strictly index-based", and code
  before the interrupt runs again **[V]** [lg-int]. MAF checkpoints *pending
  requests* and re-emits them on restore **[V]** [maf-hitl]. Temporal waits
  on signals or validated updates, with timers "as long as several years"
  **[V]** [tmp-msg] [tmp-timers]; Restate on awakeables and durable promises **[V]**; Inngest on
  `waitForEvent`, bounded by the plan's maximum run length (30–366 days)
  **[V]** [inn-limits]; n8n offloads any Wait over 65 s to the database and
  resumes on `$execution.resumeUrl` **[V]** [n8n-wait]; Dify's Human Input
  node defaults to a three-day timeout with its own branch **[V]** [dify-hi].
- **Serialised pause.** OpenAI parks a run as a `RunState` (`to_json` /
  `from_json`) with `ToolApprovalItem`s to approve or reject; deserialising
  authenticates neither the snapshot nor the reviewer **[V]** [oai-hitl].
  Pydantic AI ends a run with `DeferredToolRequests` and resumes with
  `DeferredToolResults`; its docs note a client-supplied history can approve
  itself, so approval is not an authorisation boundary **[V]** [pai-defer].
- **Per-tool review.** LangChain's `HumanInTheLoopMiddleware` offers
  approve / edit / reject / respond **[V]** [lc-hitl]; n8n places a review step
  in front of chosen tools and delivers it to Slack, Teams, mail and others
  **[V]** [n8n-hitl]; Claude's SDK evaluates hooks → deny rules → ask rules →
  permission mode → allow rules → `canUseTool`, and a `PreToolUse` hook may
  return `defer` to end the turn and resume later **[V]** [cas-perm]. ADK's
  tool confirmation does not work on its database or Vertex session services
  **[V]** [adk-confirm].
- **Time travel.** LangGraph's `update_state(values, as_node=)` forks a new
  checkpoint rather than editing one; `get_state_history` and a prior
  `checkpoint_id` replay or branch **[V]** [lg-tt]. One closed issue records
  `update_state(None, as_node=END)` erasing a hosted thread (#8653) **[V]**.

### 3.5 Concurrency and fan-out

LangGraph fans out with a conditional edge returning `Send(node, arg)` and
joins with list edges or `defer=True` ("until no tasks are pending anywhere")
**[V]** [lg-graph]. Temporal caps incomplete activities, signals and children
at 2,000 per execution, 500 recommended **[V]** [tmp-limits]. Inngest and
Hatchet bring *flow control* the graph libraries lack: concurrency keys
(Hatchet's are CEL expressions), throttling versus rate limiting, debounce,
priority, singleton, and Hatchet's `CANCEL_IN_PROGRESS` "newest wins"
strategy for chat **[V]** [inn-flow] [hat-conc]. LangGraph's server answers
the same question for a thread with *double-texting* strategies — `enqueue`,
`reject`, `interrupt`, `rollback` — server-only, with half-finished tool calls
possible under `interrupt` **[V]** [lg-dt]. A failing branch of a Mastra
`.parallel` fails the block **[V]**. In LangGraph, a tool returning
`Command(graph=PARENT, goto=[Send(...)])` has its state update dropped
without error, and with two such handoffs in one turn only one reaches the
parent (#9072, open) **[V]** [lg-9072].

### 3.6 Tools

Schema derivation from signatures is universal. The differences are around
the edges:

- **Tool access to run state.** LangChain's `ToolRuntime` parameter replaces
  the `Injected*` annotations; a tool may return a `Command` updating state
  **[V]** [lc-tools].
- **Errors.** LangChain routes them through `ToolErrorMiddleware` /
  `ToolRetryMiddleware` **[V]**; OpenAI provides `error_handlers` for
  `max_turns`, refusals and invalid output **[V]** [oai-run].
- **Code as the action.** smolagents' `CodeAgent` writes Python and runs it
  in an AST interpreter with import allow-lists and operation caps, or in
  E2B / Modal / Docker; remote execution does not support managed
  sub-agents, and its WASM executor was removed in 1.26 **[V]** [smol-sec].
- **Durable tools.** Under Temporal, an MCP session is reconnected per
  activity **[V]** [pai-tmp]; `@function_tool` code runs in the workflow and
  must be deterministic **[V]** [tmp-oai].

### 3.7 Limits and authority

Most systems bound *how much* an agent does; few bound *what it may touch*.

- **Step and spend limits.** LangGraph `recursion_limit` (default 1000
  supersteps since 1.0.6) and `RemainingSteps` **[V]**; LangChain
  `ModelCallLimitMiddleware` / `ToolCallLimitMiddleware` **[V]** [lc-mw];
  OpenAI `max_turns` **[V]**; Claude `max_turns`, `max_budget_usd`, subagent
  depth (3) and concurrency (20) caps **[V]** [cas-sub].

Validators, permission engines, identity, credentials and isolation — what
bounds *what* an agent may touch — are surveyed with the research literature
in [agentic-workflow-security-survey.md](./agentic-workflow-security-survey.md).

### 3.8 Triggers and scheduling

n8n's breadth is the reference: webhooks (test and production paths), a
schedule trigger with missed-execution policies, polling app triggers, chat,
form, error-workflow and MCP-server triggers **[V]** [n8n-rel]. LangGraph
offers cron only on its server — thread-bound or stateless, same input every
tick, with a warning that unused crons keep spending — and only *outbound*
webhooks on run completion **[V]** [lg-cron] [lg-hook]. Mastra added
persistent cron `schedules` for agents and workflows in 2026 **[U]**
[mastra-sched]; n8n's standalone Agents (preview) take cron schedules and
Slack, Telegram and Linear channels **[V]** [n8n-agents]. Temporal has
schedules, Inngest events and cron, DBOS scheduled workflows; Restate has
delayed sends and no built-in cron — its guide builds a scheduler from them
**[V]** [rst-cron]. The SDKs (OpenAI, Claude, ADK, Pydantic AI)
have no trigger concept: a run starts when the host calls it.

### 3.9 Runtime, deployment and scale

- **Library in your process.** OpenAI, Claude, ADK, MAF, Pydantic AI,
  LlamaIndex, DBOS (Postgres as the only infrastructure).
- **Server with a queue.** LangGraph's Agent Server enqueues a pending run in
  Postgres for a worker to lease, at most one run per thread at a time, with
  Redis only for signalling **[V]** [lg-scale]; n8n's queue mode is
  Redis/Bull with worker and webhook processors, multi-main on Enterprise;
  since 2.0 stalled jobs are no longer retried automatically **[V]**
  [n8n-queue] [n8n-2].
- **Engine cluster plus workers.** Temporal (self-hosted or Cloud; serverless
  workers on Lambda pre-release), Restate (single binary), Hatchet (Postgres,
  optional RabbitMQ).
- **Serverless callback.** Inngest calls your HTTP functions; `step.ai.infer`
  offloads inference so the function is not running while the model works
  **[V]** [inn-ai].

Limits that bind agent loops: Temporal Cloud's history cap of 51,200 events or
50 MB and 2 MB per payload (External Storage, public preview, offloads larger
ones) **[V]** [tmp-limits]; Inngest's 1,000 steps per function and 32 MiB per
run **[V]** [inn-limits]; Restate's inactivity timeout, 1 minute by default,
after which the service is asked to suspend — raise it for long model calls
**[V]** [rst-conf]; Temporal's agents integration's
60 s default model-activity timeout **[V]** [tmp-oai].

### 3.10 Streaming

Graph libraries stream *steps* as well as tokens: LangGraph's modes are
`values`, `updates`, `messages`, `custom`, `checkpoints`, `tasks`, `debug`,
with a typed v2 stream part and v3 per-channel projections (beta) **[V]**
[lg-stream]. Durable engines struggle with tokens: Restate "does not yet"
stream token by token **[V]** [rst-stream]; `step.ai` does not stream, and
Inngest's Realtime separates durable `step.realtime.publish` from non-durable
token publishing **[V]** [inn-rt]; Temporal's Workflow Streams (public preview)
carry batches in a signal or update envelope and drop a batch, at-most-once,
once `max_retry_duration` is exhausted **[V]** [tmp-streams]; DBOS stream writes from steps can
duplicate **[V]**. n8n streams only if a node in the path streams **[V]**
[n8n-stream].

### 3.11 Memory

Short-term memory is the thread everywhere. Long-term memory diverges:

- **Key/value plus vectors.** LangGraph `BaseStore` with tuple namespaces and
  optional semantic index; one embedding model per deployment, no
  re-embedding tooling **[V]** [lg-store].
- **Structured layers.** Mastra: message history, a working-memory template,
  semantic recall, and *observational memory*, where background agents
  compress old messages into observations **[V]** [mastra-mem]. CrewAI
  replaced four memory kinds with one LLM-scored `Memory` over LanceDB,
  blending similarity, recency and importance **[V]** [crew-mem].
- **Compaction.** OpenAI `responses.compact` sessions **[V]**; LangChain
  summarisation middleware **[V]**; Dify context compaction **[V]**.
- **External systems.** Mem0 moved to single-pass, add-only extraction in
  April 2026 ("nothing is overwritten") **[V]** [mem0]; Zep/Graphiti keep a
  temporal knowledge graph whose facts carry validity windows and are
  invalidated rather than deleted **[V]** [graphiti]; Letta retired its V1
  API server in favour of `letta-code` **[V]** [letta].

### 3.12 Observability

Two models. **Trace trees** — OpenAI's built-in tracer (on by default, sent to
OpenAI, sensitive data included unless turned off) **[V]** [oai-trace],
LangSmith, Logfire, Langfuse, Phoenix — record spans per agent, model call,
tool, guardrail and handoff. **Execution logs** — n8n's per-node execution
data, log streaming of workflow, node, audit and AI events (Enterprise), and
an Insights dashboard over production runs **[V]** [n8n-logs]. The
OpenTelemetry GenAI conventions moved to their own repository with agent
spans (`create_agent`, `invoke_agent`, `invoke_workflow`, `plan`,
`execute_tool`) all at *Development* status and no tagged release **[V]**
[otel-genai]; MCP 2026-07-28 propagates `traceparent` in `_meta` **[V]**.
Langfuse has been part of ClickHouse since January 2026 **[V]** [langfuse].

### 3.13 Evaluation

Built in: n8n's Evaluation Trigger and node over a Data-table dataset with
LLM-judged correctness and helpfulness, string similarity and tools-used
metrics (Pro/Enterprise) **[V]** [n8n-eval]; ADK's `adk eval` with user and
environment simulation **[V]** [adk]; LangChain's `agentevals` trajectory
matchers and judges **[V]** [lc-evals]. DSPy is the outlier: it compiles a
program's prompts against a metric (GEPA, MIPROv2) rather than scoring runs
**[V]** [dspy]. OpenAI's Agent Builder and hosted Evals are scheduled to shut
down on 2026-11-30, evals becoming read-only on 2026-10-31 **[V]** [oai-dep].

### 3.14 Authoring and in-flight versioning

What happens to a run that is paused while its code changes is where the
engines differ most, and it is rarely in the marketing.

- **Pin the run.** Temporal's worker versioning (GA announced 2026-03-30 **[U]**) pins a
  workflow to one deployment version until it completes, or auto-upgrades it,
  which keeps patching necessary **[V]** [tmp-wv];
  Restate deployments are immutable and a run stays on its endpoint **[V]**
  [rst-ver]; Vercel Workflow pins runs to the deployment that started them
  **[V]** [vwf-ver].
- **Match by name.** Inngest matches step results by id; a changed id
  re-runs the step even if it completed **[V]** [inn-ver]. LlamaIndex's
  durable identity is the workflow name — rename the class and the journal is
  orphaned **[V]** [lli-dbos].
- **Apply the latest.** LangGraph: "Unlike workflow engines that pin a run to
  the version of code it started with, LangGraph applies the latest graph
  immediately to every thread." Edge changes are safe; renaming or removing a
  node a thread is paused at, or a state key, breaks it; the advice is
  `NotRequired` fields, add-then-remove renames and a `flow_version` stamped
  into state **[V]** [lg-compat]. MAF requires the same topology and executor
  ids on rehydration **[V]**.
- **Choose at retry.** n8n offers "retry with currently saved workflow" or
  "with original workflow"; named versions, diffs and a publish timeline
  arrived during 2026 **[V]** [n8n-exec] [n8n-rel].

Agent definitions as versioned data: LangGraph *assistants* (graph + config,
every edit a version, promote or roll back) **[V]** [lg-asst]; Claude's
`AgentDefinition` (prompt, tools, model, skills, memory, permission mode)
**[V]** [cas-sub]; n8n Agents' draft/publish snapshots **[V]**.

### 3.15 Multi-agent

The patterns converge on four: *agent as tool* (returns a result, keeps
control), *handoff* (transfers control), *supervisor* (a coordinator routes),
and *graph* (explicit topology). LangChain archived `langgraph-supervisor`
(repository archived 2026-09-20) in favour of subagents under `create_agent`
**[V]** [lc-sup]; Mastra
deprecated `agent.network()` for supervisor agents **[U]**; MAF ships
`Sequential`, `Concurrent`, `GroupChat`, `Handoff` and `Magentic`
orchestrations **[V]** [maf]. Claude's SDK scans subagent output for
instruction-shaped text before the parent reads it **[V]** [cas-sub].

## 4 Protocols

| Protocol | Standardises | Unit of work | Authority | State on 2026-10-09 |
| --- | --- | --- | --- | --- |
| MCP | agent ↔ tools, resources, prompts | one request (`tools/call`, …) | OAuth 2.1, server as protected resource (RFC 9728), issuer validation (RFC 9207) | revision 2026-07-28 **[V]** [mcp-cl]; under the Agentic AI Foundation (Linux Foundation) |
| A2A | agent ↔ opaque agent | `Task` with `contextId`, states incl. `INPUT_REQUIRED`, `AUTH_REQUIRED` | Agent Card `securitySchemes`, signed cards | v1.0 (March 2026), v1.0.1; joined AAIF 2026-08-17 **[V]** [a2a] [aaif] |
| AG-UI | agent backend ↔ user front end | a run emitting typed events | transport's | first-party in MAF, ADK, Mastra, Pydantic AI, LlamaIndex; community for Claude **[V]** [agui] |
| OTel GenAI | traces of model and agent work | span | n/a | Development status, own repo **[V]** [otel-genai] |

MCP 2026-07-28 is a large revision **[V]** [mcp-cl]: the protocol is
stateless — no `initialize`, no session id, version and capabilities carried
per request, a `server/discover` RPC; server-initiated sampling, elicitation
and roots are replaced by *multi round-trip requests* (`input_required` →
client retries with `inputResponses`); tasks moved out of core into an
extension; Roots, Sampling, Logging and Dynamic Client Registration are
deprecated; SSE resumability is removed. Hosts on 2025-11-25 SDKs and servers
on 2026-07-28 will meet during the transition.

Exposure, both ways, is common: LangGraph serves `/mcp` and
`/a2a/{assistant_id}` from a deployed graph **[V]** [lg-a2a]; n8n's MCP Server
Trigger exposes workflows **[V]** [n8n-mcp]; Langflow exposes project flows
**[U]**.

## 5 Market state at the compile date

- **Retired or archived:** Flowise (code freeze 2026-07-29, archived
  2026-08-10, end of life 2026-08-31) **[V]** [flowise]; `langgraph-supervisor`
  (archived 2026-09-20) **[V]**; Letta's V1 API server **[V]**; OpenAI Agent
  Builder and hosted Evals (shutdown scheduled 2026-11-30) **[V]** [oai-dep];
  smolagents' WASM executor (1.26.0) **[V]**.
- **Reached 1.0 / GA in 2026:** MAF 1.0 (2026-04-03, "production-ready" in
  the vendor's words) **[V]** [maf-1], ADK 2.0 (Python 2026-05-19, Go
  2026-06-30, TS 2026-08-21) **[V]**, A2A 1.0 (2026-03-12; 1.0.1 on GitHub
  2026-05-28, the spec page still naming 1.0.0) **[V]**,
  Temporal worker versioning (2026-03-30 **[U]**) and its OpenAI Agents
  integration 1.0.0 **[V]**.
- **Licence boundaries:** the open libraries are MIT or Apache; the *servers*
  that provide queues, cron and double-texting are where licences tighten —
  LangGraph's Agent Server needs a licence key, Restate is BSL, Inngest's
  server SSPL, n8n fair-code with an enterprise tier, Dify's modified Apache
  forbids unauthorised multi-tenant SaaS **[V]**.
- **Observed releases on or just before 2026-10-09:** `langgraph` 1.2.14,
  `langchain` 1.4.4, n8n 2.42.6 (2.43.3 in beta), Temporal server 1.32.1, OpenAI Agents SDK
  (Python) 0.23.1, Claude Agent SDK (Python) 0.2.165, ADK (Python) 2.11.0, MAF
  (Python) 1.21.0, Pydantic AI 2.54.0, CrewAI 1.15.26 **[V]**.

## 6 Observations

These are the compiler's reading of §2–§5, not sourced claims.

1. **Resume always re-executes.** No surveyed system continues a step from
   the middle. The unit that re-runs is a node (LangGraph), a step (Mastra,
   DBOS, Inngest), the workflow code around recorded effects (Temporal,
   Restate), or nothing (Cloudflare fibers). Idempotent side effects are
   therefore a requirement everywhere, and exactly-once holds only where the
   effect and its record share a transaction — DBOS's database transactions,
   Restate's per-key state written with the journal — never across a tool
   call to a third system.
2. **Detection is the expensive half.** Replay engines detect divergence
   (Temporal, Restate, DBOS); checkpoint libraries mostly do not, so a code
   change silently alters paused runs (LangGraph applies the latest graph by
   design; Inngest re-runs renamed steps). Pinning a run to the version it
   started on is the durable engines' answer; the libraries document a
   migration discipline instead (MAF refuses a changed topology, LangGraph
   prescribes add-then-remove renames).
3. **A durable human wait is a checkpointed pending request.** MAF and
   LangGraph store the question with the run; OpenAI and Pydantic AI hand a
   serialised state to the caller, which then owns its integrity. Three
   vendors' docs say so: OpenAI and Pydantic AI warn that the handed-back
   state is not an authorisation boundary, MAF that checkpoint storage is a
   trust boundary and its Python stores unpickle through an allow-list.
4. **Authority is the thinnest layer.** Limits (steps, tokens, money) and
   validators (guardrails) are common; scoping what an agent may touch to
   what a person granted for a task is not, outside Claude's rule engine and
   per-tool review. Guardrails that run in parallel with the agent bound
   output, not effects. The security survey's §4–§5 carry this further.
5. **Triggers live in servers, not libraries.** Cron, webhooks and queueing
   are exactly the features the open libraries leave to a commercial or
   source-available server.
6. **Tokens and durability do not mix well.** Every durable engine either
   does not stream tokens, streams them outside the durable boundary, or
   streams at-most-once. Step-level streaming is the durable part.
7. **The checkpoint is the product.** Across the graph libraries the
   feature users depend on is the addressable, forkable checkpoint that
   serves crash recovery, human waits, time travel and state edits at once;
   the superstep scheduler around it is the same Pregel shape in each.
8. **Documentation and measurement disagree.** Every row above states
   intended behaviour. The two measurements this survey found — the issue
   trackers (§3.3, §3.5) and the conformance harness in the literature
   survey's §2 (Khan) — contradict the documentation for LangGraph across a
   SIGKILL and for CrewAI's completed tasks. A decision that depends on a
   resume guarantee should be preceded by a test of it, not a reading.

## References

Read by the compiling pass on 2026-10-09; the full per-system source lists, including pages read only as search snippets, are not reproduced.

- [aaif] — <https://aaif.io/blog/a2a-joins-aaif>
- [a2a] — <https://a2a-protocol.org/latest/specification/>
- [adk] — <https://adk.dev/>
- [adk-2] — <https://adk.dev/2.0/>
- [adk-confirm] — <https://adk.dev/tools-custom/confirmation/>
- [adk-resume] — <https://adk.dev/runtime/resume/>
- [adk-state] — <https://adk.dev/sessions/state/>
- [agui] — <https://docs.ag-ui.com/introduction>
- [cas-perm] — <https://code.claude.com/docs/en/agent-sdk/permissions>
- [cas-sub] — <https://code.claude.com/docs/en/agent-sdk/subagents>
- [cf-durable] — <https://developers.cloudflare.com/agents/api-reference/durable-execution/>
- [crew-ckpt] — <https://docs.crewai.com/en/concepts/checkpointing>
- [crew-flows] — <https://docs.crewai.com/en/concepts/flows>
- [crew-mem] — <https://docs.crewai.com/en/concepts/memory>
- [dbos-arch] — <https://docs.dbos.dev/architecture>
- [dbos-wf] — <https://docs.dbos.dev/python/tutorials/workflow-tutorial>
- [dify-hi] — <https://docs.dify.ai/en/use-dify/nodes/human-input>
- [dspy] — <https://dspy.ai/current/>
- [flowise] — <https://flowiseai.com/sunset>
- [graphiti] — <https://github.com/getzep/graphiti>
- [hat-conc] — <https://docs.hatchet.run/home/concurrency>
- [inn-ai] — <https://www.inngest.com/docs/features/inngest-functions/steps-workflows/step-ai-orchestration>
- [inn-exec] — <https://www.inngest.com/docs/learn/how-functions-are-executed>
- [inn-flow] — <https://www.inngest.com/docs/guides/flow-control>
- [inn-limits] — <https://www.inngest.com/docs/usage-limits/inngest>
- [inn-rt] — <https://www.inngest.com/docs/features/realtime>
- [inn-ver] — <https://www.inngest.com/docs/learn/versioning>
- [langfuse] — <https://github.com/langfuse/langfuse>
- [lc-evals] — <https://docs.langchain.com/oss/python/langchain/test/evals>
- [lc-hitl] — <https://docs.langchain.com/oss/python/langchain/human-in-the-loop>
- [lc-mw] — <https://docs.langchain.com/oss/python/langchain/middleware/built-in>
- [lc-sup] — <https://docs.langchain.com/oss/python/migrate/langgraph-supervisor>
- [lc-tools] — <https://docs.langchain.com/oss/python/langchain/tools>
- [letta] — <https://github.com/letta-ai/letta>
- [lg-7417] — <https://github.com/langchain-ai/langgraph/issues/7417>
- [lg-9072] — <https://github.com/langchain-ai/langgraph/issues/9072>
- [lg-a2a] — <https://docs.langchain.com/langsmith/server-a2a>
- [lg-asst] — <https://docs.langchain.com/langsmith/assistants>
- [lg-compat] — <https://docs.langchain.com/oss/python/langgraph/backward-compatibility>
- [lg-cron] — <https://docs.langchain.com/langsmith/cron-jobs>
- [lg-dt] — <https://docs.langchain.com/langsmith/double-texting>
- [lg-fault] — <https://docs.langchain.com/oss/python/langgraph/fault-tolerance>
- [lg-graph] — <https://docs.langchain.com/oss/python/langgraph/graph-api>
- [lg-hook] — <https://docs.langchain.com/langsmith/use-webhooks>
- [lg-int] — <https://docs.langchain.com/oss/python/langgraph/interrupts>
- [lg-invoke] — <https://reference.langchain.com/python/langgraph/pregel/main/Pregel/invoke>
- [lg-persist] — <https://docs.langchain.com/oss/python/langgraph/persistence>
- [lg-pregel] — <https://docs.langchain.com/oss/python/langgraph/pregel>
- [lg-scale] — <https://docs.langchain.com/langsmith/agent-server-scale>
- [lg-store] — <https://docs.langchain.com/oss/python/langgraph/stores>
- [lg-stream] — <https://docs.langchain.com/oss/python/langgraph/streaming>
- [lg-tt] — <https://docs.langchain.com/oss/python/langgraph/use-time-travel>
- [lli-dbos] — <https://developers.llamaindex.ai/python/llamaagents/workflows/dbos/>
- [lli-wf] — <https://github.com/run-llama/workflows-py>
- [maf] — <https://learn.microsoft.com/en-us/agent-framework/overview/>
- [maf-1] — <https://devblogs.microsoft.com/agent-framework/microsoft-agent-framework-version-1-0/>
- [maf-ckpt] — <https://learn.microsoft.com/en-us/agent-framework/workflows/checkpoints>
- [maf-hitl] — <https://learn.microsoft.com/en-us/agent-framework/workflows/human-in-the-loop>
- [mastra-flow] — <https://mastra.ai/docs/workflows/control-flow>
- [mastra-mem] — <https://mastra.ai/docs/memory/overview>
- [mastra-sched] — <https://mastra.ai/blog/introducing-schedules-for-agents-and-workflows>
- [mastra-sr] — <https://mastra.ai/docs/workflows/suspend-and-resume>
- [mcp-cl] — <https://modelcontextprotocol.io/specification/2026-07-28/changelog>
- [mem0] — <https://github.com/mem0ai/mem0>
- [n8n-2] — <https://docs.n8n.io/2-0-breaking-changes/>
- [n8n-agents] — <https://docs.n8n.io/build/build-and-manage-agents>
- [n8n-eval] — <https://docs.n8n.io/advanced-ai/evaluations/metric-based-evaluations/>
- [n8n-exec] — <https://docs.n8n.io/workflows/executions/single-workflow-executions/>
- [n8n-hitl] — <https://docs.n8n.io/advanced-ai/human-in-the-loop-tools/>
- [n8n-logs] — <https://docs.n8n.io/log-streaming/>
- [n8n-loop] — <https://docs.n8n.io/flow-logic/looping/>
- [n8n-merge] — <https://nordflux.de/en/guides/execution-order-and-merge-why-branches-don-t-run-the-way-you-think>
- [n8n-mcp] — <https://docs.n8n.io/integrations/builtin/core-nodes/n8n-nodes-langchain.mcptrigger/>
- [n8n-queue] — <https://docs.n8n.io/hosting/scaling/queue-mode/>
- [n8n-rel] — <https://docs.n8n.io/release-notes/>
- [n8n-settings] — <https://docs.n8n.io/workflows/settings>
- [n8n-static] — <https://docs.n8n.io/code/cookbook/builtin/get-workflow-static-data/>
- [n8n-stream] — <https://docs.n8n.io/workflows/streaming/>
- [n8n-v1] — <https://docs.n8n.io/changelog/v10-migration-guide>
- [n8n-wait] — <https://docs.n8n.io/integrations/builtin/core-nodes/n8n-nodes-base.wait/>
- [oai-dep] — <https://developers.openai.com/api/docs/deprecations.md>
- [oai-handoffs] — <https://openai.github.io/openai-agents-python/handoffs/>
- [oai-hitl] — <https://openai.github.io/openai-agents-python/human_in_the_loop/>
- [oai-run] — <https://openai.github.io/openai-agents-python/running_agents/>
- [oai-trace] — <https://openai.github.io/openai-agents-python/tracing/>
- [otel-genai] — <https://github.com/open-telemetry/semantic-conventions-genai>
- [pai-defer] — <https://pydantic.dev/docs/ai/tools-toolsets/deferred-tools/>
- [pai-durable] — <https://pydantic.dev/docs/ai/integrations/durable_execution/overview/>
- [pai-tmp] — <https://pydantic.dev/docs/ai/integrations/durable_execution/temporal/>
- [rst-conf] — <https://docs.restate.dev/services/configuration>
- [rst-cron] — <https://docs.restate.dev/guides/cron>
- [rst-exec] — <https://docs.restate.dev/concepts/durable_execution>
- [rst-stream] — <https://docs.restate.dev/ai/patterns/streaming-responses>
- [rst-ver] — <https://docs.restate.dev/operate/versioning>
- [smol-sec] — <https://huggingface.co/docs/smolagents/tutorials/secure_code_execution>
- [tmp-def] — <https://docs.temporal.io/workflow-definition>
- [tmp-limits] — <https://docs.temporal.io/cloud/limits>
- [tmp-msg] — <https://docs.temporal.io/encyclopedia/workflow-message-passing>
- [tmp-oai] — <https://docs.temporal.io/develop/python/integrations/openai-agents-sdk>
- [tmp-streams] — <https://docs.temporal.io/develop/python/workflows/workflow-streams>
- [tmp-timers] — <https://docs.temporal.io/workflow-execution/timers-delays>
- [tmp-wv] — <https://docs.temporal.io/production-deployment/worker-deployments/worker-versioning>
- [vwf-es] — <https://workflow-sdk.dev/docs/how-it-works/event-sourcing>
- [vwf-glob] — <https://workflow-sdk.dev/docs/api-reference/workflow-globals>
- [vwf-ver] — <https://workflow-sdk.dev/docs/foundations/versioning>
