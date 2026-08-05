---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative.

> **Provenance.** Analysis compiled 2026-08-05, ahead of any decision — nothing
> in here is settled; an ADR, not this page, is where any of it would become
> so. Claims are two-tiered: (a) statements about this repository were checked
> against the working tree on the compile date; (b) statements about the Go
> coverage runtime were checked against the pinned toolchain's source
> (`go1.26.5`, `runtime/coverage` and `internal/coverage`) — they hold for that
> toolchain and must be re-checked on toolchain bumps.

# Continuous Go coverage as facts — real-time acquisition and viewing inside keelson

## 1 Question and scope

What would it take to acquire Go code coverage from a *running* boxer process
continuously, store it pre-aggregated in the `boxer.facts` data model over the
bus, and view it live — fitted into keelson the way sysmetrics
([ADR-0090](../adr/0090-sysmetrics-pubsub-data-plane.md)) and the introspection
tables ([ADR-0094](../adr/0094-keelson-introspection-tables.md)) already are?

The stated requirements: program counters stored **periodically**, lookup
information stored **once per run**, stored records **pre-aggregated**,
storage in `boxer.facts` **via the bus**, data-centric throughout
([ADR-0148](../adr/0148-app-workingsets.md) invariant).

In scope: the coverage runtime mechanics, the acquisition/decoding path, the
pre-aggregation model, the bus plane, the facts modelling, live keelson
tables, and the viewing surface. Out of scope: coverage of the Rust render
client (Go instrumentation only), multi-host fleets beyond the existing NATS
bridge shape, and per-line *history* (per-line stays a live view; see §5.6).

## 2 The shape of Go binary coverage *(tier b, checked against go1.26.5)*

A binary built with `go build -cover` carries counter arrays and meta-data,
exposed at runtime through `runtime/coverage`:

- `WriteMeta(io.Writer)` / `WriteCounters(io.Writer)` — in-memory snapshots,
  no files, no toolchain needed in the running process. Both error cleanly
  when the binary was not built with `-cover`, so runtime detection needs no
  build tag.
- **Meta blob** (the lookup information): per package → per function
  (`Funcname`, `Srcfile`, `Lit`) → coverable units
  (`StLine/StCol/EnLine/EnCol/NxStmts`). Identified by `MetaFileHash
  [16]byte` — stable per *build*, shared by every run of that binary.
- **Counter blob** (the periodic half): per function `(pkgid, funcid,
  counters[numUnits])`, one `uint32` per coverable unit. Its header carries
  the same `MetaHash`, the native join key back to the lookup data. The split
  the requirements describe is exactly how the format is factored.
- **Modes**: `set` (default; one store per basic block — cheapest), `count`,
  `atomic`. `ClearCounters()` works **only** in atomic mode. Uncleared
  counters are cumulative, so in set mode coverage is a **monotone** signal —
  the property the whole pre-aggregation design leans on. Unsynchronized
  snapshot reads are benign for the covered/not-covered bit.
- **Scope**: default instrumentation is main-module packages only — for this
  repository ~498 packages, ~26k non-test functions, ~527k LOC (estimates,
  include generated code). Granularity is per-block; `cmd/go` hardcodes it,
  so aggregation to function/package level is ours to do at ingest — which is
  what "stored records are pre-aggregated" asks for anyway.
- **Formats are internal** to the toolchain but small and versioned
  (`MetaFileVersion = 1`, `CounterFileVersion = 1`); the reference decoder
  stack (`internal/coverage/{defs,decodemeta,decodecounter,stringtab,slicereader}`)
  is ~1,480 lines total. A minimal in-process decoder is a bounded write, and
  deploy targets need no Go toolchain (`go tool covdata` stays a dev-only
  convenience).

## 3 What the tree already has *(tier a)*

- **A coverage seed already exists**:
  [`public/observability/coverage`](../../public/observability/coverage/coverage.go)
  — `--coverageTrapDir` + SIGUSR1 → `WriteCountersDir`/`WriteMetaDir`, wired
  into both CLI hosts. File-dump only: no decode, no bus, no facts, no
  viewing. One defect: the flag's probe detects cover support via
  `ClearCounters()`, which also errors on `set`/`count` builds, so the trap
  refuses every non-atomic cover build; a `WriteMeta(io.Discard)` probe
  detects all of them.
- **The acquisition pattern to clone** (ADR-0090): pure data types split from
  collectors (`sysmsnap`), a collector-free bus package
  (`sysmetricsbus`: subjects, `Codec` seam — still interim CBOR — `Producer`
  over a `BundleSampler` interface, `Consumer`, `Bridge`, `LatestHolder`), a
  wiring package (`sysmscrape.StartScraper`), a carousel hook gated on a NATS
  env var, and a pure-consumer app (`imztop`) that captures `ctx.Bus()` at
  Mount. `Publish` is unaudited by design; the metric plane rides coarse
  subject grants.
