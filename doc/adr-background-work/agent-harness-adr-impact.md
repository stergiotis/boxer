---
type: explanation
audience: whoever turns the agent-harness design into decisions — which accepted ADRs would need a dated Update or a superseding record, which proposed ones change in place, and which decisions have no ADR yet
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.
> Compiled 2026-10-10 as the companion of
> [agent-harness](./agent-harness.md). Nothing here is a decision, and no
> ADR is changed by this page. Each row says what an ADR's section reads on
> the compile date (read from the working tree **[V]**), what the design
> would ask of it *if the open decisions of agent-harness §6 go with their
> leanings*, and by which edit tier of
> [DOCUMENTATION_STANDARD §1](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way):
> a proposed ADR is edited in place (Tier 1); an accepted ADR changes only
> by a dated `## Updates` entry (Tier 2); a decision that supersedes one
> gets its own ADR (Tier 3). Several rows take up a deferral the ADR
> already records; those need no edit until something is built, and are
> listed so that the record is one list. Rows marked *threat model* follow
> the [threat model](./agent-harness-threat-model.md) compiled later the
> same day, which the design page's §4 now follows. On 2026-10-11 the new
> records in §3 were drafted as ADR-0307 to ADR-0317 (proposed) and
> ADR-0303 was superseded by ADR-0308; the rows below describe the state
> before that and are kept as the derivation.

# Agent harness: what the design asks of existing ADRs

Caveats first. The design is unbuilt and undecided, so every row is
conditional. The list was made by reading the ADRs the design page and
its four since-folded working pages cite, not every ADR in the tree; an ADR not listed may still be touched by
an implementation. Where a row says "no edit", it means the ADR already
records the deferral or the rule the design relies on, and the design cites
it rather than changes it.

## 1 Accepted ADRs

Ordered by how much the design asks of each.

### ADR-0269 — App operations: a command/query contract agents drive under a task grant

| Section | Reads now | Would change | Tier |
| --- | --- | --- | --- |
| §SD6 budgets | the grant lists budgets for calls, rows and bytes, launches, captures, wall time; the task struct has call, launch and deadline fields | budgets restated as an **exit budget** (bytes sent to a remote route per task) plus **bytes** on every read path and **instances** on the agent schema, in ADR-0300's ledger shape; rows dropped as a bound, since a leeway row bounds nothing (agent-harness §4.4, L19) | Tier 2 |
| §SD6 "the restricted database user that closes it" | views, dictionaries and engines can reach beyond the endpoint; a restricted user closes it | the restricted user becomes the **agent schema's principal**: a constrained scope setting, definer views, a setting-keyed restrictive policy as backstop (§4.3, L18) | Tier 2 pointing at the agent-schema ADR |
| §SD7 labels | a window's and a capture's label; outcomes carry labels; taint belongs to the conversation and "is recorded on every action"; untrusted content delimited and attributed | the label becomes **integrity level 0–4 × TLP tier × exposure and origin marks** (*threat model* SD2, SD6, SD7); taint is the integrity axis and **gates at sinks** instead of only changing how widenings are shown; re-entered content is delimited like untrusted content, with its own marker; same-organisation peer content enters at level 1 with carried marks (§4.2, §4.6) | Tier 2 |
| §SD8 writer | every change a command makes carries the task as its writer; the person's gestures carry the person | the writer of each revision is **folded into a resource label**: integrity the lowest surviving writer, confidentiality the join of what each had read; an unrecognised paste enters at level 2 (§4.6) | Tier 2 |
| §SD1 / operation contract | commands and queries, effect classes, declared writes | the **stateful-tool contract**: footprint (observed ⊆ declared, L9), epochs as journaled notes, rebuild class per resource, fork class, completion as its own entry (§2.4) | Tier 2, or its own ADR if the conformance test is a new package |
| §SD12 deferred | "policies over argument values beyond destinations"; "separating planning from data (CaMeL and FIDES)" | **no edit**: the design keeps the CaMeL split deferred and checks labels at sinks instead; the sink registry and outbound SQL text as a sink are new policy over *where*, not over argument values | — |
| Updates 2026-10-04 (action record) | `AgentAction` keeps a digest of the arguments, never the arguments | **no edit** on the record; the design relies on it and notes that `LlmMessageBody.ToolCalls` still holds the arguments under retention (§4.5) | — |

