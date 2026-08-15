---
type: adr
status: proposed
date: 2026-08-14
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0184: sysmetrics persistence tee — metric samples as facts on a generated record store

## Context

[ADR-0090](./0090-sysmetrics-pubsub-data-plane.md) §SD9 reserved a persistence
tee as phase **P5**, "optional … only if history is ever wanted", and §SD4 wrote
down why it could wait: the payload is already facts-shaped, so persistence is
"a free future add". Two other ADRs then queued behind it —
[ADR-0126](./0126-appliance-topology-as-data.md) §SD6 defers topology history to
"the ADR-0090 P5 tee … the day it is built", and
[ADR-0169](./0169-continuous-coverage-keelson.md) §SD6 designed the tee
generically, calling itself "the first realization of ADR-0090 P5".

**History is now wanted, and the want is recorded in code.**
[`loadstudy`](../../public/analytics/timeseries/loadstudy/doc.go) takes its load
channels from ClickHouse's own `system.asynchronous_metric_log` and documents
why:

> They do *not* come from […]`sysmetrics`, which would be the natural source.
> That scraper publishes to NATS and persists nothing, so its data does not
> exist to analyse. Nor do they come from `boxer.facts`, which carries no
> numeric payload at all — it is an event log.

That is the trigger §SD9 was waiting for. The second sentence is also wrong
about the schema, and has been since before it was written:
[`factsschema`](../../public/keelson/runtime/factsschema/factsschema.go) builds
`u8`…`i64Array` sections, `u32Set`/`u64Set`, and `f32Array`/`f64Array` under
`AspectLightSlowlyChangingFloat` — an encoding hint chosen for slowly-changing
floating-point series. The substrate was provisioned for metric-shaped payloads
before anything wrote metrics into it; what is absent is a *metrics* writer,
not a column — and not a writer of numbers at all, since `gov/capmapfacts` has
been putting an `f64` on that table (a normalized compression distance) since
well before this ADR.

### What has changed since P5 was deferred

- **`recordstore` exists** ([ADR-0100](./0100-recordstore-generated-leeway-clickhouse-store.md)).
  A generated store's `Ingest<Kind>` verb takes a per-tick SoA batch — the exact
  shape ADR-0090 §SD3 described by hand.
- **[ADR-0105](./0105-keelson-adopts-generated-record-stores.md) D3b is
  ungated.** Its two prerequisite generator features (membership-id override,
  id-level disjointness) landed in `dc7b43ce` as
  `marshallgen.FixedIdsWrapper` + `MembershipIdSourceI` + `gen.Input.Wrapper`.
  D1 (`keelson/data/storeexec`) remains the one unbuilt blocker.
- **[ADR-0181](./0181-leeway-dql-authoring-surface.md) shipped M0–M5.**
  `LW_GET` / `LW_GET_NULL` / `LW_GET_LIST` exist in
  [`constructsql`](../../public/semistructured/leeway/constructsql/extractsql.go);
  the skip-index policy layer (`SkipIndexPolicy`, `DeriveSkipIndexes`,
  `TableOptions.SkipIndexes`) exists, and `302df7bc` carried the last of it
  through `recordstore/gen`'s option merge.
- **[ADR-0150](./0150-timeseries-subsequence-anomaly-detection.md) shipped
  M1–M3** — `matrixprofile`, `adscore`, `damp`.

### How the recorded design aged

Three parts of it did not survive contact, and this ADR corrects them rather
than inheriting them:

- **ADR-0090 §SD4's "tee the same bytes" is void.** It assumed §SD3's
  facts-codec wire. P2/P3 shipped `CBORCodec` and the swap never happened, so a
  tee decodes CBOR into `sysmsnap` structs and re-models them. The half that
  survives is the useful half: producer and consumer stay untouched.
- **ADR-0090 §SD3's schema sub-decision rests on a cost gap that closed.** It
  preferred the generic `boxer.facts` schema over a parallel one for "zero new
  codegen", when the alternative meant hand-driving the leeway generators.
  `recordstore/gen` reduced that alternative to one `Input` literal in a
  gen-test. The conclusion stands; the recorded reason no longer carries it.
