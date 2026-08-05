---
type: adr
status: proposed
date: 2026-08-05
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0169: continuous coverage — live Go code coverage as keelson data

## Context

boxer measures test coverage in CI and uploads it nowhere
(ENGINEERING_PRACTICES §CI), and a running boxer process reports nothing about
which of its code has executed. Wanted: continuous acquisition of Go coverage
from a *running* process and a live view of it, fitted into keelson the way
sysmetrics ([ADR-0090](0090-sysmetrics-pubsub-data-plane.md)) and the
introspection tables ([ADR-0094](0094-keelson-introspection-tables.md)) are.
Requirements: counters stored periodically, lookup information stored once per
run, stored records pre-aggregated, storage in the `boxer.facts` model over
the bus, data-centric throughout ([ADR-0148](0148-app-workingsets.md)).

The Go runtime meets this halfway (claims checked against the pinned
toolchain, go1.26.5; re-check on bumps). A binary built with `go build -cover`
exposes `runtime/coverage.WriteMeta(io.Writer)` / `WriteCounters(io.Writer)` —
in-memory snapshots, no files, no toolchain in the running process, clean
errors on uninstrumented builds. The **meta blob** (per-function file, line
span, coverable units) is stable per build under a `MetaFileHash [16]byte`;
each **counter blob** carries the same hash as its join key — precisely the
periodic/once-per-run split required. Default scope is main-module packages
(~498 packages, ~26k non-test functions here); granularity is per-block and
not selectable, so aggregation is ours to do. In `set` mode (the default,
cheapest) uncleared counters make coverage a **monotone** signal. The blob
formats are toolchain-internal but small and versioned; the reference decoder
stack is ~1,480 lines.

In the tree today: a coverage seed exists —
[`public/observability/coverage`](../../public/observability/coverage/coverage.go)
traps SIGUSR1 and dumps meta+counter files, wired into both CLI hosts. Its
flag probes cover support via `ClearCounters()`, which errors on `set`/`count`
builds too, so the trap refuses every non-atomic cover build. There is no
decode, no bus, no facts, no viewing. The acquisition pattern to mirror is
sysmetrics (pure-types package, collector-free bus package with a codec seam,
wiring package, carousel hook, pure consumers); live introspect tables take
constructor-injected in-process state; the freshest facts-ingestion recipe is
capmap's ([ADR-0168](0168-capmap-business-capability-corpus.md)); run
identity exists (`runinfo` + `WriteRuntimeStart`). No bus→facts tee exists —
ADR-0090 reserved one as P5 and it is unbuilt.

## Design space (QOC)

**Question.** What is the pre-aggregated representation of "what has run" —
on the wire, in the live tables, and (later) in the stored facts?

**Options.**

- **O1** — Covered-unit **sets** as roaring bitmaps (uint32) over a
  per-build global unit index; scalar rollups derived from them.
- **O2** — Scalar rollups only (covered counts per function/package).
- **O3** — Raw counter arrays (per-unit execution counts).

**Criteria.**

- **C1** — Fidelity: does line-level "did this run" survive aggregation?
- **C2** — Volume under continuous sampling.
- **C3** — Model fit: modelled data (ADR-0148), existing codec/marshall reach.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | ++ | −− | ++ |
| C2 | +  | ++ | −− |
| C3 | ++ | +  | −  |

O1 wins. Coverage's signal is the 0→1 transition, so sets of covered units
are the lossless pre-aggregation of counter arrays; monotonicity makes
changed-only emission sound; and `*roaring.Bitmap` is already "a Set of
uint32 in the canonical model"
([marshallreflect](../../public/semistructured/leeway/marshall/go/marshallreflect/shape.go)),
round-tripped by the keelson bus codec fixture — the representation is
native, not imported. O2 discards the per-line lens for little volume gain
over run-length-friendly bitmaps; O3 stores hotness, which is pprof's job
([pprof-profiles-as-data](../adr-background-work/pprof-profiles-as-data.md)),
at unbounded periodic volume.

## Decision

### SD1 — Opt-in cover build lane

Coverage builds are a dev-script/env lane (`go build -cover`, `set` mode),
not an entry in `./tags`. The sampler detects instrumentation at runtime by
probing `WriteMeta` against a discarding writer and idles with one log line
otherwise — no build tag. The existing `--coverageTrapDir` probe switches
from `ClearCounters()` to the same `WriteMeta` probe, fixing its refusal of
non-atomic builds. Instrumentation overhead (frame time, binary size,
per-tick snapshot cost) is measured in M0 before anything ships further.

