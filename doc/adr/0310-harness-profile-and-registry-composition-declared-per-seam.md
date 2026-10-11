---
type: adr
status: accepted
date: 2026-10-11
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-11
---

# ADR-0310: A harness profile and registry — every seam declares how it composes, guards only refuse, the choice of components and constants is data recorded on the run

## Context

The policy that decides how well and how cheaply a model does its work in
the chat — round limit, stop conditions, the prompt and its notes, the tool
set and its caps, provider quirks, the three context estimators — sits in
`apps/chat` as constants, string literals and `if`/`switch` chains, with
one knob per `BOXER_CHAT_*` variable
([agent-harness §2.9](../adr-background-work/agent-harness.md)). The
provider client and the metering ledger are behind seams; the loop, the
context, the prompts and the tools are not. The consequences are recorded
there: a workaround for one model generation written as unconditional
behaviour; tuned constants whose evidence was never recorded; the same
screenshot setting touching nine files.

The harnesses read for that page converged on composition declared per
extension point, a registry built once with runs naming components rather
than code, and permissions merged in a lattice by maximum; the one with a
generic hook mechanism and load-order mutation is the one whose behaviour
depends on load order without saying so. Runtime code loading stays
declined. The behaviour-preserving refactors that prepared this record are
SD11.

## Design space (QOC)

**Question.** How is a harness policy chosen and recorded?

**Options.**

- **O1 — Constants and environment variables**, as the compile date.
- **O2 — Runtime-loaded plugins** selected by configuration, as DeepSeek
  Harness.
- **O3 — A registry of compiled components with a declared composition
  shape per seam, selected by a named profile that is data.**

**Criteria.**

- **C1 — Recorded**: every run says which policy ran, so "A against B" is a
  query.
- **C2 — Safe to extend**: adding a component cannot widen authority or
  depend on registration order.
- **C3 — Consistent with P1 and P6** of why-boxer (no runtime loading, one
  architect, machine-checked).
- **C4 — Cost** of the change.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | −− | +  | ++ |
| C2 | −  | −  | ++ |
| C3 | +  | −− | ++ |
| C4 | ++ | −− | −  |

O3 is taken. O2's selection mechanism is what O3 keeps; its loading
mechanism is what boxer has declined.

## Decision

We will put every policy point of the harness behind a seam with a
**declared composition shape**, build a **registry** of compiled components
once at start-up, and select among them per run with a **profile** that is
data: registered, listed as an introspection table, recorded as a `profile`
entry on the run and on the trail's call rows, and fingerprinted. Guards
combine by maximum and may only add refusals; authority stays compiled in
the dispatcher. An environment variable selects a profile; it does not
toggle a knob.

### SD1 — Composition shapes

| Shape | Go reading | Law | Ordering |
| --- | --- | --- | --- |
| slot | one interface-typed field | exactly one provider, or a declared default | — |
| registry | `map[id]T`, duplicate refused | contributions commute | none needed |
| chain | ordered slice, explicit `next` | not commutative | declared `Order`, never registration order |
| observers | fan-out, results ignored | commutative; a failing observer is contained | none needed |
| first claim | first non-nil wins | order-sensitive | declared |
| guard | returns `Allow ≤ Ask ≤ Deny`; combined by maximum; an error is `Deny` | associative, commutative, idempotent, monotone | none needed |
| fan-out then reduce | N samples of one request reduced by a rule | a vote commutes once ties break on the ordinal; rounds are a chain | none for a vote |

A chain is the shape an ADR must justify when it introduces one; the
commuting shapes can be extended without thinking about order. Under
[ADR-0312](./0312-exits-sinks-from-the-cell-boundary-approval-tiers-and-exit-budgets.md)'s
approval tiers, `Ask` is the person's path and the organisation path
refuses until an approver is wired; the maximum is unchanged. The `harness`
package holds the slot, registry, chain, first claim and guard shapes with
a test per law; a chain's elements run in declared order and cannot
short-circuit (no explicit `next`), observers and fan-out-then-reduce wait
for a first use (SD10).

### SD2 — The driver's seams

`runTurn` is split at the points the design names, each with its shape and
with the compile-date behaviour as its only implementation until a profile
selects another: pre-step injection (chain; the context injector of SD11 step 4 is its
first element), request
configuration (chain: the round, the compaction of ADR-0316, the last-round
note), request-error recovery (first claim; the rationing signals
`Remaining`, `RetryAfter`, `Refusal` the coordinator ignores today are read
here), tool mode (slot; sequential by default), turn stopping (guard).
Order is a declared slice. Post-tool shaping stays inside each tool's
execution for now (SD10); the exit-approval resend of ADR-0312 §SD3 sits
beside recovery, in the coordinator's completion call, since it is not a
recovery from an error but a second attempt under the person's answer.

