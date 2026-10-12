---
type: explanation
audience: whoever picks up the agent harness after the impediment work of 2026-10-11 — a running list of what is left, kept current as items land
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** A worklist, not a decision. Each
> item names the document that decides it; an item leaves this list when
> its commit lands, and the list is edited in place.

# Harness: what is left after the impediment work

Last revised 2026-10-12. The impediments and their mitigations are in
[plan002](../../plan002.md) (store and bodies), ADR-0319 (ids), ADR-0320
(vault), ADR-0322 (lifetime scope), and the two problem notes beside this
file. Everything below is open.

## Decisions waiting on the owner

| Item | Decides | Blocks |
| --- | --- | --- |
| Accept or amend ADR-0322 (lifetime scope), 0323 (attention levels), 0324 (decision queue), 0326 (tuple parameters) | the owner | doclint flags accepted docs that link to them |
| Accept or amend ADR-0327 (the evidence profile: chain, vault, marks, seals, anchors by scope, the audit event), which moved the trail onto the journal's laws and the journal onto the generator's profile; M1–M6 have landed | the owner | the open items of its §SD11 |
| The env registry seam: boxer is handed the cell declaration and the profile rather than reading them | an ADR to write, from `env-registry-scope-problem.md` | the chat's second `cell.FromEnv` read; integrators |
| The httpegress wall: run label and tier on a fetch, `cell.Decide` at the service, approvals, canaries, trail columns | ADR-0312 §SD4 deferral; owner's own session | laws L14 and L33 over fetches |

## Code to migrate

| Item | Where | Why it waits |
| --- | --- | --- |
| `journalCtx` (`context.WithoutCancel`) in the approval, endorsement and conversation paths should write under the coordinator's lifetime scope tail, and the helper go | `apps/chat/chat_approval.go`, `chat_endorse.go`, `chat_conversation.go`, `chat_journal.go` | the chat files were in another session's hands when the scope landed |
| `bgjob` derives from `context.Background()`; it should derive from the scope's work context so an action is retired with its scope | `public/keelson/runtime/bgjob/bgjob.go` | same |
| `afterAction` and `deleting` in the chat stand in for a completion hook on the render thread, which ADR-0322 did not add | `apps/chat/chat_actions.go`; `host-lifecycle-hooks-problem.md` | needs a decision on the hook |
| The per-window writer lease: the lease is per host process, so a resume in one window is not ordered against a close draining in another; the service's checks keep the record whole | ADR-0322's deferral; ADR-0319 §SD7 | a lease per window instance |
| `hostboot` provisions the id lease table once itself and once through `agentrunstore.ProvisionIn`; the first call is redundant | `public/keelson/runtime/hostboot/hostboot.go` | harmless; fold when the function is next touched |
| Cross-cell receipts for erasure; the read event on the vault seam | ADR-0320 §SD7, ADR-0327 §SD11 | the audit event and the forwarder exist (ADR-0327 M4a); a peer to send to does not |
| Attachments move to the vault if they ever carry more than references | ADR-0320 §SD8 | no consumer needs it |

## Found by the verification sweep of 2026-10-12

Not blocked, each a few lines, in files the session that wrote them may
still hold:

- codelint CS008: type aliases re-exporting the evidence package's types
  in `trail/auditevent.go` (`AuditEvent`, `ForwarderI`) and
  `headstore/store.go` (`Receipt`, `Cadence`); the house rule wants named
  types or direct imports.
- codelint CS013 in `trail/forward.go`: an `eh.Errorf` format carrying a
  count; the count belongs on `eb.Build().Int(…)`.
- gofmt drift in `llm/introspect.go` and `watchbillstore/claim.go`.
- doclint DL010 on ADR-0298, missing `## Alternatives` and `## Status`;
  pre-existing since 2026-10-09, not from this work.
- plan002's Part B table and ADR-0320's text still describe the vault as
  the journal's own `SqlBodyStore`; since ADR-0327 M3 the profile emits
  it (`<Store>Vault`, `<Store>Vaulted`) and `agentrun` keeps only the
  writer's view. ADR-0320 has its dated Update; plan002 has a note.

## Done 2026-10-11 and 2026-10-12 (for the record, so the list reads as a delta)

Part A and Part B of plan002; the vault and per-entry erasure; tagged
ids and the writer lease; the lifetime scope and the drain step; the
`headstore` rename; the small cleanups (the reserved `keyuse` kind, the
comma rule on marks, the unused JSON helper, one provisioning call);
ADR-0327's six milestones, the attention levels of ADR-0323 on the bus and
in the chat, and the conversations roster of ADR-0324's background.
