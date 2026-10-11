---
type: adr
status: accepted
date: 2026-10-11
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-11
---

# ADR-0312: Exits — sinks derived from the declared cell boundary, clearances in TLP, approval tiers as policy data, exit budgets, canaries

## Context

On the compile date the places where data leaves a boxer host are checked
unevenly ([agent-harness §4.5](../adr-background-work/agent-harness.md)):
the remote model route and HTTP egress refuse one bit (`confined`) unless the
destination is local; play's publish operations check effect, consent and
ceiling and no label; the SQL text an agent sends to an endpoint is not
itself checked as a payload; persisted command arguments are not checked on
the write path, which [ADR-0264](./0264-retained-model-conversations-on-facts.md)'s
Status records; the clipboard broker audits app id and bytes and no label.
Each check was added by the ADR that introduced the channel, with no list of
channels to check against.

The threat model ([ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md)) supplies the list: an edge that leaves the declared cell is a
sink. It also supplies who may approve an exit, in what terms (TLP tiers,
[ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md)), and that the agent is read-only outside the cell but for the
model route. Budgets bound how much can leave through that one send;
[ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
§SD6 lists budgets for rows, bytes, launches, captures and wall time, and
the task struct has call, launch and deadline fields only. A security spike
on `clickhouse local` 26.9 showed that an exact canary (`position` over
every outbound payload joined with a canary table) and four-word shingles of
a protected body find a verbatim leak and nothing else, while `ngramSimHash`
distances only order candidates on sentence-length texts.

## Decision

We will derive the harness's **sinks** from the declared cell boundary, give
each a **clearance** in TLP terms served as an introspection table, and check
every exit against it: nothing above a sink's clearance leaves by a machine
channel; an exit above a channel's free tier follows the **approval path**
the deployment declares as data; the agent has no send outside the cell but
the model route, whose **exit budget** in bytes per task bounds what can
leave; and **canaries** detect a verbatim leak at any sink.

### SD1 — Sinks from the boundary

The cell declaration ([ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md)
§SD1, in [ADR-0126](./0126-appliance-topology-as-data.md)'s topology) names
what is inside. Every edge boxer mediates that leaves it is a sink:

| Sink | Carrier | Clearance from |
| --- | --- | --- |
| remote model route | the llm service's sensitivity wall ([ADR-0254](./0254-model-inference-as-a-keelson-capability.md) §SD3) | the route's declaration; a trusted host is inside the cell and is not a sink |
| HTTP egress, the outbound half of a fetch | `httpegress` ([ADR-0262](./0262-http-egress-as-a-keelson-capability.md) §SD4) | the destination's declaration |
| a ClickHouse endpoint outside the cell | play's run and the query dispatcher | refused by the topology before any text is composed; not a sink an agent can reach |
| sync to a peer cell | the person's act | the peer's clearance |
| clipboard out of boxer, file export, printing | [ADR-0317](./0317-clipboard-and-export-copy-by-reference-and-one-choke-point.md) | the channel's tier policy |
| persisted arguments on a facts server outside the cell | the trail's write path | ADR-0264 §SD5's locality rule, now a clearance; the Status gap closes |
| images leaving the host | the capture PEP ([ADR-0281](./0281-window-captures-through-one-policy-enforcement-point.md)) and the pixels setting ([ADR-0287](./0287-chat-pixels-a-setting-for-showing-screenshots-to-the-model.md)) | the route's clearance |

The registry is served as `keelson('sinks')` with the columns `kind`,
`edge`, `clearance`, `clearance_rank` and `check`, derived from the one
`cell.Declaration` the host builds at boot (ADR-0307 §SD1): the model route
when it is not loopback or trusted, every registered egress destination
outside the cell, the clipboard, export and each declared peer. The facts
write path, sync and images leaving the host are not derived yet (SD7). A
sink the registry does not list is one the declaration omitted, which is
the finding to act on; channels that bypass boxer on a host the person
administers are not in it, and the threat model says so.

### SD2 — The check is a guard

Each sink's check is a guard in [ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md)'s
algebra, combined by maximum with authority and budget: the payload's label
(the projection's, for a model request; the run's current label, for a
fetch; the body's, for a publish or copy) at or below the clearance is
`Allow`; above it, the approval path of SD3 says `Ask` or `Deny`. The
clearance is a ceiling: a tier the mapping calls *free* that sits above it
is denied, since *free* opens nothing above a clearance. Law **L14**: every
row the trail records as leaving the cell carries a tier at or below its
sink's clearance, or the key of a person-path approval; routes inside the
cell, loopback or trusted, are no sinks and are outside it.

### SD3 — Approval tiers are policy data

A deployment maps tiers to one of *free*, *ask the person with a recorded
justification*, *ask an organisation approver*, or *never*; boxer ships a
default mapping (`BOXER_CELL_APPROVALS`, one table for every channel kind
rather than one per kind) and enforces whatever is declared, failing closed
on a value it cannot parse. As built only the person's path opens an exit;
the organisation path refuses until an approver is wired. An exit above
the free tier that is approved writes an `approval` entry (the channel, the
edge, the tier, the path, the approver, the justification) before the exit
leaves. At the model route the sink **verifies** the approval rather than
trusting the caller: the refused call's id, the tier, the sink and the
digest of the request's deterministic wire encoding are remembered for an
hour; the resent request carries the entry's key (`<run>#<ordinal>`), and
the route loads the entry through the journal store and requires it to be
an approval of that call for that sink on the person's path, bound to the
run (the entry's run is the request's conversation) and to the sender
(that run's owner app), with the bytes and the tier the ones it refused.
An approval is consumed once, by the one exit that passes the last refusal
point after it. Without a journal store wired, every key is refused. The
chat journals the question and the person's answer as `asked` and
`answered` entries (form `exit`) and asks once per turn, sink and tier,
writing an approval per exit under the standing answer; a yes needs a
justification. Law **L33**: every exit above its channel's free tier
carries the key of an earlier `approval` at the required path whose own
key is the refused call's id, joined by run and ordinal. The organisation's
mapping binds the agent over the person (ADR-0307 §SD7): no grant lowers
it. Sealed data (`confined`) keeps its compile-date wall at every route
outside the cell, managed or not; the tier wall and the person's path
apply to labelled tiers.

### SD4 — The agent is read-only outside the cell

An agent may fetch from outside the cell; it may not send, publish to a
peer, sync or act outside it. Its one outward send is the remote model call.
The outbound half of a fetch (URL, headers, body) can carry data, and the
egress service polices it against the tier the caller declares on the
request (ADR-0262 §SD4), refusing above the destination's clearance with no
approval path; the harness is to **declare the run's current label on every
agent-caused fetch**, which no consumer does yet, so a fetch is checked as
CLEAR until it does (SD7), and canaries (SD6) run on model-route payloads
only. What comes back is level 2.
Publishing *inside* the cell (`publish_result`, `publish_projection` of
[ADR-0270](./0270-play-operations-catalog-and-agent-limits.md)) is a
consequential act under the endorsement rule of ADR-0311 §SD3, not an exit.

### SD5 — Budgets as exit bounds

The metered quantity at the model-route sink is **bytes sent to a remote
route per task** (`bytes.sent`), in [ADR-0300](./0300-metered-model-calls-and-rules-a-moderator-writes.md)
(proposed)'s ledger shape with the refusal naming the rule; the exact size
of the request body is reserved at admission, so the call that would cross
the budget is refused rather than charged after. Read budgets stay as
least-privilege hygiene: bytes on every read path, and attribute instances
(`arraySum(<channel>card)` per section) on the agent schema
([ADR-0313](./0313-agent-schema-generated-relations-an-agent-may-read-in-full.md)),
where the view can compute them; neither is metered yet (SD7). Rows are
dropped as a bound: a leeway row is an entity of unbounded width. Law
**L19**: per task account, metered bytes and instances never exceed the
rule in force at each read or send.

### SD6 — Canaries detect; fingerprints flag

Two exact checks over every outbound payload — the projected request to a
remote route, each outbound intent body, each published body: canary
tokens per scope (`BOXER_CELL_CANARIES`), matched exactly (law **L10**: no
outbound payload contains a live canary of a scope its sink's clearance
excludes); and the four-word shingles of each protected body, which need no
inserted token. Both are cheap and miss a paraphrase, which the integrity
level and the label are for. As built the model route runs both over the
request bytes about to leave and over the decoded strings they carry, and
refuses on a hit above its clearance;
intent bodies and published bodies are not checked yet (SD7). Similarity
fingerprints (`ngramSimHash`) as a reviewer's cue on the trail are
deferred. Leaning: shingles first; inserted tokens where a body has no
shingles worth protecting.

### SD7 — Deferred, recorded

- Differential privacy on aggregates released by sync, until a question the
  group floor does not answer is asked.
- Peers of another organisation.
- A proxy for hosts that do not route fetches through the egress service.
- The organisation approver's path; approval paths for egress, export and
  print; the harness declaring the run's label on fetches; canaries on
  intent and published bodies; read budgets; sinks for the facts write
  path, sync and images; the declaration's move into ADR-0126's topology.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `keelson('sinks')` | added, derived from the topology | the introspection registry; ADR-0126's topology (dated Update) |
| the llm sensitivity wall (ADR-0254 §SD3) and `httpegress` (ADR-0262 §SD4) | read a TLP tier against a clearance; a fetch carries the harness's declared label; canaries on the body | both services' tests; dated Updates on both records |
| ADR-0269 §SD6 budgets | restated as an exit budget in bytes plus read budgets in bytes and instances; rows dropped | the task struct; the grant dialog; a dated Update |
| ADR-0300 §SD2 (proposed) | bytes sent and instances read join the quantities; a Tier 1 edit | the ledger |
| ADR-0270 publish operations | an endorsement check, not a clearance; a dated Update | play's consequential operations |
| ADR-0264 §SD5 and Status | the facts write path's locality is a sink clearance; the Status gap closes when built | the trail's write path; a dated Update |
| trail rows ([ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md)) | outbound model-call rows carry the tier the wall saw and the approval key, for L14 and L33; marks deferred | the generated views |
| `approval` entry ([ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md) §SD2) | written by the chat's approval question, verified by the route | the chat's chrome; the llm service's approval checker |

## Alternatives

- **A hand-listed sink registry**, as the design's first draft. Rejected:
  nothing checks a hand list's completeness; deriving sinks from the
  boundary makes an omission a declaration error, which is visible.
- **Checking labels only at the model route.** Rejected: the spike's exact
  canary found the leak in an outbound intent body as readily as in a
  request; an injected agent chooses its channel.
- **Row budgets**, as ADR-0269 §SD6 lists. Rejected: a leeway row bounds
  nothing.
- **Fingerprints that gate.** Rejected: on sentence-length payloads a
  threshold misfires either way.

## Consequences

### Positive

- "Which sinks exist and what each admits" is a query, and a missing one is a
  declaration error rather than a hunt.
- The person's exits and the agent's are checked by one rule; the agent
  cannot be used to make an exit the person could not make.
- A verbatim leak is found at any sink by two cheap queries.

### Negative

- A recorded justification or an approver for exits above the free tier,
  which a deployment must calibrate or the person approves by reflex.
- Two walls and the publish path change; a facts write path gains a check it
  did not have.
- Canaries find copies, not paraphrases.

### Neutral

- Trusted model hosts are unchanged in effect; they are inside the cell.

## Migration — Tier 1

- **Breaks.** A deployment with no cell declaration has only the compile-date
  walls; nothing else refuses. A deployment that declares one may see
  refusals it did not see before, each naming the sink and the clearance.
- **Path.** Declare the cell; set the default mapping of `confined` to a tier;
  review the approval mapping.
- **Old shape.** One bit at two walls for sealed data, which stays; the
  declaration adds the tier walls beside it.

## Verification plan — Tier 1

- **Lane.** Default: L14 and L33 as Go checks and as SQL run against
  `clickhouse-local` where installed; the guard's ceiling, the verified
  approval, the canaries and the reserved budget as service tests.
  Integration: L10 over the trail's retained bodies. A scene that attempts
  each exit above its clearance is not written.
- **What would fail.** An outbound row above its sink's clearance; a payload
  containing a live canary; an exit above the free tier without an
  `approval`; a fetch with no declared label; a task that sent more bytes
  than its rule.
- **Gap.** Channels that bypass boxer; paraphrased leaks.

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

- [ADR-0126](./0126-appliance-topology-as-data.md) — the topology the sinks derive from.
- [ADR-0254](./0254-model-inference-as-a-keelson-capability.md), [ADR-0262](./0262-http-egress-as-a-keelson-capability.md) — the two walls.
- [ADR-0264](./0264-retained-model-conversations-on-facts.md) — the facts write path's locality.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) §SD6, [ADR-0270](./0270-play-operations-catalog-and-agent-limits.md) — budgets; publish operations; agent limits.
- [ADR-0300](./0300-metered-model-calls-and-rules-a-moderator-writes.md) (proposed) — the ledger.
- [ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md), [ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md) — the threat model and the label.
- [agent-harness](../adr-background-work/agent-harness.md) §4.4, §4.5 and Appendix A (c) — the analysis and the canary spike.
