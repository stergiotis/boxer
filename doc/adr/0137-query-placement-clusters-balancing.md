---
type: adr
status: proposed
date: 2026-07-21
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0137: query placement — clusters, balancing, and affinity

## Context

[ADR-0136](./0136-play-query-dispatch-resolver.md) gives play a resolver
that classifies a query and picks between two fixed endpoints. The
deployments this repository targets have more than two: replicated
ClickHouse hosts grouped into clusters, single hosts addressed directly,
plus the loopback introspection plane. Deployment-side naming conventions
encode the intended placement in the table name — for example a
`facts_h`-style table balances across a default cluster, a `_rem` suffix
names a remote cluster, and a per-host table (`facts_<host>`) lives only
on that host. None of these conventions appear in this repository; they
are site vocabulary.

Placement decisions interact with two recorded boundaries. The
introspection plane is loopback-bound and serves live in-process state
([ADR-0094](./0094-keelson-introspection-tables.md) §SD3/§SD7), and
encrypted ad-hoc datasets are decrypted in-process under a disk-only
threat model ([ADR-0134](./0134-adhoc-datasets.md) §SD2) — a persistent
server's spill files, query cache, and core dumps are outside this
repository's control. Any placement machinery must treat those as hard
walls, not defaults.

This ADR decides the placement model, the balancing rules, and the
conditions under which the previously irreducible cases (a query joining
introspection tables against server tables) become executable.

## Decision

Placement is a data-driven mapping from referenced tables to a named set
of endpoints; balancing chooses a member per run under an affinity key.

- **SD1 — Placement model.** A *placement* is a named endpoint set with
  properties: kind (cluster | host | loopback-introspection), member
  list, credential reference, and sensitivity constraints. The resolver
  pipeline becomes classify → place → balance; the
  [ADR-0136](./0136-play-query-dispatch-resolver.md) two-endpoint policy is
  the degenerate case (two singleton placements).

- **SD2 — Rules are data.** The table-name → placement mapping is
  configuration the resolver consults, not code. The suffix grammar must
  be injective (a reserved suffix vocabulary; longest match), and an
  unresolvable name falls through to the default placement — never a
  guess. The placement map is served as an introspection table
  (`keelson('placements')`), in the
  [ADR-0126](./0126-appliance-topology-as-data.md) topology-as-data
  manner, so routing configuration is queryable with the same tooling it
  routes for.

- **SD3 — Balancing and affinity.** Within a multi-member placement the
  balancer picks a member per run. The affinity key pins one choice for a
  whole query-graph evaluation generation
  ([ADR-0097](./0097-play-reactive-query-graph.md)): panels of one
  cascade must not straddle replicas with different replication lag, or
  co-displayed results disagree with each other. Balancer state (cursor,
  failover marks) lives in the resolver implementation.

- **SD4 — Coordinator, not shards.** Where a placement is a real
  ClickHouse cluster (configured `remote_servers`, `Distributed` tables),
  the client picks a *coordinator* only; shard routing stays server-side.
  Client-side balancing earns its keep exactly where no shared cluster
  configuration exists — independent hosts, or a cluster reachable only
  from the client side.

- **SD5 — Co-located collapse, probe-gated.** A query mixing
  introspection tables and server tables is executable by a server that
  can reach the loopback table source
  ([ADR-0094](./0094-keelson-introspection-tables.md) §SD3 anticipates
  exactly this join). The endpoint URL cannot prove that reachability: an
  SSH tunnel makes a remote server answer on `127.0.0.1`, and its `url()`
  fetch would then dereference the *remote* loopback. The gate is a
  capability-style probe: the candidate server is asked to fetch a
  one-time nonce URL from the introspection server, and only an observed
  nonce proves the path. Under a tunnel the probe fails (refused or 404)
  and the collapse is simply not offered — it fails safe. Only after a
  successful probe does the resolver expand `keelson()` to `url()`
  client-side for that server. Encrypted ad-hoc datasets are excluded
  from collapse unconditionally (§Context walls); they stay on the
  loopback `/query` path.

- **SD6 — Mutating statements require a cardinality-1 placement.** DDL
  and UDF installation route only to singleton placements (a directly
  addressed host; the local server). Against a multi-member placement
  they are refused with a message — never balanced onto one member, which
  would leave replicas silently divergent. Fan-out installation across a
  cluster is **deferred**; the accepted consequence is that a UDF
  installed locally makes cluster-placed queries that use it fail with
  ClickHouse's ordinary unknown-function error, which is loud and
  attributable.

