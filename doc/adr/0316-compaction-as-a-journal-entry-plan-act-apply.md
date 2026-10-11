---
type: adr
status: accepted
date: 2026-10-11
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-11
---

# ADR-0316: Compaction as a journal entry — a plan that is a query, an act that calls the summariser, an apply that is a pass; derive and elide before summarising; the person is never summarised

## Context

The chat has no compaction: the full history is resent each round, three
estimators disagree on how full the context is, and the response to pressure
is a warning that tells the person to start over
([agent-harness §2.9](../adr-background-work/agent-harness.md)).
[ADR-0265](./0265-chat-app-over-retained-model-calls.md) §SD5 defers "a
context window: what to drop when the conversation outgrows the model, sent
as a declared omission".

With the journal as the only input
([ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md)) and the projection as SQL
([ADR-0309](./0309-context-projection-as-sql-over-the-journal-in-a-local-worker.md)), compaction splits along the factoring rule every other part
follows: deciding is a projection, calling a summariser is a producer,
applying is a pass. The harnesses read for the design converged on
compaction that appends and never deletes, in three representations; the
formal result (Context Compaction Theory) says selection is strictly weaker
than generation and gives sketch baselines a compactor must beat. A spike
replayed a synthetic 40-turn run through the real projection under four
policies with a placeholder summary: the cheapest policy cost 0.87 M uncached
tokens with the summariser on the turn's route, the most aggressive thrashed
(35 compactions, prompt plus summary plus tail above its own trigger), and a
separate summariser route cost about five times more per summary than
extending the turn's cached prefix. Figures are the spike's own, under the
conditions in that page's Appendix A; nothing has run against a model.

## Design space (QOC)

**Question.** How is a compaction represented in the journal?

**Options.**

- **O1 — Span replace from a cut**: a `compact` entry naming a cut ordinal,
  the span replaced and a summary; the projection keeps entries from the cut.
- **O2 — Tombstone set**: an entry listing forgotten ordinals and a summary;
  the projection filters.
- **O3 — Full replacement checkpoint**: an entry carrying the whole replaced
  history.
- **O4 — Sliding window**: no entry; the projection drops results older than
  K rounds per request.

**Criteria.**

- **C1 — Append stability** (law L5): a rewrite happens only at an entry.
- **C2 — Replay length** and storage.
- **C3 — The plan is a pure query** over the prefix.
- **C4 — Checkpoints the prompt** so a resumed run reads one place.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | ++ | ++ | −− |
| C2 | +  | +  | −  | ++ |
| C3 | ++ | +  | +  | −  |
| C4 | ++ | −  | ++ | −− |

O4 breaks the cached prefix on every request and is excluded by L5. O1 is
taken; O3 is kept as a later option if replay length becomes a measured
problem.

## Decision

We will compact by appending a **`compact` entry** that names a cut, the
span it replaces and a summary. The decision to compact is a **plan view**
over the journal prefix; the summariser call is a producer with an intent and
an outcome; applying the compaction is a projection pass. Derivation and
elision run before any summary; the person's messages are never summarised;
the summary is model output and labelled as such; the constants are profile
data checked against thrash.

### SD1 — Plan, act, apply

| Part | Role | What it is |
| --- | --- | --- |
| plan | projection | `PlanCompaction(entries, order, constants, reported, at)` in the projection package returning a verdict (`none`, `elide`, `summarise`, `refuse`), the cut ordinal, the span replaced, the least label the compaction may carry, the person's messages inside the span and the elision's text; it sizes the request by running the projection itself, anchored to the provider's reported prompt tokens for the last request plus the one meter's estimate for what came after, and never emits a plan that removes nothing |
| act | producer | the driver writes an intent, calls the summariser, appends `compact{target = cut, from = previous cut, body, key, form, label}` with the form `elide` or `summary`; the act changes nothing outside the journal, so resume may run it again |
| apply | projection pass | the latest cut wins; the compaction follows the system prompt in the recalled band of ADR-0311 §SD4; a declared `Rewrites` pass |

### SD2 — A ladder, cheapest first

