---
type: explanation
audience: whoever decides whether boxer gets a data-centric agent harness, what its parts are, what it promises a person, how it keeps data in and injected text out, and how its quality is checked — read before the ADRs that would take those decisions
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.
> Compiled 2026-10-10. Nothing here is a decision. This page is the merge of
> two same-day consolidations of four working pages (a policy inventory of
> the tier with candidate seams; a journal, registry and profile sketch with
> its laws; a decomposition into subsystems and concerns with placement,
> ensembles, stateful tools, compaction and self-improvement; a security and
> provenance study with five spikes). The four pages were folded in and
> removed; what they held beyond this page — the spike SQL, the policy
> inventory, the concern-by-subsystem matrix — is in §2 and Appendix C, and
> the rest was duplication or detail an ADR would re-derive from the tree.
> Provenance tags carry over: **[V]** the primary source was read on the
> compile date (a paper's text, a project's documentation or source, or a
> file in this tree named by symbol, not line); **[U]** a secondary source,
> an abstract, or the compiler's inference. Spike figures carry their
> conditions (Appendix A) and are the spike's own; a spike is not a trial.
> §4 follows the [threat model](./agent-harness-threat-model.md) compiled
> from the owner's interview later the same day; where the source pages and
> that page differ, the threat model wins, and §4.11 lists what changed.
> The changes this design would ask of accepted ADRs are in the companion
> [agent-harness-adr-impact](./agent-harness-adr-impact.md). On 2026-10-11
> the decisions were drafted as ADR-0307 to ADR-0317 (proposed), and
> ADR-0303 was superseded by ADR-0308; citations of ADR-0303 as proposed
> below are as of the compile date.

# A data-centric agent harness for boxer

The question: if boxer's agentic tier — the chat coordinator, the model
service, the dispatcher, the moderator, the trail — were rebuilt on the
premises of [why-boxer](../explanation/why-boxer.md) rather than around the
chat window it grew in, what would it be, for whom, with what security model,
and how would anyone know it works? The answer below is one design: every
byte a model sees is a query over an append-only run journal; every act is
written to that journal before it happens; authority, labels and budgets are
three carriers of one guard algebra that can only refuse; the choice of
components and constants is data recorded on every run; and the properties
that matter are queries that return the rows that break them. §1 maps the
design space as the literature and contemporary systems draw it, §2 the
end-to-end architecture, §3 the product statement, §4 the security model and
§5 the quality-control model; §6 collects the open decisions and §7 the ADRs
they would need.

Caveats first.

- **Almost nothing below is built.** The run journal
  ([ADR-0303](../adr/0303-a-conversation-outlives-its-process-a-run-journal-written-before-each-act.md))
  is proposed and unbuilt; the chat holds its history in memory, has no
  compaction, and loses the conversation on a crash. The label machinery
  that exists is one `confined` bit checked at two walls, a per-conversation
  `tainted` flag that is shown and recorded but never gates, and `Untrusted`
  marks on operation output **[V]**. Everything else is design.
- **Several decisions were taken in a design discussion on the compile
  date, not in any ADR,** and are carried here as working assumptions: the
  journal is the only source of model input; the context projection is SQL,
  run per call in a stateless `clickhouse-local` worker; guards combine by
  maximum and may only add refusals; labels form a join-semilattice per
  entry, lowered only by a journaled entry; and what SQL cannot do is a new
  small pass shape beside nanopass rather than a generalisation of it.
- **Figures are from one-machine spikes** on `clickhouse local` and a
  scratch `clickhouse server`, both 26.9.13.15, and on the `watermark` crate
  built from the working tree. None is a trial result; none should be quoted
  without its row in Appendix A.
- **The literature is mostly 2026 preprints** graded in §1; several headline
  results are single runs on small subsets. Four classic security references
  could not be obtained and are tagged **[U]**.
- **Four premises of the security brief did not match the tree** and are
  corrected in §4: ADR-0025's vault is unbuilt; ADR-0198 has no row-policy
  design; no ADR says keys stay out of SQL text; screenshots reach the model
  unscaled.
- **ADR-0026's threat model ("hygiene, not security") stands** for
  in-process code. §4 adds enforcement against four principals that are not
  in-process code; it does not make the process a boundary.

## In brief

- A harness is the software around the model. Its loop is small
  everywhere; its difficulty comes from a few concerns that touch every
  step, and from tuned constants whose evidence is not recorded (§1.1).
- Contemporary harnesses converged on a small loop, an append-only log as
  the source of model input, compaction that appends, permission lattices
  combined by maximum, and composition declared per extension point (§1.2).
  None produces a record an outsider can verify without trusting the
  runtime that wrote it.
- The boxer-shaped answer: an append-only **run journal** is the only input;
  every wire request is a **SQL projection** of a journal prefix under a
  **profile**; **guards** combine by maximum and may only add refusals;
  **laws** are queries that return the rows that break them (§2).
- Security is stated for a **cell**: one person's deployment, whose
  declared boundary is the security boundary and whose owner is the
  organisation. Labels carry an integrity level, a TLP tier and two
  provenance marks on every journal entry; **sinks** are the edges that
  leave the declared cell; a **non-Truman agent schema** bounds reads;
  re-entry of the model's own output is detected by journal metadata;
  clipboard and screen are relay channels; chain heads anchored outside the
  cell make the record tamper-evident (§4). Five data shapes are cheap to
  reserve in the journal now and expensive later (§4.9).
- Quality is controlled in layers, from pass properties and laws to offline
  counterfactual replay, verification at run time, one-step checks and
  paired trials. Self-improvement is a new profile version admitted through
  those layers and adopted by new runs only (§5).

---

## 1 The design space, from the literature

### 1.1 Where harness complexity comes from

The loop at the centre of a harness is small everywhere: `runTurn` in
`apps/chat` is about a hundred lines; DeepSeek Harness's agent-loop package
and its framework kernel are each under 3k lines of a 389k-line tree **[V]**;
λA writes the ReAct loop as one bounded fixpoint **[V]**. The size and the
difficulty sit elsewhere, in five sources:

| Source | What produces it | In this tree (2026-10-10) | What reduces it |
| --- | --- | --- | --- |
| Essential | the model: context is its only memory, its output is untrusted, its acts leave the process, tokens cost | the authority layer — grants, ceiling, ladder, labels (`public/keelson/runtime/agent`, about 7.7k lines) | nothing; keep the core small and the concerns separate |
| Interaction | a few cross-cutting concerns, each touching every step | the screenshot setting touches consent, sensitivity, history rewriting, the ration estimate, a provider rule, the model's notes and the tool set — nine files of `apps/chat` | a declared composition rule per seam, with its laws checked |
| Environmental | providers × model generations | 22 provider- or model-named sites in `openaichat`, applied unconditionally | per-route capability declarations; closed, versioned entry kinds |
| Epistemic | tuned constants and prompt text whose evidence was not recorded | 24 rounds allowed while the moderator escalates at 16; "80 % of context" computed three ways | constants as recorded profile data, compared by query |
| Accidental | structure that does not factor the above | conversation, turn and call state under one lock in the coordinator | the seams |

The tier is young — the first chat commit is dated 2026-09-27 and eleven
agentic ADRs landed by 2026-10-09 — so what accumulated is rate, not age:
each ADR landed in the one object that existed. A workaround for one model
generation written as unconditional behaviour (forcing `tool_choice: none`
on a last round, folding late system messages for one template family)
outlives the generation. The policy is not the debt; encoding it where it
cannot be swapped or measured is. Two results suggest much tuned material is
inert or wrong rather than merely unmeasured: NLAH cut a harness policy from
about 60k to 2.9k tokens with comparable scores **[V]**, on single runs; and
"Context Compaction Theory" measured a commercial compaction endpoint
answering membership queries "close to a random guess", worse than a Bloom
filter of the same size **[V]**.

### 1.2 What contemporary harnesses converged on

Read from source or official documentation on the compile date **[V]**:
OpenHands Software Agent SDK, OpenAI Codex, Claude Code and the Claude Agent
SDK, LangGraph, pi and pi-durable, opencode, Letta, Temporal, Restate, DBOS,
DSPy, DeepSeek Harness, and a lighter reading of Pydantic AI, the OpenAI
Agents SDK, Goose, Mastra, Google ADK, Microsoft Agent Framework, Inngest
AgentKit and smolagents. They started far apart — "everything built in",
"radically minimal", "everything is a plugin" — and moved toward the middle.
An independent study of the convergence (arXiv 2608.23953) names external
verifiability as the dimension none of them has **[V]**.

| Convergence | Evidence | What boxer takes |
| --- | --- | --- |
| Composition is declared per extension point, not once for the framework | pi-durable's per-hook table (replacement chain, observers, first wins); Codex's one rule per contributor kind; opencode's load-order mutation and Codex's "the hook that actually finished last" as the counter-examples | one shape per seam, order declared (§2.5) |
| A registry built once, then frozen; runs name components, never code | Codex's immutable `ExtensionRegistry`; pi-durable stores extension names; OpenHands agents are frozen configuration | the profile as data (§2.6) |
| Permissions merge in a lattice, maximum wins, scoped rules cannot loosen | Codex `Allow < Prompt < Forbidden`; Claude Code "deny > defer > ask > allow"; OpenHands maximum risk; Goose monotone; opencode's last-wins as the exception | guards `Allow ≤ Ask ≤ Deny` by maximum (§2.5) |
| The log is the source of model input | OpenHands "a read-only projection over the event log"; Codex "live and persisted histories remain identical"; pi logs even the system prompt as section patches; Letta's context as a pointer list over rows | the journal as the only input (§2.2) |
| Compaction appends; nothing is deleted | OpenHands tombstone set; pi and ADK span replace from a cut; Codex full replacement checkpoint; pi `context_edit` as an appended edit | span replace from a cut (§5.5) |
| Branches are a tree in the log | OpenHands and pi `parent_id`; Codex, Claude Code, Restate and DBOS fork by copying a prefix | a parent ordinal, if wanted (§6) |
| Entry kinds are closed and replayable forever | Codex still replays retired kinds; opencode versions each event; Claude Code's internal transcript forfeits outside replay | closed enum, ordinals never retired (§2.2) |
| Effects are sandwiched: intent, effect, outcome; replay safety declared per tool | pi-durable `replay: safe \| unsafe`; Temporal, Restate, DBOS record steps and never re-run one | effect class from `OperationSpec.Effect` (§2.4) |
| Traces are tables; observe first, interpret later | Restate `sys_journal`; DBOS one row per step; Codex's JSONL indexed in SQLite; Mastra spans to ClickHouse | the journal and trail on ClickHouse, laws as queries (§5.2) |
| Prefix stability is designed | DeepSeek Harness: stable sections first, dynamic facts appended as snapshot messages, the tool catalogue constant across modes, a "KV cache effect" note per package; its summariser replays the exact prefix | §2.3 |
| Less coaching, more durability | deepagents removed authored prompt text; pi added crash-safe infrastructure | §3 |
| One law written as an equation | LangGraph's reducer law `reducer(reducer(s, xs), ys) == reducer(s, xs + ys)`; everyone else states laws in prose | every law a query that returns violating rows (§5.2) |

DeepSeek Harness adds two invariants worth more than any seam: **model-visible
means logged** (every byte a model receives derives from an append-only
session log through a pure projection; decisions the model never sees are
still log events) and **prefix stability is designed** (stable sections
first, environment-bearing ones last, a constant tool catalogue, per-package
"KV cache effect" notes) **[V]**. Its composition vocabulary — singleton
slot, named registry, around-chain, monotonic restriction, scoped shadowing —
is the map §2.5 uses. What does not transfer from it: reversible runtime
loading (boxer declines runtime code loading,
[spatiotemporal-composability-lessons §5](../explanation/spatiotemporal-composability-lessons.md)),
open sum types by declaration merging, emergent chain order, and
agent-authored extensions (P1 and P6 of why-boxer).

Laws written as tests are rare: LangGraph's `DeltaChannel` requires reducers
to be "deterministic and batching-invariant" and ships a conformance
package; Codex validates execpolicy examples at load; OpenHands checks view
properties. Everyone else states orders and invariants in prose.

### 1.3 What the formal literature offers

About twenty works were read at least to their formal model and evaluation.
The usable parts are small — a label lattice, a projection, a compaction
bound — not a framework. Grouped by the concern they speak to; "fit" grades
how directly each applies: **direct** (adopt the definitions and laws),
**lens** (vocabulary and reasoning, no machinery), **later** (a deferred
subsystem). Classic references are recalled and should be re-checked before
an ADR cites them; recent papers read on the compile date carry **[V]**.

| Concern | Framework | Gives | Fit |
| --- | --- | --- | --- |
| decomposition | Parnas 1972 (information hiding); Cockburn's ports and adapters; Kang et al. FODA 1990 | hide each likely-to-change policy; producers as the shell, projections and guards as the core; profiles as feature-model configurations | direct |
| logs and projection | Lamport 1978, Schneider 1990 (state-machine replication); Meijer et al. 1991 (folds); Kahn 1974 (prefix-monotone streams); Hellerstein 2010 and Ameloot et al. 2011 (CALM); DBSP, differential dataflow; JIT-Agent `ξ≤t = ξ<t ⊕ (s,e,o)`, `vt = M(ξ<t, st)` **[V]** | one writer per run suffices; projections are folds; `AppendStable` is Kahn monotonicity; CALM says which SQL is monotone and so which passes keep the prefix; incremental maintenance if projection cost matters | direct |
| context capacity | Tirmazi, Bishop, Markelon, Mitzenmacher, "Context Compaction Theory" **[V]**; Yao 1979; Krause and Golovin 2014; Bloom 1970, count-min | selection versus generation games; minimum budget equals one-way communication complexity; sketch baselines a compactor must beat; submodular greedy selection | direct |
| effects and the loop | Plotkin and Power 2003, Plotkin and Pretnar 2009; Etas **[V]**; McCann's governance algebra **[V]**; SHEPHERD **[V]**; Clements et al. 2013; Harel 1987; Lamport's TLA+ | the loop as an interpreter of a small signature; a handler may discharge an effect, never erase its request; capability sets by union under sequencing; reversibility tiers; commutativity as the licence for parallelism; the turn machine as a statechart; model-check the resume protocol | direct |
| durability | ARIES; Elnozahy et al. 2002 (output commit); Garcia-Molina and Salem 1987 (sagas); Temporal, Restate, DBOS, pi-durable **[V]** | write-ahead; an emission is withheld or reported, never silently replayed; compensation as the middle tier | direct |
| authority and flow | Dennis and Van Horn 1966, Miller 2006; Denning 1976 **[V]**; Goguen and Meseguer 1982; Myers and Liskov 1997 **[V]**; LIO 2011 **[V]**; Biba 1977 **[U]**; LLMbda **[V]**, Fides **[V]**, CaMeL **[V]**; Bonatti et al. 2002, XACML; Schneider 2000, edit automata; Park and Sandhu UCON | grants as capabilities; labels as a lattice with flows checked by order; noninterference as the property; declassification by the owner's authority; a floating label bounded by a clearance; low-water-mark integrity; endorsement as an explicit act and a demonstrated implicit-flow leak to avoid; combining operators with laws; which guards are enforceable by watching the trace; authorisation re-evaluated as usage accumulates | direct |
| access control | Stonebraker and Wong 1974 **[V]**; Rizvi, Mendelzon, Sudarshan, Roy 2004 **[U]**; Denning, Denning, Schwartz 1979 **[V]**; Lunt 1989 **[U]**; Dwork 2006 **[V]** | query modification is Truman; the non-Truman alternative refuses rather than filters; query-set-size floors are defeated by trackers; aggregation versus inference; the only bound against inference is differential privacy | direct (§4.3) |
| provenance and measurement | Green, Karvounarakis, Tannen 2007 (semirings); Claessen and Hughes 2000 (`rapid` in the tree); Chen et al. 1998 (metamorphic); Dudík et al. 2011 (off-policy) | per-message provenance carries labels and costs through the SQL by one algebra; generators of journals for the laws; relations between runs where no oracle exists; the limit on learning from logged runs | direct |
| ensembles | Wang et al. (self-consistency); Li et al. 2402.05120; Brown et al. 2407.21787; Qi et al. rStar; Wang et al. MoA; Du et al. debate; Chen et al. NeurIPS 2024, Li et al. Self-MoA, Huang et al. as counter-evidence **[U]** | gains need a selection signal — an executable check, agreement between independent models, or estimated reliability; without one, more calls can hurt | direct (§5.4) |
| interaction and delegation | Horvitz 1999; Honda et al. (session types); Bollig, Függer, Nowak MSC **[V]**; Hewitt, Agha (actors) | when to ask; typed protocols; a global protocol projected to per-role programs | later |
| prompts as parameters | DSPy, DSPy Assertions **[V]**; Oracular Programming **[V]** | profiles as optimisable parameter state; choice points separate from search | later |

