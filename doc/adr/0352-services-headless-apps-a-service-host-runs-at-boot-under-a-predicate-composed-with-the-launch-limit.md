---
type: adr
status: proposed
date: 2026-11-01
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0352: Services — headless apps a service host runs at boot, under a predicate composed with the launch limit

## Context

Every registered app in the tree is a windowed imzero2 app. The manifest has
carried `SurfaceHeadless` since
[ADR-0026](./0026-app-runtime-and-capability-subjects.md) §SD13 reserved it
for a later lift of CLI commands into apps, and nothing activates it: the
window host ignores a headless manifest, and no other host exists.

Background work that is not a window has three homes today, none of which
is an app:

- `hostboot.RegisterHostService` starts a named function once the bus, the
  facts store and the task supervisor are up. It has no manifest, so no
  declared capabilities, no row in `keelson.apps`, no lifecycle rows, no
  instance key for effects to attribute to, and no way to choose which ones
  a given process runs.
- Headless binaries — the system-metrics daemon, watchbill workers — hold a
  `BusI` and a facts store directly, each with its own bootstrap.
- Pipelines run as external processes under a streaming framework
  ([ADR-0252](./0252-stevedore-routing-failure-and-state-handling-for-framework-driven-ingestion.md));
  they are out of this ADR's scope.

Two decisions already supply the pieces a service model needs. The
launch limit ([ADR-0272](./0272-limit-launchable-apps-with-a-sql-predicate.md))
selects apps with a SQL predicate over `keelson.apps`, evaluated once at boot
on clickhouse-local, and the table shows the result as `launchable`. The
appliance topology ([ADR-0126](./0126-appliance-topology-as-data.md))
declares process families and deferred, in its §SD6, the question of which
of them should run on a given box.

What is wanted is to split a large deployment into several OS processes,
each running a chosen subset of the background work, with the same manifest,
capability, audit and introspection story a window has.

## Decision

We will make a **service** a headless app: an `AppI` whose manifest declares
`SurfaceHeadless`, mounted at boot by a **service host** on a goroutine of
its own, and stopped at shutdown through the closing edge
[ADR-0188](./0188-app-instance-effect-tracking.md) defines for windows. Which
services a process runs is a SQL predicate over `keelson.apps`, composed with
the launch limit by intersection, and the result is one introspection table.
No new app-like type is introduced.

### SD1 — A service is a headless app

A service implements `AppI` and registers with `RegisterFactory` like any
app. Its manifest declares `Surface: SurfaceHeadless`, a `Summary` and
`Topics` as a windowed app must, and the subject filters it needs; the
capability cross-check of ADR-0026 §SD10 covers it unchanged. `Frame` is
never called. `Mount` starts the work, `Cancel()` ends it, `Unmount` releases
what `Mount` acquired, and work that outlives `Mount` counts itself into the
instance's lifetime scope
([ADR-0322](./0322-app-instances-drain-a-lifetime-scope-between-unmount-and-unload.md)).

A service may declare a `LaunchKind` and receive a config at mount through
the door windows use ([ADR-0135](./0135-app-launch-requests.md) §SD4). It
may not declare `Workingset`: a workingset is written at a closing edge a
person caused, and a service has none. `Manifest.Validate` refuses the
pair.

### SD2 — A service host beside the window host

A new runtime package, `servicehost`, does for a headless app what
`windowhost` does for a windowed one. At boot, after the host services and
before the window host, it mounts each selected service once:

- an instance key from the host-wide allocator windows draw from, so a
  window and a service never share one;
- a bus client minted from the registry's declared caps, as a window's is;
- a `started` app-lifecycle row, and a `stopped` row with a stop reason at
  the end: `shutdown`, `mount-error`, or `unmount-error`;
- `Mount` on the service's own goroutine, under pprof labels naming the app
  id and the instance key, so a service's CPU is attributable the way
  ADR-0351 (proposed) attributes a window's.

A `Mount` that returns an error is logged, recorded, and the service left
out; the host boots on. That is hostboot's policy for every optional
service, and nothing here restarts a service. At shutdown the host walks the
services in reverse start order through *leave, unmount, drain, unload*:
retire the scope (which closes `Cancel()`), call `Unmount`, wait for the
scope up to the shutdown grace, close the bus client. Windows are reaped
before services, since a window may still be talking to a service while it
unmounts and never the reverse. The screenshot tour, which has no window
host either, starts no services: it renders one app per capture and is not
a deployment. `hostboot.Options.SeedServices` delivers launch configs to
services as `SeedWindows` does to windows.

`RegisterHostService` stays for services that need the booted `Runtime`
itself. A host service that wants a manifest, declared caps, a row in the
tables and selection becomes a headless app; the list is not removed.

### SD3 — Selection: a predicate that composes with the launch limit

`KEELSON_SERVICES_WHERE` holds a ClickHouse boolean expression over the
columns of `keelson.apps`, for example:

```sh
KEELSON_SERVICES_WHERE="has(topics, 'ingestion') AND NOT kind = 'demo'"
```

The host evaluates it once, at boot, on clickhouse-local, after the launch
limit has been applied, as

```sql
SELECT id FROM apps WHERE surface = 'headless' AND launchable AND (<expr>)
```

The three conjuncts are the whole composition rule:

- `surface = 'headless'` is the host's: a service predicate cannot start a
  window, whatever it says.
- `launchable` is the launch limit's column, already reflecting
  `KEELSON_LAUNCHABLE_APPS_WHERE`. A service runs only if the launch limit
  admits it *and* the service predicate selects it. The limit stays what
  ADR-0272 made it — a ceiling over the apps a process may use — and the
  service predicate chooses within the ceiling. An operator who writes both
  needs no third rule to predict the result, and one who writes only the
  limit starts no service by it.
- `(<expr>)` is the operator's choice.

**Unset means no service runs.** This differs from the launch limit, whose
unset means no limit, on purpose: a window opens when a person asks, while a
service runs unprompted, and a process that boots background work nobody
named is the worse default. Set and not evaluable — no clickhouse-local, an
expression that does not parse, a query error — the host does not boot, as
for the launch limit.

The selected set is the boot set. A headless app registered later is not
started, matching ADR-0272 §SD2.

### SD4 — `keelson('services')`

One live introspection table ([ADR-0094](./0094-keelson-introspection-tables.md)),
one row per headless app registered at boot:

| column | meaning |
| --- | --- |
| `id` | the app id |
| `selected` | whether SD3's predicate selected it |
| `state` | `not_selected`, `starting`, `running`, `stopped`, `failed` |
| `instance_key` | the host-minted key, zero when never mounted |
| `started_at`, `stopped_at` | lifecycle edges; empty until reached |
| `stop_reason` | SD2's reasons |
| `last_error` | the `Mount` or `Unmount` error, when there was one |

`keelson('apps')` is unchanged; `surface` and `launchable` already carry what
the predicate reads.

### SD5 — The selection is on the record

At boot the host writes one audit event
([ADR-0296](./0296-audit-events-as-a-trail-component.md))
carrying the predicate text and the selected ids, so a reader of the trail can
tell which services a process was configured to run and did. A running
service is a manifest app of this process, so the topology's
`app-in-component` edge covers it with no new vocabulary.

### SD6 — A process without a window

A deployment splits by booting the same binary several times with different
predicates. `hostboot.Options` gains `NoWindowHost`: Boot wires identity,
facts, bus, the selected services and the service host, and skips the window
host and the Rust client; `Runtime.Wait` blocks until the shutdown signal.
Each process carries its own component mark
([ADR-0126](./0126-appliance-topology-as-data.md) §SD2) and its
own run id.

Such a process hosts headless apps only. A windowed app's `Frame` is an
opcode stream the Rust client interprets, and its state advances only inside
`Frame`, so without that client it cannot run at all. A windowed app that
must run unattended runs under the headless carrier
([ADR-0154](./0154-headless-carrier-tree-and-driver.md)), which pays the
egui pass per frame; work it wants to keep running without a window belongs
in a headless service it reaches over the bus.

Several brokers exist only where a window does — the file Powerbox needs the
picker, the clipboard needs egui, ad-hoc datasets live in a store sealed to
one process. The service host therefore compares each selected service's
publish-direction caps against the subjects this process serves, which
`keelson('subscriptions')` already lists, and logs one warning per service
naming the caps nothing answers. The comparison runs once every selected
service's `Mount` has returned, because services subscribe in `Mount` and
one service's subjects may be another's. Refusing to start such a service is deferred until a
deployment wants it; cross-process transport between services is the NATS
bus ADR-0026 §SD4 describes, with its per-app authorisation still unbuilt.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `app.Manifest.Validate` | headless apps need `Summary` and `Topics`; `Workingset` refused for them | the manifest tests; no registered headless app existed to break |
| Environment registry (ADR-0009) | `KEELSON_SERVICES_WHERE` | `doc/env-vars.md` regenerated |
| `hostboot.Options`, `Runtime` | `NoWindowHost`, `SeedServices`, `Runtime.Wait`, `Runtime.Services` | the carousel, which keeps its defaults |
| `launchlimit` | `EvaluateAs`: the same boot-time evaluation under another caller's identity and name | nothing; `Evaluate` is unchanged |
| `windowhost.Inst` | `AllocateInstanceKey`, the window key allocator opened to the service host | nothing |
| Introspection tables | `keelson('services')` | the providers registry; ADR-0094's table list |
| App-lifecycle rows | stop reasons `mount-error` and `unmount-error` for services | readers that enumerate reasons |

No wire encoding, subject or vocabulary changes.

## Alternatives

- **A `ServiceI` type with its own registry and `keelson('services')` as
  its inventory.** Killed: it duplicates the registry, the capability
  declaration and cross-check, the launch limit and the app center, and
  splits the inventory into two tables that a topology or audit query must
  union.
