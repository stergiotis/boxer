---
type: explanation
audience: whoever designs durable, triggered or multi-step agent runs on top of the chat coordinator (ADR-0265), app operations (ADR-0269) and watchbill (ADR-0223)
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.
> Compiled 2026-10-09 as the research-literature companion to
> [agentic-workflow-engines-survey.md](./agentic-workflow-engines-survey.md);
> nothing here is a decision. Provenance per entry: **[V]** the abstract (or
> the paper) was read by the compiling pass on that date; **[U]** citation
> metadata only, summary from memory or a secondary source — re-read before
> citing. **PR** peer-reviewed (venue named), **PP** preprint. Full papers
> were mostly not read: numbers are the authors' own claims. Many 2025–2026
> entries are single-group preprints; treat them as hypotheses, not results.

# Agentic workflows in the research literature

The engine survey asks what shipping systems do. This page asks what the
research literature knows about the same dimensions — in particular the three
the engines handle worst: what a run does after a crash, what starts a run
when no person does, and how a run is explained and evaluated afterwards.
What an agent may touch has its own page,
[agentic-workflow-security-survey.md](./agentic-workflow-security-survey.md). Dimension labels
follow the engine survey's §3: D*n* is its §3.*n* (D1 control flow, D3
persistence, D4 human in the loop, D7 authority, D8 triggers, D12
observability, D14 versioning, D15 multi-agent).

Six searches fed it (2026-10-09): durable-execution foundations, agent
orchestration and serving, memory/evaluation/observability, trigger
foundations, unprompted agents, and — moved to the security page — authority.
Each found the same split. **Systems research** on durability and triggers is mature
and almost entirely pre-LLM; **agent research** is young, and its systems
half — durability, transactions, authority — consists largely of preprints
from 2025–2026.

## 1 Durable execution: the foundations

What the engines' persistence models (engine survey §3.3) rest on.

| Work | Venue | Contribution | Dims |
| --- | --- | --- | --- |
| Garcia-Molina & Salem, *Sagas* | SIGMOD 1987, PR **[U]** | Long-lived transaction as local steps with compensations run in reverse on failure: semantic atomicity without isolation. The ancestor of every "agent saga". | D1 D3 |
| Chandy & Lamport, *Distributed Snapshots* | TOCS 1985, PR **[U]** | Marker-based consistent cut without stopping the system. | D3 D5 |
| Elnozahy, Alvisi, Wang, Johnson, *A Survey of Rollback-Recovery Protocols* | CSUR 2002, PR **[V]** | Checkpoint-based vs log-based recovery; log-based records nondeterministic events (*determinants*) and replays. An LLM response is a determinant. | D3 |
| van der Aalst et al., *Workflow Patterns* | DPD 2003, PR **[U]** | The vocabulary of control-flow constructs (split, sync, discriminator, deferred choice, cancellation) engines are measured against. | D1 |
| Rinderle, Reichert, Dadam, *Correctness Criteria for Dynamic Changes in Workflow Systems* | DKE 2004, PR **[V]** | Migrating running instances to a changed schema: compliance and trace-equivalence criteria. The developed theory of what LangGraph does informally (engine survey §3.14). | D14 |
| Malewicz et al., *Pregel* | SIGMOD 2010, PR **[U]** | Bulk-synchronous supersteps, combiners, checkpoint per superstep — LangGraph's and MAF's runtime model. | D1 D2 D3 D5 |

### 1.1 Durable functions and stateful serverless

- **Burckhardt et al., *Durable Functions: Semantics for Stateful
  Serverless*,** OOPSLA 2021, PR **[V]**. A calculus for workflows, actors
  and critical sections; proves record-replay execution equivalent to the
  high-level model. The formal statement of the deterministic-orchestrator
  contract Temporal and Durable Functions implement.
- **Burckhardt et al., *Netherite*,** PVLDB 2022, PR **[V]**. Partitioned
  instances with speculative group commit on a hybrid log; often >10× the
  original engine. Per-step durability need not cost a round trip per step.
- **Setty et al., *Locks with Intent* (Olive),** OSDI 2016, PR **[V]**;
  **Zhang et al., *Beldi*,** OSDI 2020, PR **[V]**. An intent persisted with
  the lock lets any machine finish a crashed holder's work; Beldi extends it
  to exactly-once stateful functions and cross-function transactions on plain
  FaaS.
- **Jia & Witchel, *Boki*,** SOSP 2021, PR **[V]**. A shared log as the
  substrate; workflows, durable objects and queues become libraries over it.
- **Qi, Liu, Jin, *Halfmoon*,** SOSP 2023, PR **[V]**. Logs only reads or
  only writes, proved log-optimal for exactly-once; 1.5–4× less logging than
  Boki. The bound on what an agent runtime must journal.
- **Liu et al., *Unum*,** NSDI 2023, PR **[V]**. Orchestration as a library
  beside each function, exactly-once through checkpoints in a consistent
  store — durability without a coordinator.
- **Skiadopoulos et al., *DBOS: A DBMS-oriented Operating System*,** PVLDB
  2021, PR **[V]**; Li et al., CIDR 2022 **[V]**. OS state in a transactional
  DBMS; provenance and time-travel debugging fall out. The DBOS Transact
  product has no paper of its own that the search found.
- **Psarakis et al., *Styx*,** SIGMOD 2025, PR **[V]**. Serializable,
  exactly-once transactions over arbitrary function call graphs on a
  streaming dataflow.

### 1.2 Stream processing: snapshots, determinism, provenance

- **Carbone et al., *Lightweight Asynchronous Snapshots*,** arXiv 2015 PP;
  *State Management in Apache Flink*, PVLDB 2017, PR **[V]**. Barrier
  snapshots without halting; coarse-grained rollback for upgrading a running
  job.
- **Akidau et al., *MillWheel*,** PVLDB 2013, PR **[V]**. Exactly-once by
  deduplicated record ids; outputs checkpointed *before* they are
  acknowledged — the rule a tool-call journal needs.
