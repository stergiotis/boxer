---
type: explanation
audience: whoever writes the threat-model ADR for boxer's agent harness, and whoever decides a deployment's cell boundaries, clearances and approval tiers
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.
> Compiled 2026-10-10 from a structured interview with the project owner,
> held after the consolidated design page, now
> [agent-harness](./agent-harness.md), named a threat-model ADR as the first
> decision. The answers are the
> owner's; the consequences drawn from them (§6) are the compiler's and
> await review. Laid out
> so that it can become the ADR with little rewriting. Where it and the
> design page differ, this page wins; the design page's §4 follows it.

# Agent harness: threat model for a cell

The question: against whom, and for what, does boxer's agent harness
enforce, and what does it only record? The answer is stated for one kind of
cell — the interactive, single-person cell — because the owner placed
unattended operation in dedicated cells with a security profile of their
own.

Caveats first. Nothing here is built. Prevention is promised only where the
person does not administer the host; elsewhere the promise is detection.
In-process code stays where
[ADR-0026](../adr/0026-app-runtime-and-capability-subjects.md) put it —
hygiene, not a boundary — and every enforcement below is only as strong as
the boxer build and the host it runs on.

## 1 Context: cells in a multiverse

A **cell** is a boxer deployment's unit of isolation, and its boundary is
**declared by the deployment** — one host, or several (a cell's own
ClickHouse server, its model hosts). Cells form a multiverse: each is its
own universe of data and history, they diverge, and they exchange data only
by **explicit sync**. A cell serves **exactly one person**, and the data in
it is data that person may see.

The **organisation** owns the data. It provisions cells, sets policy, runs
an anchor store, pins the models, and may constrain the person: seeing data
inside the cell does not entitle the person to move it out.

Consequences of those two sentences together:

- Confidentiality *inside* a cell is not a security question. Nothing the
  agent reads inside the cell is hidden from the person it acts for.
- The security boundary is the **cell boundary**, and the questions are
  what crosses it, who caused the crossing, and whether the record of it
  can be trusted.
- The agent is a **channel**: anything it can move out is something the
  person could ask it to move out.

## 2 Principals and their trust

| Principal | Trust | Note |
| --- | --- | --- |
| Organisation and its administrators | trusted | set policy, approve high-tier exits, hold the anchor store; insider threat from administrators is out of scope |
| The cell's person | trusted to see everything in the cell; **constrained** at its boundary | may endorse model-derived content for acts; may release data only as the deployment's tier policy allows |
| The agent (model + harness acting for the person) | untrusted output, task-scoped authority | bound by the organisation's policy even when the person instructs otherwise |
| Peer cells of the same organisation | trusted for content, with per-peer clearance for what is sent to them | marks carried on synced entries survive |
| Remote model providers | adversary for what they receive | outside the cell; retain what they see; their output is untrusted |
| Trusted model hosts (`BOXER_LLM_TRUSTED_HOSTS`) | inside the cell | the deployment's declaration that a model host belongs to the cell |
| Local model weights | pinned by digest, approved by the organisation | not a planted adversary; their output is still untrusted text |

## 3 Adversaries

| | Adversary | In scope as | Promise |
| --- | --- | --- | --- |
| A1 | **careless person** — approves by reflex, pastes model text as their own | error | exits above tier refused or held for approval; model-derived text keeps its marks |
| A2 | **injected agent** — the model steered by content it read | exfiltration, unwanted consequential acts, corruption | prevented at the cell boundary and at consequential acts, by guards that do not depend on the model's cooperation |
| A3 | **remote model provider** | retention of what is sent; untrusted output | nothing above the remote route's clearance is sent; exposure and origin recorded |
| A4 | **constrained person as insider** — moves org data out of the cell | exfiltration by machine channels, directly or through the agent | prevented on org-managed hosts; detected on hosts the person administers; screen and camera attributed, never prevented |
| A5 | **person tampering with the record** on a host they administer | falsified audit | detected by chain heads anchored in an org-held store |

Sources of injected content (A2): content fetched from outside the cell,
files, captures, the model's own earlier output returning through storage,
and synced peer content that a peer marked as model output or external.

## 4 Harms in scope

1. **Exfiltration of organisation data** — labelled data crossing the cell
   boundary by any machine channel, including through the agent, a remote
   model route, or sync.
2. **Unwanted consequential acts** — publishing, deleting or otherwise
   acting against the person's or the organisation's intent, by an injected
   or mistaken agent.
3. **Corruption of cell data** — the agent silently altering data or
   documents, or its output later mistaken for the person's or the system's
   (integrity, re-entry).

## 5 Decisions

### SD1 — The cell boundary is declared data

A deployment declares its cell: hosts, engines and model hosts inside it,
and peers outside it. `BOXER_LLM_TRUSTED_HOSTS` is the first such
declaration already in the tree. The natural home for the rest is the
appliance topology of [ADR-0126](../adr/0126-appliance-topology-as-data.md):
an edge that leaves the declared cell is a **sink**, and every sink is
registered with a clearance (design §4.5).

### SD2 — Confidentiality tiers are TLP 2.0

`CLEAR < GREEN < AMBER < AMBER+STRICT < RED`, read from leeway's existing
TLP use-aspects. Which tier today's binary `confined` maps to is a
deployment default, not fixed by boxer. Sinks, peers and remote routes carry clearances in
the same terms.

### SD3 — Exits: prevent machine channels, attribute analog ones

