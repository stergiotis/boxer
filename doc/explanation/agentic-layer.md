---
type: explanation
audience: a contributor or integrator meeting boxer's chat and agent machinery for the first time
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-28
---

# The agentic layer — what the pieces are, and the choices that shape them

Boxer lets a language model work inside the desktop on the person's behalf:
read what windows hold, change them, run queries, write a document, ask the
person a question. This page is the map. It names the parts, says what each
is for, and gives the one reason behind each choice, so that a reader can
then open the right ADR. The ADRs are the record; where this page and an ADR
disagree, the ADR wins. The design study behind most of it is
[agent-harness](../adr-background-work/agent-harness.md); the threat model is
[ADR-0307](../adr/0307-threat-model-for-an-interactive-single-person-cell.md).

## The cast

| Word | Meaning here | Where it is decided |
| --- | --- | --- |
| person | the human at the windows; trusted inside the cell, constrained at its edge | ADR-0307 §SD2 |
| model | the language model; it acts only through tool calls, and its output is untrusted text | ADR-0269, ADR-0307 |
| harness, or coordinator | the one app that runs the model's loop: the chat app. It holds the model grant, writes the journal, enforces the person's settings | [ADR-0265](../adr/0265-chat-app-over-retained-model-calls.md), [ADR-0318](../adr/0318-model-inference-is-the-harnesss-apps-expose-operations.md) |
| app | any other window: play, mdedit, a map. It declares operations and never calls the model itself | [ADR-0269](../adr/0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) |
| task | one unit of delegated work in one conversation, with one grant | ADR-0269 §SD6 |
| grant | the host's record of what a task may do: which windows, which operations, in which mode, with which budgets | ADR-0269 §SD6 |
| run, or journal | one conversation's append-only record, the only thing the model is ever shown | [ADR-0308](../adr/0308-the-run-journal-is-the-only-source-of-model-input.md) |
| cell | one person, one host, one ClickHouse server: the declared boundary everything else is measured against | ADR-0307 §SD1 |

## One turn, end to end

```text
 person types ──▶ chat window ──▶ journal: user entry ──▶ projection ──▶ model service ──▶ model
                                       ▲                                   (wall, ration, trail)
                                       │                                           │
                               outcome entry                               reply with tool calls
                                       │                                           │
                 app window ◀── dispatcher checks the grant ◀── coordinator: intent entry first
```

Every arrow that leaves the host is preceded by a journal entry. That one
rule, "write before you act", is what lets a conversation survive a crash
and lets a reviewer trust the record; most of the rest of this page is a
consequence of it.

## The choices, one at a time

### 1. Only the harness talks to the model

An app that wants model work done on its content does not call the model.
It declares operations that read the content and accept a result, and the
chat does the model work under the person's settings. Model inference is a
declared capability of the host
([ADR-0254](../adr/0254-model-inference-as-a-keelson-capability.md)), and the
chat is meant to be its only app-side holder
([ADR-0318](../adr/0318-model-inference-is-the-harnesss-apps-expose-operations.md)).

*Why:* the loop, the approvals, the journal, the ceilings and the labels
live in one place. A second caller would run beside them, not under them.
The rule is a review convention, not an enforced one; the ADR says so.

### 2. Apps expose operations; agents drive them under a grant

An app's agent-facing surface is a catalog of **commands** and **queries**
with typed arguments, each declaring an **effect**: none, view, document,
run, or consequential
([ADR-0269](../adr/0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)).
The model works only in windows the person shared for a task, each in a
**mode**: observe, suggest, or act. Every call passes the host's dispatcher,
which checks it against the grant, stamps it, and records it. The person's
own input always lands first; a consequential command is confirmed each
time. Agents also read the desktop as data and may arrange windows
([ADR-0276](../adr/0276-agents-read-and-arrange-windows.md)) and point at
things through an overlay anchored to windows
([ADR-0297](../adr/0297-inscribe-an-overlay-agents-annotate-the-desktop-through.md),
proposed).

*Why not drive the UI by clicking?* A click reaches only widgets, breaks on
a layout change, and cannot be checked against a declared purpose. A
declared operation can be refused, previewed, undone and audited.