- **Murray et al., *Naiad*,** SOSP 2013, PR **[V]**. Timestamps with loop
  counters and progress tracking: knowing when a cyclic computation is done.
- **Gulisano et al., *ScaleJoin* / ScaleGate,** IEEE BigData 2015, PR
  **[V]**; ***Viper*,** FGCS 2018, and ***STRETCH*,** TPDS 2022, PR **[V]**.
  Deterministic output under parallelism by merge-sorting timestamped inputs
  in the transport layer; elastic reconfiguration without moving state.
- **Palyvos-Giannas et al., *GeneaLog*,** Middleware 2018, PR **[V]**;
  ***Ananke*,** PVLDB 2021, PR **[V]**; research summary at ApPLIED 2022
  **[V]**. Backward provenance with constant-size tuple annotations; live
  forward provenance that marks a source *expired* once it can no longer
  contribute.

## 2 Durability, transactions and replay for agents (2024–2026)

Almost all preprints, most from 2026. Together they are the closest the
literature comes to the engine survey's §3.3–§3.4.

| Work | Status | Mechanism and claim | Dims |
| --- | --- | --- | --- |
| Patil et al., *GoEX* (2404.06921) | PP **[V]** | Post-facto validation instead of pre-validation; runtime primitives *undo* and *damage confinement*. | D4 D7 |
| Chang & Geng, *SagaLLM* (2503.11951) | PP **[V]** | Plan steps with compensations, checkpoints and validator agents; saga consistency instead of ACID. | D1 D3 |
| Geng & Chang, *ALAS* (2511.03094) | PP **[V]** | Versioned execution log for restore points; localized plan repair under declared retry, timeout and compensation policies. | D1 D3 D14 |
| Mohammadi et al., *Atomix* (2602.14849) | PP **[V]** | Progress-aware transactions over tool calls; commit releases buffered effects, abort compensates reversible ones. | D3 D5 |
| Balakrishnan et al. (Meta), *LogAct* (2604.07988) | PP **[V]** | Agent as a state machine over a shared log: actions logged before execution, vetoable by pluggable voters, recovered from the log; ~3 % utility cost. Argues replay-based durable execution assumes predeclared control flow, which agents lack. | D3 D7 D12 |
| Wu et al., *Crab* (2604.28138) | PP **[V]** | On shell-intensive and code-repair workloads, chat-history-only checkpoints recover correctly 8 % of the time; eBPF-classified OS effects drive turn-aligned sandbox checkpoints (100 %, up to −87 % traffic). | D3 |
| Lyu et al., *CoAgent* (2606.15376) | PP **[V]** | Concurrency control for multi-agent writes: fixed serialization order, speculative writes, LLM-judged invalidation, inverse operations. | D2 D5 |
| Perera, Leymann et al., *Robust Agent Compensation* (2605.03409) | ACM CAIS 2026, PR **[V]** | Log-based compensation attached through framework hooks (LangChain; LangGraph without code change); "1.5–8× or more" better latency and token use than LLM-driven recovery on τ-bench and REALM-Bench. | D1 D3 |
| Zhang, *Agent libOS* (2606.03895) | PP, single author **[V]** | Admission (capabilities, approvals, budgets) separated from an evidence log that "records but never grants authority"; prepare–dispatch–settle "exposes ambiguity and prevents blind replay". Disclaims injection prevention, sandboxing and rollback of irreversible effects. | D3 D7 D12 |
| Khan, *Resume Means Resume* (2608.03836) | PP, single author **[V]** | A six-property resume contract model-checked in TLA+, and a deterministic conformance harness over pinned releases: LangGraph 1.2.9 exactly-once across interrupts but at-least-once across SIGKILL; CrewAI 1.15.2 re-runs completed side-effecting methods against its own docs; pydantic-graph 1.x cannot resume after a crash inside a node; consume-once interrupts break under concurrent resumers. | D3 D4 D5 |
| Trofimov & Novikov, *When Tool Calls Succeed but Workflows Fail* (2609.15397) | PP **[V]** | Effect-history model; eight anomalies at the agent–tool boundary; across 98,291 MCP tools none of the required boundary capabilities is fully expressible in the standard annotation vocabulary (2026-09). | D3 D5 D7 |
| Li, *Where Does Exactly-Once Live?* (2609.29095) | PP, single author **[V]** | Fault-injection sandbox, 25,930 episodes: without read-back, frontier models duplicate writes in 56 % (in-flight) to 74 % (redelivered) of episodes and report success in 90 % of those; idempotency keys on every write cut duplicates 28 % → 4 % under heavy-tailed delays; verification alone cannot give exactly-once. | D3 D12 |
| Xu et al., *CapLease* (2608.01710, titled *Beyond Single-Use Tokens*) | PP, no measurements **[V]** | "Semantic replay": retries and crash-resume re-spend one human approval under fresh token ids. Binds a confirmation to a canonical action with issue–prepare–commit. | D3 D4 D7 |
| Chawla & Koul, *Chronicle* (2609.20625) | PP **[V]** | Records a run at its nondeterministic cut points; replays some from the recording and runs the rest live against new code — regression tests from incidents. | D12 D14 |
| Seyedkazemi Ardebili, *NovaFabric* (2609.12582) | PP, single author **[V]** | Signed, Merkle-logged "run capsules" with W3C PROV lineage. Replay with every model response served from the capsule worked for 10/10 workloads, but only **2/10 tool-using** ones completed — tool responses were not substituted. | D3 D12 |

Two serving papers touch pause and resume from below: **Abhyankar et al.,
*InferCept*,** ICML 2024, PR **[V]** — re-computing context after a tool or
human pause is 37–40 % of forward time; per-pause preserve/swap/discard —
and **Mei et al., *AIOS*,** PP **[V]** — an agent kernel with context save
and restore.

## 3 Orchestration, planning and multi-agent systems

