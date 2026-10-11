---
type: adr
status: proposed
date: 2026-10-11
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0318: Model inference is the harness's — an app exposes operations instead of holding an `llm` grant

## Context

[ADR-0254](./0254-model-inference-as-a-keelson-capability.md) made model
inference a declared capability any app could hold: an app declared
`llm.ClientCaps(reason)` and called the host's model over the bus. Two kinds
of holder followed. The agentic harness (`apps/chat`,
[ADR-0265](./0265-chat-app-over-retained-model-calls.md),
[ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md))
runs the loop, the approvals, the journal and the ceilings a model call is
held to. mdedit's transformations
([ADR-0216](./0216-mdedit-llm-transformations.md)) made their own one-shot
calls beside it, with none of that machinery: the call carried a purpose and
reached the audit trail, but the conversation, the grant and the person's
settings were the harness's and did not cover it.

Since then [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
gave apps a second way to take part in model work: declare a catalog of
commands and queries, and let the harness drive them under a task grant.
play takes part that way and holds no `llm` grant. With both paths open,
every app that wants a model feature has to choose, and the choice decides
whether the feature runs under the harness's controls or beside them.

## Decision

**SD1 — The harness is the one app that holds an `llm` client grant.** An
app that wants model work done on its content declares operations
(ADR-0269) that read the content and accept the result, and the harness does
the model work. Host-side services that observe or govern model use (the
moderator, [ADR-0302](./0302-a-built-in-moderator-loop-signals-and-an-escalation-ladder.md)) are not apps and are
unaffected. This is a rule for now, not an enforced one: nothing refuses a
second holder, and a reviewer checking a new `llm.ClientCaps` against this
record is the guard.

**SD2 — mdedit drops its grant and its model surface, and declares three
operations.** `get_document` reads the source, its revision, and the
selection as byte offsets. `propose_edit` takes a span, the revision it was
computed against, a replacement and a title; it refuses a stale revision or
a span that cuts a character, and otherwise opens the review pane. Its
effect is a view change: nothing an agent does through the catalog changes
the buffer. `get_proposal` reports whether a proposal waits and what became
of the last one.

**SD3 — Preview-then-apply survives, fed by the proposal.** ADR-0216 §SD4's
pane, its Apply / Discard / Copy verdicts and its refusal to splice into a
buffer that moved are unchanged; the pane's header names who proposed the
change and the call's reason instead of the model, host and token counts,
which are now the harness's to show.

**SD4 — The transformation corpus moves to the harness.** The three prompt
documents become a book in `apps/chat` (`transforms`), offered by a picker
above the composer while Apps is on. Picking one fills the draft with the
request and a line naming what it works on and the two operations that read
and propose; the person sends it as any other message. The slugs stay; the
bodies change from system prompts to requests, and `temperature` /
`max-tokens` go, since the harness's profile governs the call.

## Alternatives

- **Keep mdedit's grant beside the harness.** Rejected: its calls run
  outside the harness's approvals, ceilings and journal, and every later app
  would face the same choice.
- **Enforce SD1** with a codelint rule over who calls `llm.ClientCaps`, a
  test over the registered manifests, or an allowlist in the `llm` service.
  Not taken for now: one app crosses the line today, and the rule is
  expected to move once it is clear which model features belong inside an
  app.
- **Let the harness splice directly** through a `replace_range` command
  under ADR-0269's document effect. Rejected: a rebind is invisible to the
  editor's undo (ADR-0178 M3), so an unreviewed change would be a
  destructive one — ADR-0216 §SD4's reason, unchanged.
- **Delete the corpus.** Rejected: the documents are the reviewed wording of
  three common requests, and the picker costs one combo box.

## Consequences

- mdedit holds no grant that sends text off the box; its manifest pins the
  four subjects it does hold.
- A transformation now needs a conversation with Apps on and a window shared
  with the task. That is more steps than mdedit's one-click Run, in exchange
  for the harness's controls covering the call.
- The model sees byte offsets and must quote them back. A model that
  miscounts gets a refusal naming the bounds, not a wrong splice.
- The proposal pane holds one proposal; a second supersedes the first, and
  `get_proposal` says so.
- `promptbook` holds and lists prompts and no longer runs them: `Run`,
  its `Result` and `FailureLine` had no caller left and are removed, and
  the completer interface moved into the chat, its one user. A document's
  `temperature` and `max-tokens` are still parsed and listed in
  `keelson('llm_prompts')`; applying them is the caller's.

## Status

Proposed (2026-10-11).

## References

- [ADR-0216](./0216-mdedit-llm-transformations.md) — superseded by this record.
- [ADR-0254](./0254-model-inference-as-a-keelson-capability.md) — the `llm` capability; §SD1's "any app" is narrowed here.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — the operations contract mdedit joins.
- [ADR-0178](./0178-mdedit-markdown-editor.md) — mdedit; M3's undo caveat.
