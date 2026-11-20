---
type: adr
status: proposed
date: 2026-11-20
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0386: Panic boundaries at runtime-owned entry points, and what the in-process bus promises a requester

## Context

The keelson runtime calls code it does not own from places it does own: a
goroutine entered through a lifetime scope, a drain hook, a background job's
compute, a task's work, a bus handler, a headless service's `Mount` and
`Unmount`, a window's `Mount`, `Frame` and `Unmount`. Without a boundary at
those places, one app's panic — an index out of range on data it did not
expect, an id-stack mismatch — ends the process, and every other window's
`Unmount` and state save with it. Two of the paths made it worse: a
background job's deferred `Done(nil)` ran during the unwind and recorded a
success just before the process died, and `task.Run` published the panic and
then re-raised it.

The in-process bus ran every handler a request matched inline, on the
requester's goroutine, and only then waited on the request's timeout. A
handler doing store I/O held its requester — possibly the render goroutine —
for as long as the I/O took, whatever the timeout said, and a subject with no
subscriber cost the requester the whole timeout. The agent service relied on
the inline dispatch to see which goroutine a request came from
([ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)).

## Decision

**SD1 — Every runtime-owned entry point that calls foreign code is a panic
boundary.** It recovers, turns the panic into an error that carries the panic
site's stack and wraps `ph.ErrPanic` (`ph.Guard`, `ph.ConvertPanicToError`),
makes that error the failure of the one unit that panicked, and logs it at
Error level through the logger the log bridge lands. The process goes on.

| Entry point | A panic becomes |
| --- | --- |
| `lifetime.Scope.Go`, a drain hook | an Error log row; the work counts as returned and the drain completes |
| `bgjob.Runner`, `bgjob.Keyed` compute | the run's error: `StateFailed`, the task ends as an error |
| `task.Run`, `task.RunToCompletion` work | the task's Error and the returned error; not re-raised |
| an `inprocbus` handler | an Error log row; the handlers after it still run |
| a service's `Mount` / `Unmount` | that service's failed `Mount` / `Unmount`, with the panic as its last error |
| a window's `Unmount`, workingset compose | that window's error; the reap goes on to the others |

**SD2 — A window's `Frame` is the exception: logged, then re-raised.** A
`Frame` that stopped midway has left the immediate-mode stream unbalanced:
containers opened on the client and not closed, the id and capture stacks
mid-push. The panic is logged with the window's app id and key and re-raised;
the host's `Run` reaps every window — `Unmount`, the workingset save, the
drain — under the shutdown grace, as after a signal, and the process then ends
with the panic. A `Mount` that panics before sending anything to the client
leaves the stream as it was, so it is a failed mount the window shows; after
drawing, it is re-raised as `Frame`'s is.

**SD3 — `task.Run` does not re-raise.** No caller recovers above it; the
panic is the returned error, tracked or not.

**SD4 — What a requester on the in-process bus may rely on.**

- A subject no subscription matches fails at once with `ErrNoResponders`.
- The wait is bounded by the request's timeout, or by its context through
  `Client.RequestCtx`. The handlers run on a goroutine of the request's own,
  in subscription order; at the timeout the requester gets `ErrTimeout` and a
  later reply is dropped.
- When every handler returns within the timeout, `Request` returns after all
  of them, as a publish does: what a handler did before it returned is visible
  to the requester. A reply sent later, from a goroutine of the handler's own,
  is waited for until the timeout.
- `Publish` is unchanged: its handlers run inline, in order, before it returns.

What a handler must not do inline: block on I/O whose bound is not well under
the requester's timeout. It answers from a goroutine of its own, or the
requester sees `ErrTimeout` while it runs on. The persist backend and the
facts store bound each round trip below the shutdown drain for that reason.

**SD5 — The requester's goroutine travels on the message.**
`app.Msg.RequesterGoroutine` names the goroutine a request was made on; zero
for a publish and on other transports. A handler that must refuse a caller's
goroutine reads it — the agent service refusing a request from the render
goroutine (ADR-0269) does.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `inprocbus.Client.Request` / `RequestWithTimeout` | no responder: `ErrNoResponders` at once instead of `ErrTimeout` after the wait; handlers off the requester's goroutine | callers that read `ErrTimeout` as "no service" (`ladingpublish`) |
| `inprocbus` | added `ErrNoResponders`, `Client.RequestCtx` | none |
| `app.Msg` | added `RequesterGoroutine` | the agent service's render-goroutine refusal |
| `task.Run`, `task.RunToCompletion` | a panic in work is returned, not re-raised | none in the tree |
| `ph` | added `Guard`, `ErrPanic`; the converted error carries a stack | none |

