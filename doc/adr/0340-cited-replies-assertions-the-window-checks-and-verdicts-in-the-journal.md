---
type: adr
status: proposed
date: 2026-10-27
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0340: Cited replies — a claim about data is a read-only assertion the window checks, and a verdict is a journal row

## Context

Everything the cell holds over an agent governs the *handling* of what it
does: grants and operation classes
([ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)),
labels ([ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md)),
exits ([ADR-0312](./0312-exits-sinks-from-the-cell-boundary-approval-tiers-and-exit-budgets.md)),
the moderator ([ADR-0302](./0302-a-built-in-moderator-loop-signals-and-an-escalation-ladder.md))
and the journal ([ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md)).
None of it says whether a reply's statement about data is true. The model
runs a statement in play, reads a sample of the result, and writes
"revenue fell 12 % in March". The journal keeps the statement, the rows the
model saw and the sentence, and nothing ties the sentence to the rows. A
misread column, a dropped filter or a wrong rounding produces a reply that
looks exactly like a correct one, and the artefact, the clipboard and every
exit carry it onward with the same confidence. The person checks by hand or
trusts.

The tree already holds most of what a check needs. Every tool call is an
`intent` and an `outcome` under one key
([ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) §SD6);
a query's outcome is the text the model was told, which for play's
`sample_rows` is at most 50 rows of cell text marked untrusted
([ADR-0270](./0270-play-operations-catalog-and-agent-limits.md) §SD1). Play
classifies every agent-caused statement before it is sent and runs it under
the agent limits (ADR-0270 §SD2). The projection is SQL over the journal and
the harness's laws are queries returning the rows that break them
([ADR-0309](./0309-context-projection-as-sql-over-the-journal-in-a-local-worker.md)
§SD6). The journal's closed enum reserves `candidate`, `verdict` and
`select` entries for fan-out (ADR-0308 §SD2) and nothing writes them; fan-out
itself is deferred "once a verifier exists for a purpose"
([ADR-0315](./0315-coordinator-tools-as-operation-specs-and-a-contract-for-stateful-tools.md)
§SD9), and an effectful operation may name a dry-run twin so a verifier can
judge a candidate without spending the effect (ADR-0315 §SD7). Query
observability records each run's normalised statement hash and result size
([ADR-0115](./0115-query-observability-data-plane-strategy.md)).

Outside the tree, the small open text-to-SQL agents that check their answers
do so by *number grounding*: every numeral in the answer must appear in the
last result. Research systems go further and translate a claim into a query
whose result is compared with the claimed value (CEDAR, ClaimDB; see
References). The first form is cheap and blind to derived figures; the
second is the shape this record takes, because the model here already
writes SQL over leeway handles.

## Design space (QOC)

**Question.** In what form does a reply's claim about data become
checkable?

**Options.**

- **O1 — Number grounding**: the harness lifts the numerals out of the
  reply and looks for each in the cells of the results the turn read.
- **O2 — Assertions as read-only SQL**: the model states each claim as a
  statement over the sources it read; the window that holds those sources
  executes it under its agent limits; the reply refers to the assertion.
- **O3 — A judge call**: a second model call under its own purpose reads
  the sentence and the rows and says whether the one follows from the
  other.
- **O4 — A structured reply**: the reply is a record in which every figure
  is a cell reference, and prose carries no numbers.

**Criteria.**

- **C1** — Deterministic and repeatable from the journal and the data: the
  same verdict on re-execution, no model in the loop.
- **C2** — Covers derived figures (a ratio, a difference, a rank), not only
  copied cells.
- **C3** — Reviewable by the person: the check itself is readable.
- **C4** — Cost in rounds and tokens per cited reply.
- **C5** — Reuses the enforcement points in place — the dispatcher, the
  agent limits, the labels — without adding a second one for the same
  statement.
- **C6** — Within reach of a modest local model.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | ++ | −− | ++ |
| C2 | −− | ++ | +  | −  |
| C3 | +  | ++ | −  | −  |
| C4 | ++ | −  | −− | +  |
| C5 | +  | ++ | +  | −− |
| C6 | ++ | −  | −  | −  |