### SD2 — In-process decoder, pinned to the toolchain

A minimal reader for the meta and counter blob formats (both version 1),
refusing unknown versions. No `go tool covdata`, no toolchain on deploy
targets. Committed fixtures with a regen flag, plus an integration-lane check
that decodes blobs emitted by the live toolchain, guard format drift across
Go bumps.

### SD3 — Covered sets as roaring bitmaps over a global unit index

Meta enumeration order defines a per-`MetaFileHash` global unit index
(uint32); each function owns the contiguous range `[unitBase,
unitBase+numUnits)`. The covered state of a run is **one cumulative
`*roaring.Bitmap`**; a tick's emission is the delta bitmap (`AndNot` against
the previous snapshot), and a low-frequency full re-statement heals losses on
the fire-and-forget bus. Values are absolute covered sets, never increments —
a dropped message is repaired by the next re-statement, and readers never
integrate. Scalar rollups (per-function, per-package, per-run
covered/total) are derived by popcount over meta ranges, emitted alongside
for cheap dashboards. Execution counts are not represented.

### SD4 — Bus plane mirrors sysmetrics

A collector-free `coveragebus` package: subjects `coverage.{host}.sample`
(wildcard `coverage.>`), `ServiceAppId "runtime.coverage"`, a `Codec` seam
with an interim CBOR codec (the sysmetrics position; roaring serializes
natively inside it), a `Producer` sampling a snapshot interface, `Consumer`,
`Bridge`. The sampler is a process-scoped host service (coverage is
per-process, like the scraper — not an app), started from the carousel
beside the sysmetrics block, ticking on an [ADR-0009](0009-environment-variable-registry.md)-registered
interval, decoding and diffing off the render thread. Samples ride
unaudited `Publish`, the deliberate metric-plane property ADR-0090 §SD8
established.

### SD5 — Live keelson tables

Providers over the sampler's in-process state, constructor-injected:
`coverage_status` (one row: run, meta hash, mode, totals, sample sequence),
`coverage_pkgs`, `coverage_funcs` — all `FreshnessLive`, empty-not-absent on
uninstrumented builds. The per-line lens (units of a file with covered
bits, from bitmap + meta) is served live and on demand; it is cheap
in-process and deliberately has no stored form yet.

### SD6 — Facts modelling contract; persistence deferred

The stored form is recorded now so the wire anticipates it, and **built in a
separate session** (user-directed deferral): a subscriber tee — the first
realization of ADR-0090 P5 — consuming `coverage.>` and writing
`boxer.facts` capmap-style (own vocabulary at the next free TagValue base,
encoder over `dml.InEntityFacts`, `RecordSinkI` sink, batched inserts).
Kinds: `coverage.func` (once per `MetaFileHash`: package, name, file, line
span, `unitBase`, unit/stmt totals; ingested lazily on first sight of an
unknown hash), `coverage.funcSample` (changed-only covered set, stored as a
modelled set-of-uint32 section — not serialized-roaring bytes in a blob
column, per ADR-0148), `coverage.pkgSample` and `coverage.runStatus`
(scalar rollups). All kinds are append-shaped with plain `max()`/set-union
reads — no tombstones, no latest-wins machinery (the class ADR-0105 wants
gone). Nothing in M0–M4 depends on this piece.

### SD7 — Viewing

An applet book ([ADR-0132](0132-sqlapplet-sql-defined-applets.md)) over the
live tables: overview percentages, a package treemap
([ADR-0166](0166-play-treemap-panel.md), sized by statements, coloured by
coverage ratio — whether a continuous colour mode exists there is an open
check), an uncovered-functions browser, and joins against the godep tables.
Growth timelines and run-vs-run diffs are history queries; they arrive with
SD6.

### Milestones

- **M0** — Cover lane script + measured overhead numbers (recorded here as an
  Update); `--coverageTrapDir` probe fix. Gate: overhead acceptable.
- **M1** — Decoder + fixtures + drift guard.
- **M2** — Sampler: snapshot, bitmap diff, rollups; unit tests over synthetic
  snapshots including the re-statement rule.
- **M3** — `coveragebus` + carousel wiring + the three live providers,
  verified via `keelson('coverage_pkgs')`.
- **M4** — `bookcoverage` applets over the live tables, fixture-gated like
  the capmap book.

## Surfaces — Tier 1

