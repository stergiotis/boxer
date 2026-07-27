---
type: adr
status: accepted
date: 2026-07-25
reviewed-by: "p@stergiotis"
reviewed-date: 2026-07-26
---

# ADR-0144: Query-engine adapters — three roles, not one plugin

## Context

Three engine classes are in scope, the third of them committed roadmap
rather than shipped:

1. **`clickhouse-local`** via the chlocal broker
   ([ADR-0028](./0028-chlocal-low-latency-sql-cap.md)) — one-shot workers,
   no listener, system tables that die with the process.
2. **A ClickHouse server over sync HTTP** — the standard interface play
   uses today. Registers runs in `system.processes` and `query_log`.
3. **A ClickHouse cluster, async, results streamed over Kafka or NATS** —
   submit, then subscribe; results arrive on a channel that is not the
   request connection, from one of several members.

The extension points of
[query-system-requirements](../explanation/query-system-requirements.md)
were built against the first two, and a natural reading is that each point
should become a plugin with one realization per engine. That reading does
not survive contact with the set: of the nine points, E1 (SQL facts), E5
(provider seam) and E6 (probe) are engine-independent, and E2 (dispatch)
sits *above* engines — it is what chooses one. Bundling them into a
per-engine interface would give both realizations six identical methods.

The two-engine world also made the variation look smaller than it is,
because engine 1 differs from engine 2 mostly by **absence**: no
`system.processes`, no `query_log`, nothing to kill. An interface whose
second implementation is a null object is not yet earning its keep.

Engine 3 is what changes the answer. It does not have fewer capabilities —
it has a **different lifecycle**. Submit-then-subscribe is not
request-then-read with parts missing, and results arriving from one of
several members over a shared channel is not a degenerate case of a body
being read. That is a genuine third implementation of something, and the
question is only: of what?

Engine 3 also exposes three problems the current design does not handle.
They are stated here because they constrain the shape:

- **Member selection has a hook and no policy.** The dispatch seam
  ([ADR-0141](./0141-play-endpoint-dispatch-seam.md)) threads an affinity
  token that nothing judges, because boxer's own two endpoints have no
  members to choose between. R4 wants member choice to be a deterministic
  function of (placement, generation), so co-displayed panels cannot
  straddle replicas with different replication lag.
- **Cancellation needs the member, not the placement.** R11 addresses
  `KILL QUERY` to the member that ran the query. Nothing currently records
  which that was.
- **Run identity is scoped per host, and engine 3 federates.** The minted
  `play-<label>-<pid>-<seq>` distinguishes lanes and processes but not
  machines — recorded as a caveat when the id contract was written down,
  and harmless while every result returned down the connection that asked
  for it. On a **shared** Kafka/NATS topic it stops being a caveat: two
  boxer processes on different hosts can mint the same id, and their result
  streams then collide on the same correlation key. Silently, and worse
  under load.

## Design space (QOC)

**Question.** At what granularity should engine differences be abstracted?

**Options.**

- **O1** — a plugin family per extension point, one realization per engine.
- **O2** — three role interfaces (deliver / observe / control), engines
  implementing the ones they support.
- **O3** — no interfaces; capability data on the dispatch decision, with
  consumers branching on it.

**Criteria.**

- **C1** — fits absent capabilities without ceremony (engine 1 has almost
  none).
- **C2** — fits a different lifecycle (engine 3 is not engine 2 minus
  features).
- **C3** — keeps site policy out of boxer; no boxer-owned taxonomy of
  other people's engines.
- **C4** — cost now, against two shipped engines and one on the roadmap.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | −− | +  | ++ |
| C2 | +  | ++ | −− |
| C3 | −  | +  | ++ |
| C4 | −− | −  | ++ |

O3 was the right answer while there were two engines, and this ADR would
have recommended it a week ago. C2 is what moves the decision: capability
data can say "this engine has no `system.processes`", but it cannot express
"results arrive later, on a topic, from a member you have not chosen yet".

## Decision

We will introduce **three role interfaces**, not a plugin per extension
point, and not one fat engine interface.

**Delivery** is the one every engine implements. It is a *source of
frames*, which is exactly the shape
[ADR-0142](./0142-runstream-result-frame-contract.md) already fixed:

> A synchronous HTTP response is the degenerate case of this stream, not an
> exception to it.

That framing was chosen so this would work. A sync engine yields data
frames then a terminal; engine 3 yields the same sequence, arriving over
time from a subscription. Consumers of results do not branch on engine at
all — they consume frames and consult the terminal, as they already do.