### 3. The person sets a ceiling

Before a task starts, the person picks how far a chat's model may go on a
six-step ladder: talk only, read, change the view, edit, run, act outside
([ADR-0280](../adr/0280-a-ceiling-on-what-a-chats-model-may-do-scored-on-a-ladder.md)).
The dispatcher refuses anything above it, whatever the grant says, and
speed is granted separately as a pace.

*Why a ladder:* a person compares two positions by the most powerful thing
each allows. Scoring the grant and the ceiling on one scale lets both be
drawn as two markers on one widget.

### 4. The journal is the only memory

The chat keeps no message list. Prompt sections, the tool set, the person's
words, the model's replies, every intended call and its outcome are
entries of one run, appended and flushed before the act leaves
([ADR-0308](../adr/0308-the-run-journal-is-the-only-source-of-model-input.md)).
Each request to the model is a **projection** of those entries, rebuilt
every time ([ADR-0309](../adr/0309-context-projection-as-sql-over-the-journal-in-a-local-worker.md)).
A conversation therefore outlives its process: the Conversations tab lists
runs, and **resume** folds one back, settles each unfinished act by its
effect class, and asks for a grant again as a new task. A run is pinned to
the fingerprint of the code it started with; a mismatch leaves it readable
and not continuing. The chat's **Keep** setting governs only whether the
message bodies also reach the audit trail; the journal is always written.

*Why:* a model reply is a nondeterministic event to record, never to
regenerate, and a crash cannot tell "never sent" from "sent, outcome
unknown" unless the intent was written first. The design study's survey of
workflow engines is the evidence
([agentic-workflow-engines-survey](../adr-background-work/agentic-workflow-engines-survey.md)).

### 5. Long conversations are compacted, cheapest step first

When a run outgrows the model's context, a plan picks the cheapest step
that fits: **elide** old tool results to stubs, which a `recall` tool can
bring back; **summarise** the span before a cut with a rolling summary
([ADR-0316](../adr/0316-compaction-as-a-journal-entry-plan-act-apply.md)).
The person's own words are never summarised. The compaction is itself a
journal entry, so a resumed run compacts the same way.

### 6. Everything is on one audit trail, and the record is tamper-evident

Model calls, kept messages, agent actions, grant events and fetches share
the same identifying components on the trail, so the question "which calls
made up this conversation, what did it read, who approved what" is a join
that holds by key ([ADR-0277](../adr/0277-one-audit-trail-for-model-calls-and-agent-work.md)).
Each journal entry carries a chain digest over its predecessor, and chain
heads are anchored in a store the organisation holds, so a tampered record
is detectable without trusting the person or the software
([ADR-0314](../adr/0314-tamper-evident-records-chain-digests-and-anchored-heads.md)).
Bodies that could be personal live in a vault and are committed to the
chain under a per-entry key, so erasure destroys the body and the key and
leaves the chain whole
([ADR-0320](../adr/0320-journal-bodies-in-a-vault-committed-to-the-chain-under-a-per-entry-key.md)).
The same chain, vault, marks and anchors are a profile any generated record
store can opt into
([ADR-0327](../adr/0327-evidence-as-a-record-store-profile-chain-vault-marks-seals-and-anchors-any-store-opts-into.md)).

### 7. Trust is a label, and the boundary is declared

The threat model names the principals and what is promised against each
adversary: the careless person, the injected agent, the remote provider,
the insider, the tamperer
([ADR-0307](../adr/0307-threat-model-for-an-interactive-single-person-cell.md)).
Three mechanisms carry it:

- **Labels.** Every journal entry carries an integrity level, a
  confidentiality tier in TLP terms, and two provenance marks. Labels join
  upward as entries are read and are lowered only by a journaled
  **endorsement** the person makes
  ([ADR-0311](../adr/0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md)).
  Content an agent fetched is marked as such; a model's earlier output
  returning through storage is recognised and marked.
