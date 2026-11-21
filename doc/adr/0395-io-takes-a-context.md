---
type: adr
status: proposed
date: 2026-11-20
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0395: I/O takes a context: the store, persist, bus-request and trail interfaces

## Context

CODINGSTANDARDS makes `ctx` mandatory and first on I/O, but the runtime's
core interfaces had none: the facts store's writes, persist and app
storage, the state stores, `app.BusI.Request`, the trail recorder's verbs
and the generated stores' ctx-less `Begin` / `Delete` / `Ingest*`. Their
implementations rooted `context.Background()` and bounded each call with a
fixed timeout (`chstore.WriteTimeout`, `persist.CallTimeout`, a per-request
`d`). Two consequences went unseen by the compiler:

- Shutdown could not bound what it waited on. The grace and the window
  drain were shorter than a ctx-less store call could take, so a slow
  ClickHouse at shutdown ended in the forced exit, with the remaining
  windows' state and the log bridge's buffered rows lost.
- A generated store's ctx-less write ran under `context.Background()`, so
  an actor stamper that reads the call identity off the context found
  none, and the trail verbs other than `Event` wrote rows without one.

ADR-0386 added a `RequestCtx` on the concrete in-process client and left
the interface change, and the stopgap timeouts, to this decision.

## Decision

1. **Every I/O interface takes `ctx` first**, with no ctx-less twin:
   `factsstore.FactsStoreI`, `app.StorageI`, `persist.StorageBackendI`,
   the statestore and appstate store interfaces, `app.BusI.Request`, the
   `trail.Recorder` verbs, and every generated store's `Begin`, `Delete` and
   `Ingest<Kind>`. The `Ctx`-suffixed names go; the plain name is the
   ctx-first form.
2. **The ctx is the caller's.** A bus request's handlers see the
   requester's ctx, bounded by the wait, as `app.Msg.Context()`; it ends
   when the requester stops waiting. A row that must land whether or not
   the requester still waits (an audit or lifecycle row) is written under
   `context.WithoutCancel` of it. Where no call carries a ctx — an app's
   render loop, a host's background writer — the owner of that lifetime
   hands it in at construction: an app uses `app.ContextOf(mountCtx)`, the
   tail of its lifetime scope, which outlives the instance's leave so a save
   in `Unmount` still runs; a host uses its boot ctx.
3. **A request's wait is its ctx.** `app.BusI.Request(ctx, …)` waits until
   ctx's deadline, or the transport default when ctx has none.
   `RequestWithTimeout` leaves the interface; `app.RequestWithin(ctx, bus,
   d, …)` keeps a caller-named wait for requests a person answers, with
   `d <= 0` meaning the transport default.
4. **Shutdown runs under one ctx** cut from the grace (four fifths of it;
   the forced exit stays the backstop for code that ignores a ctx). The
   window and service reaps, the heartbeat's stop, every cleanup and the
   log bridge's final drain see it. Stop rows are written under it rather
   than under the drain's shorter ctx, so a drain that uses its budget does
   not also lose them.
5. **Fixed timeouts bound only a ctx without a deadline.**
   `chstore.WriteTimeout` and `persist.CallTimeout` stay for that case; a
   ctx with a deadline is not shortened. The persist backend takes its lock
   under ctx, so a call queued behind a slow one gives up with its own ctx.
6. **Codelint CS020** (error) reports an exported function under `public/`
   that roots a context with `context.Background()` / `TODO()`, or that
   calls an I/O verb (the packages above, a generated store's write verb)
   without taking a ctx. `main`, `init`, tests and generated files are
   exempt; a documented root carries `//boxer:lint disable=CS020
   reason="…"`. The functions still rooting one are held in the rule's
   baseline, which only shrinks.

## Alternatives

- **Keep ctx-less forms beside the ctx-first ones** (the generated stores
  did, as `Begin` / `BeginCtx`). Killed: the ctx-less form is the one that
  gets called, and it silently drops the call identity and the caller's
  deadline. VERSIONING's deprecation window does not yet bind, so no
  forwarding wrappers are kept.
- **A context in `app.MsgHandlerFunc`'s signature** instead of
  `Msg.Context()`. Killed: every subscriber in the tree changes for a value
  most of them never read; `Msg.Context` is the `net/http` shape.
- **A ctx parameter on every UI-facing method** (window open, column-width
  load, the widgets' calls). Killed for now: a render loop has no ctx to
  hand each call; the owner's lifetime ctx, given at construction, is the
  same ctx with less plumbing.

## Consequences

- A slow store at shutdown costs the shutdown its deadline, not each
  write's own timeout; a store that honours its ctx can no longer push the
  process into the forced exit.
- Rows written by the trail verbs and the generated stores carry the call
  identity on the caller's ctx.
- Every caller of these interfaces changes; the migration note lists the
  renames.
- An app keeps one more field, its `ioCtx`, set in `Mount`; a test that
  builds an app without mounting it sets it.

## Choices for review

- `app.ContextOf` returns the lifetime scope's **tail** ctx rather than its
  work ctx, so persist saves in `Unmount` still run; work that must stop at
  leave uses `LifetimeOf(mc).Ctx()`.
- The shutdown ctx gets four fifths of the grace.
- `app.Msg.Context()` ends when the in-process request returns, including
  on the reply; a handler that works on after replying uses its own
  lifetime.
- A bus request whose ctx has a deadline waits that deadline even when it
  is longer than the transport default. `chlocalbroker.ExecOnPool` waits a
  short grace past the caller's deadline because the service enforces that
  deadline itself and answers with the more precise error.
- The window host, the column-width resolver, the task supervisor, the
  agent service, the coverage trail, sysmtee and the log bridge take a
  lifetime ctx at construction instead of a ctx per call.
- Deferred: the introspect providers' `Snapshot` / `SnapshotArgs` /
  `SnapshotWhere` take no ctx yet (a `// deferred:` note on
  `introspect.Provider`); the providers that read a store are in CS020's
  baseline. The persist backend's round trip stays inside its lock (the
  ADR-0386 deferral stands).

## Status

Proposed.

## References

- [ADR-0386](./0386-panic-boundaries-at-runtime-owned-entry-points-and-what-the-in-process-bus-promises-a-requester.md) — the in-process bus's promises and the deferrals this ADR picks up.
- [ADR-0387](./0387-what-a-binary-does-when-a-stored-table-is-older-or-newer-than-its-schema.md) — the generated stores this changes.
- [ADR-0322](./0322-app-instances-drain-a-lifetime-scope-between-unmount-and-unload.md) — the lifetime scope whose tail `app.ContextOf` returns.
- [doc/migration/2026-11-io-context.md](../migration/2026-11-io-context.md) — the rename table and which ctx to pass.