### SD3 — Guards: one algebra, three carriers

Authority (grants, ceiling, consent, modes — the dispatcher's decision of
[ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
and [ADR-0280](./0280-a-ceiling-on-what-a-chats-model-may-do-scored-on-a-ladder.md),
read as a guard), labels
([ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md))
and resource cost
([ADR-0300](./0300-metered-model-calls-and-rules-a-moderator-writes.md)
(proposed)) are three carriers of one guard algebra, combined by maximum. A
harness-side guard can only add refusals, the rule ADR-0300 states for
moderators. Authority is not in the profile schema at all: no profile can
name a grant, a ceiling or a consent class. As built, the three carriers
share the `Allow ≤ Ask ≤ Deny` vocabulary (the label guard of ADR-0312
§SD2 returns it) and run as sequential checks in the dispatcher's order,
each able only to refuse; the dispatcher's own rows are one decision table
([ADR-0283](./0283-chat-analytics-the-agent-surface-and-what-was-granted-and-used.md),
Update of 2026-10-11).

### SD4 — Model routes with declared capabilities

Model access grows from one host-wide client into **routes**, each
declaring context tokens, reasoning echo, images in tool results, late-system
handling and streaming, which the rest of the tier reads instead of
guessing. The think-tag and late-system quirks become per-route
declarations. The profile names a route per purpose: turn, title, summariser
([ADR-0316](./0316-compaction-as-a-journal-entry-plan-act-apply.md)),
moderator, verifier. Encoding the projected batch of
[ADR-0309](./0309-context-projection-as-sql-over-the-journal-in-a-local-worker.md)
§SD3 to a route's wire format is the route's. The route id and the profile
reference (`<id>@<fingerprint prefix>`) land on every `llmCall` row, in
memory and on the trail. As built there is one route per host, declared by
`openaichat.Route`, which every purpose uses; a route per purpose is the
profile's `Route` field waiting for a second route (SD10).

### SD5 — One token meter

One estimator, anchored on provider-reported usage with bytes over four as
fallback, behind the chat's context bar, the moderator's context signal
([ADR-0302](./0302-a-built-in-moderator-loop-signals-and-an-escalation-ladder.md)
(proposed) §SD2) and ration's admission estimate: the `tokenmeter` package.
The compaction plan sizes with the same formula over the projected batch,
adding an allowance per declared tool, and anchors to the reported usage
(ADR-0316 §SD1). Route capabilities supply the window.

### SD6 — The prompt is a book; the tools are a registry

Prompt sections live in a `promptbook` book with a declared order, volatile
sections last, written to the journal as `section` entries
([ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md) §SD2). The coordinator's tools are a registry
([ADR-0315](./0315-coordinator-tools-as-operation-specs-and-a-contract-for-stateful-tools.md)) written as `tools` entries. The rendered sections and the tool
schemas hash to the fingerprint ADR-0308 §SD3 folds.

### SD7 — The profile

A profile names, for one run: the component at each slot, the passes in each
chain with their order, the guards, the prompt book, the route per purpose,
the compaction constants and trigger (ADR-0316), the anchoring cadence
(ADR-0314 §SD3), the samples per purpose when fan-out is taken up, and every
constant that is a Go literal or a `BOXER_CHAT_*` variable on the compile
date. It is registered like the other static-explicit registries, served as
`keelson('harness_profiles')` (one row per app, profile, fingerprint, field
and value), validated at registration (every chain ordered, every slot
filled, the thrash check of ADR-0316), and recorded on the run row, as a
`profile` entry, and on the trail's call rows. The first profile is a Go
literal equal to the compile-date constants; whether later ones are data
files gate-tested like `promptbook`'s corpus is open. Two parts are not yet
in it: the label and the **cell kind** it is for
([ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md)),
and the rounds, pixels and image-budget constants, which the profile
fingerprints but the runtime still reads from their `BOXER_CHAT_*`
variables, so a variable set away from the profile's value runs under a
fingerprint that does not say so (SD8).

### SD8 — Selection and the environment

One environment variable, `BOXER_CHAT_PROFILE`, selects the default profile
([ADR-0009](./0009-environment-variable-registry.md)); the `BOXER_CHAT_*`
family retires variable by variable as each constant moves into the profile,
each retirement a registry entry closed — none has retired yet (SD7). A
conversation selecting a profile at its first send, recorded as the
`profile` entry, waits for a second profile (SD10).

### SD9 — Change between runs only

A run is pinned to the profile version it started with; a change of profile
is a new run or a journaled `profile` entry at a turn boundary. What may
change by any later self-improvement path is a profile, between runs, under
gates; guards may only be added; producers, the journal and the laws never
change by that path. The gates and the proposers are deferred to their own
record once a second profile exists.

### SD10 — Deferred, recorded

- Fan-out and selection (`candidate`, `verdict`, `select` entries exist in
  ADR-0308; the producer and the selection query are a later record).
- The observers and fan-out-then-reduce shapes, the post-tool shaping seam,
  a route per purpose, per-conversation profile selection, the profile's
  label and cell kind, `keelson('harness_components')`, and retiring the
  `BOXER_CHAT_*` variables the profile already fingerprints.
- Laws L27 (no guard or moderator rule raised a limit), L28 (declared
  versus observed components) and L29 (every profile version's guards
  contain its parent's; no profile carries an authority field): no checker
  is written, since each is a query over stored journals the integration
  lane does not run yet, and L29 has one profile to check.
- Three visible changes SD11's steps left for this record and did not
  take: capping `call_operation` results, which changes what the model
  sees; reading the rationing signals `Remaining`, `RetryAfter` and
  `Refusal` in the recovery seam, after which a refusal no longer fails the
  turn; and the divergence of the three context estimators the one meter
  (SD5) replaced, whose thresholds each caller kept.

### SD11 — The behaviour-preserving steps

Eleven refactors preceded this record, each leaving what a person sees
unchanged and what a model receives byte-identical, under one operational
test: the **preservation harness** (step 0), which drives the coordinator
over a scripted corpus and records every wire request and dispatched call
to a golden the later steps keep unchanged. The steps, now landed, are
the names code comments cite:

| Step | What landed |
| --- | --- |
| 0 | the preservation harness and its golden in `apps/chat` |
| 1 | `runTurn` takes `promptbook.CompleterI` |
| 2 | a tool registry with a budget and a parallel-safe flag per tool replaces the dispatch chain; `call_operation` keeps budget 0 |
| 3 | the prompt book with a fingerprint over its rendered sections and the tool schemas (SD6) |
| 4 | one context injector over the mid-turn notes and their "told" watermarks |
| 5 | the driver's seams with the compile-date behaviour as their only elements (SD2) |
| 6 | route capabilities declared; provider quirks read the declaration (SD4) |
| 7 | one token meter (SD5) |
| 8 | one decision table for the dispatcher and the analytics surface ([ADR-0283](./0283-chat-analytics-the-agent-surface-and-what-was-granted-and-used.md), Update of 2026-10-11) |
| 9 | the footprint conformance test with its allowlist ([ADR-0315](./0315-coordinator-tools-as-operation-specs-and-a-contract-for-stateful-tools.md) §SD2) |
| 10 | the broker as the only builder of the clipboard opcode, held by CS014 ([ADR-0317](./0317-clipboard-and-export-copy-by-reference-and-one-choke-point.md) §SD2) |
| 11 | the profile struct with the compile-date constants (SD7) |

Two defects the steps found and left as they were, each recorded where it
sits: a note's "told" watermark advances when the note is composed and is
not restored when the turn fails, which the harness's failed-turns golden
pins and the injector's `deferred` note names; and the compaction plan's
estimator counts tool schemas where the meter of SD5 does not, so the two
agree on messages and differ by the tools' allowance. The visible changes
the steps excluded are SD10's last item, with the coordinator's tools as
operation specs (ADR-0315 §SD1), play's effect declarations (ADR-0315
§SD8) and the run's label on agent-caused fetches ([ADR-0312](./0312-exits-sinks-from-the-cell-boundary-approval-tiers-and-exit-budgets.md)
§SD4, under [ADR-0262](./0262-http-egress-as-a-keelson-capability.md)).
- Self-improvement proposers and the T0–T4 gates
  ([agent-harness §5.6](../adr-background-work/agent-harness.md)).
