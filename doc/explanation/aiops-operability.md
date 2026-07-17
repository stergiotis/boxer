---
type: explanation
audience: contributors designing the operability seams; agents operating a deployed box
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# AIOps operability, day 1 and day 2

A boxer-built application should be operable — installed, configured, and
verified on day 1; observed, diagnosed, updated, and repaired on day 2 — by
AIOps: deterministic automation for the routine loop, and an AI operator for
the judgment calls, both acting through machine-readable surfaces.

Most of that surface does not exist yet. This page holds the target, an
inventory of what stands and what is missing, the requirements, the
separations the design must keep, and the map of which external paradigms are
worth taking. It is deliberately mechanism-light and decides nothing:
decisions live in the bound ADRs — several of which are still to be written —
and where this page and an ADR disagree, the ADR is the record. The state
columns below reflect July 2026; live ADR status is tool-derived
([ADR-0092](../adr/0092-adr-overview-tool.md)) rather than maintained here.

## What "AIOps" means here

Two tiers, with different requirements:

1. **Deterministic closed loops** — reconcilers, gates, scheduled detection.
   No model in the loop. Routine day-2 work (converge, catch up, roll back on
   a failed probe) lives here.
2. **An AI operator** — judgment work: triage of novel incidents, diagnosis
   across subsystems, planning a remediation, writing the postmortem. It acts
   only through governed machine surfaces, and its plans are verified by
   machine gates, not by its own confidence.

The operational unit is the **boxer appliance**: the keelson GUI monolith
(apps behind [ADR-0026](../adr/0026-app-runtime-and-capability-subjects.md)
manifests), satellite single-purpose daemons (the sysmetrics scraper, the
deploy timer, the capture services of
[ADR-0115](../adr/0115-query-observability-data-plane-strategy.md)),
ClickHouse, NATS core, and a TLS front — pull-managed and outbound-only.

The premises of [why-boxer](./why-boxer.md) already name the target: P7 sends
highly complex tasks to agentic systems that operate the machinery through
machine-readable surfaces rather than pixels, and P3/P4 make the substrate
machine-readable by construction. That inverts the mainstream AIOps posture,
which retrofits a perception layer (log parsing, statistical anomaly
detection) onto exhaust built for humans. Here, most of the "AI" in AIOps
collapses to SQL plus deterministic reconcilers, with the model reserved for
judgment — so the work below is mostly seam-completion, not an ML stack.

## Where operability stands

| Ops function | Mechanism | State |
|---|---|---|
| Deploy / update / rollback | signed-tag pull, immutable releases, probe gate, version floor, audited break-glass ([ADR-0085](../adr/0085-imzero2-demo-pull-build-atomic-deploy.md)) | built, demo-scoped |
| Provisioning | [showcase/onbox](../../showcase/onbox/ONBOX.md) + ansible kit; OS-level substrate out of scope here | manual how-to |
| Configuration | env registry ([ADR-0009](../adr/0009-environment-variable-registry.md)) → [doc/env-vars.md](../env-vars.md) → `keelson.env` | built |
| Live state as SQL | `keelson.*` providers, `url()` + `/query` ([ADR-0094](../adr/0094-keelson-introspection-tables.md)) | built |
| History, audit, logs | `runtime.facts`: grants, audit, state, logs-as-facts ([ADR-0026 §SD6](../adr/0026-app-runtime-and-capability-subjects.md), `keelson/runtime/logbridge`) | built |
| Host metrics | unidirectional pub/sub plane, metrics are leeway facts ([ADR-0090](../adr/0090-sysmetrics-pubsub-data-plane.md)) | built, unpersisted |
| Query telemetry | ELT capture pipelines as schema objects ([ADR-0115](../adr/0115-query-observability-data-plane-strategy.md)) | proposed |
| Supply chain | license gate ([ADR-0004](../adr/0004-license-gate-cyclonedx.md)), airgapped bundle ([ADR-0095](../adr/0095-airgapped-build-bundle.md)), `keelson.extbin` digests ([ADR-0118](../adr/0118-extbin-external-process-chokepoint.md)), capslock drift gate ([ADR-0026 §SD10](../adr/0026-app-runtime-and-capability-subjects.md)) | built |
| Actuation governance | capability-as-subject; audited request/reply (publish deliberately unaudited) | built |
| Synthetic verification | `ws_probe`, screenshot tours ([ADR-0057](../adr/0057-demo-registry-and-drivers.md)), [egui-mcp](../howto/egui-mcp.md) | built |
| Task supervision | background tasks ([ADR-0038](../adr/0038-keelson-background-task-primitive.md)), `bgjob` | built, in-process |
| Liveness | heartbeat ticks as facts (`keelson/runtime/heartbeat`) | GUI host only |
| Topology as data | component marks + scraper-observed procs/sockets + the `keelson.topology_*` node/edge graph with an `origin` column ([ADR-0126](../adr/0126-appliance-topology-as-data.md), [howto](../howto/topology-queries.md)) | built, live-only; unit states deferred |
| Runbooks | [doc/howto/](../howto/adr-overview.md) corpus | human prose only |
| Health verdicts, backup/restore, alerting | — | absent |

