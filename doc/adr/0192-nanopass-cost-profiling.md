---
type: adr
status: proposed
date: 2026-08-17
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0192: nanopass cost profiling — per-pass wall clock on the pre-execute rewrite

## Context

The client-side rewrite is the dominant cost of running a large statement from
`play`, and nothing in the app says so. A measurement taken on 2026-08-16 while
diagnosing a slow applet found the server answering in **90 ms** and the client
spending **2.4–3.9 s** compiling the statement it was about to send.

The cause is structural, not a bug.
[`nanopass.Sequence`](../../public/db/clickhouse/dsl/nanopass/nanopass_pipeline.go)
hands each child the previous child's **output string**, so every pass re-parses
the statement from text. `passes.CanonicalizeFull` alone is a `Sequence` of
twelve children, four of them fixed points (≥2 parses each to detect
convergence); with the LW_GET extraction and column-handle resolution on top, a
`play` Run parses one statement on the order of **34 times**.
`BenchmarkPlayPipeline` in
[`nanopass_pipeline_bench_test.go`](../../public/db/clickhouse/dsl/nanopass_test/nanopass_pipeline_bench_test.go)
pins the ratio against a 9 KB applet buffer.

The cost is **superlinear** in expression complexity, so it arrives as a cliff
rather than a slope. Corpus calibration through the same pipeline: a 265 B
applet costs 5 ms, 681 B costs 91 ms, 2.8 KB costs 525 ms, 9.4 KB costs 3.9 s.
An author has no way to tell which edit crossed the cliff, because the pipeline
degrades silently in the other direction too — [ADR-0108](./0108-keelson-sql-pass-registry.md)
§SD3 makes every unit skip rather than fail, so a slow buffer and a fast one
look identical in the UI.

Two `play` panes already describe this rewrite and neither carries a number.
The **Passes** tab draws the [ADR-0119](./0119-imzero2-pipelineview-widget.md)
schematic of the registry stage and tints each unit by outcome; the
**Diagnostics** tab owns the prose for the same trace. Both read one memoised
`Client.RewriteTrace`, which reports *what* each unit did (applied, changed,
skipped, declined) via `passreg.ApplyObservation`. What it cannot report is what
each unit **cost**, because a `Pass` is a black box to its caller: `Pass.Run`
returns a string and an error, and `Pass.Children` describes the composite's
shape without describing any invocation of it.

That last point is what forces this decision past the leaf tier. Attributing
cost to `CanonicalizeFull` as a whole is not actionable — it is one registry
unit and always the expensive one. The actionable fact is *which of its twelve
children* is spending the time, and an earlier measurement suggests the answer
is uncomfortable: roughly **eight of the twelve rewrote nothing** on a real
buffer and still cost a full parse each. Reaching that granularity means
instrumenting the combinators, which is exported API on a Tier 1 surface.

## Design space (QOC)

**Question.** How does a caller of `nanopass.Pass.Run` obtain the wall-clock
cost of each pass invocation inside the pass tree, including invocations made by
combinators the caller did not construct?

**Options.**

- **O1 — Top-level only.** Time each registry unit around `Pass.Run` in
  `passreg`. No nanopass change.
- **O2 — Observer on `env.Environment`.** Add an observer field to the
  environment every combinator already threads, and have the combinators call
  it.
- **O3 — Rebuild the tree.** Walk `Pass.Children` and reconstruct an equivalent,
  instrumented pass tree per profiled run.
- **O4 — Recorder keyed by the run's environment.** Keep the observer in
  `nanopass`, in a side table keyed by the `*env.Environment` pointer that
  `Pass.Run` mints, and look it up at the existing recursion points.

**Criteria.**

- **C1 — Granularity.** Does it reach inside a composite it did not construct?
- **C2 — Fidelity.** Does it measure the code path that actually executes, or a
  re-derivation of it?
- **C3 — Layering.** Does each package keep its own business?
- **C4 — Cost when off.** What does an un-profiled Run pay?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | ++ | +  | ++ |
| C2 | ++ | ++ | −− | ++ |
| C3 | ++ | −  | +  | +  |
| C4 | ++ | +  | ++ | +  |

O3 fails C2 outright and that is disqualifying rather than merely negative.
`Pass.Apply` closes over its members independently of `Pass.Children` — the
struct documents this — and the members it closes over carry data the struct
does **not** record: `FixedPoint`'s `maxIter`, `Validating`'s grammar,
`Conditional`'s predicate. A rebuilt tree would therefore be a *different*
pipeline reporting on itself, which is exactly the self-oracle a profiler must
not be.

