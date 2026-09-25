---
type: adr
status: accepted
date: 2026-09-23
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-25
---

# ADR-0256: rutter — a road-network routing engine on customizable contraction hierarchies

## Context

ADR-0229 §SD7 deferred weighted shortest paths "until a weighted `edges`
contract has a consumer". The consumer is a downstream project's
routing proposal: foot and bike routing, reachability and travel-time matrices
over the Swiss topographic road layer — about 1.65 M nodes and 4 M arcs
once every two-way segment is two arcs — with a cost profile that changes
per question (a hiking grade cap, a surface rule, a closure) and, should a
car profile ever be taken up, weights that change per minute.

The shape of the problem is settled in the literature and recorded in
that consumer's survey of road routing algorithms in Go:
a plain Contraction Hierarchy bakes the metric into its order, so each
profile and each closure is a rebuild of minutes; a Customizable
Contraction Hierarchy (Dibbelt, Strasser, Wagner 2016) computes a
metric-independent nested-dissection order once and customizes a metric
in a fraction of a second, with elimination-tree queries in tens of
microseconds. No pure-Go implementation exists; the two Go routing
engines in the open are a plain CH over pointer-heavy maps and an
unfinished CRP port.

The CSR container of ADR-0229 is not the right substrate, for three
reasons that are decisions of that ADR rather than defects: parallel arcs
collapse into one whose weight is the sum, where a road graph wants every
arc kept and the minimum taken per metric; a slot carries no arc identity,
where a path has to name the segments it used; and its weights are one
`float32` column built with the topology, where a routing graph is one
topology and many `uint32` metrics customized over it.

## Decision

We will add **rutter** — a mariner's book of routes — as
`public/analytics/graph/rutter`: a routing graph with arc identities, a
4-ary heap and Dijkstra as the oracle, an Inertial Flow nested-dissection
order, a Customizable Contraction Hierarchy with basic customization and
an elimination-tree query, one-to-all with a cut-off, many-to-many by
buckets, and a grid index for snapping a point to the nearest polyline.
It takes arrays and returns arrays; nothing in it knows a coordinate
system, a road class or a country.

### Subsidiary design decisions

- **SD1 — The graph is arcs with identities, and a metric is a column over them.** `Graph` is built from parallel `tail`, `head` and `edge` arrays of `int32`: an arc keeps the edge it came from, parallel arcs are kept, self-loops are dropped. Forward and reverse adjacency are offset-and-target arrays; the reverse rows carry the forward arc index so a weight is looked up once. A `Metric` is a `[]uint32` parallel to the arcs, in milliseconds or any unit the caller chooses, with `Inf` for an impassable arc. Everything else takes a graph and a metric.

- **SD2 — Dijkstra is the oracle, and it is the reachability engine.** A `Dijkstra` holds its scratch — labels, parent arcs, a 4-ary heap with decrease-key, generation stamps so a reset is free — and answers one-to-one, one-to-all under a cut-off, and one-to-many. Every hierarchy result is tested against it over random pairs; at a million nodes a cut-off search is interactive, so reachability needs nothing faster.

- **SD3 — The order is Inertial Flow, and it is the one tunable.** Nested dissection by recursive bisection: nodes sorted along four directions of the plane, the first and last quarter as source and sink, a unit-capacity Dinic max-flow, the smallest cut's source-side endpoints as the separator, recursion into each component of the remainder, and pieces below a leaf size ordered by degree. The order is what decides customization time and query time, and a better order (FlowCutter, InertialFlowCutter) is a drop-in replacement of this one function.

- **SD4 — The hierarchy is metric-independent, customization is triangles, the query is the elimination tree.** Contraction by the contraction-graph rule — each node's lowest upward neighbour becomes its elimination-tree parent and inherits the rest — yields the upward arcs and the tree in one pass and no witness search. Customization sets each upward arc's two weights from the original arcs and relaxes every lower triangle in rank order, recording the middle node that improved an arc so a path unpacks. A query walks the two ancestor chains in rank order, relaxing upward arcs, and meets where both labels are finite; no priority queue, and a reset touches only what was labelled.

- **SD5 — Many-to-many is buckets on the backward chains.** Every target's backward search over its ancestors leaves a bucket entry at each ancestor; every source's forward search scans the buckets on its chain. Cost is sources × chain × bucket, which at country scale is seconds for a thousand by a thousand.

- **SD6 — Snapping is a grid over polyline bounding boxes.** A fixed-cell grid holds each polyline in every cell its box touches; a query scans the cells within the radius and returns the nearest polyline, the closest point and the fraction along it. The caller decides what to do with a mid-polyline point.

- **SD7 — Pointer-free, `int32`, single-goroutine.** Every structure is slices of fixed-width integers so the collector never scans them; ids are slot indices, never `uint64`s through a map; scratch is reused across queries; a customized metric is an immutable value the caller may publish behind an `atomic.Pointer`. Parallel customization over elimination-tree levels is deferred until a measurement asks for it.

### Milestones

- **M1 — Graph, heap, Dijkstra, snapping.** ✓
- **M2 — Order, hierarchy, customization, query, buckets.** ✓

### Deferred