### ADR-0303 (proposed) is in §2; it is the ADR most of the design lands in.

### ADR-0264 — Retained model conversations on `boxer.facts`

| Section | Reads now | Would change | Tier |
| --- | --- | --- | --- |
| §SD5 and Status | confined text is kept only on a local store; the Status records that "the write path does not yet check where the facts server is" | the **facts write path's locality check** becomes a registered sink with a clearance, checked by L14; the Status gap is closed when built | Tier 2 when built |
| §SD6 ditch test | an app must behave the same with kept bodies cleared; a chat resuming across sessions relies on the record by choice | **no edit**: the journal (ADR-0303 §SD1) is the run's state, the kept `llmMessage` rows stay audit, and the ditch keeps holding; the design cites this | — |
| §SD2 branching | "branching is a parent id, not an edit" | **no edit**: carried into the journal as a parent ordinal if branching is taken (§2.2, open decision 8) | — |
| §SD7 deferred: streaming, purge, masking | deferred | **no edit**; per-run encryption is deferred by the *threat model* (erasure out of scope), with a reserved key-id column (§4.7); if taken up, its ADR says how key destruction relates to the purge | — |

### ADR-0265 — A chat app over retained model calls

| Section | Reads now | Would change | Tier |
| --- | --- | --- | --- |
| §SD2 | "the history the app resends each turn is its state"; a new window starts an empty conversation | superseded by the journal as the only input: the app keeps no history; every request is `π(J[0..n])` (§2.2) | Tier 2 pointing at ADR-0303, or Tier 3 if ADR-0303 does not take the stronger reading |
| §SD3 and §SD8 | the turn loop; the turn is a declared machine that observes only | the **driver** (S2) owns the turn and step machine; the loop's hooks take the composition shapes of §2.5; `runTurn` takes `promptbook.CompleterI` | Tier 2 |
| §SD5 deferred | resume, a context window ("what to drop … as a declared omission"), a system prompt of the person's, edit and regenerate, branches, deleting a conversation, sensitivity of pasted results | resume is ADR-0303; the context window becomes **compaction** (§5.4) with `compact` and `edit` entries rather than a declared omission; the system prompt becomes `section` entries in a prompt book; pasted results carry labels through the clipboard handle (§4.8) | Tier 2 recording which deferrals moved where |
| §SD6 the coordinator | fixed tools as JSON literals routed by a chain | the coordinator's tools become **operation specs** (§2.4, open decision 6) | Tier 2 |

### ADR-0270 — Play's operations catalog and its agent limits

| Section | Reads now | Would change | Tier |
| --- | --- | --- | --- |
| §SD2 agent limits | a plain read against the granted endpoint is allowed; `keelson()` tables by name; `readonly = 2`; "the limits fail a run; they do not change it" | the statement runs as the **agent-schema principal**, so base tables are not nameable and `readonly = 2`'s remaining `SET` surface is bounded by a constrained profile (§4.3); an endpoint outside the declared cell is refused by the topology before any text is composed, so SQL text is an exit only there (*threat model* SD1, SD4; §4.5) | Tier 2 |
| §SD4 label of a result | the main lane keeps the dispatch decision's sensitivity; the window is confined while the result is | the label widens to the pair; several panes' lanes carry no confinement label on the compile date (subsystems §7.3) | Tier 2 |
| Updates 2026-10-05 (publishing a projection) | `publish_result` / `publish_projection` check effect, consent and ceiling | publishing inside the cell is a **consequential act**, not an exit (*threat model* SD5): the endorsement rule applies when the justification is at integrity 3–4 (§4.6, L15); a publish to a peer is sync and the person's act | Tier 2 |
| §SD1 / §SD3 effects | effect declarations | the footprint check would find live re-runs and fetches understated; `run`'s outcome records queuing, not the result (§2.4) | Tier 2 with the stateful-tool contract |

