---
type: explanation
audience: maintainer, downstream system builder
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Query systems over the boxer toolbelt — requirements and extension points

Boxer is a toolbelt, not a query-federation product. Yet the systems
people assemble from it — play against external servers, the loopback
introspection plane, broker-spawned `clickhouse-local`, encrypted ad-hoc
datasets — keep running into the same set of dispatch, placement,
transport, and observability problems. A design exploration of those
problems was first written up as three proposed ADRs (0136–0138,
introduced in commit `432f2e29` and removed again in favour of this
document; retrievable from git history). That framing was wrong, because
most of what they specified —
naming conventions, balancing strategy, poller topology — is *system*
policy that depends on a concrete deployment, not a decision boxer should
record about itself. This document keeps what the exploration actually
established: the requirements such systems exhibit, and the small,
policy-free extension points boxer must offer so they can be built. When
an extension point is implemented, it gets its own narrow ADR; this
document is the map, not a decision.

## Background: the engine landscape

A query system assembled from boxer spans engines with materially
different properties:

- **External ClickHouse servers**, singly or in replicated clusters,
  reached over HTTP; versions and settings are not boxer's to pin.
- **Broker-spawned `clickhouse-local` workers**
  ([ADR-0028](../adr/0028-chlocal-low-latency-sql-cap.md)): one-shot
  processes with no listener; their system tables die with them.
- **The loopback introspection plane**
  ([ADR-0094](../adr/0094-keelson-introspection-tables.md)): live
  in-process state served over a loopback-bound HTTP table source;
  deliberately unreachable from off-machine.
- **Encrypted ad-hoc datasets**
  ([ADR-0134](../adr/0134-adhoc-datasets.md)): decrypted in-process under
  a disk-only threat model; a persistent server's spill files, query
  cache, and core dumps are outside boxer's control.

The central finding of the exploration: **dispatch is a placement
problem, not a syntax problem.** The `keelson()` macro pass owns its
syntax completely, but every expansion pins a query to an execution
locality; which engine executes is decided before any rewrite is
meaningful, and today that decision lives in the user's head.

## Requirements observed

Stated as properties a query system needs; each carries the concrete
finding that motivated it.

- **R1 — Logical naming.** Users address logical tables; the mapping to
  engines, clusters, or hosts is site data (deployment naming
  conventions appear nowhere in this repository, and must not).
- **R2 — Hard locality walls.** Some placements are constraints, not
  preferences: live in-process state exists only in its process;
  encrypted datasets never leave loopback. A router must be unable to
  override these.
- **R3 — Locality is proven, never inferred.** A `127.0.0.1` endpoint
  string does not establish machine-locality — an SSH tunnel makes a
  remote server answer locally, and its `url()` fetches then dereference
  the *remote* loopback. Reachability of the loopback plane must be
  established by demonstration (a nonce the engine can only present by
  actually fetching it), which fails safe under tunnels.
- **R4 — Read consistency per evaluation.** Reactive query graphs
  ([ADR-0097](../adr/0097-play-reactive-query-graph.md)) re-run dependent
  queries; one evaluation generation must not straddle replicas with
  different replication lag, or co-displayed panels disagree. Affinity
  is per generation, and member choice should be a deterministic
  function of (placement, generation) so divergence is impossible rather
  than merely discouraged.
- **R5 — Mutation has different placement semantics.** DDL and UDF
  installation address *all* members of a placement or a single
  deliberately-chosen host — never "whichever member the balancer
  picked", which leaves replicas silently divergent. Detection must be
  default-deny: a statement is read-only only when its kind provably is.
- **R6 — Sensitivity constrains placement and transport independently.**
  Sync-vs-async is a latency choice; what data may reach which engine,
  and what may transit which channel, are security choices. A dispatch
  label needs both axes so "async" never implies "less protected".
- **R7 — One run identity end to end.** The ClickHouse `query_id`,
  minted by the client, is the join key across live progress, terminal
  `query_log` facts, results/pins, cancellation, and the dispatch
  decision itself. Play already mints stable per-lane ids for exactly
  this reason.
- **R8 — Issuer-decoupled observability.** Inflight progress must be
  observable by parties other than the connection holder (a second
  window, ops tooling). In-band progress headers cannot provide this;
  polling `system.processes` can — with the recorded caveats that
  sub-tick queries never appear (absence of signal is not signal) and
  that a run *vanishing* is ambiguous (done, killed, or failed), so
  terminal truth comes only from the result path or `query_log`.
- **R9 — Completeness honesty.** A result capped by
  `max_result_rows`/overflow, a stream that died, and a complete result
  are three distinguishable outcomes; a consumer must be unable to
  mistake one for another. No terminal signal means *incomplete*.
- **R10 — Heterogeneous engine capabilities.** Engines differ in
  observability source (a server has `system.processes`; a one-shot
  worker has only its in-band stream), session semantics, and version
  behavior. Placement data must be able to say what an engine class
  supports.
- **R11 — Cancellation by identity.** `KILL QUERY` addressed by run id
  on the executing member; observation and control share addressing.
- **R12 — Decisions are auditable data.** Which placement, which member,
  which transport, and why — recorded on the run's durable record and
  queryable afterwards, in the
  [ADR-0126](../adr/0126-appliance-topology-as-data.md) manner.

## Extension points derived

Each is small, policy-free, and independently adoptable. *Exists* points
at machinery already in the repository; *delta* names what is missing.