- **Exits.** Every edge boxer mediates that leaves the cell is a **sink**
  with a clearance: the remote model route, HTTP egress, clipboard and
  export, peers. A guard refuses or holds for approval above the clearance;
  budgets bound how much may leave; canaries detect what slipped
  ([ADR-0312](../adr/0312-exits-sinks-from-the-cell-boundary-approval-tiers-and-exit-budgets.md),
  [ADR-0317](../adr/0317-clipboard-and-export-copy-by-reference-and-one-choke-point.md)).
  Screenshots are their own exit: a **Pixels** setting says which captures
  the model may see, and with whose consent
  ([ADR-0287](../adr/0287-chat-pixels-a-setting-for-showing-screenshots-to-the-model.md),
  [ADR-0281](../adr/0281-window-captures-through-one-policy-enforcement-point.md)).
- **The agent schema.** What an agent may read of the host's own tables is
  a generated set of relations under a scope it cannot override, with
  withheld data marked rather than silently absent
  ([ADR-0313](../adr/0313-agent-schema-generated-relations-an-agent-may-read-in-full.md)).

*Why marks and not a wall around the model:* the guards do not depend on
the model's cooperation. An injected model can ask for anything; what it
reaches is decided by labels and clearances it cannot change.

### 8. The harness is data

The turn loop is split at declared **seams**, each a composition shape with
a law: a chain runs in declared order, a guard combines decisions by
maximum so no element can lower another's, a slot holds one provider. Which
elements run, the prompt book, the tool registry and the constants form a
**profile** with a fingerprint, recorded on the run
([ADR-0310](../adr/0310-harness-profile-and-registry-composition-declared-per-seam.md),
[ADR-0335](../adr/0335-profiles-are-handed-to-the-host-a-mount-value-seam-and-a-selector-over-the-registry-in-force.md)).
The coordinator's own tools sit in a registry with a budget and an effect
class each, and a tool that holds state declares its footprint, epochs,
rebuild class, fork class and completion
([ADR-0315](../adr/0315-coordinator-tools-as-operation-specs-and-a-contract-for-stateful-tools.md)).

*Why:* a second compactor, route or tool mode must be selectable and
comparable without a code change in the loop, and a run must say which
composition produced it.

### 9. The person's attention is budgeted

Several conversations may run at once, and any of them may need the
person. Three records shape how that is presented:

- **Levels.** Every event has a level on one of two scales: *attention*
  (data, message, highlight) for events nothing waits on, *waiting* (your
  turn, critical) for events a turn is blocked on. The table is the host's,
  configurable within a floor
  ([ADR-0323](../adr/0323-attention-levels-one-table-two-scales-configurable-within-a-floor.md)).
- **A queue, not modals.** Decisions wait in a host queue as cards the
  person can acknowledge, shelve, answer quickly, or let escalate; a flood
  limit bounds how fast a stream may add to it
  ([ADR-0324](../adr/0324-the-persons-decisions-wait-in-a-host-queue-not-in-modals.md)).
- **Status.** Here, Busy or Away changes what opens on its own and starts
  an away log; it never changes what is decided
  ([ADR-0328](../adr/0328-the-persons-status-here-busy-away-changes-what-opens-never-what-is-decided.md)).

In the chat window the same levels mark the transcript, fold the trail,
and colour the **roster**: the Conversations tab lists the conversations
other chat windows hold with what their turn is doing, then runs that can
be resumed, then closed ones (ADR-0265, Updates). The precedents, from IRC
clients to alarm management, are in
[juggling-many-agent-streams](../adr-background-work/juggling-many-agent-streams.md).

### 10. Cost and runaway loops are the host's to bound

Three records are proposed, not accepted; read them as direction.
Model calls are metered per account and admitted, clamped, queued or
refused by rules, with the queue ordered by window state
([ADR-0300](../adr/0300-metered-model-calls-and-rules-a-moderator-writes.md)).
A built-in moderator turns live events into signals, such as a repeated
call or a context that only grows, and climbs an escalation ladder that
tells the person rather than asking them
([ADR-0302](../adr/0302-a-built-in-moderator-loop-signals-and-an-escalation-ladder.md)).
An unattended mode, compiled in by a build tag, lets the host decide grant
requests within a ceiling the person preset
([ADR-0298](../adr/0298-an-unattended-mode-the-host-decides-within-preset-bounds.md)).