### 3.1 Loops and plans

- **ReAct** (Yao et al., ICLR 2023, PR **[U]**): interleaved thought,
  action, observation — the loop every SDK implements.
- **Reflexion** (Shinn et al., NeurIPS 2023, PR **[U]**): verbal
  self-critique carried across attempts — a retry loop with state.
- **Plan-and-Solve** (ACL 2023, PR **[U]**), **ReWOO** (PP **[V]**: whole
  plan with placeholders up front, observations never re-enter the planner;
  ~5× fewer tokens).
- **LLMCompiler** (Kim et al., ICML 2024, PR **[V]**): planner emits a DAG
  of function calls dispatched as dependencies resolve; up to 3.7× lower
  latency than ReAct. The research form of engine-side fan-out (D5).
- **Tree / Graph of Thoughts** (NeurIPS 2023, AAAI 2024, PR **[U]**):
  control flow over model calls as search.
- **CodeAct** (Wang et al., ICML 2024, PR **[V]**): actions as executable
  Python, up to +20 % success across 17 models — workflow-as-code inside the
  agent; the basis of smolagents.
- **CoALA** (Sumers et al., TMLR 2024, PR **[V]**): working, episodic,
  semantic and procedural memory and a decision cycle — the nearest thing to
  a reference architecture for agent state (D2).

### 3.2 Multi-agent frameworks and why they fail

- **AutoGen** (COLM 2024, PR **[V]**), **MetaGPT** (ICLR 2024, PR **[V]**:
  SOPs as an assembly line exchanging structured artifacts), **ChatDev** (ACL
  2024, PR **[V]**), **CAMEL** (NeurIPS 2023, PR **[U]**: catalogues role
  flipping and infinite loops).
- **Cemri et al., *Why Do Multi-Agent LLM Systems Fail?* (MAST),** NeurIPS
  2025 D&B, PR **[V]** (v3; v1 reported 1,000+ traces): 14 failure modes in three
  groups — specification and system design, inter-agent misalignment, task
  verification — from 1,600+ annotated traces across 7 frameworks. Failures
  are mostly design, not model.
- Framework bug studies: **Xue et al.,** ASE 2025 industry showcase, PR
  **[V]** — 1,026 bugs in LangChain, LlamaIndex, Haystack; **Zhu et al.,
  *Where Agent Frameworks Fall Short*** (2602.21806, PP **[V]**) — 5,669
  bugs across AutoGen, CrewAI, LangChain, LangGraph, MetaGPT, 76 %
  incorrect functionality, concentrated at APIs, configuration, parsing and
  serialization boundaries.
- **Zhang et al., *Who&When* failure attribution,** ICML 2025, PR **[V]**:
  best method 53.5 % on the responsible agent, 14.2 % on the decisive step.

### 3.3 Workflows as optimizable programs

DSPy (ICLR 2024, PR **[V]**: modules with signatures, compiled against a
metric), GPTSwarm (ICML 2024, PR **[V]**: agents as graphs whose prompts and
edges are optimized), AFlow (ICLR 2025, PR **[V]**: Monte Carlo tree search
over workflows written as code), ADAS (ICLR 2025, PR **[V]**: a meta-agent
writes agents as code), TextGrad (PP **[V]**; a Nature 2025 version exists **[U]**), EvoAgent (NAACL 2025, PR
**[V]**). The shared position: topology and prompts are parameters searched
against a metric, where engines treat them as hand-authored, versioned code.
A 2026 survey (Yue et al., 2603.22386, PP **[V]**) frames the field as
*agentic computation graphs* and separates the template, the realized graph
and the execution trace.

### 3.4 Languages and verification

LMQL (PLDI 2023, PR **[V]**), SGLang (NeurIPS 2024, PR **[V]**: fork/join
primitives plus a prefix-sharing runtime), PDL (PP **[V]**: declarative YAML
with typed variables). Verification is at the start: **Lean4Agent**
(2606.06523, PP **[V]**) models workflows and trajectories in Lean 4 with a
formal library and a verification loop; two 2025 preprints check plans with LTL and
model checkers (PAT-Agent at ASE 2025). All verify the plan, not the
runtime.

## 4 Serving and scheduling agent programs

The research consensus: a request-level model API discards the program
structure needed to schedule well, and exposing it pays an order of
magnitude.

| Work | Venue | Mechanism | Claim |
| --- | --- | --- | --- |
| Parrot (Lin et al.) | OSDI 2024, PR **[V]** | *Semantic variables* expose the dataflow between calls | up to 11.7× |
| InferCept | ICML 2024, PR **[V]** | preserve/swap/discard the KV cache per tool pause | 1.6–2× throughput |
| Teola / Ayo | ASPLOS 2025, PR **[V]** | primitive-level dataflow graph optimized across modules | up to 2.09× |
| KVFlow | NeurIPS 2025, PR **[V]** | workflow-aware KV eviction and prefetch by steps-to-execution | up to 2.19× |
| Autellix | PP **[V]** | the *program* as scheduling unit; least-attained-service at program level | 4–15× vs vLLM |
| Continuum (Li, …, Stoica) | PP, ICLR 2026 listing **[V]** | KV cache pinned for a time-to-live across tool calls; program-level FCFS | >8× lower completion time |
| ALTO | PP **[V]** | streamed partial outputs with nested ancestry metadata | 10–30 % latency |

The sweep found no peer-reviewed work on *fairness* across agent programs —
the published schedulers optimize completion time.

## 5 Authority, isolation and prompt injection

Moved to [agentic-workflow-security-survey.md](./agentic-workflow-security-survey.md),
which holds the research literature and the shipped controls together.

## 6 Memory and context