Not usable as foundations: the ArchAgents framework (eighteen theorems
without proofs, never mentions LLMs or tools), Meng et al.'s harness survey
(a useful checklist whose safety and liveness definitions coincide),
"Monadic Context Engineering" (withdrawn by its authors), and "Harness
Engineering as Categorical Architecture" (rests on ArchAgents) **[V]**. The
literature's own open problems are compositional verification of a whole
harness, replay of model-chosen control flow, and compaction under repeated
application. Two lessons cut across it: audit is a law, not a feature (Etas,
SHEPHERD, McCann); and the parts with proofs are small.

### 1.4 Combining weak models

Evidence here is **[U]** — a secondary summary and recall; the primary
papers were not re-read.

| Family | Representative work | Reported effect | Counter-evidence |
| --- | --- | --- | --- |
| Sample and vote | self-consistency; "More Agents Is All You Need" (2402.05120) | accuracy grows with samples | returns can rise then fall with more calls |
| Weighted weak sources | "Ask Me Anything" (2210.02441); Weaver | reliability-weighted aggregation beats a plain vote | — |
| Generate then verify | "Large Language Monkeys" (2407.21787); rStar (2408.06195) | coverage keeps growing; with an automatic verifier weak models pass far beyond single-sample scores | without a verifier, voting plateaus |
| Layered mixtures | Mixture-of-Agents (2406.04692) | open models in layers outscore one frontier model | Self-MoA (2502.00674): sampling the best model often beats mixing |
| Debate | Du et al. (2305.14325) | fewer errors | gains largely reduce to consistency across samples |

The common factor of the strong results is a reliable **selection signal**:
an executable check, agreement between independent models, or estimated
reliability per source. Boxer's machine-readable surfaces supply exact,
cheap verifiers (§5.4), and confined runs, which may use only local and
usually weaker routes, are where this matters most.

---

## 2 End-to-end architecture

### 2.1 One factoring rule

Every part of the harness relates to the run journal in one of four ways:

| Role | Signature (informal) | Pure? | Examples |
| --- | --- | --- | --- |
| **Producer** | performs an act and appends its intent and outcome | no | the model call, a tool call, a question to the person, the summariser |
| **Projection** | journal prefix → value | yes | the model request, the resume fold, the transcript view, the fingerprint, the compaction plan, selection over candidates |
| **Guard** | journal prefix × proposed act → decision in a lattice | yes | authority, labels, rations, the moderator's ladder |
| **Configuration** | selects which producer, projection and guard implementations run, with which constants | data | the profile |

One scheduler, the driver, decides which producer acts next, consulting
projections to build the act and guards to admit it. Projections and guards
can be queries; producers cannot, because they emit. That line is where
determinism ends: everything on the projection and guard side is a function
of (journal, profile); every nondeterministic thing enters through a
producer's outcome entry. No producer reads another producer's state except
through the journal, which is what removes the coupling the policy
inventory (§2.9) found in the coordinator.

```text
                      S14 profile ──── selects implementations of everything below
                           │
  S17 initiation ──► S2 driver ◄──── S11 recovery (starting state)
                       │   ▲
          builds act ◄─┤   └─ admits act ◄── S8 authority ⊔ S9 labels ⊔ S10 resources
             S3 ◄──────┤
                       ├──► S5 model ── S6 actions ── S7 human ── S4 condensation ── S13 delegation
                       │        │            │             │              │
                       ▼        ▼            ▼             ▼              ▼
                    S1 run journal (intent / outcome / note / section / tools / compact / edit / profile / …)
                       │
                       └──► S3, S11, S12, S15, S16, S18 (projections)
```

The decomposition criterion is Parnas's: a module hides a decision likely
to change. In a harness those are policies — how context is built, when to
compact, how hooks merge — with a half-life of months. Each of why-boxer's
premises commits the design to one move:

| Premise ([why-boxer](../explanation/why-boxer.md)) | What the design takes from it |
| --- | --- |
| P1 dependencies owned | no framework adopted; every mechanism is an existing house shape |
| P2 small languages, boring host | entry kinds, pass properties and composition shapes are a small vocabulary; the harness is Go over it |
| P3 one data spine | the journal is the only history; prompts, tools and profiles are entries; no second trace store |
| P4 its own first workload | profiles and laws are queried with the same SQL surface as everything else |
| P5 mechanical sympathy | append-only entries suit MergeTree; projections are folds over an ordinal range |
| P6 one architect, machine-checked | composition laws and pass properties are tests; violations are rows |
| P7 interfaces by task complexity | the coordinator's tools join the operation catalogue agents already read |

### 2.2 The journal is the only source of model input

ADR-0303 proposes a journal written before each act and folded on resume:
`agentrun` (one row per run) and `agentrun_entry(run, ordinal, kind, key,
body, label)`, append-only, with its own retention. The stronger reading,
which ADR-0303 has not taken and which is the single most consequential
choice on these pages: the coordinator keeps no history of its own, and every
wire request is a pure function of the journal prefix and the profile.

```text
request(run, n) = encode_route( π_profile( J[0 .. n] ) )
```

Entry kinds are a closed enum whose ordinals never retire — the discipline
the vocabulary registries keep with a golden — with an `ignorable` kind for
extension-private entries the projection skips. One writer per run assigns
ordinals; ClickHouse provides no total order, and the coordinator already is
that writer. Kinds, with the page each comes from:

| Kind | Carries | Changes the projection? |
| --- | --- | --- |
| `user`, `request`, `reply`, `intent`, `outcome`, `asked`, `answered`, `note`, `turn` | ADR-0303 §SD2's kinds; `note` gains a source and a form (DeepSeek Harness's one channel for injected context) | yes |
| `section` | a named prompt section set or cleared, with its book and slug (pi's section patches) | yes, rewrites the head |
| `tools` | tools added and removed, by name and schema digest (pi `toolsAdded`/`toolsRemoved`) | yes, on change |
| `compact` | a cut ordinal, a summary, the summariser's call key, the summary's label (pi `compaction`, DeepSeek Harness `replace`) | yes, rewrites from the cut |
| `edit` | a target ordinal and a replacement or nothing; latest edit wins (pi `context_edit`) | yes, rewrites one entry |
| `profile` | profile id and fingerprint at first request and on change (§2.6) | no |
| `candidate`, `verdict`, `select` | a sample of one request; a verifier's result; the chosen sample with its rule — none is a message kind, so none moves the projection (§5.4) | no |
| `completed` | completion of asynchronous work a command started, separate from its `outcome` (§2.4) | no |
| `endorse` | who, the digest of the body as shown, the display epoch, the act classes endorsed, the diff since the last endorsement (§4.6) | no |
| `approval` | an exit above a channel's free tier: the channel, the tier, the approver (person or organisation), the recorded justification (§4.5) | no |
| `head` | a chain head shipped to the anchor store: the ordinal it covers, the digest, the receipt (§4.10) | no |
| `copy`, `issued` | a clipboard write of labelled text with its digest; a watermark payload issued for a frame (§4.8) | no |
| `keyuse` | reserved, unused while encryption is deferred: key id, writer run, reader run, purpose, the entry it feeds (§4.7) | would mark the re-entered body |

Every entry carries a **label** — an integrity level, a TLP tier and two
provenance marks (§4.2) — a **chain digest** over the previous entry's
digest and its own content, computed by the single writer before the
append (§4.10), and a reserved key-id column (§4.7). Entries that arrive by
sync from a peer cell keep their cell-qualified run id and ordinals, so the
one-writer rule holds per (cell, run) and the single writer of this cell
never assigns ordinals in a synced run. The prompt and tool set stop
being coordinator state, which removes the class of defect the inventory
found ("told" watermarks that advance when a note is composed and are not
restored when the turn fails). ADR-0303 §SD6's fingerprint becomes a fold
over `section`, `tools` and `profile` entries. Branching, if ever wanted, is
a parent ordinal on the entry — ADR-0264 §SD2's "branching is a parent id,
not an edit" carried into the journal.

### 2.3 The projection: SQL per call, in a local worker

π is a pipeline of passes over the journal prefix, each with declared
properties the test lane checks:

| Property | Meaning | Check |
| --- | --- | --- |
| `Reads` | entry kinds the pass consults | a pass that reads an undeclared kind fails a corpus test |
| `AppendStable` | appending an entry only appends to the pass's output | `π(J·e)` has `π(J)` as prefix, over a `rapid` generator of journals |
| `Rewrites` | the pass may change earlier output (compaction, edits, the pixel placeholder, a section change) | declared; the profile's count of rewriting passes is its cache cost |
| `Idempotent` | applying twice changes nothing | the existing nanopass `AssertProperties` shape |
| `Shapes` | tool calls and results stay paired, batches whole | a validating pass at the end of π |

CALM predicts which passes can be append-stable: selection, projection, join
and union are monotone; latest-section-wins, latest-edit-wins and the
compaction cut are `argMax` and filters on a maximum, which are not. Those
are exactly the `Rewrites` passes, and the theory and the cache cost agree.
A spike on `clickhouse local` **[V]** (Appendix A, projection) wrote the
whole projection as one statement — latest-per-name sections in declared
order, the latest cut with its summary after the system prompt,
latest-per-target edits, message kinds by ordinal, the label as
`groupBitOr`, one JSON array out — and measured:

| Observation | Result (spike conditions) |
| --- | --- |
| semantics on a hand-built run with a compaction, three edits and a section change | as intended |
| warm query, 400-entry run | p50 15 ms over 20 repetitions in one process |
| warm query, 5 000-entry run (≈3 000 messages, 1.8 MB) | p50 17 ms |
| cold process plus one projection | 90 ms wall |
| determinism | the same prefix projected twice is byte-identical |
| append stability | every ordinary entry extends the previous request; exactly the compaction, the three edits and the section change do not |
| cache cost of a rewrite, as first changed message index | section change 0 (whole prefix lost); compaction 1; edits 8 and 10 |

The last row prices each rewrite in the unit a prompt cache charges, and
explains why pi and DeepSeek Harness append a changed system prompt "in
history" on routes that allow it.

**Where the query runs is the constraint, not how long it takes.** The
server is reached over HTTP one statement per request, with `async_insert`
at the server's default; a per-call server projection makes the server a
per-call dependency, the shape ADR-0300 rejected for admission, and one an
appliance without a server cannot meet. `clickhouse-local` workers
(chlocalpool, ADR-0028) are pre-spawned, single-use, hold nothing, take
Arrow input tables, and are the one engine local by identity for confined
runs (ADR-0145 §SD5). Three placements were weighed; the one taken as a
working assumption is **Q2**: the server holds the journal as the record
(leeway-laid-out); the coordinator, as single writer, holds its run's
entries and ships them as a plain Arrow table to a worker per call; a
plain-shaped view over the journal on the server (the `trailviews`
precedent) lets the same projection SQL serve analysis. Its obligations: a
shape contract between the two layouts; the engine version in the run's
fingerprint, because the projection's bytes depend on it; a golden of
projected requests over a fixed corpus in the default lane; and, from
§4.10, the chain digest computed by the single writer before each append,
so that the worker's input and the server's record commit to the same
prefix.

| | Placement | For | Against |
| --- | --- | --- | --- |
| Q1 | journal on the server; a server query per call | one store, one query | a per-call server dependency; insert settings to pin; no server, no chat |
| Q2 | journal on the server as the record; the single writer ships the run's entries to a `clickhouse-local` worker per call | SQL stays the projection language; local by identity, so confined runs are legal; the ledger's pattern | entries shipped per call (milliseconds at the spiked sizes); two engines whose versions must agree |
| Q3 | SQL as the specification; Go on the hot path; equivalence tested on stored journals | no engine on the hot path | two implementations of one projection |

Two facts of the worker path bear on it **[V]**: the broker writes each
Arrow input table to a private temporary directory and reads it back with
`file()`, so whatever the writer ships — decrypted bodies included (§4.7) —
sits on disk for the call; and `ExecRequest.Settings` is "ignored by the M2
broker", so a setting the projection or an agent query needs enforced has to
travel another way. A sealed or piped input (ADR-0145 retired the pipe route
as unused) and an enforced read-only setting on that engine are
preconditions for confined runs and for agent queries on it. Journal inserts
state `async_insert = 0`, which current servers default on. Who owns the
final request bytes — SQL emitting JSON or Go serialising rows — is open;
ClickHouse and Go escape JSON differently.

What SQL cannot do — encoding a provider-neutral request to a route's wire
format, producers' result shaping, the sequential replay in compaction —
is a small Go `Pass[S]` shape beside nanopass, not a generalisation of it;
whether π's SQL stages are themselves spliced by nanopass passes per profile
is left open (§6).

### 2.4 Acts: effect class is replay safety; tools as operation specs; tool state

ADR-0303 §SD3 settles resume by effect class — a query without an outcome is
not resent, a command is never resent, a consequential one is listed to the
person — which is pi-durable's `replay: safe | unsafe` with the declaration
taken from `app.OperationSpec.Effect`. Two extensions:

- **The coordinator's own tools become operation specs** declared through
  `appops` and `opjson`, so each gets a schema and codec from one reflection
  walk, a class and effect, and the label rules (`Untrusted`, `Consent`) the
  dispatcher already enforces for app operations. The journal's `intent`
  then carries a declared effect class for every call, which the fixed tools
  cannot supply.
- **Write-ahead is a law over data** (L1): joins between the journal and the
  trail's `agentAction` and `llmCall` rows by the coordinator's call key
  (ADR-0277 §SD6), in the integration lane.

Stateful tools strain not the projection (a request stays a function of the
journal) but the agreement between the model's belief and the tool's state,
its survival across a restart, and its isolation under parallel samples.
Reading play and an interpreter-kernel tool in a downstream application
**[V]** gave a contract, each part with a check:

| Declaration | Meaning | Check |
| --- | --- | --- |
| footprint | every resource a command writes, hidden ones included | observed changes ⊆ declared writes, from what the operations engine already records (L9); both tools' understatements would fail it |
| epochs | the tool publishes revisions or generations; each change by another actor is a journaled `note` | a lost kernel is a note, not a number the model must notice |
| rebuild class per resource | `durable`, `refold` (re-apply the latest set-style command), `replay` (re-execute, nondeterminism declared), `volatile` (lost; resume says so) | the resume fold reads it like an effect class; a `refold` rebuild is itself a query |
| fork class | `read-shared`, `per-instance` (from an explicit configuration, never an ambient workingset), `none` | samples fan out only on `read-shared` |
| completion | asynchronous work returns a handle; `completed` is its own entry | "applied, not completed" is a resume state |
| dry-run twin | an effectful operation with a pure core offers a read-only query that runs the core without committing (validation, explanation; not execution of caller-written statements, §5.4) | verifiers use the twin, never the command |

One rule ties this to ensembles: **explore read-only, commit once** — fan
the model out, verify candidates only with read-only acts, execute a command
once, for the selected candidate. A stateful session carries a floating
label: loading confined data into a kernel makes its later results confined
until a reset.

The two tools read, against the contract **[V]**:

| Concern | play | interpreter-kernel tool |
| --- | --- | --- |
| where state lives | declared, revisioned resources (`sql`, `params`, `signals`, panes) plus volatile results | an opaque kernel process; cells in window memory |
| declared versus actual effect | understated: parameter and signal writes trigger live re-runs; opening a bundle runs the buffer | understated: evaluation is `run` with no writes, yet later evaluations read what it changed |
| belief versus state | revisions, read-before-write conflicts, `turn` reports changes | a kernel loss shows only as a changed generation number |
| resume | resources rebuild from the latest set-style command; results recompute and differ | kernel lost; faithful re-run only for seeded, self-contained cells |
| isolation for samples | queries fan out on one window; commands do not (one task per window) | per window; no fork; kernel count bounded by a licence |
| undo; start state | none (`Restorable` unimplemented), set-style commands compensate; the person's last workingset | reset only; always clean |

Gaps in play an adopting ADR would close, each small: effect declarations
understate live re-runs and fetches; `run`'s outcome records queuing, not
the result; an agent-opened window restores the person's last workingset and
the open tool cannot pass a launch configuration; no resource is
`Restorable`; result ids come from a process-wide counter restarting at 1;
several panes' lanes carry no confinement label; the command log is a ring
of 1 024 entries, so `turn` can miss changes; the coordinator sends no
expected revisions, which hides conflicts between samples of one task.

A canvas in a downstream application that compiles model-written WebAssembly
text adds four lessons for program-like arguments: the journal must hold full
arguments (the trail keeps a digest only); replay needs every input, not just
the program (canvas size, theme, wall-clock budget); a pure core exposed only
as a command blocks verification, and the twin must be a query because
commands run on the render goroutine; and any app that stores model-written
text is a re-entry store (§4.2).

Gaps found in play, each small: effect declarations understate live re-runs
and fetches; `run`'s outcome records queuing; an agent-opened window
restores the person's last workingset; no resource is `Restorable`; result
ids come from a counter that restarts; several panes' lanes carry no
confinement label; the command log is a ring that `turn` can outrun; the
coordinator sends no expected revisions.

### 2.5 Guards: one algebra, three carriers; seams with a declared shape

Authority (`Allow ≤ Ask ≤ Deny`, combined by join, delegation by meet;
under the threat model's approval tiers `Ask` splits into *ask the person,
with a recorded justification* below *ask an organisation approver*, which
keeps the chain and the maximum), labels (a join-semilattice, propagated by
join, checked at sinks) and
resource cost (a commutative monoid of usage vectors bounded by rules) are the
same order-theoretic shape, so a guard built on any of them is associative,
commutative, idempotent and monotone — safe to run in any order or in
parallel, and unable to lower another's decision. Authority stays where
ADR-0280 put it, compiled in the dispatcher; a harness-side guard can only
add refusals, the rule ADR-0300 already states for moderators; and the
organisation's exit policy binds the agent over the person (threat model
SD4), so no grant the person gives can lower a guard the deployment
declared. The
policy-composition literature (Bonatti et al., XACML) supplies the operators.

