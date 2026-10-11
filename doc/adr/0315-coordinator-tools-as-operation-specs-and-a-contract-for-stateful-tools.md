---
type: adr
status: accepted
date: 2026-10-11
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-11
---

# ADR-0315: The coordinator's tools are operation specs, and a stateful tool declares its footprint, epochs, rebuild class, fork class and completion

## Context

[ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
gives app operations a declared class, effect, schema and label rules the
dispatcher enforces, and [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md) §SD4 settles resume by that effect class. The coordinator's own
tools — the artefact, question, capture and view tools — are JSON literals
routed by an `if`/prefix/`switch` chain and carry no declared effect, so the
journal's `intent` cannot say what class a call was.

Stateful tools strain something else. The journal records what the model was
told, not the tool's state, so a request stays a pure function of the
journal; what strains is the agreement between the model's belief and the
tool's actual state, the survival of that state across a restart, and its
isolation when several samples of one request run. Two tools were read for
[agent-harness §2.4](../adr-background-work/agent-harness.md): play, whose
resources are declared and revisioned but whose effect declarations
understate live re-runs and fetches; and an interpreter-kernel tool in a
downstream application, an opaque process whose loss shows only as a changed
generation number. ADR-0269 §SD12 and
[ADR-0270](./0270-play-operations-catalog-and-agent-limits.md) record several
of play's gaps without a contract to measure them against.

## Decision

We will declare the coordinator's tools as **operation specs** through the
same reflection walk app operations use, so that every call the model makes
carries a class, an effect and the dispatcher's label rules; and we will
require of every tool that holds state a five-part **contract** — footprint,
epochs, a rebuild class, a fork class, and completion as its own entry —
each part with a check. The contract's fields and their validation are
built; the tools as operation specs are the part still ahead (SD1).

### SD1 — Coordinator tools as operation specs

Each coordinator tool is to be an `OperationSpec` with a schema and codec
from one reflection walk (`appops`, `opjson`), a class and effect, and the
`Untrusted` and `Consent` rules the dispatcher already enforces for app
operations. What is built is the half that does not change what the model
sees: the chain is a registry lookup
([ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md)
§SD6) over a tool registry that carries each tool's budget and whether it
is parallel-safe, the journal's `intent` carries an effect class per call,
and the `recall` tool of ADR-0316 joined the registry. The effect class is
declared per tool in that registry rather than derived from an
`OperationSpec`, and the per-tool caps are applied inside each tool rather
than by one post-execute step: both are the shape this record names as the
rejected alternative, kept for now because moving to operation specs
changes the tool schemas the model sees, which is why it was not among
the behaviour-preserving steps of ADR-0310 §SD11 and is recorded under
SD9.

### SD2 — Footprint

A tool declares every resource a command writes, hidden ones included. The
operations engine already observes which resources changed in each frame
([ADR-0188](./0188-app-instance-effect-tracking.md)) and attributes each
change to the command applied in it, or to the person when it landed
between frames, so a conformance test requires observed changes ⊆ declared
writes (law **L9**). Play's conformance test drives the app's frame over a
corpus of its commands against a capture server; its allowlist names the
gaps it observed — a signal write re-running the result, `set_sql` touching
the parameters, `run` touching the panes — and fails when an allowlisted
gap stops being observed, so the list can only shrink. A kernel evaluation
declared `run` with no writes would fail it too; a kernel becomes a
resource whose revision includes its generation.

### SD3 — Epochs

A tool publishes revisions or generations, and the harness journals each
change made by another actor as a `note` entry — what play's `turn` already
returns. A lost kernel is a journaled note, not a number the model has to
notice.

### SD4 — Rebuild class per resource

| Class | Meaning | Examples |
| --- | --- | --- |
| `durable` | survives a restart | facts, captured query runs |
| `refold` | rebuilt by re-applying the latest journaled set-style command | play's `sql`, `params`, `signals`, pane bindings |
| `replay` | rebuilt by re-execution, with its sources of nondeterminism declared | kernel definitions from self-contained, seeded cells |
| `volatile` | lost; resume tells the model | results, result ids, task refs, kernel random state |

As built the class is declared **per command** on `OperationSpec`, not per
resource, since a command is what the catalog declares and validates; a
command may not declare a rebuild class without declaring writes. Writers of
one resource may therefore disagree today — in play, `run` declares its
results volatile while the other command that runs leaves them unspecified,
and the panes refold from `show_pane` and `bind_pane` while
`compute_projection` says nothing — so a resume fold that reads classes per
resource needs a rule for disagreeing writers; the fold does not read them
yet, and both are deferred (SD9). With the projection in SQL, a `refold`
rebuild is itself a query — the latest applied set-command per resource —
yielding the launch configuration of a rehydrated instance.

### SD5 — Fork class

`read-shared` (queries fan out on one instance), `per-instance` (isolate by
opening a new instance from an explicit configuration, never from an ambient
workingset), or `none`; a command may not declare `read-shared` nor a query
`per-instance`. The rule that ties this to fan-out: **explore read-only,
commit once** — verify candidates only with read-only acts; execute a
command once, for the selected candidate.

### SD6 — Completion

A command that starts asynchronous work declares `Async`, returns a handle,
and the harness journals a separate `completed` entry when the work
settles; the resume fold is to treat "applied, not completed" as its own
state. Play's `run` records queuing as its outcome and returns no handle on
the compile date, so it does not declare `Async` until the `completed`
entry lands (SD8).

### SD7 — A dry-run twin

An effectful operation with a pure core names, as `DryRun`, a read-only
query of the same catalog that runs the core without committing, with the
command's argument type, so a verifier can judge a candidate without
spending the effect. The catalog's validation holds the contract: a query
declares none of rebuild, async or dry-run; every enum value is a declared
one; the twin exists and is a query. The declarations are served on
`keelson('app_operations')` as `rebuild`, `fork`, `async` and `dry_run`.

### SD8 — Play's gaps, to close under this contract

Effect declarations understating live re-runs and fetches; `run`'s outcome
recording queuing; an agent-opened window restoring the person's last
workingset with no launch configuration from the open tool; no resource
`Restorable`, so engine undo never succeeds; result ids from a process-wide
counter restarting at 1; several panes' lanes without a confinement label; a
command log ring of 1 024 entries so `turn` can miss changes; no expected
revisions sent by the coordinator, which hides conflicts between samples.
Each is a dated Update on ADR-0269 or ADR-0270 when fixed; the footprint
test's allowlist names those of these it observes until then (SD2).

### SD9 — Deferred, recorded

- Fan-out itself (the producer and the selection query), once a verifier
  exists for a purpose.
- A sandbox for model-written code; the harness is not one.
- The coordinator's tools as `OperationSpec`s and the single post-execute
  budget step (SD1); the resume fold reading rebuild classes, with a rule
  for writers of one resource that disagree (SD4); play's `completed` entry
  (SD6).

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| the coordinator's tools (ADR-0265 §SD6) | a registry with budgets and effect classes now; operation specs, with the schemas the model sees changing, deferred | the chat's prompt text; the preservation harness's golden, re-recorded once with the change named |
| `app.OperationSpec` (ADR-0269 §SD1) | gains footprint (`Writes`), rebuild class, fork class, completion (`Async`) and dry-run twin declarations, validated; play declares them on its set-style commands and `run` (ADR-0269 and ADR-0270, Updates of 2026-10-11) | the manifest validation; `keelson('app_operations')` |
| the operations engine (ADR-0188) | its observed-changes record feeds the conformance test | none in behaviour |
| play's catalog (ADR-0270) | SD8's gaps, each a dated Update when fixed | play's agent-limit and scene tests |
| ADR-0308 §SD4 | the fold is to read rebuild classes and `completed`; deferred | the fold's tests |

## Alternatives

- **Leave the coordinator's tools as literals** and give the journal's
  `intent` a hand-written effect per tool. Rejected: a second place for the
  same declaration, which the catalog already is.
- **A summary that remembers tool state** instead of a rebuild class.
  Rejected: a summary is model output re-entering
  ([ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md)); what can be recomputed from the journal should be.
- **No fork class; samples share one instance.** Rejected: on the compile
  date samples of one task on one play window overwrite each other silently.

## Consequences

### Positive

- Every call the model makes has a declared effect the journal and the
  dispatcher agree on.
- Resume knows what to rebuild and how; "applied, not completed" is a state,
  not a surprise.
- Understated effects become a test failure instead of a consent prompt that
  did not appear.

### Negative

- Tool schemas the model sees change once; the preservation golden is
  re-recorded with the change named.
- Every stateful tool owes five declarations; play's gaps are work.

### Neutral

- The interpreter-kernel tool is a downstream application's; this record
  gives it a contract, not a change.

## Migration — Tier 1

- **Breaks.** Nothing yet; the coordinator's tool schemas change once when
  SD1 lands, and play's consent prompts move as its declarations are
  corrected.
- **Path.** Land the footprint test with its allowlist and shrink the
  allowlist; then declare the coordinator's tools.
- **Old shape.** Tools without a footprint are treated as writing everything
  they touch, which is the conservative reading.

## Verification plan — Tier 1

- **Lane.** Default: the footprint conformance test over play's command
  corpus (L9), its allowlist observed and shrinking; the catalog's
  validation tests for the contract fields. The resume fold's
  rebuild-class tests and a resume lane with a `volatile` and a `refold`
  resource wait for SD4.
- **What would fail.** An observed resource change outside the declared
  footprint; a command whose asynchronous completion was never journaled; a
  resumed instance opened from an ambient workingset.
- **Gap.** A tool whose state is outside the engine's observation (a kernel's
  scratch files) is checked only as far as its declared generation.

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

- [ADR-0188](./0188-app-instance-effect-tracking.md) — the observed-changes record.
- [ADR-0265](./0265-chat-app-over-retained-model-calls.md) §SD6 — the coordinator's tools.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md), [ADR-0270](./0270-play-operations-catalog-and-agent-limits.md) — the operation contract and play's catalog.
- [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md), [ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md), [ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md) — resume, the registry, labels.
- [ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md) §SD11 — the footprint test and tool registry steps that preceded this record.
- [agent-harness](../adr-background-work/agent-harness.md) §2.4 — the analysis.