**Observation** and **control** are optional. An engine implements them or
does not, and a consumer discovers which by asking for the interface rather
than by consulting a table of engine names. Engine 1 implements neither;
engine 2 implements both, against `system.processes` and `KILL QUERY`;
engine 3 implements observation over its async channel and control against
the executing member. The E7 poller
(`public/keelson/runtime/queryprogress`) becomes engine 2's observation
implementation rather than a free-standing component.

Two consequences for the dispatch seam, both small:

- The decision **records the resolved member** where the engine has
  members. It is what cancellation addresses and what an audit of the run
  needs (R12) — not a new interface, a field.
- The **affinity token gets judged** by a cluster adapter, as the
  deterministic (placement, generation) function R4 asks for. Boxer still
  supplies no cluster policy; it supplies the place where one goes.

**Run identity gains a host component** so it is unique on a shared
channel. One id, not two: R7 requires a single identity end to end, and a
second stream-scoped correlation id would be exactly the fragmentation it
exists to prevent. `sysmetricsbus.HostToken` is already the repo's
convention for naming a box on a shared bus, and is the natural source. The
change is wire-visible — it lands in `query_log`, in the `log_comment`
stamp, and in pins — so it belongs *before* engine 3 exists, not after.

## Alternatives

- **O1, a plugin per extension point.** Six of nine points do not vary by
  engine; the interface would be mostly identical methods, and a
  boxer-owned registry of engine classes re-imports the site-policy framing
  that removed ADRs 0136–0138.
- **O3, capability data alone.** Correct for two engines, and what this ADR
  would have recommended before engine 3 was committed. It cannot express a
  different lifecycle.
- **One fat engine interface** with every method, returning "unsupported"
  where absent. Moves the null objects inside the interface instead of
  removing them, and makes every consumer handle at runtime a fact known
  statically.
- **A second, stream-scoped correlation id** instead of widening the run
  id. Cheaper to land, and it breaks R7: two ids for one run means every
  join has to know which one it is holding.

## Consequences

### Positive

- Result consumers stop caring which engine ran the query — they consume
  frames, which is what ADR-0142 set up.
- Absent capabilities are expressed by not implementing an interface,
  which is checkable statically rather than at runtime.
- Cancellation and read consistency finally have somewhere to live (R4,
  R11), instead of a threaded token nothing judges.
- The identity defect is fixed while it is still theoretical.

### Negative

- Widening the minted run id is a migration: existing `query_log` rows,
  facts and pins carry the old shape, so anything joining on it must
  tolerate both for one retention window.
- Three role interfaces are three more things to keep honest, and the
  third has no implementation until engine 3 lands. The first two are real
  today, which is the bar this ADR sets for building them at all.

### Neutral

- boxer would ship adapters for engines 1 and 2 only. Engine 3's adapter
  can live wherever the cluster policy lives; the interfaces do not oblige
  boxer to own it.
- Capability *data* published through E5 stays useful and separate: an
  interface answers "can this engine do X" in process, a table answers it
  for a system that wants to query its own placement map.

## Status

Accepted 2026-07-26. **Partly implemented.**

The identity widening has landed: `public/keelson/runtime/runid` owns the
contract and play mints through it, verified against a real server (a
widened id comes back on both the `QueryStart` and `QueryFinish` rows of
`query_log`). It was taken first, ahead of acceptance, because it is the
one piece that is cheap now and a migration later — it is wire-visible, so
the longer engine 3 waits the more history carries the narrow shape.

Remaining: the delivery role over the two shipped engines, then observation
and control. The role interfaces are deliberately unbuilt until then — two
real implementations is the bar this ADR sets for drawing them at all, and
engine 3's is not one of them yet.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-07-26 — The three roles are built, over both shipped engines

`public/keelson/runtime/queryengine` carries the roles; an adapter package
sits beside it per engine (`chlocal`, `chserver`). Both engines' delivery is
in use rather than merely available: `introspectengine` consumes engine 1
through it, and play's result path consumes engine 2 through it.

**Delivery is a pull iterator, and the frames carry bytes.** `Deliver`
returns a `StreamI` the consumer advances (`Next`/`Err`/`Close`), which suits
all three lifecycles — a bus reply already in hand, an HTTP body being read,
and a subscription arriving over time — without any of them needing a
goroutine to invert control. The payload is `[]byte` because what an engine
delivers is a result body in a requested FORMAT; decoding belongs to the
consumer, and both shipped engines produce bytes natively. play decodes Arrow
above the seam, exactly as it did below it.