### 11. A conversation has a workspace

Beside the transcript, a conversation may hold an **artefact**: one
markdown document the model edits with tools, under the ceiling, with a
lint and a diff the person reads
([ADR-0282](../adr/0282-chat-artefact-one-markdown-document-per-conversation.md)).
Screenshots the model collects sit beside it, sealed on disk under a budget
([ADR-0284](../adr/0284-chat-artefact-screenshots-sealed-under-a-budget.md)).
The artefact can be exported to the snapshot store and handed to mdedit
([ADR-0342](../adr/0342-chat-artefact-export-to-lading-and-the-handover-to-mdedit-by-snapshot.md),
proposed). An Analytics tab shows the agent surface and what of it was
granted and used ([ADR-0283](../adr/0283-chat-analytics-the-agent-surface-and-what-was-granted-and-used.md)),
and past conversations are read in play over SQL
([ADR-0239](../adr/0239-play-chat-panel-and-chatview-widget.md),
[ADR-0264](../adr/0264-retained-model-conversations-on-facts.md)).

### 12. Windows, instances and lifetimes

The chat app is multi-instance: each window is one conversation, one run
and at most one turn at a time, and the person opens as many as they like.
The window is the presentation and the audit key; the run is the security
unit; the task is the authority unit. A grant is bound to the window
instance that asked for it and dies when the instance drains its lifetime
scope ([ADR-0322](../adr/0322-app-instances-drain-a-lifetime-scope-between-unmount-and-unload.md)).
Identifiers for runs and entries come from tagged id streams leased from
the one server
([ADR-0319](../adr/0319-tagged-id-streams-leased-through-one-row-on-the-one-clickhouse-server.md)).

## What is deliberately not there

The design study lists these as sketched, not designed; each needs its own
record before code
([agent-harness §2.7, §6](../adr-background-work/agent-harness.md)):

- **Delegation**: a conversation that spawns child conversations and gets
  results back. The journal reserves a parent column and the trail a
  delegation component; nothing consumes them.
- **Fan-out and selection**: several samples of one request, verified and
  chosen by a rule.
- **Parallel tool calls** within one turn. The tool-mode seam has one
  element, sequential; tools already declare whether they are parallel-safe.
- **Cross-run memory**: a model reading another conversation's journal.
- **Window-less or scheduled runs**: a conversation with no window to show
  it (ADR-0308 §SD11).

## How it is exercised

The headless host runs **scenes**: a scripted model answers from a file,
the driver clicks and reads the accessibility tree, and the trace asserts
what the person would see. The chat's scenes are under
[apps/chat/scenes](../../apps/chat/scenes); the step vocabulary is in the
[imzero2-drive skill](../skills/imzero2-drive/SKILL.md). A coordinator
driven by hand, [apps/agentconsole](../../apps/agentconsole), exercises an
app's catalog and the dispatcher without a model. Settings arrive through
the `BOXER_CHAT_*`, `BOXER_AGENT_*`, `BOXER_LLM_*` and `BOXER_ATTENTION_*`
families in [env-vars](../env-vars.md).

## Reading order

| If you want to… | Start at |
| --- | --- |
| add an operation to an app | ADR-0269 §SD1–SD2, then an existing catalog such as play's ([ADR-0270](../adr/0270-play-operations-catalog-and-agent-limits.md)) |
| change what the model is told | ADR-0308, ADR-0309, then the prompt book under `apps/chat/prompts` |
| add a coordinator tool | ADR-0315, then the tool registry in `apps/chat` |
| understand a refusal | ADR-0269 §SD5–SD6, ADR-0280, ADR-0312 |
| read a conversation back | ADR-0308 §SD10, ADR-0313 |
| judge a security claim | ADR-0307, then ADR-0311 and ADR-0312 |
| extend to several cooperating agents | this page's "What is deliberately not there", then agent-harness §2.4 and §6 |