Every other extension point names one composition shape, and the
commutativity column is the reason to classify: the Cordis paper proves its
order-free guarantees only for commuting operations and names an ordered
middleware chain as the case that does not.

| Shape | Go reading | Law | Ordering |
| --- | --- | --- | --- |
| Slot | one interface-typed field | exactly one provider, or a declared default | — |
| Registry | `map[id]T`, duplicate refused | contributions commute | none needed |
| Chain | ordered slice, explicit `next` | not commutative | declared `Order`, never registration order |
| Observers | fan-out, results ignored | commutative; a failing observer is contained | none needed |
| First claim | first non-nil wins | order-sensitive | declared |
| Guard | each returns `Allow ≤ Ask ≤ Deny`; max; error is Deny | ACI, monotone | none needed |
| Fan-out then reduce | N samples of one request, reduced by a rule | a vote is commutative once ties break on the ordinal; rounds are a chain | none for a vote |

The loop's hooks fall into these shapes: pre-step injection and request
configuration are chains; request-error recovery is first claim (retry,
overflow compaction and image offload compete for one failure); tool
admission and turn stopping are guards; post-tool shaping is a chain. A chain
is the shape to justify in the ADR that introduces it.

### 2.6 The profile is data; the registry is built once

A profile names, for one run, the component at each slot, the passes in each
chain, the guards, the prompt book, the model route per purpose (turn, title,
summariser, moderator, verifier), and the constants that are Go literals and
`BOXER_CHAT_*` variables on the compile date. It is registered like the
other static-explicit registries, listed as an introspection table, recorded
as a `profile` entry and on the run row, and its fingerprint joins ADR-0303
§SD6's. An environment variable selects a profile; it does not toggle a knob
(ADR-0009). A constant then becomes a recorded parameter of every run that
used it, so "what did 24 rounds cost against 16" is a `GROUP BY` over
`llm_usage`.

Model access grows from one host-wide client into **routes** with declared
capabilities (context tokens, reasoning echo, images in tool results, late
system handling, streaming) the rest of the tier reads instead of guessing;
the think-tag and late-system quirks become per-route declarations and the
route id lands on every `llmCall` row. A second consolidation sits under the
profile: one **token meter**, anchored on provider-reported usage, replaces
the three estimators behind the UI warning, the moderator's context signal
and ration's admission estimate.

A profile also carries a **label** and names the **cell kind** it is for:
one tuned on AMBER journals cannot run on a route cleared below AMBER
(§5.6), and a profile for the interactive single-person cell is not the
profile of an unattended cell (§4.1).

### 2.7 Subsystems

| # | Subsystem | Role | Owns | On the compile date |
| --- | --- | --- | --- | --- |
| S1 | Run journal | store | ordered, typed, append-only entries per run; ordinals; branch parents | ADR-0303, proposed |
| S2 | Driver | scheduler | turn and step machine: claim input, choose next act, stop, cancel, steer | `runTurn`; the turn machine observes only (ADR-0265 §SD8) |
| S3 | Context projection | projection | journal → provider-neutral request: sections, history with cuts and edits, injection with source and form, tool exposure | inline in `runTurn` and `conversation`; four "told" watermarks |
| S4 | Condensation | producer | decides and writes `compact`; calls a summariser | none |
| S5 | Model access | producer | routes, wire encoding, capabilities, reasoning, error normalisation, retry, fan-out | `openaichat`, `llm` service |
| S6 | Action system | producer | tool catalogue, argument decoding, dispatch, result shaping, concurrency mode | `fixedTools` + `dispatch` chain; app operations via the dispatcher |
| S7 | Human interaction | producer | questions, approvals, widenings, confirmations, steering, durable waits, endorsements | `asker`, dispatcher chrome |
| S8 | Authority | guard | grants, ceilings, consent, modes, the effect ladder | the dispatcher (ADR-0269, ADR-0280) |
| S9 | Information flow | guard | labels (level, TLP tier, marks), sinks derived from the declared cell boundary with clearances, exit approval tiers, endorsement, re-entry marking, relay channels | `labels.go`, two walls on one bit |
| S10 | Resource governance | guard | metering, admission, budgets, pacing, moderation | `ration`, `moderator` (ADR-0300, ADR-0302, proposed) |
| S11 | Recovery | projection | resume fold, unknown outcomes, fingerprint and stranding | ADR-0303 §SD3, §SD6 |
| S12 | Run memory | producer + projection | documents and knowledge that outlive a turn or run | the artefact (ADR-0282); nothing cross-run |
| S13 | Delegation | producer | sub-runs, forks, handoffs | none |
| S14 | Profile and registry | configuration | registry, composition shapes, profiles, fingerprints | env vars and constants |
| S15 | Observation and evaluation | projection | trail, introspection, laws as queries, counterfactual projection, trials; chain heads and their shipment to the anchor store (§4.10) | trail (ADR-0277), introspection (ADR-0094); no chain, no anchor |
| S16 | Presentation | projection | the chat window, step view, analytics | `apps/chat` render code, which also writes coordinator state |
| S17 | Initiation | producer | runs started by schedules or events; out of this cell kind's scope — unattended runs belong to dedicated cells (§4.1) | deferred (ADR-0303 §SD9; ADR-0298) |
| S18 | Verification and selection | producer + projection | verifiers as read-only acts; selection over candidates and verdicts as a query | none |

Boundaries the compile-date code crosses: presentation is a projection and
holds no state the driver depends on; condensation is a producer, applying a
compaction is a projection; recovery acts only through producers; model
access owns provider quirks, consulted by S3 as data; authority stays
compiled in the dispatcher.

### 2.8 Cross-cutting concerns

Each concern is factored by the structure that carries it, so a subsystem
participates by supplying an element rather than implementing the concern.

| # | Concern | Structure | Law to check |
| --- | --- | --- | --- |
| C1 | identity and correlation | keys run ⊃ turn ⊃ step ⊃ call; the coordinator's call key | every outcome names an intent's key |
| C2 | order and time | a total order per run, one writer; `ts` as "when it began" | ordinals contiguous |
| C3 | durability and atomicity | write-ahead intent → act → outcome | no act without an earlier intent |
| C4 | authority | `Allow ≤ Ask ≤ Deny` by join; delegation by meet | no guard lowers another's decision |
| C5 | confidentiality and integrity | integrity level × TLP tier × marks (§4.2); join on read; checks at the sinks derived from the cell boundary; lowering only by `endorse` | noninterference at the sinks the declared boundary names |
| C6 | resource cost | commutative monoid of usage vectors (calls, tokens by kind, bytes, instances, wall time) | batching invariance of every fold |
| C7 | context capacity | a token measure over projected entries, one estimator | projected size ≤ route window − output reserve |
| C8 | prefix stability | prefix order; append-stable passes monotone in it | `π(J·e)` extends `π(J)` |
| C9 | provenance and audit | append-only trace with a chain digest per entry; heads anchored outside the cell; a handler may discharge, never erase | every decision recorded; anchored heads match the record (§4.10) |
| C10 | failure | closed failure kinds; error is Deny; retry as first-claim recovery | an error never widens; unknown stays unknown |
| C11 | concurrency and cancellation | commutativity licenses parallel steps; cancellation is an entry | parallel only for declared-commuting acts |
| C12 | versioning | closed entry-kind enum; ordinals never retired; fingerprints | every kind ever written still projects |
| C13 | variability | a profile as a point in a configuration space; per-route capabilities | every registered profile validates |
| C14 | determinism | projections and guards pure in (journal, profile) | rebuilt request digest equals recorded digest |
| C15 | suspension | a durable wait is a pending `asked` | an `asked` without `answered` survives resume |
| C16 | measurement | metrics as folds over journal and trail; profile id on every row | declared versus observed components agree |
| C17 | placement | which engine and process run each part, all inside the declared cell; a model host is inside the cell only by declaration | a run's projection never leaves the cell; nothing above a route's clearance reaches it |
| C18 | tool state | footprint, epochs, rebuild class, fork class (§2.4) | observed changes ⊆ declared writes |

C4, C5 and C6 are one shape (§2.5). C8 and C14 are what make "SQL is the
projection" checkable on stored journals without a model. C17 makes the
journal's placement a decision, not a detail. The densest rows — identity,
durability, labels, audit, failure — are the ones the journal carries for
everyone once producers write intents and outcomes to it.

Concern × subsystem — ● must carry, ○ must respect; a column is a
subsystem's obligations, a row is where a concern's law is tested:

| | S1 | S2 | S3 | S4 | S5 | S6 | S7 | S8 | S9 | S10 | S11 | S12 | S13 | S14 | S15 | S16 | S17 | S18 |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| C1 identity | ● | ● | ○ | ○ | ● | ● | ● | ○ | ○ | ○ | ● | ○ | ● | ○ | ● | ○ | ● | ● |
| C2 order | ● | ● | ○ | ○ | ○ | ○ | ○ | | | | ● | | ○ | | ○ | ○ | ○ | ○ |
| C3 durability | ● | ● | | ● | ● | ● | ● | ○ | | ○ | ● | ● | ● | | | | ● | ● |
| C4 authority | | ○ | ○ | ○ | ○ | ● | ● | ● | | | ○ | ○ | ● | ○ | | | ● | ○ |
| C5 labels | ○ | ○ | ● | ● | ● | ● | ○ | ○ | ● | | ● | ● | ● | | ○ | ○ | ○ | ● |
| C6 cost | | ○ | ○ | ● | ● | ○ | | | | ● | | | ● | ○ | ● | | ○ | ● |
| C7 capacity | | ○ | ● | ● | ● | ○ | | | | ○ | | ○ | ○ | ○ | ○ | | | ○ |
| C8 prefix | ○ | | ● | ● | ● | ○ | | | | | | ○ | | ○ | ○ | | | ● |
| C9 audit | ● | ○ | | ○ | ● | ● | ● | ● | ● | ● | ○ | ○ | ● | ○ | ● | | ● | ● |
| C10 failure | ○ | ● | ○ | ● | ● | ● | ● | ● | ● | ● | ● | ○ | ● | | ○ | ○ | ● | ● |
| C11 concurrency | ○ | ● | | ○ | ○ | ● | ○ | | | ○ | | ○ | ● | | | ○ | ○ | ● |
| C12 versioning | ● | | ● | ○ | ○ | ○ | | | | | ● | ○ | ○ | ● | ○ | ○ | | ○ |
| C13 variability | | ○ | ● | ● | ● | ● | | | | ○ | ○ | ○ | ○ | ● | ○ | | | ● |
| C14 determinism | ○ | ○ | ● | | | | | ● | ● | ● | ● | ○ | | ● | ● | ○ | | ● |
| C15 suspension | ○ | ● | | | | ○ | ● | ○ | | ○ | ● | | ○ | | | ○ | ○ | |
| C16 measurement | ○ | | ○ | ○ | ● | ○ | ○ | ○ | ○ | ● | | | ○ | ● | ● | ○ | | ● |
| C17 placement | ● | ○ | ● | ○ | ● | ○ | | ○ | ● | ○ | ○ | ● | ○ | ○ | ○ | ○ | ○ | ● |
| C18 tool state | ● | ● | ○ | | | ● | ○ | ○ | ● | ○ | ● | ○ | ● | ○ | ● | ○ | ○ | ● |

### 2.9 Where the tree is, and the order that changes nothing

The inventory **[V]** found the provider client and the metering ledger
already behind seams, and the four things that most directly set what the
model sees per call — the loop, the context, the prompts and the tools — as
constants, string literals and `if`/`switch` chains in `apps/chat`:
`roundLimit` with env clamps; last-round `tool_choice: none`; strictly
sequential tool execution; repeat suppression per turn; retry off; system
prompt sections joined in fixed order and frozen at the first send; four
mid-turn notes with "told" watermarks; no compaction; three context
estimators; tools as inline JSON schema recomputed every round; one
host-wide client; rationing signals (`Remaining`, `RetryAfter`, `Refusal`)
returned and never read. The inventory, by what each policy decides;
"Seam" says whether a replacement point existed:

| Policy | Where | Compile-date behaviour | Seam |
| --- | --- | --- | --- |
| round limit | `coordinator.roundLimit`, `BOXER_CHAT_ROUNDS*` | 24 rounds, clamped 4–96 | env only |
| last round; stop conditions | `runTurn`, `lastRoundNote` | system note and `tool_choice: none`; no tool calls → done; tool calls on the last round → stopped; a model error after tool activity → stopped, before → failure | none |
| tool execution; repeat suppression | `runTurn`; `coordinator.exec`, `inst.refused` | strictly sequential; an identical refused call answered with a fixed refusal, cleared per turn | none |
| retry; model access type | `llm` service construction; `runTurn(ctx, cli *llm.Client, …)` | provider retry off; concrete client type | `openaichat.WithRetry` and `promptbook.CompleterI` exist, unused |
| system prompt; mid-turn notes | `systemPromptOf` and four prompt constants; `ceilingNote`, `artefactNote`, `pixelsNote`, `changesNote` | constants joined in fixed order, frozen at the first send; notes inserted before the newest user message when news, with "told" watermarks | none |
| late system messages | `openaichat.foldLateSystemMessages` | leading system messages merged; later ones folded into the next user turn | provider-level |
| history; compaction | `conversation.landTurn`, `stripPixels` | every message kept; screenshots replaced by a placeholder after the turn; failed turns never land; no compaction | none |
| context pressure | `contextWarnFrac`, `Thresholds.ContextPercent`, `estimateInput` | three estimators; the response is a warning to start over | none |
| call parameters | `llm.Request` | no `MaxTokens`, `Temperature`, `EnableThinking`, `Seed` per conversation; host default `BOXER_LLM_MAXTOKENS` | none |
| tool set; presentation; dispatch | `coordinator.fixedTools` and friends; `BOXER_CHAT_OPERATION_TOOLS`; `coordinator.dispatch` | inline JSON schema recomputed every round; generic `call_operation` or one typed tool per operation; an ordered `if`/prefix/`switch` chain | env only |
| result shaping; images | `wrapUntrusted`, `callOutcome`, per-tool caps; `takeShown`, `pixelsLead` | per tool, inconsistent (`call_operation` uncapped); screenshots as a separate user message after the round | none |
| backend; streaming; caching | `openaichat.ClientI`; `encodeRequest` | one host-wide client from `BOXER_LLM_ENDPOINT`/`MODEL`, `ScriptedClient` headless; replies arrive whole; no cache hints, cached tokens metered | **yes**, host-level |
| metering and admission | `ration.Ledger`, `ration.Rule` | rules are runtime data moderators write | **yes**, data |
| moderator | `moderator.Engine`, `DefaultThresholds`, `Engine.raise` | thresholds compiled in; ladder note → slow → stop | engine separable; external moderators over bus verbs work |
| permission decision; pace | `agent.Service.check`, `authority.go`; `DefaultPace`, `BOXER_AGENT_PACE` | one ordered `switch`; 750 ms between calls unless unpaced | none (authority stays compiled) |

Out-of-process attach points exist in principle **[V]**: a moderator
subscribes to `llm.event.call` and `runtime.agent.action.recorded` and acts
through `llm.ration.*` and `runtime.agent.moderate.*`; a coordinator is a
client of `runtime.agent.*`; a trail consumer uses `trail.ForwarderI`. All
of it runs on `inprocbus`; `natsbus` carries only sysmetrics and has no
server-side authorisation, so exposing the authority verbs over NATS would
publish them unauthenticated. An out-of-process plugin is gated on that bus
work, not on any seam here. `llm.ScriptedClient` makes loop policy testable
without a model but not comparable across policies, because a script
replies by position.

A behaviour-preserving order, each step changing nothing a person sees until
a second implementation is selected: make `runTurn` take
`promptbook.CompleterI` → tool registry, with the coordinator's tools as
operation specs → prompt book with a fingerprint → journal entry kinds for
`section`, `tools` and `profile` with the compile-date prompt as the only
book, and the five reservations of §4.9 → the projection as passes with the
compile-date behaviour as the only implementation, `AppendStable` checked →
laws as queries in the integration lane → the harness profile recorded on
the trail → a second implementation of one seam (a compactor) → route
capabilities, a second moderator policy, fan out. Descoped: parallel tool
execution, bounded by the dispatcher's per-instance busy checks, not the
loop.

### 2.10 One round, end to end

Every append to the server happens before the act it records leaves the
host; that ordering is what lets a resumed run know which acts may have
happened.

1. The driver appends `user` and `note` entries; the writer inserts them on
   the server (write-ahead).
2. The writer ships the run's entries and the profile's parameters to a
   local worker; the worker returns the request, its label, its provenance
   and its digest.
3. The guards admit the model call (label against the route, budget,
   authority); the `request` entry with call key and digest is inserted
   before the call.
4. Model access encodes for the route and calls the provider; the reply and
   usage are appended as a `reply` entry.
5. For each tool call: the guards answer `Allow`, `Ask` or `Deny` on its
   effect class and grant; an `intent` is inserted before dispatch; the
   dispatcher runs the operation; the `outcome` is appended.
6. The next round projects again from the grown journal. The request only
   grows unless an entry declared as rewriting was appended.

The trail rows of ADR-0277 are written ahead in parallel by the existing
recorder; they are the audit and are not read on this path.

| Failure | Behaviour |
| --- | --- |
| append to the record fails | the act is refused; an act the journal does not know of is what the design exists to prevent |
| local worker unavailable | no model call; the turn stops with a stated reason; a respawn is the first remedy |
| host dies mid-round | resume folds the journal: a request without a reply is remade; a query without an outcome is reported as not completed; a command is never resent; a consequential command is listed to the person first; resources are rebuilt by their rebuild class |
| code or profile changed since the run started | fingerprint mismatch: the run is stranded and read-only; continuing is a new conversation |
| ClickHouse version changed | projected bytes may change; the golden catches it in the lane. At resume, comparing the engine version outright would strand every run on each upgrade — the reason ADR-0303 §SD6 records the build id without comparing it — so the run's last request is re-projected and its digest compared (L4): equal continues, different strands |

**Two names collide.** ADR-0191's *run* is a host process; ADR-0303's,
used throughout this page, is a conversation's journal that outlives
processes. ADR-0269's *epoch* is the grant counter; §2.4's epochs are tool
revisions. Laws that join the journal to the trail cross both meanings, so
an ADR adopting this design maps or renames them.

---

## 3 Product statement

What follows describes an intent, not a property of any shipped code.
Proposed wording, in the template boxer's own positioning uses:

> **For** a small team that must run AI agents on its own infrastructure and
> answer for what they did, **boxer's agent harness is** a journal-driven
> harness in which every request a model receives is assembled by a query
> over that journal, **that** makes each request reproducible, each tuning
> choice measurable, each policy replaceable, and each piece of data
> traceable to every exit. **Unlike** harnesses whose context is built inside
> their own code and recorded, at best, as an opaque file, **boxer** keeps the
> journal, the query, the profile and the labels as ordinary data in the
> same tables as everything else, so a person, an automatic check or an
> outside auditor can re-derive what the model was told and check where data
> went.

**What it is.** A harness through which a model drives boxer's apps on a
person's behalf, under a grant the person gave, over data that stays where
the person keeps it. It is boxer's own first workload for its own data spine
(P3, P4): the model's context is a query, the run is a table, the decisions
taken on the model's behalf are rows, and the person can read all three with
the same SQL surface as everything else.

**For whom.** The deployment why-boxer describes, read through the threat
model's cell: an organisation provisions each person one cell — their
hosts, their engine, the model hosts declared inside it — and owns the data
in it; local or trusted-host models sit beside remote ones; appliances may
have no server; downstream applications host the harness as a package. Not
a hosted multi-tenant service, not a multi-agent platform, and not an
unattended runner, which the threat model puts in cells of its own.

**What it is meant to give a person.**

- *A run that outlives its process.* Close the window, lose the host,
  resume; what was applied is known, what is unknown is listed, grants are
  asked again (ADR-0303).
- *A context that can be re-derived.* Anyone with the engine rebuilds any
  request from the journal and compares its digest with the one sent (L4).
- *Acts written down before they happen,* with their effect class, so a
  command is never silently replayed and a consequential one is never
  re-run.
- *Authority that only narrows.* Grants, ceilings, consent, labels and
  budgets combine by maximum; nothing a model reads, nothing a profile
  learns, and nothing a moderator writes can widen what the person allowed.
- *Data that stays in the cell.* Nothing above a route's or a peer's
  clearance leaves by a machine channel; an exit above a channel's free tier
  is refused or held for the approval the deployment declares, by the
  person or by the organisation; the agent has no send outside the cell but
  the model route; an agent reads only relations it may read in full, and is
  told when something was withheld rather than shown an empty answer.
- *Its own output told apart from the person's.* A model's earlier text that
  comes back through storage is recognised at the read, marked in the
  context, and cannot justify a consequential act without the person's
  endorsement of that specific text.
- *Costs and comparisons as queries.* Which profile ran, what each call
  cost, how often compaction broke the cache, which candidate a verifier
  passed — all `GROUP BY` over tables the person already has.
- *A record four readers can trust.* The organisation's security officer,
  a regulator, the person and an incident responder each get what the
  threat model asks for them, from chain heads anchored outside the cell
  and requests that re-derive to their digests; on a host the person
  administers, completeness is not promised and the record says so.

**What it is not.** Not a boundary against hostile in-process code
(ADR-0026 stands). Not a sandbox for model-written code. Not CaMeL's
privileged-planner split; labels are checked at sinks instead, and the
split stays deferred as ADR-0269 §SD12 records. Not a guarantee against
inference from aggregates; differential privacy is deferred until a
question the group floor does not answer is asked. Not a multi-agent
protocol; sub-runs and forks are sketched, not designed. Not runtime code
loading; every component is compiled in and selected by configuration. Not
a framework others build on, and not an autonomous agent platform. It does
not make a model better; it makes what the harness did checkable and its
settings comparable.

**What it costs the person daily.** A grant asked again after a resume. An
endorsement gesture before a consequential act on model-written or
re-entered text. A recorded justification, or an organisation approver, for
an exit above a channel's free tier; a copy handle instead of plaintext
inside boxer for content whose tier requires it; model-written text at
`CLEAR` still copies plainly with a journaled entry. A warm local engine on every model call (milliseconds at
the spiked sizes, a cold start otherwise). Withheld markers in the model's
context where a policy withheld something.

**What it costs the tree.** A closed enum of entry kinds whose ordinals never
retire; one writer per run; two ClickHouse processes whose versions must
agree and are part of the fingerprint; a chain digest per entry and an
anchor store the organisation must run; a cell declaration the deployment
must keep true; two languages for one harness, SQL for the projection and
Go for the wire. The journal aggregates everything an agent saw, but inside
a single-person cell it holds nothing the person may not see, so its
protection at rest is the host's, not the harness's.

