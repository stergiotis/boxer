---
type: adr
status: accepted
date: 2026-07-17
reviewed-by: "@spx"
reviewed-date: 2026-07-17
---

# ADR-0126: Appliance topology as data — the node/edge vocabulary and its planes

## Context

[aiops-operability](../explanation/aiops-operability.md) names topology-as-data
as absent: the appliance's *declared* topology partially exists as data
(manifest `Caps` in `keelson.apps` are the intended bus graph; unit files in
[showcase/onbox](../../showcase/onbox/ONBOX.md) carry ordering and dependency
edges, but only in git), while the *observed* side — which processes belong to
which component, listening sockets, ClickHouse, NATS, and the TLS front as
nodes — exists nowhere as data, and no node/edge vocabulary joins the two.
Every later diagnosis join ("what is running that should not be", "which
component owns the process behind this socket") builds on that vocabulary, and
the desired-versus-observed reconciliation the operability page turns on *is*
the join between the halves.

The seams this decision builds on:

- The sysmetrics scraper ([ADR-0090](./0090-sysmetrics-pubsub-data-plane.md))
  is the relocated machine-state trust boundary — the sole `/proc` reader,
  publishing per-domain snapshots to `sysmetrics.{host}.{domain}`.
- The introspection registry ([ADR-0094](./0094-keelson-introspection-tables.md))
  turns in-process state into ClickHouse-queryable `keelson.*` tables; the GUI
  host already consumes the metric plane (`sysmetricsbus` consumer + bridge),
  so a plane-fed `Live` provider is an established shape.
- `keelson.apps` already serves the declared bus graph (manifest subject
  filters with direction and reason); the extbin registry
  ([ADR-0118](./0118-extbin-external-process-chokepoint.md)) is the precedent
  for a compiled-in declared inventory served as a table.
- systemd already labels every process it starts through injected environment
  (`INVOCATION_ID`) and maintains `/proc/[pid]/cgroup`; supervisor-injected
  environment is an established process-identity channel.

## Design space (QOC)

**Question.** How does the appliance's topology — declared and observed nodes
and edges — become one queryable surface with a vocabulary that supports the
desired-versus-observed join?

**Options.** **O1 (chosen)** components self-identify through a
supervisor-injected mark; the scraper collects marks and sockets as plane
data; the GUI host serves typed `keelson.*` tables plus a narrow node/edge
projection carrying an `origin` (declared/observed) column. **O2** in-process
collection: the GUI host's providers read machine state directly. **O3** a
standalone topology daemon with its own subject family. **O4** an on-demand
CLI snapshot tool (structs→Arrow, no plane). **O5** build the R1 declarative
desired-state store first and derive topology from it.

**Criteria.** C1 GUI capability hygiene (the carrier must not regain
machine-state reads); C2 one vocabulary for both halves — drift is a join; C3
reuse (plane, codec, provider registry); C4 liveness (GUI panels and SQL see
current state); C5 cost and new dependencies; C6 supervisor independence
(dev shell, systemd, container all attributable).

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | −− | ++ | +  |
| C2 | ++ | +  | +  | +  |
| C3 | ++ | +  | −  | −− |
| C4 | ++ | +  | +  | −− |
| C5 | +  | +  | −− | +  |
| C6 | ++ | −− | +  | −  |

O2 reverts ADR-0090: the carrier regains `READ_SYSTEM_STATE` to observe
processes and sockets. O3 stands up a second machine-state trust boundary and
subject family beside a scraper whose capability envelope already covers the
reads. O4 is never live — a diagnosis surface that must be re-run is a report,
not a plane. O5 gates the light cut on the heaviest undecided piece (the whole
desired-state store) — exactly the descope-over-gate failure. O1's cost is a
small marking contract every launch path must honour (SD2).

## Decision

Adopt **O1**. Boxer components carry an explicit identity mark; the scraper
observes marks and sockets and publishes them on the existing metric plane;
the GUI host serves the typed tables and a deliberately narrow graph
projection whose row vocabulary — node kinds, edge kinds, key format,
`origin` — is the contract every later diagnosis and detection query builds
on. Interrogating the supervisor (systemd via D-Bus) is *deferred*, not
designed in: v1's topology is what the box itself declares and exhibits.

### SD1 — Node identity: one key format shared by both halves

A node is identified by a `kind:name` string key, stable across the
declared/observed divide so the same thing lands on the same key:

| kind | key example | named by |
|------|-------------|----------|
| `host` | `host:demo-box-1` | the ADR-0090 §SD1 node id |
| `component` | `component:imzero2-demo` | the component registry token (SD2), verbatim in the mark |
| `proc` | `proc:4711` | live pid (durable identity is the row's `(host, pid, started_at_unix_ms)` columns, since pids recycle) |
| `sock` | `sock:tcp/127.0.0.1:8123` | protocol + bound address + port |
| `app`  | `app:imztop` | manifest app id (`keelson.apps`) |
| `subject` | `subject:sysmetrics.>` | bus subject filter pattern, verbatim |

Edge kinds, each directed: `proc-in-component` (observed, mark);
`proc-child-of` (ppid); `proc-listens` (fd↔socket-inode); `app-in-component`
(the carrier stamps its own mark onto its app rows); `app-pub`, `app-sub`
(manifest caps, app→subject); `component-needs` (declared, registry
dependencies). External services need no special node kind: ClickHouse, NATS,
and the TLS front are components (their unit files carry the mark) with
listening sockets. A `unit` kind is reserved for the deferred supervisor
collector (SD6); until then the raw cgroup string rides as a proc attribute,
not a node.

Every node and edge row carries `origin` — `declared` (intent: a registry
entry, a manifest cap) or `observed` (running state) — and `source` (which
mechanism reported it: `mark`, `proc`, `manifest`, `registry`). Drift is then
a single-table `GROUP BY key` comparing origins, not a cross-store join.

### SD2 — The marking contract and the component registry

**The mark.** Every deliberately-run boxer process carries
`BOXER_COMPONENT=<token>` in its environment, injected by whatever launched
it: an `Environment=` line in the unit files (including the ClickHouse, NATS,
and Caddy units the kit owns), the dev launcher scripts, a container env
entry. The value is a registry token, not a uuid — self-describing, and
instance identity is already `(host, pid, started_at)`; systemd's
`INVOCATION_ID` exists where a per-start uuid is ever needed. Because
environment is inherited, children — including every extbin-spawned tool —
attribute to their component for free unless a spawner scrubs `Env` (a
documented hazard; a helper/lint can guard the repo's spawn sites). The
carrier reads its own mark to stamp `app-in-component` edges. The variable
registers in the env registry ([ADR-0009](./0009-environment-variable-registry.md))
like any other.

Two limits, accepted: the mark is **cooperative** — any process can claim one,
so it is operability identity, not a security boundary (uid and the cgroup
attribute corroborate); and environment is **exec-frozen** — `/proc/[pid]/environ`
is the exec-time image, so a bare un-launched dev run shows no mark
(`os.Setenv` cannot retrofit one; keelson processes still self-identify
through the existing runtime-run facts, which carry pid).

**The registry.** A data-only package (`keelson/runtime/topo`) holds the
compiled-in component inventory in the extbin mold: token, short role,
declared dependencies (`component-needs` edges — boxer's own intent, not
parsed from unit files). The registry is the v1 declared half for components;
manifests remain it for apps and subjects.

### SD3 — Observed collectors: two `/proc` reads, no new dependency

- **marks + cgroup** — `ProcInfo` gains additive `Component` (from
  `/proc/[pid]/environ`, read once per `(pid, starttime)` and cached — the
  image is immutable) and `CgroupUnit` (from `/proc/[pid]/cgroup`, the nearest
  ancestor path element with a unit suffix; kernel-maintained corroboration
  the mark cannot fake). The environ read needs the same ptrace-class
  privilege as the fd walk below; where denied, the field is empty.
- **`sockets`** — a new collector: listening sockets from
  `/proc/net/{tcp,tcp6,udp,udp6,unix}`, attributed to pids via the
  `/proc/[pid]/fd` socket-inode walk. Where fd links are unreadable the row is
  published with pid unset (partial over absent).

Both stay inside the scraper's existing capability envelope — pure `/proc`,
no exec, no new socket connects.

### SD4 — Plane placement: one new domain

`sockets` becomes a `sysmsnap.Domain` token riding
`sysmetrics.{host}.{domain}` with the standard per-domain error rows, sampled
on its own slower default cadence (order 15 s vs 1 s) — cadence stays
scraper-owned (ADR-0090 §SD5), now per-domain. The mark and cgroup fields ride
the existing `proc` domain. Cmdlines, marks, and socket addresses follow the
§SD8 sensitivity posture unchanged: exposed in v1 under the single-tenant
loopback assumption, maskable later via membership tags at one policy point.
Accepting this ADR appends the token to the §SD1 taxonomy via a dated update
there.

### SD5 — Typed tables and the graph projection

The GUI host registers `Live` providers fed by a host-level plane consumer
(the imztop consumer pattern, but host-scoped — imztop's is mount-gated; rows
empty plus an error column when no scraper is publishing):

- **`keelson.procs`** — deferred in ADR-0094 §SD8 v2, landed here because the
  proc↔component join needs it; carries `component` and `cgroup_unit`.
- **`keelson.sockets`** — the listener rows.
- **`keelson.components`** — the registry (`Static`): token, role, declared
  dependencies.
- **`keelson.topology_nodes` / `keelson.topology_edges`** — the vocabulary
  made executable: `keelson/runtime/topo` holds the kind enumerations, key
  formatting, and pure assembly functions from the snapshot types, manifests,
  and registry; the providers serve the assembled graph. Rows are deliberately
  narrow — `kind`, `key`, `host`, `origin`, `source`, plus
  `src_key`/`dst_key`/`edge_kind` on edges — detail lives in the typed tables,
  reachable by key.

A canonical-queries howto (drift anti-joins, socket-owner walk, component
dependency closure) documents the intended use; detection rules that
*evaluate* those queries are out of scope (SD6).

### SD6 — Non-goals and deferrals, recorded

- **Supervisor interrogation (the D-Bus services collector).** Unit
  load/active/sub states, enablement, restart counts, and systemd dependency
  edges would need boxer's first D-Bus dependency plus per-unit property
  chatter — for verdicts ("failed", "activating") that are *health*
  territory. Deferred to the operability-contract/health work (or a dated
  update here when demanded). Until then v1 cannot distinguish
  failed-with-no-process from absent — drift says "component absent", and
  journald remains the human escalation path. The `unit:` node kind and the
  `services` domain token are reserved for that collector.
- **Health verdicts** (`keelson.health`, probes) — the operability-contract
  ADR's half of the pair; topology does not gate on it.
- **Drift detection and alert facts** — the detection layer (operability item
  5) evaluates the joins this ADR makes expressible; nothing here writes facts.
- **Persistence/history** — the ADR-0090 P5 tee covers topology domains the
  day it is built; nothing stored meanwhile, so "what was true at 03:12"
  stays unanswerable here.
- **The appliance desired-state manifest** (R1) — which components *should*
  exist on which box is today ansible's knowledge in git; v1's declared half
  is what the binaries themselves compile in (registry, manifests). The R1
  store, when built, becomes another `origin=declared` source for the same
  keys.
- **NATS server internals, multi-box edges** — the bus stays a blind node
  observed only as component + socket; `{host}` scopes rows but no cross-host
  edge kind is defined yet.

### SD7 — Phasing

Independently shippable: **P1** this ADR; **P2** the marking contract —
`BOXER_COMPONENT` env registration, the `topo` registry, `Environment=` lines
in the kit units and launcher scripts; **P3** collectors + the `sockets`
domain + per-domain cadence (scraper side, includes the `ProcInfo` fields);
**P4** the typed providers; **P5** graph assembly + providers + the
canonical-queries howto; **P6** dated updates to ADR-0090 (§SD1 token,
per-domain cadence) and ADR-0094 (table catalogue), and the operability
page's state column.

## Alternatives

- **O2 — in-process collection in the GUI host.** The carrier regains
  `READ_SYSTEM_STATE`/`FILES` — reverts the ADR-0090 relocation and re-opens
  the §SD10 bypass class it closed.
- **O3 — a standalone topology daemon.** A second trust boundary, subject
  family, unit, and sandbox for reads the scraper's envelope already covers;
  pays O1's costs plus a service without removing any.
- **O4 — on-demand snapshot tool.** Not live: panels and detection would poll
  a tool exec; no plane, no `{host}` fan-in, bespoke transport.
- **O5 — desired-state store first.** Gates the observable half on the
  largest undecided design (R1); the store slots in later as a declared-row
  source without vocabulary changes.
- **D-Bus-first observation (this draft's own prior shape).** systemd as the
  identity authority: `ListUnits`/`ListUnitFiles` plus glob-gated per-unit
  properties. Rejected for v1: boxer's first D-Bus dependency and a per-unit
  property protocol, to obtain unit-state *verdicts* that belong to health —
  while making topology supervisor-shaped (a dev-shell run has no unit, so
  the vocabulary would not cover it). Kept as the deferred source for the
  reserved `unit:` kind (SD6) — the states an incident responder needs
  (`failed`, `activating`) genuinely cannot come from marks, because absent
  things carry no marks.
- **Argv marker instead of environment.** Not inherited by children, and
  third-party binaries (extbin tools) reject unknown flags — so exactly the
  processes that need external attribution cannot carry it.
- **Thread names (`PR_SET_NAME`) as the mark.** 15-char comm, Go runtime
  threads are not individually nameable in practice, and topology needs
  process-level identity — threads share environ anyway.
- **Relying on systemd's `INVOCATION_ID`.** Already-injected uuid, but
  resolving it to a unit needs supervisor interrogation (the dependency this
  option exists to avoid), the value is opaque where the mark is
  self-describing, and it does not exist off systemd.
- **Self-marking after exec (re-exec with env, argv rewriting, an abstract
  unix-socket rendezvous per process).** Each makes a process's external
  identity depend on surprising runtime behaviour (re-exec, unsafe argv
  memory writes, a live socket per process) to cover the one case —
  un-launched dev runs — that the runtime-run facts already identify.
- **A stored generic graph instead of typed tables (or typed only, graph as
  prose).** Only-generic makes every attribute stringly; only-typed leaves the
  vocabulary as convention no query enforces. Both layers, each narrow, keep
  detail typed and the join contract executable.
- **A new `systopo.*` subject family.** A parallel plane with the same
  producer, transport, and consumers as `sysmetrics.*`; one more domain token
  is cheaper than a second taxonomy.

## Consequences

**Positive.** The reconciliation loop gets its substrate with **no new
dependency**: drift between declared and observed is one `GROUP BY` over
`origin`, and the process↔component↔socket↔app walk is a join, not
archaeology. Identity is supervisor-independent — the same vocabulary covers a
systemd unit, a container, and a launcher-started dev run. The carrier's
capability surface is unchanged; extbin children attribute to their component
via inheritance. Every later layer — the D-Bus collector, detection, health,
the R1 store — lands as new rows in an existing vocabulary.

**Negative.** The mark is cooperative (not a security boundary) and
exec-frozen (un-launched processes show none; a spawner that scrubs `Env`
silently drops attribution — helper/lint guard). Failed-with-no-process and
absent are indistinguishable until the deferred supervisor collector.
Environ and fd reads need the scraper to hold ptrace-class privilege over
observed processes; rows degrade (mark empty, pid unset) where denied. The
node/edge vocabulary, one domain token, and the table schemas become
public-stability surfaces.

**Neutral.** The `cgroup_unit` attribute corroborates marks under systemd
without being load-bearing. The registry is a compiled-in closed set — adding
a component is a code change, which is the point (declared means declared).
`keelson.build` may need a `pid` column to close the app→carrier-process
join; additive if so. imztop or a dedicated panel can render the graph later;
play reaches all five tables immediately via `keelson('…')`.

## Status

Accepted on 2026-07-17 by @spx — with the SD6 blind spot (failed-with-no-process
indistinguishable from absent until the deferred supervisor collector) accepted
explicitly. Implementation begins at P2 (SD7). Supersedes nothing.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded
by ADR-XXXX)`. Post-acceptance edits follow
[DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
tiers (Tier 2 dated `## Updates`).

## Updates

### 2026-07-18 — P2–P5 implemented

The marking contract, collectors, typed tables, and graph projection are
built and merged; only the SD7 P6 documentation pass remained (this
entry). What landed, and where it deviates from the letter of the SDs:

- **SD2/SD3 as designed**, with one addition the design pass missed: the
  proc collector's `MaxProcs` cap ranks by CPU, which would silently
  evict an idle marked daemon — exactly the processes topology exists to
  see. Marked processes are now cap-exempt (at most `MaxProcs` unmarked
  rows plus every marked one), and the identity reads moved to the
  cheap phase so the exemption can act before capping; the
  once-per-instance cache keeps the added cost to first sightings.
- **SD4**: the per-domain cadence is collector-owned (the sockets
  collector caches internally between due times) rather than a scraper
  scheduler change — the bundle tick loop is untouched.
- **SD5 deviation**: the "error column" for a silent plane is not
  built. The tables carry `sampled_at`/`received_at`/`collected_at`
  staleness stamps instead, and zero rows mean no scraper has published;
  per-domain collector errors still ride the bundle's error map,
  currently unexposed as a table. Revisit if the detection layer needs
  them relationally. The `keelson.build` `pid` column the Consequences
  anticipated already existed — the app→carrier-process join works
  unchanged.
- The canonical queries in
  [doc/howto/topology-queries.md](../howto/topology-queries.md) — the
  drift `GROUP BY` both directions, the socket-owner walk in typed and
  edges-only forms, the recursive dependency closure — are each
  verified end-to-end against clickhouse-local in the engine tests, as
  are the exec-frozen environ read (a spawn-env'd test binary observes
  its own mark on live `/proc`) and listener attribution (a test-owned
  listener attributes to the test's pid).

## References

- [aiops-operability](../explanation/aiops-operability.md) — the gap map this
  ADR implements item 2 (topology half) of
- [ADR-0090](./0090-sysmetrics-pubsub-data-plane.md) — the plane, the scraper
  trust boundary, §SD1 subjects, §SD8 sensitivity
- [ADR-0094](./0094-keelson-introspection-tables.md) — provider registry,
  `keelson.*` catalogue
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — manifests,
  `Caps` subject filters, capability taxonomy
- [ADR-0118](./0118-extbin-external-process-chokepoint.md) — the compiled-in
  declared-inventory precedent; the spawn sites that inherit the mark
- [ADR-0009](./0009-environment-variable-registry.md) — where
  `BOXER_COMPONENT` registers
- [ADR-0085](./0085-imzero2-demo-pull-build-atomic-deploy.md) — the deploy
  units that carry `Environment=` marks
- Code: `observability/sysmetrics/sysmsnap`, `keelson/runtime/{sysmscrape,sysmetricsbus,introspect,vocab}`,
  `showcase/onbox/`, `showcase/ansible/`
