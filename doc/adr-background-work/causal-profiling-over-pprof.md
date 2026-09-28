---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative.

> **Provenance.** Analysis compiled 2026-09-28, before any decision, and
> revised the same day after an independent review. Tiers: **(a)** claims
> about this repository were checked against the working tree on that date;
> **(b)** claims about the Go runtime were checked against the pinned
> toolchain's sources (ADR-0199); **(c)** claims about the literature
> summarise the cited work. This page's own reasoning is marked
> *(inference)*.

# Causal reasoning over pprof data — what boxer would need

## 1 Question and scope

A large fleet of cells runs the same Go codebase from byte-reproducible
builds (ADR-0215). Each cell has its own ClickHouse on the same machine. A
deployment agent outside this repository sets the software version per cell
and relays records to monitoring cells.

**Question.** What does boxer need so that pprof data can answer causal
questions — *what happens to latency, throughput or cost if this code
changes?* — and not only *where do the cycles go?*

The design draws on three bodies of work:

- fleet-wide continuous profiling: GWP, Kanev et al., FBDetect;
- causal profiling: Coz, Virtual Causal Profiling, BCOZ, wPerf;
- detection of hardware that runs slow without failing: Limplock,
  Fail-Slow at Scale, IASO, Perseus, CPI².

Online-experiment practice supplies the statistics: ratio metrics, CUPED,
A/A tests, switchback designs and sequential testing.

**In scope:**

- the Go processes built from this tree;
- the in-process sampler `runtime/pprof`;
- the counting, storage, experiment and analysis layers above it.

**Out of scope** (§7):

- system-wide eBPF sampling;
- ClickHouse's own profiler;
- the Rust render client;
- the deployment agent, for which boxer only states requirements.

The earlier [pprof-profiles-as-data](./pprof-profiles-as-data.md) settled
the single-capture view and deferred continuous fleet profiling. This page
takes up that deferral.

## 2 What "causal" means here

### 2.1 Estimands

A causal claim needs four things:

- a unit;
- a treatment;
- an assignment of treatment that makes groups comparable;
- an outcome.

| # | Question | Unit | Treatment | Assignment | Outcome | Causal? |
|---|---|---|---|---|---|---|
| E1 | What does this call path cost per unit of work? | cell-hour | — | — | CPU per unit | No — an upper bound |
| E2 | What did this build change? | cell × period | build | randomised rollout | CPU per unit, latency | Yes |
| E3 | Is this cell slower than its peers? | cell-hour | — | — | cost per unit of size on shared stacks | No — a comparison |
| E4 | Does this stage limit the outcome? | work unit, or cell × period | injected delay | randomised | unit latency | Yes, under §4.6's conditions |
| E5 | What is this unit waiting on? | work unit | — | — | time blocked, by cause | No — structural |

Four notes on the table:

- **E1 is a bound, and only for some questions.** It bounds the gain from
  removing a path in CPU-bound sequential work, and the per-path effect in
  E2. It does not bound E3 (a slow cell can exceed any path's share). Nor
  does it bound E4 for a stage that waits rather than computes.
- **E3 is descriptive.** Cells differ in data, neighbours and load as well
  as hardware. A difference becomes a hardware candidate only after the
  covariates and the interaction test of §4.7 A3. Until then, E3 is a
  confounder to control for in E2 and E4.
- **E4 uses latency, not throughput.** Work arrives at a cell from outside.
  Below saturation, completions per second equal the arrival rate
  whatever the code does. A delay raises latency and in-flight work (Little's
  law) but not throughput. Coz measured throughput on closed-loop benchmarks,
  where this does not arise. Throughput is therefore an E4 outcome only for
  backlog-driven work, such as stevedore drains, or on a cell that is
  measurably saturated.
- **E5 describes structure.** It says where time goes while a unit waits,
  not what would change if the wait were removed.

### 2.2 Denominators and cost

A pprof profile is a distribution over stacks. A cell that is uniformly
slower has the same profile as a healthy one. Every estimand therefore
divides by work done in the same interval, measured two ways:

- **Completions** — Coz's progress points.
- **Size** — bytes, rows or items per unit. A cell with larger tables spends
  more CPU per query on the same stacks. Dividing by size separates "this
  machine is slow" from "this machine has more to do".

Cost has three measures, and each analysis states which one it uses:

- **CPU time**, from the CPU profile. The profiler's timers count thread CPU
  time (tier b), so time spent descheduled is excluded. It detects slower
  cycles.
- **Wall time**, from per-unit latency counters. It includes waiting and
  descheduling. Comparing it with CPU time separates a slow core from a busy
  machine.
- **Allocation.** No per-goroutine counter exists, and heap and allocs
  profiles carry no labels (tier b). It is estimated per kind from labelled
  CPU samples in `runtime.mallocgc` and in GC assist, which run on the
  allocating goroutine *(inference)*.

**GC is a shared cost.** Assist runs on the allocating goroutine, so it is
charged to the unit that allocates. Background mark workers, the sweeper and
the scavenger are system goroutines without labels, so their CPU lands in an
unlabelled bucket (tier b). A1 reports the unlabelled share and splits
background GC across kinds by their estimated allocation, so the rule is
stated rather than implied.

## 3 What the tree has (tier a, 2026-09-28)

| Need | Present | Missing |
|---|---|---|
| Sampler | `runtime/pprof`, started by [`profiling`](../../public/observability/profiling/profiling.go), the `/profile` handler in [`pprofhttp`](../../public/observability/profiling/pprofhttp/pprofhttp.go) (ADR-0212), and [`imzrt`](../../apps/imzrt/imzrt_panel_profiles.go) | A continuous schedule; arbitration of the one-CPU-profile-per-process limit |
| Profile → table | [`pprofarrow`](../../public/observability/profiling/pprofarrow/pprofarrow.go): one row per unique stack | Sample labels (dropped); a stack id; build and host identity |
| Work units | [`task`](../../public/keelson/runtime/task/spawn.go) spawn and [`Handle.finishLocked`](../../public/keelson/runtime/task/handle.go) (used by watchbill and bgjob); watchbill `Worker`; [`stevedore/drive`](../../public/streaming/stevedore/drive/), [`stevedore/host`](../../public/streaming/stevedore/host/), [`stevedore/lander`](../../public/streaming/stevedore/lander/); `queryengine.DeliveryI`; [`chclient`](../../public/keelson/data/chclient/chclient.go); the render frame (ADR-0261); generated record-store `Flush` | Any pprof label or trace task; any completion, size or failure counter |
| Run identity | [`runinfo.Inst`](../../public/keelson/runtime/runinfo/runinfo.go), stored as a runtime-run fact | Binary hash; GOMAXPROCS |
| Host identity | Four separate hostname derivations (`runinfo.Init`, `runid.HostToken`, `sysmetricsbus.DefaultHostToken`, watchbill) | A stable cell id |
| Host covariates | [`sysmfacts`](../../public/keelson/runtime/sysmfacts/doc.go): CPU and topology descriptors; per-tick CPU (with per-core frequency), PSI, memory, disk, network | CPU temperature |
| Runtime metrics | [`goruntime`](../../public/observability/goruntime/collector.go) snapshot collector (ADR-0061) | Recording per profile window |
| Storage pattern | ADR-0184: bus tee → generated store on `boxer.facts`. Its skip-index policy is declared but not applied (§SD5); raw kinds have no TTL (§SD7) | — (a profile tee inherits both limits) |
| Traces | [`tracing`](../../public/observability/tracing/flightrecorder.go): flight recorder, dumped on signal; parser `golang.org/x/exp/trace` | Tasks to slice traces by; latency-triggered dumps |
| Experiments | — | Delay seams, assignment records, a control channel |
| Views | Single-capture profile books; icicle widget (ADR-0160) | Fleet and experiment views |

## 4 Design

### 4.1 Work units

A new package, called `workunit` here, gives each unit of work a kind,
labels and counters:

```go
// Kinds are registered at init in a closed registry (modelled on ADR-0009).
var KindWatchbillJob = workunit.MustRegister(workunit.Spec{
    Name:     "watchbill.job",
    SizeUnit: workunit.SizeItems,
})

// Begin labels the goroutine and returns the labelled context, which the
// caller passes to child work. End restores the labels of Begin's input.
func Begin(ctx context.Context, k Kind) (uctx context.Context, u Unit)
func (u Unit) AddSize(n int64)
func (u Unit) End(o Outcome) // completed | failed | cancelled | timedOut

func Do(ctx context.Context, k Kind, fn func(context.Context) error) error
```

**Labels.** Four keys, with values only from registries:

- `wu` — the innermost unit's kind;
- `wu_root` — the outermost unit's kind;
- `arm` — the experiment arm;
- `app` — the app instance, where there is one.

Request ids and other per-request values never become labels. pprof merges
samples by stack and labels, so every distinct value adds rows, and labels
end up in shared storage.

`wu_root` is needed because an inner label overrides an outer one of the
same key. Without it, a job's cost would exclude the queries it runs.

**Counters,** per kind and outcome, are snapshotted at each window boundary:

- units started;
- completed, failed, cancelled and timed out;
- total size;
- latency, as sum, sum of squares and a histogram.

Failed and timed-out counts guard against a change that looks faster only
because it drops slow units.

**Label semantics (tier b)** shape the API:

- A new goroutine inherits its creator's labels.
- `pprof.Do` builds its label set from the *context* it is given, and on
  return resets the goroutine to that context's labels. A goroutine that
  calls `Do` with a context derived from `context.Background()` therefore
  loses its inherited labels for good.
- There is no API to read a goroutine's current labels. Restoring them
  requires passing contexts, which is why `Begin` returns one.

Code that re-roots contexts must be changed to derive from the labelled one:

- bgjob `Runner.begin` and `Keyed.start`;
- watchbill `execute` and `settle`;
- the bus request services, which create fresh timeout contexts.

**Trace tasks.** `trace.NewTask` allocates even when tracing is off (tier b).
`Begin` opens a task only when `trace.IsEnabled()`.

**Attachment points:**

| Seam | Where | Note |
|---|---|---|
| Tasks | `task` spawn → `Handle.finishLocked` | Covers watchbill, bgjob and direct users |
| watchbill jobs | the goroutine body started by `Worker.start` | Supplies the job kind |
| stevedore | `drive` attempt → land or dead-letter; `host` handle → reply | Items landed give the size |
| Queries | `queryengine.DeliveryI.Deliver`; `chclient` `postSQL` / `doStream` | Client-side cost only |
| Bus handlers | `inprocbus.(*Inst).publish`; the `natsbus` subscribe wrapper | In-process handlers run on the publisher's goroutine, so the seam must set and restore labels |
| Render frame | `FinishServersideFrame`; around each app's `Frame` | Single goroutine: switch pre-built contexts with `SetGoroutineLabels` to avoid per-frame allocation |
| Record-store flush | the generated `Flush` | `Ingest<Kind>` only buffers |

### 4.2 Capture

A host service beside the existing sysmetrics and coverage samplers
(ADR-0169). Its rates and intervals are environment variables (ADR-0009).

**CPU profile.**

- Captured in short windows at a random phase per process, so the fleet does
  not sample in lockstep.
- Counters and runtime metrics are snapshotted at each window's start and
  end.
- The rate is 100 Hz unless set before profiling starts (tier b). That makes
  per-path comparisons data-hungry, and each experiment's power calculation
  (§4.6) has to account for it.

**Per-window ratios are not estimates.** A long unit may be sampled in one
window and complete in another. Estimates sum numerator and denominator over
at least a cell-hour before dividing. The in-flight count at each boundary
bounds the remaining error.

**Allocs, block and mutex profiles.** These are cumulative, so each window
stores the difference from the previous one. That is the same method as
`net/http/pprof` (tier b). Block and mutex profiling are off by default and
enabled by setting their rates.

**Goroutine profile.** Taken at window boundaries. It carries labels, so it
gives in-flight units per kind, which is the saturation signal E4 needs. It
pauses the process in proportion to the goroutine count.

**Runtime metrics per window,** through `goruntime`:

- GC CPU fraction;
- heap goal and memory limit;
- GC cycles;
- scheduler latency;
- GOMAXPROCS.

**Flight recorder.** Dumped when a unit's latency exceeds a high quantile for
its kind, at a limited rate.

**One CPU profiler per process** (tier b). The service owns the profiler and
lends it to on-demand captures from `imzrt` and `pprofhttp`. Windows missed
while it is lent are counted.

**Overhead is measured first** (the ADR-0169 M0 precedent):

- the schedule;
- the goroutine-profile pause;
- the flight recorder;
- labels and counters on the render frame, the hottest seam.

### 4.3 Identity

Each window carries:

- **Binary hash** — SHA-256 of the executable. Builds from the same source in
  the same build mode are byte-identical (ADR-0215; airgapped and networked
  builds differ), which is what makes stacks comparable across cells.
  Analyses key on the hash, not the revision. Two hashes for one revision and
  mode means reproducibility has broken, and those builds are not pooled.
- **Run id** — joins to the runtime-run fact. GOMAXPROCS is added there.
- **Cell id** — a stable id issued by the deployment agent, since the
  comparison is about the physical machine. It replaces the four hostname
  derivations, which remain as the fallback.
- **Active experiment arms and their registrations** (§4.6).
- **A sequence number per run,** because windows travel over an
  unacknowledged bus. Gaps measure loss. Loss matters because the busiest
  windows are the likeliest to be dropped.
- **UTC start and end times,** with durations from the monotonic clock.
  Rollups wait for a watermark before closing an hour and count late
  arrivals.

Hardware class is joined from `sysmfacts`, not repeated.

### 4.4 Stacks

pprofarrow is extended, not replaced:

- **Label columns** for the four keys.
- **`stack_fn_id`** — a hash of the root-first, inline-expanded function
  names. Using names rather than addresses keeps ids stable across builds.
  Expanding inlined frames keeps them stable when PGO changes inlining. Ids
  do change when functions are renamed, closures renumbered or generics
  re-instantiated. A2 reports the cost it could not match rather than
  dropping it.
- **A frames record** with file and line per frame, written once per build
  and stack. It serves PGO and source drill-down. Line numbers stay out of
  the id so that editing a file does not break continuity.
- **Truncation (tier b).** Profiles keep a fixed number of frames counted
  from the leaf (64 for CPU), so deep stacks lose their root. Samples at the
  limit are flagged. Labels belong to the sample, not to a frame, so `wu` and
  `wu_root` survive truncation and serve as the root for attribution.
- **A fleet allowlist of stacks.** Dropping rare stacks per window would
  record a borderline stack only in busy windows, biasing comparisons towards
  busier arms and cells.

  Instead:

  - The monitoring cells publish a versioned allowlist per build of the
    stacks with the largest fleet-wide share.
  - Cells fold the rest into one `(other)` row per kind, so totals are kept.
  - A2 and A3 use allowlisted stacks only.
  - A new build keeps all stacks until its first allowlist arrives.

### 4.5 Storage and transport

**On the cell.** Windows travel on a bus subject, `profile.{host}.window`, as
sysmetrics and coverage do (ADR-0090, ADR-0169). A tee writes them to
**`boxer.facts`** through a generated store, following ADR-0184 (decided
2026-09-28). The reason is that every analysis joins profile windows to facts
already on that table: runs, host metrics, jobs and queries.

**What that choice costs.** `boxer.facts` gives up per-kind control of
indexes and retention. This is accepted:

- **Indexes.** Skip indexes would not help. ADR-0184's are not applied, they
  would not cover these columns, and the analyses read every stack anyway.
  Reads are bounded instead by time-range pruning and by reading rollups
  rather than raw samples.
- **Retention.** `boxer.facts` has no TTL by decision (ADR-0184 §SD7), so M3
  must add retention for the profile kinds: roll up, then delete raw samples
  past the analyses' look-back.

  That has to be reconciled with §SD7 in M3's ADR. If it cannot be, the
  fallback is a separate sample table with its own sort order and TTL
  (ADR-0105 D3a).

**To the monitoring cells.** Sufficient statistics are relayed, not samples.
The analyses' unit is the cell-hour. Three rollups per build, arm, cell and
hour carry what the delta method (Deng et al. 2018), clustered inference and
A3 need.

1. **Per stack and kind:**
   - window count, including missed and late windows;
   - sums and sums of squares of CPU time, completions and size;
   - the cross-products of CPU time with completions and with size;
   - a histogram of per-window log cost per unit of size.
2. **Per kind:**
   - outcome counts;
   - latency sums, sums of squares and histogram;
   - in-flight counts at window boundaries.
3. **Per cell:**
   - the unlabelled CPU share;
   - GC CPU fraction and GC cycles;
   - host CPU;
   - the co-located ClickHouse's CPU;
   - per-core frequency;
   - PSI.

boxer defines these rollups and the kinds that receive them. The deployment
agent carries them.

### 4.6 Experiments

**Registration.** An experiment is recorded before it starts. The record
states:

- kind — rollout, delay, A/A or calibration;
- arms and assignment;
- primary outcome and guard outcomes (failed and timed-out units, crashes,
  restarts);
- cells per arm, duration, and the smallest effect the design can detect;
- the stopping rule;
- washout periods;
- the owner.

Each window lists the experiments it belongs to. Without a stopping rule,
checking results repeatedly inflates false positives (Johari et al. 2017).
Monitoring therefore uses always-valid p-values or a fixed horizon.

**Rollouts (E2).** The treatment is the build. The deployment agent must:

- assign cells at random, within hardware classes;
- run both arms over the same period;
- record the assignment.

Picking cells by availability is not random assignment.

Three statistical safeguards:

- Each cell's pre-experiment cost is a covariate that reduces variance
  (CUPED; Deng et al. 2013).
- With few cells per stratum, inference uses the wild cluster bootstrap.
- Crashes and restarts are guard outcomes, because a build that crashes
  under load loses its worst windows and looks faster.

FBDetect works mostly from observational data. A fleet with its own deployer
can randomise.

**Delay injection (E4).** Every `workunit.Begin` is a seam where a delay can
be injected. A small registry adds named inner seams, such as codec calls and
the ClickHouse round trip. Seams sit at stage boundaries, so E4 asks whether
a stage is on the critical path. Coz can ask this of individual lines; this
design cannot.

- **Two delay modes.** Neither one reproduces slower code.
  - **Spin** occupies a processor but allocates nothing. It misses the GC
    cost that slower code usually adds.
  - **Sleep** frees the processor, which changes contention.

  Together they bound the effect from both sides. A spin inside a lock also
  causes queueing that a speedup would not undo. The modes are reported
  separately.
- **Per-unit assignment.** Each unit is delayed with a small probability
  *p*. Delayed and undelayed units on one cell share processors, locks and
  queues. The contrast therefore measures the effect at intensity *p*, not
  the effect of the delay alone (Hudgens & Halloran 2008). *p* is reported
  with each estimate.
- **Per-cell assignment** uses a switchback design (Bojinov et al. 2023):
  randomised on and off periods, with a washout at each switch. A fixed split
  of cells would be confounded by E3.
- **Back-to-back experiments** on one cell are separated by a washout.
- **Control channel.** Delays are switched through a separate control
  subject family (ADR-0090 §SD5 keeps data subjects one-way). It is gated by
  capability (ADR-0026) and accepts only registered experiments, since it
  degrades production deliberately.
- **Limits enforced in-process:**
  - caps on delay, probability and share of cells;
  - automatic stop when a guard outcome is breached;
  - no injection on cells that A3 flags;
  - every sample tagged with its arm, so A2 and A3 can exclude experiment
    windows.

**What a delay result means** *(inference)*. Where a unit's latency is the
maximum over parallel paths, latency is a convex function of a stage's time.
Two things follow:

- Shortening the stage by δ gains at most what lengthening it by δ loses.
- The slowdown slope *S* is an upper bound on the benefit of a small speedup,
  and *S* ≈ 0 means a small speedup would not improve the measured outcome.

This holds only while the outcome rises steadily with the stage's time and
nothing else reacts to the delay. It fails in these cases:

- **Timeouts** remove slow units, so completed units can look faster. The
  guard counts catch this.
- **Timer-driven batching** is flat in both directions. *S* ≈ 0 then says
  nothing.
- **Controllers** — backpressure, the deployment agent, A3's own limits —
  react to slowdowns and not to speedups.
- **GC and locks** respond as described under delay modes.
- **Throughput below saturation** follows the arrival rate (§2.1).

A reported *S* ≈ 0 therefore states its mode, its outcome and the cell's
utilisation. It means "no gain from a small speedup, for this outcome", not
"this stage does not matter".

Coz slows the other threads rather than the target, and Virtual Causal
Profiling (Pourghasemi et al. 2020) recasts the method in terms of relative
speed. Either approach in Go needs a modified runtime, as Morsing's
`causalprof` shows.

**Validation.** Two experiment kinds check the pipeline before any result is
trusted, and again after changes to capture, rollup or analysis:

- **A/A.** Two arms run the same build. A2 must find no effect beyond the
  expected false-positive rate. This is the equivalent of Coz's null
  experiments; Kohavi et al. 2020 describes A/A practice.
- **Calibration.** A known delay is injected in one arm. The pipeline must
  recover it within its stated interval.

### 4.7 Analyses

Where one query suffices, an analysis is a SQL book (ADR-0132). The rest —
median polish, bootstrap, sequential tests, subtree deduplication — is a Go
package that publishes its results as ad-hoc datasets (ADR-0240) for the
same books and the icicle view.

**A1 — cost per unit of work, by call path (E1).**

- Total CPU time divided by total completions or size, per function and kind,
  over cell-hours. Recursion is counted once per stack.
- Shown beside the unlabelled share and the apportioned GC.
- Labelled as an upper bound.

**A2 — build effect (E2).**

- **What is tested.** Each function's own (exclusive) cost, clustered by
  cell, with the CUPED covariate. Inclusive changes are derived from those.
  Testing inclusive cost directly would flag every ancestor of a single
  regression.
- **Reporting.** One finding per subtree: the deepest function whose change
  explains its ancestors' change. FBDetect deduplicates correlated
  regressions for the same reason.
- **Multiplicity.** Benjamini–Hochberg controls false discoveries across
  findings.
- **Variance.** Sampling error per stack is only a floor. Most of the
  variance comes from changes in the mix of work, which the relayed second
  moments capture.

**A3 — cell comparison (E3).** For one build, over allowlisted stacks and
cell-hours:

> log(CPU per unit of size) = stack effect + cell effect + cell × stack-class
> effect + host covariates + error

- **Fit.** Median polish (Tukey), with cell effects centred on the fleet
  median.
- **Identification.** A cell is placed only if it shares enough stacks with
  enough other cells; unplaced cells are reported.
- **Covariates.** Host CPU, co-located ClickHouse CPU, core frequency and PSI
  enter the fit, not only the diagnosis afterwards.
- **The interaction term is the fail-slow signature** — for example,
  memory-bound stacks slow while others are not. A purely additive model
  would hide it. A cell is flagged on its cell effect or on any class
  interaction.
- **Persistence** is judged by a sequential rule, such as CUSUM or an
  always-valid test.

A flag names a hardware candidate, not a diagnosis. Neighbours, data growth
or configuration can remain.

Comparing CPU time with wall time separates slower cycles from waiting.
CPI² measured cycles per instruction, which is blind to cycles that became
slower. Cost per unit of work is not *(inference)*.

**A4 — stage sensitivity (E4).**

- **Outcome.** Unit latency against injected delay, per experiment and mode,
  with an interval. Throughput only where §2.1 permits.
- **Reporting.** Beside the stage's A1 share, the guard outcomes and cell
  utilisation.
- **Reading.** A large share with *S* ≈ 0 is Coz's typical finding: a hotspot
  whose speedup would not move the outcome.

**A5 — what units wait on (E5).**

- **Source.** Flight-recorder dumps, sliced by unit.
- **Output.** For each unit: time running, time waiting to be scheduled, and
  time blocked by cause, with the goroutine that unblocked it. This is
  wPerf's wait-for graph at goroutine level. Kernel-level off-CPU data cannot
  provide it for Go, because the kernel does not see parked goroutines.
- **Related work.** Critical-path tracing (Chow et al. 2014; Google, 2022) is
  the distributed analogue.
- **Scope.** Exploratory, and can be dropped without affecting A1–A4.

### 4.8 Profile-guided optimisation

Fleet CPU profiles per main package are merged into a committed
`default.pgo`, which keeps builds reproducible. PGO needs call-site lines,
which the frames record holds. M10 decides whether that is enough or whether
raw windows must be sampled and kept. Each PGO refresh is itself a rollout
experiment, measured by A2.

## 5 Milestones

Milestones marked † change a core surface and get an ADR.

| # | Deliverable | Needs |
|---|---|---|
| M0 | Overhead measurements (§4.2) | — |
| M1 † | `workunit`: registry, `Begin`/`End`/`Do`, labels, counters, conditional trace tasks; attached at the §4.1 seams; context re-rooting fixed | M0 |
| M2 | pprofarrow: label columns, stack id, truncation flag, frames record, allowlist folding | — |
| M3 † | Capture service and profiler lending; runtime metrics per window; bus subject with sequence numbers; store on `boxer.facts`; binary hash and cell id; retention; measured row rate and read cost | M1, M2 |
| M4 | A1 books per cell | M3 |
| M5 † | Rollups with watermark; allowlist computation and distribution; A3 | M3 |
| M6 † | Experiment registration; delay seams, control channel, limits; switchback scheduling | M1, M3, M5 |
| M7 | Validation: A/A and calibration pass | M5, M6 |
| M8 | A4 | M7 |
| M9 | A2, with CUPED, subtree deduplication and sequential monitoring; requirements to the deployment agent | M7 |
| M10 | PGO export and refresh | M2, M9 |
| M11 | A5 | M1 |

M1 touches the most packages. Without it, the rest can only compute
proportions. M7 gates every causal result.

## 6 Open questions and risks

- **Label discipline.** The registry rejects unknown values. Whether a lint
  is needed as well is open.
- **Size units.** Each kind's owner must pick a size measure that means the
  same thing on every cell.
- **Retention.** The profile kinds need deletion that ADR-0184 §SD7
  deliberately avoids. M3's ADR resolves this or falls back to a separate
  table.
- **Self-measurement.** The capture service and the tee appear in the
  profiles. They get their own kind so they can be seen and excluded.
- **Changing work mix.** A2 and A4 assume a stable mix during an experiment.
  Size denominators and stratifying by kind reduce this risk, but do not
  remove it.
- **Ownership.** Undecided:
  - who acts on an A3 flag;
  - what A2 result triggers a rollback;
  - who owns the delay budget.

  These must be settled before M8 and M9 produce results.
- **Privacy.** Stacks show which features a cell uses, and the `app` label
  shows who uses a shared cell. The code is public; the usage record is not.
  Rollups follow the monitoring cells' existing access grants.
- **Cell ids** must be issued by the deployment agent.

## 7 Out of scope or deferred

- **eBPF system-wide sampling** (the OpenTelemetry eBPF profiler). It would
  add kernel and non-Go stacks where allowed. It cannot replace work units or
  A5, because it sees threads rather than goroutines. It also needs
  privileges that the compartment design (ADR-0207, proposed) avoids
  granting. Its output would load into the same tables.
- **ClickHouse server cost.** ClickHouse's `system.trace_log` records sampled
  stacks per query id, and those ids join to boxer's own. It is the next
  source to add after M4, and would improve A3's co-location covariate.
- **The Rust render client,** via folded-stack import (see
  pprof-profiles-as-data).
- **Rollout scheduling, cell ids and relay** — handled by the deployment
  agent.

## References

**Profiling and causal profiling**

- Curtsinger & Berger, *Coz: Finding Code that Counts with Causal Profiling*, SOSP 2015.
- Pourghasemi et al., *Only Relative Speed Matters: Virtual Causal Profiling*, Performance 2020.
- Ahn et al., *Identifying On-/Off-CPU Bottlenecks Together with Blocked Samples*, OSDI 2024.
- Zhou et al., *wPerf: Generic Off-CPU Analysis to Identify Bottleneck Waiting Events*, OSDI 2018.
- Ren et al., *Google-Wide Profiling*, IEEE Micro 2010.
- Kanev et al., *Profiling a Warehouse-Scale Computer*, ISCA 2015.
- *FBDetect: Catching Tiny Performance Regressions at Hyperscale through In-Production Monitoring*, SOSP 2024.
- Hunter et al., *Beyond malloc efficiency to fleet efficiency*, OSDI 2021.
- Mytkowicz et al., *Evaluating the Accuracy of Java Profilers*, PLDI 2010.

**Critical paths**

- Chow et al., *The Mystery Machine*, OSDI 2014.
- *Distributed Latency Profiling through Critical Path Tracing*, ACM Queue 2022.
- Zhang et al., *CRISP*, ATC 2022.

**Slow hardware and peer comparison**

- Zhang et al., *CPI²*, EuroSys 2013.
- Do et al., *Limplock*, SoCC 2013.
- Gunawi et al., *Fail-Slow at Scale*, FAST 2018.
- Panda et al., *IASO*, ATC 2019.
- Lu et al., *Perseus*, FAST 2023.
- Lin et al., *Understanding Stragglers in Large Model Training Using What-if Analysis*, OSDI 2025.

**Experiments and inference**

- Deng et al., *Improving the Sensitivity of Online Controlled Experiments by Utilizing Pre-Experiment Data* (CUPED), WSDM 2013.
- Deng, Knoblich & Lu, *Applying the Delta Method in Metric Analytics*, KDD 2018.
- Kohavi, Tang & Xu, *Trustworthy Online Controlled Experiments*, 2020.
- Johari et al., *Peeking at A/B Tests*, KDD 2017.
- Bojinov, Simchi-Levi & Zhao, *Design and Analysis of Switchback Experiments*, Management Science 2023.
- Hudgens & Halloran, *Toward Causal Inference with Interference*, JASA 2008.
- Cameron, Gelbach & Miller, *Bootstrap-Based Improvements for Inference with Clustered Errors*, REStat 2008.
- Tukey, *Exploratory Data Analysis*, 1977.

**Production fault injection**

- Netflix ChAP and FIT.

**This repository**

- [pprof-profiles-as-data](./pprof-profiles-as-data.md)
- ADR-0009, ADR-0026, ADR-0061, ADR-0090, ADR-0105, ADR-0132, ADR-0160, ADR-0169, ADR-0184, ADR-0199, ADR-0207 (proposed), ADR-0215, ADR-0240, ADR-0261.