**How it fails.** If the journal becomes a crash record beside an in-memory
history, nothing in §2.3–§2.6 and §5 follows. If the cell declaration
omits an edge boxer mediates, every label check on that edge is beside the
point; if a channel bypasses boxer on a host the person administers, no
record exists and the threat model says so. If no anchor store is
provisioned, tamper evidence is a promise with no reader. If
endorsement is asked too often, the person approves by reflex (CaMeL's "user
fatigue" **[V]**) and the integrity axis becomes theatre. If no task set and
no trial exist, every profile is as good as the constant it replaced.

**Costs and risks.**

| Risk | Containment |
| --- | --- |
| new ground; nothing proven at scale | staged delivery; each step changes nothing a person sees until a second profile is selected |
| measuring whether the model does better work stays hard and expensive | exact offline checks where they exist (§5); paired trials with stated conditions elsewhere |
| a database engine takes part in every model call | a warm local worker, not the server (Q2) |
| the record is falsified on a host the person administers | chain digests per entry; heads anchored in an org-held store; detection granularity equals the anchoring cadence |
| stricter data controls change daily work, for example copying model-written SQL out | copy by reference inside boxer; plaintext copy of integrity-only content with a journaled entry |
| watermarks and copy controls do not stop an insider with a phone | the threat model says so: screens and cameras are attributed, never prevented |

**Against the field.** "Not found elsewhere" means not found in the systems
and papers read for §1; it is not a claim of priority.

| Aspect | Why | Evidence |
| --- | --- | --- |
| reproducibility and outside checking | a request is a query anyone re-derives; digests compare (L4) | spike: byte-identical projection |
| provenance and labels | computed per message by the query that builds the context | spike: L8 caught a planted label drop |
| invariants | laws are queries over live and stored journals, not tests alone | spike: L1–L5, L8 |
| measurement | the profile id on every row; cache cost is the first changed message | spike: rewrite costs per kind |
| counterfactual pricing | a stored run replays under another profile through the real projection, no model | spike: four compaction policies priced |
| recovery | resume and a `refold` rebuild are folds | spike: resume fold |
| self-improvement | what changes is data, swapped between runs with nothing to revert | §5.6 |
| compaction | the plan is a query; tool state is recomputed, not remembered | spike: plan view |
| ensembles | candidates and verdicts are entries; selection is a query | spike: three rules |
| data access | scope generated from leeway declarations, enforced by the engine | security spike (a) |

| Aspect | Cost | Containment |
| --- | --- | --- |
| latency and availability | an engine in every model call | a warm local worker |
| engine coupling | bytes depend on the ClickHouse version; `groupArray` order and `argMax` ties are nondeterministic; JSON escaping differs from Go's | version in the fingerprint; a golden; deterministic tie-breaks |
| expressiveness | wire encoding and sequential policies fit SQL poorly; two languages | `Pass[S]`; SQL for the projection only |
| concentration of sensitive data | the journal holds everything the agent saw, full arguments included | inside a single-person cell that is the person's own data; labels bound where it goes; encryption reserved, deferred |
| stored re-entry | summaries, lessons and model-written documents keep any injection they carried | integrity levels, endorsement |
| single writer | ordinals need one writer per run | the coordinator already is one |
| false completeness | tool state is not in the journal | the stateful-tool contract (§2.4) |
| quality | exact offline for cost, not for quality | one-step checks, trials |
| evolution discipline | closed entry kinds, ordinals never retired | the vocabulary-registry golden |

**Follows the field**: a small loop with one scheduler; an append-only log
as the record; compaction that appends, rolling, on the turn's route,
keeping the person's messages; write-ahead intents with replay by effect
class and runs pinned to a version; permissions combined by maximum;
declared extension seams; verify-then-vote ensembles; confidentiality and
integrity labels with endorsement (CaMeL, Fides, LLMbda); profiles as
optimisable data (DSPy, GEPA).

**Lags**: nothing is built; no streaming, provider retry off, quirks in code;
no agent-quality task set and far fewer runs to tune on than large
harnesses; provider-native compaction and context editing unused; no
general sandbox for model-written code and no planner/data separation as in
CaMeL; delegation and forks only sketched.

**Not found elsewhere**: the context as a declarative query whose digest a
third party re-derives on a stock engine; labels and provenance per message
from the same query; laws over production journals; append stability priced
in prompt-cache units; a compaction plan as a query with a label floor, and
the person never summarised, argued from integrity; profiles priced by
replaying stored runs; profiles that carry labels; a self-improvement
boundary by construction; a stateful-tool contract with refold as a query
and a dry-run twin; a non-Truman agent schema generated from data-mapping
declarations with withheld content marked; re-entry detected by journal
metadata, with key use reserved; a security boundary stated as a declared
cell whose exits are the sinks.

---

## 4 Security model

This section follows the
[threat model](./agent-harness-threat-model.md) compiled from the owner's
interview on the compile date; where the four source pages and that page
differ, the threat model wins, and §4.11 records what changed. Its decisions
are the owner's; the readings of them below are the compiler's.

### 4.1 The cell, its principals, its adversaries

A **cell** is a deployment's unit of isolation, with a boundary the
deployment declares — hosts, engines and model hosts inside it, peers
outside. A cell serves **one person**, who may see everything in it; the
**organisation** owns the data, provisions cells, sets policy, holds an
anchor store and may constrain the person at the boundary. Cells exchange
data only by explicit sync. Three consequences shape everything else:
confidentiality *inside* a cell is not a security question; the security
boundary is the cell boundary, and the questions are what crosses it, who
caused the crossing, and whether the record can be trusted; and the agent is
a **channel** — anything it can move out, the person could ask it to move
out. This page covers the interactive single-person cell only; unattended
operation (ADR-0298) belongs to dedicated cells with a profile of their own.

| Principal | Trust |
| --- | --- |
| organisation and administrators | trusted; set policy, approve high-tier exits, hold the anchor store |
| the cell's person | trusted to see everything in the cell; constrained at its boundary; may endorse model-derived content for acts |
| the agent (model plus harness acting for the person) | untrusted output, task-scoped authority; bound by the organisation's policy even when the person instructs otherwise |
| peer cells of the same organisation | trusted for content, with a clearance per peer for what is sent |
| remote model providers | adversary for what they receive; outside the cell |
| trusted model hosts (`BOXER_LLM_TRUSTED_HOSTS`) | inside the cell, by the deployment's declaration |
| local model weights | pinned by digest; not an adversary; output still untrusted text |

| | Adversary | In scope as | Promise |
| --- | --- | --- | --- |
| A1 | careless person | error | exits above tier refused or held for approval; model-derived text keeps its marks |
| A2 | injected agent — the model steered by content it read | exfiltration, unwanted consequential acts, corruption | prevented at the cell boundary and at consequential acts, by guards that do not depend on the model's cooperation |
| A3 | remote model provider | retention; untrusted output | nothing above the route's clearance is sent; exposure and origin recorded |
| A4 | constrained person as insider | exfiltration by machine channels, directly or through the agent | prevented on org-managed hosts; detected on hosts the person administers; screen and camera attributed, never prevented |
| A5 | person tampering with the record on a host they administer | falsified audit | detected by chain heads anchored in an org-held store |

Harms in scope: exfiltration of organisation data across the cell boundary
by any machine channel; unwanted consequential acts by an injected or
mistaken agent; corruption of cell data, including the agent's output later
mistaken for the person's or the system's. Out of scope, recorded: hostile
in-process code (ADR-0026 stands); a compromised operating system on an
org-managed host; administrator insiders; planted weights; resource
exhaustion (governance, ADR-0300 and ADR-0302); data-subject erasure;
prevention against screens and cameras; unattended cells.

| Measure | A1 | A2 | A3 | A4 | A5 |
| --- | --- | --- | --- | --- | --- |
| sinks derived from the declared boundary, with clearances (§4.5) | an exit above tier is refused or held | the enforcement point for exfiltration | nothing above the route's clearance leaves | prevented on org hosts, recorded elsewhere | — |
| exit budgets at the model route (§4.4) | — | bounds what can leave per task | bounds what reaches the route | — | — |
| agent schema, least privilege and non-misleading (§4.3) | — | bounds what the run can read, so what it can leak; refusals are syntactic | — | — | — |
| canaries, fingerprints (§4.5) | — | detects a verbatim leak | detects a leak into a request | — | — |
| integrity levels, endorsement (§4.6) | an approval names a digest, not a feeling | a consequential act on untrusted or re-entered content needs the person's endorsement | foreign output is level 4 with a remote-origin mark | — | — |
| re-entry detection by metadata (§4.7) | — | its own earlier output is marked when it returns | — | — | — |
| clipboard by reference, export policy (§4.8) | plaintext of labelled text needs a recorded act | an app cannot copy plaintext on the model's behalf | — | machine channels closed on org hosts | — |
| watermark (§4.8) | — | an image that re-enters is the host's own | — | screen and camera attributed, cooperatively | — |
| chain heads anchored outside the cell (§4.10) | — | — | — | — | the detection |

### 4.2 Labels: an integrity level, a TLP tier, and two marks

A journal entry's label has an **integrity** component (§4.6's levels 0–4),
a **confidentiality** component in TLP 2.0 terms — `CLEAR < GREEN < AMBER <
AMBER+STRICT < RED`, read from leeway's existing exclusive `tlp` use-aspect
family (`AspectTlpClear` to `AspectTlpRed`) **[V]** — and two **marks**
propagated by join like labels: *exposure*, which entries were sent to which
remote provider, and *origin*, which text, SQL, documents and acts derive
from a remote model's output through compaction, artefacts and sync. Marks
are sets joined by union, so the whole label is still a join-semilattice and
the projection carries it per message by the same query that builds the
request. Which tier the compile-date binary `confined` maps to is a
deployment default, not fixed by boxer. Sinks, peers and routes carry
clearances in the same tiers.