### ADR-0277 — One audit trail for model calls and agent work

| Section | Reads now | Would change | Tier |
| --- | --- | --- | --- |
| §SD6 | the coordinator keys a tool call by model call id and index | **no edit**: L1 and the write-ahead laws join journal and trail on this key | — |
| §SD7 | what an agent's call causes carries the task | outbound rows (`LlmCall` by route, `HttpFetch` by destination, consequential `AgentAction`, clipboard writes) carry the **TLP tier and marks** so L14 can join them against the sinks the cell boundary names; trail rows gain a **chain digest** so the heads of §4.10 cover the trail as well as the journal (*threat model* SD8) | Tier 2 |
| §SD10 deferred: host verification of `Cause` | deferred | taken up by L1 as a query rather than a host check; the ADR can record that | Tier 2 |
| trail views (`trailviews`) | labels passed through as columns, not enforced | **no edit**: enforcement stays the reader's; the agent schema is generated the same way (§4.3) | — |

### ADR-0280 — A ceiling on what a chat's model may do

| Section | Reads now | Would change | Tier |
| --- | --- | --- | --- |
| §SD2, §SD3 | the dispatcher refuses above the ceiling; the ladder | **no edit**: authority stays compiled in the dispatcher; harness guards only add refusals (§2.5) | — |
| §SD7 deferred: "a ceiling on budget and time"; "reads are unpaced, so how fast a model reads data is unbounded" | deferred | read budgets in bytes and instances (§4.4) bound what the ceiling does not; the ADR records that the bound lives in the ledger, not the ceiling | Tier 2 |
| §SD7 "persisting the settings" | deferred | settled by ADR-0303 §SD7 already; the profile (§2.6) is where the rest of the constants go | — |

### ADR-0281 — Window captures through one policy enforcement point

| Section | Reads now | Would change | Tier |
| --- | --- | --- | --- |
| §SD6 | each capture is a trail row with a digest of the bytes after every obligation, "so a later watermark is part of what the digest covers" | the watermark becomes an obligation that embeds a **session handle plus display epoch**, with an `issued` journal entry (§4.8, L21); the epoch is what an endorsement names (§4.6); the *threat model* now relies on the live-frame path for screen attribution, which ADR-0241 leaves unqualified | Tier 2 |
| §SD6 | every capture is untrusted and taints the conversation | unchanged in substance; "untrusted" becomes integrity level 2 on the pair | Tier 2 wording |

### ADR-0282 — The chat's artefact

| Section | Reads now | Would change | Tier |
| --- | --- | --- | --- |
| §SD1–§SD3 | one markdown document per conversation edited by the model under the ceiling | the artefact carries **per-revision labels** folded into a document label (§4.6); an endorsement by digest and act class is what lifts it before a consequential act (L15) | Tier 2 |
| §SD7 deferred: keeping the artefact; a pasted image's label | deferred | keeping it is a `refold`-class resource of the journal (§2.4); a paste enters through the clipboard handle with its labels or at level 2 (§4.8) | Tier 2 recording where the deferrals moved |

### ADR-0283 — Chat analytics

| Section | Reads now | Would change | Tier |
| --- | --- | --- | --- |
| §SD2 sources | `keelson('agent_grants')`, `keelson('agent_actions')` | the **profile id** on every row, so "A against B" is a `GROUP BY`; new introspection tables for profiles, the sink registry and law violations (§2.6, §5.1) | Tier 2 |
| `surface.go` `Classify*` | restate the dispatcher's `check` order for analytics | one **decision table** both read, so they cannot drift (§5.2) | Tier 2 |