- **ADR-0169 §SD6 prescribes the hand-rolled shape** — "capmap-style … encoder
  over `dml.InEntityFacts`, `RecordSinkI` sink". That is the code class
  ADR-0105 exists to delete, and its D5 says the next kind lands as a generated
  store.

One thing no prior ADR anticipated: `chstore.Store.SetupTable` is the sole
author of the `boxer.facts` DDL. A generated store's `EnsureTable` would make it
the second, on a live table.

## Design space (QOC)

**Question.** What carries persisted system metrics?

**Options.**

- **O1** — facts-bound generated record store on `boxer.facts` (ADR-0105 D3b).
- **O2** — a dedicated generated table, the ADR-0105 D3a posture applied to
  metrics.
- **O3** — hand-rolled encoder over `dml.InEntityFacts` into `boxer.facts`,
  ADR-0169 §SD6 as written.
- **O4** — no tee; consumers keep reading `system.asynchronous_metric_log`.

**Criteria.** C1 correlates with runtime facts in one query; C2 hand-written
lines; C3 DDL ownership and drift exposure on a live table; C4 ad-hoc read
ergonomics; C5 index and retention control; C6 host and domain coverage.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | −  | ++ | −− |
| C2 | ++ | ++ | −− | ++ |
| C3 | −  | ++ | −  | ++ |
| C4 | +  | +  | +  | ++ |
| C5 | −  | ++ | −  | −  |
| C6 | ++ | ++ | ++ | −− |

O4 is the status quo `loadstudy` documents as a workaround: ClickHouse's
asynchronous metrics describe the ClickHouse server, not the box — no GPU,
battery, PSI, process table or container view — and cover one host where the
plane already carries many (ADR-0090 §SD1). O3 loses C2 outright for no gain
over O1. O2 wins the two criteria O1 loses, and loses the one the tee exists
for: putting load beside the runtime's own lifecycle events without a
cross-table join, which is precisely what `loadstudy` correlates. **O1 is
taken**, with SD2 and SD5 as the explicit, named mitigations of its C3 and C5
losses rather than as omissions.

## Decision

We will build ADR-0090 P5 as a **bus-subscriber tee writing `boxer.facts`
through a generated record store**, opt-in and default-off.

### SD1 — Substrate: `boxer.facts` through a generated record store

The tee binds the `boxer.facts` `TableDesc`
(`factsschema.GetSchemaInManipulator`) through `recordstore/gen`, realizing
ADR-0105 D3b and superseding ADR-0169 §SD6's hand-rolled prescription. This is
the *same* conclusion ADR-0090 §SD3 reached, restated on the reason that
survives: metric samples belong beside the runtime facts they are correlated
against, per [ADR-0148](./0148-app-workingsets.md)'s modelled-substrate
invariant — not because the alternative is expensive to generate, which it no
longer is.

### SD2 — The tee does not own the table: `ExternallyProvisioned`

`recordstore/gen` already carries an `ExternallyProvisioned` option
(`308a7cbe`, predating this ADR). Under it the emitted store **omits
`EnsureTable` entirely** and keeps `VerifySchema`; the DDL file is still
written as the schema of record for review and diffing, but nothing in the
generated surface can apply it. `chstore.Store.SetupTable` stays the sole DDL
author for `boxer.facts`.

`storegen` sets it unconditionally rather than exposing it: every facts-bound
store is externally provisioned, and a store that cannot run DDL cannot be
wired up to run it by a later caller who did not read this section.

Suppression at generation time rather than by convention is deliberate: a store
that cannot provision cannot be wired up to provision by a later caller who did
not read this ADR. The tee calls `VerifySchema` at startup and refuses to write
on mismatch.

The transport agreed independently while M0 was built: `EnsureTable` ran its
DDL as one multi-statement script, and the ClickHouse HTTP interface rejects a
multi-statement body outright ("Multi-statements are not allowed", verified
against 26.7.3). That accident is gone — since 2026-08-15 a generated
`EnsureTable` issues one statement per `Exec` (ADR-0100's Update of that
date), so a store bound to the HTTP executor *can* provision itself. SD2 is
therefore the only thing keeping a facts-bound store from running DDL, which
is why it is a declared property rather than a runtime surprise.

### SD3 — Fact model: one append-shaped kind per domain