| Stage | What | Model |
| --- | --- | --- |
| derive | a state digest folded from the journal and the stateful-tool contract ([ADR-0315](./0315-coordinator-tools-as-operation-specs-and-a-contract-for-stateful-tools.md)): open instances, current resource revisions, grants, withheld markers, open questions — exact and labelled, recomputed rather than remembered; deferred (SD6), the chat re-tells its ceiling and artefact snapshots after a cut instead | no |
| elide | a tool result before the cut at or over the elision size becomes a stub naming its call key, ordinal, size and digest; the person's and the model's words stay verbatim; a read-only `recall` tool, offered once a run has compacted, brings a stub's result back from the journal under its own label (tool results were about two thirds of the spike journal's tokens) | no |
| summarise | a rolling summary: the previous summary plus the entries in `[from, cut)`, the person's words after it verbatim up to the pin budget | yes |
| overflow | the provider refuses the request as too long; the plan is to run again with a tighter budget under the one recovery owner of [ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md) §SD2; not built, the compile-date recovery stands (SD6) | as above |

The plan takes the cheapest stage that fits with one round's margin, the
earliest cut first, keeping the latest turns whole; when none fits it says
`refuse`, and the chat records that once per turn as an `ignorable` entry
(name `compaction`, form `refused`) and sends the request as it is, for
the recovery owner. A summariser that fails leaves no elision in its place
— the plan had judged the elision too small — but an intent, its failed
outcome and a `summariser-failed` record, and is not asked again that
turn.

### SD3 — Rules that follow from earlier decisions

- **Rewrites happen only at an entry** (L5): elision and summary both happen
  at a `compact`; no sliding window.
- **A cut sits on a boundary**, never strictly inside a call–result span
  (L2); a turn start is preferred to a round boundary.
- **The person is not summarised.** Their messages stay verbatim up to a pin
  budget; summarising them would move integrity from level 0 to the model's
  own ([ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md)).
- **A summary is model output re-entering.** Its confidentiality is at least
  the join of what it replaces (L3), its integrity at least the model's
  own, level 3, and it is rendered as model-written in the recalled band
  (ADR-0311 §SD4), never in the person's voice; an elision re-enters the
  same way, since it is derived from entries the model may have written. A
  consequential act whose only justification is a summary needs an
  endorsement.
- **The summariser call is a sink** ([ADR-0312](./0312-exits-sinks-from-the-cell-boundary-approval-tiers-and-exit-budgets.md));
  it goes through the route's wall like any other call, under the purpose
  `chat/summariser`, and a run above the route's clearance summarises on a
  route inside the cell. The chat does not loop the summariser's own
  refusal through the person's approval path.
- **The summariser request extends the turn request** on the turn's route,
  so it reads the cached prefix; a separate route is for when a label or a
  cheaper model requires it.
- **A profile is checked for thrash**: system prompt plus summary plus the
  pinned person text plus kept tail plus one round's margin stays below the
  trigger, or every request compacts; the check runs at profile validation
  (ADR-0310 §SD7) against the smallest context the profile is meant for,
  and again against the route's real trigger when a route's context is
  smaller, compaction switching off for the conversation when it fails.

### SD4 — Laws

- **L22 coverage** — each `compact` entry's `from` equals the previous cut,
  so no span is skipped;
- **L23 progress** — cuts strictly increase;
- **L24 bound** — the request after a compaction fits under the trigger by
  the same anchored meter the plan used, and the profile's thrash check
  holds;
- **L25 the person verbatim** — every user message is in the request
  verbatim or marked as summarised;
- with L2, L3 and L5 as above. L22, L23 and L25 are one Go check over a
  journal and L24 another, run in the default lane over the preservation
  harness's journals.

### SD5 — Judging a compactor

Cost is exact offline: a stored run replays under another profile through
the real projection with no model
([ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md)
§SD9). Quality has three handles and no exact answer: one step at each
compaction point (the first request after a cut is the first that differs;
compare the next action compacted and uncompacted, or let a verifier judge;
later steps diverge and need a trial in the
[doc/trials](../trials/README.md) sense); the recall rate after elision, a
journal query and a direct sign of over-eager elision; and a sketch baseline
of the same size for declared query classes, as Context Compaction Theory
does. Compaction is the first target for profile tuning because its
constants are numeric, its cost is exact offline and its quality has this
bounded check.