## Alternatives

- **Recover in `Frame` and go on drawing.** Killed: the stream is sent message
  by message and has no rollback point, the client's interpreter holds the
  open containers, and the Go side holds the id and capture stacks mid-push.
  The next frame would be drawn on that.
- **Isolate each window's frame in a capture buffer and drop it on a panic**
  (the sound form of the above). Deferred: every widget inside would see
  `IsCapturing`, which fetchers refuse, and a call that waits on the client's
  answer cannot be captured. It needs a capture that only the window host can
  tell from a deferred block's.
- **A goroutine per request without waiting for its handlers to return.**
  Killed: a requester would no longer see an observer's effects, and the
  handler's own writes before its reply, on return; tests and callers rely on
  that ordering.
- **`ErrNoResponders` that also matches `ErrTimeout`.** Killed as a
  compatibility shim; the change is marked breaking instead.
- **Refusing every request from the render goroutine at the bus.** Killed:
  apps request from `Frame` legitimately; only a handler knows whether it
  waits on the next frame.
- **A codelint rule (CS019) for a bare `go` in the runtime.** Not written: the
  runtime tree has some seventy `go` statements, most of them the runtime's own
  loops calling no foreign code, so the rule would be noise.

## Consequences

### Positive

- One app's panic in any of the SD1 places costs that app's unit of work, and
  is in the log with its stack.
- A shutdown after a `Frame` panic saves every window's state, within the
  grace.
- A slow or absent responder costs a requester its timeout at most, and an
  absent one nothing.

### Negative

- A request's handlers no longer share the requester's goroutine: a handler
  that relied on that, for a lock or a goroutine-local check, has to change.
  The agent service did and reads SD5's field.
- One goroutine per request while its handlers run.
- A test that set a tight request timeout to fail fast on an absent responder
  now races its handler instead; such tests get a timeout above the handler's
  work.
- A `Frame` panic still ends the process (SD2).

### Neutral

- The audit record of a request with no responder is an error, not a timeout.

## Migration — Tier 1

- **Breaks.** A request on a subject nobody subscribes returns
  `inprocbus.ErrNoResponders`, not `inprocbus.ErrTimeout`.
- **Path.** A caller that reads `ErrTimeout` as "the service is not running"
  also checks `errors.Is(err, inprocbus.ErrNoResponders)`. A handler that
  compared the current goroutine with a caller's compares
  `msg.RequesterGoroutine` instead.
- **Regeneration.** None.
- **Old shape.** Removed.

## Verification plan — Tier 1

- **Lane.** Default `go test`: one test per SD1 boundary panics inside the
  callback and asserts the unit's failure and that other work went on; the
  bus tests cover no responder, a slow handler against the timeout, a reply
  after the timeout and the publish-like ordering.
- **What would fail.** A boundary removed: its test panics the test binary.
  Inline request dispatch restored: the slow-handler test exceeds its bound.
- **Gap.** The `Frame` path is tested at its function, not through a running
  host; that needs the client.

## Choices for review

- SD2 takes the light cut — re-raise after logging — rather than isolating a
  window's frame.
- A workingset saved at the shutdown after a `Frame` panic reopens the window
  that panicked on the next start. Not addressed here; a crash loop through
  restore is possible.
- `task.Run` returns the panic for untracked work too, not only for tracked.
- The persist backend's mutex still spans the round trip; a per-call bound of
  3 s (`persist.CallTimeout`) and a per-insert bound on facts writes
  (`chstore.WriteTimeout`) are the stopgap. Taking the round trip out of the
  mutex needs a store per call in flight and is deferred.
- `RequestCtx` is on the concrete client only; `app.BusI` gains a context with
  the change that puts one on the bus and store interfaces.
- A request with no responder is audited as an error.

## Status

Proposed — awaiting review by the keelson runtime maintainers.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0038](./0038-keelson-background-task-primitive.md) — the task primitive
- [ADR-0261](./0261-one-render-goroutine-for-every-app.md) — one render goroutine
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — the refusal of a request from the render goroutine
- [ADR-0322](./0322-app-instances-drain-a-lifetime-scope-between-unmount-and-unload.md) — lifetime scopes and the drain
- [ADR-0352](./0352-services-headless-apps-a-service-host-runs-at-boot-under-a-predicate-composed-with-the-launch-limit.md) — headless services
- [watchbill-architecture](../explanation/watchbill-architecture.md) — the per-handler recover this follows
