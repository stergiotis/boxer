---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-10-30, and revised
> the same day to cover every event kind on the trail rather than the
> agentic ones alone, as background to one decision: how the audit trail
> on `boxer.facts` and the per-conversation run journal are shown to a
> person — a *viewer*, whatever its shape — and what a reviewer can do from
> it. Nothing here is a decision;
> [ADR-0350](../adr/0350-a-trail-viewer-one-system-app-over-the-marts-a-case-a-window-a-verdict-and-review-marks.md)
> (proposed 2026-10-31) is, and settles §9's forks. §0 states what was
> found; §8 maps it onto the shapes available in this tree; §9 lists the
> forks. §2 says where the tree stood on the compile
> date and will go stale as it moves; the ADRs it cites are authoritative.
>
> **Provenance — clean-room.** This page rests on product and API
> documentation, specifications and standards, regulations and regulatory
> guidance, published papers and their abstracts, vendor and author blog
> posts, help-centre pages, and the *prose* of public issue pages. **No
> source file, repository code view, diff, commit page or package content of
> any surveyed project was read**; where a permitted page carried code, the
> behaviour is described and the code is not. The rule was applied to
> permissively-licensed projects exactly as to copyleft ones — the bar
> [ADR-0119 §SD6](../adr/0119-imzero2-pipelineview-widget.md) set.
> Specifications that have moved into a repository (the OpenTelemetry GenAI
> conventions, CloudEvents, Trillian's whitepaper) were not followed there
> and are marked accordingly. The tree's own code was read freely and is
> what §2 describes.
>
> Five delegated web passes — visualization techniques; product UX
> paradigms; standards and reviewer requirements; implementation methods;
> human review evidence — and one tree pass did the reading on the compile
> date, each tagging every claim as read on a fetched page, seen only in a
> search snippet, recalled, or inferred. A claim appears below as fact only
> where a pass read it on a page; the rest is marked *(snippet)*,
> *(recalled)* or *(inference)*, or left out. **A number or a quoted rule is
> a pass's reading of a page only where it sits inside quotation marks.**
> The web passes were briefed before the scope widened, so §3 leans on the
> agent-oversight literature more than on operations review; §3's opening
> note says what a further pass would add. §10 lists the pages consulted
> and the incidents under the protocol.

# Showing an audit trail to a person — the design space, and what it meets in this tree

## 0 In short

- **The tree records more than it shows.** Every row of `boxer.facts`
  carries `Origin`; the trail adds `Conversation`, `Delegation` and `Cause`
  where they apply; the journal is hash-chained, and its verifier
  vocabulary — holds, broken at, gap, fork, unanchored — is served
  in-process. No surface shows any of it as a trail. Each app shows one
  kind: the log viewer log rows, play query runs, chat its own
  conversation's grants from in-process rings, the app center a fold of bus
  requests (§2).
- **Two columns the regulated tuple names are absent**: a before/after pair
  and a person-entered reason. Who, what, when, outcome, origin and
  authority each have a column or a join (§3.1).
- **The form of the trail is itself a requirement.** The GxP texts and
  NIST AU-7 demand sort, search and export and rule out flat files. Review
  is done with the record, at the record's frequency, by exception where a
  validated rule justifies it, and the review itself leaves evidence
  (§3.2–§3.3).
- **The reviewer's bottleneck is context, not reading.** SOC, forensic,
  healthcare and code-history studies agree; analysts prefer a dense table
  and distrust a highlight that does not show its rule. On agent rows, raw
  traces overwhelm, summaries mislead, and per-action approval decays
  (§3.4–§3.5).
- **The forms that scale aggregate to the display**: binned counts by kind,
  subject, domain and action, or outcome; actor × object matrices past
  about twenty nodes; the dotted chart as the one per-event form that needs
  no cause chain. Span trees, provenance graphs and per-record timelines
  serve a selected key only, and only where a parent edge exists (§5).
- **Shipped audit logs converge**: a time-ordered table with few fixed
  columns and a per-row pane; histogram, table and facets bound to one
  query; pivot by pin or context; a correlation id in every product;
  integrity asserted in prose and never shown per row. No product draws
  the person → agent → service chain, and the tree records more of it than
  any of them (§6).
- **Implementation**: neither sort key the tree carries names a
  correlation value, so reads by conversation, task or subject scan within
  the time range; a verifier needs saved heads, proofs and gossip, and a
  chain break is a range alarm rather than a per-row flag; reduction is
  permitted only where the originals and their order survive, so every
  summary row must open into its rows (§7).
- **Five shapes, no recommendation**: a system app over the marts (O1), a
  pane in chat (O2, agentic rows only), SQL through play (O3), a shared
  widget set (O4), a review ledger (O5). O1 and O5 compose, and O2 is O1
  scoped to one key. §8.3 scores them against ten criteria.
- **What every shape needs and the tree lacks**: a server-windowed table,
  a verdict glyph vocabulary, a field diff, sparkline strips, a tier→tone
  mapping, an accessibility projection for painted widgets, and a
  correlation read path that is not a scan (§8.4, §8.10).
- **Eighteen forks** for the dialogue; the first five decide the shape
  (§9).

## 1 Question and scope

The tree writes a record of what ran in it, not only of what its models
and agents did. The **trail** is a set of leeway components on the shared
facts table `boxer.facts`, one row per event, written by one recorder
([ADR-0277](../adr/0277-one-audit-trail-for-model-calls-and-agent-work.md)).
By kind: HTTP egress fetches; dataset and lading publishes; coverage
builds, functions and samples; `AuditEvent` rows in whatever domain an
emitter defines — the in-process bus writes one per request in domain
`bus` (§2.3), the attention table one per level change, and a downstream
consumer its records of processing, a case that is non-agentic by
construction
([ADR-0296](../adr/0296-audit-events-as-a-trail-component.md)); evidence
seals; and, on the rows where a model or an agent was involved, model
calls, messages, agent actions, grant events, captures and disclosures.
Every row carries `Origin` — run, app, instance, where `(run, instance)`
is a window ([ADR-0191](../adr/0191-runtime-instance-attribution.md)) —
and that is the only identity a reader may rely on finding. The three
agentic lanes, `Conversation`, `Delegation` and `Cause`, are present when
they apply and absent otherwise, and absence is a legal reading (ADR-0277
§SD1). Beside the trail on the same table, outside the recorder, sit the
runtime's own rows — process start, heartbeat, app lifecycle, launch, log
— which name run and instance but no task or call; play's query runs,
lifted from `system.query_log` with a stamp that ties each query to a run,
an instance and, when an agent asked for it, a task and a call
([ADR-0115](../adr/0115-query-observability-data-plane-strategy.md),
ADR-0277 §SD7); and the other facts-bound stores — system metrics,
watchbill worker presence, stevedore dead letters, markdown documents,
lading policy — which carry `Origin` since 2026-10-30 (ADR-0277 Updates),
so one `has()` selects every kind of a run.
The **journal** is the one record that is agentic by nature: a
per-conversation, hash-chained, append-only ledger of entries whose bodies
sit in a vault and whose heads can be anchored outside the process
([ADR-0308](../adr/0308-the-run-journal-is-the-only-source-of-model-input.md),
[ADR-0314](../adr/0314-tamper-evident-records-chain-digests-and-anchored-heads.md),
[ADR-0327](../adr/0327-evidence-as-a-record-store-profile-chain-vault-marks-seals-and-anchors-any-store-opts-into.md)).

What does not exist is a surface that shows this to a person as a *trail*:
one place to ask what happened, to what, under which run and window, on
whose behalf where a delegation is recorded, with what outcome, in what
order, and whether the record itself still holds. The chat's Analytics
panel shows this run's grants and actions from in-process rings; the
transcript shows a turn's steps inline; play's History tab shows query
runs; the log viewer shows the `log` rows of `boxer.facts` through a
cache refreshed on an interval (since 2026-10-30; empty without
`BOXER_LOG_FACTS`); the app center shows a
bounded fold of bus requests per app; capinspector a strip per capability.
Each is one app's slice of one kind.

The question has four parts, and the page follows them:

1. **Practices** — what reviewers of audit trails are required to do and
   able to do, in regulated domains and in security operations, and what
   the human-factors literature says they actually do (§3).
2. **Visualization methods** — the forms the visualization, process-mining,
   tracing, security and diff literatures have for event sequences,
   provenance, processes and changes, and which of them survive at the
   volumes a facts table reaches (§5, with the event schemas those forms
   presuppose in §4).
3. **UX paradigms** — what shipped audit logs, version histories, blame
   views, as-of databases, log explorers and agent-trace viewers converged
   on, organised by pattern rather than by product (§6).
4. **Implementation methods** — storage models, the ClickHouse patterns
   that matter for a wide facts-shaped table, diff algorithms, tamper
   evidence and what a verifier needs, rendering at scale, correlation,
   and audit reduction (§7).

§8 maps all of it onto what §2 finds in the tree: the data, the relations
an app may read, the widgets, the operations contract a system app joins,
the labels that bound what may be drawn, and the scene runner that has to
be able to assert on it.

**Reader tasks the viewer must serve.** Reading the standards (§3) and the
tree's own ADRs together, the tasks fall into a short list. None of them
names an event kind; §8 scores the candidate shapes against them:

- *Reconstruct by a correlation key* — everything that shares a run, a
  window `(run, instance)`, a watchbill job, a bus subject, a query
  fingerprint, an `AuditEvent` subject or principal, or — where the
  agentic lanes are set — a conversation or a task, in order, with what
  each row caused where a cause is recorded. For an agent's task that is a
  walk from a person's message through model calls, tool calls, dispatched
  actions and what they caused; for a bus subject it is a flat run of
  requests with outcomes and latencies; for a data subject it is every
  processing event that names the subject.
- *Account for a period or an app* — what ran, how often, by kind, subject
  and outcome, at what cost, and what changed since the reviewer last
  looked.
- *Verify the record* — whether a run's chain holds, where it breaks, which
  rows no seal names, whether heads were anchored and where.
- *Explain a single row* — the record as written, its context components,
  the bodies when kept and permitted, the label it carries, and the rows it
  joins.
- *Review by exception* — a short list produced by named predicates (a
  denied or failed outcome, a refusal, a widened grant, a clock gap, a row
  no seal names, a kind or subject not seen before), cleared with a
  recorded mark, as §3.3's doctrine asks.

Grant review — the terms as they stood after each event, the ceiling, the
refusals, the consent that admitted a consequential call — is
reconstruction by task plus review by exception, and §8.1 keeps it as the
agentic row of each.

Out of scope: designing the trail's schema (ADR-0277 and ADR-0296 own it),
the vocabulary of a downstream domain's `AuditEvent` rows (the consumer's
own ADR owns it, ADR-0296), the journal's laws (ADR-0309), the agent-facing
schema
([ADR-0313](../adr/0313-agent-schema-generated-relations-an-agent-may-read-in-full.md)),
and any export or anchoring format. The viewer is for a person; what a
model may read of the same data is settled elsewhere and §8.7 only notes
where the two constrain each other.

## 2 What the tree has, and where it stops (2026-10-30)

Paths are repository-relative links. Component, column and relation names
are the tree's own; the field lists are not reproduced — the DTO files and
the generated store serve them.

### 2.1 The trail: components on `boxer.facts`

The trail is one generated record store
([trail_store.out.go](../../public/keelson/runtime/trail/trail_store.out.go))
over `boxer.facts`, whose DDL another package owns
([facts-bound-record-stores](../explanation/facts-bound-record-stores.md)).
One writer, `trail.Recorder`, composes every row from **context components**
plus **one domain component** ([doc.go](../../public/keelson/runtime/trail/doc.go);
ADR-0277 §SD1–SD2):

| Context component | Names | Who vouches |
|---|---|---|
| `Origin` (alias of `audit.Origin`) | `Run`, `App`, `Instance` — `(Run, Instance)` is a window, 0 unattributed ([ADR-0191](../adr/0191-runtime-instance-attribution.md)) | the host, except on `Event` where it is the writer's claim weighed by `OriginBy` ([ADR-0296](../adr/0296-audit-events-as-a-trail-component.md) §SD1) |
| `Conversation` | `Conversation`, `Turn`, `Round` | the owning app |
| `Delegation` | `Task`, `Epoch`, `Call` — caused work joins on `(Task, Call)` | the dispatcher ([ADR-0269](../adr/0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) §SD6) |
| `Cause` | `ModelCall`, `ToolCall`, `ToolIndex` | the coordinator; not checked by the dispatcher (ADR-0277 §SD6) |

`Origin` is on every row; the others when they apply, and absence is a legal
reading. The domain components, one per row, each carrying a `Kind` whose
membership id is the row filter: `LlmCall`, `LlmMessage`, `LlmMessageBody`
(only under `BOXER_LLM_RETAIN`, ditchable — ADR-0277 §SD4), `AgentAction`,
`AgentGrant`, `AgentCapture`, `AgentDisclosure`, `HttpFetch`,
`AdhocDataset`, `LadingPublish`, the four `Coverage*` components
([ADR-0343](../adr/0343-coverage-on-the-trail-what-an-agents-call-executed.md),
proposed), `AuditEvent` (alias into the evidence package —
[auditevent_dto.go](../../public/storage/recordstore/evidence/audit/auditevent_dto.go))
and `EvidenceSeal`. Two facts about the domain set matter for a viewer:

- **What is on the row and what is not.** `AgentAction` carries the
  operation, the effect, an `ArgsDigest` and the model's one-line
  `CallTitle`/`CallReason` — never the arguments and never the result.
  `AgentGrant` carries the grant's `Entries`, `Launches`, `Destinations`,
  budget and deadline "as they stood after the event", so a widening is
  read by comparing consecutive rows, not from a before/after pair.
  `AuditEvent` carries `Outcome` (`ok`, `denied`, `failed`, `timeout`,
  `error`), a pseudonymous
  `Principal` with `PrincipalBy`/`PrincipalVia`, a `Subject`, and bounded
  `Ref*`/`Attr*` arrays. No component carries an old value and a new
  value of a record it changed; the closest are `AgentGrant`'s successive
  states and `AdhocDataset`'s `Revision`.
- **What a reader can reconstruct.** Parent/child: `Cause.ModelCall` →
  `LlmCall.CallId`; `LlmCall.Parent` chains continued calls
  ([ADR-0264](../adr/0264-retained-model-conversations-on-facts.md));
  `LlmMessage.ToolCallId` answers a `ToolCallIds[]` entry; `Cause.ToolIndex`
  orders the tool calls of one reply; `Delegation.(Task, Call)` joins an
  action to everything it caused. Grants: `AgentGrant.Request` joins a
  request to its decision, `DecidedBy` says who. Screenshots:
  `AgentCapture.Digest` → `AgentDisclosure.RootDigest` →
  `LlmMessage.Images`
  ([ADR-0281](../adr/0281-window-captures-through-one-policy-enforcement-point.md),
  [ADR-0287](../adr/0287-chat-pixels-a-setting-for-showing-screenshots-to-the-model.md)).
  Conversation→child-conversation delegation is **not** recorded: the
  journal reserves a `parent` column and the trail a delegation component,
  and nothing consumes them ([agentic-layer](../explanation/agentic-layer.md),
  "What is deliberately not there").