### ADR-0287 — Pixels

| Section | Reads now | Would change | Tier |
| --- | --- | --- | --- |
| §SD4 locality | "only to a local model" labels the request confined; the wall enforces it; "a lying coordinator is not caught" | the model route is a sink with a TLP clearance, inside the cell only when the host is declared trusted (*threat model* SD1); the check reads the tier, not one bit (§4.5) | Tier 2 |
| §SD7 deferred: downscaling before sending | deferred, "a ceiling on the long edge would be an environment variable" | spike (d) found a rescale **defeats** the watermark entirely, so downscaling must happen **before** the mark is applied, or the mark must be applied at the sent size; the deferral should carry that constraint | Tier 2 |
| §SD7 deferred: labels per image | the conversation's label covers it | per-image labels become the pair, so a screenshot's integrity (level 2) and its issuance epoch travel with it | Tier 2 |

### ADR-0241 — Cooperative image identification watermark

| Section | Reads now | Would change | Tier |
| --- | --- | --- | --- |
| Status | accepted; "live-stream integration" unqualified; not evidence of authorship | a dated entry recording spike (d): recovery under crops and a JPEG re-save, **no recovery under 80 % and 50 % rescales** or at thumbnail size, mean luminance change 4.4–6.1; and the payload layout (session handle plus epoch) if issuance is adopted | Tier 2 |

### ADR-0026 — App runtime and capability subjects

| Section | Reads now | Would change | Tier |
| --- | --- | --- | --- |
| threat model; 2026-07-15 Update | "hygiene, not security"; unchanged and correct as written | **no edit**: a separate **threat-model ADR** names A1–A4 and the sinks, and cites ADR-0026 for the in-process posture it leaves standing (§4.1) | Tier 3, beside, not superseding |
| Update 2026-05-30 `clipboard.write` | write-only, UTF-8 text, request/reply with an empty ack; the broker is the only builder of the opcode | the payload grows a **label and a source**; a **host plugin** on egui's `output_hook` drops `CopyText` that did not come through the broker under a policy opcode; copy by reference inside boxer for content whose tier requires it; plaintext out of boxer follows the deployment's approval tiers on org-managed hosts and is recorded on administered ones (*threat model* SD3); a lint that keeps "only builder" true (§4.8, L20) | its own clipboard-and-export ADR, with a dated entry here pointing at it |
| §SD3 taxonomy | subject families | new subjects for the key service and the sink registry if they are bus services; introspection tables otherwise | Tier 2 when they exist |

### ADR-0262 — HTTP egress as a keelson capability

| Section | Reads now | Would change | Tier |
| --- | --- | --- | --- |
| §SD4 sensitivity wall | a confined request is refused unless the destination is local; "the label is the caller's declaration" and no consumer forwards one | the wall reads a **TLP tier**; the harness **declares the run's current label on every agent-caused fetch**, since the *threat model* (SD5, open question 4) leaves the outbound half of a fetch to this service; the request body is a canary sink (§4.5, L10, L14) | Tier 2 |
| §SD5 every call is a record | — | the record carries the tier and marks | Tier 2 |

### ADR-0145 and ADR-0240 — sealed app data; sealed store, owned capability

| Section | Reads now | Would change | Tier |
| --- | --- | --- | --- |
| ADR-0240 §SD1 | "custody is ownership, not lookup"; the key is a private field | **deferred** with per-run encryption (*threat model* §6): if taken up, a host key service is that custody shared across runs with a journal row per use, a new ADR citing both | — |
| ADR-0145 design space | decryption on the ClickHouse side of `file()`/`url()` rejected, "key material on a wire that today carries only a handle" | **no edit**: the design keeps keys out of every statement, including the worker's | — |

### ADR-0171 — Leeway SQL read surface