O2 and O4 are the same mechanism differing only in where the observer is
parked. `env.Environment` holds SQL regions — settings, params, format, prelude
comments — and a pass-cost observer is not one; parking it there would also
oblige the `env` package to define the shape of a pass invocation, which it has
no other reason to know about. O4 keeps that vocabulary in `nanopass` at the
cost of an indirection through a side table, which is the smaller loss.

## Decision

We will add **opt-in cost profiling to `nanopass`**, exposed as
`Pass.RunProfiled`, which returns the tree of pass invocations one `Run`
performed with a wall-clock duration, a fixed-point iteration count, and a
changed/failed verdict on each node. The recorder is found at the existing
recursion points (`applyWithProps`, `runFixedPoint`) through a side table keyed
by the `*env.Environment` that `Run` mints, so no `ApplyFunc` signature changes
and no pass is aware it is being measured.

`passreg.ApplyObservation` will carry that tree per unit, and `play` will render
it in the two panes that already describe the rewrite: the **Passes** tab tints
a slow unit on the schematic and shows the measured tree under the selection,
and the **Diagnostics** tab owns a new *Rewrite cost* section, drawn as
staggered bar charts (§SD7).

The warning fires at **250 ms** for the whole rewrite of one buffer. That is
below the 525 ms the 2.8 KB corpus entry measures and above the 91 ms the 681 B
one does, so it separates the buffers that are on the cliff from the ones that
are not.

### SD1 — Wall clock, not CPU time

The number is wall clock. It is what the author waited for, it is what makes a
buffer feel broken, and the alternative would understate a rewrite that blocks
on nothing but still takes four seconds. Its weakness is the usual one: a
descheduled goroutine inflates it, and this measurement is taken on the render
thread (§SD3), where the only other work in the frame is the rewrite itself.

### SD2 — Profiling is opt-in and free when off

`Pass.Run` is unchanged and installs nothing. `applyWithProps` consults an
atomic counter of live profilers before touching the side table, so a Run with
no profiler attached pays one relaxed atomic load per pass invocation — on the
order of 34 loads against a rewrite measured in milliseconds. The execute path
is never profiled; only `RewriteTrace`, which the panes already call, is.

### SD3 — The trace stays on the render thread, and says so

`PlayApp.rewriteTraceFor` computes the trace synchronously in the frame that
needs it, memoised by buffer. This ADR does not move it. The consequence is
worth stating plainly rather than hiding: on a buffer whose rewrite costs
seconds, the UI is blocked for those seconds when the buffer settles, and the
number the pane then reports *is* the freeze the author just experienced. That
sentence is in the heading badge's tooltip rather than the pane body — §SD7
keeps the section wordless — but it is stated, not implied.

Moving the trace off the render thread — the `armColumnDiag` pattern of a
goroutine, a generation counter, and a poll — is **deferred**. It is a
responsiveness change, it adds a concurrency seam and a "measuring…" state to
both panes, and it is better made once the numbers say which buffers need it.

### SD4 — The trace is an equivalent re-run, not the shipped one

`RewriteTrace` runs its own rewrite and discards the statement; the body that
ships comes from a separate `BuildStatement` call. The two run the same code
over the same input, so the *outcomes* cannot drift — that property is what
[ADR-0108](./0108-keelson-sql-pass-registry.md)'s trace already rests on — but
the *durations* describe the trace's run, not the shipped one. Two ways they
differ, both surfaced in the pane rather than papered over:

- The first parse in a process pays roughly **1.1 s extra** for a cold ANTLR
  DFA cache ([ADR-0084](./0084-nanopass-antlr-dfa-cache-bounding.md)), so a first
  measurement can be dominated by a cost no later run pays.
- A concurrent Run on another lane competes for CPU with the trace.

Recording timings from the shipped rewrite instead was considered and rejected
for the pre-flight case: the warning is most useful *before* the author runs a
statement, and a shipped-rewrite timing does not exist until after.

### SD5 — The tree does not account for the whole unit, and says so

A unit's duration is its whole `Run`; the tree measures the pass invocations
inside it. The env round trip — extracting the `SET` prelude, integrating the
rewritten body — sits between the two, and it is not a rounding margin. Measured
against the 9 KB fixture: `CanonicalizeFull` cost **1.99 s** as a unit while its
twelve members summed to **1.64 s**, leaving ~344 ms (17%) outside the tree.

The pane names that remainder on its own line rather than folding it into the
parent, because a unit whose cost is in the env round trip is a different
problem from one with a slow member.

### SD6 — Thresholds are declared in source