- **Live introspect tables fed from in-process state** (ADR-0094):
  `Provider{Name/Schema/Freshness/Snapshot}`, constructor-injected
  dependencies (`RegisterTopology(reg, holder)` over a bus-fed
  `LatestHolder`; `RegisterWorkingsets(reg, facts)`), wired in
  `introspecthost.Start`. `keelson('x')` reaches them from play and applets.
- **The freshest facts-ingestion recipe**
  ([ADR-0168](../adr/0168-capmap-business-capability-corpus.md)): own
  vocabulary package with its own `TagValueBase` (allocation by next unused
  multiple of 16; next is 32), an encode package building
  `dml.InEntityFacts` rows (typed sections + memberships, blake3-16
  domain-prefixed natural keys, `DeriveId`), a one-method `RecordSinkI
  {InsertArrow}` sink that `chclient.Client` satisfies. Re-ingest re-states
  rows under stable keys; readers dedupe.
- **Run identity exists**: `runinfo.Inst` (run id, hostname, pid, started-at,
  VCS revision + dirty flag) and `factsstore.WriteRuntimeStart` already land
  a run row in `boxer.facts` — the "once per run" dimension has an anchor.
- **No bus→facts tee exists.** Every facts writer calls the store in-process;
  ADR-0090 P5 reserved a persistence tee and it is unbuilt. The nearest
  periodic-ingest precedent is not bus-based:
  [ADR-0115](../adr/0115-query-observability-data-plane-strategy.md)'s
  `queryrunsvc` (loopback `/pull` + ClickHouse refreshable MV, derived ids in
  a reserved band).
- **CI already measures test coverage** (`scripts/ci/gotest.sh` runs
  `-cover`) and `scripts/dev/coveragehtml.sh` renders `GOCOVERDIR` dumps via
  the toolchain; ENGINEERING_PRACTICES notes the numbers are currently
  uploaded nowhere.

## 4 Requirements mapped onto the format

| Requirement | Format/runtime fact | Consequence |
| --- | --- | --- |
| lookup info once per run | meta blob, keyed `MetaFileHash`, stable per build | ingest once per *hash* (runs of one build share it); dedupe lazily |
| counters periodically | `WriteCounters` snapshot, monotone in set mode | changed-only emission; absolute cumulative values, never deltas |
| pre-aggregated records | per-block counters, granularity not selectable at build | aggregate in the sampler: unit bits → per-function and per-package counts |
| facts + bus | no tee exists; ADR-0089 keeps bus wire ≠ ingest wire | build the P5 tee: short-key wire on the bus, `InEntityFacts` Arrow at the sink |
| data-centric | ADR-0148: modelled facts, not blobs | typed sections + memberships per field; no CBOR-in-blobArray |

## 5 Proposed shape

### 5.1 Acquisition

A process-scoped sampler (coverage is per-process, like the scraper — a host
service, not an app): every tick (env-registered interval per
[ADR-0009](../adr/0009-environment-variable-registry.md), default a few
seconds), `WriteCounters` into a reused buffer, decode, diff against the
previous snapshot, emit pre-aggregated records. Meta is decoded once at
startup. All off the render thread, on the producer goroutine (the pprof
lesson). Non-cover builds: one log line, sampler idles.

### 5.2 Pre-aggregation model (the core decision)

Store the covered **bit**, not the count — hotness is pprof's job
(`pprof-profiles-as-data.md`), coverage's signal is the 0→1 transition. With
cumulative set-mode counters the signal is monotone, so dense periodic rows
are waste; **store changes only**, as absolute cumulative values:

- **Tier 1 — run status, every tick** (~1 row/tick): totals
  (covered/total units, stmts, funcs), run id, meta hash, sample sequence.
  Gives the dense real-time curve and run liveness cheaply.
- **Tier 2 — package rollups, changed-only**: covered units/stmts/functions
  per package. Startup burst ≤ ~498 rows, steady state a few rows per tick.
- **Tier 3 — function grain, changed-only**: covered-unit count per function,
  referencing `(metaHash, pkgid, funcid)`. Boot burst = functions executed at
  init (thousands, once); afterwards only functions whose coverage deepens.
- **Meta — once per `MetaFileHash`**: one row per function (package, name,
  file, line span, unit and statement totals). ~26k rows per build.
  Per-*unit* meta is **not** persisted (≈10× more rows; see §5.6).

Absolute cumulative values make the stream safe on a lossy transport (core
NATS semantics): a dropped message is healed by the next change or by a
low-frequency full re-statement tick; readers use plain
`max(...) GROUP BY key` within a run — monotonicity means no
latest-wins/tombstone machinery, which keeps the kind append-shaped
(ADR-0105-friendly, §7).