- **SD7 — Credentials per placement.** Each placement carries a
  credential reference (user, TLS material) resolved outside the SQL
  path; secrets appear in neither queries nor logs. Non-loopback
  transport security remains
  [ADR-0082](./0082-imzero2-remote-session-auth-tls.md)'s design to
  inherit, not re-derived here.

- **SD8 — Cache and pin identity.** Result caches, lane memos, and pins
  key on the *logical placement*, not the balanced concrete host —
  balancing must not fragment them. The stated cost: within a placement,
  replicas may serve marginally different states under the same key
  (replication lag inside the affinity window). That is a property of
  choosing balancing, recorded here rather than hidden.

- **SD9 — Failover.** An alternate member is tried only when the chosen
  member fails to *connect* — never after response bytes have flowed
  (a mid-stream retry against a lagging replica is a correctness hazard).
  Failover is reported in the decision's reason string. Health *probing*
  (marking members bad ahead of use) is deferred.

- **SD10 — Cancel addressing.** `KILL QUERY` addresses a run by
  `query_id` on the placement member that executes it; the placement map
  is what tells the cancel path where to go. The transport that carries
  cancel is [ADR-0138](./0138-streaming-query-transport-observability.md)'s
  concern.

Open, not decided here: how the editor's schema/autocomplete universe
follows placement (per-placement schema fetch, or an aggregate view).
Today it reflects the sticky endpoint; under placements that is sometimes
wrong in both directions.

## Alternatives

- **Suffix conventions hardcoded in the classifier.** Rejected: the
  vocabulary is site-specific and would rot in code; as data it is
  swappable per deployment and introspectable (SD2).
- **Locality by URL inspection.** Hostname/IP parsing to detect "local"
  servers. Rejected: undecidable under tunnels and port-forwards; the
  probe (SD5) proves the actual property (reachability of the loopback
  source) instead of inferring it from a string.
- **Balancing mutating statements to one member.** Rejected: silently
  divergent replicas; the failure surfaces later, far from the cause, as
  intermittently missing functions/tables (SD6's refusal keeps the
  failure at the point of intent).
- **Client-side shard routing.** Teaching the resolver which shard holds
  which data. Rejected: duplicates what `Distributed`/`cluster()` already
  own server-side; the client stops at coordinator choice (SD4).
- **Session-level state under balancing.** Allowing session `SET` / temp
  tables and pinning sessions to make them work. Rejected: play's
  parameter channel is per-request by design
  ([ADR-0133](./0133-chhttp-server-dialect-and-param-binding.md)); session
  state would couple correctness to affinity lifetime. Statements that
  require session state are refused under multi-member placements.

## Consequences

### Positive

- Site naming conventions become executable routing without entering the
  repository; the same map drives dispatch, cancel addressing, and (as
  data) documentation of the deployment.
- The mixed introspection/server query — previously unroutable — becomes
  executable exactly where it is provably safe, and nowhere else.
- Monotonic-read hazards of balancing are contained by construction
  (affinity per evaluation generation) rather than by user care.

### Negative

- A placement map is one more piece of deployment configuration to keep
  truthful; a stale member list degrades to failover noise.
- The probe adds a moving part to the collapse path (nonce endpoint,
  probe query, observation check).
- Deferred fan-out (SD6) means clusters cannot receive UDFs/DDL through
  play at all for now.

### Neutral

- Balancing is only as good as the placement data; this ADR deliberately
  does not add health probing, quotas, or drain semantics.
- The affinity window trades read monotonicity against load spread; the
  generation granularity is a tunable, not a law.

## Status

Proposed — awaiting review. Depends on
[ADR-0136](./0136-play-query-dispatch-resolver.md) (the seam it fills).

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0136](./0136-play-query-dispatch-resolver.md) — the resolver seam this fills
- [ADR-0138](./0138-streaming-query-transport-observability.md) — transport, run identity, cancel carriage
- [ADR-0094](./0094-keelson-introspection-tables.md) — loopback table source; the join SD5 makes reachable
- [ADR-0134](./0134-adhoc-datasets.md) — encrypted datasets excluded from collapse
- [ADR-0097](./0097-play-reactive-query-graph.md) — evaluation generations (affinity unit)
- [ADR-0126](./0126-appliance-topology-as-data.md) — configuration served as queryable data
- [ADR-0082](./0082-imzero2-remote-session-auth-tls.md) — non-loopback auth/TLS to inherit