| Work | Venue | Mechanism | Note |
| --- | --- | --- | --- |
| MemGPT | PP **[V]** | OS-style paging between main context and recall/archival stores, driven by the model's own function calls | reference design |
| Generative Agents | UIST 2023, PR **[U]** | memory stream scored by recency, importance, relevance; periodic reflection | origin of consolidation |
| A-Mem | NeurIPS 2025, PR **[V]** | linked Zettelkasten notes; new memories rewrite old ones | memory writes are mutations |
| Mem0 | PP **[V]** (vendor) | extract → consolidate → retrieve with add/update/delete/no-op | the product moved to add-only in 2026 (engine survey §3.11) |
| Zep / Graphiti | PP **[V]** (vendor) | bi-temporal knowledge graph; facts superseded, not overwritten | "what was believed at time t" |
| Agent Workflow Memory | ICML 2025, PR **[V]** | reusable sub-routines induced from logged trajectories | +51 % relative on WebArena |
| ACON (2510.00615) | ICML 2026, PR **[V]** | compression *guidelines* optimized where compressed context failed and full context succeeded | −26–54 % peak tokens |
| ACE (Agentic Context Engineering, 2510.04618 — not the security survey's ACE) | ICLR 2026, PR **[V]** | names *context collapse* from repeated summary rewriting; incremental structured edits instead | +10.6 % |
| Lost in the Middle | TACL 2024, PR **[U]** | U-shaped use of long contexts | placement matters |

Benchmarks: LoCoMo (ACL 2024 **[V]**), LongMemEval (ICLR 2025 **[V]**; ~30 %
drop for commercial assistants), MemoryAgentBench (ICLR 2026 **[V]**; no
method masters retrieval, test-time learning, long-range understanding and
selective forgetting together). Surveys: context engineering (2507.13334
**[V]**), agent memory (2512.13564 **[V]**).

## 7 Evaluation

- **Environments with state checkers:** WebArena (ICLR 2024 **[U]**),
  OSWorld (NeurIPS 2024 D&B **[V]**), TheAgentCompany (NeurIPS 2025 **[V]**;
  partial credit by checkpoint, best agent ~30 % complete). Gradability comes
  from resettable environment snapshots.
- **Consistency over repeats:** τ-bench (PP **[V]**) introduced **pass^k** —
  every one of k trials succeeds — with gpt-4o under 25 % at pass^8 in
  retail; τ²-bench (PP **[V]**) adds a user who also acts on shared state.
  Rabanser, Kapoor et al. (ICML 2026, PR **[V]**): 12 reliability metrics;
  reliability improves far slower than accuracy.
- **Cost:** Kapoor et al., *AI Agents That Matter* (TMLR 2025 **[V]**):
  report cost–accuracy Pareto fronts; simple baselines often match. The
  Holistic Agent Leaderboard (ICLR 2026 **[V]**): 21,730 rollouts, and log
  inspection found agents searching for the benchmark. *Cost-of-Pass* (ICLR
  2026 **[V]**): money per correct answer. *Budget-Aware Tool-Use* (Google,
  COLM 2026, PR **[V]**): agents unaware of their budget plateau; showing the remaining
  budget shifts the frontier.
- **Judging trajectories:** Agent-as-a-Judge (ICML 2025 **[V]**; ~90 %
  agreement with experts vs ~70 % for LLM-as-a-judge); TRAIL (PP **[V]**;
  148 annotated traces, the best model scores 11 %).
- **Benchmark quality:** SWE-Bench+ (PP **[V]**) and *The SWE-Bench
  Illusion* (PP **[V]**) find leakage and memorization; the ABC checklist (PP
  **[V]**) finds task and reward flaws distorting scores by up to 100 %
  relative.
- **Humans in the loop:** Collaborative Gym (2412.15701, ICLR 2026, PR **[V]**):
  asynchronous agent–human–environment evaluation; collaborative agents win
  66–86 % with real users.

## 8 Observability, provenance and reproducibility

- **AgentOps** taxonomy (Dong et al., PP **[V]**): span types — agent,
  reasoning, planning, workflow, task, LLM, tool, evaluation, guardrail.
- **PROV-AGENT** (IEEE e-Science 2025, PR **[V]**): W3C PROV extended so
  prompts, responses, decisions and MCP calls link to downstream data — the
  nearest research template for engine provenance. **AgentTrace** (PP
  **[V]**): operational, cognitive and contextual log surfaces over
  OpenTelemetry.
- **Inference is not reproducible.** Atil et al. (Eval4NLP 2025 **[V]**):
  under settings configured to be deterministic, hosted models' accuracy
  varied up to 15 % across runs.
  Yuan et al. (NeurIPS 2025 oral **[V]**): floating-point non-associativity
  across batch size, GPU count and type. He (Thinking Machines blog, 2025
  **[V]** via secondary coverage): the cause is reduction kernels that are
  not batch-invariant; batch-invariant kernels make temperature-0 serving
  bit-reproducible at a throughput cost.

## 9 Triggers: what starts a run

Two searches, one over the database, stream and workflow literature and one
over agents that act without being prompted. The first is old and settled;
the second is young and mostly preprints.

### 9.1 Event–condition–action rules and active databases

- **HiPAC** (Dayal et al., SIGMOD Record 1988, PR **[V]** abstract; mode
  names from Paton & Díaz): event–condition–action rules and *coupling
  modes* — immediate, deferred to commit, or detached (HiPAC's own word is
  *decoupled*) into a transaction of their own — which decide whether a
  triggered action shares its cause's atomicity.
- **Starburst** (Widom & Finkelstein, SIGMOD 1990 **[V]**; Widom, Cochrane,
  Lindsay, VLDB 1991 **[V]**): set-oriented rules over transition tables,
  fired on the *net effect* since the last assertion point, so an insert
  undone in the same window fires nothing.
- **Ode** (Gehani & Jagadish, VLDB 1991 **[V]**): triggers armed explicitly,
  *once-only* or *perpetual* — arming separated from firing.
- **Aiken, Widom, Hellerstein,** SIGMOD 1992, PR **[V]**: static analysis of
  rule sets over a *triggering graph* — acyclic means termination, pairwise
  commutativity means confluence — naming the rules responsible when a
  property cannot be guaranteed.
- **Paton & Díaz, *Active Database Systems*,** CSUR 1999, PR **[V]**; Widom &
  Ceri (eds.), 1996 **[V]**: the reference frameworks; Paton & Díaz's
  knowledge and execution models (event granularity, coupling, net effect,
  cycle policy, priorities) are a checklist of what a trigger mechanism has
  to fix.

### 9.2 Composite events and complex event processing

Snoop (Chakravarthy et al., VLDB 1994, PR **[V]**) gave composite events
*parameter contexts* — recent, chronicle, continuous, cumulative — deciding
which occurrences combine and which are consumed; Zimmer & Unland (ICDE 1999,
PR **[U]**) separate instance *selection*, *consumption* and type pattern.
SASE (Wu, Diao, Rizvi, SIGMOD 2006 **[V]**) and SASE+ (Agrawal et al., SIGMOD
2008 **[V]**) evaluate windowed sequence patterns under selection strategies
— strict contiguity, partition contiguity, skip till next match, skip till
any match — that fix how many matches, and so how many runs, one stream
produces. Cayuga (Demers et al., *Towards Expressive Publish/Subscribe
Systems*, EDBT 2006 **[V]**; the system paper is CIDR 2007) adds stateful,
parameterised subscriptions between topic filters and stream SQL. Surveys:
Cugola & Margara (CSUR 2012 **[V]**), Giatrakos et al. (VLDB J. 2020 **[V]**).

### 9.3 Delivery, identity and idempotence

- **MillWheel** (PVLDB 2013 **[V]**; §1.2): exactly-once *effect* from
  at-least-once delivery, a record id per production, and atomic checkpoint
  of input id, state and output.
- **The Dataflow Model** (Akidau et al., PVLDB 2015 **[V]**): *triggers*
  decide when a window's result is emitted — on the watermark, after a
  delay, by count — with accumulation modes for re-firing on late data.
  **Akidau et al. 2021** (PVLDB **[V]**) define the watermark as an assertion
  of completeness, which is what "fire when nothing arrived by T" depends on.
- **Kafka** (Wang et al., SIGMOD 2021 **[V]**): idempotent producers and
  transactions committing output, state and offsets together; consistency
  separated from completeness.
- **Ramalingam & Vaswani, *Fault Tolerance via Idempotence*,** POPL 2013,
  PR **[V]**: idempotent failure-freedom from store-local transactions alone
  — the formal basis of "trigger at least once, key the run".
  **Helland, *Idempotence Is Not a Medical Condition*** (ACM Queue 2012
  **[V]**): dedup needs a stable request identity and remembered outcomes.
- **Log first, derive the rest.** DBLog (Netflix, PP **[V]**): change data
  capture interleaving snapshot chunks with the log under watermarks — the
  commit *is* the event, with no dual write. Kleppmann, Beresford, Svingen
  (CACM 2019 **[V]**) generalise it. Overeem et al. (JSS
  2021 **[V]**): five schema-evolution tactics observed in 19 event-sourced
  systems — durable trigger payloads will need one.

### 9.4 Correlation: new instance or existing one

**Barros, Decker, Dumas, Weber,** FASE 2007, PR **[V]**: correlation as
grouping message events into conversations; key- versus property-based
correlation and *instance-creating* versus *instance-routing* messages.
**Montesi & Carbone,** ICSOC 2011 **[V]**: a type system making correlation
to session instances unambiguous. **Motahari-Nezhad et al.,** VLDB J. 2011
**[V]**: each candidate correlation key yields a different process view —
the instance key is a modelling choice. In the workflow-pattern catalogue
(Russell et al., BPM report 2006 **[U]**) a signal nobody is waiting for is
either lost (*transient trigger*, WCP-23) or buffered (*persistent trigger*,
WCP-24); an external trigger reaching a running case is an exception with
an explicit handling choice (Russell et al., CAiSE 2006 **[V]**).

### 9.5 Time and overload

**Davidovič & Guliani, *Reliable Cron across the Planet*** (ACM Queue 2015
**[V]**): cron state replicated over Paxos; the leader records a launch
before acting and names the job by its scheduled instant, so a successor can
finish launches left open — and *skipping* is preferred to launching twice,
because tolerance for each differs per job. Eder, Panagos, Rabinovich (CAiSE
1999 **[U]**) compute activity deadlines from periodic and fixed-date
constraints; Durable Functions (§1.1) treats timers and awaited events as
replayed history entries. For overload: Aurora's load shedding (Tatbul et
al., VLDB 2003 **[V]**) drops by utility to hold latency; SEDA (Welsh et al.,
SOSP 2001 **[V]**) rejects at stage queues. No peer-reviewed treatment of
debounce or throttle *as trigger semantics* was found.