Per ADR-0090 §SD3, one DTO kind per collector domain; one entity per
`(host, domain, tick)`.

- **Key** — `xxh3.HashString(host + "/" + domain)`, over
  `github.com/zeebo/xxh3` (a direct dependency, already the id source in
  `fffi2/typed` and the egui2 id stack).
- **Order** — the sample timestamp.
- **Natural key** — the `(host, domain)` pair in the vdd natural-key format, so
  the digest need not be inverted to read a row.
- **No state view.** `boxer.facts`'s `expiresAt` is `z64`, and
  `recordstore/gen` binds the Lifecycle role only on a `uint8` column, so the
  emitted store carries no `Delete` / `GetLive`. ADR-0169 §SD6's "no
  tombstones, no latest-wins machinery" is enforced by the schema rather than
  by discipline.
- **Raw counters only.** Rates, windows and EWMA stay consumer-side
  (ADR-0090 §SD3).
- **Descriptor/sample split**, following ADR-0169 §SD6's `coverage.func`
  pattern: per-host facts that do not move between ticks — CPU model, logical
  core count, the topology tree — are ingested once on first sight of an
  unknown host, and the per-tick kinds carry only what changes.

The bus carries one whole-bundle subject (`sysmetrics.{host}.bundle`), not the
per-domain split ADR-0090 §SD1 described, so domain selection is tee-side
configuration rather than a subscription.

### SD4 — Vocabulary at a claimed tag value, under ADR-0183's regime

A new vocabulary package takes its own tag value. It was **base 32** when this
section was written — the next unused multiple of 16 under
[ADR-0168](./0168-capmap-business-capability-corpus.md) §SD6's allocation rule
— and it is now **2178312**, claimed from the width-32 class through
`identity/tagmint`, one day later:
[ADR-0183](./0183-leeway-component-consumer-simplification.md) D0's re-key
moved every vocabulary into one class and made the value a checked claim
rather than a written-down convention. The base rule went with it, since a
width-32 tag holds ~4.3·10⁹ ids and needs no reserved room beside it. The
sysmetrics ids all moved; the tee had written none that outlive the change. ADR-0090 §SD8's `sysmetrics.sensitive` membership is declared
from the start even though masking stays deferred, so rows written before the
switch exists are already tagged.

[ADR-0183](./0183-leeway-component-consumer-simplification.md) is **proposed,
not accepted**; this ADR is written to work under either outcome and places one
requirement on it:

- **D0 (explicit ordinals).** ✓ Landed. This vocabulary was born under
  declaration-order minting and joined D0's migration as the fifth
  VCS-managed registry; its 153 registrations now state their ordinals, at the
  values they already had.
- **D1 (assignment golden).** Written from day one regardless of D0, because it
  is cheap and independently valuable: it makes the append-only discipline
  mechanical for a registry that starts empty, which is the only moment the
  golden costs nothing to establish.
- **D1 (`MembershipIdSnapshot`).** Specified vdd-side. A fifth vocabulary needs
  the same helper per registry. That generalization is a **requirement this ADR
  places on ADR-0183 D1**, not an assumption it may make; if D1 lands vdd-only,
  the snapshot for this registry is built here and D1 absorbs it later.

### SD5 — The store states its index policy; `chstore` applies it

SD2 splits DDL ownership, so the store cannot apply an ADR-0181 §SD4 skip-index
policy to a table it does not own. The generated store therefore **declares**
the policy it wants — bloom filters on the membership lanes it writes,
`set(N)` where the extraction shapes call for it — as the schema of record;
applying it to `boxer.facts` is a `chstore`-side change, deferred with the
trigger **measured volume**, not shipped speculatively.

This is a real cost of SD1 over O2, recorded as such.

### SD6 — Read surface: `LW_GET`, and the id-not-name caveat

ADR-0090 §SD3's promise that metrics become "`play`/ClickHouse-queryable the
moment persistence is wanted" is met at ship time. The `LW_GET*` family expands
against the metric sections;
[the read-surface page](../explanation/leeway-sql-read-surface.md) is the entry
point, and hand-writing array arithmetic against these tables is the documented
mistake, not the fallback.

**Two rules of the surface, neither optional on this table.** Both were
written down wrong here first and corrected by building the expansion golden
the verification plan asks for:

- **The channel token is mandatory.** Every `boxer.facts` section carries more
  than one membership channel, so a call without `chan:` is refused rather
  than defaulted. This vocabulary writes on the ordinary
  one-membership-per-attribute channel, `chan:low-card-ref`.
- **The verb follows the section's arity, not the Go field's.** Only `symbol`
  and `bool` store one value per attribute and read through `LW_GET`. Every
  `*Array` section stores a run per attribute and reads through
  `LW_GET_LIST` — including where the DTO field is a plain scalar, which is
  then the run's only element, read as
  `arrayElement(LW_GET_LIST(…), 1)`. `LW_GET` and `LW_GET_NULL` are refused
  on those sections outright, so an `option.Option` field's absence reads as
  an **empty run, not NULL**.

So a scalar metric reads
`arrayElement(LW_GET_LIST('f32Array', '<id>', 'chan:low-card-ref'), 1)`, not
the `LW_GET('f32Array', '<id>')` an earlier draft of this section gave — that
form is refused by the pass on both counts.

**Named caveat.** `membershipLiteral` requires a **registry id**, not a name,
for a ref-channel membership — there is no server-side name lookup
([ADR-0171](./0171-leeway-sql-read-surface.md) §SD4 is that gap). Ref channels
remain right for storage (a small closed set of metric names, compact on the
wire); the ergonomic cost is mitigated by generated Go constants, `leeway id`,
and — if ADR-0183 D3 is accepted — its `vocabclaim` publication, which turns
the id into a join rather than a lookup the author must perform by hand.

The mitigation is narrower than it reads: `providers.MembershipLookup`, which
backs `keelson('memberships')`, resolves against the **runtime** vocabulary
only, so a sysmetrics membership name does not resolve there today. A Go
caller binds `sysmvocab.NkRegistry` and writes names; an ad-hoc SQL author
writes ids.

### SD7 — Retention and rollup, and what ADR-0150 actually needs

ADR-0150's detectors split on this seam, and the split narrows what this tee
may claim:

- **`damp` is streaming.** Left discords run on the live bus with no
  persistence at all. Anomaly detection does **not** depend on this tee.
- **`matrixprofile` and `adscore` are batch.** They need history in a table.
  That is what the tee contributes, and the honest scope of the claim.

**No TTL on the raw kinds in v1 — a decision, not an omission.** A retention
window shorter than the analysis window silently truncates a matrix profile's
input, and the failure is a wrong answer rather than an error. Rollup and
downsampling stay SQL-authorable through ADR-0181 §SD1's transform contract and
§SD2's constructors; statement wrapping is manual until §SD8's grammar port.

`loadstudy` is the first consumer: when the tee lands it can take its channels
from the tee instead of `system.asynchronous_metric_log`. At minimum its
package documentation is corrected in the same milestone, since it states as
fact something this ADR makes false.

### SD8 — Opt-in, default off

The tee is a flag on `sysmetricsd`. Unset, ADR-0090 §SD4's "no CH instance
enters the metric path" remains literally true. The tee subscribes through
`sysmetricsbus.Consumer` on the same bus client, so the identical code later
runs as a standalone sink against a remote scraper without change.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `recordstore/gen` `Input` (generated-code input) | **unchanged** — `ExternallyProvisioned` already existed (`308a7cbe`); this ADR consumes it | nothing |
| `runtime/sysmtee` (exported Go API under `public/`) | new — the subscriber tee (SD8) | `sysmetricsd`; a standalone sink if one is written |
| `runtime/sysmfacts` (generated-code output) | new — the DTOs and the generated store | regenerated by its gen-test when a DTO or the vocabulary changes |
| `keelson/data/storeexec` (exported Go API under `public/`) | new — `recordstore.ExecutorI` over `chclient` (ADR-0105 D1) | its first consumer, this tee |
| `runtime/factsschema/storegen` (generated-code input) | new — feeds `recordstore/gen` the facts `TableDesc` + id snapshot (ADR-0105 D2, ADR-0183 D2) | the generated store package; the gen-test lane |
| sysmetrics vocabulary (named registry, claimed tag value 2178312) | new — memberships + `sysmetrics.sensitive` (SD4) | assignment golden; ADR-0183 D0 migration ✓; disjointness test |
| `sysmetricsd` CLI (exported surface) | +tee flag, default off (SD8) | CLI help; a deployment unit when one is written |
| `boxer.facts` content (not schema) | first numeric-payload writer | `loadstudy` package documentation; any reader assuming event-log-only content |
| `boxer.facts` DDL | **unchanged** — SD2 keeps `chstore.SetupTable` sole author | nothing; recorded because the obvious implementation changes it |

