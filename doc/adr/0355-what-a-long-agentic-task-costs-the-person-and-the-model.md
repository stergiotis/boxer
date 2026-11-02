---
type: adr
status: proposed
date: 2026-11-02
reversibility: local-edit
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; implemented, not yet reviewed.

# ADR-0355: What a long agentic task costs the person and the model — reads that warn, requests that open, a history that sheds

## Context

A downstream team recorded one chat-driven task four times: the model opens
play and other windows, runs queries, publishes datasets, captures windows,
writes a report and judges its audit trail. Two recordings stopped short. The
causes were not in the model: a read that silently cost the export
([ADR-0288](./0288-adhoc-bundles-datasets-published-with-the-applet-that-reads-them.md)), commands no agent could ever call,
schema refusals over arguments the window already held, a citation handle
the model cannot see, and approvals behind two clicks and a timer. Compaction
([ADR-0316](./0316-compaction-as-a-journal-entry-plan-act-apply.md)) never ran: it cuts at turn starts at a share
of the route's context, and this task was long turns under a large window.

## Decision

We will fix these at the layer each belongs to, without moving authority: the
ceiling, the confirmation of consequential calls and the locality wall stay
as they are. What changes is what the model is told before it acts, and what
the person has to do to answer.

### SD1 — A read of a sealed window is held once, with its cost

A query's outcome carries its window's label, so what confines a conversation
is the window, not the operation; a `Sealed` flag on operations would claim
less than what happens. The coordinator holds the first query or window tree
of a window the host lists as confined, answering with what the read costs and
that a capture does not. Repeating the call reads it. The export refusal names
the call that confined the conversation, from the journal. A confined
conversation still cannot export: a text-only export is a route for what it
read to reach the store.

### SD2 — A write cannot wait on a revision nobody can read

A query reports the revisions of what it declares in `Reads`, and a command
is refused until its task read those it writes. A catalog whose queries
declare no `Reads` for a resource makes its commands uncallable by any agent.
The engine therefore checks a base revision only for resources some
agent-visible query reports, and a read-first conflict names that query.
`OperationSpec.AnyRevision` marks a command whose effect does not depend on
what it writes staying as read, such as a refresh over a resource a live read
moves every few seconds. The person's own edit still wins either way.

### SD3 — A query's arguments default to what its window shows

A required argument the window already holds costs a round and invites drift.
Arguments whose description says they may be empty are optional in the schema.
The trail viewer's queries take the window's case and range when left out, and
its filter when the case is left out too, so a query with no arguments reads
what the window shows and one naming its case stays independent of the view,
as [ADR-0350](./0350-a-trail-viewer-one-system-app-over-the-marts-a-case-a-window-a-verdict-and-review-marks.md)
§SD8 wanted.

### SD4 — A grant is asked for once

`request_access` already takes destinations; the model did not plan them. The
prompt and the tool say each request is a dialog, and a refusal's next step
says to add what the plan still needs. A grant of a bundle now covers a dataset
of it a window has not bound, which took a second dialog. A bundle's name reads
its dataset when it holds one, and one of several names them.

### SD5 — More rounds go through the same dialog

A grant request may ask for more rounds of the running turn; the card says so,
the coordinator keeps the count, and the host's most still bounds it.
`BOXER_CHAT_ALLOW` seeds a new window's ceiling for a host that runs such tasks.
It is a starting value under ADR-0310 §SD7, outside the profile, and carries no
authority beyond what Settings offers.

### SD6 — A request opens where the person is working

A grant request, widening or confirmation opens at once when its window is the
active one or the one the person last gave input to. A window the task just
opened takes the focus without the person moving, so the active window alone
would miss the common case. A request from another window waits, so an app
cannot pop a card from behind, and the person's status holds it back as it does
an escalation ([ADR-0324](./0324-the-persons-decisions-wait-in-a-host-queue-not-in-modals.md),
[ADR-0328](./0328-the-persons-status-here-busy-away-changes-what-opens-never-what-is-decided.md)). `BOXER_ATTENTION_OPEN_OWN` turns it off, which
the scenes about the queue itself do.

