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

Last revised 2026-10-25. The impediments and their mitigations are in
[plan002](../../plan002.md) (store and bodies), ADR-0319 (ids), ADR-0320
(vault), ADR-0322 (lifetime scope), and the two problem notes beside this
file. Everything below is open.

## Decisions waiting on the owner

| Item | Decides | Blocks |
| --- | --- | --- |
| The profile seam: the host hands the chat the profile registry in force, and the selector is checked against it (the cell half landed 2026-10-11, ADR-0307 §SD1 Update) | ADR-0335 (accepted 2026-10-25), from `env-registry-scope-problem.md`; nothing built | an integrator with a second profile |
| The httpegress wall, what is left: canaries on fetch bodies, and whether a fetch above the clearance ever gets an approval path rather than a refusal | ADR-0312 §SD6, §SD7 (the tier, the trail column and L14/L33 over fetches landed with the Update of 2026-10-25) | law L10 over fetches |

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
| The record-store generator emits a qualified type for a component whose DTO lives in another package, so `trail.AuditEvent` stops being an alias (CS008) | `public/storage/recordstore/gen`, `trail/auditevent.go` | a generator change; the alias carries the reason meanwhile |

## Done 2026-10-11 and 2026-10-12 (for the record, so the list reads as a delta)

Part A and Part B of plan002; the vault and per-entry erasure; tagged
ids and the writer lease; the lifetime scope and the drain step; the
`headstore` rename; the small cleanups (the reserved `keyuse` kind, the
comma rule on marks, the unused JSON helper, one provisioning call);
ADR-0327's six milestones, the attention levels of ADR-0323 on the bus and
in the chat, and the conversations roster of ADR-0324's background; the
thrash check fitted to small contexts and compaction going off told in the
meter (ADR-0316 Update of 2026-10-12); ADR-0321 to 0324, 0326 and 0327
accepted; the verification sweep's lint and format findings fixed but for
the one alias generated code needs; the context probe reading llama.cpp's
loaded size from `/props` (2026-10-13).