| Section | Reads now | Would change | Tier |
| --- | --- | --- | --- |
| read-back UDFs; `ComposeCreateView` | same-shape views with a callback per column; lane arithmetic warned against | the **`withheld` section** must enter the view IR so a generator emits its two lanes; the agent schema is generated through this surface, not hand-written (§4.3) | Tier 2, or part of the agent-schema ADR |
| third tension (encrypted columns) | an encrypted column answers no value predicate; per-writer keys defeat sort-key locality | **no edit**: the design accepts the cost for journal bodies, which are not sort keys (§4.7) | — |

### ADR-0188 — App instance effect tracking

| Section | Reads now | Would change | Tier |
| --- | --- | --- | --- |
| observed resource changes per frame | the engine records which resources changed | the **footprint conformance test** (observed ⊆ declared, L9) is built on this record (§2.4) | Tier 2 noting the consumer |

### ADR-0028 — chlocal low-latency SQL cap

| Section | Reads now | Would change | Tier |
| --- | --- | --- | --- |
| pre-spawned single-use workers taking Arrow input | — | the **projection worker** per model call (placement Q2, §2.3); the worker and server versions enter the run's fingerprint; a golden of projected requests in the default lane | Tier 2 noting the new consumer and the version obligation |

### ADR-0133 and ADR-0115 — chlocal param prelude; query observability

| Section | Reads now | Would change | Tier |
| --- | --- | --- | --- |
| ADR-0133 | the chlocal path sends a `SET param_<name>` prelude in SQL text | **no edit**; recorded as the reason a key may never be a parameter on that path (§4.7) | — |
| ADR-0115 | parameters never reach `query_log` | **no edit**; measured true for `{k:String}` on 26.9.13.15, with the addition that `encrypt`/`decrypt` literals are `[HIDDEN]` and other literals are not — worth a dated note if an ADR ever relies on it | Tier 2, optional |

### ADR-0009, ADR-0094, ADR-0108 (with ADR-0002, ADR-0006), ADR-0126, ADR-0132, ADR-0025, ADR-0254

| ADR | Reads now | Would change | Tier |
| --- | --- | --- | --- |
| 0009 environment-variable registry | one variable per knob | a variable **selects a profile**, replacing the `BOXER_CHAT_*` family (§2.6); each retired variable needs its registry entry retired | Tier 2 per variable, when retired |
| 0094 introspection tables | `keelson('…')` tables | new tables: profiles, the sink registry, law violations, `agent_runs` and `agent_run_entries` (ADR-0303 §SD8) | Tier 2 as each lands |
| 0108 pass registry; 0002, 0006 nanopass | `(Stage, Order, Name)`, `AssertProperties`, `Requires`/`Produces` | **no edit**: the harness copies the shape as a small `Pass[S]` and does not import it (open decision 4); the unused `Requires`/`Produces` is a lesson, not a change | — |
| 0126 appliance topology as data | nodes with one key format, a marking contract and a component registry, typed tables and a graph projection; declared-versus-observed `GROUP BY`; "cell" is not a term it uses | the *threat model* (SD1) proposes it as the home of the **cell declaration**: hosts, engines and model hosts inside, peers outside, so that an edge leaving the cell is a sink with a clearance; `BOXER_LLM_TRUSTED_HOSTS` is the first fragment; the law over a run's declared profile reuses its `GROUP BY` | Tier 2, or a new ADR that extends it |
| 0132 SQL-defined applets | agent-proposed SQL as data, within premises | the line for **model-written projection SQL** (§5.5, proposer 4) is drawn the same way; a new ADR cites it | — |
| 0025 pushout forget architecture | a vault with per-subject envelope keys, unbuilt | **no edit**: the per-run encrypted bodies of §4.7 are not that vault and must not be mistaken for it; the key-service ADR says so | — |
| 0254 model inference as a capability | §SD3's locality rule | **no edit** to the rule: a trusted host is inside the cell by declaration; the check widens from one bit to a TLP tier inside the llm service (§4.5) | — |
| 0298 an unattended mode | the host decides within preset bounds | the *threat model* places unattended operation in **dedicated cells with their own security profile**; this design covers the interactive single-person cell only, and ADR-0298's mode is out of its scope (§4.1) | Tier 2 recording the cell kind, or a separate threat model |
| 0278 tab mode | a browser renders a cell's windows through `tabhost` | open whether the browser is inside the declared cell; if not, the clipboard frame and the display protocol are exits (design §6, item 34) | none until decided |