### SD6 — Deferred, recorded

- A full replacement checkpoint (O3), if replay length becomes a measured
  problem.
- Provider-native compaction and context-editing endpoints.
- Compaction of a branch, pending `parent` ordinals.
- The derive stage and the overflow claimant (SD2); the plan as a SQL view
  beside the Go plan; the judging of SD5.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `compact` entry (ADR-0308 §SD2) | written by the act | the kind enum's golden |
| the projection (ADR-0309) | the apply pass, declared `Rewrites`; the plan; the recall act's projection | the pass-property tests; the golden |
| the profile (ADR-0310 §SD7) | the trigger as a share of the route's context and the floor the thrash check runs at; kept turns, pin budget, elision size, summary and tail allowances, round margin; the thrash check | profile validation |
| ADR-0265 §SD5 "a context window" | settled here, not as a declared omission; a dated Update on acceptance | the chat's context bar reads the one meter |
| the coordinator's tools (ADR-0315) | a `recall` read-only operation | the tool registry |

## Alternatives

O2, O3 and O4 are weighed in the QOC. Two narrower choices:

- **Summarise the person's messages too** when the pin budget is exceeded.
  Rejected: it moves what the person said to the model's integrity level;
  over the pin budget the plan elides tool results further or refuses.
- **A separate summariser route by default.** Rejected: about five times the
  cost per summary in the spike, and no label reason in the common case.

## Consequences

### Positive

- A conversation outgrows a window without starting over; the cached prefix
  breaks exactly once per compaction, which the plan can price in advance.
- What a summary might misremember about tool state is recomputed instead.
- A profile that would thrash is refused at validation.

### Negative

- A summary is a place to launder an injection; the label and endorsement
  rules are what stand against it, and they add gestures.
- Quality is not exact offline; only trials answer whether a summary kept
  what a later step needed.

### Neutral

- Nothing here has run against a model; the spike priced policies with a
  placeholder summary.

## Migration — Tier 1

- **Breaks.** The default profile compacts, at seven tenths of the route's
  context, keeping the latest two turns whole; a conversation that would
  have hit the warning to start over now sees a compaction instead, and a
  `recall` tool once it has.
- **Path.** Tune the constants on the profile; run the counterfactual
  replay over stored journals before changing them.
- **Old shape.** No compaction, with the warning to start over; the
  recovery owner still handles a provider's overflow.

## Verification plan — Tier 1

- **Lane.** Default: the plan's boundary and sizing tests, the elision and
  the recalled band in the projection package; the thrash check in profile
  validation; L22–L25 with L2 and L3 over the preservation harness's
  journals, including a scenario that elides and recalls; the chat's
  summarise, failed-summariser, refuse and small-context tests. The
  counterfactual replay of the spike as a lane fixture is not written.
- **What would fail.** A cut inside a call–result span; a summary labelled
  below what it replaced; a user message neither verbatim nor marked; a
  profile whose first request after compaction does not fit.
- **Gap.** Whether a summary preserves what a later step needs.

## Status

Accepted 2026-10-11.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [ADR-0265](./0265-chat-app-over-retained-model-calls.md) §SD5 — the deferred context window.
- [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md), [ADR-0309](./0309-context-projection-as-sql-over-the-journal-in-a-local-worker.md), [ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md), [ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md), [ADR-0312](./0312-exits-sinks-from-the-cell-boundary-approval-tiers-and-exit-budgets.md), [ADR-0315](./0315-coordinator-tools-as-operation-specs-and-a-contract-for-stateful-tools.md) — the journal, the projection, the profile, labels, sinks, the stateful-tool contract.
- [doc/trials](../trials/README.md) — the trial discipline for quality claims.
- [agent-harness](../adr-background-work/agent-harness.md) §5.5 and Appendix A — the spike, its conditions and the papers (Tirmazi, Bishop, Markelon, Mitzenmacher, "Context Compaction Theory").