- **Perfect customization and precomputed triangles.** Trigger: a query time measured above budget on a real graph.
- **Turn costs by edge-based expansion.** Trigger: a car profile with turn restrictions to honour.
- **Time-dependent metrics by slices.** Trigger: predicted traffic.
- **Alternatives.** Trigger: a consumer asks.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API under `public/` | added: `public/analytics/graph/rutter` | the consumer's pin, once tagged |
| House names | `rutter` joins the register in README | the README table |

## Alternatives

- **Extend the ADR-0229 CSR with arc ids and a no-collapse option.** Rejected: it would change what every analytics algorithm assumes about a row for a consumer with a different shape; a second container costs one counting sort.
- **A plain Contraction Hierarchy.** Rejected: metric-dependent, so every profile is a rebuild.
- **Customizable Route Planning (multilevel overlay).** Rejected: more code for the same customization behaviour, and the CCH's elimination-tree query needs no priority queue.
- **A cgo binding to RoutingKit.** Rejected: the downstream binary builds with `CGO_ENABLED=0`.
- **Hub labels or transit-node routing.** Rejected: metric-dependent and a kilobyte per node.

## Consequences

### Positive

- Weighted shortest paths exist in boxer, with nothing Swiss in them.
- A profile change is a customization measured in a fraction of a second; the topology and its order are computed once per edition.
- Every result is tested against Dijkstra over random pairs.

### Negative

- Order quality is a heuristic; a poor separator on some graph shows up as a slow customization, not an error.
- Customization is single-threaded in this cut.
- A second graph container beside the ADR-0229 CSR.

### Neutral

- The package has no opinion on units: a metric is whatever the caller counts, saturating at `Inf`.

## Migration — Tier 1

- **Breaks.** Nothing; a new package.
- **Path.** Nothing to migrate.
- **Regeneration.** None.
- **Old shape.** None.

## Verification plan — Tier 1

- **Lane.** Default `go test`: hand-checked paths on small graphs; Dijkstra against Floyd-style brute force on random graphs; the hierarchy's distances equal to Dijkstra's over random pairs and random metrics, including `Inf` arcs and parallel arcs; unpacked paths that are valid walks whose arc weights sum to the distance; buckets equal to pairwise queries; the grid index against a linear scan; a property test on the order (every node once, the elimination tree a forest whose parents have higher rank).
- **What would fail.** A customization that misses a triangle produces a distance above Dijkstra's; an unpacking that loses an arc produces a walk that is not connected; an order that is not a permutation fails the property.
- **Gap.** Order quality and timings on a country-size graph are measured by the consumer and recorded in its ADR, not asserted here.

## Status

Accepted (2026-09-25). M1 and M2 shipped (Updates, 2026-09-24); the
Deferred items stand as follow-ups, each with its trigger.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-09-24 — M1 and M2 shipped, and the first country-size numbers

Both milestones landed in one cut, with the lane the verification plan
names: every hierarchy answer equal to Dijkstra's over random grids,
metrics, parallel and forbidden arcs and snapped ends; unpacked paths as
walks summing to the distance; buckets equal to pairwise queries; the
order a permutation whose tree parents are upward neighbours; the grid
index equal to a linear scan. Polyline coordinates are float32, and
`Index.NearestWhere` takes an acceptance predicate so a consumer snaps
under a profile.

The consumer measured the package on the Swiss road layer (its routing
ADR, Updates 2026-09-24): 1 654 880 nodes and 4 154 136 arcs; the
Inertial Flow order in 138 s single-threaded; contraction in 1 s to
7 843 889 up-arcs (1.89× the arcs) and a tree of height 792;
customization in 4.3 s single-threaded; a country-length query with
unpacking in 5 ms, a six-by-six matrix by buckets in 21 ms. Two of the
context's expectations are corrected by that: the fill-in is above the
1.15–1.7× the literature reports, and a query is milliseconds, not tens of
microseconds. Both are the order's quality — plain Inertial Flow with the
separator taken as the source-side endpoints of an edge cut — and §SD3
names the replacement. On a 300×300 grid the order costs 2.8 s and the
fill is 7.7×, which is a lattice's nature rather than a defect.

### 2026-09-24 — map matching, pulled out of Deferred

Its trigger fired the same day: `Matcher` is Newson–Krumm over the
index's candidates, with route distances from bounded one-to-many
Dijkstra searches under a length metric that leaves and enters a
polyline at the right ends, decoded by Viterbi and breaking the sequence
where an observation has no candidate or no reachable predecessor.
`Index.Candidates` keeps every admitted polyline within the radius,
nearest first, for it. The consumer matched 146 synthetic fixes along a
4 km route onto the Swiss graph in 204 ms.

## References

- [ADR-0229](./0229-graph-analytics-engine.md) — the CSR container and the deferral this decision answers.
- A downstream project's routing ADR (proposed) — the consumer.
- Dibbelt, Strasser, Wagner, "Customizable Contraction Hierarchies", JEA 2016 — https://arxiv.org/abs/1402.0402
- Bläsius et al., "Customizable Contraction Hierarchies – A Survey", 2025 — https://arxiv.org/abs/2502.10519
- Schild, Sommer, "On Balanced Separators in Road Networks", SEA 2015 — https://aschild.github.io/papers/roadseparator.pdf
- Knopp, Sanders, Schultes, Schulz, Wagner, "Computing Many-to-Many Shortest Paths Using Highway Hierarchies", ALENEX 2007.