### 9.6 Agents that act unprompted

- **Over-eager by default.** Proactive Agent (Lu et al., ICLR 2025 **[V]**):
  66.5 % F1 at offering help when it is wanted, with false alarms scored.
  ProEvent (2607.17701, PP **[V]**): agents keeping a timetable from noisy
  chat streams act when they should not and mishandle cancellations; the
  best timetable success rate is 27.2 %. Proactivity-Gym (2609.37267, PP **[V]**): trust drops after one
  misaligned intervention even when its outcome was right.
- **A cheap gate in front.** A small temporal-graph controller deciding
  whether to wake the model had the highest AUC of nine trigger
  architectures, three of them LLM-based, at 4–7× the speed of the
  single-forward LLM triggers (2605.30152, PP **[V]**). Horvitz's mixed-initiative principles
  (CHI 1999 **[V]**) — act, ask or stay silent by expected utility — and the
  interruption-timing literature (Adamczyk & Bailey, CHI 2004 **[V]**;
  Chen et al., CHI 2025 **[V]**) put deliveries and questions at task
  boundaries.
- **Time and events in benchmarks.** TimeArena (ACL 2024 **[V]**), Gaia2 on
  Meta's ARE platform (2509.17158, PP **[V]**: 1,120 scenarios with injected
  events in advancing time), τ²-bench's dual control and Collaborative Gym's
  asynchrony (§7). *Learning to Wait* (2512.16262, PP **[V]**): agents
  predicting their own re-check delay instead of polling. MemGPT's
  `request_heartbeat` is an early self-continuation primitive.