### 5.3 Bus plane

Clone the sysmetrics split: `covsnap` (pure types) and decoding under
`public/observability/coverage/`; `coveragebus` (subjects
`coverage.{host}.sample` + wildcard, `ServiceAppId "runtime.coverage"`,
`Codec` seam, `Producer`/`Consumer`/`Bridge`/`LatestHolder`) collector-free
under `public/keelson/runtime/`; `covscrape`-style wiring package; carousel
hook beside the sysmetrics block. Per
[ADR-0089](../adr/0089-rowdml-serialization-clickhouse-native-ingestion.md)
the bus payload is a short-key codec form (interim CBOR with the same
declared swap-to-facts-codec seam sysmetrics has), **not** Arrow IPC and not
leeway physical column names.

### 5.4 Persistence tee (the new piece)

A subscriber — the first realization of the ADR-0090 P5 seam — consumes
`coverage.>` and writes `boxer.facts`, capmap-style: own vocabulary package
(`TagValueBase = 32` + disjointness test), an encode package building
`dml.InEntityFacts` rows, `RecordSinkI` sink, gated on a ClickHouse-backed
store being present (memory store ⇒ live tables only, nothing durable).
Batching is mandatory (one insert per tick is one part per tick — the
merge-pressure defect `WriteLogs` exists to avoid); flush on row/interval
thresholds. Meta ingest is lazy: first sight of an unknown `MetaFileHash`
triggers the once-per-build function-meta ingest; re-statement is idempotent
under derived ids.

### 5.5 Live keelson tables

Providers over the sampler's in-process state (constructor injection):
`coverage_status` (1 row), `coverage_pkgs` (~500 rows), `coverage_funcs`
(~26k rows), all `FreshnessLive`, empty-not-absent when the build is not
instrumented. Optionally `coverage_units` (~150–250k rows) as the line-level
lens — serving it is cheap in-process; `url()` fetches whole payloads
(ADR-0094), so it is an opt-in table with a documented cost. For consumers in
*other* processes the bus + `LatestHolder` route exists (the `keelson.procs`
precedent), deferred until wanted.

### 5.6 Viewing