- **E1 — SQL fact extraction.** Pure functions over a parsed statement:
  referenced tables including table-function macros, statement kind
  (read / mutating / unknown), parameter references. Total and
  best-effort: never errors on strange SQL, returns *unknown* instead.
  Boxer states facts about SQL; it attaches no routing meaning to them.
  *Exists:* `analysis.ExtractTables`/`ExtractColumns`, the
  [ADR-0117](../adr/0117-passthrough-table-classifier.md) classifier, the
  `keelson()` reference predicate (unexported). *Delta:* export the
  macro-reference helper; add statement-kind classification.
- **E2 — Dispatch seam.** The query issuer consults a resolver once per
  run with the finalized outgoing SQL and an affinity token; the
  returned decision (executor + human-readable reason) rides that
  request only. Every issuer sharing the same finalized SQL — including
  diagnostic probes — must consume the same decision, or probe and run
  diverge. Boxer ships the seam and a default resolver covering its own
  two endpoints (external base, loopback introspection); systems replace
  the resolver. *Exists:* the single `buildResidual` choke point;
  the manual endpoint switcher. *Delta:* the interface, the threading,
  the default resolver.
- **E3 — Result frame contract.** A run's result is a sequence of typed,
  sequenced frames: data, progress, and exactly one terminal frame —
  complete, truncated (with reason), or failed (with error). Consumers
  render a stream without a terminal frame as incomplete. The
  synchronous HTTP binding is the degenerate stream (data then
  terminal). One subscriber library owns these invariants; panels do not
  re-implement them. *Exists:* nothing formal; the `/table` decrypt path
  already practices abort-on-truncation. *Delta:* the types, the
  library, the sync adapter.
- **E4 — Run identity discipline.** Client-minted `query_id` with a
  documented uniqueness scope, stamped on execution and carried by
  frames, facts, and pins. *Exists:* play's per-lane minting and the
  `queryrunfacts` join. *Delta:* promote from convention to documented
  contract consumed by E3/E7.
- **E5 — Introspection provider seam.** Systems publish their placement
  maps, cluster rosters, and routing decisions as ordinary introspection
  tables via the existing `TableProvider` registry; boxer provides the
  mechanism and catalog, never the schema of site data. *Exists:*
  [ADR-0094](../adr/0094-keelson-introspection-tables.md), fully.
  *Delta:* none — this is the pattern to reuse.
- **E6 — Reachability probe primitive.** The introspection server mints
  a single-use, expiring nonce URL and answers whether it was fetched.
  Semantics: a successful check proves the probed engine could reach
  this process's loopback plane at that moment — nothing more. Proof
  caching, re-probe cadence, and what to do with the proof are the
  caller's policy. *Exists:* nothing. *Delta:* the endpoint and check
  API (small).
- **E7 — Progress observation component.** A poller bound to one server
  polls `system.processes` once per tick for all registered run ids and
  publishes progress frames on per-run bus subjects. Guarantees: ticks
  only (it never synthesizes terminal states, per R8), self-excluding,
  staleness bounded by the tick. *Exists:* the pattern
  ([ADR-0090](../adr/0090-sysmetrics-pubsub-data-plane.md)'s single
  scraper; `sysmetricsbus.LatestHolder` on the consumer side) and the
  in-band capture that remains the one-shot workers' only witness.
  *Delta:* the poller itself.
- **E8 — Streaming reply channel.** The bus broker's request/reply
  gains an ordered, chunked, backpressured reply stream with an explicit
  end-of-stream or error marker; retention for late joiners is bounded
  and caller-configured. This is the only wire-level novelty in the
  whole catalog. *Exists:* one-shot replies
  ([ADR-0028](../adr/0028-chlocal-low-latency-sql-cap.md));
  [ADR-0089](../adr/0089-rowdml-serialization-clickhouse-native-ingestion.md)'s
  rule that result wire format stays separate from ingestion wire format.
  *Delta:* the streaming reply primitive.
- **E9 — Dispatch label vocabulary.** The two-axis label of R6
  (sensitivity; execution mode) as shared types, so classifiers,
  resolvers, and transports compose without re-encoding each other's
  meanings. Boxer enforces only the walls it owns (R2); everything else
  is carried, not judged. *Exists:* the walls, in
  [ADR-0094](../adr/0094-keelson-introspection-tables.md)/[ADR-0134](../adr/0134-adhoc-datasets.md)
  enforcement. *Delta:* the label types.

## Deliberately out of scope for boxer

The exploration produced concrete designs for all of the following;
they are recorded in the removed ADRs (git history, `432f2e29`) and
belong to system builders, not to this repository: table-suffix grammars and any placement rule
content; balancer strategy and health probing; cluster membership
management; fan-out installation of UDFs/DDL; retention windows and
replay policy beyond E8's bounded mechanism; live *partial-aggregate*
display (which requires plan-level query decomposition and was
explicitly deferred even in the exploration).

## Status and process

The three source ADRs are removed from the tree (the ADR-0120 withdrawal
pattern: a forward commit carries the why; history keeps the record, and
the numbers 0136–0138 stay burned). Boxer work proceeds extension point
by extension point, smallest first (E1,
E2, E6 are each a sitting; E3 and E4 are contracts plus adapters; E7
builds on E3/E4; E8 is the one substantial engine change). Each lands
with its own narrow ADR when its shape is decided in code, and this
document's *Exists/Delta* notes should be updated as they land.
