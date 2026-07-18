---
type: explanation
audience: contributors implementing or reviewing the query-observability slices
status: draft
---

> **Status: draft — pre-human-review.** Living overview. This page binds the
> work; it decides nothing. Where this page and an ADR disagree, the ADR is
> the record.

# Query observability, db to glass

Every ClickHouse query boxer runs should be a first-class citizen of the data
it runs against: its **definition**, its **environment**, its **execution**,
its **performance**, and its **result** stored as leeway data in the same
ClickHouse, streamed to the glass while it runs, durable after it finishes,
and traversable in both directions — from a pixel in play back to the run,
the profile, the query text, and the source rows that produced it; and from
that data forward into new queries, with the tool observing itself.

This page holds that goal, the entity model, and the map of which ADR decides
which part. It is deliberately mechanism-light: decisions live in the bound
ADRs, and their live status is tool-derived
([ADR-0092](../adr/0092-adr-overview-tool.md))
rather than maintained here.

## Where the requirements come from

[ADR-0050](../adr/0050-clickhouse-observability-pipeline.md) and
[ADR-0051](../adr/0051-query-categorization-provenance.md) (April 2026,
never implemented) articulated the requirements first, against a boxer that
had none of today's substrate — which is why their mechanisms were heavy and
why the requirements outlived them. Restated:

| # | requirement | origin | state |
|---|---|---|---|
| R1 | Live progress per running query (rows/bytes/memory, cancel) | 0050 | shipped — in-band progress headers consumed live (an incremental-header transport; Go's stock client only delivers the block at completion) into the status-bar and loading badges; cancel via the Run/Cancel toggle + server-side supersession |
| R2 | Result batches to the glass as they arrive | 0050 | inline Arrow on the response shipped; incremental rendering open |
| R3 | Terminal run accounting (profile events, peak memory, exceptions) | 0050 | decided — [ADR-0115](../adr/0115-query-observability-data-plane-strategy.md) (plane B) |
| R4 | A facts substrate absorbing operational records | 0050 | shipped — `runtime.facts` (ADR-0026 §SD6), recordstore, DimensionStore in flight |
| R5 | Result archival routed by shape, with provenance to source rows | 0050+0051 | Tier 1 shipped — a pin freezes the batch as-is into a per-pin ClickHouse table with the result's own schema plus a metadata row (content fingerprint, query/run/lane anchors); opening a pin is plain SQL. Ref-tuple lineage and Tier-2 weaving stay open (S6) |
| R6 | Auditable query categorization; governed ingestion for data products | 0051 | open — rescoped from gate to affordance (below) |
| R7 | Machine-consumable export without re-implementing the CH protocol | 0050 | facts + `url()` cover pull; NATS-core forwarding decided as the push leg, built at consumer trigger (plane E) |

## The entity spine

```
QueryDef ──(TransformChain)── ExecutedDef
    ▲                              ▲
    └──────────┐      ┌────────────┘
             QueryRun ◄──ref── ResultSet ──ref→ source rows (lineage)
                │    ▲
           RunProfile └── ParamEnv
```

### QueryDef — the program, in two forms plus the chain between them

The text a user authors is not the text that executes. play's
`BuildStatement` extracts parameters, applies the registered pre-execute
rewrites ([ADR-0108](../adr/0108-keelson-sql-pass-registry.md) — e.g. the
`LW_ID_*` identity-tag macro expansions of ADR-0106), and rewrites the FORMAT
clause. The server's `normalized_query_hash` identifies only the **executed**
form. A definition therefore has three parts:

- the **authored text** (editor buffer / caller input), client-fingerprinted;
- the **executed text** (as-sent body), identified server-side by
  `normalized_query_hash`;
- the **transform chain** between them: the ordered pass names *with
  versions/content hashes*, sourced from the pass registry that already
  catalogs itself as `keelson('sql_passes')`. The nanopass-backed
  preprocessing chain is growing; capturing which rewrite regime produced an
  executed text is what keeps archived runs interpretable after the passes
  (and their goldens) evolve.

Texts and chains are interned dimensions
([ADR-0112](../adr/0112-dimensionstore-interned-facts-additive-memberships.md)
substrate); until that lands, runs carry fingerprints and a capped inline
text.

### ParamEnv — the environment a definition evaluates in

Parameters deserve first-class treatment because they are what *factorizes*
run identity. play harvests top-level `SET param_x = …` statements out of the
buffer (`ExtractParams`); values ride the HTTP `param_*` channel while the
body keeps typed `{x:Type}` placeholders for server-side substitution. The
consequence is exactly the property we want: the executed text — and its
`normalized_query_hash` — stays **stable across parameter values**. The
definition is the function; the parameter set is the environment it is
applied to. Inlining literals instead would smear the environment into the
definition, giving every value its own hash and destroying per-definition
history and performance trends.

Capture constraints, verified on ClickHouse 26.6: query parameters do **not**
appear in `system.query_log` — there is no parameters column and they are not
recorded in the `Settings` map. Environment capture is therefore
**client-side by necessity**:

- parameter **names and declared types**: always recorded;
- **values**: inline up to a size cap, content-hash beyond it (values can be
  large — the URL channel exists precisely because they exceed comfortable
  SQL-literal size; the client documents the URI-size ceiling);
- an **environment fingerprint** (hash over the canonical name→value
  binding) rides the `log_comment` stamp so runs with identical definitions
  but different environments are distinguishable server-side from day one;
- interning of large or recurring values is deferred to the DimensionStore
  substrate.

Signals close the loop to the glass:
[ADR-0097](../adr/0097-play-reactive-query-graph.md) §SD8 defines signals as
*unbound parameters* bound by the UI (viewport, selection, …). Recording
which signal bound which parameter gives provenance from a glass gesture to
the result it produced.

### QueryRun — the anchor

One execution: a `runtime.facts` row (kind `QueryRun`), captured by the
ADR-0115 pipeline, deterministic identity, stamped with
`{run_id, app, lane, authored_fp, sent_fp, chain_fp, env_fp}` via
`log_comment` so the server's own log is independently attributable. Every
other entity hangs off the run by ref-tuple membership
([ADR-0109](../adr/0109-leeway-marshall-multi-membership-ref-tuples.md)).

### RunProfile — performance at three depths, three lifetimes

- **live**: progress ticks from the in-band HTTP headers — transient glass
  state, never persisted;
- **post-hoc**: ProfileEvents, counters, peak memory — attributes on the
  QueryRun fact;
- **deep**: `query_metric_log`, `processors_profile_log` and friends — never
  captured; they are one drill-down query away by construction, issued
  through play itself.

Per-definition trends (latency distributions over time, via tdigest pushdown
into boxenplots) are plain queries over QueryRun facts joined to QueryDef.

### ResultSet — tiered, from ephemeral to woven

The rows a run produced, identified by content fingerprint (play already
fingerprints every lane result) and ref-tupled to their run:

- **Tier 0 — ephemeral** (default): the lane memo. Today's behaviour; free.
- **Tier 1 — pin** (shipped): persist the Arrow batch as-is, any query, no
  classification required. A pin is a per-pin ClickHouse table carrying the
  result's own schema (the batch bytes go in verbatim) plus one metadata row
  in `runtime.resultsets` — so the "resultsets store" is ordinary queryable
  tables on the user's endpoint, and opening a pin is plain SQL every panel
  already renders.
- **Tier 2 — weave**: when shape analysis proves the result data-mart-shaped
  or lineage-carrying, rows land as typed leeway rows with ref-tuple lineage
  to source rows. A candidate first cut, to be decided at the S6 ADR: for
  results the passthrough classifier (ADR-0117) proves 1:1-as-stored, land
  the pinned rows in an on-demand *sibling* of the base table
  (`T` → `T_pinned`) carrying a pin-provenance column and partitioned by pin
  id — per-pin reads prune on the partition, unpinning is a partition drop,
  and base-schema joins plus row lineage come nearly free, at the price of
  the classification gate Tier 1 deliberately avoids.

Two native ClickHouse neighbours were considered for Tier 1 and deliberately
not used. The **query result cache** (`use_query_cache`) answers a different
question: it is keyed by the *query*, TTL-bounded, in-memory, evicted, and
opaque — recompute avoidance, where a pin is a durable, *content*-addressed
snapshot that arbitrary new SQL can query; staleness is the cache's risk and
the pin's point, and the two compose (the cache accelerates re-runs, the pin
preserves what a run returned). Plain `CREATE TABLE … AS SELECT` is the
closer relative — a pin is CTAS plus content-addressed identity (idempotent
re-pins), run provenance, and a browser; CTAS also *re-executes* the query
server-side, while a pin freezes the batch the client actually received.

