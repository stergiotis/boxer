---
type: adr
status: accepted
date: 2026-08-15
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-15
---

# ADR-0188: App-instance effect tracking — derived teardown, guarded dataset withdrawal, live-effect tables

## Context

An app instance acquires effects the runtime mediates — bus subscriptions,
capability grants, background tasks, ad-hoc dataset bindings — and today
every one of them is released by the app author's `Unmount`, or not at all.
The host's closing edge (`windowhost.reapWindow`) refcounts the mount, saves
the workingset, and calls `Unmount`; it unsubscribes nothing, revokes
nothing, cancels nothing. The one host→app lifetime signal,
`MountContextI.Cancel()`, is wired to a `nil` channel in `windowhost`, so
`task.ForApp`'s documented cascade-cancel on window close never fires
([ADR-0155](./0155-app-embed-seam.md) §SD4 decided a real channel for
embeds and left the window case as a recorded follow-up). Seven of the
fourteen `Unmount`s under `apps/` were empty on 2026-08-15; `capdemo`
releases its file-watch subscription on its Stop and Close buttons but not
in `Unmount`. The runtime cannot tell a complete empty teardown from a
missing one.

On the provider side, an ad-hoc dataset ([ADR-0134](./0134-adhoc-datasets.md))
is retracted in one step inside the producer's `Unmount`; a consumer bound to
the handle learns of the loss on its next query, as an unknown-table error.
The applet host's `datasetRebinder` handles only the appear direction — it
polls until an alias resolves and then stops — and ADR-0134 already defers
the push notification (`adhoc.published`) that would replace the poll,
"wanting its own wire surface".

