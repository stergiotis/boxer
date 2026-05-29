---
type: explanation
audience: contributors
status: draft
---

> **Status: draft — pre-human-review.** Rendered by the capability
> inspector for the Bus cap. Refine when M4 swaps inprocbus for
> NATS — the prose here will need to call out the transport flip.

# In-proc subject router

`inprocbus.Inst` is the universal substrate every other cap rides on.
Routes `Publish` / `Subscribe` / `Request` between per-app
`inprocbus.Client` instances minted from `Manifest.Caps`.

Each app gets its own client at window-open via
`bus.NewClient(manifest.Id, manifest.Caps)`. The client enforces the
declared `SubjectFilter` patterns per call: a `Request` to a subject
the app doesn't declare a `Pub` direction for is rejected before
hitting the wire.

`Request` is special-cased: it allocates an `_INBOX.<rand>` reply
subject and subscribes the calling client to it. The inbox subscribe
bypasses the cap check (apps don't need to declare `_INBOX.>` Sub) —
without that bypass every `Request` consumer would have to repeat the
boilerplate.

**The bus is the audit boundary.** Every audited `Request` lands one
`AuditRecord` on the bus's configured `AuditSinkI` — the carousel
wires `MultiSink{factsstore.AsAuditSink(facts), capinspector.Tally}`
so a single audit row populates both the durable runtime.facts table
and the live counters this inspector renders.

**M4 swap:** inprocbus is replaced by `nats.Conn` per app; minted
NKey/JWT pairs replace the Manifest.Caps filter set. The BusI surface
stays stable across the swap.

**Where to look in the code:**

- `runtime/inprocbus/bus.go` — process-wide router
- `runtime/inprocbus/client.go` — per-app permissioned client
- `runtime/inprocbus/permission.go` — `SubjectAllowed` predicate
- `runtime/audit/audit.go` — `AuditSinkI` + `MultiSink`

**ADR reference:** §SD3 + §SD5.
