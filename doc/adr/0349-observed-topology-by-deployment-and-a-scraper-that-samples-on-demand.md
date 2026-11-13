---
type: adr
status: accepted
date: 2026-10-31
reviewed-by: "p@stergiotis"
reviewed-date: 2026-11-12
---

# ADR-0349: Observed topology by deployment, and a co-located scraper that samples on demand

## Context

[ADR-0090](./0090-sysmetrics-pubsub-data-plane.md) makes a standalone
scraper (`sysmetricsd`) the sole `/proc` reader and lets a host with no
NATS plane fall back to a **co-located** scraper on its in-process bus. The
fallback ticks at a fixed cadence for the life of the process, whether or
not anything reads the bundles.

[ADR-0126](./0126-appliance-topology-as-data.md) §SD5 then gives every GUI
host a process-lifetime consumer — a `sysmetricsbus.LatestHolder` started
by `introspecthost` — so that `keelson('procs')`, `keelson('sockets')` and
the observed half of `keelson('topology_nodes')` / `keelson('topology_edges')`
are always current. That consumer was designed for the showcase box: a
systemd-supervised machine whose units carry the `BOXER_COMPONENT` mark
(ADR-0126 §SD2) for tokens in `topo.Registry()`. There, observed rows join
declared ones and drift is a `GROUP BY`.

[ARCHITECTURE](../ARCHITECTURE.md) §2 lists the places the same host runs
that are not that box, and on most of them the observed half has nothing to
join to:

- **A desktop seat or a development run.** No supervisor injects a mark, so
  every observed process is unattributed; the processes on the machine are
  the person's whole session, not a component inventory.
- **A downstream host.** Its components are not in the registry unless it
  calls `topo.Declare`, and its launch paths carry no mark unless its
  deployment adds one.
- **The gokrazy images (ADR-0206).** The image is the declaration, fixed at
  build time and supervised by gokrazy; the observed half restates it.
- **The browser (ADR-0077).** There is no `/proc`.

The cost lands in the host process regardless of mode. On 2026-10-31, a
desktop run of a downstream host (`imzero2 demo`, no NATS URL, no mark, 669
processes on the machine) spent 1.28 s of its 3.08 s CPU over a 20 s CPU
profile in the proc collector's per-process walk, against 0.71 s for
rendering every open window. The holder was the only subscriber; nothing
queried the tables it fed.

## Decision

We will make the observed half of the topology a property of the
**deployment**, and make the co-located scraper sample only while something
on the bus wants its bundles.

### SD1 — A host observes topology when its deployment declares one

The host starts the topology holder only when the deployment gives evidence
of a topology to observe: the process carries a component mark
(`topo.Self()` non-empty), or a metric plane is bridged from NATS
(`IMZERO2_SYSMETRICS_NATS_URL` set). Both are deliberate acts of whoever
deployed the host; neither is the default of a run from a shell. No new
switch is added — a person who wants the observed tables on a development
run sets the mark.

### SD2 — The tables stay registered; their observed rows stay empty

