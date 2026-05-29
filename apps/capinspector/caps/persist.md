---
type: explanation
audience: contributors
status: draft
---

> **Status: draft — pre-human-review.** Rendered by the capability
> inspector for the runtime.persist.* cap. Refine when the
> facts-backed backend lands and `persist:facts` becomes a value of
> the status segment.

# runtime.persist.* — per-app key-value state

Apps that declare `PersistedKeys` in their Manifest get the
runtime-allocated host-injected cap
`runtime.persist.{ownAlias}.>` and a per-app
`persist.Client` threaded through `MountCtx.Storage()`.

Each `Storage.Set` / `.Get` / `.Delete` translates to a
`bus.Request("runtime.persist.{ownAlias}.{key}.{op}", payload)`. The
service is the bus subscriber on `runtime.persist.>`; it parses
`(alias, key, op)` and dispatches to a pluggable `StorageBackendI`.

**Backend today:** `persist.NewMemoryBackend()` — process-scoped,
does NOT survive restart. The status line's `persist:mem` segment
makes this explicit. A future facts-backed backend (writing to
`runtime.facts` via `WriteState`) will make state durable when
ClickHouse is reachable; until then `persist:mem` is the truthful
readout.

**Key constraint:** a single NATS token — no dots, no wildcards.
The service rejects dotted keys with "malformed subject"; use
camelCase or snake_case names like `editorFont` or `selected_tab`.

**Where to look in the code:**

- `runtime/persist/service.go` — bus subscriber + dispatcher
- `runtime/persist/client.go` — `StorageI` adapter the host hands apps
- `runtime/persist/memory.go` — in-memory backend
- `runtime/windowhost/windowhost.go` — host-side cap auto-injection
- `capdemo/` — first consumer; the scratchpad section exercises
  Set / Get / Delete

**ADR reference:** §SD3 (subject grammar) + §SD6 (storage layer).