O2 is taken. O1 is not discarded but demoted: a numeral the reply states and
no result or assertion accounts for is a *law* over the journal (SD5), not a
verifier, so its cheapness is kept and its blindness is not relied on. O3 is
the only option that reaches judgement in prose, and it is recorded as the
deferred second cut (SD9) because its verdicts are neither repeatable nor
reviewable. O4 changes the reply form every consumer reads and is rejected
on C5 alone.

## Decision

We will let the model **cite**: each claim about data in a reply is first
stated as an **assertion** — a read-only statement over the sources of
calls the model already made — through a `cite` tool; the **window** the
sources came from executes the assertion under its own agent limits through
a query operation, `check`; each result is a **verdict entry** in the run's
journal; the reply refers to its assertions by id; and the chat, the
artefact, the exit guards and the laws read verdicts as rows. Number
grounding becomes a law, not a verifier.

### SD1 — An assertion

An assertion names what it rests on, says what it claims, and gives the
statement that decides it:

| Field | Carries |
| --- | --- |
| `id` | the model's name for it, unique within the turn; the reply refers to it |
| `rests_on` | the tool-call ids, as the model saw them, of the query outcomes the claim is drawn from; the harness maps each to the coordinator's call key (ADR-0277 §SD6) |
| `says` | the sentence as the reply will state it |
| `form` | `value` — the statement returns one row of one cell, compared with `expect`; `holds` — the statement returns one boolean |
| `statement` | a read-only SQL statement over the sources of the cited calls, in the dialect and with the handles the cited result used |
| `expect` | for `value`: the figure as the model states it, with the precision it displays |

A `value` assertion **holds** when the observed cell, rounded to the
precision of `expect`, equals it: "12 %" holds for 12.4 and not for 12.6,
"12.4 %" holds for 12.41 only. The displayed precision is the tolerance, so
no tolerance constant exists. A `holds` assertion holds when its one
boolean is true. A statement returning more than one row or cell is
refused. The outcomes of a check are:

| Outcome | Meaning |
| --- | --- |
| `held` | the statement ran and the claim holds |
| `failed` | the statement ran and the claim does not hold; the observed value is recorded |
| `refused` | the gate of SD3 refused the statement, with its reason |
| `unchecked` | no window can run it: the cited call's window is gone, or `rests_on` is empty or names no query outcome |
| `moved` | reserved for a recheck (SD9): the statement ran and the observed value differs from the one first recorded |

### SD2 — The `cite` tool, and verify-then-write

`cite` is a tool of the coordinator's registry
([ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md)
§SD6, ADR-0315 §SD1) with effect `none`, not parallel-safe, a budget of
assertions per call and calls per turn (SD8; a refused call still counts,
since its intent was journaled), listed on `keelson('agent_tools')` like
the others. It takes a list of assertions and
returns, per assertion, its outcome and the observed value. The order within
a turn is **verify, then write**: the model calls `cite`, reads the
outcomes, and writes the reply knowing which claims held. The reply refers
to an assertion by its id in square brackets — `[a1]` — and the harness
lifts nothing out of the reply body; the references are read by the
renderer (SD6) and by law L36 (SD5). A failed assertion refuses nothing: the
model restates the claim as observed, asserts again within the budget, or
says what it found and what did not hold. A reply that states figures and
cites nothing is allowed; law L38 lists it.

The harness gates the journal side of each assertion before dispatch: every
`rests_on` id resolves to an `outcome` of this run whose `intent` is a
query-class call on an app window; all cited calls of one assertion are of
one window, which is the window `check` is dispatched to; an assertion that
fails this gate is `unchecked` with its reason and costs no dispatch.

### SD3 — `check`: the window runs the statement under its own limits

`check` is an external read in play's catalog (ADR-0270 §SD1): it takes
the id of the result an assertion rests on and the statement, and returns
at most one row as cell text, a digest of the statement in its canonical
form, the same digest of the statement that produced the result, and the
endpoint as the grant names it. In this cut the result is the window's
main result: a check against another id is a conflict naming the one
held, as `sample_rows` answers. It reads the result's provenance and
writes nothing — no buffer, no result, no pane, no mark on the window
(ADR-0270 §SD3) — and is marked untrusted, since the cell is the data's.
Its gate, in order:

1. the statement's bounds, and the endpoint among the grant's
   destinations, as the catalog probes require;