### SD7 — The coordinator keeps a slot, and a task closes what it opened

While a task runs, its coordinator's window holds the slot behind the main one
under a tiled layout ([ADR-0346](./0346-imzero2-tiled-desktop-dynamic-layouts-over-floating-windows.md)), so a new window does
not push it down the stack. The newest window stays the main one, because that
is the window being worked in. Monocle gives the active window the whole frame
by definition and is not changed. `close_window` closes a window the task
opened; a window the person opened stays theirs.

### SD8 — A consent is one field

The action row's `consent` carried a grant destination or nothing, so a call the
person confirmed looked unconsented and a policy rule needed three joins. It now
also carries `dialog:person` once the person confirmed, so "was this consented"
is one column and the mart's shape does not change.

### SD9 — A citation rests on what the model can read

A tool call's provider id is not something every model sees, and the recorded
model offered result numbers and call ordinals instead. `rests_on` takes the
`result_id` a query answered with, and a refusal lists what can be cited.

### SD10 — What a later read replaces leaves the projection

A read of current state is out of date once the same read answers again, and a
window tree once the step that used it is past. A journal edit replaces each
with a stub `recall` brings back; the journal keeps the body. Results are not
in the list: an earlier result's figures are not out of date. Edits rewrite a
request's middle, which costs a provider's cached prefix, so they are written
in batches. The reads and the batch size are profile data (ADR-0310 §SD7).

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `app.OperationSpec` | added `AnyRevision` | nothing; additive |
| `opengine` | a write to a resource no query reports is not refused | no test relied on the refusal |
| chat's tools and prompt book | `close_window`, `rests_on`'s grammar, `rounds` | the prompt fingerprint: a conversation started before resumes read-only |
| chat's profile | `SupersededReads`, `SupersedeBatchBytes`, the `supersede` seam | the profile fingerprint |
| `agent.HostI` | added `OpsClose` | every host implementation |
| Action rows | `consent` also holds `dialog:person` | rules reading it as "a grant destination" |
| Env | `BOXER_CHAT_ALLOW`, `BOXER_ATTENTION_OPEN_OWN` | `doc/env-vars.md` |

## Alternatives

- **A `Sealed` flag on operations.** Rejected: the label is the window's, so the
  flag would be true of every query of a confined window and false otherwise.
- **Exporting a confined conversation's text.** Rejected: the text is what the
  model wrote after reading sealed rows.
- **Task templates a person approves whole.** Deferred: planned destinations
  in one request cover most of it, and a template is a new authority to design.
- **A `capture_cell` rendering one result off screen.** Not taken: a capture is
  of the window as drawn, and drawing the cell alone gets there without a second
  render path.
- **Superseding on every request.** Rejected: each edit costs the cached prefix
  from that point on, for a small saving.

## Consequences

### Positive

- The commands of several catalogs that no agent could call now work.

### Negative

- The first read of a sealed window costs a round.
- A card can open while the person reads something else in the same window.
- The new tool and prompt text add about 1.6 KiB to the stable head of every
  conversation with Apps on.

### Neutral

- Conversations started under the old prompt resume read-only, as any prompt
  change does.

## Verification plan — Tier 1

- **Lane.** Default `go test`: the engine's revision rules; the trail viewer's
  defaults and that every resource a command writes is reported; the bundle
  grant and the bundle-name read; the close verb's refusal; the pinned slot; the
  own-request cards; citation handles; the sealed hold; the batch rule.
- **Scene.** A scripted chat on a tiled desktop: the card opens unasked with the
  rounds it asks for, and the chat stays in view beside the new window.

## Status

Proposed.

## References

- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
- [ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md)
- [ADR-0340](./0340-cited-replies-assertions-the-window-checks-and-verdicts-in-the-journal.md)