**Where it lands and how it is read.** Backbone columns `id` (on trail rows
a hash of the natural key, not a sequence) and `ts` DateTime64(9, UTC).
The table `SetupTable` creates by default is sorted on `ts` alone
(`chstore.defaultEngineClause`; chstore is the sole DDL author, ADR-0184
§SD2); the trail's own golden
([facts_ddl_clickhouse.out.sql](../../public/keelson/runtime/trail/facts_ddl_clickhouse.out.sql))
sorts `(id, ts)`; a deployed table keeps whatever clause it was created
with. Neither key names a correlation value.
Reads are `Scan<Kind>(ctx, ScanOpts)` per component, with a `ReadTable`
option (ADR-0296 §SD3, §SD8). The hand-written reads
([reads.go](../../public/keelson/runtime/trail/reads.go)) narrow by a `ts`
range plus `has(<section column>, <literal>)` and match the slot exactly in
Go after decoding (ADR-0296 §SD5). No skip index exists; a read by
conversation or task scans (ADR-0277 §SD10; ADR-0296 §SD5 "No index is
added").

**The data marts.** [trailviews](../../public/keelson/runtime/trail/trailviews/trailviews.go)
composes read-only ClickHouse views (versioned by `ViewsVersion`): one
`dm_trail_*` mart per kind, a unified **`dm_trail_timeline`** (UNION ALL of
kinds, carrying the context columns and computed `ref`, `seq`,
`parent-ref`, `status`, `subject`, `headline`, `text`, `confined`,
`tainted`, token and elapsed columns, ordered `(ts, seq)`), and three
aggregates `agg_trail_llm_agent_action_outcomes` (one row per dispatcher
key, dispatch and final folded), `agg_trail_llm_conversations` and
`agg_trail_llm_tasks`. The views are leeway-shaped so play reads them by
handle ([ADR-0171](../adr/0171-leeway-sql-read-surface.md),
[ADR-0181](../adr/0181-leeway-dql-authoring-surface.md)). The package doc
states the limits: no arguments, no operation results, text only when kept;
`confined`/`tainted` passed through, not enforced.

**Posture.** Trail rows do not depend on `BOXER_LLM_RETAIN`; messages are
flushed before the request leaves, so a message row without a call row
reads as "sent, outcome unknown"; `BOXER_TRAIL_REQUIRED` refuses work whose
row cannot land first; loss is accounted by `Recorder.Counts()` and by
`audit-gap` rows the recorder writes about itself (ADR-0277 §SD3; ADR-0296
§SD4). `EvidenceSeal` rows chain per writer run and
`Recorder.VerifySeals(ctx, run)` returns a break, the unnamed rows and the
unanchored seals ([seals.go](../../public/keelson/runtime/trail/seals.go)).

### 2.2 The journal

A generated store over three tables — the run row (compare-and-set
rewritten), the append-only entry ledger `ORDER BY (id, ordinal,
revision)`, and the vault
([agentrunstore](../../public/keelson/runtime/agentrun/agentrunstore)) —
built over the same facts data sections with an envelope of run id, ordinal
and revision (ADR-0308 §SD1 and Updates). Entry kinds are a closed,
golden-pinned enum ([kinds.go](../../public/keelson/runtime/agentrun/kinds.go)):
`user`, `request`, `reply`, `intent`, `outcome`, `note`, `section`,
`tools`, `compact`, `edit`, `profile`, `turn`, `asked`, `answered`,
`candidate`, `verdict`, `select`, `completed`, `endorse`, `approval`,
`head`, `copy`, `issued`, `deleted`, `ignorable`, `erased`. `Entry.Key`
correlates request↔reply and intent↔outcome
([entry.go](../../public/keelson/runtime/agentrun/entry.go)); `Prev` and
`Digest` carry the chain; `Parent` is reserved and zero; `WrittenBy`
records the writer's run, app, instance, origin-by, principal and purpose
(ADR-0327 §SD6). Bodies live in the vault under a per-entry opening, the
ledger holds a commitment per component, and the chain commits to the
commitments
([ADR-0320](../adr/0320-journal-bodies-in-a-vault-committed-to-the-chain-under-a-per-entry-key.md)).
Deleting a run appends `deleted` and destroys its vault rows; erasing an
entry appends `erased` and destroys one; in the evidence profile these are
*marks* ([mark.go](../../public/storage/recordstore/evidence/mark.go)), and
`Entry.Tombstoned()` is "commitments standing, no openings".

**The verifier vocabulary** is the one part of §2 a viewer must reproduce
exactly, because it is what the person is told:

- `agentrun.Verify` / `VerifyRun` → the first ordinal whose digest fails,
  0 when the chain holds (law L31).
- `evidence.Verify(scheme, seed, rows) → (broken, why BreakE)` with `why`
  one of `BreakNone`, `BreakForm`, `BreakPrev`, `BreakDigest`,
  `BreakCommitment`, `BreakUnexplained`, `BreakAfterDeleted`,
  `BreakOrdinal` ([verify.go](../../public/storage/recordstore/evidence/verify.go)).
- `evidence.VerifyHeads` / `headstore.VerifyHeads` → heads that do not
  match anchors (L32).
- `evidence.VerifySeals(scheme, seed, seals, present) → (broken SealBreak,
  unnamed)` with `SealBreakE` ∈ {`None`, `Order` (a fork: two seals follow
  one), `Prev` (follows no seal), `Digest`, `RowMissing`, `RowDigest`,
  `RowTwice`}; `unnamed` rows are a **gap** the store reports, not a break
  ([seal.go](../../public/storage/recordstore/evidence/seal.go); L34).
- A `head` entry with form `local` is "recorded and anchoring nothing"
  (ADR-0314 §SD2).

So the reportable states are **holds**; **broken at ordinal N, for reason
R**; **gap** (rows no seal names); **fork** (`SealBreakOrder`);
**unanchored**. They are served in-process as `keelson('evidence_checks')`
— columns `store`, `scope`, `law`, `holds`, `broken`, `why`
([evidence_checks.go](../../public/keelson/runtime/introspect/introspecthost/evidence_checks.go);
ADR-0327 §SD7 and Update) — and anchors as `keelson('anchors')`
([headstore/introspect.go](../../public/keelson/runtime/headstore/introspect.go)),
with a durable anchor store behind `BOXER_ANCHOR_STORE_URL`
([anchorsql](../../public/storage/recordstore/evidence/anchor/anchorsql)).
The journal laws as SQL over `dm_journal_entries` run in the integration
lane only ([ADR-0309](../adr/0309-context-projection-as-sql-over-the-journal-in-a-local-worker.md)
Update 2026-10-25).

### 2.3 The older `audit` kind and play's query stamps

Until 2026-10-30 one row per in-process bus request was written by a
hand-written verb as a kind of its own
([ADR-0026](../adr/0026-app-runtime-and-capability-subjects.md) §SD12). On
that date it was migrated onto `AuditEvent` (ADR-0296 Updates): the bus
sink is `trail.AsAuditSink`
([bussink.go](../../public/keelson/runtime/trail/bussink.go)), the row is
domain `bus`, action `request`, the bus subject as a reference, latency
and sizes as attributes, under the requester's origin with no principal,
and the outcome from the result — `ok`, `denied`, `timeout`, `error`, and
`failed` for any other result. The rows sit in the one audit-event mart `dm_trail_audit_events`
and reach the timeline as kind `auditEvent`; the separate mart of the
2026-10-13 Update is withdrawn, and the fold
`keelson('app_audit')` the app center reads is unchanged. Rows of the
retired kind are not migrated and no longer read; its vocabulary entry
stays, written by nothing. The other hand-written
kinds on the same table (`RuntimeStartRow`, `HeartbeatRow`,
`AppLifecycleRow`, `LaunchRow`, `LogRow`) name run and instance but **not
task or call** (ADR-0277 §SD10 "Known gaps"); `RunEventRow` is a view over
them, served as `keelson('runtime_events')` for the current run only
(ADR-0191 §SD7).

Every query play runs carries a `log_comment` stamp with run, app, instance,
four fingerprints, lane, and — under an `app.OnBehalfOf` — task, epoch and
call ([play_stamp.go](../../apps/play/play_stamp.go)); an optional daemon
lifts `system.query_log` into `boxer.facts` as kind `QueryRun`
([queryrunfacts.go](../../public/keelson/runtime/queryrunfacts/queryrunfacts.go)),
read back as `HistoryRow` with a `Delegated()` predicate and filterable by
run or task ([query-observability](../explanation/query-observability.md)).

### 2.4 Relations an app can read; labels and attention

Introspection relations are argument-less snapshots
([ADR-0094](../adr/0094-keelson-introspection-tables.md)). The ones a viewer
would lean on: `agent_grants` (**this run's** grants only — history is on
the trail; [records.go](../../public/keelson/runtime/agent/records.go)),
`agent_actions` (a **bounded ring**,
[ADR-0283](../adr/0283-chat-analytics-the-agent-surface-and-what-was-granted-and-used.md)
Consequences), `app_operations`, `windows`, `apps`, `build`,
`runtime_events`, `app_runs`/`app_logs`/`app_audit` (bounded look-back,
[ADR-0260](../adr/0260-app-center-one-page-per-app.md) §SD5), `llm_calls`
([llm/subjects.go](../../public/keelson/runtime/llm/subjects.go)),
`http_calls`/`http_destinations`
([httpegress/subjects.go](../../public/keelson/runtime/httpegress/subjects.go)),
`agent_runs` and `agent_run_entries` ("a snapshot bounded to the newest
runs", ADR-0308 §SD10;
[agentrun/introspect.go](../../public/keelson/runtime/agentrun/introspect.go)),
`agent_verdicts` ([ADR-0340](../adr/0340-cited-replies-assertions-the-window-checks-and-verdicts-in-the-journal.md)),
`anchors`, `evidence_checks`, `attention_queue`
([attention/introspect.go](../../public/keelson/runtime/attention/introspect.go)),
`clipboard_copies`, `watchbill_event`. The agent schema proper (`my_run`,
`my_conversation`, the content-free `agent_history`) is what a *model* may
read; its DDL is generated and, on the compile date, "no host executes it
yet" (ADR-0313 §SD1, §SD8;
[agentschema](../../public/keelson/runtime/agentschema)).

**Labels.** Journal entries and run rows carry `label_level` (`person`,
`system`, `external`, `own-model`, `other-model`), `label_tier` (`CLEAR` <
`GREEN` < `AMBER` < `AMBER+STRICT` < `RED`), `label_exposure[]` and
`label_origin[]` ([label.go](../../public/keelson/runtime/agentrun/label.go);
[ADR-0311](../adr/0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md)
§SD1). Trail rows carry a *tier* and a sensitivity, not the full label
(`LlmCall.Tier`, `HttpFetch.Tier`, `LadingPublish.Tier` + `Confined`,
`AgentAction.Tainted`/`Confined`, `AgentDisclosure.LocalOnly`); marks on
trail rows are deferred (ADR-0311 §SD8), and marks are "neither shown to the
person nor read by any guard by default" (§SD7). **Attention levels**
([ADR-0323](../adr/0323-attention-levels-one-table-two-scales-configurable-within-a-floor.md))
— `data`/`message`/`highlight` and `your turn`/`critical`
([levels.go](../../public/keelson/runtime/attention/levels.go)) — are not a
column on any trail or journal row; they are derived from kind and effect
by the host's table, appear live in `attention_queue.level`, and a person's
changes to them land as `AuditEvent` rows in domain `attention`
([attention/audit.go](../../public/keelson/runtime/attention/audit.go)).

### 2.5 Time and ordering