- **Long runs decay.** METR's 50 % task horizon (2503.14499, PP **[V]**)
  doubled roughly every seven months to 2025, far shorter at 80 %
  reliability; Vending-Bench (2502.15840, PP **[V]**) shows coherence
  decaying over 20 M-token runs; *The Illusion of Diminishing Returns* (ICLR
  2026 **[V]**) attributes part of it to *self-conditioning* on the run's own
  errors. The AI Scientist relaunched itself by system call and tried to edit
  its own time limit (2408.06292, PP **[V]**).
- **Triggers that propagate.** Morris II (Cohen, Bitton, Nassi, 2403.02817,
  PP **[V]**): a self-replicating prompt spreads through RAG email assistants
  with no click, each received message triggering a reply; Prompt Infection
  (2410.07283, PP **[V]**) does the same between agents; malfunction
  amplification (EMNLP 2025 **[V]**) drives agents into loops above 80 % of
  the time. HomeGuard (Chi et al., DSN 2020, PR **[V]**) classifies covert and
  loop triggering among smart-home rules and finds them with SMT; trigger-
  action programming studies (Ur et al., CHI 2014/2016 **[V]**; Brackenbury
  et al., CHI 2019 **[V]**) find users confuse events with states.
- **Oversight without a person present.** OpenAI's *Practices for Governing
  Agentic AI Systems* (white paper 2023 **[V]**): constrain the action space,
  keep actions attributable and the agent interruptible; *Visibility into AI
  Agents* (FAccT 2024 **[V]**): agent identifiers, real-time monitoring,
  activity logs that follow delayed effects; OWASP LLM10:2025 names
  denial-of-wallet. No academic study of denial-of-wallet against agents was
  found.

### 9.7 The questions a trigger mechanism must answer

The two literatures meet on seven:

1. **What is the event** — a tuple, a set, the net effect after coalescing;
   for composites, which occurrence is selected and whether it is consumed?
2. **When does the run start relative to its cause's commit** — the coupling
   mode; distributed, a trigger is durable when its cause commits (log
   first, outbox, CDC), not after.
3. **New instance or existing one** — the correlation key, and whether a
   signal nobody awaits is dropped or buffered.
4. **How many runs per cause** — delivery is at-least-once, so "once" is an
   effect of a stable trigger identity plus idempotence; for time, the
   identity is the scheduled instant and the policy says whether a missed or
   uncertain launch is skipped or retried.
5. **In what order, and when is "nothing more will come"** — per-key order
   versus completeness, decided by watermarks.
6. **What under overload** — admit, delay, shed or coalesce, against a
   stated objective.
7. **Do triggered runs that trigger runs terminate** — acyclicity and
   confluence of the triggering graph, which is the agent-worm problem in
   1992's vocabulary; and should the model decide to act at all, or a
   cheaper gate in front of it?

## 10 What the literature settles and leaves open

**Settled, by pre-LLM systems research.**

- Durability by logging nondeterministic events and replaying a
  deterministic orchestrator is sound (Elnozahy et al.), formally equivalent
  to the high-level semantics (Durable Functions), has a known logging lower
  bound (Halfmoon), and need not cost a round trip per step (Netherite,
  Boki).
- Exactly-once holds inside a boundary with intents and deduplication ids
  (Olive, Beldi, MillWheel); compensation is the accepted substitute for
  atomicity beyond it (Sagas).
- Consistent cuts of parallel work need no stop-the-world (Chandy–Lamport,
  Flink); a superstep is a sound checkpoint unit (Pregel).
- Determinism under parallelism is a transport property (ScaleGate,
  Viper); provenance at constant per-tuple cost, with expiry, is achievable
  (GeneaLog, Ananke).

- Trigger semantics — coupling, net effect, selection and consumption,
  correlation, idempotent identity, watermarks, termination of cascading
  rules — were worked out for databases, streams and workflows between 1988
  and 2021 (§9.1–§9.5).

**Open, and argued mostly in 2026 preprints.**

1. **Replay of model-chosen control flow.** Hosted inference is not
   reproducible, so model outputs must be journalled, not regenerated — and
   LogAct argues the deterministic-replay contract does not fit control flow
   the model decides at run time.
2. **Exactly-once across the tool boundary** is unattainable by
   verification alone (Li), inexpressible in the MCP tool annotations of 2026
   (Trofimov & Novikov), and recording model I/O does not make tool
   workloads replayable (NovaFabric 2/10; Crab 8 %).
3. **Resume soundness in shipping frameworks** fails across crashes and
   concurrent resumers (Khan).
4. **Approvals must be durable and bound to the action,** or retries and
   resumes re-spend them (CapLease).
5. **Versioning in-flight runs** has old formal theory for static schemas
   (Rinderle et al.) and, for agents, only partial replay (Chronicle) and
   plan-local repair (ALAS).
6. **Scheduling agent programs** wins an order of magnitude when program
   structure reaches the server (Parrot, Autellix, Continuum), but no work
   addresses fairness across programs.
7. **Unprompted runs.** Agents act too readily when nobody asked (ProEvent,
   Proactive Agent), reliability decays with run length (METR,
   Vending-Bench), and runs that trigger runs propagate injected prompts
   (Morris II, Prompt Infection). No work applies the triggering-graph
   analysis of 1992 to agent triggers, and no benchmark tests multi-day
   trigger cascades.

What is settled and open for security — enforcement outside the model,
labels, approvals — is the security survey's §5.