2. the agent limits of ADR-0270 §SD2, on the statement as it would be
   sent: the read class, the `keelson('…')` and endpoint destinations of
   the grant, `readonly = 2`;
3. **scope**: the tables the statement names are a subset of the tables
   the result's statement named, the assertion's own CTEs aside; a
   keelson table counts; a statement naming nothing is refused, so a
   tautology (`SELECT true`) cannot be asserted;
4. the signal parameters the result's run shipped, bound again;
5. a bound of one row, one kilobyte of cell text and the check's own
   timeout; a second row or a second column is refused.

The check runs on the window, not in the harness, because the endpoint, the
parameters and the limits are the window's: a verifier that executed
statements through the query engine itself would be a second enforcement
point for a statement the window already gates, which ADR-0269's one
dispatch seam exists to prevent. A `check` needs the destinations the cited
run needed, which the task holds because it ran it; once the task has ended,
`request_access` asks again as for any call. For this, play's `describe_result`
and `sample_rows` carry the result's id and, for the main result, the
digest of its statement, so the harness can name what a citation rests on
and a later reader can tell whether the result the claim was drawn from is
the one the window still shows.

### SD4 — Verdicts are journal rows

The `verdict` kind (ADR-0308 §SD2, ordinal 16) gets its first writer. The
harness appends one entry per assertion with `Key` the cite call's key,
`Name` the assertion's id and `Form` `check`, classified by
[ADR-0325](./0325-the-journals-canonical-form-is-the-canonical-wire-over-its-rows.md)
§SD1: a `Verdict` record on the ledger holds the form, the outcome, the
cited call keys, the result id, the two statement digests, the endpoint
destination and the reason; the content is the entry's own — the
assertion as the model wrote it is the entry's arguments, shredded as a
tool call's are, and the observed value is its body — so the vault gains
no component and the commitments cover both. A verdict's label is integrity level 1 —
deterministic system data — joined with the tier of the result it read
(ADR-0270 §SD4). The reply's label is unchanged: a verdict is evidence about
one claim, not an endorsement (ADR-0311 §SD3), and lowers nothing; a held
verdict says nothing about the sentences beside the cited one.

Verdicts are not projected into model requests — the model saw them as the
cite call's outcome — so the fold skips the kind, as it skips every kind it
does not read (ADR-0309 §SD2). They are served three ways: on `agent_run_entries` as every
entry is; on `keelson('agent_verdicts')`, one row per verdict with the record
flattened and the ordinal of the reply that refers to it, for play and the
analytics surface
([ADR-0283](./0283-chat-analytics-the-agent-surface-and-what-was-granted-and-used.md));
and, deferred, as `my_verdicts` on the agent schema
([ADR-0313](./0313-agent-schema-generated-relations-an-agent-may-read-in-full.md)).

A **recheck** is a later `check` of a recorded assertion, writing a new
verdict entry under `Form` `recheck` whose outcome is `held` or `moved`;
the latest verdict per assertion is the one consumers read, and a recheck
with no window is `unchecked` and leaves the earlier verdict standing
beside it. The readers resolve the latest verdict already; dispatching a
recheck is deferred (SD9).

### SD5 — Laws as queries

In the shape of ADR-0309 §SD6: each law returns the rows that break it, as a
Go check over the preservation harness's journals in the default lane and
as SQL in the integration lane where written.

- **L36 — references resolve.** Every `[id]` reference in a reply, outside
  code, names a verdict entry of the same turn written before the reply;
  returns the dangling references. A reference is letters then digits in
  square brackets, so a checkbox, a footnote and a link are none.
- **L37 — a verdict that ran has its sources.** Every verdict whose
  outcome is `held`, `failed` or `moved` rests on at least one outcome of
  the run that precedes it and whose intent is a query, and carries its
  statement digest, its source's and its endpoint; returns the verdicts
  that do not. The scope rule itself is the window's (SD3), checked where
  the source statement is; the journal holds its digest.
- **L38 — figures are accounted for.** Every figure a reply states outside
  code, references and list markers appears in a result the turn read, in
  an assertion's observed value or expectation, or in the person's message
  of that turn; returns `(reply ordinal, figure)`. L38 is a report, not a
  refusal; making it one is a profile's choice (SD9).