## Alternatives

- **A dedicated `runtime.sysmetrics` table (O2, the ADR-0105 D3a posture).**
  Wins DDL ownership and index control outright. Rejected because it splits
  load metrics from the lifecycle events they are read against, which is the
  correlation `loadstudy` performs and the reason to persist at all. The two
  criteria it wins are answered narrowly by SD2 and SD5 instead. This is the
  closest call in the ADR and the first thing to revisit if SD5's deferral
  turns out to bite.
- **Hand-rolled encoder, ADR-0169 §SD6 as written (O3).** The code class
  ADR-0105 exists to delete; ADR-0105 D5 already says the next kind lands as a
  generated store.
- **Keep reading `system.asynchronous_metric_log` (O4).** Describes the
  ClickHouse server, not the box, and covers one host.
- **Teeing the wire bytes.** ADR-0090 §SD4's original framing. Not available:
  the wire is CBOR, not the facts codec §SD3 assumed.
- **Per-domain subjects for tee-side domain selection.** Would follow ADR-0090
  §SD1, but the bus carries one bundle subject today; splitting it is a
  producer-side change with its own consumers, out of scope here.
- **A TTL on the raw kinds.** Rejected for v1: it truncates the analysis window
  ADR-0150's batch detectors need, and does so silently (SD7).

## Consequences

### Positive

- ADR-0090 P5 exists, which unblocks ADR-0126 §SD6 (topology history) and
  ADR-0169 §SD6 (coverage history) — both of which named it as their
  precondition.
- `loadstudy` gains the source its documentation calls "the natural source",
  covering domains ClickHouse's own metrics cannot see.
- Append-only is schema-enforced rather than conventional (SD3), so the
  latest-wins machinery ADR-0105 wants gone cannot appear here by accident.
- ADR-0105 D1 and D2 get built for a concrete consumer instead of
  speculatively; the mesh example ADR-0183 D2 names is unblocked by the same
  work.

### Negative

- The tee cannot control the indexes or retention of the table it writes
  (SD2/SD5). Both are recorded with triggers rather than solved.
- Ad-hoc queries address ref memberships by id until ADR-0171 §SD4 lands (SD6).
- A fifth VCS-managed registry is created shortly before ADR-0183 D0 proposes
  to change how all of them mint ids, adding one entry to that migration.
- Metric volume lands on the same table as runtime facts, so `boxer.facts`
  growth becomes dominated by whatever cadence the tee runs at. This is the
  concrete form of the O2 trade-off.
- **A facts-bound store re-emits the table's DML scaffolding.** Measured at
  M1: a store over `boxer.facts` generated ~305 KB of DML and ~211 KB of RA
  into its own `internal/lowlevel`, against the ~299 KB and ~206 KB the tree
  already carries in `factsschema/dml` and `factsschema/ra`. The generic
  schema's 21 sections are what make it large, so this cost is specific to
  binding *this* table and would be a fraction of it for a dedicated one —
  a point in O2's favour the QOC above did not weigh, found only by building
  it.

  **The RA half is now shared** via `gen.Input.SharedRA`, a generic
  (import path, package, stylable) triple `storegen` fills in with
  `factsschema/ra` — the package `codec/factswrapper` already emits into
  every keelson wire codec, so a store and a codec decoding the same row
  share one definition of its columns. `sysmetrics_store.out.go` lost 5,569
  lines. The seam is off by default and the six non-facts stores regenerate
  byte-identical.

  **The DML half stays duplicated, deliberately.** Its entity-frame control
  surface is walled by the `internal/lowlevel` import barrier
  ([ADR-0100](./0100-recordstore-generated-leeway-clickhouse-store.md) §SD6),
  and `factsschema/dml` exports that surface for the hand-written facts
  writers — so binding it would put every facts-bound store into the `Flat`
  shape ADR-0100 deferred as "wide, unguarded". That is a decision for
  ADR-0100 to revisit, not a default to change here. RA has no control
  surface, which is why only that half moved.