| Channel | Org-managed host | Host the person administers |
| --- | --- | --- |
| clipboard out of boxer, file export, printing | prevented above the tier policy; copy by reference inside boxer | detected and recorded; hygiene only |
| sync to a peer | data above the peer's clearance does not leave | detected |
| remote model route | data above the route's clearance is never sent | detected |
| outbound part of a fetch (URL, headers, body) | the HTTP egress service or a proxy | the same, if the host routes through it |
| screen, camera | attributed by watermark | attributed |

The **approval path** for an exit above a channel's free tier is **policy
data per deployment**: boxer ships a default mapping from tiers to "free",
"person with a recorded justification", "organisation approver" and
"never", and enforces whatever the deployment declares.

### SD4 — The organisation binds the agent, over the person

The agent cannot be used to make an exit the person could not make
directly: encoding data into SQL text, a document, a file or a fetch is
checked as the exit it is. Guards enforce the organisation's policy
regardless of who asked.

### SD5 — Agents are read-only outside the cell

An agent may fetch from outside the cell; it may not send, publish, sync or
act outside it, with one exception:

- a **remote model call** is the one outward send an agent makes — through
  the model-route sink, under the route's clearance;
- the **outbound half of a fetch** (URL, headers, body) can carry data; it
  is the HTTP egress service's or a proxy's to police
  ([ADR-0262](../adr/0262-http-egress-as-a-keelson-capability.md)), not the
  harness's. What comes back is external content (level 2).

Sync, publishing to peers and export are the person's acts, possibly
prepared by an agent.

### SD6 — Integrity levels and endorsement

| Level | Source |
| --- | --- |
| 0 | the person; harness text |
| 1 | deterministic system data; same-org peer content not otherwise marked |
| 2 | external content (fetched, files, captures) |
| 3 | this run's model output returning through storage |
| 4 | another run's model output |

**Remote origin** is a separate mark on levels 3–4, not a lower level.
No consequential act whose justification is at level 3–4 proceeds without
the **person's endorsement**, by digest and act class (design §4.2). Marks
on synced entries survive sync: a peer's model output arrives as model
output.

### SD7 — Remote model involvement is recorded

Two provenance marks, propagated by join like labels:

- **exposure** — which entries were sent to which remote provider;
- **origin** — which text, SQL, documents and acts derive from a remote
  model's output, through compaction, artefacts and sync.

Both are recorded for audit. They are neither shown to the person nor
used by guards by default; a deployment may opt into either later without
a change of data shape.

### SD8 — Records must convince four readers

| Reader | Must be able to |
| --- | --- |
| organisation security officer | reconstruct what left which cell, what caused it, under which approval |
| external regulator | check the record without trusting the person or the software |
| the person | see what the agent saw and did on their behalf, and contest it |
| incident responder | after a suspected injection, find every act and exit that depended on the poisoned content |

Records are made **tamper-evident by chain heads** (digests over the
journal and trail) **shipped to an org-held store** outside any person's
control. The regulator's check is then: the anchored heads match the
cell's record, and every recorded request re-derives to its digest (law
L4). What a regulator cannot get from a host the person administers is
**completeness** — an exit that bypassed boxer leaves no record; the
threat model says so rather than implying otherwise.

### SD9 — Trusted by assumption

The pinned boxer build and its dependencies; on org-managed hosts, the
operating system, ClickHouse and the model runtime; the organisation's
administrators; the pinned model weights.

### SD10 — Out of scope, recorded

- hostile in-process code (ADR-0026);
- a compromised operating system on an org-managed host;
- insider threat from organisation administrators;
- planted or backdoored model weights;
- resource exhaustion — a governance concern of rationing and the
  moderator (ADR-0300, ADR-0302), not a security harm;
- data-subject erasure — no erasure obligation applies to the harness's
  records;
- prevention against screens and cameras;
- unattended operation, which belongs to dedicated cells with their own
  security profile (integration, validation) — a separate threat model.

## 6 What this changes in the design

| Design element | Change |
| --- | --- |
| non-Truman agent schema (design §4.3) | no longer access control against the person; kept for least privilege against A2 and for not misleading the model; the `withheld` marker stays |
| labels (design §4.2) | confidentiality is TLP; remote origin and remote exposure become marks; same-org peer content enters at level 1 with carried marks |
| sinks (design §4.5) | derived from the declared cell boundary; agents have no send sink except the model route; fetch requests stay with the egress service |
| remote routes | a sink with a TLP clearance; trusted hosts are inside the cell |
| clipboard and export (design §4.8) | needed against A4 on org-managed hosts; on administered hosts, recorded only |
| watermark | attribution for A4's analog channel; its live path is unqualified (ADR-0241), which this model now relies on for screens |
| encryption and key use (design §4.7) | with erasure out of scope, per-run keys would serve only re-entry detection; the key-id column stays reserved and encryption is deferred; re-entry is detected by journal metadata where it survives |
| self-improvement (design §5.6) | unaffected; profiles carry TLP labels; a profile tuned on AMBER journals cannot be used on a route cleared below AMBER |
| unattended mode (ADR-0298) | moves to dedicated cells with their own profile |
| tamper evidence | new: chain heads per cell to an org-held store |

## 7 Open questions

1. Peers of *another* organisation: presumably level 2 content and a
   clearance per peer; not asked.
2. The anchoring cadence (per turn, per run, per sync) and what the
   anchor store must hold to let a regulator check without the cell.
3. The watermark on live frames, which this model relies on for the
   screen and ADR-0241 leaves unqualified.
4. What the HTTP egress service must check on the outbound half of an
   agent-caused fetch, now that the harness relies on it (ADR-0262 §SD4
   carries a sensitivity label no consumer forwards yet).
5. The security profile of dedicated unattended cells.