New: `public/observability/coverage/covsnap` (pure types) and decoder/sampler
files beside the existing seed; `public/keelson/runtime/coveragebus` and a
wiring package; provider registrations in `introspecthost`; a carousel hook;
one env var (interval; plus the lane's build recipe in the dev scripts); the
applet book. Changed: the `--coverageTrapDir` probe. Untouched:
`FactsStoreI`, the facts schema (set sections exist via the canonical-model
set modifier; re-verify when SD6 is built), `./tags`, all app manifests
except viewers that subscribe.

## Alternatives

- **`GOCOVERDIR` files + `go tool covdata`** — needs a toolchain and file
  choreography on the target; stays as the dev-only HTML path.
- **ClickHouse-pull instead of the bus** (ADR-0115 shape: loopback `/pull` +
  refreshable MV) — fewer moving parts, but fails the bus requirement,
  serves no live consumers, couples cadence to CH availability.
- **Ad-hoc datasets** ([ADR-0134](0134-adhoc-datasets.md), the pprof route) —
  session-scoped and ephemeral by design; wrong for a durability requirement.
- **Scalar-only or raw-counter storage** — rejected in the QOC.
- **Signal-trap-only status quo** — no decode, no aggregation, no view;
  whether the trap is retired once the sampler exists is left open.

## Consequences

### Positive

- Live coverage joins the SQL reach: treemaps, uncovered browsers, and
  godep joins over `keelson('coverage_*')` with no new render surface.
- The monotone-set representation makes continuous storage cheap
  (changed-only, run-length-friendly) and reads trivial (`max()`, unions).
- SD6, when built, realizes the reserved ADR-0090 P5 seam and closes the
  "coverage uploaded nowhere" note via a later test-lane ingest.

### Negative

- Instrumentation overhead on the render loop is unmeasured until M0; the
  lane stays opt-in regardless.
- The decoder couples to internal toolchain formats; the drift guard turns
  breakage into a red test, not into silence, but the coupling is real.
- When SD6 lands, history reads mean leeway physical column names in SQL
  (confine to a book prelude), and the facts table's missing TTL/partition
  stance starts to matter.

### Neutral

- Go side only; the Rust render client is out of scope.
- Coverage is process-scoped; per-app attribution inside the carousel is not
  claimed (a per-scene mode via `atomic` + `ClearCounters` is a deferral).

## Migration — Tier 1

Purely additive; no data or schema migration. The `--coverageTrapDir` probe
fix is a behaviour change only in that `set`/`count` builds stop being
refused — previously they could not use the flag at all.

## Verification plan — Tier 1

Decoder: fixture round-trips + the integration-lane live-toolchain decode.
Sampler: synthetic-snapshot diff tests (first tick, deepening, re-statement,
uninstrumented no-op). Bus/tables: an end-to-end gate publishing synthetic
samples and querying `keelson('coverage_pkgs')` through the introspect
engine. M0 produces the overhead numbers (fps distribution before/after, an
instrumented binary's size delta) recorded as a dated Update. The book gets
the capmap-style fixture query gate.

## Deferrals

- **SD6 build** — the bus→facts tee, vocabulary, encoder, and history
  applets: separate session (user-directed).
- Test-lane `GOCOVERDIR` ingest verb into the same kinds.
- Per-line codeview highlighting (live tables already carry the data).
- NATS bridge for external processes' coverage.
- Per-scene tour attribution (`atomic` mode + `ClearCounters`).
- Retention/TTL stance for periodic sample kinds — travels with SD6.
- Retiring the SIGUSR1 trap.

## Status

Proposed, 2026-08-05.

## References

- [ADR-0090 — sysmetrics pub/sub data plane](0090-sysmetrics-pubsub-data-plane.md)
- [ADR-0094 — keelson introspection tables](0094-keelson-introspection-tables.md)
- [ADR-0132 — SQL-defined applets](0132-sqlapplet-sql-defined-applets.md)
- [ADR-0148 — app workingsets (data-centricity Update)](0148-app-workingsets.md)
- [ADR-0168 — capmap corpus as `boxer.facts`](0168-capmap-business-capability-corpus.md)
- [ADR-0089 — row-DML wire vs CH ingestion](0089-rowdml-serialization-clickhouse-native-ingestion.md)
- [ADR-0115 — query observability data plane](0115-query-observability-data-plane-strategy.md)
- [pprof profiles as data (background)](../adr-background-work/pprof-profiles-as-data.md)