- **Selection on `RegisterHostService`.** Killed: a start function has no
  manifest, so nothing for a predicate to read and nothing for a row to
  attribute to.
- **One variable for both.** Letting `KEELSON_LAUNCHABLE_APPS_WHERE` also
  start every headless app it admits. Killed: a limit over what may open and
  a choice of what runs unprompted are different intents, and an operator
  who limits a seat to "data apps" has not asked for every headless data app
  to run.
- **Intersection by a second mechanism.** Evaluating the two predicates
  separately and intersecting ids in Go. Killed: the `launchable` column is
  the intersection, visible in the table the operator already queries.
- **Unset runs every headless app.** Killed above: unprompted work by
  default.
- **A list of ids or one boolean variable per service.** Killed for the
  reasons ADR-0272 gives: a list cannot say "no demos", and it changes when
  an app is renamed.
- **`Mount` on the render goroutine.** Killed: a window-less process has
  none, and a service's `Mount` may block.
- **Restart on failure.** Deferred, with the restart policy and the
  dependency order between services; the first cut starts in registry order
  and leaves a failed service out.
- **A desired-state store in ClickHouse** in place of the variable, the R1
  ADR-0126 §SD6 deferred. Deferred again: a predicate over a table the
  process already serves needs no second store, and a store can supply the
  predicate later without changing what it selects over.

## Consequences

### Positive

- Background work gains what windows have: a manifest, declared caps checked
  against the call graph, lifecycle rows, an instance key, a table, and a
  page in the app center.
- A deployment splits by configuration: the same binary, a different
  predicate per process.
- The launch limit keeps one meaning; an operator reads two variables and
  one column.

### Negative

- Evaluation needs clickhouse-local at boot in a process that may otherwise
  not need it, the cost ADR-0272 already accepted.
- A service in a window-less process may declare a capability nothing there
  answers; the first cut warns and lets the request fail at call time.
- No supervision: a service that fails stays down until the process
  restarts.

### Neutral

- `RegisterHostService` and headless apps coexist; the former is for code
  that needs the `Runtime`, the latter for everything with a manifest.
- The app center lists services on their app pages. A Running-tab row per
  service ([ADR-0348](./0348-a-running-page-in-the-app-center-open-windows-what-they-cost-raise-and-close.md), proposed)
  is a later cut.

## Migration — Tier 1

- **Breaks.** Nothing: no headless app is registered, and `Validate`'s new
  checks reach only headless manifests.
- **Path.** A host service that wants selection writes a manifest and a
  factory, moves its start into `Mount` and its stop into `Unmount`, and
  drops its `RegisterHostService` call.
- **Regeneration.** `doc/env-vars.md` for the variable.
- **Old shape.** `RegisterHostService` is kept.

## Verification plan — Tier 1

- **Lane.** Default `go test`; the predicate tests skip where no
  `clickhouse` binary is installed, as the launch limit's do.
- **What would fail.** Service-host tests with a fake headless app: both
  predicates set, only the intersection mounts; the service predicate set
  and the launch limit excluding the app, nothing mounts; the variable
  unset, nothing mounts; set without clickhouse-local, Boot returns an
  error; a windowed app selected by the predicate is not mounted. The
  closing edge is observed in order on a fake app that records calls, and
  the scope is drained before the bus client closes. The `services`
  provider's snapshot matches the host's state through one start and one
  stop. A headless scene boots a demo service under a predicate and reads
  `keelson('services')` through play.
- **Gap.** Cross-process behaviour over NATS is not tested; this ADR does
  not change that transport.

## Milestones

- **M1 — The host ✓** SD1, SD2, SD4: manifest validation for headless apps,
  `servicehost`, lifecycle rows, the table, and a demo service under
  `apps/` for the scene.
- **M2 — Selection ✓** SD3, SD5: the variable, the composed evaluation after
  the launch limit, the audit event.
- **M3 — A process without a window ✓** SD6: `NoWindowHost`, `Runtime.Wait`,
  the served-subjects warning.

## Status

Proposed — awaiting review.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — the app contract, `SurfaceHeadless`, capability subjects.
- [ADR-0272](./0272-limit-launchable-apps-with-a-sql-predicate.md) — the launch limit this composes with.
- [ADR-0211](./0211-hostboot-runtime-bootstrap.md) — hostboot and host services.
- [ADR-0188](./0188-app-instance-effect-tracking.md) — the closing edge: leave, unmount, unload.
- [ADR-0322](./0322-app-instances-drain-a-lifetime-scope-between-unmount-and-unload.md) — the lifetime scope.
- [ADR-0135](./0135-app-launch-requests.md) — launch configs.
- [ADR-0094](./0094-keelson-introspection-tables.md) — introspection tables.
- [ADR-0126](./0126-appliance-topology-as-data.md) — component marks; the deferred desired-state question.
- [ADR-0296](./0296-audit-events-as-a-trail-component.md) — the audit event.