## 2 Proposed ADRs, edited in place

### ADR-0303 — A run journal written before each act

The ADR most of the design lands in. It is proposed, so these are Tier 1
edits, and the first is the one to take before it is built:

- **§SD1 / §SD2: the journal is the only source of model input.** Not a
  crash record beside an in-memory history; every request is a pure function
  of the prefix and the profile (§2.2, open decision 1).
- **§SD2 entry kinds**: add `section`, `tools`, `compact`, `edit`,
  `profile`, `candidate`, `verdict`, `select`, `completed`, `endorse`,
  `approval`, `head`, `copy`, `issued`, a reserved `keyuse`, and an
  `ignorable` kind; a closed enum with a golden; a parent ordinal if
  branching is taken; synced entries keep their cell-qualified run id.
- **§SD1 tables**: the `label` column widened to integrity level, TLP tier
  and marks; a **chain digest** per entry computed by the single writer
  (*threat model* SD8); a reserved key-id column, encryption itself
  deferred; the plain-shaped view for the Q2 worker.
- **§SD8 deletion against the chain**: "deletion removes the entries"
  breaks a chain the anchor store holds; either a tombstone that keeps
  digests and drops bodies, or an accepted recorded gap (design §6, item
  31).
- **§SD3 resume**: reads rebuild classes beside effect classes; "applied,
  not completed" as a state.
- **§SD6 fingerprint**: a fold over `section`, `tools` and `profile`
  entries plus the engine version.
- **§SD7 labels**: the pair, not taint and confinement as two flags.
- **§SD8 read surface**: the agent schema's `my_run`, `my_conversation`
  and `agent_history(scope)` are this surface's relations, generated, with
  the scope as a constrained setting, for least privilege against an
  injected agent rather than as access control against the person.
- **A projection pass** that marks re-entered content; the compaction plan
  view; the laws in the integration lane it already plans (M5).
- **§SD9 stays**: initiation, triggers, seeding stay out.

### ADR-0300 — Metered model calls and rules a moderator writes

- **§SD2 usage quantities**: bytes read and instances read join the set;
  samples of a fan-out are charged under their purpose (§5.3).
- **§SD3 ledger**: the profile id on every row.
- **§SD4 rules**: "never raise a limit" is the rule every harness guard
  inherits; worth saying once for both.

### ADR-0302 — A built-in moderator

- **§SD3 / §SD4**: thresholds and the ladder read from the profile rather
  than compiled constants; the engine behind an interface so the ladder can
  be a second implementation (design §2.9).
- **§SD2 live inputs**: the one token meter of §2.8 (C7) replaces the
  moderator's own context estimate.

## 3 New ADRs