## From gate to affordance (rescoping ADR-0051)

ADR-0051 posed categorization as a mandatory gate: classify every query
statically or reject it. Rescoped for today's boxer, classification becomes
an **upgrade detector for the tiers above**: everything is Tier-0/1-archivable
without analysis; analysis *offers* Tier 2. Two constraints it imposed on
itself no longer hold:

- **lexical-only** died — boxer owns a catalog now (leeway mapping plans,
  TableDesc, the ADR-0066 readback validators, `keelson('…')`
  introspection), and identity columns are typed (ADR-0106 tags), so
  provenance tracing can be tag-aware instead of name-based;
- the bespoke lineage encodings (`__lineage__` alias, edge table) died —
  ref tuples are the native encoding.

Its Perm-style shape lattice and auditability discipline (rule-cited
derivations) remain the theory record to build S6 on. The whitelist /
data-product gatekeeping machinery stays deferred until an *automated*
ingestion pipeline exists to gate; play's affordance-evaluator seam is the
natural delivery vehicle for the interactive case.

## Planes

| plane | carries | mechanism | persistence |
|---|---|---|---|
| A — live | progress, cancel | HTTP progress headers → lane → node badge | none (glass state) |
| B — record | terminal run + profile + identity | ADR-0115 pipeline (`queryrunsd`) | `runtime.facts` |
| C — weave | results, tiered, lineage | pin/weave affordances; recordstore + ref tuples | resultsets + typed tables |
| D — glass | history, run detail, per-def trends, resultset browser | play panels over plain SQL; "open as query" everywhere | reads B+C |
| E — export | push to external consumers | NATS-core forwarding (ADR-0090 pattern) | at consumer trigger |

## Slices

1. **S1** — capture pipeline live (`queryrunsd`, KindQueryRun, stamping —
   including *all four fingerprints from day one* so later interning
   backfills history instead of starting blind).
2. **S2** — History tab + run-detail panel over facts.
3. **S3** — progress headers into lane badges (plane A).
4. **S4** — Tier-1 pin + resultset browser.
5. **S5** — QueryDef/TransformChain/ParamEnv interning (after ADR-0112) +
   per-definition trend view.
6. **S6** — Tier-2 weave: catalog-aware shape analysis as affordance, typed
   archival, lineage refs. Expected to need its own design ADR.
7. **S7** — deferred with triggers: data-product gating, NATS export leg,
   incremental batch rendering, OpenTelemetry spans.

## ADR bindings

| concern | decided by | state at last edit |
|---|---|---|
| data-plane architecture & technology (ELT/ETL rule, URL services, NATS forwarding leg, in-DB escalation) | [ADR-0115](../adr/0115-query-observability-data-plane-strategy.md) | proposed |
| facts substrate | [ADR-0026](../adr/0026-app-runtime-and-capability-subjects.md) §SD6 | accepted |
| pass registry / transform chain | [ADR-0108](../adr/0108-keelson-sql-pass-registry.md) | accepted |
| lineage encoding (ref tuples) | [ADR-0109](../adr/0109-leeway-marshall-multi-membership-ref-tuples.md) | accepted |
| interning substrate (texts, chains, large values) | [ADR-0112](../adr/0112-dimensionstore-interned-facts-additive-memberships.md) | proposed, in flight |
| lanes, signals, per-lane query ids, glass | [ADR-0097](../adr/0097-play-reactive-query-graph.md) | accepted, slices ongoing |
| identity bands / leased ids | [ADR-0106](../adr/0106-identity-fibonacci-tags-build-tag-retirement.md), [ADR-0111](../adr/0111-identity-technology-neutral-leased-id-generation.md) | accepted / proposed |
| service anatomy precedent | [ADR-0090](../adr/0090-sysmetrics-pubsub-data-plane.md) | accepted |
| requirement origins | [ADR-0050](../adr/0050-clickhouse-observability-pipeline.md) (superseded), [ADR-0051](../adr/0051-query-categorization-provenance.md) (dormant) | records kept |
| weave semantics (S6) | future ADR, at slice | — |

## What this page is not

Not a decision record — proposals and kill-reasons live in the ADRs above.
Not a status dashboard — the ADR overview tool derives that. Not a promise of
sequence — slices are dependency-ordered, not scheduled.