The flow rules are unchanged by the threat model: a run's confidentiality
floats upward as it reads (LIO's current label bounded by a clearance
**[V]**) and its integrity downward (Biba's low-water-mark **[U]**);
lowering integrity is an `endorse` entry (LLMbda's `endorse` weakens
noninterference along the integrity axis and keeps secrecy **[V]**);
nothing lowers confidentiality; a summary's label is at least the join of
what it replaces (L3), a reply's at least its request's (L6), a selection
carries its verdicts' (L8), a profile tuned on AMBER journals cannot run on
a route cleared below AMBER (§5.6). Marks are recorded for audit, shown to
no one and used by no guard by default; a deployment may opt into either
without a change of data shape. One consequence the threat model does not
spell out: marks are monotone and will saturate — after one summary on a
remote route, everything downstream carries *origin* — so their value to an
incident responder is in the per-message provenance the projection already
carries, which names the first remote entry, not in the mark on the run.

On the compile date **[V]**: `queryengine.SensitivityConfined` is checked at
the llm service's sensitivity wall and at `httpegress`; the conversation's
`tainted` flag is displayed and recorded, never enforced;
`app.OperationSpec.Untrusted` delimits content for the model; the trail's
views pass `confined` and `tainted` through as columns. No TLP tier, mark,
endorse or lowering primitive exists on any label.

### 4.3 Access: a generated agent schema, for least privilege and for not misleading the model

With confidentiality inside the cell settled by the cell's definition, the
agent schema is **no longer access control against the person**. It stays
for two reasons the threat model keeps: least privilege against A2 — what an
injected agent cannot read, it cannot move to a remote route — and
non-Truman behaviour, so that the model is never misled by an empty answer.
The mechanism is unchanged by the threat model, and spikes (a) and (b)
stand:

- A ClickHouse `ROW POLICY` is Stonebraker and Wong's query modification
  **[V]**, a *Truman* mechanism in Rizvi et al.'s terms **[U]**: the filtered
  query returns fewer rows and a model cannot tell "no such row" from "a row
  withheld"; leeway's positivity makes absence meaningful, which makes this
  worse than in a plain table **[V]**.
- Agents query only declared, generated relations (`my_run`,
  `my_conversation`, content-free `agent_history(scope)`, aggregates); base
  tables and `system.*` are not nameable from the agent's principal, so a
  refusal names a relation, never a row. `my_conversation` no longer needs a
  body-visibility rule: inside the cell every body is the person's to see,
  and the agent's reading of it is bounded by budget (§4.4), not by label.
- Spike (a) **[V]**: the scope is a settings-profile constraint the query
  cannot override; the view is `SQL SECURITY DEFINER`; a read of the base
  table fails `ACCESS_DENIED`; the agent's view and the definer's filtered
  base agree on count and body hash (L18). A per-user row policy is bypassed
  inside a definer view and a `RESTRICTIVE` policy `TO ALL` keyed on the
  setting is not, so the latter is the backstop; a parameterised definer
  view answered zero rows for an out-of-scope run with no refusal, so scope
  is never a caller parameter.
- Policy is stated on the leeway model and compiled per layout: an entity
  predicate on axis R; sections passed or dropped; memberships withheld by
  registry id on axis I, applied to every co-lane and both ragged children;
  value aspects from the physical name (`secret` → withheld,
  `application-level-encryption` → opaque, `pseudonymized` → passed). What a
  deployment withholds from the model inside its own cell is hygiene —
  credentials, RED data with no business in a model context — not a
  confidentiality boundary.
- Spike (b) **[V]**: a hand-written packed view with a keep-mask per
  section; L11–L13 returned no rows. **Withheld, not absent**: the masked
  instance reappears in a value-less `withheld` section with its original
  memberships plus two markers, so the reader tells "withheld" from "never
  recorded".

The schema is expressible as generated views through `trailviews` and
`clickhouse.ComposeCreateView` **[V]** once the `withheld` section enters
the view IR, from a four-rule declaration with no value predicates:

```text
relation  <name>  OVER <table|view>  SCOPE <setting> = <backbone column>
  section   <s>   { PASS | DROP }
  withhold  <s>   MEMBERSHIP IN (<registry keys…>)
  aspect    secret → WITHHOLD ; application-level-encryption → OPAQUE ; pseudonymized → PASS
```

### 4.4 Budgets as exit bounds

Budgets bound *how much* an injected agent can move through the one send it
is allowed, the model route. The exit budget is **bytes sent to a remote
route per task**, metered where the request leaves; a read budget in bytes
on every read path, and instances (`arraySum(<channel>card)` per section) on
the agent schema, stays as least-privilege hygiene. ADR-0269 §SD6 lists
rows, bytes, launches, captures and wall time; on the compile date the task
struct has call, launch and deadline fields only **[V]**, and a leeway row
bounds nothing. All three quantities fit ADR-0300's ledger shape with the
refusal naming the rule (L19).

Aggregate relations with small-group suppression, and the tracker argument
behind them, lose their force inside a single-person cell: there is no
one inside it to hide a group from. They return at the cell boundary, where
a peer with a lower clearance receives aggregates by sync; that is the
person's act, and Denning, Denning and Schwartz's result that "almost all
databases have a general tracker" **[V]** then applies to what the person
chooses to release, not to the agent. Differential privacy stays deferred.

### 4.5 Sinks derived from the boundary; exits; canaries

The threat model turns the earlier hand-listed sink registry into a
**derived** one: an edge that leaves the declared cell is a sink, every sink
carries a clearance in TLP terms, and the proposed home for the declaration
is the appliance topology of ADR-0126 (its first fragment in the tree is
`BOXER_LLM_TRUSTED_HOSTS`). That answers the objection that nothing checks a
hand-listed registry's completeness for the channels boxer mediates; it does not answer it for channels that bypass boxer, which the
threat model says plainly cannot be recorded on a host the person
administers.

**Agents are read-only outside the cell** (threat model SD5), with one send:
the remote model call, through the model-route sink under the route's
clearance. The outbound half of a fetch (URL, headers, body) can carry data
and is the HTTP egress service's to police (ADR-0262 §SD4), but the
service enforces a label the caller declares and no consumer forwards one
on the compile date **[V]**; the harness must therefore **declare the run's
current label on every agent-caused fetch**, and canaries run on the
request body. Sync, publishing to peers and export are the person's acts,
possibly prepared by an agent.

| Channel across the boundary | Org-managed host | Host the person administers |
| --- | --- | --- |
| clipboard out of boxer, file export, printing | prevented above the tier policy; copy by reference inside boxer | detected and recorded |
| sync to a peer | data above the peer's clearance does not leave | detected |
| remote model route | data above the route's clearance is never sent | detected |
| outbound half of a fetch | the egress service, with the harness's declared label | the same, if the host routes through it |
| screen, camera | attributed by watermark | attributed |

The approval path for an exit above a channel's free tier is **policy data
per deployment**: boxer ships a default mapping from tiers to *free*,
*person with a recorded justification*, *organisation approver* and
*never*, and enforces what the deployment declares. The organisation binds
the agent over the person (SD4): encoding data into SQL text, a document, a
file or a fetch is checked as the exit it is, whoever asked.

Two sinks of the earlier list are recast. `publish_result` and
`publish_projection` publish *inside* the cell (ad-hoc datasets another
window reads), so they are consequential acts under harm 2 and need the
endorsement rule of §4.6, not a boundary clearance; a publish to a peer is
sync and the person's act. SQL text to a ClickHouse endpoint is an exit
only when the endpoint is outside the declared cell, in which case the
topology refuses the destination before any text is composed; inside the
cell it is a read bounded by §4.3 and §4.4.

What checks a sink on the compile date **[V]**: the model route and HTTP egress check one bit; publish checks
effect, consent and ceiling; persisted arguments are not checked on the
write path (ADR-0264's Status records the facts server locality gap);
pixels route through the wall; the clipboard broker audits app id, bytes
and result. Widening the two walls from one bit to a tier is a change
inside one function each; the rest is new code.

Spike (c) **[V]**: an exact canary (`position(payload, token)` over every
outbound payload joined with `canary(scope, token)`) returned the verbatim
post and the remote request and nothing else (L10); four-word shingles of
the protected body found the verbatim post with no inserted token;
`ngramSimHash` distances ordered the payloads sensibly (5, 12, 9, 15 on
sentence-length texts) and `ngramMinHashArg` caps at 25 hashes.
Fingerprints flag on the trail and never gate. Leaning: shingles first.

### 4.6 Integrity levels, endorsement, mixed documents, re-entry

| Level | Source |
| --- | --- |
| 0 | the person; harness text |
| 1 | deterministic system data; same-organisation peer content not otherwise marked |
| 2 | external content: fetched, files, captures |
| 3 | this run's model output returning through storage |
| 4 | another run's model output |

Remote origin is a mark on levels 3–4, not a lower level. **No
consequential act whose justification is at level 3–4 proceeds without the
person's endorsement, by digest and act class** (L15). Marks on synced
entries survive sync: a peer's model output arrives as model output. Fides
reaches the same place by constrained output types; CaMeL confirms rather
than blocks and warns of fatigue **[V]** — which is why the approval tiers
of §4.5 and the endorsement here must stay rare, or A1 approves by reflex.

**A person's review does not lift an artefact to level 0 by itself.** It is
an `endorse` entry naming who (as `AgentGrant.DecidedBy` records a grant),
the digest as shown, the display epoch it was shown under (§4.8), the act
classes, and the diff since the last endorsement or a flag that none
exists; written by a deliberate gesture, never by a pane closing. A later
agent edit changes the digest and voids it; a later person edit does not.

**A mixed document's label** — the chat artefact (ADR-0282), a play
resource — is the join over its revisions' writers: integrity the lowest
level among writers whose text survives, confidentiality the join of what
each writer had read, marks the union. The writer of each revision is known
(`opwire.CallRequest.Writer`; `opengine.Engine.Writer` in memory;
`AgentAction`'s `Delegation` durably) and no label is derived from it
**[V]**. Practical cut: per-revision labels on the resource's command log,
folded by the same projection; a paste of unknown origin enters at level 2.

**Re-entered content is marked in the projection**, not only labelled:
`<recalled source="self" run="…" ord="…">` around the body, for the reason
ADR-0269 §SD7 delimits untrusted content. The pass declares `Reads` of the
writer metadata (§4.7) and L17 checks it. A verdict from a foreign route is
level 4 with a remote-origin mark, and the selection carries both (L8).

### 4.7 Re-entry detection by metadata; encryption deferred

The earlier security draft proposed encrypting every agent-written body
under the writer run's key, with every decryption a journaled `keyuse` entry, so that
a copy that lost its writer column could not lose its key. The threat model
defers it: with data-subject erasure out of scope, per-run keys would serve
only re-entry detection, and inside a single-person cell the journal holds
nothing the person may not see, so encryption at rest is the host's
business. **Re-entry is detected by journal metadata where it survives**:
the writer run, turn and ordinal stay plaintext beside every body; the
reader's level is 3 when the writer run is the reader's and 4 otherwise
(L16, restated); the key-id column stays reserved so the design can be taken
up without a schema change.

What the deferral gives up, stated plainly: a body copied by a channel that
drops its metadata — an `INSERT … SELECT` the agent writes into another
table, a plaintext paste — re-enters at level 2 as external content rather
than at level 3 or 4. Inside boxer that channel is narrow, because the
clipboard handle (§4.8) restores labels on paste and an agent's writes are
journaled with their writer; outside boxer it is an exit, prevented or
recorded under §4.5. The residual is a person on a host they administer
pasting plaintext back, and A4 is detected, not prevented, there anyway.

Two measurements remain on record for whoever takes encryption up
**[V]**: on 26.9.13.15, a literal key in `encrypt()` /
`decrypt()` is `[HIDDEN]` in `query_log.query` and in exception text, the
same literal in any other function is logged verbatim, a `{k:String}`
parameter does not reach `query`, and the chlocal path's `SET param_`
prelude (ADR-0133) is query text; so a key would never enter a statement
and decryption would happen in the host before the Arrow table is shipped
to the Q2 worker. The precedents stand: `sealed`'s key as a private field
under `O_TMPFILE` (ADR-0240 §SD1), ADR-0145's refusal to put key material on
the `file()`/`url()` wire. ADR-0025's vault is a different design for a
different obligation and is not what was deferred here.

### 4.8 Relay channels: clipboard and screen

**Clipboard and export** are needed against A4 on org-managed hosts and are
recorded only on hosts the person administers. The findings of spike (e)
**[V]** stand: a broker copy and a native selection copy both reach the host
as one `egui::OutputCommand::CopyText(String)`; the headless host forwards
it, the browser host sends a one-field `ClipboardData{text}` frame, the
desktop host hands it to egui-winit outside this tree; the broker's audit
carries app id, bytes and result and no label or source; per-widget
selection can be disabled for `label`, `labelAtoms`, `codeView` and via
`textEdit.interactive(false)` under egui 0.35 but not for `labelWidgetText`,
markdown bodies, Rust-side text edits or Ctrl+C in an editable field; egui
0.35's `Plugin::output_hook` would see every `CopyText` on all three hosts
and is unused; two callers bypass the broker and no lint prevents it.

The design: labelled text renders non-selectable with a broker-routed copy
action; the broker payload grows a label and a source; a host plugin on
`output_hook` drops non-broker `CopyText` under a policy opcode — the one
Rust change, and the only choke point the desktop host can have.
Model-touched content copies **by reference** inside boxer
(`boxer:clip/<id>`, resolving only inside with its labels, inert outside);
a paste of a handle restores labels; an unrecognised paste enters at level
2. Plaintext out of boxer above the channel's free tier follows the
deployment's approval path (§4.5). **Daily cost:** agent SQL pasted into
another boxer window costs nothing; agent SQL copied out to another program
costs a recorded justification only when the content's tier requires one.
Integrity-only labels (level 3 text at `CLEAR`) copy plaintext with a
journaled `copy` entry and no gesture (L20).

**Screen and camera** are attributed, never prevented. Every frame or image
leaving the host carries the `watermark` crate's 8-byte payload (ADR-0241)
as a session handle plus a display epoch, issuance journaled (`issued`,
L21). The threat model now relies on this for the live screen, and
ADR-0241 leaves live-stream integration unqualified; that is open question
3 of the threat model and item 24 of §6. Spike (d) **[V]**, over real scene
captures at the unscaled sizes the pixels feature sends:

| Input | First tile | Whole frame | Complete / usable tiles |
| --- | --- | --- | --- |
| four captures, 1400×900, 960×720, 1000×700 | BER 0, recovered | recovered | 24/24, 12/12 |
| 464×432 crop, tile-aligned | recovered | recovered | 4/4 |
| 464×432 crop at an arbitrary offset | BER 0.476, not recovered | recovered, phase located | 1/1 |
| 700×600 and 400×300 crops at arbitrary offsets | not recovered | recovered | 4/4, 1/1 |
| JPEG quality 85 re-save | BER 0, recovered | recovered | 24/24 |
| 80 % and 50 % Lanczos rescale | not recovered | **not recovered** | 12/0, 2/0 |
| 160-pixel thumbnail | too small for a tile | not recovered | 0/0 |
| the unmarked capture | BER 0.476 | not recovered | 20/0 |

Mean absolute luminance change 4.4–6.1 with the default contrast, maximum
near full scale. A crop of at least a tile is recovered; a rescale, which
any browser tab, chat client or phone does, fails outright rather than
degrading. For A4's analog channel this means the watermark attributes a
photograph only when the photograph preserves the tile geometry, which a
phone camera does not; "attributed by watermark" in the threat model's exit
table should be read with that qualification, and ADR-0241's statement that
the mark is not evidence of authorship stands **[V]**.

### 4.9 Five reservations the core needs now

The harness core depends on the security layer at five narrow points. Each
is a data shape or a choke point, cheap before the journal ships and a
migration after it. The list is the superseded consolidation's, revised
against the threat model.

| Reserve now | Why it cannot wait |
| --- | --- |
| a label on every journal entry — integrity level, TLP tier, exposure and origin marks — with `endorse` as the only lowering entry | every projection, guard and law computes over it; adding a component later migrates every stored journal; the marks cost nothing while no guard reads them |
| sinks derived from the declared cell boundary, one choke point per channel boxer mediates (model route, egress, persisted arguments, clipboard, images), with clearances and an `approval` entry | a producer that can emit around it turns data-loss prevention into a hunt; the threat model's exit table is only as complete as this list |
| agent reads only through declared relations, deny by default | the compile-date endpoint-wide grant would otherwise become a contract that tools and prompts depend on |
| a chain digest per entry and a `head` entry, even before an anchor store exists | tamper evidence over a prefix cannot be added to rows written without it |
| projection markers: `withheld`, `recalled`, visible edits; a reserved key-id column | the markers change request bytes, the prompt cache and L4; the column makes encryption a setting rather than a format change if it is taken up |

Everything else in this section is policy that plugs into seams the harness
already has.

### 4.10 Tamper evidence: chain heads anchored outside the cell

New against A5. Records must convince four readers — the organisation's
security officer, an external regulator checking without trusting the person
or the software, the person contesting what was done for them, and an
incident responder tracing every act and exit that depended on poisoned
content. The mechanism: **chain heads** over the journal and the trail
shipped to an org-held anchor store outside any person's control; the
regulator's check is that the anchored heads match the cell's record and
that every recorded request re-derives to its digest (L4).

What that asks of the architecture, which the earlier drafts did not have:

- **Each journal entry carries a chain digest** over the previous entry's
  digest and its own content, computed by the single writer before the
  append, so a head commits to the whole prefix and not to a row count a
  table engine cannot defend (L31). The trail needs the same per row or per
  batch.
- **A `head` entry** records each shipped head: the ordinal it covers, the
  digest, the anchor receipt. The anchoring cadence (per turn, per run, per
  sync) is open; the detection granularity against A5 equals that cadence,
  since a person who administers the host can rewrite anything between two
  anchors.
- **The profile, the pass SQL and the engine version** must be inside what
  the heads cover, or the regulator cannot re-derive a request without
  trusting the software; ADR-0303 §SD6's fingerprint is the place.
- **Deletion conflicts with the chain.** ADR-0303 §SD8 lets the person
  delete a run and "deletion removes the entries"; a deleted run breaks the
  chain the anchor store holds. Either deletion becomes a journaled
  tombstone that keeps the digests and drops the bodies, or the threat model
  accepts that a deleted run is a recorded gap. This is a decision for the
  ADR and is listed in §6.
- **Completeness is not promised** on a host the person administers: an
  exit that bypassed boxer leaves no record, and the threat model says so.

### 4.11 What the threat model changed in the earlier drafts

| Earlier element | Change |
| --- | --- |
| threat model with A1–A4 | A5 added; principals and harms stated for a single-person cell; the organisation binds the agent over the person |
| non-Truman agent schema | kept for least privilege against A2 and for not misleading the model; no longer access control against the person; `my_conversation`'s body rule dropped |
| labels as (integrity, confined) | confidentiality is TLP from leeway's aspects; exposure and origin become marks; same-org peer content enters at level 1 with carried marks |
| hand-listed sink registry | derived from the declared cell boundary, proposed home ADR-0126; agents have no send but the model route; fetch requests stay with the egress service, which the harness must now give a label |
| publish as a boundary sink | an in-cell consequential act under the endorsement rule |
| budgets as read bounds | exit budget at the model route first; read budgets as hygiene; aggregation and trackers move to the sync boundary |
| per-run encryption and key service | deferred; key-id column reserved; re-entry by metadata; what is lost stated |
| clipboard and watermark | prevention on org-managed hosts, recording on administered ones; the watermark is attribution and its live path is now relied on |
| unattended mode (ADR-0298) | moved to dedicated cells with their own profile; S17 leaves this cell's scope |
| no tamper evidence | chain digests per entry, `head` entries, an anchor store, the deletion conflict |
| five reservations | kept; the label gains the tier and the marks, the sink choke point is derived from the boundary, the chain digest joins the list, encryption becomes a reserved column |

## 5 Quality control model

Quality here means three things, each with its own check: the harness does
what its laws say (§5.2–§5.3); the model's output is selected and condensed
in ways whose cost is exact and whose quality is bounded (§5.4–§5.5); and
what changes between runs changes under gates (§5.6).

### 5.1 The layers

Quality is controlled at several layers, cheapest and most exact first. Each
layer catches what the ones above cannot.

| Layer | What it checks | When | Model calls |
| --- | --- | --- | --- |
| 1 Pass properties | `AppendStable`, `Idempotent`, `Shapes`, `Reads` over generated journals (`rapid`) | default test lane | none |
| 2 Goldens | projected requests over a fixed journal corpus, per engine version; the entry-kind enum | default lane | none |
| 3 Conformance | tool footprint against the engine's observation; profile validity (thrash check, guards ⊇ parent); every registered profile validates | default lane | none |
| 4 Laws | queries returning violating rows (§5.2) over a corpus and over production journals; cheap ones served as introspection tables | integration lane, on resume, continuously | none |
| 5 Counterfactual replay | another profile over stored journals: request sizes, prefix breaks, overflow, laws, cost | on demand, at admission | none |
| 6 Verification at run time | verifiers over candidates (§5.4); selection by rule | every step of a verified purpose | N samples |
| 7 One-step checks | at each point where two profiles' requests differ, compare the next action or ask a verifier | at admission | bounded |
| 8 Paired trials | same task set and route, two profiles, outcomes and `llm_usage` per profile ([doc/trials](../trials/)) | before a profile becomes a default | many |
| 9 Monitoring | the declared profile against the passes, guards and tools the journal shows; metrics per profile over time | continuously | none |

Layers 1–5 and 9 are exact and need no model, because the journal is the
only input and the profile is data. Selection (layer 6) is the one part of
the harness evaluable exactly offline as well: candidates and verdicts are
recorded, so another rule or verifier replays against stored runs. Layers 7
and 8 are where quality — not cost or correctness of the harness — is
measured, and they are expensive and noisy.

### 5.2 Laws as queries

Each law is a SQL assertion over the journal and `boxer.facts` that returns
the rows that break it, kept in the integration lane and, where cheap,
served as an introspection table of violations. One catalogue; L10–L21
keep the numbering of the security spikes. *Ran* means a spike returned the planted
violations and nothing else.

| Law | Statement | Status |
| --- | --- | --- |
| L1 write-ahead | every outcome follows an intent with its call key; every reply its request | ran |
| L2 tool pairing | in a projected request, every tool result answers a call made earlier in it | ran; caught a planted compaction |
| L3 compaction labels | a summary's label is at least the join of what it replaces | ran |
| L4 reproducibility | each recorded request digest equals the digest of the projection rebuilt | ran |
| L5 append stability | an entry not declared as rewriting only extends the request | ran |
| L6 reply labels | a reply's label is at least its request's | ran; found a gap in the demo journal (replies carried no label) |
| L7 required context | policy notes the profile requires (the ceiling) appear in every request | proposed |
| L8 selection labels | a selection and its reply carry the labels of the verdicts that decided it | ran; caught a planted drop |
| L9 tool footprint | resource changes a command causes are among its declared writes | proposed |
| L10 canary | no outbound payload contains a live canary its sink's clearance excludes | ran |
| L11 co-alignment | in every agent relation, co-lanes align and ragged lengths sum | ran |
| L12 withheld memberships | no withheld membership survives on a valued section | ran |
| L13 conservation | instances in the base equal kept plus withheld | ran |
| L14 sink clearance | every row the trail records leaving the cell carries a TLP tier at or below the clearance of the sink the declared boundary names | proposed |
| L15 endorsement before consequence | every consequential outcome at integrity 3–4 has an earlier `endorse` naming its body's digest | proposed |
| L16 re-entry level | every projected body written by a model run carries level 3 when its writer run is the reader's and 4 otherwise, from the writer metadata; if encryption is taken up, from the `keyuse` entry | proposed |
| L17 re-entry marking | every projected message whose provenance includes a model-written body carries the recalled marker naming the writer run and ordinal | proposed |
| L18 backstop never fires | through every scoped relation, the agent's count and body hash equal the definer's under the same scope | ran (one relation) |
| L19 budget | metered bytes and instances never exceed the rule in force | proposed |
| L20 clipboard provenance | labelled text reaches the clipboard as a handle or after an endorsement; every forwarded copy has a broker request | proposed |
| L21 issued frames | every image leaving the host decodes to an earlier issuance entry | proposed |
| L22 compaction coverage | each `compact` entry's `from` equals the previous cut | proposed |
| L23 compaction progress | cuts strictly increase | proposed |
| L24 compaction bound | the request after a compaction fits under the trigger; a profile's prompt + summary + tail + margin below the trigger (thrash) | ran (bound and thrash) |
| L25 the person verbatim | every user message is in the request verbatim or marked as summarised | proposed |
| L26 consent | no consequential command ran without a consent entry | proposed |
| L27 lowering only | no moderator rule and no harness guard raised a limit | proposed |
| L28 declared versus observed | a run's declared profile agrees with the passes, guards and tools its journal shows (ADR-0126's `GROUP BY`) | proposed |
| L29 profile authority | every profile version's guards contain its parent's; no profile carries an authority field | proposed |
| L30 placement | a run's projection never ran on an engine outside the declared cell | proposed |
| L31 chain continuity | every entry's chain digest equals the digest of the previous entry's digest and its own content; the first entry of a run chains to the run row | proposed |
| L32 anchored heads | every `head` entry's digest equals the chain digest at the ordinal it covers, and the anchor store holds the same digest for it | proposed |
| L33 exits approved | every outcome of an act that crossed the cell boundary above its channel's free tier has an earlier `approval` at the tier the deployment's policy names | proposed |

### 5.3 Conformance of the harness itself

- **Pass properties** (§2.3) are tests over a `rapid` generator of
  journals; `AppendStable` turns prose about cache effects into a property
  that fails. `Requires`/`Produces`, which nanopass declares and nothing
  reads (spatiotemporal lessons L8), are either checked at registration or
  not declared.
- **A golden of projected requests** over a fixed journal corpus in the
  default lane, because the bytes depend on the engine version; `groupArray`
  order and `argMax` ties need deterministic tie-breaks; JSON escaping
  differs from Go's.
- **A resume-conformance step** in the lane ADR-0303 plans, running L1 and
  the write-ahead joins.
- **The stateful-tool footprint check** (§2.4) as a conformance test over
  the operations engine; both tools read would fail it on the compile date.
- **Metamorphic relations** where no oracle exists: adding an irrelevant
  note must not change a guard's decision.
- **A single decision table** for the dispatcher's `check` and the
  `Classify*` functions `surface.go` restates for ADR-0283 analytics, which
  can drift.

### 5.4 Verification and selection

Boxer's surfaces are machine-readable, which makes exact verifiers unusually
available: nanopass parse and canonicalisation plus read-only classification
and a row-limited execution in a worker for generated SQL; `opjson`
validation for operation arguments; a dry run of the guards for authority;
`kindcheck` for leeway kinds. One constraint from the operation contract:
under ADR-0269 §SD1 "a statement the caller writes is never a query", so
executing a candidate's SQL — even read-only, in a worker holding no app
state — is a command, not a dry-run twin. Validation and explanation are
queries; sampled execution needs either a declared class of its own or the
`run` effect (see the ADR impact page). Confinement adds a second reason:
confined runs may only use local routes, which are usually the weaker
models, and fan-out with verification is how such a run recovers capability
without its data leaving.

Verifiers that execute model-written code need a sandbox. The tree holds a
pooled, compile-once WebAssembly runtime for a trusted first-party guest; a
downstream application runs model-written WebAssembly in a fresh interpreter
per call whose capabilities are exactly its imports. A small shared package
for the second pattern, with instruction-bounded rather than wall-clock
limits, is an open decision (§6).

- **Fan-out in S5**: one projected request, N samples, one or several
  routes; all share the prefix, so a caching server prefills once.
- **Three entries that do not move the projection**: `candidate`,
  `verdict` (the verifier's run journaled as a read-only act), `select`; in
  the spike the request digest stayed identical while five candidates and
  nine verdicts were appended **[V]**.
- **Selection is a query**: a vote, a verifier filter, or a
  reliability-weighted sum over candidate and verdict entries, with
  reliability itself a query over earlier verified outcomes — weak
  supervision in SQL. Three selection rules agreed in the spike.
- **Guards apply per sample**: rations count the N calls under their
  purpose; the label guard checks each sample's route, so a confined run
  fans out to local routes only.
- **L8**: selecting because of a verdict is an implicit flow.
- **Exact offline evaluation**: another vote rule or verifier replays
  against stored runs with no model calls; Coverage@k is a query (a
  synthetic corpus showed the shape — coverage rising from 0.35 at k=1 to
  0.82 at k=16 while majority-correct stayed near 0.3 — not evidence).

Design rules: N is a profile parameter per purpose set by measurement, with
early stop on agreement; prefer sampling the best route with diverse
verifiers over mixing weaker generators; vote and verify first, debate and
layered mixtures only where a trial shows a gain; **no selection signal, no
ensemble**.

### 5.5 Compaction: plan, act, apply; a ladder; what can be judged

Compaction splits along the factoring rule, and only the middle part calls a
model: the **plan** is a view over the prefix returning a verdict, the cut
ordinal, the span replaced, the least label the summary may carry and the
person's messages inside the span, sizing the request by running the
projection itself; the **act** writes an intent, calls the summariser and
appends `compact`; **apply** is the existing pass. The ladder, cheapest
first: **derive** a state digest (open instances, revisions, grants, withheld
markers, open questions) folded from the journal with no model; **elide** a
tool result before the cut to a stub naming its key, size and digest, with a
read-only `recall` act to bring it back (tool results were about two thirds
of the spike journal's tokens); **summarise** rolling, previous summary plus
the span; **overflow** re-plans with a tighter budget under one owner, so
retry, compaction and image offload do not compete.

Rules that follow from earlier decisions: rewrites happen only at an entry
(L5 forbids a sliding window); a cut sits on a boundary, never inside a
call–result span (L2), a turn start preferred; **the person is not
summarised** — their messages stay verbatim up to a pin budget, because
summarising them moves integrity from level 0 to the model's own; a summary
is the model's output re-entering, rendered as model-written, with
confidentiality at least the join of what it replaces and integrity the
least of its inputs, and a consequential act justified only by a summary
needs an endorsement; the summariser call is a sink and label-checked; the
summariser request extends the turn request on the turn's route so it reads
the cached prefix. The token count starts from the provider's reported
prompt tokens for the last request plus an estimate for newer entries.

The compaction spike **[V]** replayed a synthetic 40-turn run through the
real projection under four policies, with a placeholder summary and a
64 000-token window (Appendix A):

| Trigger / kept tail | Compactions | Prefix breaks | Over budget | L2 orphans | Cost, summariser on the turn route | Cost, separate route |
| --- | --- | --- | --- | --- | --- | --- |
| none | 0 | 0 | 117 of 186 | 0 | the run fails at the first | — |
| 90 % / 8 000 | 3 | 3 | 0 | 0 | 0.87 M | 1.01 M |
| 75 % / 16 000 | 6 | 6 | 0 | 0 | 0.94 M | 1.17 M |
| 50 % / 24 000 | 35 | 35 | 0 | 0 | 1.59 M | 2.5 M |

The plan is a deterministic query (about a quarter of a second per call,
process start included); the last row thrashes, which is the profile check;
the cheapest policy keeps the least verbatim text and its cost in quality is
invisible here. A summariser on the turn's route was about five times
cheaper per summary than a separate route (the whole-run costs in the table
are closer, summariser calls being a minority of run spend). Summary quality
was not measured. Quality has three handles and no exact answer: **one step
at each compaction point** (the first request after a cut is the first that
differs; compare the next action compacted and uncompacted, or let a
verifier judge; later steps diverge and need trials); the **recall rate**
after elision as a journal query, a direct sign of over-eager elision; and
a **sketch baseline** of the same size for declared query classes, as
Context Compaction Theory grades a compactor against a Bloom filter.

### 5.6 Evaluation ladder, gates, and bounded self-improvement

With the journal as input and the profile as data, comparison falls out at
rising cost:

| Tier | Check | Cost |
| --- | --- | --- |
| T0 | static: parse and canonicalise with nanopass, read-only classification, the journal view as the only relation, no nondeterministic functions; profile validity (thrash); guards ⊇ the parent's | milliseconds |
| T1 | laws over a corpus of stored journals | seconds |
| T2 | counterfactual replay through the real projection: request sizes, prefix breaks, overflow, cost, laws — no model | seconds to minutes |
| T3 | one step at each point where the candidate's request differs | bounded model calls |
| T4 | paired trials on a held-out task set, in the [doc/trials](../trials/) sense: same task set, same route, two profiles, `llm_usage` and outcomes per profile | expensive |

What may change: **only profiles, and only between runs.** A profile version
is a row with a fingerprint, a parent, a status (candidate, admitted,
evaluated, approved, retired), its evidence and a label; a pointer per
purpose names the default; a run is pinned to the version it started with
(ADR-0303 §SD6), so rollback moves a pointer and reverts nothing. Projection
passes, constants, prompt sections and routes per purpose may change under
the gates; guards may only be added; authority is not in the profile schema
at all; producers, the journal and the laws never change by this path.
Pure projections can be swapped between calls with nothing to undo, which is
the hard problem of runtime replacement (Cordis) that this design avoids
rather than solves. Who proposes, by rising risk: a person editing a profile
(build first — it is what makes tuning measurable at all); a parameter
search without a model (compaction trigger and tail, elision size, samples
per purpose; exact on cost); prompt sections revised by a model reading
failed runs, judged only by trials; projection SQL written by a model, which
needs the T0 gate and an ADR that draws the premise line as ADR-0132 does
for applets; cross-run lessons or memory, the riskiest, because a lesson is
self-re-entering content that needs an endorsement before a default profile
carries it. Boundary by construction: a candidate's guard set contains its
parent's; automatic rollback targets only a version whose guards contain the
current one's; a route change marks every profile's evidence on it stale;
the proposer is itself an agent run whose journal is the candidate's
lineage, reading through a scoped view and asking consent before spending on
trials; the evaluator is not the model being tuned, and the journals a
proposer reads are not the ones that judge it. A profile tuned on confined
journals is confined. Order: compaction first, because its constants are
numeric, its cost is exact offline and its quality has the bounded check
above.

Signals the journal already holds: verifier pass rates, recalls after
elision, consent denials, moderator escalations, retries and corrections by
the person. Rationing and moderation (ADR-0300, ADR-0302) are the runtime
controllers over the same quantities — UCON's authorisation re-evaluated as
usage accumulates — and every lever they hold only lowers.

### 5.7 Limits

- **Counterfactual replay is exact for the first divergent request only.**
  Later steps depend on what the model would have done; that is the
  off-policy problem, and only trials answer it.
- **Goodhart.** Optimising tokens or cache hits degrades quality unless
  outcomes are measured apart from the optimiser; required-context laws
  (L7, L25) keep a cost optimiser from dropping what matters.
- **The literature's quality claims are weak.** Several single runs; few
  variances. The tree's trial discipline — a citable claim with its
  conditions, no figure without its pair of arms — applies to every claim
  this harness would make about itself.

---

## 6 Open decisions, with leanings

Collected from the two consolidations, duplicates folded. An ADR would have
to take each; the leaning is the compiler's. *Taken* marks a working
assumption from the design discussion of the compile date, still to be
recorded by an ADR. The ADRs each decision touches are in
[agent-harness-adr-impact](./agent-harness-adr-impact.md).

| # | Decision | Leaning |
| --- | --- | --- |
| 1 | the journal as the only source of model input | taken; into ADR-0303 before it is built |
| 2 | where π runs | taken: SQL in a local worker (Q2), the server holding the record |
| 3 | who owns the request bytes | open: SQL emits JSON, or rows that Go serialises |
| 4 | compaction | span replace from a cut, a full checkpoint only if replay length becomes a measured problem; derive and elide before summarising; the person pinned; summariser on the turn's route by default |
| 5 | a pass shape for what SQL cannot do | taken: a new small `Pass[S]`; nanopass unchanged; declared `Requires`/`Produces` either checked or dropped in both |
| 6 | whether π's SQL stages are spliced by nanopass passes per profile | open |
| 7 | profile representation | open: Go literals in a registry, or data files gate-tested like `promptbook` |
| 8 | coordinator tools as operation specs | adopt; the widest change in `apps/chat` |
| 9 | the label lattice | integrity level × TLP tier (from leeway's `tlp` aspects) × exposure and origin marks, per entry, with `endorse` as the only lowering, on the integrity axis only; which tier the compile-date `confined` maps to is a deployment default |
| 10 | branching | a parent ordinal now, unused until a consumer asks |
| 11 | fan-out and selection | for purposes with an exact verifier; N per purpose by measurement; no ensemble where nothing selects |
| 12 | the stateful-tool contract | adopt, with the footprint checked by conformance |
| 13 | a sandbox for model-written WebAssembly | a small shared package, instruction-bounded; the harness itself is not a sandbox for model-written code |
| 14 | threat model | drafted as [agent-harness-threat-model](./agent-harness-threat-model.md) for one cell kind; the ADR is next, before any measure ships |
| 15 | the five reservations | in ADR-0303's journal (§4.9), now including the chain digest and the reserved key id |
| 16 | agent schema | non-Truman by grant; one declaration compiled to a server principal and to the local worker; a `withheld` section |
| 17 | where the agent schema's principal lives | open: a server user per scope setting, or the host's introspection engine with the scope pinned in process |
| 18 | withheld encoding | the value-less section; blanking a value is absence under positivity for list sections |
| 19 | metered quantity | bytes everywhere, instances on the agent schema |
| 20 | canaries | shingles first; inserted tokens where a body has none worth protecting |
| 21 | endorsement scope | by digest and act class, never by conversation |
| 22 | encryption of bodies | deferred by the threat model; the key-id column reserved; per run if taken up |
| 23 | clipboard and export | prevented above the tier policy on org-managed hosts, recorded on administered ones; reference-only inside boxer for content whose tier requires it; an `output_hook` plugin as the desktop choke point |
| 24 | watermark payload | session handle and epoch; live-path embedding deferred with ADR-0241 |
| 25 | self-improvement | profiles only, between runs, through T0–T4 and a person's approval; guards add only; authority never |
| 26 | cross-run memory | none until lessons can be endorsed and labelled |
| 27 | differential privacy | deferred until a question the group floor does not answer is asked |
| 28 | what the chat's "Keep off" means once the journal is always written | open: a journal deleted when the window closes, or kept only until resume is no longer possible; ADR-0265 calls Keep off "the one 'no text kept' choice a user has" |
| 29 | executing model-written SQL to verify it | open: a declared operation class for sandboxed evaluation in a worker, or the `run` effect |
| 30 | anchoring cadence and anchor-store contents | open: per turn, per run or per sync; detection granularity against A5 equals the cadence; what the store must hold for a regulator to check without the cell |
| 31 | deletion against the chain | open: ADR-0303 §SD8's "deletion removes the entries" breaks a chain the anchor store holds; a tombstone that keeps digests and drops bodies, or an accepted recorded gap |
| 32 | the label the harness declares on agent-caused fetches | the run's current label on every fetch, so the egress service's wall (ADR-0262 §SD4) has something to enforce; canaries on the request body |
| 33 | peers of another organisation | open; presumably level 2 content and a clearance per peer (threat model open question 1) |
| 34 | the browser tab host and the cell | open: whether a browser that renders a cell's windows (ADR-0278) is inside the declared boundary; the clipboard frame and the display protocol are exits if it is not |
| 35 | the security profile of unattended cells | out of this page's scope; a separate threat model |

## 7 What needs an ADR

The companion [agent-harness-adr-impact](./agent-harness-adr-impact.md)
lists, per accepted ADR and section, what the design would change and by
which tier. In short: the threat-model ADR, for which
[agent-harness-threat-model](./agent-harness-threat-model.md) is the draft,
beside ADR-0026 and with the cell declared in ADR-0126's topology; an
agent-schema ADR; a harness-profile-and-projection ADR or an extension of
ADR-0303 in place, including the chain digest and the deletion decision; a
tamper-evidence ADR for heads and the anchor store; dated `Updates` on
ADR-0269 (budgets as exit bounds, sinks from the boundary, revision writers,
the stateful-tool contract), ADR-0264 (locality of the write path),
ADR-0265 (history as state), ADR-0262 (the label a caller forwards),
ADR-0287 (downscaling against the watermark), ADR-0241 (scaling, and the
live path the threat model now relies on), ADR-0298 (unattended mode moves
to its own cells) and ADR-0026 (clipboard payload); a clipboard-and-export
ADR; a compaction ADR.

## Appendix A Spike conditions

All on one machine on 2026-10-10; material in the session scratchpad, not in
the tree. Figures are the spike's own and are not citable as trial results.

- **Projection.** `clickhouse local` 26.9.13.15; a plain MergeTree
  `entry(run, ord, kind, role, key, target, body, calls, label, drop) ORDER BY
  (run, ord)`; 2 000 synthetic runs of 400 entries (504 MiB uncompressed),
  one hand-built run with a compaction, three edits and a section change,
  one run of 5 000 entries; one SQL statement; 20 warm repetitions in one
  process.
- **Laws L1–L5, L8 and selection.** The same engine and schema plus
  `candidate`, `verdict` and `select` kinds; planted violations per law;
  five candidates and nine verdicts on one step; three selection rules; a
  synthetic corpus for Coverage@k.
- **Compaction.** The projection schema plus the plan view; one synthetic
  run of 40 turns (801 entries, about 140 000 tokens by characters over
  four, tool results heavy-tailed); a 64 000-token context with 8 000
  reserved; a 1 500-token placeholder summary; a small driver replaying
  entries and appending `compact` when told; a cached input token at 0.1 of
  an uncached one, output at 4×, units uncached tokens, replies' own output
  excluded.
- **(a) Row policy.** A scratch `clickhouse server` 26.9.13.15 with
  `custom_settings_prefixes = custom_`; `agentrun_entry(run, ord, kind,
  body, label)` of 1 000 rows over 10 runs; user `agent42` under
  `SETTINGS readonly = 2 READONLY, custom_agent_run = 4 READONLY`; views
  `my_run` and `agent_history` `SQL SECURITY DEFINER DEFINER = default`;
  restrictive and per-user policies; probes through `clickhouse client`.
- **(b) Masking.** `clickhouse local`; a three-row table with sections
  `contact` (scalar) and `note` (string array), memberships as registry ids
  on `lr`/`lrcard`, one instance carrying two memberships;
  `LW_RAGGED_PARENT_IDS` by its read-back body; the view and the L11–L13
  checks.
- **(c) Canaries.** `clickhouse local`; the subsystems page's Appendix A DDL;
  nine entries of one run; a one-row `canary` table; `position`, four-word
  shingles, `ngramSimHash`, `ngramMinHashArg(…, 3, 25)`.
- **(d) Watermark.** The `watermark` crate built `--locked --release` under
  its pinned toolchain; a scratch binary depending on it by path, reporting
  `recover_words` at origin, `decode_at_origins` and `decode_report` (the
  crate's `roundtrip` needs ffmpeg, absent on the host); `TileSpec::new(8.0)`;
  payload `deadbeef12345678`; four git-ignored scene captures (RGBA, opaque);
  crops, rescales and the JPEG re-save with Pillow 12.1 on the marked
  grayscale PNG.
- **(e) Clipboard.** Read-only: the broker, the three hosts, the IDL
  definitions and the Go widgets.
- **Keys in `query_log`.** The server of (a); `encrypt`/`decrypt` literals, a
  `reverse()`-wrapped literal, a `sipHash64` literal, a `{k:String}`
  parameter; `clickhouse local` with a `SET param_` prelude.

## Appendix B Theory, dated

| Work | Year | Gives | Read |
| --- | --- | --- | --- |
| Stonebraker, Wong, "Access control in a relational data base management system by query modification" | 1974 | query modification; the mechanism behind row policies | **[V]** |
| Rizvi, Mendelzon, Sudarshan, Roy, "Extending query rewriting techniques for fine-grained access control", SIGMOD | 2004 | Truman vs non-Truman; authorization views | **[U]** abstract and one author's lecture notes |
| Bell, LaPadula, MITRE MTR-2997 Rev. 1 | 1976 | no read up, no write down; tranquility | **[U]** metadata only |
| Biba, MITRE MTR-3153 | 1977 | strict integrity, low-water-mark, ring | **[U]** not obtainable |
| Denning, "A lattice model of secure information flow", CACM | 1976 | `FM = (N, P, SC, ⊕, →)`; classes form a universally bounded lattice | **[V]** |
| Denning, Denning, Schwartz, "The tracker", TODS | 1979 | individual and general trackers | **[V]** |
| Lunt, "Aggregation and inference: facts and fallacies", IEEE S&P | 1989 | aggregation vs inference | **[U]** not obtainable |
| Myers, Liskov, "A decentralized model for information flow control", SOSP | 1997 | owner-stated policies; declassification by authority | **[V]** |
| Stefan, Russo, Mitchell, Mazières, LIO, Haskell Symposium | 2011 | the floating current label bounded by a clearance | **[V]** |
| Dwork, "Differential privacy", ICALP | 2006 | the definition; auxiliary information | **[V]** |
| Debenedetti et al., CaMeL, arXiv 2503.18813 | 2025 | privileged/quarantined split; capabilities on values; confirmation, not blocking; fatigue | **[V]** |
| Costa et al., Fides, arXiv 2505.23643 | 2025 | product lattice; conservative propagation; constrained output types | **[V]** |
| Garby, Gordon, Sands, "The LLMbda calculus", arXiv 2602.20064 | 2026 | labels through every reduction; `endorse`; `send` checks `pc ⊑ l_c`; the implicit-flow leak; TIPNI in Lean | **[V]** |
| Tirmazi, Bishop, Markelon, Mitzenmacher, "Context Compaction Theory", arXiv 2608.01326 | 2026 | selection vs generation; minimum budget as one-way communication complexity | **[V]** |
| JIT-Agent, arXiv 2608.25593 | 2026 | log/view factorisation | **[V]** |
| Etas, arXiv 2607.17780; McCann, arXiv 2605.01030/01032; SHEPHERD, arXiv 2605.10913 | 2026 | effects, governance algebra, reversibility tiers | **[V]** |
| Bollig, Függer, Nowak, MSC for LLM agents, arXiv 2604.17612; λA, arXiv 2604.11767; Oracular Programming, arXiv 2502.05310; NLAH, arXiv 2603.25723; Cordis, arXiv 2608.25512 | 2025–2026 | per-role projection; bounded fixpoint; choice points; prompt-size result; commutativity and revert | **[V]** |
| ArchAgents, arXiv 2603.28906 | 2026 | eighteen theorems, no proofs, no tools — graded unusable (§1.3) | **[V]** |
| "Harness Engineering as Categorical Architecture", arXiv 2605.12239 | 2026 | rests on ArchAgents — graded unusable (§1.3) | **[V]** |
| Dai et al. on harness convergence, arXiv 2608.23953 | 2026 | external verifiability as the missing dimension | **[V]** |
| DSPy, arXiv 2310.03714; DSPy Assertions, arXiv 2312.13382 | 2023 | compiled prompt programs; assertion retry | **[V]** |
| Ensemble papers (self-consistency; 2402.05120; 2407.21787; rStar; MoA; debate; and their counter-evidence) | 2022–2025 | selection signals | **[U]** secondary summary and recall |

## Appendix C Example SQL from the spikes

Ran on `clickhouse local` 26.9 against the demo journals of Appendix A.
Illustrative, not a contract; the labels here are the spike's one-byte bit
set, not the label of §4.2. The plain-shaped journal:

```sql
CREATE TABLE agentrun_entry
(
    run UInt64, ord UInt32,
    kind Enum8('user' = 1, 'request' = 2, 'reply' = 3, 'intent' = 4, 'outcome' = 5, 'note' = 6,
               'section' = 7, 'tools' = 8, 'compact' = 9, 'edit' = 10, 'profile' = 11, 'turn' = 12,
               'asked' = 13, 'answered' = 14, 'candidate' = 15, 'verdict' = 16, 'select' = 17),
    key String, pos Int32, target UInt32, body String, calls String,
    effect Enum8('none' = 0, 'view' = 1, 'document' = 2, 'run' = 3, 'consequential' = 4),
    label UInt8, drop UInt8, digest String, route LowCardinality(String), ref UInt32
)
ENGINE = MergeTree ORDER BY (run, ord);
INSERT INTO agentrun_entry (...) SETTINGS async_insert = 0 VALUES (...);
```

The projection, one CTE per pass, each message carrying the ordinals it came
from (abridged):

```sql
CREATE VIEW agentrun_request AS
WITH
    j AS (SELECT * FROM agentrun_entry WHERE run = {run:UInt64} AND ord < {n:UInt32}),
    sections AS (...),   -- rewrites: argMax(body, ord) per name, declared order
    cut      AS (...),   -- rewrites: latest compaction, keep ord >= target
    edits    AS (...),   -- rewrites: latest per target, drop flag
    msgs     AS (...),   -- appends: user, reply, outcome, note by ordinal; src = [ord]
    tools    AS (...),   -- rewrites on change: latest declaration per name, sorted
    ordered  AS (SELECT arraySort(x -> (x.1, x.2), groupArray((part, ord, role, content, calls, tool_call_id, src, lab))) AS rows FROM seq)
SELECT request_json, label, provenance, lower(hex(SHA256(request_json))) AS digest FROM body;
```

A law returns the rows that break it — L2, tool pairing:

```sql
SELECT r AS run, tool_call_id
FROM (SELECT r, JSONExtract(request_json, 'messages',
                  'Array(Tuple(role String, content String, tool_calls String, tool_call_id String))') AS m
      FROM (SELECT 43 AS r, request_json FROM agentrun_request(run = 43, n = 21, sys_mode = 'consolidate')))
ARRAY JOIN arrayFilter(x -> x.1 = 'tool', m).4 AS tool_call_id
WHERE NOT has(arrayFlatten(arrayMap(x -> arrayMap(c -> JSONExtractString(c, 'id'), JSONExtractArrayRaw(x.3)),
                                    arrayFilter(x -> x.1 = 'assistant', m))), tool_call_id);
```

Guards combine by maximum; the resume fold lists every act begun and not
settled (ADR-0303 §SD3):

```sql
SELECT CAST(greatest(authority, labels, budget), 'Enum8(\'allow\' = 0, \'ask\' = 1, \'deny\' = 2)') AS decision
FROM (SELECT /* g_authority */ 1 AS authority, /* g_labels */ 0 AS labels, /* g_budget */ 0 AS budget);

WITH j AS (SELECT * FROM agentrun_entry WHERE run = {run:UInt64})
SELECT b.ord, b.kind, b.key, b.effect,
       multiIf(b.kind = 'request', 'make the model call again',
               b.kind = 'asked', 'show the question again',
               b.effect IN ('none', 'view'), 'tell the model it did not complete',
               b.effect = 'consequential', 'never resend; list to the person first',
               'never resend; outcome unknown') AS on_resume
FROM j AS b
LEFT JOIN (SELECT key, kind FROM j WHERE kind IN ('reply', 'outcome', 'answered')) AS s
       ON b.key = s.key AND s.kind = multiIf(b.kind = 'request', 'reply', b.kind = 'intent', 'outcome', 'answered')
WHERE b.kind IN ('request', 'intent', 'asked') AND s.key = '';
```

Selection over candidates and verdicts (§5.4), reliability-weighted:

```sql
WITH
    c AS (SELECT ord, route, lower(replaceRegexpAll(trim(body), '\\s+', ' ')) AS norm
          FROM agentrun_entry WHERE run = {run:UInt64} AND kind = 'candidate' AND key = {step:String}),
    v AS (SELECT ref, min(body = 'pass') AS all_pass, groupBitOr(label) AS vlabel
          FROM agentrun_entry WHERE run = {run:UInt64} AND kind = 'verdict' GROUP BY ref),
    w AS (SELECT route, acc FROM route_reliability WHERE purpose = {purpose:String})
SELECT argMin(c.ord, c.ord) AS chosen, sum(log(w.acc / (1 - w.acc))) AS score, groupBitOr(v.vlabel) AS label
FROM c INNER JOIN v ON v.ref = c.ord INNER JOIN w ON w.route = c.route
WHERE v.all_pass = 1
GROUP BY c.norm ORDER BY score DESC, chosen ASC LIMIT 1;
```

The compaction plan (§5.5), abridged; the boundary test keeps a cut out of a
call–result span:

```sql
CREATE VIEW compaction_plan AS
WITH
    j     AS (SELECT * FROM agentrun_entry WHERE run = {run:UInt64} AND ord < {n:UInt32}),
    prev  AS (...),   -- latest compact: previous cut, its label
    spans AS (SELECT groupArray((r.ord, o.ord)) AS sp
              FROM (SELECT ord, key FROM j WHERE kind = 'outcome') AS o
              INNER JOIN (SELECT ord, arrayJoin(arrayMap(c -> JSONExtractString(c, 'id'), JSONExtractArrayRaw(calls))) AS k
                          FROM j WHERE kind = 'reply') AS r ON r.k = o.key),
    live  AS (...),   -- message entries from the previous cut, with token counts
    cand  AS (SELECT ord, kind,
                     sum(tok) OVER (ORDER BY ord DESC ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS tail_tok,
                     kind IN ('user', 'reply') AND NOT arrayExists(s -> s.1 < ord AND ord <= s.2, (SELECT sp FROM spans)) AS safe
              FROM live)
SELECT ... AS verdict, ... AS cut, ... AS summary_label_floor FROM ...;
```

The agent-schema spike's scoped view and backstop (§4.3), on the scratch
server:

```sql
CREATE SETTINGS PROFILE agent42_p SETTINGS readonly = 2 READONLY, custom_agent_run = 4 READONLY TO agent42;
CREATE VIEW harness.my_run SQL SECURITY DEFINER DEFINER = default AS
  SELECT * FROM harness.agentrun_entry WHERE run = getSettingOrDefault('custom_agent_run', 0);
CREATE ROW POLICY backstop ON harness.agentrun_entry AS RESTRICTIVE
  USING getSettingOrDefault('custom_agent_run', 0) = 0 OR run = getSettingOrDefault('custom_agent_run', 0) TO ALL;
GRANT SELECT ON harness.my_run TO agent42;
```

## References

Systems read on the compile date **[V]**: DeepSeek Harness (`docs/` and
package READMEs at the head of 2026-10-09, version `0.2.1-alpha.2`);
OpenHands `software-agent-sdk`; `openai/codex`; `langchain-ai/langgraph`;
`pi-mono`; `sst/opencode`; `letta-ai/letta`; DSPy; Claude Code, Temporal,
Restate and DBOS documentation.

In this tree: ADR-0002, 0006, 0009, 0025, 0026, 0028, 0094, 0108, 0115,
0126, 0132, 0133, 0145, 0171, 0188, 0198, 0240, 0241, 0254, 0262, 0264,
0265, 0269, 0270, 0277, 0278, 0280, 0281, 0282, 0283, 0284, 0287, 0288,
0296, 0298, 0300 (proposed), 0302 (proposed), 0303 (proposed);
[agent-harness-threat-model](./agent-harness-threat-model.md);
[why-boxer](../explanation/why-boxer.md);
[iso25010-temperament](../explanation/iso25010-temperament.md) (D3, D11,
D12);
[spatiotemporal-composability-lessons](../explanation/spatiotemporal-composability-lessons.md);
[leeway-query-algebra](../explanation/leeway-query-algebra.md);
[leeway-sql-read-surface](../explanation/leeway-sql-read-surface.md);
[agentic-workflow-engines-survey](./agentic-workflow-engines-survey.md);
[agentic-workflow-literature-survey](./agentic-workflow-literature-survey.md);
[agentic-workflow-security-survey](./agentic-workflow-security-survey.md);
[llm-chat-app-requirements-survey](./llm-chat-app-requirements-survey.md);
the `watermark` crate's [README](../../rust/watermark/README.md) and
[EXPLANATION](../../rust/watermark/EXPLANATION.md).
