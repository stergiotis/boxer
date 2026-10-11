---
type: adr
status: accepted
date: 2026-10-10
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-11
---

# ADR-0307: Threat model for an interactive single-person cell — the boundary is declared, exits are the sinks, the record must convince four readers

## Context

[ADR-0026](./0026-app-runtime-and-capability-subjects.md) states the threat
model for in-process code: the capability system makes resource access
visible, consistent and auditable, and is "hygiene, not security"; its
2026-07-15 Update reaffirms that. The agentic tier adds principals that are
not in-process code — a model, the text it reads, the provider that runs it,
and a person who may be constrained by the organisation whose data the
deployment holds — and every ADR of that tier so far has stated its measure
without a threat model to cite: the sensitivity wall
([ADR-0254](./0254-model-inference-as-a-keelson-capability.md) §SD3,
[ADR-0262](./0262-http-egress-as-a-keelson-capability.md) §SD4), taint and
untrusted content
([ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
§SD7), captures through one enforcement point
([ADR-0281](./0281-window-captures-through-one-policy-enforcement-point.md)).

This record states against whom the harness enforces and for what, and what
it only records. It is stated for one kind of deployment, the interactive
cell serving one person, because the owner placed unattended operation in
cells with a security profile of their own. The analysis behind it is
[agent-harness §4](../adr-background-work/agent-harness.md) and the interview
record [agent-harness-threat-model](../adr-background-work/agent-harness-threat-model.md).

## Decision

We will state security for the agentic tier in terms of a **cell**: one
deployment serving one person, whose boundary the deployment declares and
whose data the organisation owns. Confidentiality inside the cell is not a
security question. The security boundary is the cell boundary; the questions
are what crosses it, who caused the crossing, and whether the record of it
can be trusted. The agent is a channel: anything it can move out is
something the person could ask it to move out, and the organisation's policy
binds the agent over the person.

### SD1 — The cell boundary is declared data

A deployment declares its cell: the hosts, engines and model hosts inside
it, and the peers outside it. `BOXER_LLM_TRUSTED_HOSTS` is the first such
declaration already in the tree and is reinterpreted as "this model host is
inside the cell". The declaration as built is the `cell` package's
`Declaration`, read once at boot from nine variables
([ADR-0009](./0009-environment-variable-registry.md)) with the trusted hosts
as an input, and handed to every service that guards a sink:
`BOXER_CELL_ROUTE_CLEARANCE`, `BOXER_CELL_EGRESS_CLEARANCE`,
`BOXER_CELL_CLIPBOARD_CLEARANCE` and `BOXER_CELL_EXPORT_CLEARANCE` for the
sinks, `BOXER_CELL_PEERS`, `BOXER_CELL_CONFINED_TIER` (SD5),
`BOXER_CELL_APPROVALS` (SD6), `BOXER_CELL_CANARIES` (ADR-0312 §SD6) and
`BOXER_CELL_MANAGED`, which says the organisation manages the host (SD6). Its intended home is the
appliance topology of [ADR-0126](./0126-appliance-topology-as-data.md), which
gains the cell as a node attribute by a dated Update when that move is made;
the variables are the interim carrier. An edge that leaves the declared cell
is a **sink**, and every sink carries a clearance
([ADR-0312](./0312-exits-sinks-from-the-cell-boundary-approval-tiers-and-exit-budgets.md)).

### SD2 — Principals and their trust

| Principal | Trust |
| --- | --- |
| organisation and its administrators | trusted; set policy, approve high-tier exits, hold the anchor store |
| the cell's person | trusted to see everything in the cell; constrained at its boundary; may endorse model-derived content for acts |
| the agent — model plus harness acting for the person | untrusted output, task-scoped authority; bound by the organisation's policy even when the person instructs otherwise |
| peer cells of the same organisation | trusted for content, with a clearance per peer for what is sent to them; marks carried on synced entries survive |
| remote model providers | adversary for what they receive; outside the cell; their output is untrusted |
| trusted model hosts | inside the cell by the deployment's declaration |
| local model weights | pinned by digest and approved by the organisation; not an adversary; their output is still untrusted text |

### SD3 — Adversaries and the promise against each

| | Adversary | In scope as | Promise |
| --- | --- | --- | --- |
| A1 | careless person — approves by reflex, pastes model text as their own | error | exits above tier refused or held for approval; model-derived text keeps its marks |
| A2 | injected agent — the model steered by content it read | exfiltration, unwanted consequential acts, corruption | prevented at the cell boundary and at consequential acts, by guards that do not depend on the model's cooperation |
| A3 | remote model provider | retention of what is sent; untrusted output | nothing above the route's clearance is sent; exposure and origin recorded |
| A4 | constrained person as insider — moves organisation data out | exfiltration by machine channels, directly or through the agent | prevented on org-managed hosts; detected on hosts the person administers; screen and camera attributed, never prevented |
| A5 | person tampering with the record on a host they administer | falsified audit | detected by chain heads anchored in an org-held store |

Sources of injected content: content fetched from outside the cell, files,
captures, the model's own earlier output returning through storage, and
synced peer content a peer marked as model output or external.

### SD4 — Harms in scope

1. **Exfiltration of organisation data** — labelled data crossing the cell
   boundary by any machine channel, including through the agent, a remote
   model route, or sync.
2. **Unwanted consequential acts** — publishing, deleting or otherwise
   acting against the person's or the organisation's intent, by an injected
   or mistaken agent.
3. **Corruption of cell data** — the agent silently altering data or
   documents, or its output later mistaken for the person's or the system's.

### SD5 — Confidentiality tiers are TLP 2.0

`CLEAR < GREEN < AMBER < AMBER+STRICT < RED`, read from leeway's existing
exclusive `tlp` use-aspect family. Which tier the compile-date binary
`confined` maps to is a deployment default, not fixed by boxer:
`BOXER_CELL_CONFINED_TIER`, AMBER unless set, RED on a value that does not
parse (a content tier fails high, a clearance fails low). Sinks, peers and
remote routes carry clearances in the same terms. The label that carries
the tier, with an integrity level and provenance marks, is
[ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md).

### SD6 — Exits: prevent machine channels, attribute analog ones

| Channel | Org-managed host | Host the person administers |
| --- | --- | --- |
| clipboard out of boxer, file export, printing | prevented above the tier policy; copy by reference inside boxer | detected and recorded; hygiene only |
| sync to a peer | data above the peer's clearance does not leave | detected |
| remote model route | data above the route's clearance is never sent | detected |
| outbound part of a fetch (URL, headers, body) | the HTTP egress service or a proxy, given the label the harness declares | the same, if the host routes through it |
| screen, camera | attributed by watermark where the geometry survives | attributed |

The approval path for an exit above a channel's free tier is **policy data
per deployment**: boxer ships a default mapping from tiers to *free*, *person
with a recorded justification*, *organisation approver* and *never*, and
enforces whatever the deployment declares (ADR-0312). As built, the person's
path is the one that opens an exit; the organisation path refuses until an
approver is wired, and the mapping is one table per deployment rather than
one per channel kind. The compile-date `confined` bit keeps its two walls
for every route outside the cell, managed or not: sealed data never leaves
by a machine channel, and the tier walls with their approval paths apply to
labelled tiers beside it.

### SD7 — The organisation binds the agent, over the person

The agent cannot be used to make an exit the person could not make directly.
Encoding data into SQL text, a document, a file or a fetch is checked as the
exit it is. Guards enforce the organisation's policy regardless of who asked;
no grant the person gives can lower a guard the deployment declared.

### SD8 — Agents are read-only outside the cell

An agent may fetch from outside the cell; it may not send, publish, sync or
act outside it, with one exception: a **remote model call** is the one
outward send an agent makes, through the model-route sink under the route's
clearance. The outbound half of a fetch can carry data and is the HTTP
egress service's to police (ADR-0262), against the label the harness declares
on every agent-caused fetch. What comes back is external content. Sync,
publishing to peers and export are the person's acts, possibly prepared by an
agent. Publishing *inside* the cell — an ad-hoc dataset another window reads
— is a consequential act under harm 2, not an exit.

### SD9 — Integrity, endorsement and marks

Integrity is ranked in five levels — the person and harness text; deterministic
system data and same-organisation peer content not otherwise marked; external
content; this run's model output returning through storage; another run's
model output. No consequential act whose justification is at the two lowest
levels proceeds without the person's endorsement, by digest and act class.
Remote model involvement is recorded as two marks propagated like labels,
*exposure* (which entries were sent to which provider) and *origin* (what
derives from a remote model's output); both are recorded for audit, neither is
shown or enforced by default. The data shapes are ADR-0311's.

### SD10 — Records must convince four readers

| Reader | Must be able to |
| --- | --- |
| organisation security officer | reconstruct what left which cell, what caused it, under which approval |
| external regulator | check the record without trusting the person or the software |
| the person | see what the agent saw and did on their behalf, and contest it |
| incident responder | after a suspected injection, find every act and exit that depended on the poisoned content |

Records are made tamper-evident by chain digests over the journal whose
heads are shipped to an org-held store outside any person's control
([ADR-0314](./0314-tamper-evident-records-chain-digests-and-anchored-heads.md)).
A deployment without such a store, the shipped default, holds its heads in
the process and records them as anchoring nothing; chaining the trail is
deferred there. What a regulator cannot get from a host the person
administers is **completeness**: an exit that bypassed boxer leaves no
record, and this record says so rather than implying otherwise.

### SD11 — Trusted by assumption

The pinned boxer build and its dependencies; on org-managed hosts, the
operating system, ClickHouse and the model runtime; the organisation's
administrators; the pinned model weights.

### SD12 — Out of scope, recorded

- hostile in-process code (ADR-0026 stands unchanged);
- a compromised operating system on an org-managed host;
- insider threat from organisation administrators;
- planted or backdoored model weights;
- resource exhaustion — a governance concern of
  [ADR-0300](./0300-metered-model-calls-and-rules-a-moderator-writes.md)
  (proposed) and [ADR-0302](./0302-a-built-in-moderator-loop-signals-and-an-escalation-ladder.md)
  (proposed), not a security harm;
- data-subject erasure — no erasure obligation applies to the harness's
  records, which is why per-run encryption of bodies is deferred and only a
  key-id column is reserved
  ([ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md));
- prevention against screens and cameras;
- unattended operation
  ([ADR-0298](./0298-an-unattended-mode-the-host-decides-within-preset-bounds.md)
  (proposed)), which belongs to dedicated cells with their own security
  profile and a separate threat model;
- peers of another organisation, pending a decision; presumably external
  content with a clearance per peer.

## Alternatives

- **The four-adversary model without a cell** (the earlier draft's A1–A4
  with "insider with a phone"). Rejected: without an owner and a declared
  boundary it treated confidentiality inside a single-person deployment as a
  security question, which made the agent schema access control against the
  person and left no principal who could approve an exit.
- **CaMeL's privileged-planner and quarantined-data split.** Stays deferred
  as ADR-0269 §SD12 records; labels are checked at sinks instead. Reopened by
  the trigger that ADR names.
- **Per-user database access control as the security mechanism.** Rejected:
  a row policy is a Truman mechanism that misleads a model, and inside the
  cell there is no one to hide rows from
  ([ADR-0313](./0313-agent-schema-generated-relations-an-agent-may-read-in-full.md)).
- **Prevention against screens and cameras.** Rejected as unachievable;
  attribution by a cooperative watermark is what
  [ADR-0241](./0241-cooperative-image-identification-watermark.md) can give,
  and a rescale defeats it.

## Consequences

### Positive

- Every later security measure has one record to cite for "why", and the
  measure's reach is stated: prevention on org-managed hosts, detection
  elsewhere, attribution for analog channels.
- The agent schema, encryption and aggregation controls shrink to what the
  cell needs; the earlier drafts' most expensive mechanisms (per-run
  encryption, small-group suppression against the agent) are deferred with a
  reason.

### Negative

- A deployment must keep a cell declaration true; an edge boxer mediates but
  the declaration omits is unguarded.
- Tamper evidence needs an organisation to run an anchor store; without one,
  SD10 is a promise with no reader.
- The approval tiers add gestures; if asked too often the person approves by
  reflex, which is A1.

### Neutral

- ADR-0026's posture for in-process code is unchanged.
- The model stated here is for one cell kind; unattended cells need their own.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| ADR-0126 topology | a cell attribute per node and a peer list, by dated Update when the declaration moves there; until then the `BOXER_CELL_*` registry entries carry it | the topology's typed tables and graph projection |
| `BOXER_LLM_TRUSTED_HOSTS` (ADR-0254) | reinterpreted: a host inside the cell; no wire change; read as an input of the `cell` declaration | its registry entry's description (ADR-0009) |
| ADR-0026 | a dated Update naming this record as the threat model for principals that are not in-process code | none in code |
| ADR-0298 | a dated Update recording that unattended mode runs in dedicated cells outside this model | none in code |

## Migration — Tier 1

- **Breaks.** Nothing; the record adds data shapes other ADRs build.
- **Path.** None.
- **Old shape.** A deployment without a cell declaration behaves as before:
  one bit at two walls, which the declaration adds the tier walls beside.

## Verification plan — Tier 1

- **Lane.** None of its own: the laws that make SD6, SD9 and SD10 checkable
  are ADR-0312's (L14, L33), ADR-0311's (L15–L17) and ADR-0314's (L31, L32),
  as Go checks in the default lane over the chat's preservation journals and
  as SQL in the integration lane.
- **What would fail.** A row leaving the cell above its sink's clearance; a
  consequential act at a low integrity level without an endorsement; an
  anchored head that does not match the record.
- **Gap.** Channels that bypass boxer on a host the person administers; the
  record cannot see them, and SD10 says so.

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

- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — the in-process threat model this record leaves standing.
- [ADR-0126](./0126-appliance-topology-as-data.md) — the home of the cell declaration.
- [ADR-0241](./0241-cooperative-image-identification-watermark.md) — attribution for screens.
- [ADR-0254](./0254-model-inference-as-a-keelson-capability.md), [ADR-0262](./0262-http-egress-as-a-keelson-capability.md) — the two walls.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — grants, taint, untrusted content.
- [ADR-0298](./0298-an-unattended-mode-the-host-decides-within-preset-bounds.md) (proposed) — unattended mode, out of scope here.
- [agent-harness](../adr-background-work/agent-harness.md) §4 and [agent-harness-threat-model](../adr-background-work/agent-harness-threat-model.md) — the analysis and the interview record.
