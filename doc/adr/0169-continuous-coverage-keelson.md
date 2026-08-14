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
not selectable, so aggregation is ours to do. Runtime counter snapshots are
an atomic-mode capability — `WriteCounters` refuses `set`/`count` builds
(only `WriteMeta*` works in every mode) — so the lane builds
`-covermode=atomic`. Counters are never cleared, which makes coverage a
**monotone** signal. The blob formats are toolchain-internal but small and
versioned; the reference decoder stack is ~1,480 lines.

In the tree today: a coverage seed exists —
[`public/observability/coverage`](../../public/observability/coverage/coverage.go)
traps SIGUSR1 and dumps meta+counter files, wired into both CLI hosts. Its
flag probes cover support via `ClearCounters()` — the right acceptance set
(the trap's `WriteCountersDir` is atomic-only too) but a side-effecting
probe: it resets whatever counters the process accumulated before flag
parsing, and its error text names the wrong mode. There is no decode, no
bus, no facts, no viewing. The acquisition pattern to mirror is
sysmetrics (pure-types package, collector-free bus package with a codec seam,
wiring package, carousel hook, pure consumers); live introspect tables take
constructor-injected in-process state; run identity exists (`runinfo` +
`WriteRuntimeStart`).

A bus→facts tee now exists. When this ADR was drafted ADR-0090's P5 was still
reserved and unbuilt, and capmap's hand-written encoder
([ADR-0168](0168-capmap-business-capability-corpus.md)) was the freshest
facts-ingestion recipe; §SD6 was written against both facts. ADR-0184 has
since built P5 as `runtime/sysmtee` over a generated store, which is the
recipe to mirror instead — see §SD6.

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

Coverage builds are a dev-script/env lane (`go build -cover
-covermode=atomic` — the only mode whose counters can be snapshotted at
runtime), not an entry in `./tags`. The sampler detects support at runtime
by probing `WriteCounters` against a discarding writer and idles with one
log line otherwise — no build tag, and no counter reset: the seed's
`ClearCounters` probe had the same acceptance set but cleared accumulated
counters as a side effect. Atomic increments are the costliest
instrumentation flavor, which is why overhead (frame time, binary size,
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
separate session** (user-directed deferral): a subscriber tee consuming
`coverage.>` and writing `boxer.facts` under its own vocabulary at the next
free TagValue base.

**How it writes is no longer this ADR's to specify.** The prescription here
was capmap-style — a hand-written encoder over `dml.InEntityFacts` into a
`RecordSinkI` sink — which is the code class [ADR-0105](./0105-keelson-adopts-generated-record-stores.md)
exists to delete, and its D5 says the next kind lands as a generated store.
[ADR-0184](./0184-sysmetrics-persistence-tee.md) §SD1 settled the shape
against a working consumer and supersedes it: a facts-bound store generated
through `runtime/factsschema/storegen`, bound externally-provisioned so
`chstore` stays the sole author of the `boxer.facts` DDL (§SD2). The
machinery did not exist when this section was written and does now.

Two things that shape carries in, neither of them optional. The store bakes
its membership ids at generation time, so the vocabulary has to be minted
before the store is generated, not alongside it. And ADR-0090's P5 seam is
already realized — by sysmetrics, not by this — so the coverage tee is the
second consumer of a built path rather than the first proof of an idea.

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

- **M0 — Cover lane script + measured overhead numbers.** ✓ Recorded here as an
  Update; `--coverageTrapDir` probe fix. Gate: overhead acceptable.
- **M1 — Decoder + fixtures + drift guard.** ✓
- **M2 — Sampler: snapshot, bitmap diff, rollups.** ✓ Unit tests over synthetic
  snapshots including the re-statement rule.
- **M3 — `coveragebus` + carousel wiring + the three live providers.** ✓
  Verified via `keelson('coverage_pkgs')`.
- **M4 — `bookcoverage` applets over the live tables.** ✓ Fixture-gated like
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
- **Sampling-derived coverage (a pprof adaptor)** — near-zero overhead and
  needs no instrumented build, but it estimates the CPU-time distribution,
  not set membership: a function's detection probability is roughly
  1 − e^(−cpuTime·sampleRate), so µs-scale handlers, init and error paths —
  the cold code coverage questions target — are precisely what a 100 Hz
  profile never sees. Rejected as a substitute; kept as a possible
  complement (Deferrals).
- **Signal-trap-only status quo** — no decode, no aggregation, no view;
  whether the trap is retired once the sampler exists is left open.

## Consequences

### Positive

- Live coverage joins the SQL reach: treemaps, uncovered browsers, and
  godep joins over `keelson('coverage_*')` with no new render surface.
- The monotone-set representation makes continuous storage cheap
  (changed-only, run-length-friendly) and reads trivial (`max()`, unions).
- SD6, when built, closes the "coverage uploaded nowhere" note via a later
  test-lane ingest. It no longer has to prove the ADR-0090 P5 seam — ADR-0184
  built that, so this rides a path with a live consumer instead of being its
  first realization.

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
  claimed (a per-scene mode via `ClearCounters` between scenes is a
  deferral — the lane is already atomic).

## Migration — Tier 1

Purely additive; no data or schema migration. The `--coverageTrapDir` probe
fix changes no acceptance behaviour — atomic-only stands, since
`WriteCountersDir` demands it — it only stops the probe from resetting
accumulated counters and corrects the error text.

## Verification plan — Tier 1

Decoder: fixture round-trips + the integration-lane live-toolchain decode.
Sampler: synthetic-snapshot diff tests (first tick, deepening, re-statement,
uninstrumented no-op). Bus/tables: an end-to-end gate publishing synthetic
samples and querying `keelson('coverage_pkgs')` through the introspect
engine. M0 produces the overhead numbers (fps distribution before/after, an
instrumented binary's size delta) recorded as a dated Update. The book gets
the capmap-style fixture query gate.

## Deferrals

- **SD6 build** — the bus→facts tee, vocabulary, generated store, and history
  applets: separate session (user-directed).
- Test-lane `GOCOVERDIR` ingest verb into the same kinds.
- Per-line codeview highlighting (live tables already carry the data).
- NATS bridge for external processes' coverage.
- Per-scene tour attribution (`ClearCounters` between scenes).
- Retention/TTL stance for periodic sample kinds — travels with SD6.
- Retiring the SIGUSR1 trap.
- A pprof-derived `observed` kind for uninstrumented runs — profile stacks
  folded onto the same global unit index (the pprofarrow pipeline already
  exists), labeled as hotness visibility and never claimed as coverage.

## Update (2026-08-05) — M0 measured

M0 landed: `scripts/dev/cover-build.sh` (atomic lane; `BOXER_COVERLANE_COVERPKG`
narrows the instrumented set), the `--coverageTrapDir` probe moved to a
side-effect-free `coverage.ProbeRuntimeSupport()` (`WriteCounters` against a
discarding writer; positive path verified on a live instrumented binary —
note `--help` alone short-circuits before flag actions, probe via a
subcommand), and a second seed defect surfaced and fixed: `SetupSignalTrap`
created its output directories with `os.MkdirAll(dir, os.ModeDir)` —
permission bits 000, so creating the nested directory always failed; now
`0o755`. Trap writes now log duration and the underlying error.

Measured (one dev machine, go1.26.5, warm build cache):

- **Binary growth**: `boxer` 125.2 → 131.4 MB (+4.9%); `imzero2` host
  85.6 → 91.2 MB (+6.5%).
- **Real cardinality**: the `boxer` binary instruments **80,295 units /
  141,285 statements**; meta blob 1.34 MiB (once per build); the counter
  blob after a `--help` run is **9.7 KiB** — counter emission skips
  never-executed functions, so periodic payloads scale with exercised code,
  not binary size.
- **Atomic instrumentation on leeway hot loops**: dml `BuildBatch`
  0.65 → 2.3 µs/row at N=10k (≈3.3×) and 1.8 → 2.9 µs/row at N=100;
  `AppendCommit` 0.65 → 2.3 µs (≈3.6×); readaccess scalar access
  8.8 → 66 ns (≈7.5×); container iteration 0.14 → 0.52 µs (≈3.7×).
  Small-N rows were noisy (one inverted); the steady-state rows carry the
  signal.
- **The narrowing lever works**: the same worst-case benches with
  `-coverpkg=./public/keelson/...` (hot path uninstrumented) run at
  baseline speed exactly (648 ns / 8.8 ns). Cost is strictly
  per-instrumented-package.

The atomic requirement is a deliberate upstream restriction, not an
accident: all counter-related `runtime/coverage` APIs were restricted to
atomic mode over relaxed-memory-model soundness (non-atomic counter reads
can observe reordered stores), though only `ClearCounters`' package doc says
so — the `WriteCounters` restriction lives in the source. Practitioner
prior art on real-time Go coverage (periodic `WriteCountersDir` from a
long-running service) uses the same build shape and publishes no overhead
numbers — hence measuring them here.

**Gate: still open.** The hot-loop factors are far above the "few percent"
folklore and settle one thing — full-tree atomic instrumentation is a
diagnosis lane, never an always-on default, and SD1's opt-in stance stands.
The remaining M0 question is wall-clock frame time in a live GUI session
(the Go side may not be hot-loop-dominated); that needs the
fps-distribution comparison on an instrumented carousel. M1 (decoder) does
not depend on that run.

## Update (2026-08-05, later) — M1 decoder landed

`covsnap` (the pure decoded model: `MetaProfile` with the §SD3 global unit
index assigned at decode, `CounterSnapshot`) plus the pinned decoders
`DecodeMeta`/`DecodeCounters` beside the seed. Bounds-checked throughout:
every prefix truncation and every single-byte flip of the fixtures decodes
to an error, never a panic or an unbounded allocation; unknown format
versions are refused; both counter flavors (ULEB128, raw u32) and both
endiannesses are handled. Fixtures come from a two-package probe module the
test builds itself (`BOXER_COVDECODE_REGEN=1` regenerates them after a
toolchain bump); the integration-lane drift guard builds the probe with the
live toolchain and reconciles unit, statement, covered-unit and
covered-statement totals against `go tool covdata textfmt`. Validated at
scale on the M0 boxer-binary blobs: 367 packages, 80,295 units / 141,285
statements, 2,697 / 4,977 covered — digit-identical to the covdata
numbers, with all 1,107 executed functions of the 9.7 KiB counter blob
resolving against the meta.

## Update (2026-08-05, third) — M2 sampler landed

`covsnap` gained the emission model — `Update` carries the
absolute-cumulative contract of §SD3 (first fold and every
`RestateEvery`-th fold are complete re-statements; delta ticks carry only
newly covered units and changed rollups; an unchanged tick is a pure
`RunStatus` heartbeat) — and with it the roaring dependency. `Accumulator`
is the pure, clock-free fold engine (fully unit-tested on synthetic
snapshots, including monotonicity under regressed counters and concurrent
readers under the race detector); a nonzero-count fast path skips the
per-unit walk for unchanged functions, sound because counters are
monotone. `Sampler` wires the engine to `runtime/coverage`, refuses
non-atomic builds at construction, and exposes the raw meta blob (for the
§SD6 tee), status and covered set (for the §SD5 providers). The
integration lane runs the real sampler inside a live instrumented probe
binary — only the probe module is instrumented, so the sampler observes
without self-observation — and checks the delta invariant
`covered₁ + |delta| = covered₂` end to end.

## Update (2026-08-06) — M3 bus plane and live tables landed

`coveragebus` mirrors sysmetricsbus (subjects `coverage.{host}.sample`
under `runtime.coverage`, interim-CBOR `Codec` seam with the covered set
travelling as roaring serialization, `Producer` over an `UpdateSampler`
interface, `Consumer`, the ADR-0009-registered `IMZERO2_COVERAGE_INTERVAL`
knob — non-positive disables, unparsable falls back to the default rather
than silently switching the lane off); `covscrape` wires the concrete
sampler to it and reuses the metric plane's host-token rule. The three
§SD5 tables serve over a constructor-injected `CoverageSourceI`:
`coverage_status`, `coverage_pkgs` (zero-covered rows included, so
"uncovered" is a WHERE clause, not an anti-join), `coverage_funcs`; a nil
source yields empty tables, never absent ones. `covsnap.AggregateCovered`
recomputes the rollups from a covered set in one two-pointer pass and is
cross-checked against a full re-statement of the accumulator. The carousel
starts the sampler beside the sysmetrics block behind the typed-nil guard.
Verified headless: codec round-trips, a producer→inprocbus→consumer
end-to-end under real capability enforcement, provider row-level tests,
and the host-level empty-without-sampler gate. The literal
`keelson('coverage_pkgs')` check on a live instrumented carousel rides
with the still-open M0 fps run (same session, same build);
`doc/env-vars.md` regeneration for the new knob is left for the next
quiet-tree regen.

## Update (2026-08-06, later) — M4 book landed

`bookcoverage` is the sixth applet book (TopicObservability): cov-overview
(the cumulative totals as one column), cov-map (the import-path tree as an
ADR-0166 nodes-contract treemap — module prefix trimmed via a new
`module_path` column on `coverage_pkgs`, subtree totals computed by prefix
contribution rather than a join, coverage rendered as seven qualitative
brackets because a continuous ratio ramp is still the open ADR-0166 check;
`size_by = 'uncovered'` turns the map into a work list), and cov-uncovered
(the function-grain work list with `pkg`/`show` knobs). The gate mirrors
the capmap book: corpus assertions, a six-book mint (25 applets), and
every buffer executed verbatim through the introspect engine over a
fixture `CoverageSourceI`, asserting the trim/root/bracket behaviour and
the browser's three populations. All milestones M0–M4 are now built; open
remain the fps-gate GUI run (with the live `keelson('coverage_pkgs')`
check) and the deferrals — the §SD6 tee first among them.

## Update (2026-08-06, third) — the fps gate ran; M0 is closed

Live A/B on a headless-weston desktop pair (software GL, vsync off,
`--launch imztop` as a continuously animating workload, one contended dev
box — read the numbers as one rig's measurement, not a benchmark):
baseline Go frame work sampled 5.4–9.1 ms (median ≈ 6 ms) at fps
p50 60; the full-tree atomic build sampled 17.6–20.3 ms (≈ 3.2×) at fps
p50 27. The frame path is leeway/widget hot-loop code, so the live factor
lands almost exactly on the M0 microbenchmark's 3.3×/row. **Verdict: the
opt-in stance stands as designed** — a full-tree atomic session stays
interactively usable for diagnosis (~27 fps here) and is not a daily
driver; `-coverpkg` narrowing is the lever when smoothness matters.

The same run closed the deferred live checks. The sampler came up on the
instrumented binary (`coverage.{host}.sample`, 5 s cadence) and
`keelson('coverage_pkgs')` answered over the introspect `/query` endpoint
with real data — 318 instrumented packages, 305 touched, 16,881/132,163
statements (12.8%) after ~30 s of imztop — with `coverage_status` ticking
monotonically between reads (samples 4→9). The M4 book ran live:
`--launch "subject_alias = 'cov-map'"` rendered the treemap over the
session's own coverage — 368 cells / 132.2k statements, all seven
brackets, the module-trimmed tree, landing on the Treemap tab — with the
`observability` subtree visibly brightest because the coverage pipeline
covers itself. Every ADR milestone M0–M4 is now built and live-verified;
the deferrals (§SD6 tee first) are what remains.

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