`procs`, `sockets`, `topology_nodes` and `topology_edges` are registered on
every host as before. Without the holder, `procs` and `sockets` answer zero
rows — the state ADR-0126's 2026-07-18 Update already defines as "no
scraper has published" — and the graph projection carries its declared rows
(manifests, registry, this process's own mark) with no observed ones. A
query or sqlapplet book written against the tables still runs; on an
unobserved host it reads the declared inventory alone.

### SD3 — The co-located producer samples on demand

The co-located scraper's producer is given a demand probe: a host-local
predicate that reports whether any subscription on the in-process bus
matches the bundle subject. Each tick consults it first:

- **No demand:** the tick samples nothing.
- **Demand after a gap:** the tick samples once to prime the collectors'
  previous-tick state and publishes nothing. The collectors compute rates
  as a delta over elapsed time, so the first sample after a pause would
  publish the average over the whole pause as though it were current. The
  next tick publishes.
- **Continuous demand:** sample and publish, as before.

A consumer that subscribes therefore sees its first bundle within two
intervals. With SD1's holder running, demand never lapses and the producer
behaves exactly as before.

The probe reads the bus's own subscription table, which the host owns
alongside both ends. No consumer sends anything toward the producer, so
ADR-0090 §SD2's "strictly one way, no backchannel" holds: what the producer
learns is that a subscription exists, not anything a consumer said.

### SD4 — Scope: the co-located scraper only

`sysmetricsd` keeps its fixed cadence. Its consumers are on NATS, where
interest is not visible to the publisher without a protocol of its own, and
a deployment that runs it has chosen to pay for the plane. The per-domain
split — cheap host-wide domains always on, the per-process walk on demand —
is deferred: gating the whole bundle removes the cost this ADR is about,
and the split changes the bundle's shape for every consumer.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `sysmetricsbus.ProducerOptions` | added `Demand func() bool`; nil keeps the fixed cadence | — |
| `sysmscrape` | added `StartScraperWithDemand`; `StartScraper` is it with a nil probe | `hostboot` passes the probe; `sysmetricsd` unchanged |
| `inprocbus.Inst` | added `HasSubscriber(subject)` | — |
| `introspecthost.Deps` | added `ObserveTopology bool`; the holder starts only when set | `hostboot` sets it per SD1 |
| `sysmetricsbus.LatestHolder` | `Hosts` and `Close` accept a nil receiver | the topology providers read through a nil holder |
| `keelson('procs')`, `keelson('sockets')` | empty on a host without SD1's evidence | — |

## Alternatives

- **Unregister the topology tables on an unobserved host.** It makes the
  absence explicit, but every query and book that names the tables fails
  instead of reading the declared half, and the graph projection would lose
  its declared rows along with the observed ones.
- **Fetch on read: the providers sample when a query arrives and the
  snapshot is stale.** It keeps the observed tables live everywhere at the
  cost of a query, but it needs a request path from a consumer to the
  sampler — the backchannel ADR-0090 §SD2 rejects — and it answers the
  wrong question on a desktop, where the rows have nothing to join.
- **A new environment switch for topology.** The mark already is that
  switch, and it carries meaning a boolean would not.
- **Lower the co-located cadence.** It divides the cost by a constant and
  keeps paying it on hosts where no one reads the result.

## Consequences

### Positive

- A host run from a shell, an appliance image, or a downstream host without
  a declared topology does no per-process walk until an app that reads
  metrics is open.
- The showcase box is unchanged: its units carry the mark, so the holder
  runs and the producer never idles.

### Negative

- An app that subscribes waits up to two intervals for its first bundle,
  against one before.
- `keelson('procs')` on a development run is empty unless the run carries a
  mark. A person reading it there sees no rows, and only the staleness
  columns' absence says why.
- The producer gains a dependency on a host-supplied predicate; a probe
  that always answers true silently restores the old cost.

### Neutral

- A catch-all subscriber (`>`) on the in-process bus counts as demand.
  None exists in the host at this writing; one added for debugging would
  keep the scraper running while it is subscribed.

## Migration — Tier 1

Nothing to migrate: every API change is additive, and `StartScraper` keeps
its signature and behaviour. A deployment that relied on observed rows
without carrying a mark sets `BOXER_COMPONENT` in its launch environment.

## Verification plan — Tier 1

- **Lane.** Default `go test`: the producer's demand states (idle, prime,
  publish) against a fake sampler; `inprocbus.HasSubscriber` against
  wildcard and literal patterns; the topology providers through a nil
  holder.
- **What would fail.** A producer that publishes the first sample after a
  gap, samples while idle, or never resumes; a provider that panics on a
  nil holder.
- **Gap.** No test asserts which hosts start the holder; SD1's condition is
  two lines in `hostboot`, and a CPU profile of an idle host is the check.

## Status

Accepted 2026-11-12.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0090](./0090-sysmetrics-pubsub-data-plane.md) — the metric plane, its
  one-way rule (§SD2) and the co-located fallback this ADR gates
- [ADR-0126](./0126-appliance-topology-as-data.md) — topology as data; §SD5's
  host-level consumer, narrowed here
- [ADR-0094](./0094-keelson-introspection-tables.md) — the introspection
  tables
- [ARCHITECTURE](../ARCHITECTURE.md) §2 — the operation modes this ADR
  distinguishes