An applet book (`bookcoverage`, [ADR-0132](../adr/0132-sqlapplet-sql-defined-applets.md))
over the live tables and the facts history: overview (percentages, growth
since boot), a package treemap
([ADR-0166](../adr/0166-play-treemap-panel.md); sized by statements,
coloured by coverage — whether a continuous-ratio colour mode exists or needs
a small treemap extension is an open check, capmap coloured categorically),
an uncovered-functions browser, a coverage-growth timeline from tier-1/2
facts, and joins with the godep tables ("high fan-in, low coverage"). The
facts history carries the queries the live tables cannot: run/session growth
curves, run-vs-run diffs ("what did this session cover that the test lane
didn't"), regression watch. Per-line highlighting in a codeview stays a
*live* view over
`coverage_units`; per-line **history** is deliberately out (volume), recorded
as a deferral.

## 6 Fact-kind sketch

All rows: blake3-16 domain-prefixed natural keys; ids via `DeriveId` (or a
reserved band, ADR-0115 precedent — detail to settle); `ts` = sample time;
run linkage via the run id value (`runinfo`, already a fact via
`WriteRuntimeStart`); `metaHash` as hex on the symbol section (low
cardinality per deployment).

- `coverage.func` (meta): symbol: kind, pkgPath, srcFile, metaHash;
  stringArray: funcName; u32Array: pkgid, funcid, numUnits, numStmts,
  startLine, endLine; bool: literal.
- `coverage.pkgSample`: symbol: kind, pkgPath, metaHash, runId; u32Array:
  coveredUnits, coveredStmts, coveredFuncs; (totals live on meta).
- `coverage.funcSample`: symbol: kind, metaHash, runId; u32Array: pkgid,
  funcid, coveredUnits; foreignKey → the `coverage.func` entity id (real
  id-joins instead of string joins).
- `coverage.runStatus`: symbol: kind, runId, metaHash, mode; u64Array:
  sampleSeq; u32Array: coveredUnits, totalUnits, coveredStmts, totalStmts,
  coveredFuncs, totalFuncs.

Volume estimate (to be measured in M0): meta ~26k rows per distinct build;
boot burst ~5–15k function rows once; steady interactive use likely low
thousands of rows/hour; a full dev day well under 10⁶ rows. Dev-build churn
makes meta the dominant term — lazy per-hash ingest keeps it bounded to
builds actually run.

## 7 Constraints honored, frictions to plan for

- **ADR-0148 data-centricity**: every field is a typed section under a
  membership; no serialized blobs. Satisfied by construction in §6.
- **ADR-0105**: samples and meta are append-shaped — exactly what D3b keeps
  on `boxer.facts`. No tombstones, no latest-wins state class, no
  hand-written `argMax`/cumsum read-back; within-run reads are plain `max()`.
  The history read path still means leeway physical column names in SQL —
  confine them to the book's shared CTE prelude (the ADR-0168 §SD8 caution).
- **ADR-0089**: bus wire and ingest wire stay two projections; the SoA/typed
  snapshot is the unification point.
- **Audit**: samples ride `Publish` (unaudited, like sysmetrics — a
  deliberate metric-plane property, not an oversight; routing ticks through
  `Request` would write one audit row per tick into the same table).
- **Retention**: the default engine clause has no TTL/partitioning; a
  periodic writer is what makes that gap bite. Needs an operator-facing
  stance (out of scope here, worth a line in the ADR).
- **Toolchain pinning**: the decoder refuses unknown format versions; a
  fixture-regen test (pprofarrow's `*_REGEN=1` pattern) plus an
  integration-lane check against the live toolchain guards drift across Go
  bumps.
- **Overhead is unmeasured**: set-mode instrumentation cost on the render
  loop, binary-size growth, and per-tick decode cost all need M0 numbers
  (the fps-distribution tooling is the instrument). Until measured, the
  cover lane stays opt-in (a build flag in the dev scripts, not `./tags`).

## 8 Alternatives considered

- **ClickHouse-pull instead of the bus** (ADR-0115 shape: `/pull` + refreshable
  MV): fewer moving parts, but fails the stated bus requirement, serves no
  live consumers, and couples cadence to CH availability. The bus stream
  feeds live tables, future cross-process bridging, *and* the tee at once.
- **`GOCOVERDIR` files + `go tool covdata`**: needs a toolchain on the target
  and filesystem choreography; keeps working as the dev-only HTML path.
- **Ad-hoc datasets (the pprof route, ADR-0134)**: session-scoped and
  ephemeral by design — the opposite of the stated durability requirement;
  right for profiles, wrong here.
- **Storing execution counts**: count/atomic modes turn every hot function
  into a changed row every tick and duplicate pprof's job poorly. Rejected;
  bits only.
- **Persisting per-unit rows**: ~10× meta volume and unbounded sample volume
  for a lens served better live; deferred, not refused.

## 9 Milestone cut (each descope-able)

- **M0 — lane + numbers.** Dev-script cover build of the carousel (set
  mode); measure frame-time impact, binary growth, `WriteCounters` cost; fix
  the `ClearCounters` probe defect in the existing flag. Gate: overhead
  acceptable.
- **M1 — decoder.** `covsnap` types + meta/counter blob decoding from bytes;
  committed fixtures + regen flag + toolchain-drift guard.
- **M2 — sampler.** Periodic snapshot/diff/pre-aggregate with the tier rules;
  unit tests over synthetic snapshots.
- **M3 — bus + live tables.** `coveragebus` + wiring + carousel hook;
  providers for status/pkgs/funcs; verified via `keelson('coverage_pkgs')`.
- **M4 — tee.** Vocabulary + encoder + batching subscriber into
  `boxer.facts`; idempotency and `max()`-read tests against a real store.
- **M5 — book.** `bookcoverage` applets incl. treemap and growth timeline;
  fixture-backed query gate like the capmap book.
- **M6 — extensions** (each optional): test-lane `GOCOVERDIR` ingest verb
  (unifies CI coverage into the same kinds; closes the "uploaded nowhere"
  note), `coverage_units` live table + codeview highlight, NATS bridge for
  external processes, per-scene tour attribution via atomic mode +
  `ClearCounters`.

## 10 Open questions

1. Function-grain persistence default-on (26k meta rows per distinct build,
   lazily deduped) — or package-grain only until wanted?
2. Bus codec: interim CBOR (sysmetrics precedent, Codec seam) vs. generated
   facts bus codec now?
3. Own vocabulary package at base 32 (capmap-style, no `FactsStoreI` change —
   the lean) vs. runtime vocab tail?
4. Id minting: `DeriveId` raw (capmap) vs. reserved band (ADR-0115)?
5. Retention stance for periodic sample kinds on `boxer.facts`.
6. Keep or retire the SIGUSR1 trap once the sampler exists?
7. Where the cover build lane lives (which script/env), and whether any CI
   job builds it routinely.

Related: [pprof-profiles-as-data](./pprof-profiles-as-data.md),
[ADR-0090](../adr/0090-sysmetrics-pubsub-data-plane.md),
[ADR-0094](../adr/0094-keelson-introspection-tables.md),
[ADR-0168](../adr/0168-capmap-business-capability-corpus.md).
