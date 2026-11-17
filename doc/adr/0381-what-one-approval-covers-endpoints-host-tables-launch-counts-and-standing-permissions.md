---
type: adr
status: proposed
date: 2026-11-18
---

# ADR-0381: What one approval covers — a window's endpoint, host tables by class, launch counts and standing permissions

## Context

A person who works with the chat's model is asked to decide often. Many
decisions repeat one another: the same app opened in act mode, the same
server reached, conversation after conversation. A grant
([ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
§SD6) belongs to one task. A task belongs to one coordinator window and
ends when the window closes. Within a task, approval is granted along four
narrow lines:

- per window instance;
- per launch count, with the chat defaulting each app to one window
  (`openArg`);
- per destination string, one `keelson:<table>` at a time;
- per publish name, unless the model asks for a prefix.

**Measurements, 2026-11-18.** The first come from the audit trail's grant
views (`boxer.dm_trail_llm_agent_grants`) on one maintainer's host. Only
conversations whose model calls name a real model are counted; scenes use
the scripted one.

- The approvals that recur across conversations are three:
  - a `keelson:<table>` destination;
  - opening play;
  - the host's server endpoint.

  Each was approved anew in most conversations that needed it.
- Of the widenings in those conversations:
  - most added only destinations, and most of the rest asked to open more
    windows;
  - none came from a call the grant did not cover.
- Nearly every task the person approved ended because the coordinator
  window closed.
- An A/B run of 2026-11-18 measured the fix of 2026-11-17 that names the
  host's server to the plain chat's model before its first request. The
  arms were `157efa3d1` without the fix and `2c7d57472` with it. Each arm
  had four prompts, two runs each, against `deepseek-flash`, with a driver
  that approved every card.
  - On the three prompts that read the server, each of the six runs needed
    two approvals without the fix and one with it.
  - On a prompt that reads `keelson('apps')`, three of the four runs needed
    two. The model asks for the table, then for the server so that `cite`
    can check the figures drawn from it, or the other way round.
  - Eight conversations an arm say which way the fix moves the count, not
    by how much in general.

The hint closes the widening it was aimed at. What remains is how narrowly
one approval reaches. This record proposes four ways to widen it, each
bounded by the ceiling the person sets
([ADR-0280](./0280-a-ceiling-on-what-a-chats-model-may-do-scored-on-a-ladder.md)),
and keeps two rules as they are:

- a change outside the app is confirmed every time;
- an exit approval is asked every time.

The threat model is
[ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md).
Each section below says what it changes there.

## Decision (proposed)

### SD1 — A shared window brings the endpoint it is bound to

An app declares, through its operations catalog, the destinations a window
is bound to: play's configured endpoint, `clickhouse:<host>`. Sharing such a
window grants them with it, when two things hold:

- its mode can reach them: act or suggest, at an effect of run or above;
- they sit under the ceiling's reach.

The access card names them as consequences of sharing the window, e.g.
"play's runs reach the ClickHouse server at localhost:8123". They are
not listed as separate destinations. A window retargeted to another
endpoint does not carry the new endpoint into the grant. The next run there
asks, as today.

*Threat model.* The person already sees the endpoint in play's toolbar and
chose to share the window that runs against it. A model that steers play to
a different endpoint gains nothing by this rule.

### SD2 — This host's catalog tables by class

A keelson table declares a class beside its provider (`introspect.Registry`):

- **catalog**: apps, tables, columns, windows, desktop, help and the like;
- **sensitive**: env, logs, journals and anything a deployment marks so.

Sealed datasets stay as [ADR-0145](./0145-sealed-app-data.md) has them.

A task whose ceiling reaches *This host* reads every catalog table without
naming each. A sensitive table is still named in `request_access` and shown
on the card. A table registered without a class is sensitive.

*Threat model.* Catalog tables describe the host, not the person's data.
`keelson('env')` is the reason the class defaults to sensitive.

### SD3 — A launch grant opens more than one window

The chat asks for an app's windows with a count of three by default, and
the card says "open up to three windows of play". The host's rule is
unchanged: a launch past the count is a widening. Each window still joins
the task in the mode the grant gave the app.

### SD4 — Standing permissions, approved once and revocable

The card offers **Approve and remember** beside Approve. It keeps the
approved terms as a standing rule of the coordinator app (the plain chat, or
one chatlet):

- apps it may open, with their modes and counts;
- destinations and destination classes;
- publish prefixes;
- the desktop mode.

The host persists the rule. A later request is approved in the person's
place only when every part of it is inside one rule and under the window's
ceiling. The approval is recorded as a grant event decided by
`standing:<rule id>`, the way unattended approvals are recorded
([ADR-0298](./0298-an-unattended-mode-the-host-decides-within-preset-bounds.md)). A request that asks for anything
outside the rules reaches the person whole, with the parts already covered
shown as such.

What no rule covers:

- the confirmation of a change outside the app;
- an exit approval;
- a widening of a running task's mode, budget or deadline;
- a window the person did not open, other than through a launch grant.

A host panel lists the rules with when each was last used and a Revoke
button. A rule unused for 30 days lapses.

*Threat model.* A prompt-injected model can do, without asking, what the
person approved once for that coordinator. That is the cost. The bound is
the scope the person approved, under a ceiling the person sets. Taint
([ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
§SD7) does not switch rules off, because reading the window list taints
nearly every conversation. A rule names apps and destinations, not data.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| an app's operations catalog | a query of the destinations a window is bound to | play declares its endpoint; the access card |
| `introspect.Registry` | a class per provider, sensitive by default | `keelson('tables')` gains the class; play's agent limits |
| `runtime.agent` grant terms | standing rules: stored, matched, revoked; grant events decided by `standing:<id>` | host chrome's panel; the trail's grant view |
| the chat's `request_access` | a launch count of three by default | the coordinator prompt; preservation goldens |

## Alternatives

- **Keep the task when the window closes; resume it with the
  conversation.** Not taken alone. It saves the decisions of a resumed
  conversation, but the measurements show new conversations, not resumed
  ones, repeating the same approvals.
- **A grant per app rather than per window.** Not taken. A window the
  person opened of the same app would join a task without being shared.
  SD3 covers windows the task opens.
- **Pre-approve everything under the ceiling.** Not taken. The ceiling
  bounds what may be asked for, and the card is where the person sees what
  is asked. Merging the two removes the second look ADR-0280 kept.
- **Lower the taint on the window list instead of SD4.** A separate
  decision. It would not reduce the count of decisions.

## Consequences

- Fewer cards for what the person approved before, and fewer things an
  injected model must ask for. SD4 is the larger share of both.
- A standing rule is state the person must remember they gave. The panel
  and the lapse are the remedy, and a weak one.
- Tables must be classed when registered. A new table is sensitive until
  someone says otherwise, so the safe default costs a decision.

## Verification plan — Tier 1

- **Lane.** Default `go test`:
  - a request inside a rule is approved by it and recorded;
  - a request with one part outside is not;
  - a confirmation is never covered;
  - a catalog table needs no destination and a sensitive one does;
  - a shared play window's endpoint is granted only at run or above.
- **Trial.** The A/B drive above, rerun with each section on, reported as
  approvals per conversation per prompt.

## Status

Proposed 2026-11-18. Nothing here is implemented.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) §SD5–§SD7 — grants, the person's decisions, taint.
- [ADR-0280](./0280-a-ceiling-on-what-a-chats-model-may-do-scored-on-a-ladder.md) — the ceiling.
- [ADR-0298](./0298-an-unattended-mode-the-host-decides-within-preset-bounds.md) — decisions made in the person's place, and how they are recorded.
- [ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md) — the threat model.
- [ADR-0324](./0324-the-persons-decisions-wait-in-a-host-queue-not-in-modals.md) — the queue the cards wait in.
- [ADR-0364](./0364-chatlets-the-chat-specialised-by-a-committed-markdown-document.md) — the server named before the first request, for chatlets first.
- [ADR-0379](./0379-the-chat-window-one-place-for-a-decision-ids-unique-per-conversation-and-what-the-person-reads.md) — the chat window's review this follows.