- SD4 depends on a proposed ADR for one seam; if ADR-0183 is rejected the
  snapshot helper is built here and the assignment golden stands alone.

### Neutral

- No change to the leeway encoding, the facts schema, the bus wire format,
  producer, or consumer. The tee is additive at every seam it touches.
- The facts codec swap ADR-0090 §SD3 anticipated for the wire stays unbuilt and
  unneeded; this ADR does not revive it.

## Migration — Tier 1

- **Breaks.** Nothing at rest. `boxer.facts` schema, DDL and existing rows are
  untouched; the tee only appends rows carrying a vocabulary nothing else
  reads.
- **Path.** Additive throughout. `storeexec` and `storegen` are new packages;
  `ExternallyProvisioned` defaults to today's behaviour, so existing generated
  stores are unaffected and regenerate byte-identical.
- **Regeneration.** `recordstore/gen` consumers regenerate to pick up the new
  option field (output unchanged unless the option is set). The new store
  generates in the gen-test lane, the `sharedsection` pattern ADR-0183 D2
  names. No FFI boundary is involved.
- **Old shape.** `loadstudy`'s `system.asynchronous_metric_log` path is kept
  working and unchanged; migrating it is a later, separate decision, and its
  documentation is corrected either way.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the vocabulary goldens, the generated-store
  shape checks and the read-surface expansion golden; the
  `//go:build integration` lane for `storeexec` **and the store round-trip**,
  against a live server.

  This is not where the plan first put the round-trip. It was written for the
  default lane over `clickhouse-local` via `recordstore/chexec`,
  skip-if-absent, and it landed in the integration lane over `chclient` +
  `storeexec` instead. The move is deliberate and was worth making: the live
  path exercises the real HTTP transport and the table
  `chstore.SetupTable` actually provisioned, which is what makes SD2's
  claim checkable rather than asserted. The cost is recorded rather than
  glossed — **the default lane carries no store round-trip**, so a machine
  with no server verifies the shapes and the schema agreement but not a
  write followed by a read.
- **What would fail.**
  - A generated-store shape test asserting `EnsureTable` is **absent** under
    `ExternallyProvisioned` and `VerifySchema` is present — the SD2 guarantee,
    which is otherwise a convention nothing checks.
  - `VerifySchema` against the DDL `chstore.SetupTable` applies: divergence
    between the two schema authors goes red here, which is the drift SD2's
    split creates.
  - The vocabulary assignment golden (SD4): an edited ordinal or a reordered
    declaration goes red; the union-with-other-vocabularies disjointness test
    catches a base-32 collision.
  - A round-trip over a synthetic `BundleSnapshot`: ingest through the tee,
    read back through the generated `Scan` verb, assert per-domain equality of
    the raw counters.
  - An `LW_GET` expansion test over the metric sections, pinned as a golden, so
    a naming-convention change is caught at the read surface too. One authored
    query per storage shape, covering every section the DTOs use; the golden
    holds the physical column names *and* the membership ids the calls expand
    into, so a re-aspected section and a re-keyed vocabulary both go red. It
    is the check the store's own round-trip cannot perform — that writes and
    reads through the same generated code, so a rename moves both sides
    together and the test stays green.
- **Gap.** SD5's index policy is declared but unapplied, so nothing verifies it
  prunes — that verification arrives with the `chstore` change its trigger
  gates. SD7's claim that no TTL preserves the analysis window is an argument,
  not a test. Live-host collector behaviour stays covered by the existing
  sysmetrics tests; this ADR adds no collector coverage. The read-surface
  golden expands the calls but does not execute them, so it demonstrates
  addressability, not that a query returns the right rows.

## Status

Proposed — awaiting review by the code owner.