**Not searched.** Fairness and admission control across concurrent agent
programs, beyond the serving papers of §4.

## References

Read by the compiling pass on 2026-10-09 unless tagged [U] above. arXiv
identifiers resolve at `https://arxiv.org/abs/<id>`.

- ACE (NDSS 2026) — <https://arxiv.org/abs/2504.20984>
- ACE, Agentic Context Engineering (ICLR 2026) — <https://arxiv.org/abs/2510.04618>
- ACON — <https://arxiv.org/abs/2510.00615>
- AFlow — <https://arxiv.org/abs/2410.10762>
- AgentDojo — <https://arxiv.org/abs/2406.13352>
- Agent-as-a-Judge — <https://arxiv.org/abs/2410.10934>
- Agent libOS — <https://arxiv.org/abs/2606.03895>
- AgentOps — <https://arxiv.org/abs/2411.05285>
- Agent Workflow Memory — <https://arxiv.org/abs/2409.07429>
- AI Agents That Matter — <https://arxiv.org/abs/2407.01502>
- AirGapAgent — <https://arxiv.org/abs/2405.05175>
- A-Mem — <https://arxiv.org/abs/2502.12110>
- ASB — <https://arxiv.org/abs/2410.02644>
- Atomix — <https://arxiv.org/abs/2602.14849>
- Attacker Moves Second — <https://arxiv.org/abs/2510.09023>
- Authenticated Delegation — <https://arxiv.org/abs/2501.09674>
- Autellix — <https://arxiv.org/abs/2502.13965>
- Beldi (OSDI 2020) — <https://www.usenix.org/conference/osdi20/presentation/zhang-haoran>
- Budget-Aware Tool-Use — <https://arxiv.org/abs/2511.17006>
- CaMeL — <https://arxiv.org/abs/2503.18813>
- CapLease — <https://arxiv.org/abs/2608.01710>
- Chronicle — <https://arxiv.org/abs/2609.20625>
- CoAgent — <https://arxiv.org/abs/2606.15376>
- Collaborative Gym — <https://arxiv.org/abs/2412.15701>
- CodeAct — <https://arxiv.org/abs/2402.01030>
- Continuum — <https://arxiv.org/abs/2511.02230>
- Cost-of-Pass — <https://arxiv.org/abs/2504.13359>
- Crab — <https://arxiv.org/abs/2604.28138>
- DBOS (PVLDB) — <https://doi.org/10.14778/3485450.3485454>
- Design Patterns for Securing LLM Agents — <https://arxiv.org/abs/2506.08837>
- DRIFT — <https://arxiv.org/abs/2506.12104>
- DSPy — <https://arxiv.org/abs/2310.03714>
- Durable Functions (OOPSLA 2021) — <https://doi.org/10.1145/3485510>
- Exactly-once (LIMBO) — <https://arxiv.org/abs/2609.29095>
- FIDES — <https://arxiv.org/abs/2505.23643>
- Firewalls / stronger benchmarks — <https://arxiv.org/abs/2510.05244>
- Flink snapshots — <https://arxiv.org/abs/1506.08603>
- Framework bug study (2026) — <https://arxiv.org/abs/2602.21806>
- GoEX — <https://arxiv.org/abs/2404.06921>
- GPTSwarm — <https://arxiv.org/abs/2402.16823>
- Greshake et al. — <https://arxiv.org/abs/2302.12173>
- Halfmoon (SOSP 2023) — <https://doi.org/10.1145/3600006.3613154>
- Holistic Agent Leaderboard — <https://arxiv.org/abs/2510.11977>
- InferCept — <https://arxiv.org/abs/2402.01869>
- InjecAgent — <https://arxiv.org/abs/2403.02691>
- IntentCap — <https://arxiv.org/abs/2609.14631>
- IsolateGPT — <https://arxiv.org/abs/2403.04960>
- KVFlow — <https://arxiv.org/abs/2507.07400>
- Lean4Agent — <https://arxiv.org/abs/2606.06523>
- Levels of Autonomy — <https://arxiv.org/abs/2506.12469>
- LLMCompiler — <https://arxiv.org/abs/2312.04511>
- LogAct — <https://arxiv.org/abs/2604.07988>
- LongMemEval — <https://arxiv.org/abs/2410.10813>
- Magentic-UI — <https://arxiv.org/abs/2507.22358>
- MAST — <https://arxiv.org/abs/2503.13657>
- MCP attack vectors — <https://arxiv.org/abs/2506.02040>
- MCPTox — <https://arxiv.org/abs/2508.14925>
- MELON — <https://arxiv.org/abs/2502.05174>
- MemGPT — <https://arxiv.org/abs/2310.08560>
- Mem0 — <https://arxiv.org/abs/2504.19413>
- MemoryAgentBench — <https://arxiv.org/abs/2507.05257>
- MillWheel — <https://doi.org/10.14778/2536222.2536229>
- MiniScope — <https://arxiv.org/abs/2512.11147>
- Netherite — <https://doi.org/10.14778/3529337.3529344>
- NeuroTaint — <https://arxiv.org/abs/2604.23374>
- Nondeterminism (Atil et al.) — <https://arxiv.org/abs/2408.04667>
- Nondeterminism (Yuan et al.) — <https://arxiv.org/abs/2506.09501>
- Nondeterminism (Thinking Machines) — <https://thinkingmachines.ai/blog/defeating-nondeterminism-in-llm-inference/>
- NovaFabric — <https://arxiv.org/abs/2609.12582>
- Out-of-band defenses, adaptive evaluation — <https://arxiv.org/abs/2606.26479>
- OSWorld — <https://arxiv.org/abs/2404.07972>
- Parrot (OSDI 2024) — <https://www.usenix.org/conference/osdi24/presentation/lin-chaofan>
- Progent — <https://arxiv.org/abs/2504.11703>
- PROV-AGENT — <https://arxiv.org/abs/2508.02866>
- Ananke (PVLDB 2021) — <https://doi.org/10.14778/3430915.3430928>
- Reliability science — <https://arxiv.org/abs/2602.16666>
- Resume Means Resume — <https://arxiv.org/abs/2608.03836>
- Robust Agent Compensation — <https://arxiv.org/abs/2605.03409>
- Rollback-recovery survey — <https://doi.org/10.1145/568522.568525>
- RTBAS — <https://arxiv.org/abs/2502.08966>
- SagaLLM — <https://arxiv.org/abs/2503.11951>
- SandboxEscapeBench — <https://arxiv.org/abs/2603.02277>
- SGLang — <https://arxiv.org/abs/2312.07104>
- Spotlighting — <https://arxiv.org/abs/2403.14720>
- Stream processing research summary — <https://research.chalmers.se/publication/531787>
- StruQ — <https://arxiv.org/abs/2402.06363>
- Styx — <https://doi.org/10.1145/3725363>
- TheAgentCompany — <https://arxiv.org/abs/2412.14161>
- TOCTOU-Bench — <https://arxiv.org/abs/2508.17155>
- Tool-boundary anomalies — <https://arxiv.org/abs/2609.15397>
- ToolEmu — <https://arxiv.org/abs/2309.15817>
- TRAIL — <https://arxiv.org/abs/2505.08638>
- Tracking Capabilities for Safer Agents — <https://arxiv.org/abs/2603.00991>
- τ-bench — <https://arxiv.org/abs/2406.12045>
- WASP — <https://arxiv.org/abs/2504.18575>
- Who&When — <https://arxiv.org/abs/2505.00212>
- Workflow optimization survey — <https://arxiv.org/abs/2603.22386>
- Adamczyk & Bailey (CHI 2004) — <https://doi.org/10.1145/985692.985727>
- Aiken, Widom, Hellerstein (SIGMOD 1992) — <https://doi.org/10.1145/130283.130296>
- ARE / Gaia2 — <https://arxiv.org/abs/2509.17158>
- Breaking Agents (malfunction amplification) — <https://arxiv.org/abs/2407.20859>
- Cayuga, Towards Expressive Publish/Subscribe Systems (EDBT 2006) — <https://doi.org/10.1007/11687238_38>
- Correlation Patterns in SOA (FASE 2007) — <https://doi.org/10.1007/978-3-540-71289-3_20>
- Cugola & Margara (CSUR 2012) — <https://doi.org/10.1145/2187671.2187677>
- Dataflow Model (PVLDB 2015) — <https://doi.org/10.14778/2824032.2824076>
- DBLog — <https://arxiv.org/abs/2010.12597>
- Event-sourcing schema evolution (Overeem et al., JSS 2021) — <https://arxiv.org/abs/2104.01146>
- Do Proactive Agents Need an LLM to Decide When to Act? — <https://arxiv.org/abs/2605.30152>
- Fault Tolerance via Idempotence (POPL 2013) — <https://doi.org/10.1145/2429069.2429100>
- Horvitz, Mixed-Initiative User Interfaces (CHI 1999) — <https://erichorvitz.com/uiact.htm>
- Idempotence Is Not a Medical Condition — <https://doi.org/10.1145/2181796.2187821>
- Illusion of Diminishing Returns — <https://arxiv.org/abs/2509.09677>
- Kafka, Consistency and Completeness (SIGMOD 2021) — <https://doi.org/10.1145/3448016.3457556>
- Kleppmann, Beresford, Svingen, Online Event Processing (CACM 2019) — <https://doi.org/10.1145/3312527>
- Learning to Wait — <https://arxiv.org/abs/2512.16262>
- METR task horizons — <https://arxiv.org/abs/2503.14499>
- Montesi & Carbone, Programming Services with Correlation Sets (ICSOC 2011) — <https://doi.org/10.1007/978-3-642-25535-9_9>
- Morris II — <https://arxiv.org/abs/2403.02817>
- Need Help? Proactive programming assistants (CHI 2025) — <https://arxiv.org/abs/2410.04596>
- OWASP LLM10:2025 Unbounded Consumption — <https://genai.owasp.org/llmrisk/llm102025-unbounded-consumption/>
- Paton & Díaz, Active Database Systems (CSUR 1999) — <https://doi.org/10.1145/311531.311623>
- Practices for Governing Agentic AI Systems — <https://cdn.openai.com/papers/practices-for-governing-agentic-ai-systems.pdf>
- ProEvent — <https://arxiv.org/abs/2607.17701>
- Proactive Agent (ICLR 2025) — <https://arxiv.org/abs/2410.12361>
- Proactivity-Gym — <https://arxiv.org/abs/2609.37267>
- Prompt Infection — <https://arxiv.org/abs/2410.07283>
- Reliable Cron across the Planet — <https://queue.acm.org/detail.cfm?id=2745840>
- SASE (SIGMOD 2006) — <https://doi.org/10.1145/1142473.1142520>
- SASE+ (SIGMOD 2008) — <https://doi.org/10.1145/1376616.1376634>
- Snoop composite events (VLDB 1994) — <https://www.vldb.org/conf/1994/P606.PDF>
- Starburst rules (VLDB 1991) — <https://vldb.org/dblp/db/conf/vldb/WidomCL91.html>
- TimeArena — <https://arxiv.org/abs/2402.05733>
- Trigger-action programming bugs (CHI 2019) — <https://doi.org/10.1145/3290605.3300782>
- Vending-Bench — <https://arxiv.org/abs/2502.15840>
- Visibility into AI Agents (FAccT 2024) — <https://arxiv.org/abs/2401.13138>
- Watermarks in Stream Processing (PVLDB 2021) — <https://doi.org/10.14778/3476311.3476389>
- Workflow Exception Handling Patterns (CAiSE 2006) — <https://doi.org/10.1007/11767138_20>
- Zimmer & Unland, semantics of complex events (ICDE 1999) — <https://doi.org/10.1109/icde.1999.754955>
