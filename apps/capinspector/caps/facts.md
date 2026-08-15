---
type: explanation
audience: contributors
status: draft
---

> **Status: draft — pre-human-review.** Rendered by the capability
> inspector for the Facts cap. Refine as the storage story
> consolidates.

# Audit + facts backend

The durable home of grants, audit records, app-lifecycle rows,
heartbeats, workingsets and column-width overrides. Backed by
ClickHouse when reachable; falls back to `InMemoryFactsStore` when CH
is down. App persist state is not here: it lives on its own generated
store, `boxer.persiststate` (ADR-0105 D3a) — see the persist
capability.

`chstore.NewWithFallback` returns the strongest live backend; the
status line's `facts:ch` vs `facts:mem` segment is the truthful
readout of which one is currently active.

**Read paths today** (all on `chstore.Store`, live-CH only):

- `LookupRunStart(ctx, runId)` — fetch the runtime-start row for a run.
- `LifecyclesByRun(ctx, filter)` — the children of one run, chronologically.
- `LastHeartbeatForRun(ctx, runId)` — most-recent liveness tick.
- `RecentLogs(ctx, filter)` — newest-first tail with app / level /
  time-range narrowing.

The in-memory backend exposes `Runs()`, `Lifecycles()`, `Heartbeats()`,
and friends — full snapshots for tests; production paths use the CH
helpers above.

The **Storage schema** section below renders that table's leeway
schema: the plain identity columns, the tagged-value sections under the
`data` streaming group, and each column's canonical type, encoding
hints and membership spec. It is the authored mapping, so it describes
what durability looks like rather than what the current backend is
doing — the in-memory store keeps the same rows in process memory and
never materialises the table.

**Where to look in the code:**

- `runtime/factsstore/factsstore.go` — `FactsStoreI` interface + in-mem impl
- `runtime/factsstore/chstore/chstore.go` — live-CH backend
- `runtime/factsstore/chstore/runsessions.go` — run-anchored read helpers
- `runtime/factsstore/chstore/recentlogs.go` — log tail
- `runtime/factsschema/factsschema.go` — leeway schema definition

**ADR reference:** §SD6 (leeway-shaped boxer.facts).