- **M0 — `storeexec`: `recordstore.ExecutorI` over `chclient` (ADR-0105 D1).** ✓
  Landed as [`public/keelson/data/storeexec`](../../public/keelson/data/storeexec).
  It implements a decision ADR-0105 already accepted, so it did not wait on
  this ADR's review. `Exec` and `InsertArrow` map straight onto `chclient`;
  `QueryArrow` appends `FORMAT ArrowStream` — the *stream* format, not the file
  format the buffered `chexec` sibling reads, because the file format's trailing
  footer would force the whole response into memory before the first batch and
  defeat the iterator shape `ExecutorI` was written for.
- **M1 — `storegen`: the facts `TableDesc` plus an id snapshot into `recordstore/gen` (ADR-0105 D2, ADR-0183 D2).** ✓
  Landed as
  [`public/keelson/runtime/factsschema/storegen`](../../public/keelson/runtime/factsschema/storegen).
  Parameterized by registry rather than bound to vdd, per SD4. Two findings
  came out of it: the scaffolding duplication recorded under Consequences, and
  a `recordstore/gen` property worth lifting — the emitted component codec
  keeps the DTO's own package clause while the store takes `PackageName`, so a
  mismatch emits two packages into one directory and is discovered only when
  someone compiles the output. `storegen` gates it; `gen` is where the check
  belongs, and moving it there is a candidate change to a shared surface this
  ADR does not own.
- **M2 — vocabulary at its own tag value, assignment golden, and the cpu/mem DTOs.** ✓
  Landed as
  [`sysmvocab`](../../public/keelson/runtime/sysmvocab) and
  [`sysmfacts`](../../public/keelson/runtime/sysmfacts).
  One correction to §SD3 as written: the host token is **one membership per
  kind** (`sysmCpuHost`, `sysmMemHost`, …), not one shared `sysmHost`. A
  generated store declares each membership's kind symbol once per package and
  refuses two kinds naming the same membership — cross-kind sharing needs the
  reflect path, which a store does not use. The cost is linear in domains. The
  domain token was dropped: the kind already identifies it.
- **M3 — `ExternallyProvisioned`, the generated store, the tee, and `sysmetricsd` wiring.** ✓
  Landed as [`sysmtee`](../../public/keelson/runtime/sysmtee), the generated
  store in `sysmfacts`, and `sysmetricsd --tee` (default off).
  `loadstudy`'s package documentation is corrected in the same milestone.

  Verified against the live server, which is what makes SD2 more than an
  assertion: the generated store's `VerifySchema` agrees with the
  `boxer.facts` that `chstore.SetupTable` actually provisioned, and samples
  written through the tee read back through the store's own `Scan` verb.

  One limitation found while wiring it: `sysmetricsbus`'s consumer handler
  receives the snapshot but not the subject, and the host token lives in the
  subject. The host is therefore configured on the tee. A co-located tee knows
  it; a standalone sink spanning several hosts would need the subject in the
  handler, which is a `sysmetricsbus` change deferred until such a sink
  exists.
- **M4 — the remaining scalar domains: psi, net, disk, battery, gpu.** ✓
  Six kinds, not five: disk splits into `SysDiskMount` and `SysDiskIo` because
  the mount table and the block-device list have independent lengths, and one
  entity per aligned group keeps every array in a row the same length. The
  vocabulary reached 106 memberships; the store reached nine kinds.

  §SD3 called these domains scalar. Four of them are not — net, disk, battery
  and gpu each carry a per-item table — so they are stored column-major, one
  array element per item, which is the shape §SD3 of ADR-0090 describes for a
  process table. That introduces an **alignment contract** the ADR did not
  anticipate: index *i* of every array in a row describes the same item, and
  nothing in leeway enforces it. The arrays sit in different sections, so
  ADR-0181 §SD5's co-length audit applies within a section, not across them.
  The writer fills every array in one pass over one slice; the tests assert
  equi-length and check values by position. A violation would read back
  plausibly, pairing one interface's name with another's counters.

  Deliberately dropped: per-interface IP address lists (a list per element does
  not flatten into this shape, and addresses are nearer the §SD8 sensitive
  class than a metric) and the raw mount options string.