`250 ms` (whole rewrite) and `100 ms` (one unit or step worth naming) are
constants in `play`, not environment variables. They are a property of what a
human perceives as a stall, not of a deployment, and a knob would make the
warning mean different things in different sessions. A test pins them so a
silent drift is a red lane rather than a quieter warning.

### SD7 — The Diagnostics section is a staggered bar chart, not prose

The section answers two questions and is shaped by them: *why is this slow*, for
whoever is waiting, and *which pass is to blame*, for whoever can fix it. Both
are read fastest from a picture, so the pane draws and does not explain.

It is the form a browser's network **Timing** panel uses, and for the same
reason: these phases are strictly sequential, so where a bar *starts* is
information, not decoration. Two tiers on one shared scale:

- **The run split** — `compile` / `server` / `transfer + decode`. This is usually
  the entire answer. A statement that compiles for half a second against a server
  answering in four milliseconds has a client problem, and no amount of SQL
  tuning will touch it.
- **The rewrite waterfall** — one bar per unit, staggered, sitting directly under
  the `compile` span it decomposes, with the costliest unit expanded to its top
  sub-passes.

Colour carries the verdict — the bar's length says how long, its hue says
whether the time bought a rewrite — so a pass that re-parsed the statement and
changed nothing is visibly grey among the blue. The only standing prose is a
three-word legend; the rest of what used to be paragraphs now lives in the
heading badge's tooltip, and a hovered bar swaps the legend for one caption line.

Two things the chart must not do, both learned by drawing them wrong first. A
collapsed "… 9 more" row gets **no bar**: those passes are interleaved in time
with the rows above, so any single span drawn for them places work at a time it
did not happen. And the run tier is **dropped entirely** when `compile + server`
exceeds the run's own elapsed — the compile figure is this measurement's rewrite
rather than the Run's (§SD4), the two genuinely differ, and a compile bar longer
than the run it claims to sit inside would be the most misleading thing on the
pane.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API — `public/db/clickhouse/dsl/nanopass` | added: `StepCost`, `Pass.RunProfiled` | nothing; `Pass.Run` keeps its signature and behaviour |
| Exported Go API — `public/keelson/data/passreg` | added: `ApplyObservation.Dur`, `ApplyObservation.Cost` | `ApplyBestEffortBoundObserved` calls `RunProfiled` on the observed path only |
| Pipeline stage contract — nanopass combinators | `applyWithProps` and `runFixedPoint` gain a recorder lookup | every combinator (`Sequence`, `FixedPoint`, `Validating`, `Conditional`) is measured through the two existing recursion points, so none of them changes |

## Alternatives

- **Time only the registry units (O1).** Rejected: `CanonicalizeFull` is one
  unit and always the expensive one, so the report would name the pass the
  author cannot act on and hide the twelve children they could.
- **Observer field on `env.Environment` (O2).** Rejected: the environment models
  SQL regions, and the field would oblige `env` to define what a pass
  invocation is.
- **Rebuild an instrumented pass tree (O3).** Rejected on fidelity: `Children`
  does not record `maxIter`, the validating grammar, or a conditional's
  predicate, so the rebuilt tree would not be the one that runs.
- **Count parses instead of timing them.** A counter at `nanopass.Parse` — the
  single grammar-1 chokepoint — would name the real unit of work more directly
  than milliseconds do. Rejected for now because the counter is process-global
  and `play` runs lanes concurrently, so a delta sampled around one unit is
  polluted by any other goroutine parsing at the same time. Attributing parses
  per run needs the same recorder this ADR introduces, and can be added to it
  later.
- **An `IMZERO2`-style environment knob for the threshold.** Rejected per §SD6.
- **A sankey for the cost breakdown** (total → unit → rewrote/unchanged).
  Rejected on two counts. The rewrite is strictly sequential with no branching or
  merging, so every ribbon carries the whole flow and has the same width —
  the encoding's one variable is constant, and it conveys nothing. And a sankey
  has no time axis, so it cannot show *when* a pass ran, which is precisely what
  makes the staggered form diagnostic (§SD7).
- **Prose ranking the units.** This is what shipped first and it was replaced.
  A ranked list answers "which is biggest" but not "how much of the whole is
  this" or "what else was running", and it took a paragraph of theory to make
  the numbers mean anything. The chart answers all three without a sentence.

## Consequences

### Positive

- The cliff becomes visible before a Run, at the granularity of a sub-pass:
  which child of `CanonicalizeFull` is spending the time, how many fixed-point
  iterations it took, and whether it changed anything for that cost.
- A sub-pass that costs a full parse and rewrites nothing is now legible as
  exactly that, which is the evidence any future memoisation of the parse would
  be argued from.