| ADR | Decides | Cites | Depends on |
| --- | --- | --- | --- |
| **Threat model for a cell** | drafted as [agent-harness-threat-model](./agent-harness-threat-model.md): the cell and its declaration, principals, A1–A5, harms, TLP tiers, exits and approval tiers, integrity levels, marks, the four readers, what is trusted and out of scope (§4.1–§4.2, §4.5) | ADR-0026 for the in-process posture; ADR-0126 for the declaration; ADR-0254, 0262 for the walls | nothing; the ADR the others cite for "why" |
| **Tamper evidence** | the chain digest per entry and trail row, `head` entries, the anchor store and its contract, the anchoring cadence, the regulator's check (anchored heads plus L4), what deletion does to the chain, the completeness caveat (§4.10) | ADR-0303, 0277 | the threat-model ADR |
| **Agent schema** | the generated relations, the four-rule declaration language, the `withheld` section, the scope setting and its constraint, the restrictive backstop, L11–L13 and L18 (§4.3) | ADR-0171, 0270, 0303 §SD8 | the threat-model ADR; ADR-0303 |
| **Harness profile and projection** (or ADR-0303 §SD1 taken the stronger way) | the journal as the only input; π as SQL in a Q2 worker; pass properties; composition shapes per seam; the profile as data with a fingerprint; routes with capabilities; laws L1–L9 (§2.2–§2.6, §5.1–§5.2) | ADR-0303, 0009, 0028, 0094, 0108 | ADR-0303's first edit |
| **Compaction** | plan/act/apply; the ladder; the person never summarised; the summariser as a sink; the compaction laws; the thrash check (§5.4) | ADR-0265 §SD5's deferred context window | the profile ADR |
| **Stateful tools** (or an ADR-0269 Update) | footprint, epochs, rebuild class, fork class, completion; the conformance test (§2.4) | ADR-0269, 0188, 0270 | — |
| **Key service and per-run bodies** | **deferred** by the threat model; the key-id column and `keyuse` kind are reserved; if taken up: the host key service, per-run keys, what destruction means for ADR-0303 §SD8 deletion and ADR-0264 §SD7's purge; not the ADR-0025 vault (§4.7) | ADR-0145, 0240, 0171 | ADR-0303; a reason erasure returns to scope |
| **Sync and peer clearances** | how entries cross between cells with their marks, a clearance per peer, level 1 for same-organisation content, peers of another organisation; the carrier is not decided here (the pushout version control system of ADR-0025 and ADR-0209 is the compiler's guess, not a reading) | the threat-model ADR; ADR-0025, 0209 | the threat-model ADR |
| **Clipboard and export** | the broker payload with label and source, the host plugin, copy by reference inside boxer, approval tiers for plaintext out, org-managed versus administered hosts, paste restoring labels, the lint (§4.8) | ADR-0026's 2026-05-30 entry | the threat-model ADR |
| **Endorsement and the integrity pyramid** (may fold into the threat-model ADR) | levels 0–4; `endorse` by digest, act class, epoch and diff; the mixed-document fold; the re-entry marker (§4.6) | LLMbda, Fides, CaMeL; ADR-0269 §SD7/§SD8, 0282 | ADR-0303 for the kinds |
| **Fan-out and selection** | `candidate`/`verdict`/`select`; verifiers as read-only acts; N per purpose; "no selection signal, no ensemble" (§5.3) | ADR-0300 for purposes | the profile ADR |
| **Self-improvement boundary** | profiles as the only thing that changes; the gates T0–T4; guards grow only; the proposer as an agent run (§5.5) | ADR-0132 for the premise line | all of the above; last |

## 4 An order

Each step changes nothing a person sees until a second implementation is
selected, and each ADR cites the ones before it:

1. ADR-0303's first edit: the journal as the only input, the entry kinds,
   the pair label. Nothing else follows without it.
2. The threat-model ADR, from the drafted page, so that every later sink
   and label decision has a "why" to cite; with it, the cell declaration
   in ADR-0126's topology and the ADR-0298 note that unattended mode lives
   elsewhere.
3. The profile-and-projection ADR, with the compile-date behaviour as the
   only implementation and the laws L1–L9 in the integration lane.
4. The agent-schema ADR and the ADR-0269/0270 Updates on budgets and the
   publish label check — the first enforcement that is new.
5. The stateful-tool contract, because compaction's derive stage and the
   resume fold both read it.
6. Compaction, as the first second-implementation behind a seam and the
   first self-improvement target.
7. Tamper evidence (chain digests ship with the journal in step 1; the
   anchor store and cadence come here), endorsement, clipboard and export,
   and the watermark entries, which each need a journal kind and a sink the
   boundary names. The key service only if erasure returns to scope.
8. Fan-out and selection; then the self-improvement boundary, last.
