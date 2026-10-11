---
type: adr
status: accepted
date: 2026-10-11
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-11
---

# ADR-0311: Labels — an integrity level, a TLP tier and two provenance marks on every journal entry, lowered only by a journaled endorsement

## Context

The label machinery on the compile date is one `confined` bit checked at the
sensitivity walls of [ADR-0254](./0254-model-inference-as-a-keelson-capability.md)
and [ADR-0262](./0262-http-egress-as-a-keelson-capability.md), a
per-conversation `tainted` flag
([ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
§SD7) that is displayed and recorded but never gates, and `Untrusted` marks
that delimit content for the model. No endorse, declassify or lowering
primitive exists, and the trail's generated views pass `confined` and
`tainted` through as columns, stating that enforcement is the reader's.

The threat model ([ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md)) needs more: confidentiality in TLP tiers so that sinks, peers and
routes carry clearances in the same terms; integrity in levels so that a
model's own output coming back through storage is told apart from a person's
instruction; and a record of remote model involvement. The literature read
for [agent-harness §1.3 and §4.2](../adr-background-work/agent-harness.md)
gives the shape: a product of an integrity and a confidentiality lattice
(Fides), labels through every reduction with `endorse` as the only lowering,
on the integrity axis (LLMbda), a floating current label bounded by a
clearance (LIO), integrity falling to the lowest observed (Biba), and the
warning that an endorsement asked too often is approved by reflex (CaMeL).

## Design space (QOC)

**Question.** What is the label on a journal entry?

**Options.**

- **O1 — Two bits**, `tainted` and `confined`, as the trail carries.
- **O2 — Integrity level × TLP tier**, marks added later as columns.
- **O3 — Integrity level × TLP tier × exposure and origin marks.**

**Criteria.**

- **C1 — Carries the threat model**: clearances in TLP, levels for
  endorsement, remote involvement recorded.
- **C2 — No later migration** of stored journals.
- **C3 — Cost now**.
- **C4 — A join-semilattice**, so projections and guards compute the label
  by one operation.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | −− | +  | ++ |
| C2 | −− | −  | ++ |
| C3 | ++ | +  | −  |
| C4 | +  | ++ | ++ |

O3 is taken: the marks cost two set columns that no guard reads by default,
and adding them later would migrate every stored journal.

## Decision

We will give every journal entry, every trail row that leaves the cell, and
every resource revision a **label** with three components — an integrity
level, a TLP 2.0 tier, and two set-valued provenance marks — that form a
join-semilattice, propagate by join through projections, are checked at
sinks, and are lowered only by a journaled `endorse` entry, on the integrity
axis alone.

### SD1 — The label

| Component | Values | Join |
| --- | --- | --- |
| integrity level | 0 the person and harness text; 1 deterministic system data and same-organisation peer content not otherwise marked; 2 external content (fetched, files, captures); 3 this run's model output returning through storage; 4 another run's model output | maximum (lowest trust wins) |
| confidentiality tier | `CLEAR < GREEN < AMBER < AMBER+STRICT < RED`, the exclusive `tlp` use-aspect family of leeway | maximum |
| exposure mark | the set of remote providers an entry was sent to | union |
| origin mark | the set of remote routes whose output the entry derives from | union |

Which tier the compile-date binary `confined` maps to is a deployment
default (`BOXER_CELL_CONFINED_TIER`, ADR-0307 §SD5), the tier sealed data
carries in the journal; the compile-date wall for sealed data at every
route outside the cell stands beside the tier walls. On the journal the four
components are the columns `label_level`, `label_tier`, `label_exposure`
and `label_origin`. A projection's label is the join over the entries it
read, carried per message by the same pass that builds the request
(ADR-0309 §SD3's provenance column), so labels and costs come from one
annotation.

### SD2 — Floating

A run's confidentiality floats upward as it reads and its integrity floats
downward: the run row carries the current label, joined by the store from
every appended entry and never lowered by an update, and resume restores it
([ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md)
§SD6). Nothing lowers confidentiality but deletion. A stateful
session (a kernel, a play window) floats the same way until a reset, which is
its explicit lowering.

### SD3 — Endorsement is the only lowering

No consequential act whose justification is at integrity level 3 or 4
proceeds without the person's recorded consent (law **L15**). The consent
is one of two entries written before the act's outcome is recorded: an
`endorse` entry, when the person decided the call at the dispatcher's
dialog — it names who endorsed (as `AgentGrant.DecidedBy` records a grant),
the **digest** of the arguments as shown, the **display epoch** (the turn;
ADR-0317's watermark epoch when that lands), the **act classes** it covers,
and the diff since the last endorsement of the same artefact or a flag that
none exists — or the `answered` entry (form `grant`) of the task grant in
force, which is the person's standing consent and is journaled when the
grant is given. The artefact's own Endorse button writes an `endorse` entry
for the publish class with the revision as its epoch. Each is a deliberate
gesture, never a pane closing: a scroll past is not a review. A later
model-written revision voids an artefact endorsement; a later person edit
does not, because the person's edit is level 0 by source. An endorsement
lifts one body for named act classes; it never lifts a conversation.

### SD4 — Re-entry is detected by metadata and marked in band

A body a model run wrote carries its writer run, turn and ordinal as
plaintext metadata beside it. When a projection reads such a body, its level
is 3 if the writer run is the reader's and 4 otherwise (law **L16**), and the
body is wrapped in band, `<recalled source="self" run="…" ord="…">`, for the
reason ADR-0269 §SD7 delimits untrusted content: the model is told that this
is its own earlier output, not an instruction (law **L17**). A closing tag
inside the body and the quotes in the attribute are escaped, the same way
in both texts of the projection. A compaction summary and an elision
re-enter the same way, at level 3 at least (ADR-0316 §SD3), and so do an
elided result the `recall` tool brings back and an artefact read after a
model wrote it. The marking is a projection pass. Per-run encryption, which
would make the detection survive a copy that drops the metadata, is deferred
with a reserved key column (ADR-0307 §SD12); a body that arrives with no
writer metadata enters at level 2.

### SD5 — Mixed documents

The chat artefact ([ADR-0282](./0282-chat-artefact-one-markdown-document-per-conversation.md))
and a play resource are edited by both the person and the model. The writer
of each revision is known (`opwire.CallRequest.Writer`, the engine's
last-writer map, `AgentAction`'s delegation). A document's label is the
join over its revisions' writers: integrity the lowest level among writers
whose text survives (a person's edit over an agent's paragraph does not
lift the paragraph), confidentiality the join of what each writer had read,
marks the union. As built, the chat tracks the artefact's last
model-written revision, so a read of it re-enters under SD4 and the
endorsement rule of SD3 sees a model revision; per-revision labels on the
resource's command log, folded by the projection, are deferred (SD8). An
unrecognised paste enters at level 2 and `CLEAR`.

### SD6 — Where labels bind elsewhere

A compaction summary's confidentiality is at least the join of what it
replaces and its integrity at least the model's own, level 3, since the
summary is model output re-entering (**L3**,
[ADR-0316](./0316-compaction-as-a-journal-entry-plan-act-apply.md) §SD3);
a reply's label is at least its request's (**L6**); a selection carries the
labels of the verdicts that decided it (**L8**), so a verdict from a remote
route is level 4 with an origin mark; a profile carries the labels of the
journals it was tuned on
([ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md)
§SD7); a capture is level 2 ([ADR-0281](./0281-window-captures-through-one-policy-enforcement-point.md)
§SD6's "untrusted and taints the conversation", in these terms). L8 and the
profile's label wait for fan-out and a second profile.

### SD7 — Marks are recorded, not shown, not enforced

Exposure and origin are for the security officer and the incident responder.
They are neither shown to the person nor read by any guard by default; a
deployment may opt into either without a change of data shape. Marks are
monotone and saturate — one summary on a remote route marks everything
downstream — so their value lies in the per-message provenance the
projection carries, which names the first remote entry.

### SD8 — Deferred, recorded

- Finer confidentiality than TLP (compartments), and labels per image
  ([ADR-0287](./0287-chat-pixels-a-setting-for-showing-screenshots-to-the-model.md) §SD7).
- Showing marks to the person; enforcing them.
- Per-run encryption and the `keyuse` entry.
- Per-revision labels on resource command logs (SD5); marks on trail rows
  (the trail carries the tier).
- Checkers for L6 and L8 as queries over stored journals, and for L26 (no
  consequential command ran without a consent entry), which L15's check
  covers for completed acts; none is written, since the integration lane
  runs no law queries yet.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| journal entry label (ADR-0308 §SD1) | the three components | the store's generator test |
| `queryengine.SensitivityConfined` readers: the llm sensitivity wall, `httpegress` | read a tier, with `confined` mapped by deployment default | the two services' tests; ADR-0254 §SD3 and ADR-0262 §SD4, dated Updates |
| ADR-0269 §SD7 taint | taint is the integrity axis and gates at sinks; untrusted content keeps its delimiting; a dated Update | the coordinator's prompt text |
| ADR-0281 §SD6, ADR-0270 §SD4 | wording: "untrusted" is level 2, "confined" a tier; dated Updates | trail row labels |
| resource revisions (ADR-0269 §SD8, ADR-0282) | a label per revision in the command log | the operations engine; a dated Update on each |
| trail rows leaving the cell ([ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md)) | carry the tier the wall saw; marks deferred | the trail's generated views |
| `endorse` gesture | the dispatcher's decision dialog and the artefact's Endorse button, each a journal entry | the chat's and the dispatcher's chrome |

## Alternatives

O1 and O2 are weighed in the QOC. Two narrower choices:

- **Remote origin as a lower integrity level** rather than a mark. Rejected:
  it would rank a remote model's output below a local one's as a matter of
  trust, which the threat model does not claim; the mark records, the level
  ranks.
- **Endorsement by conversation** ("this conversation is reviewed").
  Rejected: an endorsement of one artefact must not lift everything the
  conversation holds; the digest and act class are what make it specific.

## Consequences

### Positive

- Sinks, peers and routes have clearances in the terms the organisation
  already uses for dissemination.
- A model's own output coming back is told apart from the person's, in band
  and in the record, and cannot justify a consequential act alone.
- A security officer can answer "what did a remote provider see" from the
  exposure mark.

### Negative

- An endorsement gesture before consequential acts on model-written text;
  asked too often it becomes reflex, which is the threat model's A1.
- Two set columns on every entry that nothing reads by default.
- Re-entry detection by metadata does not survive a channel that drops the
  metadata; inside boxer that channel is narrow (ADR-0317), outside it is an
  exit.

### Neutral

- The `Untrusted` delimiting of ADR-0269 §SD7 is unchanged; it gains a level.

## Migration — Tier 1

- **Breaks.** Nothing: the trail's `confined` and `tainted` columns keep
  their meaning as projections of the new label; readers of the two walls see
  the same refusals under the deployment's default mapping.
- **Path.** The mapping of `confined` to a tier is set once per deployment.
- **Old shape.** Rows written before the change carry no marks and level 2
  where they carried taint.

## Verification plan — Tier 1

- **Lane.** Default: the join is a lattice (property tests); L15, L16 and
  L17 as Go checks over the projection's corpus and the preservation
  harness's journals; L3 within the compaction checks of ADR-0316. L6 and
  L8 as queries over stored journals are not written.
- **What would fail.** A consequential outcome at level 3–4 without an
  earlier `endorse` or a grant in force; a reply labelled below its
  request; a summary labelled below what it replaced; a projected
  model-written body without the recalled marker; a selection without its
  verdicts' labels.
- **Gap.** Whether the person reads what they endorse is not a property the
  record can check.

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

- [ADR-0254](./0254-model-inference-as-a-keelson-capability.md), [ADR-0262](./0262-http-egress-as-a-keelson-capability.md) — the two walls that read the tier.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) §SD7, §SD8 — taint, untrusted content, writers.
- [ADR-0270](./0270-play-operations-catalog-and-agent-limits.md) §SD4, [ADR-0281](./0281-window-captures-through-one-policy-enforcement-point.md) §SD6, [ADR-0282](./0282-chat-artefact-one-markdown-document-per-conversation.md), [ADR-0287](./0287-chat-pixels-a-setting-for-showing-screenshots-to-the-model.md) — labels these records carry today.
- [ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md) — the threat model.
- [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md) — the entry the label sits on.
- [agent-harness](../adr-background-work/agent-harness.md) §4.2, §4.6, §4.7 — the analysis and the papers (Denning 1976; Biba 1977; Myers and Liskov 1997; LIO 2011; CaMeL, Fides, LLMbda).