The three are Go checks in the `projection` package, run by the
preservation harness over its journals (L36, L37) and by the chat's tests;
none is written as SQL, since each reads a body, which the journal views
the integration lane's laws read over do not carry.

### SD6 — Consumers

- **The chat** shows each reference with its verdict's mark beside it —
  held, failed, moved, or unknown for a reference no verdict names — and
  a cited reply ends with its citations: the claim, the outcome, what was
  observed and why it did not hold. The transcript's export carries the
  same. A verdict is read from the journal, so a resumed conversation
  shows the same marks. Opening the cited outcome's rows as a table
  ([ADR-0336](./0336-recordtable-row-grouping-a-line-sequence-over-the-permutation.md))
  is deferred (SD9).
- **The artefact** ([ADR-0282](./0282-chat-artefact-one-markdown-document-per-conversation.md))
  shows the citations its text refers to under the document, resolved
  against the conversation's verdicts, the latest turn's standing where
  two turns reused an id, and the hand-off to the editor carries them as a
  foot.
- **Exits.** The guard of ADR-0310 §SD3 and ADR-0312 §SD2 in the chat's
  own form: a copy or an editor hand-off of text that cites a claim that
  did not hold, or refers to no verdict, is held once with the reason
  shown, and the next gesture sends it; text with no references goes. The
  dispatcher's sinks — a publish, a fetch — do not read verdicts in this
  cut (SD9). Law L14 is untouched: a verdict is not a tier.
- **Fan-out.** This record is the verifier ADR-0315 §SD9 waits for, and the
  selection rule is a query over verdict rows: the candidate with no
  `failed` and the most `held`, ties broken by the fewest `unchecked`, then
  the lowest ordinal; a verdict counts for the candidate whose key it
  carries, and the choice is recorded as the `select` entry carrying the
  labels of the verdicts that decided it (ADR-0311 §SD6, L8). `check` is
  read-only, so candidates' assertions run on one shared instance under
  the `read-shared` fork class (ADR-0315 §SD5). Fan-out itself stays
  where ADR-0315 left it; the rule is a fold in the `projection` package,
  tested over hand-built journals before any producer exists.

### SD7 — The prompt

A `citations` section of the chat's book, offered when Apps is on, says: a
figure, count, comparison or rank drawn from a result is asserted through
`cite` before it is stated, naming the call it was read from, in the handles
that result used; a judgement is not asserted; what failed is stated as it
was observed, or dropped. The section enters the fingerprint (ADR-0310
§SD6) and the preservation golden is re-recorded once with the change
named, under ADR-0310 §SD11's rule.

### SD8 — Bounds

Constants of this cut, in the chat and play's catalog: eight assertions
per `cite` call; two `cite` calls per turn; one row and one kilobyte of
cell text per `check`; a statement no longer than the catalog probes
take; a timeout of the check's own. A profile setting that requires
citations for every figure is deferred (SD9).

### SD9 — Deferred, recorded

- **Judgement in prose** (O3): a judge call under a `verify` purpose on its
  own account ([ADR-0300](./0300-metered-model-calls-and-rules-a-moderator-writes.md)
  §SD1), its verdict labelled as model output.
- **`check` on other apps**: any catalog declaring a query of `check`'s
  shape — a result reference and a statement — is a source; a SQL applet
  window ([ADR-0274](./0274-sql-applets-as-agent-operable-windows.md),
  proposed) is the next.
- **Assertions about the conversation** over `my_run` and `agent_history`
  (ADR-0313), once the agent schema is bound on a host.