- **M5 — the fan-out domains: proc and sockets.** ✓
  Decided against the working store: **column-major**, as for the M4 per-item
  domains. An entity per `(host, pid, tick)` would make "this process over
  time" a key lookup instead of an array walk, but it multiplies the row rate
  by the process count — the collector caps at 256 processes, so at 1 Hz that
  is ~22M rows/day/host against ~86k. `boxer.facts` is shared with runtime
  facts and Consequences already records volume as the cost of that sharing; a
  256× multiplier on the busiest domain is not worth one query shape array
  functions can express anyway. Three kinds, taking the store to twelve.

  **A correction to §SD4's sensitivity plan, for the durable form.** §SD8 of
  ADR-0090 puts a `sensitive` membership beside each sensitive attribute's own,
  so the tag travels with the data. Two things make separation better here, and
  the command line, user name and uid/gid are their own kind
  (`SysProcCmd`, `--tee-proc-cmd`, off by default):

  - A component DTO binds **one membership per field**, so §SD8's second tag is
    unreachable from the generated write path at all.
  - §SD8's accepted exposure was scoped to *"the single-tenant, localhost-bound
    bus"*, where a command line lives as long as its subscriber. A row in
    `boxer.facts` outlives the process, is readable by anything with database
    access, and is backed up with everything else. The masking switch §SD8
    defers does not exist, so a tag today annotates without enforcing — while a
    kind a deployment never writes needs no enforcement.

  The rest of the process table is stored either way, so ADR-0126's topology
  marks and the load view are unaffected by the default.

  **Sockets are written once per observation.** That collector samples on its
  own slower cadence and consecutive bundles repeat one snapshot; a row per
  bundle would store one observation many times and put a rising Order on rows
  that never changed. The row is dated by the collection stamp rather than the
  bundle's.
- **M6 — topology, closing ADR-0126 §SD6.** ✓
  The containment tree is stored as an **adjacency list**: a pre-order walk
  numbers the nodes and each carries its parent's number. It is the one shape
  in this vocabulary that parallel arrays cannot hold, and it is recursive, so
  no fixed nesting expresses it either. A serialized blob would put the
  structure beyond SQL entirely — the opposite of what modelling metrics as
  facts is for. The node number is stored rather than left implicit in array
  position, because a query that filters the arrays loses position and the
  parent references would dangle.

  Written on first sight of a host, like the CPU descriptor: the topology is
  static, so a row per tick would be duplication rather than observation.

  ADR-0126 §SD6 said *"the ADR-0090 P5 tee covers topology domains the day it
  is built"*. That day is this one; its `sockets` domain landed in M5.
  Vocabulary 153 memberships, store thirteen kinds.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [ADR-0090](./0090-sysmetrics-pubsub-data-plane.md) — the metric data plane;
  §SD1 subjects, §SD3 the fact model, §SD4 the deferral, §SD8 sensitivity,
  §SD9 P5.
- [ADR-0105](./0105-keelson-adopts-generated-record-stores.md) — D1 the
  executor, D2 storegen, D3b the facts-bound store, D5 "next kind is generated".
- [ADR-0100](./0100-recordstore-generated-leeway-clickhouse-store.md) — the
  record-store generator this builds on.
- [ADR-0169](./0169-continuous-coverage-keelson.md) — §SD6, the tee designed
  generically and superseded in shape here.
- [ADR-0126](./0126-appliance-topology-as-data.md) — §SD6, waiting on this tee
  for topology history.
- [ADR-0181](./0181-leeway-dql-authoring-surface.md) — `LW_GET` (§SD3), the
  transform contract (§SD1), skip-index policy (§SD4).
- [ADR-0171](./0171-leeway-sql-read-surface.md) — §SD4, the missing name→id
  lookup behind SD6's caveat.
- [ADR-0183](./0183-leeway-component-consumer-simplification.md) — proposed;
  D0 explicit ordinals, D1 the snapshot helper and assignment goldens, D2
  storegen.
- [ADR-0150](./0150-timeseries-subsequence-anomaly-detection.md) — the batch
  detectors this tee feeds, and the streaming one it does not.
- [ADR-0168](./0168-capmap-business-capability-corpus.md) — §SD6, the
  tag-value allocation rule SD4 follows.
- [ADR-0148](./0148-app-workingsets.md) — the modelled-substrate invariant SD1
  rests on.
- [leeway SQL read surface](../explanation/leeway-sql-read-surface.md) — the
  read entry point for the persisted metric facts.
