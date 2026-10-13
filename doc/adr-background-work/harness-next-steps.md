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

Last revised 2026-10-13. The impediments and their mitigations are in
[plan002](../../plan002.md) (store and bodies), ADR-0319 (ids), ADR-0320
(vault), ADR-0322 (lifetime scope), and the two problem notes beside this
file. Everything below is open.

## Decisions waiting on the owner

| Item | Decides | Blocks |
| --- | --- | --- |
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
| A job's terminal state reaches the attention service: a notice source that is an owning app and a job id, the worker reporting at settle time, the chrome's highlight opening the watchbill window | ADR-0323 §SD6, §SD7 (accepted 2026-10-12) | not started |
| A refused or failed compaction, and compaction going off, as a transcript step | `apps/chat/chat_compact.go`, a step kind in `chat_steps.go` and its renderer | the renderer is in the dock work another session holds |
| The context meter shows the re-projected size right after a compaction instead of one answer later | `apps/chat/chat_render.go` `contextFill` | same file |
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