## The observation inventory

An operator whose perception is queries is limited by which sources exist as
data. Per source: is it collected at all, does it reach the bus plane, does
history persist, and can it be queried?

| Source | Collected | On the plane | Persisted | Notes |
|---|---|---|---|---|
| Host metrics (cpu/mem/disk/net/proc/psi/gpu…) | yes | yes | no | the [ADR-0090](../adr/0090-sysmetrics-pubsub-data-plane.md) P5 persistence tee is deliberately unbuilt |
| Go runtime metrics (heap, GC, scheduler) | yes (`observability/goruntime`) | no | no | feeds imzrt in-process only ([ADR-0061](../adr/0061-imzero2-imzrt-go-runtime-dashboard.md)) |
| Frame / render metrics | yes (per-app frame cost, fps distributions) | no | no | pixels only |
| App-defined instruments | no primitive | no | no | see below |
| Topology — declared | yes (component registry → `keelson.components`; manifest `Caps` in `keelson.apps`; unit files remain in git) | n/a | in git / compiled in | [ADR-0126](../adr/0126-appliance-topology-as-data.md) |
| Topology — observed (marks, ports, proc↔component) | yes (`BOXER_COMPONENT` marks + cgroup + listening sockets, scraper-collected) | yes | no | node/edge vocabulary is `keelson.topology_*`; unit *states* deferred ([ADR-0126 §SD6](../adr/0126-appliance-topology-as-data.md)) |
| pprof profiles / execution traces | opt-in (`observability/profiling`, flight recorder in `observability/tracing`) | no | no | file- or endpoint-shaped; never becomes data |
| Linux perf | no | — | — | descoped; would ride extbin if ever wanted |
| ClickHouse internals | server-side `system.*` | — | server TTL | queryable in place; [ADR-0115](../adr/0115-query-observability-data-plane-strategy.md) is the lift-to-facts path |
| NATS server internals | no | — | — | the bus is a blind node |
| Edge (TLS front) and journald of non-boxer units | no | — | journald | logbridge captures only in-process zerolog |
| Health verdicts | liveness only (heartbeat) | no | facts | readiness and dependency checks absent |

Three observations structure the gap:

**Metrics split into four domains with one shared fix.** Host metrics are
sourced, on-plane, unpersisted; process metrics (goruntime) are sourced but
plane-less; frame metrics are pixels-only; app-defined instruments have no
primitive at all. The `goruntime` collector already mirrors the sysmetrics
Bundle shape, so publishing it as another plane domain is prepared, and a
single persistence tee then covers every domain at once. The one genuinely
new abstraction is the **instrument**: a declared, named, typed,
self-enumerating unit of app instrumentation — the
[ADR-0009](../adr/0009-environment-variable-registry.md) move (declare →
registry → self-documenting → `keelson.*` table) applied to metrics instead
of config. The design rule that keeps it small: **split by event rate**.
Low- and mid-rate events are emitted as raw facts — counters are `count()`
in ClickHouse and RED views are SQL; pre-aggregation exists in mainstream
stacks because raw events are expensive there, a constraint the facts spine
removes. High-rate paths aggregate in-process (the existing ring and
t-digest primitives) and snapshot summaries on a cadence, following the
ADR-0090 rule: publish raw counters, derive rates consumer-side.