Both are instances of one shape, named formally in
[spatiotemporal-composability-lessons](../explanation/spatiotemporal-composability-lessons.md)
(L2–L4, L7): correctness that rests on the author remembering the inverse,
where the runtime already holds enough to run it. The bus records each
subscription's owning app id; the broker holds each client's grants; the
task API captures the mount-cancel channel; the dataset service knows every
live handle. What is missing is the *instance* scope on that bookkeeping,
the host running it at the closing edge, and the withdrawal side of dataset
notification. Constraints carried from earlier decisions: the four-method
`AppI` contract does not grow (the composition survey's DN1); threat model
is hygiene, not security ([ADR-0026](./0026-app-runtime-and-capability-subjects.md));
the in-proc bus and NATS must keep one client-facing shape (ADR-0026 §SD5);
every relation should be queryable ([ADR-0094](./0094-keelson-introspection-tables.md)).

## Design space (QOC)

**Question.** Who releases an app instance's runtime-mediated effects when
its window closes, and how do dependents of a withdrawn ad-hoc dataset learn
of the withdrawal?

**Options.**

- **O1 — Authored teardown, audited.** Keep `Unmount` as the release site;
  add a codelint that flags an app holding an `unsubscribe func()` field
  with an empty `Unmount`, and document the checklist.
- **O2 — Instance-scoped resources, host-run disposal.** Each mediated
  resource is owned by the instance that acquired it and carries its own
  inverse: the per-window bus client closes (dropping every subscription and
  runtime grant it holds), the per-window stop channel closes (cascading
  task cancel), and the host runs both at reap after `Unmount`. Dataset
  withdrawal becomes two-phase (leave, notify, unload) on a small event
  surface. Live tables expose the effect graph.
- **O3 — A generic effect accumulator on the mount context.** `ctx.Effect(f)`
  where `f` returns its inverse; the host runs the accumulator at reap;
  every acquisition — mediated or app-private — goes through it.
- **O4 — Process per app.** Rejected by ADR-0026 (its O4); listed for
  completeness.

**Criteria.**

- **C1 — Structural for mediated resources.** No author step between
  acquisition and release for anything the runtime brokers.
- **C2 — Instance-correct.** Two windows of one app, and a singleton mounted
  once and shown twice, release exactly their own effects.
- **C3 — Contract growth.** `AppI` unchanged; `MountContextI` additive at
  most; no new authoring model.
- **C4 — Cost.** No per-effect closure; bookkeeping the runtime already
  keeps, gaining an instance dimension.
- **C5 — Observability.** The effect graph is a query (P3/P4).
- **C6 — Transport parity.** The in-proc client and the NATS client behave
  alike at close.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | ++ | +  | ++ |
| C2 | −  | ++ | +  | ++ |
| C3 | ++ | +  | −  | −− |
| C4 | ++ | +  | −  | −− |
| C5 | −  | ++ | −  | +  |
| C6 | +  | ++ | +  | −  |

O1 cannot see the failure it targets — a lint sees fields, not effects, and
an app that never stores the closure passes. O3 is structural only for
authors who call it, which is the discipline it set out to remove, and it
duplicates `Unmount` for app-private state; a NATS connection per instance
already *is* the accumulator for the bus. O2 wins on C1/C2/C5/C6 and costs
one method on the in-proc client, one channel per window, one event subject
family, and three providers.

## Decision

We will make every runtime-mediated effect instance-scoped and have the host
release it at the closing edge, in the order *leave, unmount, unload*: close
the instance's stop channel, run the app's `Unmount`, then close the
instance's bus client. Ad-hoc dataset withdrawal becomes two-phase with a
notification, and the live effect graph is exposed as introspection tables.
Apps keep `Unmount` for app-private state; they may still release mediated
effects early, and doing so stays a no-op at close.

### SD1 — The bus client is the instance's accumulator

`inprocbus.Client` records the ids of the subscriptions it created and gains
`Close() error`: it unsubscribes them all, drops the client from the
router's registry only if the registry still points at this client
(the registry is keyed by app id and the newest client wins, unchanged),
and marks itself closed — later `Publish`/`Subscribe`/`Request` return
`ErrClosed`. Runtime grants are already attributes of the client
(`AddCap`), so closing the client is the revocation ADR-0026 called "later";
sticky grants recorded as facts are unaffected and re-apply at the next
mount as before. `natsbus.Client.Close()` already exists with the same
meaning (connection close). The host reaches the method through an optional
`app.BusCloserI` type assertion — the ADR-0155 §SD1 optional-capability
pattern — so `app.BusI` does not change and hosts on a `NoopBus` skip it.
The client carries the instance key the host minted it for; subscriptions
record it beside the app id.

### SD2 — A real stop channel per window; the closing order is a contract

`windowhost` allocates one `chan struct{}` per window and passes it to
`NewStaticMountContext`; `reapWindow` closes it *before* `Unmount` and
closes the bus client *after*. `task.ForApp`'s cascade then works as its
doc comment says; nothing in `task` changes. The order is documented on
`AppI`: an app may use its bus inside `Unmount`; a goroutine that outlives
`Unmount` sees `ErrClosed` rather than a silently live client. Embedded
instances follow ADR-0155 §SD4 unchanged (real channel closed at
`handle.Close()`); this SD closes its recorded window-side follow-up.

### SD3 — Two-phase ad-hoc dataset withdrawal, with events

`adhocdata.Service.Retract(handle)` becomes: **leave** — remove the dataset
from resolution (the catalog and `adhoc.resolve` stop naming it);
**notify** — publish `adhoc.event.retracted` with `{handle, alias,
publisher, revision}`; **unload** — after a bounded grace (`RetractGrace`,
default one bus request timeout) deregister the provider, the key, and the
file, so a query that had already resolved the handle completes. The
symmetric `adhoc.event.published` (same payload) lands the push
notification ADR-0134 deferred, on the same wire surface. Consumers:
`sqlapplet`'s `datasetRebinder` subscribes for the aliases it declares —
`published` binds without polling, `retracted` of a bound alias unbinds and
returns the alias to pending, so a later publish re-resolves (the provider
may be a different instance; the handle tells them apart); play's
`bindDataset` unbinds on `retracted` and surfaces the existing dataset
notice. Minted applet manifests gain `Sub adhoc.event.>` beside their
`Pub adhoc.resolve`. The guard is bounded rather than exact: the service
does not know its grantees (grants are audited, not recorded), and
first-party consumers under the hygiene model do not need an ack protocol
to be correct — the deferral below records the exact form.

**Events are hints; request/reply is truth.** The bus makes no delivery
promise the binder may rely on: the in-proc router delivers synchronously
and in order to whoever is subscribed at that instant and loses nothing,
but NATS core (ADR-0026 §SD4, the transport the client-facing shape must
survive unchanged) is at-most-once — a slow consumer or a reconnect drops
messages — and an at-least-once transport would redeliver late. So the
binder treats a `published` under a pending alias as a reason to *ask*
(`adhoc.resolve`, off the render thread) and binds the answer; treats a
`retracted` as monotone truth (a retracted handle never returns, so a
duplicate or late one is a no-op); replays hints in arrival order; and runs
a slow reconcile tick (`datasetReconcileInterval`, 30 s) that re-asks for
pending aliases and, in the same round trip, verifies each bound handle —
`adhoc.resolve` accepts an optional `handle` and answers `handle_live` —
swapping a binding whose handle has left for its successor. A lost hint is
therefore a delay of at most one tick, a stale hint is corrected by the
answer, and the open-time race is closed by subscribing before resolving
(per-connection ordering at the server makes that sufficient on NATS as
well). Where events cannot be subscribed at all the tick runs at the
seconds-scale poll interval. The tick also carries revisions: a republish
whose hint was lost is noticed as a higher revision on the same handle.
Two knobs exist for the headless lane, not for operation:
`BOXER_SQLAPPLET_DATASET_EVENTS` (`on` / `drop` — subscribe but discard,
the slow-consumer simulation / `off`) and `BOXER_SQLAPPLET_DATASET_RECONCILE`
(the interval; default 30 s).

### SD4 — The live effect graph as introspection tables

Three `FreshnessLive` providers, registered where the state lives, in the
`keelson('…')` namespace: `subscriptions` (app id, instance key, pattern,
subscription id — from the router), `client_caps` (app id, instance key,
pattern, direction, origin `manifest|host|grant` — from each live client),
`tasks` (task id, kind, owner app id, owner instance key, state, started
— from the task observer/supervisor). Names follow the plural-noun
convention of `apps`, `windows`, `adhoc`. Together with `windows` and
`adhoc` these make the composition survey's descriptive graph (its S1) a
query over effects, not only over launches.

### Milestones

- **M0 — Stop channel and client close.** ✓ SD2 and SD1: `Close()` on the
  in-proc client, per-window channel, reap order, `AppI` doc comment,
  `capdemo` left as it is (its leak now closes with the client). Landed
  2026-08-15: the router keeps the *live* clients per app id and
  `ClientByAppId` answers with the newest live one; the instance key is
  stamped through a second optional capability, `app.BusInstanceI`; the
  read surface SD4 needs (`Inst.Subscriptions()`, `Inst.LiveClients()`)
  came with the bookkeeping. A singleton shown in two windows carries the
  mounting window's stop channel and client to the last release. Follow-up
  found by driving the closing edge live (2026-08-15): the task monitor
  used to mark a handle terminal *silently* on parent or mount cancel, on
  the premise that the bus might be tearing down, so `keelson('tasks')`
  and the supervisor's audit kept a closed window's task as running until
  the 30 s abandon watchdog. Under the leave → unmount → unload order the
  bus is open at that moment, so the cascade now announces a
  `task.<id>.cancel` (reasons `CancelReasonMountReleased`,
  `CancelReasonParent`) and leaves the terminal to the worker's own
  Done/Error, which publishes as usual (ADR-0038 carries a dated Update).
- **M1 — Dataset withdrawal.** ✓ SD3: service phases, two event subjects,
  `sqlapplet` and play consumers, applet manifest cap. Landed 2026-08-15:
  `RetractGrace` defaults to one bus request timeout (F1 taken as
  written; `FlushRetracts` makes it synchronous for `Close` and tests);
  the applet binder subscribes before it resolves at open and keeps the
  seconds-scale poll only as a fallback for a bus that cannot subscribe
  to the events; ADR-0134 carries a dated Update. Adjusted the same day
  after the delivery-guarantee review: the binder is level-triggered —
  hints trigger a resolve, a 30 s reconcile verifies bound handles through
  the extended `adhoc.resolve` — and replays hints in arrival order (an
  earlier fold into per-kind sets lost a publish that followed a retract
  within one frame).
- **M2 — Live tables.** ✓ SD4: three providers, howto snippet. Landed
  2026-08-15 as `RegisterEffects` in the introspection providers package
  (ADR-0094 carries a dated Update); `client_caps` is its own table (F3
  taken as written) with a `declared` column in place of an origin enum,
  since the client records no origin per cap and a comparison against the
  registered manifest is checkable where a label would not be.
- **M3 — Interleaving lane.** ✓ The property test in Verification, and a
  dated Update on ADR-0026 pointing here for revocation and lifecycle.
  Landed 2026-08-15 as `TestClosingEdge_InterleavingLeavesNoTrace`
  (`rapid.Check` over open / close / republish of a factory app and a
  singleton, checking after every step and against a fresh assembly at
  the end); a mutation that skips the client close fails it.

### Deferred

- **Exact withdrawal guard.** Recording bindings (`adhoc.bind`/`unbind`
  through the instance's client, so SD1 releases them) would let `Retract`
  wait for the dependents that actually resolved the handle instead of a
  grace period. Trigger: a consumer whose teardown must read the dataset
  after the producer left.
- **Broker-side eviction of file handles.** `fsbroker` keys handles by app
  id; the wire (`Msg.Sender`) carries no instance key, so a handle cannot be
  attributed to a window. The handle's *cap* dies with the client (SD1);
  the broker entry stays until explicit close or process exit. Trigger: an
  instance dimension on the bus envelope, which the NATS swap
  (ADR-0026 §SD4) may supply.
- **Lifecycle subjects.** ADR-0026 §SD3 reserved `runtime.app.{id}.{event}`;
  M0 does not publish them. Trigger: the first broker that needs to observe
  an instance closing rather than be closed by it.
- **Subscription history.** Audit rows stay request-only; the live
  `subscriptions` table can be teed into facts later (ADR-0090 P5 shape).
- **Declared provisions.** Which datasets or subjects an app *offers* stays
  the composition survey's F3 — a manifest decision, not this one.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `inprocbus.Client` (exported API) | `Close() error`, `SetInstanceKey`, instance key on subscriptions; `Inst` keeps live clients per app id, `ClientByAppId` = newest live | `windowhost` reap; the embed seam when it lands |
| `app.BusCloserI`, `app.BusInstanceI` (exported API) | added, optional | none — asserted, not required |
| `app.MountContextI.Cancel()` (contract) | semantics now real under `windowhost` | `AppI` doc comment; `task.ForApp` unchanged |
| Subjects (named registry) | `adhoc.event.published`, `adhoc.event.retracted` | applet manifests: `Sub adhoc.event.>`; capslock baseline if the SSA sees a new sink |
| Introspection tables (named registry) | `subscriptions`, `client_caps`, `tasks` | `keelson('tables')` catalog reflects them; howto snippet |
| `adhocdata.Service.Retract` (exported API) | two-phase, `RetractGrace` | producers unchanged; consumers gain a subscription |
| `adhoc.resolve` (wire, additive) | request `handle`, reply `handle_live` / `no_live`; `adhocdata.ResolveVerifyRequest`, `ErrNoLiveDataset`, `Service.IsLive` | the applet binder's reconcile; older callers unaffected |

## Alternatives

- **Sweep subscriptions by app id at reap.** Kills a sibling window's
  subscriptions and a singleton's shared ones; the unit is the client.
- **Close the bus client before `Unmount`.** Breaks every app that
  publishes a farewell or flushes state through the bus in `Unmount`.
- **Exact guard now (ack protocol).** Needs a binding registry and an app-side
  unbind — the authored step this ADR removes; bounded grace first, exact
  form deferred with its trigger.
- **One combined `effects` table.** Heterogeneous columns force a
  key/value shape; three narrow tables join on `(app_id, instance_key)`.
- **Per-effect closure accumulator on `MountContextI` (O3).** Structural
  only for callers; the mediated resources already carry their inverse.

## Consequences

### Positive

- A closed window leaves no subscription, no runtime grant, and no
  goroutine that could still act with its authority; the guarantee is the
  host's, not the author's.
- `task.ForApp` matches its documentation.
- The in-proc client and the NATS client agree at close.
- Dataset consumers react to withdrawal in a frame and re-resolve on the
  next publish; the appear-side poll goes away.
- "Who is subscribed to what, holding what, running what" is SQL.

### Negative

- Reap does more work and must keep its order; the order test is the guard.
- An app whose goroutine kept using the bus after `Unmount` now gets
  `ErrClosed` where it silently succeeded — a latent defect made visible.
- One more cap on minted applet manifests; every dataset consumer carries a
  subscription for the lifetime of a binding.
- The withdrawal guard is bounded, not exact; a consumer mid-query at the
  end of the grace can still see an unknown table.

### Neutral

- Grants remain addressed to an app id and land on the newest client for
  that id (pre-existing).
- Sticky grants and the audit trail are untouched.
- The lessons page's L1 (declared provisions) and L5–L6 stay outside this
  decision.

## Migration — Tier 1

- **Breaks.** Nothing at compile time. Behaviourally: bus use from a
  goroutine that outlives `Unmount` returns `ErrClosed`.
- **Path.** Apps that already unsubscribe in `Unmount` need no change
  (unsubscribing a released id is a no-op). Dataset producers need no
  change; consumers other than `sqlapplet` and play add the `retracted`
  handler and the `Sub adhoc.event.>` cap.
- **Regeneration.** None; no generated surface changes.
- **Old shape.** Authored release keeps working indefinitely; the host's
  release is additive.

## Verification plan — Tier 1

- **Lane.** Default `go test`: `inprocbus` — two clients of one app id,
  close one, the other's subscriptions survive, post-close ops return
  `ErrClosed`; `windowhost` — an app that spawns via `task.ForApp` and
  publishes in `Unmount`: close → task cancelled, publish succeeded, client
  closed, in that order; `adhocdata` — resolve stops at leave, provider
  answers within grace and not after, both events observed; `sqlapplet` —
  bound alias returns to pending on `retracted` and rebinds on the next
  `published`.
- **Lane.** Property test (`rapid.StateMachine`) over interleaved
  open / close / publish / retract on a fake host: after any sequence the
  router's subscription set and the dataset service's live set equal those
  of a fresh host given the same final configuration — the confluence
  oracle from the lessons page (L5), which is what "no leaked effect after
  any interleaving" means.
- **Lane.** Headless: `keelson('subscriptions')` gains and loses rows across
  a window open/close.
- **What would fail.** A leaked subscription or task after close; a
  publish in `Unmount` failing; a retracted handle still resolving; a bound
  alias not returning to pending.
- **Lane.** Default `go test`: `sqlapplet` — a binder over a fake resolver
  binds from a hint's *answer* rather than its handle, recovers a lost
  `published` and a lost `retracted` from the tick alone, keeps a binding
  whose handle is live when a newer sibling appears, and replays a
  retract-then-publish frame in order; the live variant drops the events
  subscription and lets the tick swap the binding against the real service.
- **Lane.** Headless, by hand
  ([launch-apps-non-interactively § 6b](../howto/launch-apps-non-interactively.md)):
  with `BOXER_SQLAPPLET_DATASET_EVENTS=drop` and a 40 s interval, imzrt's
  `Capture Heap` published 4 s after the `profile-heap` applet mounted and
  the applet bound at the first tick, 38 s later; with events on, in the
  same second (2026-08-15).
- **What would fail.** A binding stuck pending or bound to a dead handle
  under message loss; a stale hint binding a retracted handle.
- **Gap.** `fsbroker` handle entries and lifecycle subjects are deferred
  above; the NATS lane is integration-tagged and not run by default, so
  the at-most-once behaviour is exercised only by simulation (dropped
  subscription, fake resolver), not against a NATS server.

## Status

Accepted 2026-08-15, with M0–M3 already landed under `proposed` status by
the owner's direction, the record edited in place as implementation
taught (the milestone notes above): the task monitor announces external
cancellation instead of terminating silently, and the dataset binder is
level-triggered — events as hints, request/reply as truth, a reconcile
tick — after a review of what the bus promises. Of the three forks the
proposal left open, F1 (`RetractGrace` = one bus request timeout) and F3
(`client_caps` as its own table, with a `declared` column) were taken as
written; F2 (publishing the reserved `runtime.app.{id}.unmount` lifecycle
subject at reap) was not taken and stays under *Deferred* above with its
trigger. From here on the record changes only through dated `## Updates`
entries.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [spatiotemporal-composability-lessons](../explanation/spatiotemporal-composability-lessons.md)
  — L2, L3, L4, L7 and the paper's leave-then-unload guard.
- [dataset-following-os-analogues](../explanation/dataset-following-os-analogues.md)
  — §SD3's two-phase withdrawal and identity-over-value against POSIX,
  macOS, Android and Fuchsia; names the guard axis the deferral moves along.
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — `AppI`,
  `MountContextI`, capability subjects, "revokes (later)", §SD3 lifecycle
  subjects, §SD5 transport parity.
- [ADR-0038](./0038-keelson-background-task-primitive.md) — the task
  primitive whose mount-cancel cascade SD2 makes real.
- [ADR-0094](./0094-keelson-introspection-tables.md) — provider registry
  and freshness classes.
- [ADR-0134](./0134-adhoc-datasets.md) — dataset capability; the deferred
  `adhoc.published` notification.
- [ADR-0155](./0155-app-embed-seam.md) — real stop channel for embeds; the
  window-side follow-up this ADR closes.
- [Composing keelson apps — a design-space survey](../adr-background-work/app-composition-survey.md)
  — DN1, D12, S1, F3.