- `passreg` consumers other than `play` inherit the per-unit duration for free —
  the field is on the observation every observed apply already produces.

### Negative

- `nanopass` grows a side table keyed by pointer identity. It is confined to the
  package and invisible to pass authors, but it is action at a distance and a
  reader of `applyWithProps` has to be told where the recorder comes from.
- A pass that spawns a goroutine and calls back into the tree from it would
  corrupt the recorder's frame stack. No pass does this today; the recorder
  documents the assumption rather than defending against it.
- The measurement is of the trace, not of the shipped rewrite (§SD4), and the
  first one in a process can be dominated by DFA warm-up.

### Neutral

- The un-profiled path gains one atomic load per pass invocation.
- `play`'s Passes tab keeps its schematic labels free of numbers. A duration in
  a stage label would re-key the layout cache on every keystroke that changed a
  timing, so the numbers live in the text below and the schematic carries only
  a tint.

## Migration — Tier 1

- **Breaks.** Nothing. Both API changes are additive: a new method alongside
  `Pass.Run`, and new fields on a struct whose existing fields keep their
  meaning. Existing `ApplyObservation` literals and consumers compile unchanged.
- **Path.** Nothing to migrate. A consumer wanting costs switches
  `Pass.Run` → `Pass.RunProfiled` or reads the new fields; one that does not,
  does nothing.
- **Regeneration.** None — no generated code, no FFI boundary.
- **Old shape.** `Pass.Run` is kept indefinitely. It is the right call for every
  caller that does not want a profile, and profiling must stay opt-in for §SD2
  to hold.

## Verification plan — Tier 1

- **Lane.** Default `go test`. In `nanopass`: a composite pass whose children
  sleep by known amounts, asserting the returned tree's shape, per-node
  ordering, and fixed-point iteration counts, plus a test that `RunProfiled` and
  `Run` return byte-identical output for the same input. In `passreg`: that an
  observed apply populates `Dur` and `Cost` and that an un-observed one still
  takes the unprofiled path. In `play`: threshold classification, and the chart
  MODEL — that bars stagger in run order, that a unit which never ran gets no
  bar, that the costliest unit expands to its top sub-passes with the remainder
  folded into a bar-less summary row, and that the run split never draws a
  negative or over-long span. The model is pure, so all of that is asserted
  without a renderer.
- **Second lane — the screenshot tour.**
  `scripts/dev/play-screenshot-tour.sh` gained `12_passes_cost` and
  `13_diagnostics_cost`, both on one deliberately expensive buffer
  (`slow_rewrite_buffer`: twelve chained CTEs over `numbers()`, no fixture,
  measured at ~545 ms). They are what covers the WARNING path — the amber unit
  on the schematic, the badge, and the sub-pass breakdown — none of which a
  synthetic observation exercises end to end. The Diagnostics scene captures
  twice, scrolling to the culprit line, because it sits below the pane fold.
  The buffer is under a kilobyte on purpose: it is the standing demonstration
  that this cost tracks expression complexity rather than length.
- **What would fail.** A combinator added later that invokes a child by a route
  other than `applyWithProps` would silently drop that subtree from the tree; a
  shape assertion over a nested composite catches it. A threshold edited without
  intent turns the pinning test red.
- **Gap.** The tests assert *shape and ordering*, not absolute durations —
  timing assertions are flaky under CI load and under `-race`, whose multiplier
  on this package is roughly 4.6×. That the numbers are *accurate* rests on
  `time.Since` and is not independently verified. The tour scenes are captures,
  not assertions: nothing fails if a faster machine drops the buffer under
  250 ms and the warning stops appearing — the scene would quietly become a
  picture of the cheap path. Nor is the render-thread behaviour of §SD3 covered
  by a lane; it is a documented property, visible in a screenshot, not a tested
  one.

## Status

Proposed — awaiting review.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0108](./0108-keelson-sql-pass-registry.md) — the pre-execute pass
  registry; §SD3 is the degrade-rather-than-fail rule, and its 2026-07-27 update
  added the per-unit observation this extends.
- [ADR-0119](./0119-imzero2-pipelineview-widget.md) — the pipelineview
  schematic the Passes tab draws.
- [ADR-0084](./0084-nanopass-antlr-dfa-cache-bounding.md) — the bounded DFA cache
  whose cold state dominates a first parse.
- `BenchmarkPlayPipeline` in
  [`nanopass_pipeline_bench_test.go`](../../public/db/clickhouse/dsl/nanopass_test/nanopass_pipeline_bench_test.go)
  — the re-parse ratio this ADR makes visible in the UI.
</content>
</invoke>