**Topology has both halves, minus unit states.**
[ADR-0126](../adr/0126-appliance-topology-as-data.md) built the layer
marking-first: every deliberately-run process carries a supervisor-injected
component mark, the scraper observes marks, cgroups, and listening sockets,
and `keelson.topology_nodes`/`keelson.topology_edges` carry one node/edge
vocabulary with an `origin` column — so declared-versus-observed drift is a
single-table `GROUP BY`, and ClickHouse, NATS, and the TLS front appear as
marked components with sockets. The recorded blind spot: without supervisor
interrogation (the deferred D-Bus collector), a `failed` unit is
indistinguishable from an absent one — unit-state verdicts belong with the
health work. History is the other limit: the tables are latest-snapshot only
until the persistence tee exists.

**Profiling should become a verb, not a stack.** Collection exists — CPU
profile to file, an opt-in pprof HTTP endpoint, a signal-triggered
execution-trace flight recorder — but it is human-triggered, artifact-shaped,
and uncorrelated with anything. The first cut for AIOps is not continuous
profiling but an audited on-demand verb: profile a named app for a duration,
return the artifact, and land it as a fact (blob plus a parsed top-N
aggregate) keyed to the running revision via `runinfo`. Diagnosis actions fit
the verb family better than permanent collectors.

## Requirements

### Day 1 — provision, configure, verify

- **R1 — One declarative desired state.** Everything an installer decides —
  units, env, front config, DDL, retention — expressible as versioned data an
  agent can diff and dry-run; components consume declared state rather than
  accreting imperative setup steps.
- **R2 — Self-describing configuration with provenance.** `keelson.env`
  covers discovery and effective values; missing is per-knob provenance
  (default vs env vs unit drop-in) so "why is this value in effect" is
  answerable.
- **R3 — "Installed correctly" is a query.** Attestation exists (signed
  source, SBOM, extbin digests); the health half is missing — a
  machine-checkable gate per component so day-1 verification is a `SELECT`
  returning green, not a human reading journald.
- **R4 — Idempotent, convergent bootstrap.** Every stateful owner reconciles
  at boot (the [ADR-0115 §SD4](../adr/0115-query-observability-data-plane-strategy.md)
  rule, made a contract) and reports drift as facts rather than fixing it
  silently.
- **R5 — Hands-free identity and secrets.** Per-app NKey/JWT minting
  ([ADR-0026 §SD4](../adr/0026-app-runtime-and-capability-subjects.md)) and
  TLS/auth ([ADR-0082](../adr/0082-imzero2-remote-session-auth-tls.md)) are
  designed but unbuilt; day-1 automation stalls here the moment anything is
  non-loopback.
- **R6 — Dependency preflight.** ClickHouse version and feature checks, NATS
  reachability, fonts, toolchain — as queryable verdicts under one
  subcommand, not scattered assumptions.

### Day 2 — observe, decide, act, learn

- **R7 — One query surface over all state, past and present.** Largely true
  (facts plus `keelson.*`), with two holes: metric history (the unbuilt tee)
  and reconstructability — "what was true at 03:12" — which live-only
  providers cannot answer. The SQL surface doubles as the context-budget
  answer: an agent pulls aggregates, never raw streams.
- **R8 — Every actuation is an audited, capability-scoped, idempotent verb
  with dry-run.** Restart, drain, reload, deploy, snapshot, gc,
  retention-apply. Only `deploy` exists today. Ad-hoc shell must not be the
  actuation API — that is the difference between an operator and an
  intruder.