The trail has one time column, `ts`, meaning "when the recorded thing
began"; elapsed is a field (ADR-0277 §SD9). There is **no durable, commit or
insertion time**: the `MATERIALIZED now64()` column is deferred with the
storage-table switch (ADR-0296 §SD9(c)), which is why the backstop
forwarder is a lagged re-scan (§SD6). Order within a run is `ts`, then
`LlmMessage.Ordinal` (the timeline's `seq`), `Conversation.Round`,
`Cause.ToolIndex`; `AgentAction.Decision` (`dispatch` then `final`) and
`AgentGrant.Request` order per key. The journal has `At` ("when written, for
the person; the ordinal is the order"), a contiguous `Ordinal` per run from
a single writer, `Revision` and `Prev` (ADR-0308 §SD9). Relations expose
time as Int64 milliseconds or ISO strings; query runs carry `event_us` from
`system.query_log` with no receive time. Everything is stored UTC; the time
range picker evaluates expressions against a `clickhouse-local` worker with
an IANA zone on the wire
([ADR-0016](../adr/0016-imzero2-time-range-picker.md)).

### 2.6 Surfaces that already show some of it

- **Chat Analytics** (ADR-0283; [chat_stats.go](../../apps/chat/chat_stats.go)):
  the agent surface as a status-grouped list or a `graphview` radial graph
  (conversation at centre, hubs per window, leaves per (window, operation),
  colour by `above-ceiling`/`not-granted`/`observe-only`/`granted`, donuts
  of call outcomes, edge width by call count), token statistics as `implot`
  ECDFs, and *Open in play* publishing `chat_turns`, `chat_calls`,
  `chat_surface` as ad-hoc datasets
  ([ADR-0134](../adr/0134-adhoc-datasets.md)). It reads `Describe`,
  `keelson('windows')`, `keelson('agent_grants')`, `keelson('agent_actions')`
  — in-process rings; reading the trail "is deferred" (ADR-0283
  Consequences). The graph "is not in the accessibility tree; scenes assert
  on the table". A conversation reopened in a later run "shows its calls
  and no grant" (Update 2026-10-08).
- **Transcript steps** ([chat_steps_render.go](../../apps/chat/chat_steps_render.go)):
  a tool entry is one italic line prefixed by its attention mark
  ([chat_levels.go](../../apps/chat/chat_levels.go)), with a collapsing
  *Details* listing the turn's steps — model round, tool title and
  operation, elapsed, in/out tokens, `refused`/`running`/`waited N s for
  you` — and a per-step pane of bounded, selectable arguments, result and
  reasoning. Approvals, grant terms, questions and endorsements render as
  journaled asks. No journal viewer or run inspector exists in chat; past
  conversations are read in play over SQL
  ([play_chat_panel.go](../../apps/play/play_chat_panel.go),
  [ADR-0239](../adr/0239-play-chat-panel-and-chatview-widget.md)).
- **The log viewer** ([ADR-0345](../adr/0345-system-apps-join-the-operations-contract-the-log-viewer-first.md);
  [logviewer.go](../../public/keelson/runtime/logviewer/logviewer.go)):
  counters, a filter row (level floor, app and message substrings), a
  virtualised egui_table tail, a detail pane of `fieldview` fields and an
  `errorview` chain. Since 2026-10-30 it reads the `log` rows of
  `boxer.facts` through a bounded per-window cache that a background job
  refreshes on an interval, on Refresh and after a filter change
  (ADR-0345 Updates); the in-process ring is gone, and a host without
  `BOXER_LOG_FACTS` sees one line saying logs are not persisted. Its catalog is the
  template a system app follows: queries `list_rows` (marked `Untrusted`),
  `get_row`, `get_counters`, `get_view`; commands of effect `view`
  (`set_filter`, `set_follow`, `select_row`); resources `filter`,
  `selection`, `follow`
  ([logviewer_ops.go](../../public/keelson/runtime/logviewer/logviewer_ops.go)).
- **play History** ([play_runs_history.go](../../apps/play/play_runs_history.go)):
  this window's session ring plus "Recorded runs" from `queryrunfacts`,
  with inline inspection (outcome, security class, confinement and
  **agent** badges; accounting grid; highlighted SQL; restore/copy/open).
  Fetching is "manual-plus-on-reveal".
- **Others, one line each.** `capinspector` paints a private 30-bucket
  sparkline strip of bus-audit records per capability
  ([audit_sparkline.go](../../apps/capinspector/audit_sparkline.go)) — the
  only activity-over-time strip in the tree, and app-private. `watchbill`
  shows one job's trail from `keelson('watchbill_event')`
  ([watchbill.go](../../apps/watchbill/watchbill.go)). `tally` shows a
  file's snapshot history and a two-pane diff as a string table
  ([tally.go](../../apps/tally/tally.go)). `agentconsole` keeps a bounded
  in-memory call log and declares no catalog
  ([agentconsole.go](../../apps/agentconsole/agentconsole.go)).
- **Scenes** ([ADR-0248](../adr/0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md)):
  [chat-analytics.scene.md](../../apps/chat/scenes/chat-analytics.scene.md)
  and [chat-statistics.scene.md](../../apps/chat/scenes/chat-statistics.scene.md)
  assert on the table, not the graph. No scene launches the log viewer
  (2026-10-30). Scenes read the accessibility tree; anything painted on a
  canvas — `graphview`, `timeline`, `implot` — is invisible to `read` and
  `expect`.

### 2.7 Widgets

Shapes per [ADR-0267](../adr/0267-imzero2-go-widget-api-contract-immediate-and-semi-retained.md):
**IM** immediate-mode, **SR** semi-retained, **F** fluid, **P** painter
helper. Where a package doc does not state the shape it is *(inference)*.

| Widget | Shape | Input | Scale | Interaction |
|---|---|---|---|---|
| [recordtable](../../public/thestack/imzero2/egui2/widgets/recordtable) | IM | Arrow record + `SortState`/`GroupState` ([ADR-0336](../adr/0336-recordtable-row-grouping-a-line-sequence-over-the-permutation.md)) | virtualised over the held record; cells replay under `CellsKey` | sort, full-cell select, group expand, `Reveal`; widths persist ([ADR-0151](../adr/0151-table-column-width-overrides.md)) |
| [timeline](../../public/thestack/imzero2/egui2/widgets/timeline) | SR | interval/point/annotation slices, epoch ms ([ADR-0043](../adr/0043-imzero2-timeline-widget.md)) | painted; greedy lane packing; lanes are packing-derived, not caller-assigned *(inference)* | pan, select (interval/bucket/annotation/lane), brush; events lag a frame |
| [timescrubber](../../public/thestack/imzero2/egui2/widgets/timescrubber) | SR | stepped series ([ADR-0251](../adr/0251-a-time-strip-for-stepped-series.md)) | painted | scrub, playhead, `Settled` |
| timerangepicker | F-ish *(inference)* | `TimeRange` → epoch ms (ADR-0016) | — | presets, tz, apply; auto-refresh interval exposed, no runner |
| [badge](../../public/thestack/imzero2/egui2/widgets/badge) | F | label + `Tone` | — | click (filter chip), hover |
| [bandscale](../../public/thestack/imzero2/egui2/widgets/bandscale) | IM | bands + markers ([ADR-0280](../adr/0280-a-ceiling-on-what-a-chats-model-may-do-scored-on-a-ladder.md)) | painted inline | read-only |
| [graphview](../../public/thestack/imzero2/egui2/widgets/graphview) | SR | nodes/edges per frame ([ADR-0224](../adr/0224-graphview-go-graph-widget-painter-lane.md)) | painted; retains layout | select, drag, zoom, legend; **not in the accessibility tree** |
| [layeredgraph](../../public/thestack/imzero2/egui2/widgets/layeredgraph) | layout + view | DAG/FSM ([ADR-0069](../adr/0069-imzero2-layeredgraph-widget.md)) | painted, host-side layout | colour hooks, click |
| [sankey](../../public/thestack/imzero2/egui2/widgets/sankey) | layout + implot items | staged flow model ([ADR-0159](../adr/0159-imzero2-sankey-flow-widget.md)) | painted | implot hover/zoom *(inference)* |
| [icicle](../../public/thestack/imzero2/egui2/widgets/icicle) | layout + implot items | tree ([ADR-0160](../adr/0160-imzero2-icicle-flamegraph-widget.md)) | painted | hit tests |
| [tree](../../public/thestack/imzero2/egui2/widgets/tree) | IM | columnar tree + `State` ([ADR-0176](../adr/0176-native-tree-widget.md)) | flattened per frame | expand, select, keys, reveal |
| [heatmapscroll](../../public/thestack/imzero2/egui2/widgets/heatmapscroll) | SR | `PushColumn` per step | GPU texture ring | hover *(inference)* |
| [implot](../../public/thestack/imzero2/egui2/widgets/implot) | plot handle | float64 slices ([ADR-0149](../adr/0149-implot-core-port-painter-lane.md)) | painted | pan, zoom, box, legend, tags |
| [codeview](../../public/thestack/imzero2/egui2/widgets/codeview) | retained builders | SQL/JSON/Go/markdown text | memo bounded by bytes | selectable text; **no diff mode** |
| [fieldview](../../public/thestack/imzero2/egui2/widgets/fieldview) | IM | typed key/value tree | collapsible | expand |
| [componentview](../../public/thestack/imzero2/egui2/widgets/componentview) | IM | decoded leeway components ([ADR-0075](../adr/0075-leeway-typed-component-views.md)) | one record | fold panels |
| [errorview](../../public/thestack/imzero2/egui2/widgets/errorview) | IM | error chain | — | fold |
| [chatview](../../public/thestack/imzero2/egui2/widgets/chatview) | IM/SR | columnar transcript model (ADR-0239) | tail window | select, follow, jump |
| [legend](../../public/thestack/imzero2/egui2/widgets/legend) | P | items | painted | toggle hidden |
| [pager](../../public/thestack/imzero2/egui2/widgets/pager) | SR | a total count | client-side | page, jump |

**What is missing for a trail viewer (2026-10-30):** (1) a swimlane/Gantt
with caller-fixed lanes and parent→child connectors — a span tree; the
timeline's lanes are packing-derived; (2) any text or record diff widget —
line diffs exist only as app code
([chat_artefact_diff.go](../../apps/chat/chat_artefact_diff.go), tally); (3) a
sparkline or small-multiples strip — `gauge`'s doc says it is "not
progress-over-time (a sparkline)"; (4) a calendar heatmap; (5) a
server-paged, windowed table — `recordtable` virtualises what is in the
record, `pager` pages client-side, and nothing under `widgets/` issues a
windowed query; (6) a chain/seal verdict glyph vocabulary — `badge` tones
can carry one, none is defined; (7) a tier→tone mapping for labels; (8)
accessibility exposure of painted canvases.

### 2.8 Data access and constraints a viewer inherits

An app reaches ClickHouse only through bus subjects, stamped (ADR-0026
§SD3; §2.3). Reading a leeway table from SQL is the solved problem of
[leeway-sql-read-surface](../explanation/leeway-sql-read-surface.md), and
the trail views are authored with it. In-process registries serve
`keelson('…')` snapshots with no arguments; ADR-0191 §SD7 chose a Go
provider over SQL for the run-event timeline because the pass pipeline
re-parsed a 7 KB statement many times per run. A table an app computed goes
to SQL as an **ad-hoc dataset** ([adhoc-datasets](../howto/adhoc-datasets.md)),
and every publish is itself a trail row. The established paged pattern is
play's Table pane feeding `recordtable` from a result record with `pager`
client-side ([play_table_panel.go](../../apps/play/play_table_panel.go));
there is no library pattern for a server-windowed time-range query that
refills a grid as the user scrolls, though the trail's reads already take
`(from, to, limit)`. Fetches off the render thread go through `bgjob`;
imzero2 `Fetcher.Fetch*` opcodes run only from `StateManager.Sync()` at
frame end ([imzero2-fetchers](../skills/imzero2-fetchers/SKILL.md)).

Constraints: colour tokens are six semantic roles × three emphases in
OKLCh, low chroma, and colour is never the only channel
([ADR-0031](../adr/0031-imzero2-design-system-color.md);
[palette.out.go](../../public/keelson/designsystem/styletokens/palette.out.go));
data encodings use published scientific colormaps. Every IM/SR widget opens
one id scope; a mismatched stack panics at render (ADR-0267;
[conformance](../../public/thestack/imzero2/egui2/widgets/conformance)). A
system app declares **queries over what it shows** and **commands only over
its view** — "a catalog carries only what no `keelson()` table carries"
(ADR-0345 §SD1–SD2); a viewer that called other windows' operations would be
a second dispatcher path. The integrity floor for consequential acts is
L15 (ADR-0311 §SD3); kept text exists only under `BOXER_LLM_RETAIN` and the
app's grant; sensitive values stay the person's click (ADR-0345 §SD4); a log
line has no label of its own (§SD5). Committed screenshots and fixtures
obey the repository's privacy rule.

### 2.9 Gaps the ADRs themselves record

| Where | What a viewer would resolve or depend on |
|---|---|
| ADR-0283 Consequences, Update 2026-10-08 | Analytics reads rings; the trail "keeps them, and reading it is deferred"; the graph is painted so a scene cannot read it; a reopened conversation shows calls and no grant |
| ADR-0277 §SD10 | no correlation index; log rows do not name task or call; no turn-outcome row; `Cause` not host-verified (the other facts-bound stores adopted `Origin` 2026-10-30, Updates) |
| ADR-0296 §SD5, §SD9, Deferred | skip index for reads by subject; the Null-table switch with storage tables, derived sort columns and an insertion-order column (the bus `audit` kind migration closed 2026-10-30, Updates) |
| ADR-0191 Deferred | reading an earlier run's trail — introspection takes no arguments; raw SQL is the route |
| ADR-0308 §SD1, §SD11; ADR-0309 §SD8 | `parent` reserved and unused; branch-aware projection deferred; no server-side plain-shaped view over the journal |
| ADR-0313 §SD8 | agent-schema DDL generated, not executed |
| ADR-0311 §SD8 | marks not shown; marks on trail rows deferred |
| ADR-0314 §SD7; ADR-0327 §SD11 | per-entry signatures; a public anchor store; the generic chain-head row |
| [ADR-0330](../adr/0330-a-shared-audit-recorder-below-the-runtime-one-sealed-writer-over-a-narrow-store-binding.md) (accepted 2026-10-30, M3 landed the same day) | the trail's buffer, seals, reads and forwarding are `auditrec`'s over a trail binding; a stored-twice batch no longer verifies as a fork; the host flushes the trail every second |
| ADR-0343 | coverage views exist, no derived view installed; a **driver stamp on the run row** (person / scene / headless driver / coordinator) deferred — the one field that tells a scene run from a person's |
| ADR-0345 Alternatives | `log_tail` under the agent schema "deferred, not rejected" |
| ADR-0323 §SD8 | highlight words, moderator-set levels, per-level effects deferred |
| [agent-harness](./agent-harness.md) §2.7 S15/S16, §6 | S16 "Presentation" is "`apps/chat` render code, which also writes coordinator state"; open decisions #10 branching, #28 what "Keep off" means once the journal is always written, #30 anchoring cadence and what a regulator needs to check without the cell |
| ADR-0016 | the picker exposes an auto-refresh interval and ships no runner |
| ADR-0119 §SD5, ADR-0224 | painted widgets outside the accessibility tree |

## 3 What reviewers need, and what the rules require

The primary case here is the regulated, non-agentic one: a
records-of-processing trail (ADR-0296's consumer), GxP audit-trail review,
an access log. §3.1–§3.4 are read from it and hold for every kind on the
table. §3.5 adds what oversight of an agent asks for beyond that, and it
bears only on the rows that carry `Delegation` and `Cause`.

The web passes were briefed before the scope widened. The operations side
— SIEM triage, change-audit and access-log review as practised — is
represented here by the SOC studies of §3.4 and the product pages of §6,
not by a pass of its own. A further pass there would add to §3, §5 and §6;
§8 and §9 were built from the tree's data and are not expected to move.

### 3.1 The content tuple

The regulated corpora fix the per-row content, and they do so twice over
with different emphasis. The GxP texts want **who, what with old and new
value, when with zone, why, and who authorised**: PIC/S PI 041-1 §9.6 lists
"details of the user that undertook the action; - what action occurred,
was changed, incl. old and new values; - when the action was taken, incl.
date and time ; - why the action was taken (reason); and - in the case of
changes or modifications to data, the name of any person authorising the
change"; the 2025 Annex 11 consultation draft §12.2 adds "the user's role,
if users may have more than one role" and that "systems should
automatically prompt the user for, and register the reason, why the change
was made" (a draft; its wording may change). The security frameworks want
**event type, when, where, source, outcome, and the identities of subjects
and objects** — NIST SP 800-53 AU-3 and PCI DSS 10.2.2 are near-identical
on this — and add two things the GxP tuple lacks, *outcome* and *origin*,
while lacking *old/new* and *reason* *(inference)*. The AI Act gives a
content floor only for biometric systems (Art. 12(3): period of use with
start and end, the reference database, matched input, "the identification
of the natural persons involved in the verification of the results"), a
retention floor of "at least six months" (Art. 19(1), 26(6)), and a purpose
test: logs must let a deployer monitor operation and detect "anomalies,
dysfunctions and unexpected performance" (Art. 14(4)(a)).

Against this tuple the tree's components stand as follows *(inference from
§2.1)*: who — `Origin` + `AuditEvent.Principal`/`PrincipalBy`, and for
agent rows the delegation chain `Delegation` → `Cause` → `LlmCall`; what —
`Kind` + `Operation`/`Action`; when — `ts` (begin), no commit time; why —
`CallTitle`/`CallReason` (the model's claim) and `Reason[]`, no
person-entered reason; old/new — absent except by consecutive `AgentGrant`
rows; outcome — `Outcome`, `Decision`/`Phase`, `Refused`, `Status`;
authority — `AgentGrant.DecidedBy`, `LlmCall.Approval` (a foreign key into
the journal), `Consent[]`; on-behalf-of — `Delegation.Task` + the grant's
`actor`. The two columns the regulated tuple names that no component
carries are a before/after pair and a person-supplied reason.

### 3.2 Reviewability as a requirement

Several texts make the *form* of the trail a requirement, not a nicety.
Annex 11 (2011) §9: "Audit trails need to be available and convertible to a
generally intelligible form and regularly reviewed." PIC/S §9.6: the trail
"should be available in a meaningful format" and "If possible, the audit
trail should retain the dynamic functionalities found in the computerised
system, (e.g. search functionality and ability to export data such as to a
spreadsheet)." The 2025 draft is the first to say it outright — §12.4 "It
should be possible for all users to sort and search audit trail data (who,
what, when and why) in the system, or alternatively, to allow export of the
data to a tool where this is possible", and §12.9 "Flat and locked files
are not acceptable, it should be possible to search and sort data." NIST
AU-7 names the presentation layer directly: audit reduction and report
generation must support "on-demand audit record review, analysis, and
reporting requirements and after-the-fact investigations of incidents" and
"Does not alter the original content or time ordering of audit records";
AU-7(1) asks for the ability "to process, sort, and search audit records
for events of interest". 21 CFR 11.10(b) wants copies "in both human
readable and electronic form suitable for inspection, review, and copying".

The practitioner literature says why these clauses exist. The ISPE
Pharmaceutical Engineering article of March/April 2026 reports systems
"generate thousands of entries per month across fragmented files, making
meaningful manual review impossible", that "Printouts, PDF exports, scanned
logs, and Excel-based review are error-prone and break the link between
audit trail entries and their records", and lists the features a review
tool needs: "Filtering and keyword search; Flagging of changes; Clear
timestamps; Clear linkage to the associated data record; Electronic
documentation of review". NIST SP 800-92 (2006) §5.2.1 names the two
reasons entries are hard to read — "Need for Context. The meaning of an
entry often depends upon the context surrounding it" and "Unclear
Messages" — and recommends "two reports: one for the entries thought to be
most important, and another for entries that are not yet fully understood."

### 3.3 Review doctrine

Three rules recur. **Review with the record**, not as a separate exercise:
FDA's 2018 Q&A, Q7 — "Audit trail review is similar to assessing cross-outs
on paper when reviewing data. Personnel responsible for record review
under CGMP should review the audit trails that capture changes to data
associated with the record as they review the rest of the record". **At the
record's frequency**: Q8 — "If the review frequency for the data is
specified in CGMP regulations, adhere to that frequency for the audit
trail review", otherwise risk-based. **By exception where justified**:
MHRA 2018 §6.13 — "Audit trails may be reviewed as a list of relevant data,
or by an 'exception reporting' process. An exception report is a validated
search tool that identifies and documents predetermined 'abnormal' data or
actions"; and the same section bounds scope — "It is not necessary for
audit trail review to include every system activity (e.g. user log on/off,
keystrokes etc.)". PIC/S §9.8 wants reviews "both random (selected based on
chance) and targeted (selected based on criticality or risk)" and
"documented and recorded". Every GxP text also requires that tampering
with the trail — disabling it, editing its settings, changing system time —
itself produce an entry (Annex 11 draft §12.3; MHRA §6.13; PIC/S §9.6), and
PCI DSS 10.2.1.3 and 10.2.1.6 log access to the logs and their starting,
stopping or pausing. The financial-audit standard adds the model for
*evidence of review*: PCAOB AS 1215 wants workpapers that record who
performed and who reviewed each item and when, locked after a deadline,
with later additions dated and reasoned *(snippet — the PCAOB pages
returned 403)*.

The warning letters the passes read show what reviewers are expected to
find: deleted results and sequences ("you deleted more than 100 test
results"; "The audit trail showed that the final standard injection was
permanently deleted"), aborted runs without investigation, repeat testing
until passing, clock manipulation ("analysts admitted to setting the clock
back"), disabled trails, shared administrator accounts "without
traceability". These are per-record exception predicates, not a reading
of the whole log *(inference)*.

### 3.4 What the human-factors studies say the bottleneck is

Across SOC ethnographies the reviewer's cost is **context acquisition, not
reading**. Alahmadi, Axon and Martinovic (USENIX Security 2022; 20 surveyed
and 21 interviewed practitioners) found validation "comes down to knowing
your environment", that "Oftentimes an alarm may result from a benign
change that occurred in the network/system. In such cases, the analyst
needs to be aware of these changes to avoid tedious false alarm triaging",
and that opaque alarms breed desensitisation — "if you don't tell me the
reason you fire ... I can't have really high respect on that." Kokulu et al.
(CCS 2019) report the most common issue as "Low visibility on devices and
network topology" and that "Fifteen participants said false positives were
not a major issue." Sundaramurthy et al. (SOUPS 2015) list "Inadequate
context — Low efficiency due to contextual information surrounding an
alert" among burnout causes. Garneau et al. (LASER 2016; 24 analysts) found
analysts "performed well on the tabular baseline and preferred it to the
other displays", with a node-link alternative "slightly faster and
similarly accurate"; Staheli et al. (VizSec 2014; 130 papers) found "no
users are recruited at all (46%)" of evaluations. The healthcare
access-audit literature reframes review as *explain-then-review*: Fabbri
and LeFevre (VLDB 2011) "can provide explanations for over 94% of data
accesses in the log" from data already in the database (appointments,
diagnoses), leaving the rest for a person.

Developers reading code history want the same things: Codoban et al.
(ICSME 2015; 217 respondents) rank non-informative commit messages "66%",
tangled changes "54%" and information overload "47%" as challenges, and
"Aggregate commits into groups" "46%" and "Ability to filter changes" "40%"
as wishes; "commit messages and commit diffs are used while skimming
commits whereas the commit author or other knowledgeable colleagues
represent the most successful strategy for understanding the deep
rationale of a commit." The forensic SoK (Breitinger, Studiawan and
Hargreaves, 2025) warns that aggregation "may result in the loss of
granularity or context" and that tools "can conflate facts with
interpretation within their interfaces"; Timesketch's documented vocabulary
— sketch, timeline, saved view, star, tag, comment, story, analyzer — is the
most mature practitioner set for marking and narrating within a log.

### 3.5 Oversight of agents

The 2025–2026 agent-oversight literature converges on a few points that
bear on a viewer directly. **Raw traces are too verbose and summaries
mislead.** Grunde-McLaughlin et al. (2026): "Designing traces to have an
informative, but not overwhelming, level of detail remains a critical
challenge"; summaries can create "an illusion of accountability"; in their
controlled study a requirements panel moved accuracy from "72.92%" to
"77.08%" but raised confidence most where the output was wrong — "Our
interface did not meaningfully improve agreement but did increase
confidence when participants were incorrect." The IJCAI 2026 four-panel
demo reduced time-to-insight "from 5.5 minutes using LangSmith to 2.4
minutes" with N = 16 and the authors' own tool. **Per-action approval
decays.** Anthropic's auto-mode page: "users accept 93% of them anyway"
and "Over time that leads to approval fatigue"; the YoloFS incident corpus
(290 reports): users "often approve everything blindly or enable 'YOLO
mode'" because "the alternative is answering hundreds of prompts per
session", and "Approval prompts show commands rather than their filesystem
effects." Zhou et al. (CHI 2026; N = 48): "81 percent of participants
preferred our intermediate confirmation approach" over confirm-at-end.
Turan (2026) models an inverted-U — "more human oversight can make a
system less safe" — and finds three LLM reviewer personas agree only at
"Fleiss' κ = 0.52", so "there is no single ground-truth safety label"; it
is "a modeling result ... not an empirical finding about real people".
**The remedies are consistent**: bounded autonomy decided in advance,
intent-level checkpoints, "batch review as a diff" of *effects* (files
added, modified, deleted, renamed), friction only for high-impact or
irreversible actions, marking irreversible actions distinctly, showing
"what the agent did not do", and monitoring reviewer-inattention
signatures ("review duration dropping while approval rates stay
constant"). OpenAI's 2023 white paper adds that a ledger of actions is "a
lighter-touch method than requiring human approval", that reasoning should
be shown "to properly contextualize the action", and that asking an agent
to justify itself afterwards "is likely to produce confabulated reasoning".
OWASP's Agentic Security Initiative names the two threats a viewer sits
between: T8 "Repudiation and Untraceability" ("Require AI-generated logs to
be cryptographically signed and immutable") and T10 "Overwhelming
Human-in-the-Loop" ("rushed approvals, reduced scrutiny, and systemic
decision failures"; "Human Intervention Interface (HII) Manipulation ...
obscuring critical information"). Art. 14(4)(d) of the AI Act wants an
overseer able "to decide, in any particular situation, not to use the
high-risk AI system or to otherwise disregard, override or reverse the
output".

The consequence for this tree *(inference)*: the trail's `AgentAction` row
is the authoritative record and the model's `CallTitle`/`CallReason` is a
claim; a viewer should draw them as different things, lead with the
action's effect and what it caused rather than its invocation, and treat
any risk colouring as the output of a named rule rather than a fact.

## 4 Event schemas and what they model

The forms in §5 presuppose a schema; which schema a trail resembles decides
which forms are cheap. No open schema the passes read carries all five
properties a trail mixing system, person and agent events needs — several
targets per event, a before/after pair, a reason, an on-behalf-of chain
where one exists, an outcome:

| Property | Carried by | Not by |
|---|---|---|
| several targets per event | OCSF `resources` array (API Activity); DICOM/ATNA `ParticipantObjectIdentification` 0..n; OCEL 2.0 `E2O` with role qualifiers; PROV several `used`/`wasGeneratedBy` | ECS (single target); CEF; XES (one case per trace) |
| before / after | OCSF Entity Management `entity` + `entity_result` ("The updated entity"; reading them as pre/post is *(inference)*); ECS `user.changes.*`; CEF `oldFile*` only; OCEL time-indexed object attributes; PROV `wasDerivedFrom` | ATNA; XES; CloudEvents *(recalled)* |
| reason | OCSF `comment` ("A user-provided reason for the change"); ECS `event.reason`; CEF `reason` (an error or return code) | ATNA; XES; LEEF |
| on-behalf-of | PROV `actedOnBehalfOf(ag2, ag1, a)` — the full chain; ECS `user.effective.*` ("The user whose privileges were assumed"); ATNA several participants with `UserIsRequestor`; OCSF actor `session`/`app_name` (weak); OTel GenAI agent/tool/conversation ids — implicit via the trace *(snippet)* | CEF; XES |
| outcome | OCSF `status_id`; ECS `event.outcome`; CEF `outcome`; ATNA `EventOutcomeIndicator` (0/4/8/12); OTel span status | XES; OCEL; PROV; CloudEvents |

Three further conventions matter. **Two timestamps** — OTel `Timestamp`
and `ObservedTimestamp`, OCSF `original_time`/`logged_time`, ECS
`event.created`/`event.ingested` — are standard, and GxP's "recorded at
the time of events, not at the end of a process" is the reason a viewer
should show both when they diverge; the trail has one (§2.5). **A sequence
beside the clock** — OCSF `sequence` ("makes event ordering unambiguous"),
ECS `event.sequence`, syslog `meta.sequenceId` — is what the trail's
`seq`/`Ordinal` columns and the journal's `Ordinal` are. **A hash on the
row** — ECS `event.hash` "for log integrity" — is what the journal's
`Digest` and the seal's `RowDigests` are. The OTel GenAI conventions, as far
as the passes could read them without opening the repository, nest
`execute_tool` spans under an `invoke_agent` root with `gen_ai.tool.call.id`
joining the model's requested call to the tool's span — the same join the
trail makes with `Cause.ToolCall` ↔ `LlmMessage.ToolCallIds[]` *(snippet)*.

The closest fit for this trail *(inference)* is a hybrid: OCSF's
class/activity/status vocabulary for `Kind`/`Operation`/`Outcome` and for
an `AuditEvent`'s `Domain`/`Action`/`Outcome`, PROV's delegation chain on
the rows that carry one (person → coordinator → task → call), OCEL's
qualified multi-object participation for the windows, subjects and datasets
an event touched, and GxP's explicit old/new/reason fields — the last two
being what the components do not have.

## 5 Visual forms for sequences, provenance, processes and traces

### 5.1 Event sequences

The design space is settled, and the one documented failure is the
per-record branching tree at scale; the remedies are filtering, alignment
and aggregation.

Guo et al.'s survey (arXiv 2020; TVCG 2021) fixes the design space: data
scales (event / subsequence / sequence / collection), five representation
families — chart-based, timeline-based, hierarchy-based ("prefix trees
shown as treemaps, node-link trees, or icicle plots"), Sankey-based,
matrix-based — and seven interactions (filter/query, editing,
segmentation, alignment, scaling, emphasis, aggregation). It gives no
numeric scale limit for any system and says the interface "will become
visually messy when the scale of event sequences is large." The one
quantified failure is EventFlow's (Monroe et al., TVCG 2013): aggregating
records "with the same event sequence" into a branching tree produced a
display "so visually complex that it was initially described as 'confetti'"
because "the chances of two records having the same sequence were extremely
small"; the paper's complexity metric is element count and mean element
height, and its worked reduction runs "2724 elements, 1.14% of display
height per element" → "35 elements, 22.75% of display height per element"
through record filtering, category filtering and a time window. Its
remedies — inter-record and intra-record filtering, interval merging,
category merging, alignment on a chosen event with mirrored aggregation
before and after it — are the ones every later HCIL tool kept. The
lab-validated aggregate form is the outcome-coloured Sankey with
per-transition cardinality and timing (Outflow, DecisionFlow; "12-person
user study" each); DecisionFlow targets "thousands of event types" where
earlier tools assumed fewer than 20. The scalable reductions are frequent
patterns (Frequence), ranked branching trees (CoreFlow: "millions of events
in a few seconds"), MDL summaries (Sequence Synopsis), clustering
(EventThread, Sequen-C) and windowed composite events (Mathisen and
Grønbæk); Du et al. (TVCG 2016) organise "15 strategies for sharpening
analytic focus" into extraction, temporal folding, pattern simplification
and iteration.

For a trail whose "record" is whatever a correlation key groups — a run,
a window, a bus subject, a job, a data subject, a conversation or a task —
and whose event categories (kinds, operations, bus subjects, domain
actions) run to the hundreds, the prefix tree is in DecisionFlow's regime
and is legible only after category coarsening (kind, operation family,
effect class, outcome) *(inference)*; alignment on a milestone — a launch,
the first denied request, the first consequential action, the grant — is
one window query per alignment over a column store and gives the
before/after mirror EventFlow describes *(inference)*.

### 5.2 Provenance

Provenance has a settled notation and a layout ceiling of a few thousand
nodes; the usable device is collapsing by process, and it applies only to
rows that carry a cause edge *(inference)*.

PROV-DM's informative notation is the de-facto convention: "Entities,
activities and agents are represented as nodes, with oval, rectangular,
and pentagonal shapes, respectively", edges "typically point leftwards",
"Entities are laid out according to the ordering of their generation";
PROV-O colours them "yellow ovals", "blue rectangles", "orange pentagons",
"The responsibility properties are shown in pink." The qualification
pattern lets a relation carry a time, a role and a plan. Macko and
Seltzer's Orbiter (TaPP 2011) states the layout ceiling — "the best graph
layouts for provenance are hierarchical, such as dot ... but their
computation usually does not scale beyond a few thousand nodes", while
force-directed layouts "are too difficult to interpret by end users" — and
collapses by the **process tree**: one summary node per process, objects
absorbed into the summary that holds all their ancestors and descendants,
same-typed objects merged. Moreau's aggregation by provenance types groups
nodes by their k-bounded path type with frequencies. Analytic-provenance
tools (VisTrails) show a version tree in which only leaves, branch points,
tagged nodes and the current version are shown and the rest are
"collapsed"; its Visual Diff colours shared, one-side-only and
parameter-changed modules.

The trail's agentic rows form a provenance graph by construction —
`Delegation`, `Cause` and `LlmCall.Parent` are its edges. The other kinds
record no cause edge beyond `Origin` and whatever `Ref*` an `AuditEvent`
carries, so for them provenance is a join on identity, not a graph, and the
forms of this section do not apply. Where the graph exists Orbiter's
collapse maps onto it directly *(inference)*: group by task (the process
tree), absorb the windows and datasets touched only inside that task, merge
same-kind leaves; a per-event node-link graph is viable only for a selected
conversation's neighbourhood, never for the whole table.

### 5.3 Process mining

Two views are universal: the **dotted chart** (events as dots, time on x,
case on y — Song and van der Aalst 2007) and the **directly-follows graph**
with frequency and performance decorations. Van der Aalst's practitioner's
guide (2019) is the authoritative statement of the DFG's hazards:
"Activities that have a flexible ordering (e.g., due to concurrency) lead
to Spaghetti-like DFGs with loops even when activities are executed at most
once"; frequency thresholds "may lead to all kinds of interpretation
problems due to 'invisible gaps' in the model"; and "the average time
reported between two activities is conditional (only the situations where
they directly follow each other are considered)" — his example has a true
gap of "3.5 days" where two tools report "1.5 days". His threshold advice:
raising the variant threshold "is quite harmless", raising the activity or
edge thresholds leads "to new problems", and most tools filter edges on
the overall DFG rather than re-mining, so "the frequency of a selected node
may be different from the sum of the frequencies of the input edges".
Disco's manual draws the line a viewer should copy: its sliders "are not
filters" — statistics, cases and variants are unchanged — and variant
focusing "works only for structured processes. Unstructured processes, such
as hospital diagnosis or clickstreams, have no dominant variants." Celonis
adds a "Fixed Layout" button so the graph stops moving as edges come and
go. Conformance as token replay and alignments is *(recalled)*.

For this trail the case is any correlation key — a window, a run, a job, a
bus subject, a data subject, a conversation or a task — the activity is the
kind, operation or domain action, and the DFG answers "what follows what";
the dotted chart is the cheapest per-event view that still scales because
it is a `(time, case)` scatter that becomes a binned density once dots
exceed pixels, and it presupposes no cause chain, so it is the one
per-event form that serves every kind alike *(inference)*.

### 5.4 Traces

A trace UI is a waterfall over a parent/child tree, with a minimap and
critical-path shading; it pays only where parents exist *(inference)*.

OpenTelemetry defines a trace as "a directed acyclic graph (DAG) of Spans,
where the edges between Spans are defined as parent/child relationship";
a span has a name, start and end, attributes, events ("a structured log
message (or annotation) on a Span"), status Unset/Error/Ok, a kind, and
links, which "imply a causal relationship" and are "typically used when
an operation triggers asynchronous follow-up work". Trace UIs render this
as a waterfall with expand/collapse, a minimap ("Drag your mouse over the
minimap to zoom into a smaller time range"), critical-path shading
("following the CRISP ... algorithm"), span filters, and trace-to-logs
links (Grafana); Jaeger raises chosen tag prefixes in span details, defaults
`criticalPathEnabled` to true, and disables its service DAG above
`dependencies.dagMaxNumServices` "200". Structural **trace diff** (Jaeger
via Logz.io) groups spans by service and operation and colours grey "Both
traces have the same node and an equal number of spans", dark green/red for
a node present in only one trace, light green/red for a count change;
durations are not compared. Flame graphs merge identical stacks — "The
x-axis shows the stack profile population, sorted alphabetically (it is not
the passage of time)" — and flame charts put time on x; Brendan Gregg's
page lists differential flame graphs but the pass could not confirm their
colouring on the page.

Where the trail has a cause chain it already has a span tree
*(inference)*: `LlmCall` with `ts` and `ElapsedMs` is a span, `AgentAction`
dispatch→final is a child span, caused queries and fetches are
grandchildren via `(Task, Call)`, and `AgentGrant.Request` → decision
across time is a span *link* in OTel's sense (causal, not parental). A row
without `Delegation` or `Cause` — a bus request, a log line, a metrics
sample, a records-of-processing event — is a span with no parent; a
waterfall over such rows is a flat list and adds nothing to a table. The
tree lacks the widget (§2.7).

### 5.5 Security

For actor × object questions the evidence favours matrices and Sankeys over
node-link diagrams past about twenty nodes, and warns that a compressed
strip must open into its rows.

Ghoniem, Fekete and Castagliola (InfoVis 2004): "when graphs are bigger
than twenty vertices, the matrix-based visualization performs better than
node-link diagrams on most tasks. Only path finding is consistently in
favor of node-link diagrams". Blinder et al. (INTERACT 2019) compared
node-link and Sankey for "which users accessed the database and what
operations were performed" and found the Sankey "more effective in
synoptic tasks, while the Node-link diagram was more effective in basic,
elementary tasks" *(snippet — the publisher page blocked the fetch)*.
ViC2ES (VizSec 2018) compressed daily user activity by binning and run-length
encoding into icon arrays and found "participants preferred the high level
of aggregation, but made the fewest errors with the low level of
aggregation". Stange et al. (VizSec 2014) kept "the whole log in an
overview" with "context-preserving subselections". Day-of-week × hour
heatmaps for off-hours access are common with the caveat that a scale
dominated by busy cells can make a small burst look empty *(snippet)*.

For access review — which principals, apps or windows touched which
subjects, operations, bus subjects or destinations — this argues for a
sortable principal × object matrix once either side exceeds about twenty, a
three-column Sankey (actor → object → outcome) for the synoptic question,
and node-link only for tracing a delegation path where one is recorded
*(inference)*; and ViC2ES's finding is a direct warning
that a compressed strip must open into its raw rows in one click.

### 5.6 Diff

A diff needs an explicit difference layer beside the two versions; for a
record the unit is the field, and no study of record-level diff
comprehension was found.

Zaman, Kalra and Stuerzlinger (GI 2011): "naïve dual-view visualization is
undesirable"; "participants particularly liked the dual-view with
difference layer technique". TreeVersity (Guerra Gómez et al.): "even in
trees with just a dozen nodes it is difficult to find those differences";
the DiffTree glyph points up/right for increases and down/left for
decreases, "height of the Bullet is proportional to the relative change (in
%) while the color is mapped to the absolute change", unchanged nodes "shown
as small gray rectangles", "created and removed nodes are highlighted with
a thick white or black border respectively"; tested with "Eight usability
test participants". The one eye-tracking study of split versus unified
text diffs (IEEE Access 2023; 12 participants) found "less visual strain
when using the unified view" and "did not find any statistically
significant differences". Wikipedia's diff is the most widely used shipped
convention: older left, newer right, changed paragraphs with a yellow "−"
border and a blue "+" border, moved paragraphs marked and clickable,
word-level changes in bold, and "(x intermediate revisions not shown.)"
when the two revisions are not adjacent. Confluence collapses "Large
unchanged sections ... to an ellipsis". No study of JSON or record-level
diff comprehension was found.

For a record diff the unit is the field, not the line *(inference)*: old and
new side by side plus an explicit per-field difference layer, with
added/removed fields bordered and changed fields marked; for nested
payloads TreeVersity's convention is the most tested; a linear,
time-ordered change list with collapsed uninteresting runs suffices for a
history that rarely branches, and the journal's does not branch on the
compile date (`parent` reserved, §2.2).

### 5.7 Which forms aggregate in SQL and which are per-event; scale

The scalable family is "aggregate to the resolution of the display".
imMens (CGF 2013): "perceptual and interactive scalability should be
limited by the chosen resolution of the visualized data, not the number of
records", with "invariant performance on data sizes ranging from thousands
to billions of records" through binned aggregation and data tiles; Falcon
(CHI 2019) adds prefetch by re-indexing on the active view and
"progressively improves" a reduced initial resolution; Nanocubes fit a cube
"in a modern laptop's main memory, even for billions of entries". M4 (PVLDB
2014) proves that per pixel column "the aggregates min(v), max(v), min(t),
and max(t)" joined back to the series give "the visualization of the
unreduced underlying time series", so a line chart needs at most "4·w"
tuples; averaging "will significantly distort the actual shape". No paper
gives a perceptual threshold for how many marks an overview tolerates;
the limits are system-reported examples (Monroe's bars, Jaeger's 200
services, Orbiter's few thousand nodes).

Classification for a column-store-backed viewer *(inference)*:

- **Aggregates in SQL to any volume** — time histograms and stacked bars
  per bin (count by kind, operation, domain and action, subject, outcome);
  actor × object matrices;
  dotted charts as binned density; DFG edge counts; flame-graph path
  counts; prefix trees after category coarsening; variant tables; M4 or
  LTTB for any numeric-over-time series (rate, latency, tokens).
- **Scales only to the display** — any line chart of counts or durations.
- **Inherently per-event, reached by selection** — per-record timelines,
  span waterfalls and Gantts, flame charts, record diffs, unsummarised
  provenance node-link, version trees without collapsing. Caps are hundreds
  to low thousands of marks.

Monroe's complexity metric (element count, mean element height as a
fraction of the display) can be computed from an aggregate query and used
to coarsen categories or shorten the window automatically; their readable
example sits around "15.01%"–"22.75%" *(inference)*.

## 6 UX paradigms in shipped products

Organised by pattern; the products are evidence, not models to copy.

### 6.1 Table conventions

Every surveyed audit log is a time-ordered table with a small fixed set of
first-class columns and the rest behind a per-row pane. CloudTrail Event
history: "Event name", "Event time", "User name", "Event source", "Resource
type", "Resource name"; the column order cannot be changed and events
cannot be deleted. Purview: "Date (UTC)", "IP Address", "User", "Record
type", "Activity", "Item", "Admin Units", "Details". Atlassian: activity
type, date, time, actor, app, location, IP. Okta: "time, actor, target, and
more" with a right arrow that expands the row. Kibana Discover: a time
column and a "Summary" column that added fields replace; a flyout for
"View details"; a JSON "View mode". Wikipedia's page history is the
counter-model where every edit is a row with cheap per-row metrics — byte
size, a coloured size delta, an "m" for minor, the summary, tags — and two
radio columns pick revisions to compare. Column choosers are common (Azure
"Edit columns", Workspace "Manage columns", Kibana "Columns", Timesketch's
"customise columns" over a default single message column). Time display
is a per-view preference — CloudTrail "Event time display" UTC or local,
Etherscan "UTC"/"Local"/"Unix Timestamp", Kibana's three time formats — not
a per-column one *(inference)*.

### 6.2 Filter and query languages; saved views

Three dialects coexist *(inference)*: free-text keyword (Okta `q`, Purview
"Keyword Search"), qualifiers (GitHub `action:`, `actor:`, `user:`,
`created:`, `operation:`, with `-` for exclusion and no free text: "cannot
search for entries using text"), and expression languages (Okta's SCIM-style
`eventType eq "user.session.start" and outcome.result eq "FAILURE"`;
Atlassian ALQL with a mandatory `created` clause and `ORDER BY` last;
Kibana KQL/Lucene/ES|QL). CloudTrail's console deliberately allows "only
one attribute filter and a time range filter" and pushes multi-attribute
questions to SQL in Lake. GCP's Logs Explorer binds a query pane, a fields
pane whose values add themselves to the query, a severity-split timeline
with drag handles, and results in one surface — the log-explorer trinity
(histogram ↔ table ↔ facets, all bound to one query) that Datadog's Audit
Trail, Grafana Explore and Kibana share *(inference)*. Saved views appear
everywhere: GCP "Query library" and a "Share link" carrying the absolute
range, Okta "Save as new" listed under Reports, Datadog org-shared saved
views with star and short-link, Kibana saved Discover sessions, Purview
"Copy this search", Timesketch "Views are saved search queries", Plaso
tagging files. Exports carry a stated cap (CloudTrail "200,000 events per
file", GCP "10,000 entries", Datadog "100K events", Purview "50,000 rows"
Standard) and CloudTrail's docs warn to "Always disable links or macros
from downloaded event history files".

### 6.3 Pivots and context

"Follow this entity" is implemented as pin, context or stepper rather than
a subscription: GCP "Pin" → "Same resource.type" / "Same resource.labels" /
"Same trace" and "Show all logs for this trace"; Grafana "Show context"
("similar to `grep -C`") with "Open in split view"; Kibana "View
surrounding documents"; Plaso's slice option with a "default window is 5
minutes on each side"; CloudTrail's "Resources referenced" table whose rows
link to the resource's console and to an AWS Config timeline, with "Link
not available" for unsupported or deleted resources; Azure's resource-scoped
Activity blade; Wikipedia "Find edits by user". Click-a-value →
include/exclude is universal (GCP "Show matching entries" / "Hide matching
entries", Grafana positive/negative field filters, Datadog filter-by and
exclude). Google Workspace's "Group Results" groups by one attribute with
counts, most frequent first, and lets a row "Add condition to search".

### 6.4 Compare and two-point diff

Compare is a two-point selection, and the products that do it well state
the base in words.

CloudTrail: "You can select up to five events in Event history and compare
their details side-by-side". Azure: "Change history" shows "any changes on
the resource up to 30 minutes before and after the time of the operation"
and a page with the size "before the change and after the change". Datadog:
an "Inspect Changes (Diff)" tab for configuration objects. Wikipedia
"Compare selected revisions"; Confluence "Compare selected versions" and
"Compare with Current"; Kibana "Compare selected". GitLab's compare page
names two base semantics in words — "Only incoming changes from source"
(merge-base) versus "Include changes to target after source was created" —
and Gerrit's patch-set selector always orders A before B; the lesson is that
a two-point diff states its base in words, not a toggle *(inference)*.
Google Docs, Notion and Figma diff only against the current version
*(recalled)*.

### 6.5 Version-history collapsing

Document products time-box edits into versions and let a person promote
one by naming it: Notion "a version every 10 minutes while you're actively
editing" plus one "two minutes after your last edit"; Figma a checkpoint
"every 30 minutes" with "Autosaved checkpoints are grouped and expand when
clicked"; Google "The revisions for your file may occasionally be merged"
with "Only show named versions". Wikis keep every edit and collapse in the
diff instead: "(x intermediate revisions not shown.)". VisTrails collapses
untagged linear runs. Non-destructive restore is the audit-friendly form
— Figma's restore "adds two checkpoints: current and restored", Confluence
copies the old version forward — and Google Sheets' restore clearing
cell-level history is the counter-example its own help notes *(inference)*.
LangSmith's "trajectory" is "a flat, ordered list of the human, AI, and
tool messages in a thread, with run nesting removed" — a documented
burst-collapsing device for agent traces.

### 6.6 Blame and attribution overlays

Blame is "follow this field backwards", with a device for hiding bulk,
mechanical changes.

GitHub's Blame groups lines by commit with author, message and date, and a
"View blame prior to this change" stepper; `.git-blame-ignore-revs` hides
mechanical commits behind a banner. GitLab adds an age legend ("Newer" →
"Older") and the same ignore-revs preference, with its own feature history
on the page (flag → enabled → GA across releases). Dolt exposes
`dolt_blame_<table>` as "the commit responsible for each row's current
value". The stepper is the VCS form of "follow this field backwards";
ignore-revs is a shipped answer to attribution drowned by bulk actions,
and an agent trail has the same problem with reformatting-class actions
*(inference)*.

### 6.7 As-of and history tables

Data platforms make "as it was at T" a clause on the read, not a UI mode:
Datomic `as-of` ("ignoring any transactions after that point"), `since`,
`history` with an `added` flag per datom, and `filter` predicates (the docs'
examples drop a known-bad transaction or hide a password hash); XTDB `FOR
SYSTEM_TIME AS OF` ("as we knew it at the time") beside user-editable valid
time, with `_system_from`/`_system_to` columns; Snowflake `AT(TIMESTAMP |
OFFSET | STATEMENT => id)` — anchoring a read to a statement id rather than
a clock; Delta `VERSION AS OF` and `DESCRIBE HISTORY` with per-operation
`operationMetrics`. History as a table: Dolt's `dolt_diff_<t>` with
`from_commit`, `to_commit`, `diff_type` ∈ added/modified/removed and a
`from_X`/`to_X` pair per column — a tabular before/after that scales to many
columns and renders as a two-column diff per row *(inference)*. None of the
fetched docs describe a GUI for these beyond Snowsight's Query History.

### 6.8 Session and correlation grouping; replay

Every product has a correlation key: Okta `transaction.id`, Slack
`context.session_id`, CloudTrail `requestID`, `sharedEventID` ("the same
AWS action that is sent to different AWS accounts") and `signInSessionArn`
("Use it to audit API activity by sign-in session"), GCP `operation.id` +
`producer` with `first`/`last` flags, Azure `correlationId`, Atlassian
`correlation id`, LangSmith `thread_id`, Langfuse `sessionId` ("replay the
entire interaction"), Phoenix and Braintrust sessions. CloudTrail's
`addendum` (`reason` ∈ `DELIVERY_DELAY`, `UPDATED_DATA`, `SERVICE_OUTAGE`,
with `originalEventID`) is an append-only correction pointing at the
original — the pattern the trail's `audit-gap` rows approximate. Replay
appears as session views (Langfuse), trajectory views (LangSmith), GCP's
timeline drag and "Zoom to time", Okta's drag across bars; agent-trace
viewers model human approval "not as a span kind but as annotation/feedback
attached to a span or session" with workflow state outside the trace
*(inference)* — this tree records approvals as journal entries and grant
events, which none of the surveyed products do in their docs.

### 6.9 Anomaly highlight

Anomaly surfacing is a separate tab or dashboard, not a row colour:
CloudTrail Lake's "Highlights dashboard" whose widgets "could surface
detected abnormal activity or anomalies", refreshed every six hours;
Slack's "Security Detections" tab; Timesketch analyzers that "can tag
events, star them, and create saved views". Timeline2GUI colours rows by
category *(snippet)*. The human-factors evidence (§3.4) is that
highlighted entries must show the rule that produced them.

### 6.10 Integrity wording to a non-expert

Cloud logs assert integrity in prose — CloudTrail "immutable record" and
"You cannot manually delete events", Azure "you can't change or delete
them" — and expose no per-row check in the console; CloudTrail's log-file
validation is *(recalled)* and not surfaced in Event history. Ledger
explorers are the one family that shows integrity routinely: Etherscan
"Status:" as one word, "Block:" with "Block Confirmations", relative and
absolute time, proofs behind "Click to show more". Transparency logs phrase
it as properties of the log ("immutable, tamper-resistant", "append-only")
and return an entry URL and an inclusion proof; Rekor v2 (GA October 2025)
stops returning signed timestamps with proofs and says inclusion proofs
"should be stored alongside the artifact and its signature". Azure
confidential ledger's receipt is a Merkle proof with `serviceEndorsements`
so "auditors [can] build the chain of trust", and its sample reports
"Receipt verification succeeded" / "failed". Trust-indicator research warns
passive badges go unseen: "38% of participants never noticed any seal"
*(snippet — Kirlappos et al. 2012, PDF blocked)*.

### 6.11 Identity chains

No surveyed product *visualises* the human → agent → service chain; all
flatten "who" into one column and nest the chain in JSON. CloudTrail's
`userIdentity` is the richest: `type` (Root, IAMUser, AssumedRole,
AWSService, IdentityCenterUser, …), `sessionContext.sessionIssuer` for an
assumed role, `sourceIdentity` ("identifies the original user identity
making the request" through role chaining), `invokedBy` (the service acting
on your behalf), `onBehalfOf`, `inScopeOf`, and `HIDDEN_DUE_TO_SECURITY_REASONS`
for a bad sign-in name. GitHub's audit log has two qualifiers — `actor` and
`user`, "the user affected by the action. For agent actions, this is the
user the agent acted for" — and documents `-actor:Copilot`. Slack shows
"acting_agent: Slackbot"; Salesforce `DelegateUser` and `CreatedByContext`
*(snippet)*; Datadog `@delegator.id` beside `@usr.id`; ECS `user.effective`.
Each adds a string field; none draws the chain. The tree records more of
the chain than any of them — `Origin` (coordinator window), `Delegation`
(task), `Cause` (model call, tool call), `AgentGrant.DecidedBy`,
`AuditEvent.Principal`/`PrincipalBy`/`PrincipalVia` — and has no surface for
it (§2.6). This is the one place the survey found open ground
*(inference)*.

### 6.12 Documented post-ship revisions

Reversals are rare; the documented changes are retention lengthenings,
label renames and progressive rollouts: Purview Audit (Standard) "changed
from 90 days to 180 days" for logs on or after 2023-10-17; Slack "As of
April 30, 2026, audit logs are kept for a maximum of two years"; CloudTrail
Lake "will no longer be open to new customers starting May 31, 2026";
GitLab inline blame flag (16.7) → enabled (19.1) → GA (19.5); Kibana "Toggle
dialog with details" → "View details" and a JSON view mode; CloudTrail
`eventVersion` 1.11 adding `addendum`; Google "Version history (formerly
known as Revision history)" *(snippet)*; MediaWiki's visual diff shipped
behind a Beta toggle *(snippet)*; QLDB retired 2025-07-31 *(snippet)* — a
caution that an anchor needs an exportable proof format that outlives the
service *(inference)*. Datadog's retention resets to seven days when the
feature is disabled. No vendor post explaining a *redesign* of an audit-log
UI was found.

## 7 Implementation methods

### 7.1 Storage models and their costs

Four families, with costs the documentation states or implies
*(inference where marked)*:

| Model | Write | State as of T | History of X | Who touched X |
|---|---|---|---|---|
| event sourcing (Fowler; Microsoft) | O(1) append | replay to T from a snapshot — "it's costly to read and replay events" | stream scan by entity | scan |
| system-versioned tables (SQL Server; MariaDB) | one history row per modification, "even if no column values change" | union + period predicate: AS OF is `ValidFrom <= t AND ValidTo > t` | history-table key scan | scan; history growth "can slow scans ... for current-data queries" unless partitioned |
| generic trigger log (Postgres wiki) | trigger + hstore of changed fields | merge old row + deltas — the reader's job | filter by table + row | filter |
| CDC envelope (Debezium) | moved off the OLTP path | n/a | per key | per key; `before` complete only under `REPLICA IDENTITY FULL` |
| bitemporal (XTDB) | new row per update; non-overlapping "rectangles" | `FOR SYSTEM_TIME AS OF` | `FOR ALL SYSTEM_TIME` | scan; `ERASE` removes "for all valid-time, for all system-time" |

Microsoft's guidance that an event store is "not SQL-queryable" is about
purpose-built stores; a columnar table holding the events inverts it
*(inference)*. The Debezium envelope — `before`, `after`, `op`, `source`,
`ts_ms`, a `transaction` block with `total_order` — is the natural row
shape for "who changed what", and its lesson is that a before-image exists
only if the writer captured it. Microsoft names the two honest options for
erasure in an append-only store: keep personal data outside by reference,
or crypto-shred; XTDB's `ERASE` admits history queries then stop returning
the data. The tree's trail is the event-sourcing family with one time
column; its journal adds a contiguous ordinal and a chain; the vault with
per-entry openings is crypto-shredding by construction (§2.2).

### 7.2 ClickHouse patterns for a facts-shaped wide table

Four of these bear on the trail directly *(inference)*: key order decides
what prunes; roll-ups and binning serve the overview; window functions
give before/after on one key; bounded parallel arrays are the pre-JSON
shape for attributes.

The documentation, read 2026-10-30, with the version markers the pages
themselves carry:

- **Key order and pruning.** "order the primary key columns by cardinality
  in ascending order"; the generic exclusion search is "most effective
  when the predecessor key column has low(er) cardinality"; a key filtered
  on its *second* column "almost executed a full table scan". The
  observability guide: 4–5 key columns, timestamps "have been shown to
  cause slow query performance if this column is used in the
  primary/ordering key" first, and "Don't use keys in attribute maps for
  the ordering key". Altinity: timestamp "as the latest column", a
  truncated-bucket-then-raw-time tail when a small part of a partition is
  queried. A second access path is a second table, a materialized view
  ("data is automatically kept in sync") or a projection ("the most
  transparent option"). `EXPLAIN indexes = 1` shows parts and granules
  before and after each index; from 25.9 it is meaningful only with
  `use_query_condition_cache = 0` and `use_skip_indexes_on_data_read = 0`.
  The trail golden's `(id, ts)` with `id` a hash is the descending-
  cardinality case these pages warn about, and the deployed default's `ts`
  alone is the time-first key they also warn about; neither puts a
  correlation value in the key, which is why reads by conversation, task or
  subject scan within the time range (§2.1) *(inference)*.
- **Roll-ups.** Incremental MVs trigger per inserted block into
  `AggregatingMergeTree` with `-State`/`-Merge`; readers use `FINAL` or
  re-aggregate; JOINs in MVs "won't retroactively update"; the docs'
  example is "an over 25x improvement" on a daily roll-up, the observability
  guide's "over 75 times!" — vendor figures on small examples. A Null-engine
  source feeding MVs avoids double storage, which is the deferred storage
  switch of ADR-0296 §SD9.
- **Latest state.** `argMax(arg, val)` with a tuple `val`; ties are "not
  deterministic". `ReplacingMergeTree` offers "eventual correctness only";
  `FINAL` is "less efficient and is not recommended for use with large
  amounts of scanned data" (Collapsing), with partition-aware settings that
  Altinity's timings show matter.
- **Before/after on one key.** `lagInFrame`/`leadInFrame` over `PARTITION
  BY subject ORDER BY (time, seq)` with an unbounded frame; `neighbor` gives
  "incorrect result over blocks" (Altinity). `groupArray` returns values "in
  an unspecified order" *(snippet)* — collect `(time, event)` tuples and
  sort.
- **Sequences.** `sequenceMatch`/`sequenceCount`/`sequenceMatchEvents`
  (up to 32 conditions; same-timestamp events "may lay in the sequence in
  an undefined order"), `windowFunnel` with strict modes, `retention`.
- **Top-N per group and sampling.** `LIMIT n BY expressions`; `SAMPLE`
  requires a sampling key and is deterministic and consistent across
  tables.
- **Binning and inline summaries.** `toStartOfInterval` with `WITH FILL
  ... STEP` for gaps; `sparkbar(width)(x, y)` returns a String and wants
  pre-aggregated data; `quantileTDigest` ("Memory consumption is log(n)",
  nondeterministic); `largestTriangleThreeBuckets` (alias `lttb`, from
  23.10) for one point per bucket.
- **Heterogeneous attributes.** JSON "is marked as production ready in
  version 25.3" with `max_dynamic_paths` default 1024 and hot paths promoted
  to typed columns; Dynamic and Variant pages carry no readiness label and
  are excluded from GROUP BY/ORDER BY by default; Map single-key reads are
  linear or bucketed. The trail's `AuditEvent` keeps `AttrKeys`/`AttrValues`
  as bounded parallel arrays, which is the pre-JSON shape.
- **LowCardinality.** "less than 10,000 distinct values" mostly helps,
  "more than 100,000" can hurt — a bound on which trail symbols qualify.

### 7.3 Diff algorithms and patch formats

Line diff is settled; for a record the shapes to copy are RFC 7386's
sparse field map and RFC 6902's `test` *(inference)*.

Myers (1986): "a simple O(ND) time and space algorithm" with "O(N + D²)
expected-time performance" and a linear-space variant "roughly twice as
slow". Git documents `myers` (default), `minimal`, `patience` and
`histogram` ("extends the patience algorithm to 'support low-occurrence
common elements'"), `--word-diff`, `--color-moved` (zebra colouring of moved
blocks) and `--anchored`. Bram Cohen's patience diff anchors on lines unique
to both sides so braces are not paired across functions. RFC 6902 JSON Patch
is an ordered list of `add`/`remove`/`replace`/`move`/`copy`/`test`
operations with JSON-Pointer paths, `test` asserting "The target location
MUST be equal to the 'value'"; RFC 7386 Merge Patch mirrors the target,
null means delete, and "it is not possible to patch part of a target that
is not an object, such as to replace just some of the values in an array".
Tree diffs: Zhang–Shasha O(m²n²) worst case, GumTree O(n²) heuristic with
move detection *(snippet)*. For a record diff the unit is the field: the
trigger log's "changed fields only" and RFC 7386 are the same sparse map;
RFC 6902's `test` is the natural "I expected the before-value to be X";
word-level highlighting and move detection are the two text refinements
worth copying *(inference)*. The tree has an LCS line diff in app code and
no field diff (§2.7).

### 7.4 Tamper evidence and what a verifier needs

A hash chain proves inclusion to a third party only by replaying it; a
verifier needs saved heads, proofs and gossip, and the tree's verdict
vocabulary already matches *(inference)*.

Crosby and Wallach (USENIX Security 2009): a hash chain has O(1) append but
"an incremental or membership proof between two commitments or between an
event and a commitment must include every intermediate event in the log";
"Hash chaining schemes, as such, are only feasible with low event volumes
or in situations where every auditor is already receiving every event";
their history tree "can return a complete proof for any randomly chosen
event in 3100 bytes" where a chain over "80 million events" would send "an
average of 40 million hashes". Fork detection is not a property of proofs:
"A dishonest logger may attempt to tamper with its history by rolling back
the log, creating a new fork"; the log "remains tamper-evident if clients
gossip their received commitments from the logger to at least one honest
auditor". RFC 9162 bounds a consistency proof at "ceil(log2(n)) + 1" nodes,
defines the STH (timestamp, tree size, root hash, signature), and says a
misbehaving log "that shows different, inconsistent views of itself to
different clients" defeats auditing, so "it is necessary to treat each log
as a trusted third party" absent gossip. transparency.dev: "without stored
historical heads, a full audit can't detect a fully recomputed, tampered
tree". immudb's docs say detection "can't identify the specific transaction
where tampering occurred" *(snippet)*. RFC 3161 time-stamp tokens bind a
hash to `genTime` with an `accuracy`; OpenTimestamps distinguishes
"Pending" from complete attestations. NIST SP 800-92 §3: "Log file
integrity checking involves calculating a message digest for each file and
storing the message digest securely".

What a verifier needs, assembled from these *(inference)*: a trusted head
saved earlier (size, root, signature, time); an inclusion proof per
entry; a consistency proof between saved heads; and gossip or witnesses for
forks. The vocabulary the sources support — *verified*, *invalid*, *gap*
(only meaningful where skipping is legal), *fork* (two valid commitments
that are not consistent), *anchor pending* — is the one §2.2 already has
in `BreakE`/`SealBreakE`/`unanchored`. Two consequences for this tree: on
a hash chain a "gap" marker means "the walk stopped here" and everything
after it is unverified, so inconsistency is a range alarm, not a per-row
flag; and the viewer must persist the heads and anchors it has seen (the
tree's `anchors` relation and durable anchor store are that memory) rather
than re-request the latest head. Rekor's sharding — "log indices are
distinct and increase across shards" — is the precedent for a chain or seal
stream restarting at a key rotation and being shown as a boundary rather
than a gap.

### 7.5 Rendering at scale

M4 and imMens/Falcon (§5.7) give the rule: one aggregate query per visible
view at pixel resolution, re-issued when the brushed dimension changes,
per-event rows only for the selection *(inference)*. Row virtualisation
renders only visible rows — AG Grid's default `rowBuffer` of "10" and a
"500"-rendered-row safety cap; egui's `ScrollArea::show_rows` takes a fixed
row height and the total count and calls back with the visible range,
variable heights unaddressed. Fekete and Primet: exploration needs latency
"below about 10 seconds because of human cognitive limitations", met by
progressive computation; Stolper, Perer and Gotz's progressive visual
analytics lets analysts "inspect partial results of an algorithm as they
become available". Cockburn, Karlson and Bederson's survey is the caution
on overviews: Ghosh and Shneiderman found "The overview+detail interface
allowed tasks to be completed more quickly" for Lifelines, but Hornbæk,
Bederson and Plaisant found "overview+detail regions increased task
completion times when semantic zooming was enabled" and "The participants
preferred the interface with the overview despite their slower
performance." The viable layering for 10^7–10^8 rows *(inference)*:
pre-aggregated per-bucket counts at fixed bucket sizes via MVs; M4/LTTB for
the viewport; a virtualised detail list fetching the visible index range
by key range; `SAMPLE` or t-digest for inline summaries; progressive
refinement when the first response is slow. None of the envelopes the
passes read were measured on an audit workload.

### 7.6 Correlation and causality

W3C Trace Context carries `trace-id` and `parent-id` per hop; OTel builds
the tree by parent id and adds links for fan-in and asynchrony (§5.4).
Reconstructing a cause chain from a flat table is the same operation
*(inference)*: group by the root (a task or a conversation, on the agentic
rows that have one), build parent → children by `parent-ref`, attach
cross-tree edges from `AgentGrant.Request` and `LlmCall.Approval`; in
ClickHouse a `groupArray((seq, parent, id, kind))` per root and a
client-side build, or an `arrayJoin` self-join for small trees — the
`dm_trail_timeline` mart already computes `parent-ref` for exactly this
walk (§2.1). Without parent ids — the case for every kind that carries
`Origin` alone — the correlated set is a join on identity, and NIST's
rule-based correlation (co-occurrence on run, window, actor, target) and
Debezium's transaction ordering are the fallbacks; logical clocks are *(recalled)*. Sessionisation
is a gap threshold with a cumulative sum over `lagInFrame`
*(snippet-sourced example)*. Burst collapsing is NIST's "event aggregation"
— "similar entries are consolidated into a single entry containing a count
of the number of occurrences" — and in ClickHouse a run-length key from
`lagInFrame` or `LIMIT 1 BY (subject, kind, bucket)` with a count; the
rsyslog precedent shows the information loss is accepted in log viewers.
No published algorithm specific to tool-call trees was found.

### 7.7 Audit reduction, sampling and summarisation

Reduction is permitted only where the originals and their order survive,
so every summary row must open into its rows; sampling that keeps the
rare rows is tail sampling done at read time *(inference)*.

NIST's glossary defines audit reduction tools as "Preprocessors designed to
reduce the volume of audit records to facilitate manual review" that
"remove many audit records known to have little security significance";
SP 800-92 distinguishes log reduction, event reduction, filtering ("does
not alter the content of the original logs"), normalization, correlation,
viewing and reporting, and footnote 23 ties it back: "Ensuring that the
original logs are not altered supports their use for evidentiary purposes."
AU-7's invariant — reduction must not "alter the original content or time
ordering" — means every summary row must open into its unaltered originals
*(inference)*. OpenTelemetry: head sampling "is not possible to make a
sampling decision based on data in the entire trace"; only tail sampling
can always keep error or rare traces, at the cost of components that "must
be stateful systems that can accept and store a large amount of data"; a
uniform `SAMPLE` is head sampling, and "keep every rare or flagged row,
sample the rest" is tail sampling done at read time *(inference)*. LLM
summarisation of logs has a 2024 benchmark (LogEval) and 2025 preprints
(REFLEX; LLMLogAnalyzer reporting "0.45 ROUGE-1 F1") *(snippet)*; no
adopted standard, and OpenAI's own caution about confabulated rationale
(§3.5) applies — a summary is a navigational aid over reduced views, not
evidence *(inference)*. Retention that preserves tamper evidence has two
honest options: prove authorised deletion (Crosby's Merkle aggregation
"safe deletion") or show the erasure (XTDB `ERASE`; the journal's `erased`
and `deleted` marks).

## 8 The design space, mapped onto this tree

### 8.1 Reader tasks against the data

Taking §1's tasks against §2's data, by the correlation key each is asked
under *(inference throughout)*. A row not marked *agentic* serves every
kind; a row marked *agentic* exists only where the lanes of ADR-0277 §SD1
are set:

| Task | Correlation key | Feeds | What is missing |
|---|---|---|---|
| reconstruct | a run; a window `(run, instance)` | `dm_trail_timeline` (`Origin` columns on every row), `runtime_events` for the current run's runtime kinds, `QueryRun` rows, `app_runs`/`app_logs`/`app_audit` | an earlier run through introspection (ADR-0191 Deferred — raw SQL is the route); log rows name no task or call (the other facts-bound stores carry `Origin` since 2026-10-30, ADR-0277 Updates) |
| reconstruct | a bus subject, a destination, an `AuditEvent` subject or principal | `dm_trail_audit_events` (domain, action, subject, principal, `Ref*`/`Attr*`), `dm_trail_http_fetches`, `http_destinations` live | a skip index for reads by subject (ADR-0296 §SD5); the meaning of a downstream domain's action names, which only the consumer knows |
| reconstruct — *agentic* | a conversation; a task | `dm_trail_timeline` (`parent-ref`, `seq`), `agg_trail_llm_conversations`, `agg_trail_llm_tasks`, `agent_run_entries` for the journal side | a correlation index (scan by conversation); child-conversation delegation unrecorded |
| review grants and use — *agentic* | a task | `dm_trail_llm_agent_grants`, `agg_trail_llm_agent_action_outcomes`, `agent_grants` (this run only) | a before/after view of a widening (consecutive rows must be diffed); grant status for a reopened conversation |
| account for a period or an app | a time range; an app | `dm_trail_*` per kind, binned by kind, domain and action, subject, outcome (bus requests inside `dm_trail_audit_events`), `app_runs`/`app_audit`, `QueryRun` rows, `CoverageSample`; sysmetrics and presence rows on the same table joined by time, not key | an insertion-order column; a driver stamp to tell a scene run from a person's; `Origin` on the other facts-bound stores |
| verify the record | a writer run (seals); a journal run | `evidence_checks`, `anchors`, `VerifySeals`, `agentrun.Verify` | a stored memory of heads seen (the anchor store is the durable half); per-entry signatures |
| explain a row | a row id | `componentview` over the decoded components, `fieldview`, bodies when kept | a person-entered reason; a label on trail rows beyond tier; a description of a downstream domain's vocabulary |
| review by exception | any of the above | the `status`/outcome columns of `dm_trail_timeline`, `audit-gap` rows, `attention_queue` live | the predicates themselves (laws L7, L28 unwritten); a store for the reviewer's marks (O5) |

### 8.2 The shapes

**O1 — A system app over the trail marts.** A new app under the operations
contract (ADR-0345 is the template), reading `dm_trail_timeline` and the
`dm_trail_*`/`agg_trail_*` marts over the bus with a time range from the
picker, drawing a `recordtable` of rows under a histogram strip, with a
detail pane (`componentview` + `fieldview`) and an evidence pane reading
`evidence_checks` and `anchors` for any writer run. It serves every kind,
and its overview is the one every kind reduces to: a binned histogram by
kind, and within a kind by subject, domain and action, or outcome — which
is all that bus traffic, log rows, metrics samples, query runs and a
records-of-processing domain have to show, since none of them carries a
cause chain. The lanes/span-tree view and the grant history pane are tabs
that mean something only where the selected key has a chain (a task, a
conversation) and are absent, not empty, elsewhere. Journal entries join
through `agent_run_entries` or, later, a server-side plain-shaped view
(ADR-0309 §SD8). Consequences: one surface for every kind, every app's rows
and every run's, which Analytics and the log viewer are not; scenes assert on the table and
the detail pane; the catalog is queries over the view plus `view`-effect
commands. Costs: the server-windowed grid, the lanes widget and the
verdict vocabulary (§8.4) do not exist; reads by conversation scan until an
index or the storage switch lands (ADR-0296 §SD5, §SD9); and a second
coordinator-state reader beside `apps/chat` has to be kept from becoming a
second writer (agent-harness S16).

**O2 — A pane inside chat, per conversation.** The agentic lens only.
Extend Analytics and the transcript's steps into a run inspector: a lanes
view of this conversation's calls, actions and caused work, grant history
for its task, and the chain verdict, reading the trail instead of the rings
so a reopened conversation shows its grants (ADR-0283 Update 2026-10-08).
It reaches the rows that carry `Conversation` and nothing else on the
table. Consequences: the person is already there; the conversation is the
natural case; the agent-schema boundary is already drawn for this app.
Kill-reasons: it never answers the cross-conversation and cross-run
questions (a window's or a period's account; who touched what across
tasks), nor any question about a non-agentic kind — bus requests, egress
outside a task, logs, metrics, a downstream domain; chat render code already
writes coordinator state (agent-harness S16), so growing it widens the
surface the tree wants to keep narrow; it leaves the log viewer, play
History and watchbill as separate slices.

**O3 — SQL-only, through play.** No new app: a set of saved queries or
applets ([ADR-0132](../adr/0132-sqlapplet-sql-defined-applets.md)) over the
marts, read in play's Table pane with `recordtable` grouping
(ADR-0336) and the Chat pane for bodies; an *Open in play* from chat already
exists. Consequences: nothing to build in the widget set; the marts are
leeway-shaped for exactly this; the reviewer has the full query language,
which is what the 2025 Annex 11 draft §12.4 asks for. Kill-reasons: no lanes,
no span tree, no verdict glyph, no grant diff — a table and a histogram are
all play draws; the reviewer composes SQL to pivot, which §6.3 shows every
product avoids; the evidence relations are in-process and play reads them
only through `keelson()`; a scene can drive it but the review is not a
repeatable artefact.

**O4 — A viewer widget set, hosted by several apps.** Build the missing
widgets (§8.4) as `widgets/` packages — a lanes/span-tree, a field diff, a
sparkline strip, a windowed table — and compose them in whichever app has
the context: chat for a conversation, the app center for a window, play
for a query run, watchbill for a job. Consequences: each surface shows the
same rows the same way; the widgets are testable under the demo registry
([ADR-0057](../adr/0057-demo-registry-and-drivers.md)). Kill-reasons: no
single place answers "what happened in this run"; every host needs the
same data layer, so the windowed-fetch pattern is duplicated or becomes a
shared package anyway; the widget set is the long pole and gates every host
on it (AGENTS.md's "descope rather than gate").

**O5 — A review ledger, not a viewer.** Treat review itself as the product:
the person's marks (reviewed, dismissed-as-benign, flagged, with a reason)
written as `AuditEvent` rows in a `review` domain, an exception-rule set
that produces a short list, and a minimal table to clear it. The most
general of the five shapes: a mark attaches to a row id and asks nothing of
the row's kind, so a bus request, a records-of-processing event and a tool
call are marked alike, and it is the one shape whose core the regulated
non-agentic case requires outright. Consequences: it meets the doctrine of
§3.3 (review by exception, evidence of review) directly, and OWASP T10's
warning about undifferentiated feeds; the marks are on the same trail as
what they review. Kill-reasons: it presupposes
rules the tree has not written (laws L7, L28 unwritten — ADR-0309 Update
2026-10-25; moderator-set levels deferred — ADR-0323 §SD8); it does not
answer reconstruction or accounting; a review mark is a *write* through a
system app, which ADR-0345 scopes to `view` effects, so it needs its own
decision on effect and grant.

O1 and O5 compose: O5's marks are a pane and a catalog entry of O1, and O2's
per-conversation view is O1 scoped to one correlation key — a conversation
— and opened from chat *(inference)*.

### 8.3 Against the criteria

| | O1 system app | O2 chat pane | O3 SQL via play | O4 widget set | O5 review ledger |
|---|---|---|---|---|---|
| C1 serves all five reader tasks | ++ | − (one conversation) | + (no verify, no lanes) | + (per host) | −− (review only) |
| C2 crosses runs and apps | ++ | −− | ++ | + | + |
| C10 serves non-agentic kinds and downstream domains | ++ (histogram by kind, subject, outcome; vocabulary unknown) | −− (`Conversation` rows only) | ++ (every mart; vocabulary unknown) | + (a host per kind only where an app has the context) | ++ (a mark is kind-agnostic) |
| C3 meets sort/search/export wording (§3.2) | + | − | ++ | + | − |
| C4 meets review-by-exception and evidence of review (§3.3) | + (with O5) | − | − | − | ++ |
| C5 shows verification states (§2.2) | ++ | + | − | + | − |
| C6 scenes can assert on it | + (table, detail) | + | + | − (painted) | + |
| C7 widget work required | −− | − | ++ | −− | + |
| C8 stays inside the operations contract | ++ | + | ++ | + | − (a write) |
| C9 avoids a second coordinator-state path | ++ | −− | ++ | + | + |

No recommendation is made; the dialogue decides. The table says where each
shape pays and where it is killed.

### 8.4 What each shape needs from the widget set

Named against §2.7's missing list, with the shape that needs it. Two
groups: **universal** widgets serve a row of any kind — the windowed table,
the histogram and sparkline strips, the calendar heatmap, the verdict
glyphs, the field diff, the tier→tone mapping, the accessibility projection
— and **agent-shaped** ones mean something only where a cause chain exists
— the lanes with connectors, and the grant diff as one use of the field
diff.

- **Lanes / span tree** (agent-shaped; O1, O2, O4): caller-fixed lanes (one per actor —
  person, coordinator, each window, the service), intervals from `ts` +
  `ElapsedMs`, points for durationless rows, parent→child connectors from
  `parent-ref`, cross links for `Request`→decision and `Approval`, a
  critical-path shade (§5.4), a minimap. The existing `timeline` has the
  axis, the brush and the selection kinds; its lanes are packing-derived,
  so either a lane-assignment seam or a new widget. Painted, so it needs
  an accessibility projection — at minimum the selected row's text and a
  per-lane summary — or scenes cannot assert on it (ADR-0283 Consequences).
- **Field diff** (universal; O1, O2, O4): two records in, a per-field
  difference layer out (§5.6): changed, added, removed, with TreeVersity's
  borders and a signed glyph for numeric change; for any kind, two rows of
  one subject; for a grant, consecutive `AgentGrant` rows; for an adhoc
  dataset, two `Revision`s; for an `AuditEvent`, its `Attr*` arrays; for
  text bodies, the LCS that lives in chat (2026-10-30), lifted. `codeview` has no diff mode.
- **Sparkline / small multiples** (universal; O1, O4): counts per bin per
  kind, subject, domain and action or outcome, fed by a `GROUP BY toStartOfInterval` or `sparkbar`; `implot`
  bars can stand in for one strip but not for a column of them in a table
  row; `capinspector`'s strip is the only precedent and is private.
- **Calendar heatmap** (universal; O1): day × hour counts for off-hours review (§5.5);
  `heatmapscroll` is a streaming texture, not a calendar.
- **Server-paged windowed table** (universal; O1, O3 partially, O4): a
  `recordtable`
  host that issues `(from, to, limit)` or key-range reads as the person
  scrolls and brushes, replacing the record under `CellsKey`; nothing under
  `widgets/` does this; play's "manual-plus-on-reveal" is the nearest
  pattern.
- **Verdict glyph vocabulary** (universal; O1, O2, O5): one glyph and one
  tone each for
  holds / broken-at / gap / fork / unanchored / anchor-pending, each with a
  visible reason and the law it comes from (§7.4; `badge` carries tone, the
  mapping does not exist). Trust-indicator research says a passive badge is
  unseen (§6.10), so an unverified state should block or demand action, not
  colour a cell.
- **Tier → tone mapping** (universal; all): `CLEAR`…`RED` onto the six
  semantic roles,
  with the label text beside the colour (ADR-0031 §SD5); nothing defines it
  (ADR-0311 §SD7).
- **Accessibility exposure of painted canvases** (universal; O1, O2, O4):
  a text projection registered with the carrier tree for `graphview`, `timeline`,
  `implot` and any lanes widget, so `read`/`expect` see what is drawn.

### 8.5 Feeds: relations and marts

Anything historical comes from the marts over the bus; anything live and
process-local from `keelson()`; the join is the viewer's *(inference)*.
`dm_trail_timeline` is the spine (context columns, `parent-ref`, `seq`,
`status`, `headline`); per-kind marts for the detail pane;
`agg_trail_llm_*` for conversation and task summaries;
`dm_trail_audit_events` for every `AuditEvent` domain — `bus` requests,
`attention` level changes, a downstream consumer's — and `app_audit` for
the bus fold; `dm_trail_http_fetches` for egress; `QueryRun` rows for what
a query did, delegated or not; sysmetrics and watchbill presence rows on
the same table, selected by `Origin` like every other kind since
2026-10-30 (ADR-0277 Updates); `agent_run_entries` and
`agent_verdicts` for the journal, bounded to the newest runs until a
server-side view exists; `evidence_checks`, `anchors`, `attention_queue`,
`windows`, `app_operations` live. The viewer's own queries are stamped like
play's and land as `QueryRun` rows, and a dataset it publishes to play
lands as an `AdhocDataset` row — the viewer appears in the trail it shows.

### 8.6 Showing verification results

The states are fixed by §2.2 and the presentation by §7.4 *(inference)*: a
run's verdict is one line — **holds**, or **broken at ordinal N: reason**
with the `BreakE` name spelled out, or **gap: rows unnamed by any seal**,
or **fork: two seals follow one**, or **unanchored since ordinal N**. On a
chain, a break shades every later row as unverified rather than marking
the break alone; a gap is a range; an anchor is a marker on the ordinal
axis with the anchor's `scope`, `scheme`, `receipt` and `durable` flag.
The viewer keeps what it has seen (the durable anchor store is the tree's
memory) and shows divergence from it. `keelson('evidence_checks')`'s rows
are the source of truth for the glyph, not a recomputation in the viewer.

### 8.7 Labels and tiers

Every projection carries the join of what it read (ADR-0311 §SD3); the
viewer is a projection for a person, so its panes carry the highest tier of
the rows drawn and say so *(inference)*. Bodies appear only where kept and
where the viewer's own grant admits them; `confined` and `tainted` are
drawn as what they are (unenforced flags from the views) and their
provenance stated. The agent schema does not change: a model reads
`my_run`, `my_conversation` and `agent_history`; the viewer's catalog is
for the person and for an agent *driving the viewer's view*, and quotes
nothing a model's principal could not already see — which is why its
queries over rows that may quote fetched content are marked `Untrusted`
as the log viewer's are. Attention levels are derived, not stored, so a
"highlight" column in the viewer is the host table's output and is labelled
as such.

### 8.8 The operations contract

Following ADR-0345 §SD1–SD2: queries `list_rows` (filters, window, limit;
`Untrusted`), `get_row`, `get_tree` (the span tree under a key that has
one — a task, a conversation),
`get_grants` (a task's grant history), `get_verdict` (a run's evidence
state), `get_view`; commands of effect `view` only — `set_range`,
`set_filter`, `select_row`, `set_lanes`, `set_follow`; resources `range`,
`filter`, `selection`. What no `keelson()` table carries is what the
catalog carries. O5's review marks are the one write and need their own
effect and grant decision (§9).

### 8.9 How a scene asserts on it

Scenes read the accessibility tree (ADR-0248). The table, the detail pane,
the verdict line and the grant diff are text and assertable as the
Analytics scenes assert on their table; the lanes view and any strip are
painted and need §8.4's projection or a sibling text summary the scene
reads instead. A scene that drives the viewer needs a fixture trail: a
scripted coordinator turn under a test grant, as
[chat-analytics.scene.md](../../apps/chat/scenes/chat-analytics.scene.md)
does, and a run whose chain is deliberately broken for the verdict states.
The driver stamp ADR-0343 defers is what would let the viewer itself show
that a run was a scene's.

### 8.10 Seams the shape needs, independent of which is chosen

Of §8.4's list, the server-windowed fetch — off the render thread through
`bgjob`, parameterised by `(from, to, limit)` and a cursor, feeding a
`recordtable` — is the one piece every shape but O3 needs and no package
has; the verdict vocabulary, the tier→tone table, the accessibility
projection and lane assignment on `timeline` follow it. Two seams are not
widgets: a **correlation read path** that is not a scan — the skip index or
the storage switch of ADR-0296, or a materialized view per key fed from the
Null source — and a **driver stamp** on the run row (ADR-0343 deferral), so
a viewer can separate scene runs from a person's.

## 9 Open forks for a design dialogue

The forks every kind shares come first; the last three concern only the
rows that carry a cause chain.

1. **One app or a pane.** O1's single surface against O2's locality in
   chat, where O2 reaches only the rows with `Conversation`; or O1 opened
   from chat scoped to a conversation as one key among those it accepts.
2. **Overview form per kind.** The human-factors evidence (§3.4, §3.5)
   favours a dense table with graphics as overlays; for the kinds with a
   cause chain the data is a span tree, for the rest a time-bucketed
   stream. Whether one overview serves every kind — a histogram by kind,
   subject or outcome over the table, lanes a tab that appears only where
   the key has a chain — or a kind may declare its own, and which is the
   default.
3. **Review marks.** Whether a reviewer's marks are recorded at all, as
   `AuditEvent` rows in a `review` domain, and under what effect and
   grant — a write by a system app is outside ADR-0345's scope.
4. **The correlation key set.** Which keys the viewer accepts as a case —
   run, window, bus subject, destination, query fingerprint, `AuditEvent`
   subject and principal, journal run, conversation, task — whether the
   set is fixed or read from the marts' columns, and what a downstream
   domain may add to it.
5. **Unknown vocabularies.** What the viewer shows for a downstream
   domain's `AuditEvent` rows whose domain and action names it does not
   know: the names as written beside outcome and subject; a description the
   consumer registers per domain; or the generic columns alone — and, if a
   description, where it lives.
6. **Exception rules.** Whether the viewer ships predicates (denied or
   failed outcomes, refusals, consent-admitted calls, widened grants, clock
   gaps, rows no seal names, novel kinds, domains or subjects per NIST's
   "newness") as named, visible rules, and where they live — moderator
   rules (ADR-0302), laws, or the viewer's own.
7. **Verdict presentation.** A line per run, a shaded range on the ordinal
   axis, or both; whether an unverified state blocks the detail pane or
   only marks it.
8. **The missing columns.** Whether a before/after pair and a
   person-entered reason are added to the trail (an ADR-0277 change) or
   derived in the viewer from consecutive rows and journal `asked`/
   `answered` entries.
9. **The correlation index.** Whether the viewer waits for the storage
   switch, adds the skip index, or installs an MV of its own per key, and
   who owns it.
10. **Windowed fetch semantics.** Key-range or `(from, to, limit)`; what
    happens to selection and `CellsKey` when the record is replaced under
    the grid; whether `recordtable` learns a cursor or the host does.
11. **Time zone and the second timestamp.** The picker's zone applies to
    `ts`; whether the viewer shows a second time (journal `At`, query
    `event_us`) when it diverges, and what it says when there is none.
12. **Bodies and retention.** What the viewer shows for a message whose
    body was never kept, ditched, or erased — a mark, a placeholder, or
    nothing — and whether the three read differently.
13. **Scenes for painted views.** A text projection the carrier tree
    exposes, or a sibling text summary the scene asserts on instead.
14. **Export.** Whether the viewer exports at all (the standards want
    sort-and-search preserved, not flat files), and if so as an ad-hoc
    dataset to play rather than a file.
15. **Rows of the retired `audit` kind.** Whether a viewer ever reads the
    bus-request rows written before 2026-10-30, which no view composes, or
    they are dropped with the table.
16. **Rows or intents** (agentic). Whether the default row under a task is
    a trail row or an intent-level group (one `AgentAction` key with
    everything it caused folded under it, as SAP's header/items and
    LangSmith's trajectory do), with raw rows one click away.
17. **The identity chain** (agentic). How person → coordinator window →
    task → model call → tool call → window acted on is drawn, given no
    surveyed product draws it: a breadcrumb per row, a lane per actor, or
    PROV's pentagons.
18. **Lanes on `timeline` or a new widget** (agentic). Extend the lane
    packer with caller-fixed lanes and connectors, or write a span-tree
    widget on the same axis and brush.

## 10 Sources and incidents

Each pass's URL list is reproduced from its report; the pages below were
fetched and read unless marked. Pages that returned 404, 403, 429, timed
out or were deliberately not opened under the clean-room rule are listed
under incidents.

### Visualization techniques

Event sequences: arxiv.org/abs/2006.14291 and ar5iv.labs.arxiv.org/html/2006.14291
(Guo et al. survey); arxiv.org/abs/2408.04752 (Zinat et al.);
doi.org/10.1109/tvcg.2016.2539960 (Du et al., via OpenAlex);
hcil.umd.edu/eventflow; cs.umd.edu/~ben/Monroe2013Temporal.pdf and
doi.org/10.1109/tvcg.2013.200; doi.org/10.1109/tvcg.2012.225 (Outflow);
doi.org/10.1109/tvcg.2014.2346682 (DecisionFlow);
doi.org/10.1145/2557500.2557508 (Frequence); doi.org/10.1109/tvcg.2017.2745320
(EventThread); doi.org/10.1111/cgf.13208 (CoreFlow);
doi.org/10.1109/tvcg.2017.2745083 (Sequence Synopsis);
ar5iv.labs.arxiv.org/html/1710.06291 (Mathisen and Grønbæk);
arxiv.org/abs/2108.03043 (Sequen-C); doi.org/10.1145/2678025.2701407 (CoCo);
doi.org/10.1109/tvcg.2024.3382760 (IVESA); doi.org/10.1109/tvcg.2021.3050071.
Provenance: w3.org/TR/prov-dm, w3.org/TR/prov-o, w3.org/TR/prov-primer;
usenix.org/legacy/event/tapp11/tech/final_files/MackoSeltzer.pdf;
arxiv.org/abs/1504.02616 (Moreau);
vistrails.org/usersguide/v2.2/html/version_tree.html;
doi.org/10.1145/1142473.1142574; vdl.sci.utah.edu/publications/2020_visshort_trrack;
doi.org/10.1111/cgf.14035; arxiv.org/abs/2410.11011;
cise.ufl.edu/~eragan/provenance-designs.html. Process mining: Song and van
der Aalst 2007 (TU/e record via OpenAlex); pypi.org/project/vispm;
vdaalst.com/publications/p1101.pdf and doi.org/10.1016/j.procs.2019.12.189;
fluxicon.com/book/read/simplification and /perspectives;
help.celonis.com/cpm47/en/process-explorer. Traces:
opentelemetry.io/docs/specs/otel/overview and /docs/concepts/signals/traces;
grafana.com/docs/grafana/latest/explore/trace-integration;
jaegertracing.io/docs/latest/frontend-ui;
docs.logz.io/user-guide/distributed-tracing/compare-traces;
brendangregg.com/flamegraphs.html; logparser.readthedocs.io (Drain);
doi.org/10.1109/issre59848.2023.00071; doi.org/10.1109/vizsec.2018.8709230.
Security: doi.org/10.1109/infvis.2004.1 (Ghoniem et al.);
doi.org/10.1109/vizsec.2018.8709212 (ViC2ES); archive.cps-vo.org/node/85381;
arxiv.org/abs/1311.7259; uclab.fh-potsdam.de (Visual Filter);
ll.mit.edu record (Staheli et al.). Diff:
graphicsinterface.org/proceedings/gi2011/gi2011-24;
cs.tufts.edu/comp/177/docs/treeversity.pdf;
doi.org/10.1109/access.2023.3336859. Scale: vldb.org/pvldb/vol7/p797-jugel.pdf
(M4); doi.org/10.1111/cgf.12129 (imMens); doi.org/10.1109/tvcg.2013.179
(Nanocubes); doi.org/10.31219/osf.io/szpqm (Falcon).

### Product UX paradigms

AWS: docs.aws.amazon.com/awscloudtrail/latest/userguide/ — view-cloudtrail-events,
view-cloudtrail-events-console, cloudtrail-event-reference-record-contents,
cloudtrail-event-reference-user-identity, cloudtrail-lake. Google:
docs.cloud.google.com/logging/docs/view/logs-explorer-interface and
/audit/understanding-audit-logs; knowledge.workspace.google.com/admin/reports/admin-log-events
and /admin/security/group-results-by-attribute-when-searching;
support.google.com/docs/answer/190843. Azure:
learn.microsoft.com/en-us/azure/azure-monitor/essentials/activity-log.
GitHub: docs.github.com enterprise-cloud audit-log searching, accessing,
exporting; docs.github.com repositories viewing-and-understanding-files.
Okta: help.okta.com reports_syslog.htm and syslog-filters.htm;
developer.okta.com/docs/reference/system-log-query. Slack:
docs.slack.dev/admins/audit-logs-api; slack.com/help/articles/360000394286.
Purview: learn.microsoft.com/en-us/purview/audit-search. Atlassian:
support.atlassian.com security-and-access-policies view-audit-log-activities,
alql-fields, what-is-advanced-search-in-audit-log, alql-keywords,
track-organization-activities-from-the-audit-log;
confluence.atlassian.com Page History and Page Comparison Views. Microsoft
365: support.microsoft.com view-previous-versions-of-office-files. Notion:
notion.com/help/duplicate-delete-and-restore-content. Wikipedia:
en.wikipedia.org Help:Page_history, Help:Diff, Wikipedia:WikiBlame;
meta.wikimedia.org VisualEditor newsletter Feb 2018. Figma:
help.figma.com/hc/en-us/articles/360038006754. GitLab: docs.gitlab.com
git_blame, compare_revisions, branches. Gerrit:
gerrit-review.googlesource.com/Documentation/user-review-ui.html. Data
platforms: dolthub.com/docs sql-reference dolt-system-tables;
docs.datomic.com/pro/time/filters.html; docs.xtdb.com/about/time-in-xtdb;
docs.snowflake.com data-time-travel and ui-snowsight-activity;
docs.delta.io/latest/delta-utility.html;
docs.datahub.com/docs/features/feature-guides/lineage. Regulated: fda.gov
media/119267/download; downloads.denovosoftware.com manual (snippet);
test.limswiki.org LabLynx (snippet); knowledge.zapliance.com and
saptechnicalguru.com (third-party, snippet). Observability and agent
traces: docs.datadoghq.com/account_management/audit_trail;
grafana.com/docs/grafana/latest/explore/logs-integration;
elastic.co/docs/explore-analyze/discover, discover-get-started,
document-explorer; docs.langchain.com/langsmith/observability-concepts and
/annotation-queues; langfuse.com/docs/observability/data-model,
/features/observation-types, /features/sessions, /docs/tracing;
arize.com/docs/phoenix/tracing what-are-traces and llm-traces;
docs.braintrust.dev/guides/traces and /human-review;
opentelemetry.io/docs/specs/semconv/gen-ai (relocation notice) and
/blog/2025/ai-agent-observability. Ledgers: etherscan.io transaction page
(live); info.etherscan.com/understanding-an-ethereum-transaction;
crt.sh?q=example.com (live); docs.sigstore.dev/logging/overview and
/rekor/CLI. HCI practitioner (snippet only): saasui.design,
becoming-a-teacher.design-history.education.gov.uk, saasframe.io.

### Standards and reviewer requirements

GxP: law.cornell.edu/cfr/text/21/11.10; fda.gov Data Integrity Q&A page and
media/119267/download; health.ec.europa.eu annex11_01-2011_en_0.pdf;
gmp-compliance.org annex-11-draft-first-analysis, the draft guideline page
and the consultation PDF; propharmagroup.com Annex 11 comparison;
assets.publishing.service.gov.uk MHRA GxP data integrity guide (March 2018);
picscheme.org/docview/4234 (PI 041-1); ispe.org pharmaceutical-engineering
march-april-2026 audit-trail-review. Privacy: gdpr-info.eu art-30 and
art-15; fedlex.admin.ch FADP consolidated English PDF; datenrecht.ch and
lawbrary.ch (DPO Art. 4, snippet); ico.org.uk law-enforcement logging pages;
ncsc.gov.uk Principle 3; law.cornell.edu/cfr/text/45/164.312;
helsenorge.no/en/patient-records. Security: nvlpubs.nist.gov SP 800-92
(2006) and SP 800-92r1 ipd; csrc.nist.gov SP 800-92r1 page and glossary;
csf.tools NIST SP 800-53 r5 AU-3, AU-6, AU-7, AU-9, AU-10, AU-12 and CIS
CSC 8; isms.online ISO 27002 8.15, 8.16, SOC 2 CC7.2, ISO 42001 Annex A;
cisecurity.org/controls/audit-log-management; ce.prod.cloudaware.com PCI DSS
v4.0.1 10.2, 10.2.2, 10.3, 10.4, 10.5. Financial: pcaobus.org AS 1215 pages
(403; snippet). Schemas: schema.ocsf.io/1.5.0 categories, api_activity,
entity_management, actor, metadata; elastic.co ECS event and user field
pages; microfocus.com CEF implementation standard ch. 1 and extension
dictionary; ibm.com LEEF overview and predefined attributes;
opentelemetry.io logs data-model and GenAI attribute registry;
sigmahq.io/docs/basics/rules.html; cloudevents.io;
rfc-editor.org/rfc/rfc5424; dicom.nema.org part15 sect_A.5;
rfc-editor.org/rfc/rfc3881; w3.org/TR/prov-dm; xes-standard.org;
tf-pm.org about-xes; ocel-standard.org and the 2.x specification PDF.
Tamper evidence: rfc-editor.org/rfc/rfc6962; transparency.dev and
/verifiable-data-structures; docs.sigstore.dev logging overview,
verify-release, sharding; blog.sigstore.dev/rekor-v2-ga;
boto3.readthedocs.io qldb get_revision (snippet); immudb docs mirrors
(snippet); learn.microsoft.com confidential-ledger architecture,
write-transaction-receipts, verify-write-transaction-receipts. AI
oversight: artificialintelligenceact.eu articles 12, 14, 19, 26;
nvlpubs.nist.gov NIST.AI.100-1.pdf; genai.owasp.org LLM06:2025 and the
Agentic AI Threats and Mitigations landing page and PDF (v1.1, December
2025); cdn.openai.com practices-for-governing-agentic-ai-systems.pdf;
arxiv.org/pdf/2604.23338 (snippet).

### Implementation methods

Storage: martinfowler.com/eaaDev/EventSourcing.html;
learn.microsoft.com/en-us/azure/architecture/patterns/event-sourcing;
learn.microsoft.com/en-us/sql/relational-databases/tables/temporal-tables;
mariadb.com/docs/server/reference/sql-structure/temporal-tables/system-versioned-tables;
docs.xtdb.com/concepts/key-concepts.html and /reference/main/sql/txs.html;
wiki.postgresql.org/wiki/Audit_trigger_91plus; pgaudit.org;
debezium.io/documentation/reference/stable/connectors/postgresql.html.
ClickHouse: clickhouse.com/docs guides/best-practices/sparse-primary-indexes,
use-cases/observability/schema-design, use-cases/time-series/basic-operations,
engines replacingmergetree, collapsingmergetree, versionedcollapsingmergetree,
sql-reference/statements/select/from, /limit-by, /sample,
aggregate-functions argmax, parametric-functions, sparkbar,
quantiletdigest, largestTriangleThreeBuckets, window-functions,
materialized-view/incremental-materialized-view, data-types newjson,
dynamic, variant, map, lowcardinality, statements/explain,
use-cases/observability/clickstack/overview; kb.altinity.com pick-keys,
final-clause-speed, lag-lead; oneuptime.com 2026-03-31 posts (third-party,
snippet). Diff: xmailserver.org/diff2.pdf (Myers); git-scm.com/docs/git-diff;
bramcohen.livejournal.com/73318.html; rfc-editor.org/rfc/rfc6902 and
rfc7386; arxiv.org/pdf/cs/0604037 and 1501.00611 (snippet); labri.fr GumTree
slides (snippet). Tamper evidence:
usenix.org/legacy/event/sec09/tech/full_papers/crosby.pdf;
rfc-editor.org/rfc/rfc9162; transparency.dev/verifiable-data-structures;
docs.sigstore.dev/logging/overview and /logging/sharding;
rfc-editor.org/rfc/rfc3161; opentimestamps.org; nvlpubs.nist.gov SP 800-92.
Rendering: vldb.org/pvldb/vol7/p797-jugel.pdf; idl.uw.edu/papers/immens and
/papers/falcon; highscalability.com Nanocubes summary and the SLAC PDF
mirror (snippet); microsoft.com/en-us/research cockburn-ComputingSurveys09.pdf;
arxiv.org/abs/1607.05162; vaclab.unc.edu tvcg_2014_stolper;
ag-grid.com/javascript-data-grid/dom-virtualisation; docs.rs egui
ScrollArea. Correlation: w3.org/TR/trace-context;
opentelemetry.io/docs/concepts/signals/traces; dash0.com, docs.galileo.ai,
axiom.co GenAI explainers (snippet); docs.rsyslog.com RepeatedMsgReduction
index; developer.chrome.com/docs/devtools/console/reference. Reduction:
csrc.nist.gov/glossary/term/audit_reduction_tools;
csrc.nist.gov/pubs/sp/800/92/final; opentelemetry.io/docs/concepts/sampling;
arxiv.org 2407.01896, 2511.07458, 2510.24031 (snippet).

### Human review evidence

SOC: usenix.org sec22-alahmadi.pdf; adamdoupe.com matched-and-mismatched-socs-ccs2019.pdf;
usenix.org soups15-paper-sundaramurthy.pdf; securedecisions.com D'Amico
and Whitley abstract; ll.mit.edu 2014-Staheli-Cyber-Visualization-Evaluation-VizSec.pdf;
usenix.org laser2016 Garneau abstract; api.openalex.org doi:10.1145/3670009;
arxiv.org/abs/2605.08316. Forensics: dfrws.org and dspace.lib.cranfield.ac.uk
(Hargreaves and Patterson; 403, snippet); opus.bibliothek.uni-augsburg.de
124207.pdf (SoK 2025); plaso.readthedocs.io Using-psort;
timesketch.org/guides/user/basic-concepts;
digitalcommons.newhaven.edu Timeline2GUI record. GxP review: ispe.org
article as above; assets.publishing.service.gov.uk MHRA guide;
fda.gov/media/119267/download; fda.gov warning letters Tismor (2019),
Chongqing Pharma Research Institute (2017), Qinhuangdao Zizhu (2017).
Healthcare: arxiv.org/abs/1109.6880 (Fabbri and LeFevre); EuropePMC REST
records for Fabbri 2013, Hedda 2017, Boxwala 2011, Kim 2011. Code history:
cs.kent.edu codoban15.pdf; microsoft.com/en-us/research Tao et al. page
(snippet); research.tudelft.nl Pascarella record; arxiv.org/abs/1805.10978;
repositorio.uc.cl eye-tracking record (403, snippet);
cs.ubc.ca rationalizer-paper-msr2011-77.pdf. Agent oversight:
microsoft.com/en-us/research/?p=1134933 and arxiv.org/html/2503.02068
(AGDebugger); arxiv.org/html/2504.08696v1 (SeaView);
arxiv.org/html/2402.08995 (AgentLens); ijcai.org/proceedings/2026/0987.pdf;
arxiv.org/abs/2602.06593 (AgentStepper); arxiv.org/html/2602.16844v1
(Grunde-McLaughlin et al.); tail.cc.gatech.edu zhou-chi-2026;
microsoft.com/en-us/research/?p=1169839 (Pista); arxiv.org/html/2604.13536
(YoloFS); anthropic.com/engineering/claude-code-auto-mode;
arxiv.org/html/2608.23642 and govinfosecurity.com summary (Mitchell et al.);
arxiv.org/pdf/2606.08919 (Turan); code.claude.com/docs/en/permissions;
openai.github.io/openai-agents-python/human_in_the_loop. Provenance
comprehension: faculty.cc.gatech.edu ragan-vast-2015.pdf;
idl.uw.edu/papers/graphical-histories;
vis.khoury.northeastern.edu DiBartolomeo2020EvaluatingEffectTimeline;
cspages.ucalgary.ca Tam and Greenberg abstract.

### Tree pass

Read freely: the files linked from §2, the ADRs cited by number, and
[agentic-layer](../explanation/agentic-layer.md),
[facts-bound-record-stores](../explanation/facts-bound-record-stores.md),
[leeway-sql-read-surface](../explanation/leeway-sql-read-surface.md),
[query-observability](../explanation/query-observability.md),
[adhoc-datasets](../howto/adhoc-datasets.md),
[imzero2-fetchers](../skills/imzero2-fetchers/SKILL.md),
[imzero2-drive](../skills/imzero2-drive/SKILL.md),
[agent-harness](./agent-harness.md).

### Incidents

- **Refused or unreachable (HTTP 403/404/429, DNS, timeout):** ecfr.gov
  (redirected to an unblock page; LII used instead); pcaobus.org (403; AS
  1215 is snippet only); hhs.gov January 2017 newsletter PDF (403);
  edoeb.admin.ch FDPIC logging recommendation (404 at both URLs);
  docs.aws.amazon.com/qldb developer guide (404, consistent with the
  service's retirement); docs.immudb.io (404 at several paths, redirect to
  the marketing site); api.slack.com/admins/audit-logs (403);
  developer.salesforce.com SetupAuditTrail (403); help.okta.com
  reports-system-log (404; two sibling pages loaded); the Okta LogEvent
  schema page (rendered only an overview); support.google.com/a/answer/7587832
  (navigation only); notion.com/help/page-history (404);
  support.atlassian.com confluence-cloud view-page-history (404; Data Center
  docs used); help.sap.com (two pages; JS-rendered body, title only);
  digitalhealth.gov.au (timeout); support.healthhub.sg (403);
  helpcenter.agaramtech.com (404); nngroup.com/articles/timelines (404, a
  guessed URL); hcil.umd.edu/lifeflow (404); idl.cs.washington.edu immens
  redirect (404; OpenAlex abstract used); researcher.ibm.com (redirect to
  404); dl.ifip.org → ifip.hal.science and hal.science GumTree (bot
  protection); napier-repository.worktribe.com (403); sciencedirect.com DFG
  paper (403; author PDF used); skemman.is LTTB thesis (403);
  altinity.com arrays part 2 (504); docs.rsyslog.com RepeatedMsgReduction
  (navigation only); arxiv.org/abs/1308.1444 (wrong paper, not Nanocubes);
  pmc.ncbi.nlm.nih.gov (bot check; EuropePMC API used); ir.library.oregonstate.edu
  (bot check; course mirror used for Codoban); cse.ust.hk Tao PDF (403);
  dfrws.org and dspace.lib.cranfield.ac.uk (403); repositorio.uc.cl (403);
  dibt-research.unimol.it (404); docs.langchain.com LangGraph HITL (404 /
  429); api.semanticscholar.org (429); healthit.gov Intermountain testimony
  (404); vast.cs.umass.edu (DNS); vis.stanford.edu (connection refused; IDL
  mirror used); link.springer.com (auth wall) and iris.unict.it (403) for
  Romeo and Conti; discovery.ucl.ac.uk (403) for Kirlappos; openai.com
  practices page (403; CDN PDF used); fedlex.admin.ch HTML (JS notice; PDF
  used); presencis.com (404); xes-standard.org definition 2.0 PDF (404).
- **Truncated reads:** w3.org/TR/prov-dm and /prov-o (first 100k
  characters); ar5iv Guo survey (Section VI cut); RFC 9162 (auditing and
  security sections not reached); the Debezium connector page (first 200k
  of 321k characters); the AGDebugger, AgentLens, Mitchell et al.,
  Grunde-McLaughlin et al. and YoloFS HTML pages (first 100k characters;
  limitations and appendices not reviewed).
- **PDFs returned as binary by the fetcher** (Annex 11 2011, the 2025
  draft, MHRA 2018, PI 041-1, FDA Q&A, SP 800-92, the OCEL 2.x
  specification, Monroe 2013, Orbiter, TreeVersity, M4, Crosby and Wallach,
  Myers, Cockburn et al., Kokulu, Alahmadi, Sundaramurthy, the SoK 2025,
  Turan, Ragan, Staheli, Codoban, Bradley and Murphy) were text-extracted
  locally from the saved files in an isolated directory and read; quotes
  from them are a pass's reading of the page.
- **OpenAlex mismatches:** the API returned abstracts for most titles but
  the wrong paper for LifeFlow (CHI 2011), MatrixWave, LogMine, CareFlow,
  Staheli et al. and Blinder et al. (abstract null); those entries are
  marked *(snippet)* or *(recalled)* above or left out.
- **Deliberately not opened under the clean-room rule:** every GitHub and
  GitLab repository page, code view, blob, raw view, PR or issue that
  surfaced — openzipkin/zipkin, logpai/logparser, Caleydo/knowledge-pearls,
  jaegertracing/jaeger-ui #513, grafana/grafana #35531, the GitLab issue
  233282 and CONFCLOUD-84276 (their prose would have been permitted; only
  snippets were used), open-telemetry/semantic-conventions-genai (the
  moved GenAI specification), cloudevents/spec, google/trillian (the
  whitepaper and README), google/keytransparency, sveinn-steinarsson/
  flot-downsample, a Timesketch docs fork, Rekor's `openapi.yaml` and
  `.proto` files; package registry code tabs (pypi pages other than vispm,
  Hugging Face and tessl.io copies of pm4py docs); search.sigstore.dev (a
  live web app); patent documents surfaced by search. Where a fetched
  documentation page carried SQL or code (ClickHouse docs, the Postgres
  wiki, M4 figures, vendor HITL pages), only documented names, signatures
  and described semantics were recorded.
- **Pages dated after the passes' training data**, read with that in mind:
  the Annex 11 2025 draft (July 2025), the ProPharma comparison (December
  2025), the ISPE article (March/April 2026), Rekor v2 GA (October 2025),
  OWASP ASI v1.1 (December 2025), Azure confidential ledger docs (October
  2026), the OCEL 2.1 specification (dated October 2026), NCSC Principle 3
  (June 2025), ICO logging (November 2025), the PCAOB AS 1215 amendment
  (effective December 2026), the Jaeger UI docs (October 2026), the GCP Logs
  Explorer and Workspace pages (October 2026), and the 2026 agent-oversight
  preprints.
- **Internal disagreements between passes** are noted in place: the
  ClickHouse docs list `lag`/`lead` as supported window functions where
  a third-party post says ClickHouse lacks a bare `LEAD()` (the docs are
  trusted); Okta's own pages state 90-day retention in one place and reject
  a `since` "over 180 days prior" in another; a vendor claim that the Annex
  11 draft fixes monthly or quarterly review frequencies is contradicted by
  the draft text read (§12.8 is risk-based); secondary coverage quotes a
  97% approval rate and a "13.6%" catch rate that do not appear on the
  Anthropic page read.
- No pass reported a clean-room breach.