- Out-of-process components: a moderator or coordinator over NATS is gated
  on server-side authorisation of the bus, not on any seam here.
- Parallel tool execution, bounded by the dispatcher's per-instance checks.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `apps/chat` coordinator | reshaped along SD11's steps 1–6 and 11; no behaviour change until a second profile is selected | the preservation harness's golden |
| `openaichat.ClientI` | routes with capabilities; quirks as declarations | its tests; the route id on `llmCall` rows ([ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md), a dated Update) |
| `llmCall` rows ([ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md), Update of 2026-10-11) | gain `llmCallProfile`, `llmCallRoute`, `llmCallTier` and `llmCallApproval`; `keelson('llm_calls')` serves them as `profile`, `route`, `tier`, `approval` | the trail's generated views and goldens; [ADR-0283](./0283-chat-analytics-the-agent-surface-and-what-was-granted-and-used.md) reads them |
| `keelson('harness_profiles')` | added; `harness_components` deferred (SD10) | the introspection registry |
| `BOXER_CHAT_*` (ADR-0009) | retire as constants move; one selector variable added | registry entries |
| ADR-0302 §SD2–§SD4 (proposed) | the moderator's context signal reads the meter of SD5; the note lives in `tokenmeter`'s package doc until that record is accepted; thresholds from the profile are still ahead | a Tier 1 edit while proposed |
| `agent.Service.check` and the analytics `Classify*` | one decision table (SD11 step 8) | ADR-0283 §SD2, Update of 2026-10-11 |

