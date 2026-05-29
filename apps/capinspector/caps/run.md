---
type: explanation
audience: contributors
status: draft
---

> **Status: draft — pre-human-review.** Rendered by the capability
> inspector for the Run identity cap. Refine as runinfo / heartbeat
> evolves.

# Run identity

A 16-character nanoid minted by `runinfo.Init()` at process boot and
exported as `PEBBLE2_RUN_ID` for child processes (including the Rust
client).

Every audit row carries this id so a session's activity groups under
one runtime-start fact. The heartbeat ticker emits liveness rows at
30s cadence so a crashed process (no `stopped` row, no recent
heartbeat) is distinguishable from a clean shutdown.

**Why it matters:** correlating Go-side and Rust-side log lines, and
attributing each app-lifecycle row back to the process that opened
the window.

**Where to look in the code:**

- `runtime/runinfo/runinfo.go` — id minting + env-var export
- `runtime/heartbeat/heartbeat.go` — periodic liveness ticker
- `src/rust/src/runinfo.rs` — Rust-side env-var bridge
- `runtime/factsstore/chstore/runsessions.go` — `LookupRunStart` /
  `LifecyclesByRun` / `LastHeartbeatForRun` read helpers

**ADR reference:** §SD12 + 2026-05-12 runtime-run amendment.