Two invariants turned out to be load-bearing, and both are stated in the
interface rather than left to convention:

- **An error means the request was rejected, never that the run failed.** A
  statement the server refused, a worker that died, a transport that never
  connected — all are terminal frames. That is what lets a consumer stop
  branching on which engine ran the query, and it is why `Deliver`'s error
  return is narrow enough to be uninteresting.
- **A data frame's bytes belong to the consumer.** The alternative
  (valid-until-next-call, which a pull iterator makes tempting) silently
  corrupts anything that collects, and collecting is the common case.

Judgement of a declared row cap moved *onto* the engine side, where the
response counters are: the caller declares `RowCap`, the engine decides. The
consequence is engine 1 finally saying something honest — it reports no
result row count at all, so a request that declared `max_result_rows` with
`result_overflow_mode = break` comes back marked *may be a prefix* rather
than as complete. Loud and ambiguous beats quiet and wrong.

**Observation is the E7 poller, unchanged.** `queryprogress.Poller` satisfies
`ObservationI` verbatim — the interface was drawn to what the poller already
did, which is the version of "fitting an existing component into a role" that
does not involve editing the component. What the engine adds is the *binding*:
`chserver.NewObserving` builds the poller against its own endpoint and
credentials, so a poller pointed at a different server than the one that ran
the query is no longer expressible. Optionality is carried by the type — a
plain `Engine` has no `Watch` method, so it does not satisfy `ObservationI`,
and an engine built without a bus cannot be asked to observe.

**Control is `KILL QUERY` addressed by run id**, on the engine bound to the
member that ran it. A nil error is explicitly not evidence that anything
stopped, for the same reason the poller never synthesises a terminal: a run
that already finished, one that never existed, and one this call ended are
indistinguishable from outside.

One thing did *not* move, and the reason is worth recording. ClickHouse's
in-band progress ticks arrive inside the still-open response-header block —
which is to say **before the stream they would have to be frames of exists**.
They stay a callback on the request. Making them frames would mean replaying
them once the headers completed, by which time the only property they had,
liveness, is gone. Frames are how a party that is *not* the connection holder
sees progress, and that party is what the observation role serves.

### 2026-07-26 — The two loose ends

Both are closed as the Decision described, and both remain unexercised by
boxer's own endpoints, which is the honest state rather than an omission.

- **The affinity token has a judge.** `queryengine.SelectMember(members,
  affinity)` is the deterministic (placement, generation) choice R4 asks for.
  It sorts the roster before selecting, which matters more than it looks: a
  placement assembled from a map has no order, and a choice that depended on
  iteration luck would break exactly the guarantee the function exists to
  provide. It is not a balancer and swapping it for one would be a mistake —
  spreading across generations is a consequence of tokens differing, not the
  goal. The roster stays site data.
- **The decision records the resolved member.** `dispatchDecision.member`,
  plus `killTarget()`, which answers "which host does a cancellation go to"
  once instead of leaving each caller to re-derive it. Empty for boxer's own
  endpoints, where the target *is* the member.

Not done, deliberately: play does not yet issue `KILL QUERY` on cancel. It
supersedes by run id (`replace_running_query`), which costs no second request
and covers the case that actually occurs — a superseded lane. Making cancel
kill is a behaviour change with its own trade-off (an extra round trip
against a server that may already have finished) and belongs to whoever wants
it, not to the ADR that made it possible.

Also unwired, and previously recorded as such: no consumer subscribes to the
poller. play is the connection holder for its own runs, and for that party
the in-band ticks are strictly better — they stream, and they are not floored
by a tick. The observation role's value is for a party that is not the
connection holder, and this repository still has none.

## References

- [doc/explanation/query-system-requirements.md](../explanation/query-system-requirements.md) — R4, R7, R10, R11 and the extension-point catalog.
- [ADR-0028](./0028-chlocal-low-latency-sql-cap.md) — engine 1, and its reserved `Streaming` flag.
- [ADR-0141](./0141-play-endpoint-dispatch-seam.md) — the dispatch seam the member and affinity fields belong to.
- [ADR-0142](./0142-runstream-result-frame-contract.md) — the frame contract delivery is a source of.
- [ADR-0143](./0143-bus-streaming-reply-channel.md) — the streaming wire engines 1 and 3 both need.