- `my_verdicts` on the agent schema; an `unfounded` signal for the moderator
  (ADR-0302 §SD6's list); a `require_citations` profile setting that turns
  L38 into a guard; a scheduled recheck as a watchbill job
  ([ADR-0223](./0223-watchbill-durable-work-on-facts.md)) writing `moved`
  verdicts; joining verdicts to query observability's rows (ADR-0115) for
  cost and for provenance beyond the window's memory; verdict counts on the
  analytics surface.
- **Write-then-verify** as a profile option: assertions in the final reply,
  the turn ending on all held; rejected for the first cut in Alternatives.
- **Dispatching a recheck** (SD4), and with it the `moved` outcome; a
  check against a result the window no longer holds, from the history.
- **Verdicts at the dispatcher's sinks**: a publish or a fetch of text
  citing a failed claim as `Ask` in the dispatcher's algebra, where the
  chat's own copy and hand-off are held in this cut (SD6).
- **The cited rows as a table** under a mark in the chat (SD6).

### Milestones

- **M1 — The window ✓.** Play's `check`; `describe_result` and
  `sample_rows` carry the result's id and statement digest; a dated
  Update on ADR-0270 at acceptance.
- **M2 — The harness ✓.** The `cite` tool and its gate, the `Verdict`
  record on the ledger (a dated Update on ADR-0325 §SD1's classification
  at acceptance), verdict entries, the `citations` section; the
  preservation golden re-recorded with the change named, and a
  `cited-reply` scenario added.
- **M3 — The readers ✓.** Marks and the citations foot in the chat and
  its export, the citations under the artefact and in its hand-off, the
  held copy.
- **M4 — The rows ✓.** `keelson('agent_verdicts')`, laws L36–L38 in the
  default lane, the selection fold over hand-built journals.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| the coordinator's tool registry (ADR-0315, Update of 2026-10-13; `keelson('agent_tools')`) | `cite` added, with its budgets | the tool schemas the model sees; the preservation golden |
| the chat's prompt book (ADR-0310 §SD6) | a `citations` section | the fingerprint; the preservation golden |
| play's catalog (ADR-0270 §SD1; `keelson('app_operations')`) | `check` added; `describe_result` and `sample_rows` carry the result id and its statement's normalised hash | ADR-0270, a dated Update; play's agent-limit and scene tests |
| the journal (ADR-0308 §SD2; ADR-0325 §SD1) | kind 16 gains the `Verdict` record on the ledger, its content the entry's body and arguments; eight memberships join the runtime vocabulary | the record store's generated code; the vocabulary's assignment golden; the kinds golden is unchanged |
| the projection (ADR-0309 §SD2) | the fold skips `verdict` entries | the fold golden |
| the chat's copy and editor hand-off (ADR-0317) | held once on a failed or dangling citation | the chat's tests |
| `keelson('agent_verdicts')` | added | the introspection registry |
| nanopass analysis | play composes a table-subset check from `ExtractTables`, `BuildScopes` and the keelson reference extractor; no pass property changes | none |

## Alternatives

The four forms are weighed in the QOC. Narrower choices:

- **Markers lifted from the reply body** (`[c:call_7]` after a sentence,
  no tool). Rejected: it parses prose for the one piece of structured
  output the model produces, where the tool-call channel is the one the
  journal already shreds canonically (ADR-0325 §SD1); and a marker names a
  call, not a claim, so nothing is executable.
- **The harness executes assertions itself** through the query engine.
  Rejected: the endpoint, the parameters and the agent limits are the
  window's, and a second executor is a second enforcement point for the
  same statement (SD3).
- **The verdict as a label component.** Rejected: a label lowers only by
  endorsement (ADR-0311 §SD3) and joins by maximum across a document; a
  held verdict on one sentence would read as trust in the reply, and a
  failed one would taint the artefact it lands in. A verdict is evidence
  beside the label.
- **Write-then-verify**: assertions in the same message as the final text,
  the harness ending the turn when all hold. Rejected for this cut: a
  failed check would have no round in which to be corrected, and one
  message carrying both text and tool calls is the shape modest local
  models produce least reliably; kept as a profile option (SD9).
- **A tolerance constant** for `value` assertions. Rejected: the precision
  the model displays is the tolerance the person reads, and needs no number
  of its own (SD1).

## Consequences

### Positive

- A claim about data is checked by the harness, and the check is a
  statement the person can read, run and keep.
- Derived figures are covered, since the assertion computes them.
- Verdicts are rows: which replies failed a check this week, and on which
  tables, is a query in play, with no new surface.
- Fan-out has its verifier and its selection query (ADR-0315 §SD9).
- A figure nothing accounts for is countable (L38), which separates what the
  model read from what it asserted.

### Negative

- A cited reply costs a round and the assertions' tokens; the budgets of SD8
  bound it.
- The model must write assertion SQL in the result's handles; a modest
  local model may cite less than the section asks.
- The preservation golden is re-recorded; the runtime vocabulary gains
  eight memberships and its assignment golden moves with them.
- Play's catalog grows by one query and two result fields.

### Neutral

- A wrong question is still wrong: an assertion over the wrong table passes.
  The statement makes that reviewable, not detectable.
- Judgement in prose is unchecked by construction, and the marks say so.
- Play is the only `check` source in this cut.

## Migration — Tier 1

- **Breaks.** Nothing stored: kind 16 had no writer, so no journal carries
  a verdict; conversations recorded before this record have none and render
  without marks.
- **Path.** M1 to M4 landed together.
- **Regeneration.** The agentrun record store's generated code
  (`TestGenerateAgentrunStores`) and the vocabulary's assignment golden;
  `keelson('app_operations')` follows play's declarations.
- **Old shape.** A reply with no references is unchanged and stays allowed.

## Verification plan — Tier 1

- **Lane.** Default `go test`: play's scope gate — a table outside the
  result's, a tautology, a CTE of the assertion's own, a keelson table —
  and the digest and the result-id checks (package `play`); the chat's
  coordinator tests with a scripted model that cites against a test app's
  `check` — held, failed, resting on no call, refused, the verdict
  entries' record and content, the budgets, the marks and the held copy
  (package `chat`); the preservation golden, with the `cited-reply`
  scenario; the verdict entry's round trip through the ledger row, the
  commitments and the wire (package `agentrun`); L36–L38 and the
  selection fold over hand-built journals (package `projection`), and
  L36 and L37 over every preservation scenario's journal.
- **What would fail.** A reply referring to no verdict; a verdict that ran
  without its sources or digests; an assertion naming a table its result
  did not read; a cite call past its budget that is taken; a changed
  `check` row bound.
- **Gap.** `check` is run against a server by no lane: the gate is tested
  and the delivery follows the catalog probes' path. Whether a local
  model writes assertions reliably is not tested without a model; the
  scripted model asserts what the test tells it to. Displayed precision
  is tested on the comparison alone.

## Status

Proposed — awaiting review by the code owner. Built ahead of the review:
M1–M4 landed and the verification plan's default lane runs. The Updates
this record owes ADR-0270, ADR-0315 §SD9 and ADR-0325 §SD1 are written at
acceptance, so no accepted record links a proposed one.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md), [ADR-0270](./0270-play-operations-catalog-and-agent-limits.md) — the operation contract, play's catalog and agent limits.
- [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) §SD6 — the call key.
- [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md) §SD2, [ADR-0325](./0325-the-journals-canonical-form-is-the-canonical-wire-over-its-rows.md) §SD1 — the reserved kinds and the record classification.
- [ADR-0309](./0309-context-projection-as-sql-over-the-journal-in-a-local-worker.md) §SD6 — laws as queries.
- [ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md) §SD3, §SD6 — the guard algebra, the book and the registry.
- [ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md), [ADR-0312](./0312-exits-sinks-from-the-cell-boundary-approval-tiers-and-exit-budgets.md) — labels and exits.
- [ADR-0313](./0313-agent-schema-generated-relations-an-agent-may-read-in-full.md) — the agent schema.
- [ADR-0315](./0315-coordinator-tools-as-operation-specs-and-a-contract-for-stateful-tools.md) §SD5, §SD7, §SD9 — fork classes, the dry-run twin, fan-out's deferral.
- [ADR-0282](./0282-chat-artefact-one-markdown-document-per-conversation.md), [ADR-0283](./0283-chat-analytics-the-agent-surface-and-what-was-granted-and-used.md), [ADR-0336](./0336-recordtable-row-grouping-a-line-sequence-over-the-permutation.md) — the artefact, the analytics surface, the table widget.
- [ADR-0115](./0115-query-observability-data-plane-strategy.md) — the normalised statement hash.
- Jayasekara et al., *CEDAR: A System for Cost-Efficient Data-Driven Claim Verification*, PVLDB 18 (2025) — claims translated to SQL and compared with the claimed value.
- *ClaimDB: A Fact Verification Benchmark over Large Structured Data*, ACL 2026 — agents verifying claims with a single SQL tool.
- *From Tables to Quantified Statements: Evaluating LLM Inference Generation through Executable Verification*, arXiv:2609.23966 — each inference compiled to an executable check over the source table.