- **R9 — Autonomous update with health gates, generalized.** The
  [ADR-0085](../adr/0085-imzero2-demo-pull-build-atomic-deploy.md) recipe
  (immutable releases, the app's own probe, version floor, break-glass)
  extended beyond the demo to every unit, including schema migrations riding
  a release.
- **R10 — State lifecycle.** Retention and partitioning policy for the facts
  tables (named open in ADR-0115), backup and restore, cross-release leeway
  schema migration. Day 2 without backup is not day 2.
- **R11 — Detection as data.** Continuous evaluation producing alert facts,
  baselined by the in-tree distribution primitives; a notification leg built
  at consumer trigger.
- **R12 — Diagnosis affordances.** Topology-as-data, an error catalog
  (structured `eh`/`eb` codes → meaning → runbook), and a unified ops
  timeline — deploys, restarts, alerts, break-glass, grants — as one view,
  the first query any responder runs.
- **R13 — The operator is itself observed.** Every AI action lands in the
  same audit and facts plane as app actions; trust is carried by gates
  (probe passed, signature verified) — never by the model's self-report.
  This is P6 extended to operations.
- **R14 — Sensitivity is structural.** Model context is an egress. The
  membership-tagged sensitivity mechanism
  ([ADR-0090 §SD8](../adr/0090-sysmetrics-pubsub-data-plane.md)) must be the
  single policy point through which anything reaches a model; deferred
  masking becomes load-bearing the day an external model operates the box.

## What must stay separated

1. **Telemetry plane vs control plane.** Already canonical (ADR-0090:
   strictly unidirectional publish; control is a separate subject family).
   It guarantees a runaway analysis loop cannot actuate through the plane it
   observes.
2. **Desired state vs observed state vs history.** Three stores with
   different owners: declared config and DDL (versioned), live introspection
   (`keelson.*`), facts history. AIOps *is* the reconciliation among the
   three; "fixing" drift without recording it breaks the loop.
3. **Mechanism vs policy.** Verbs are mechanisms; grants, signing
   requirements, version floors, and break-glass are policy.
   [ADR-0085 §SD11](../adr/0085-imzero2-demo-pull-build-atomic-deploy.md)
   (standing posture vs momentary audited action) is the template for every
   gate an AI may need to pass.
4. **Judgment vs execution.** The model proposes; deterministic tools
   execute; machine gates verify. An agent never "carefully restarts
   things" — it invokes a restart verb whose postcondition is probed the
   same way regardless of who called it.
5. **Machine surface vs human surface.** Same capability, two projections
   (P7). Pixels are never the API; [egui-mcp](../howto/egui-mcp.md) is a
   verification instrument — does the human view corroborate the data view —
   not an actuation path.
6. **App vs substrate.** What every app and daemon must provide (the
   operability contract below) vs what the platform supplies (bus, brokers,
   scraper, deployer, introspection host). Without this split, each new app
   re-decides its operability.
7. **Shareable vs sensitive.** One membership-keyed masking point (R14), not
   scattered redaction.

## Missing functionality

Eight pieces, roughly in dependency order. The ones marked *(ADR)* are
decision-shaped — their vocabularies or contracts become public-stability
surfaces and warrant a record before code.

1. **The operability contract** *(ADR)* — an extension of the app manifest:
   every app and daemon declares and provides a health probe (liveness,
   readiness, dependency checks), its ops verbs, a boot reconciler, env
   registration, facts emission, and a help book. Enforced the house way — a
   lint or test gate in the capslock mold — so an app that skips the
   contract fails CI, not the operator during an incident.
2. **`keelson.health` and `keelson.services`.** Health as a provider
   (per-process) plus observed topology as data. The topology half is
   built — the node/edge vocabulary, marks, and socket observation shipped
   as [ADR-0126](../adr/0126-appliance-topology-as-data.md) — with unit
   *states* (`keelson.services`, the D-Bus read) explicitly deferred into
   this item's remaining half, beside the health verdicts.
3. **The ops verb family.** Audited request/reply subjects with CLI parity,
   dry-run, typed errors, idempotency keys. The
   [ADR-0085](../adr/0085-imzero2-demo-pull-build-atomic-deploy.md) derived
   practice — ops tools are Go commands on the house libraries — already
   mandates the shape.
4. **The instrument registry** *(ADR)* — the app-metrics primitive described
   above: declaration API, membership vocabulary, `keelson.instruments`,
   snapshots onto the plane.
5. **Persistence and detection.** The ADR-0090 P5 tee for all metric
   domains; detection rules as refreshable materialized views writing alert
   facts — reusing the ADR-0115 machinery so alert rules are schema objects:
   declared in DDL, drift-detectable, self-observing.
6. **State lifecycle tooling.** Retention as declared TTL policy; backup and
   restore verbs verified by restore-probe (the `ws_probe` lesson applied to
   data); schema-version facts plus a migration verb integrated with the
   deploy gate.
7. **Machine runbooks.** The repo already carries agent-facing skills
   (`doc/skills/`); ops runbooks belong there as the agent-facing pair of
   [doc/howto/](../howto/adr-overview.md), keyed by symptom (error code,
   alert kind) and composed only of audited verbs and queries — with a
   `keelson.runbooks` index so symptom-to-procedure is itself a join.
8. **The operator access surface.** v1 needs no new server: an SSH session
   driving the CLI and the SQL endpoints inherits the human operator's trust
   model and keeps every loopback-only default intact. An MCP server over
   the same verbs is the v2 formalization — gated on
   [ADR-0082](../adr/0082-imzero2-remote-session-auth-tls.md), which is the
   true blocker for any *remote* machine surface.

## Paradigms worth taking — and refusing

- **MAPE-K** (autonomic computing): Monitor–Analyze–Plan–Execute over shared
  Knowledge. The facts store and `keelson.*` *are* the K; monitors are the
  planes; analysis is SQL plus baselines; execution is the verb family.
  Boxer sits unusually close to this blueprint because K exists as one
  queryable substrate.
- **Wide events** (observability-2.0): one high-cardinality event store
  beats three siloed pillars. `runtime.facts` with membership vocabulary is
  wide events; the standing rule is to converge new telemetry as fact kinds
  and never add a separate logs/metrics/traces stack.
- **Golden signals / USE / RED**: adopt as per-app SQL view conventions —
  cheap, and agent and human get the same first look.
- **Desired-state reconciliation** (the GitOps lineage): declarative,
  pull-based, converge-and-report-drift.
  [ADR-0085](../adr/0085-imzero2-demo-pull-build-atomic-deploy.md) embodies
  it for releases; R4 extends it to config and schema.
- **The operator pattern** (Kubernetes lineage): operational knowledge as
  level-triggered reconcile loops, not event-reaction scripts. The ADR-0115
  boot reconcilers are the seed — without importing the platform.
- **Promise theory**: autonomous convergent agents, no inbound control
  channel — the sovereign-appliance posture stated as theory, and a
  principled argument against push-style AIOps SaaS integration.
- **Event-driven runbook automation**: trigger = alert fact, action = verb,
  both as data — the StackStorm/Rundeck shape without the platform.
- **Agentic SRE interface design**: small orthogonal idempotent tools, typed
  machine-readable errors, documentation adjacent to tools, approval gates
  only at destructive edges, full audit of agent actions. The grant-prompt,
  audit, and break-glass machinery is precisely this governance layer.
- **Classic AIOps ML** (anomaly detection, event correlation, topology
  root-cause): mostly *refused* — correlation is a join when everything is
  one table with ref-tuples
  ([ADR-0109](../adr/0109-leeway-marshall-multi-membership-ref-tuples.md)),
  baselining is a t-digest window, root-cause needs only the services graph.
  Statistical models enter when data volume forces them, not before.
- **OpenTelemetry / Prometheus**: take the instrument-kind vocabulary and
  the registry-of-named-instruments idea; refuse the SDK, collector, wire
  protocol, and pull endpoint — a second data model and a service tether
  against P1/P3. The pull surface falls out of `keelson.instruments` as SQL.
- **Synthetics**: `ws_probe` and the screenshot tours are synthetic
  monitoring already; scheduled runs against the live box close the loop.
  Fault injection is deferrable.

## Sequencing and failure modes

The critical path is items 1–3 (contract, health and topology, verbs):
detection without verbs can only page; verbs without health cannot gate; a
model without either can only advise. State lifecycle (item 6) is the
largest pure gap, and [ADR-0082](../adr/0082-imzero2-remote-session-auth-tls.md)
is the blocker for any remote operator surface.

The failure mode is P3's, restated for operations: the moment operability
ships as bespoke health JSON, prose-only runbooks, or un-audited shell
scripts beside the spine, the compounding stops and AIOps regresses to
log-parsing archaeology. The rule that keeps the above coherent: **every ops
artifact — health, alerts, instruments, runbooks, actions, drift — is leeway
facts or a keelson table, and every action is an audited subject.**

## Further reading

- [why-boxer](./why-boxer.md) — the premises (P3, P4, P6, P7) this page
  extends to operations.
- [query-observability](./query-observability.md) — the sibling page for the
  query plane; its entity-spine method is the model for the ops planes here.
- [ADR-0026](../adr/0026-app-runtime-and-capability-subjects.md) — runtime,
  capabilities, `runtime.facts`.
- [ADR-0085](../adr/0085-imzero2-demo-pull-build-atomic-deploy.md) — deploy,
  gates, break-glass; the derived ops-tooling practices.
- [ADR-0090](../adr/0090-sysmetrics-pubsub-data-plane.md) — the metrics
  plane and its deliberate non-persistence.
- [ADR-0094](../adr/0094-keelson-introspection-tables.md) — live state as
  ClickHouse-queryable tables.
- [ADR-0115](../adr/0115-query-observability-data-plane-strategy.md) —
  pipelines as schema objects; the reconciler-at-boot rule.