## Alternatives

O1 and O2 are weighed in the QOC. Two narrower choices:

- **One generic hook with a mutable shared output**, opencode's shape.
  Rejected: behaviour depends on load order without saying so, and competing
  rewrites resolve by completion order, which no replay reproduces.
- **Authority as a profile field**, so a profile could relax a ceiling for a
  trusted route. Rejected: authority stays compiled in the dispatcher; a
  profile that could widen authority makes every later self-improvement path
  a security path.

## Consequences

### Positive

- A constant becomes a recorded parameter of every run that used it; "what
  did 24 rounds cost against 16" is a `GROUP BY` over the call rows'
  profile reference joined to `harness_profiles`.
- Provider quirks become declarations a second route can differ on.
- A second implementation of any seam — a compactor, a moderator ladder, a
  retry policy — lands without touching the loop.

### Negative

- More structure for one implementation per seam until a second exists; the
  plan's order keeps each step behaviour-preserving to pay for that early.
- A profile registry and fingerprint to validate and keep stable; a change to
  a profile strands the runs pinned to it (ADR-0308 §SD5).

### Neutral

- The dispatcher's authority decision is unchanged in substance; it is read
  as a guard.

## Migration — Tier 1

- **Breaks.** Nothing: the first profile equals the compile-date constants;
  every `BOXER_CHAT_*` variable keeps working until its registry entry is
  retired with a diagnostic.
- **Path.** SD11's steps, in order; then this record's registry and
  profile.
- **Old shape.** None kept once the selector variable exists.

## Verification plan — Tier 1

- **Lane.** Default: a unit test per shape asserting its law (a chain runs
  in declared order; a guard combines by maximum and treats an error as
  `Deny`; first claim stops at the first non-nil); every registered profile
  validates; the preservation harness's golden unchanged under the first
  profile. Laws L28 (declared versus observed components) and L27 (no guard
  or moderator rule raised a limit) over stored journals are not written.
- **What would fail.** A guard that lowers another's decision; a profile
  with an authority field; a run whose journal shows a pass its declared
  profile does not name; a second chain element changing behaviour under the
  first profile.
- **Gap.** Whether a second profile does better work is a trial, not a test.

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

- [ADR-0009](./0009-environment-variable-registry.md) — a variable selects, it does not toggle.
- [ADR-0094](./0094-keelson-introspection-tables.md) — introspection tables.
- [ADR-0265](./0265-chat-app-over-retained-model-calls.md) — the chat's turn loop this record restructures.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md), [ADR-0280](./0280-a-ceiling-on-what-a-chats-model-may-do-scored-on-a-ladder.md) — authority, read as a guard.
- [ADR-0300](./0300-metered-model-calls-and-rules-a-moderator-writes.md), [ADR-0302](./0302-a-built-in-moderator-loop-signals-and-an-escalation-ladder.md) (proposed) — rations and the moderator.
- [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md), [ADR-0309](./0309-context-projection-as-sql-over-the-journal-in-a-local-worker.md) — the journal and the projection the profile parameterises.
- [agent-harness](../adr-background-work/agent-harness.md) §1.2, §2.5–§2.9 — the analysis.
