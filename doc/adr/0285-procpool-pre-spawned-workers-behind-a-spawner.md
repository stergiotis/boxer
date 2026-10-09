---
type: adr
status: accepted
date: 2026-10-05
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-07
---

# ADR-0285: procpool — pre-spawned workers behind a spawner

## Context

[ADR-0028](./0028-chlocal-low-latency-sql-cap.md) §SD3 keeps warm
`clickhouse-local` processes so an interactive query does not pay a process
start. The pool half of that package — refill to `MinIdle`, the
`MaxConcurrent` ceiling, bounded parallel spawns, the watchdog, a `Stop` that
settles its race with a concurrent acquire — carries no ClickHouse knowledge,
and several review rounds went into it. Its worker half does: it starts
`clickhouse local` with fixed flags and accepts exactly one SQL text.

A second engine wants the same thing. A consuming repository runs a computer
algebra kernel inside a rootless gVisor sandbox (2026-10-05, one handheld
machine): the kernel takes ~7 s to become ready under the sandbox against
~2 s natively, then answers in about a millisecond — the shape ADR-0028's M0
spike found for ClickHouse, with a start cost a thousand times larger. Three
properties of that engine do not fit the ClickHouse pool as it stood:

- **Ready is later than started.** The kernel checks its licence and loads for
  seconds after `exec`, and can fail in that window. ClickHouse is ready when
  its process is (ADR-0028, M0 spike), so the pool treated a started process as
  a servable one.
- **A slot is a scarce resource.** Its licence allows two concurrent kernels,
  and the sandboxes cannot see each other to count — three ran at once in the
  same measurement. The pool's ceiling is then the only enforcement, and it has
  to count a slot as taken until the sandbox is deleted, not until a signal
  was sent.
- **Zero spares is a real setting.** An idle kernel holds a seat. `MinIdle = 0`
  was unreachable, because the zero value meant "use the default of 2".

## Decision

We will move the pool mechanics into
[`github.com/stergiotis/boxer/public/keelson/runtime/procpool`](../../public/keelson/runtime/procpool),
generic over the worker type, and keep
[`github.com/stergiotis/boxer/public/keelson/data/chlocalpool`](../../public/keelson/data/chlocalpool)
as a ClickHouse spawner over it with its exported API unchanged.

### SD1 — The seam is a spawner and a release callback

`procpool.SpawnerI[W]` has one method, `Spawn(ctx, release)`, which returns a
worker once it can serve. The worker's `Close` frees what it holds and then
calls `release` exactly once. The pool counts a slot as free only on that
call, so `MaxConcurrent` bounds whatever the worker stands for — a process, a
sandbox, a licence seat — through its whole teardown. A worker whose process
dies on its own calls `release` too; that is how a dead idle worker leaves the
idle list and is never handed out.

The pool keys its records on its own slot, not on `W`, so a release that
arrives before `Spawn` returns still finds its record (the spawn then fails
with "worker exited before it was handed over") and `W` need not be
comparable.

### SD2 — Readiness belongs to the spawner, under the pool's deadline

`Spawn` returns when the worker is ready, by whatever handshake the engine
offers. The pool applies `Config.SpawnTimeout` to the context it passes. A
failed spawn is returned to the `Acquire` caller that asked for it; the
refill goroutine does not retry it until the next nudge (an acquire or a
release), so an engine that cannot start costs one attempt per demand rather
than a loop.

### SD3 — Every zero value in `procpool.Config` means zero

`MinIdle = 0` keeps no spares; `SpawnTimeout = 0` adds no deadline;
`WatchdogMaxLifetime = 0` lets a caller hold a worker for as long as it likes,
which a conversation over one worker needs. Defaults are the consumer's:
`chlocalpool.Config` keeps filling ADR-0028 §SD3's.

### SD4 — What happens between acquire and close is the caller's

The pool hands out `W` and takes it back through `Close`. One request then
exit (ADR-0028's O3) and many round trips over one worker are both uses of the
same pool; reuse across *callers* is not offered, so ADR-0028's case against
O2 is untouched.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `procpool` exported API (`Pool`, `New`, `Config`, `Stats`, `WorkerI`, `SpawnerI`, `ReleaseFunc`) | added | — |
| `chlocalpool` exported API | unchanged | `chlocalbroker`, `launchlimit` and the `play` / `sqlapplet` tests compile as before |
| `chlocalpool` log message text | the pool's two log lines lose their `chlocalpool:` prefix | the watchdog test that matched the text |

## Alternatives

- **A second, copied pool in the consuming repository.** Rejected: the stop
  and acquire races the ClickHouse pool fixed would be fixed twice, and
  anything of general use belongs in boxer.
- **The worker type as an interface value instead of a type parameter.**
  Rejected: every `Acquire` would hand back an interface the caller asserts to
  its concrete type; with two instantiations the parameter is justified.
- **Learning of a worker's death by waiting on a `Done` channel per worker.**
  Rejected: a ClickHouse worker's process may only be waited on after its
  stdout is drained, so `Done` fires when the caller calls `Wait`, not when
  the process exits; the pool would also need a goroutine per worker. A
  callback lets each worker report at the moment it knows.
- **Retry with backoff in the refill loop.** Deferred: no consumer needs it —
  a failed spawn is returned to the caller who asked — and a backoff policy is
  a second set of knobs to tune.

## Consequences

### Positive

- The pool's behaviour is tested against a fake spawner with `testing/synctest`,
  without a ClickHouse binary and without sleep-calibrated timing.
- An engine with a slow, fallible start or a counted licence gets warm spares,
  a ceiling that holds through teardown, and dead-spare eviction.

### Negative

- The idle list is a slice under the pool's mutex with a broadcast channel for
  waiters, rather than a buffered channel, so dead spares can be removed;
  acquire and stop now settle under one lock instead of a channel select plus
  a claim step.

### Neutral

- `chlocalpool` passes `4 × SpawnTimeout` as the pool's spawn deadline and
  keeps bounding `Start` by `SpawnTimeout` itself, which is the arithmetic it
  had before.

## Migration — Tier 1

- **Breaks.** Nothing compiles differently. A log consumer matching
  `chlocalpool: watchdog reaping forgotten worker` or `chlocalpool: refill
  spawn failed` must drop the prefix.
- **Path.** None.
- **Regeneration.** None.
- **Old shape.** The pool internals of `chlocalpool` are removed.

## Verification plan — Tier 1

- **Lane.** Default `go test` with `-race`: the `procpool` package tests
  (fake spawner, synctest) and the unchanged `chlocalpool` / `chlocalbroker`
  suites, which run against a real `clickhouse` where one resolves and skip
  otherwise.
- **What would fail.** `TestPool_SlotIsBusyUntilReleased` if a slot is freed
  before release; `TestPool_DeadSpareIsNotHandedOut` if a dead spare stays
  idle; `TestPool_ConcurrencyNeverExceedsMax` if the ceiling is crossed under
  random acquire, close and death; the `chlocalpool` stop and watchdog tests if
  the wrapper changes behaviour.
- **Gap.** A real slow-starting engine is exercised only in the consuming
  repository.

## Status

Accepted 2026-10-07. SD1–SD4 are built: `procpool` with the tests the
verification plan names, and `chlocalpool` as a ClickHouse spawner over it
with its exported API unchanged.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0028](./0028-chlocal-low-latency-sql-cap.md) — the ClickHouse pool this generalises; §SD3 and the O2 rejection.
- [ADR-0118](./0118-extbin-external-process-chokepoint.md) — spawners resolve their binaries through `extbin`.
