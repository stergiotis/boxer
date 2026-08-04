---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative.

> **Provenance.** Analysis compiled 2026-08-01, ahead of any decision — nothing
> in here is settled, and an ADR, not this page, is where any of it would
> become so. Claims are two-tiered: (a) statements about this repository were
> verified against the working tree on the compile date (paths and line counts
> cited); (b) statements about the pprof wire format and Go runtime profiling
> behaviour come from general knowledge of `profile.proto` and `runtime/pprof`
> and were **not** re-verified upstream — treat the specifics (stack-depth
> truncation limits, default sample types) as pointers to check during
> implementation, not citations.

# pprof profiles as data — exploring Go profiles inside the keelson app system

## 1 Question and scope

What would it take to make pprof profiles — CPU, heap, goroutine, block,
mutex; captured from this process or loaded from a file — renderable and
explorable **inside** the keelson app system, SQL-first: profiles as tables a
`play` buffer can query, packaged as SQL applets
([ADR-0132](../adr/0132-sqlapplet-sql-defined-applets.md)), with new `play`
panels or widgets only where SQL genuinely cannot carry the view.

In scope: the data model, the parse dependency, the serving transport, the
capture/ingest path, the rendering ladder, and a milestone cut. Out of scope
(recorded as deferrals in §10): profiling the Rust render client (the
`cargo flamegraph` wrapper in
[`application.go`](../../public/thestack/imzero2/application/application.go)
already covers it externally), continuous fleet profiling, and durable
profile history.

## 2 The shape of a pprof profile *(tier b)*

A pprof profile is one gzipped protobuf message (`profile.proto`, a small and
frozen schema):

- **sample types** — the value dimensions, each `(type, unit)`. CPU profiles
  carry `samples/count` + `cpu/nanoseconds`; heap profiles carry four
  (`alloc_objects`, `alloc_space`, `inuse_objects`, `inuse_space`).
- **samples** — the rows: a stack as a list of location ids (**leaf-first**),
  one value per sample type, optional string/numeric labels.
- **locations → lines → functions** — a location may expand to *several*
  lines (inlining); functions carry display name, filename, start line.
- **mappings** — memory ranges, relevant for native frames only.

Two properties matter for the design. Go self-profiles are **pre-symbolized**
— function names ride in the file, so no symbolization step or external
binary is needed. And the runtime **truncates deep stacks** (a few tens of
frames), so converters and queries must tolerate truncated roots.

## 3 What the tree already has (verified 2026-08-01)

The striking finding: **every stage except two already exists.** Capture,
transport, cataloguing, parameterized applets, launch handoff, and a
call-graph renderer are all in the tree; what is missing is the
profile→Arrow converter and (optionally) hierarchy-native views.

| Stage | What exists | Where |
| --- | --- | --- |
| Capture | `--pprofCpuOutputFile` (file) and `--pprofHttpListenAddress` (`net/http/pprof`), behind the `boxer_enable_profiling` build tag — which is **active** in [`./tags`](../../tags) | [`public/observability/profiling/`](../../public/observability/profiling/profiling.go) |
| Runtime dashboard | `imzrt` reads `runtime/metrics` in-process; its ADR already routes pprof concerns to the profiling package | [ADR-0061](../adr/0061-imzero2-imzrt-go-runtime-dashboard.md) |
| Serve as table (live) | introspection providers: Arrow schema + `Snapshot(Projection)`, pulled by ClickHouse via `url()` or fed as in-process temp tables; queried as `keelson('name')` | [ADR-0094](../adr/0094-keelson-introspection-tables.md), [`runtime/introspect/`](../../public/keelson/runtime/introspect/introspect.go) |
| Serve as table (published) | ad-hoc datasets: `PublishRequest(bus, {Alias, ArrowIPCStream})` → `keelson('<handle>')`; encrypted at rest; republish bumps a revision on the *same* handle; catalog table `keelson('adhoc')` (handle, alias, publisher, rows, bytes, revision, created-at); quotas 256 MiB/dataset, 1 GiB store; Arrow `List(T)` maps to `Array(T)` | [ADR-0134](../adr/0134-adhoc-datasets.md), [`runtime/adhocdata/`](../../public/keelson/runtime/adhocdata/service.go), [`structure.go`](../../public/keelson/runtime/adhocdata/structure.go) |
| Open on the data | launch requests: a producer publishes, then opens `play` seeded with SQL referencing the handle | [ADR-0135](../adr/0135-app-launch-requests.md) |
| Package as applet | committed markdown books (frontmatter manifest + first `sql` fence), params strip with server-side binding, read-class security gating | [ADR-0132](../adr/0132-sqlapplet-sql-defined-applets.md), [`apps/sqlapplet/book/`](../../apps/sqlapplet/book/recent-queries.md) |
| Call-graph view | the layered-graph result panel adopts `edges` / `vertices` CTEs by name (tab slug `graph`) | [ADR-0129](../adr/0129-play-layered-graph-panel.md), [`play_layeredgraph_panel.go`](../../apps/play/play_layeredgraph_panel.go) |
| Hierarchy widget | a treemap widget with drill navigation, ancestor-keyed coloring, and value labels — **no `play` panel exposes it** (~2.8k non-test lines) | [`widgets/treemap/`](../../public/thestack/imzero2/egui2/widgets/treemap/treemap.go) |
| Duration-bounded work | `bgjob` + job-progress widgets — the right shape for a 10–30 s CPU capture | [`runtime/bgjob/`](../../public/keelson/runtime/bgjob/) |

Adjacent but not load-bearing: the ImPlot port
([ADR-0149](../adr/0149-implot-core-port-painter-lane.md)) is a cartesian
core (lines/bars/heat/pie) — a flame view is a partition layout, not an
ImPlot item. The timeline widget
([ADR-0043](../adr/0043-imzero2-timeline-widget.md)) is genuinely
time-typed (tick rulers, `time.Duration` LOD); bending its axis into a
value-domain icicle would fight it.

**Defect found while reading** *(tier a)*: the capture stop path never fires.
[`profiling.go`](../../public/observability/profiling/profiling.go) declares
the flag as `pprofCpuOutputFile`, but `ProfilingHandleExit` (which both CLI
hosts call on exit) checks `context.IsSet("cpuProfileFile")` — a name no flag
registers — so `pprof.StopCPUProfile()` is never called. `runtime/pprof`
serializes the profile *at Stop*, so the flag as shipped produces an unusable
(empty) file. One-line fix plus a test; it is M0 below regardless of any
other decision here. *(Fixed since — commit `ff2965e6`.)*

## 4 What is actually missing

1. **A pprof→Arrow converter** — the pivot piece. Pure Go, no UI, testable
   against golden fixtures.
2. **A producer** — something allowed to cause a capture. Applets are gated
   to read-class queries by design (ADR-0132), so *capture cannot be an
   applet action*; it must be an app affordance, a bus service, or a CLI
   verb.
3. **The applet books** — pure SQL once (1) and (2) exist.
4. ~~Optionally, a **treemap result panel**~~ — *dropped 2026-08-04, see §9 R1.*
5. Optionally, a **flame/icicle widget + panel** — the only genuinely new
   render surface in the whole plan.

## 5 Data model — the load-bearing decision

### O1 — denormalized samples with stack arrays *(recommended)*

One table per capture, one row per unique stack: `stack Array(String)`
**root-first** (inline-expanded display names), plus denormalized helper
columns so common queries never touch array positions:

| column | type | notes |
| --- | --- | --- |
| `stack` | `Array(String)` | root-first frame names |
| `leaf` | `String` | last stack element, denormalized for `GROUP BY` |
| `pkg` | `String` | leaf's Go package path, converter-derived |
| `value` | `Int64` | the kind's conventional default sample type |
| per-kind extras | `Int64` | e.g. CPU: `samples_cnt`; heap: `inuse_objects`, `alloc_space`, `alloc_objects` (`value` = `inuse_space`) |
| profile constants | | `kind`, `captured_at_unix_us`, `period`, `duration_ns` — repeated per row so every dataset is self-describing |

Wide (per-type columns) rather than long (`sample_type` rows) so applet SQL
reads `value` unqualified; long form doubles-to-quadruples rows and forces a
`WHERE sample_type =` into every query. The cost is per-kind schemas, hence
per-kind applet fences — acceptable for committed books, revisit if it
grates.

What this shape buys, verbatim ClickHouse over `keelson('<handle>')`:

```sql
-- top functions, self cost
SELECT leaf AS fn, sum(value) AS self
FROM keelson('h_cpu') GROUP BY fn ORDER BY self DESC LIMIT 50
```

```sql
-- top functions, cumulative; arrayDistinct guards recursive double-count
SELECT fn, sum(value) AS cum
FROM (SELECT arrayJoin(arrayDistinct(stack)) AS fn, value FROM keelson('h_cpu'))
GROUP BY fn ORDER BY cum DESC LIMIT 50
```

```sql
-- call graph, rendered by the graph tab's edges-CTE convention (ADR-0129)
WITH edges AS (
    SELECT p.1 AS source, p.2 AS target, sum(value) AS weight
    FROM (
        SELECT arrayJoin(arrayDistinct(
                   arrayZip(arrayPopBack(stack), arrayPopFront(stack)))) AS p,
               value
        FROM keelson('h_cpu'))
    GROUP BY source, target ORDER BY weight DESC LIMIT 200)
SELECT * FROM edges
```

```sql
-- callers of one function; {fn:String} renders in the applet params strip
SELECT p.1 AS caller, sum(value) AS w
FROM (SELECT arrayJoin(arrayDistinct(
                 arrayZip(arrayPopBack(stack), arrayPopFront(stack)))) AS p,
             value
      FROM keelson('h_cpu'))
WHERE p.2 = {fn:String}
GROUP BY caller ORDER BY w DESC
```

```sql
-- diff of two captures (handles are literals; §8 explains why)
WITH a AS (SELECT leaf, sum(value) AS v_before FROM keelson('h_before') GROUP BY leaf),
     b AS (SELECT leaf, sum(value) AS v_after  FROM keelson('h_after')  GROUP BY leaf)
SELECT leaf AS fn, v_after - v_before AS delta, v_before, v_after
FROM a FULL OUTER JOIN b USING (leaf)
ORDER BY abs(delta) DESC LIMIT 50
```

Focused subtrees are `WHERE has(stack, {fn:String})`; the classic folded
export is `arrayStringConcat(stack, ';')`. Every query is a single `SELECT`,
which is all grammar1 parses — the shape fits the applet grammar by
construction.

### O2 — normalized four-table mirror of the proto

Faithful (samples / locations / functions / mappings), but every exploration
query becomes joins over array *positions*, which ClickHouse makes painful
and applet authors will not write. Also: an ad-hoc publish is one table per
handle, so one capture would shatter into four handles. **Rejected** as the
primary shape; a converter can later emit an optional `frames` side table
(function → file, start line) if source-line drill-down is wanted.

### O3 — folded-text ingestion as an *additional input*

Brendan Gregg's `frame;frame;frame count` format is a five-line parser away
and widens the intake to `perf script`, `py-spy`, and anything else that
folds — including, eventually, the Rust client. Cheap sidecar to O1's
converter, same output shape; worth doing opportunistically, not a
replacement.

## 6 Parsing — the dependency question

- **P1 `github.com/google/pprof/profile`** *(recommended)* — the canonical
  parser/writer: gzip handling, string tables, inline-line expansion, legacy
  format fallbacks, profile merge, all maintained upstream. Self-contained
  package (stdlib-only imports). One new direct dependency, inside the
  existing govulncheck + osv-scanner posture.
- **P2 hand-rolled decoder** — `profile.proto` is small and frozen; a few
  hundred lines of varint walking plus goldens. Zero new deps, full
  sovereignty, but re-implements the string-table/inlining subtleties P1 has
  already survived, and forgoes merge and legacy-format handling.
- **P3 `protocompile` + `dynamicpb` over already-present deps** — the most
  machinery for the least ergonomics. Rejected.

P1 vs P2 is a genuine judgment call for the design dialogue; this page leans
P1 because the converter's correctness burden (inlining, truncation, sample
aggregation) is exactly the part P1 amortizes.

## 7 Serving — which transport

**Ad-hoc datasets (ADR-0134) fit captures** *(recommended)*: a profile is an
event, not process state — ephemeral, quota-bounded, encrypted at rest,
catalogued in `keelson('adhoc')`, and **republishable onto the same handle**
(revision bump), so a "re-capture" refreshes every open view whose SQL names
that handle. Size is comfortable: a busy 30 s CPU capture is single-digit MB
of Arrow *(estimate — measure in M1)* against a 256 MiB per-dataset cap.

**Live introspection providers** (`keelson('pprof_heap')` snapshotting on
query) fit only the instantaneous kinds — a CPU capture has a duration and
cannot live inside `Snapshot()`. A live heap provider is a plausible later
convenience; it is not the backbone.

**Durable CH storage** (compare across days/versions) is deliberately
deferred: it drags in the storage doctrine (retention, identity, who writes
schema) and none of the exploration value below needs it. Profiles saved as
files re-enter through the ingest verb whenever history is wanted manually.

## 8 Capture and handoff

**Producer affordance in `imzrt`** *(recommended)*: capture buttons per kind
— instantaneous kinds publish immediately; CPU runs as a `bgjob` with
progress, then publishes. On completion the producer issues an ADR-0135
launch request opening `play` (or the profile applet) seeded with SQL that
splices the fresh handle. This is exactly the producer pattern the ad-hoc
ADR's adopters already exercise. `imzrt` today only *reads* the runtime —
whether a capture affordance belongs there or in a tiny dedicated producer
app is a question for the dialogue; the plumbing is identical either way.
The producer holds one handle per kind and republishes onto it, so seeded
buffers and saved applet params stay valid across re-captures.

**CLI ingest verb** for files (`app pprof-publish <file.pb.gz>` on the
existing CLI host): parses, publishes, prints the handle — covers profiles
from other machines, CI, or older saved captures. *(Adjusted during M2,
2026-08-01: the ad-hoc publish subject lives on the in-process bus, so a
separate CLI process cannot reach it; file ingest needs an in-app affordance
(filepicker + fsbroker caps) or a bus bridge, and is deferred with that
note rather than shipped as a verb that couldn't publish.)*

**A seam friction worth recording**: `keelson('…')` table-function arguments
are resolved by rewrite *before* server-side parameter substitution, so a
handle must be a **literal** in the buffer — an applet cannot take "latest
`pprof_cpu` dataset" as a param default, and the diff applet takes two
pasted handles. Mitigations: the catalog query (`SELECT handle FROM
keelson('adhoc') WHERE alias = 'pprof_cpu'`) makes handles one copy-paste
away, and stable per-kind handles (above) mostly hide the problem. A real
fix — alias-latest resolution such as `keelson('alias:pprof_cpu')` — is a
small, self-contained follow-up worth its own mini-dialogue.

## 9 Rendering ladder

- **R0 — zero new UI** *(free today)*: tables, detail, projection for
  top/callers/diff; the `graph` tab for the call graph via the `edges` CTE.
  One thing to verify while authoring the book: how edge *weight* can be
  encoded (the panel exposes stroke/fill override hooks; worst case the book
  ships top-N edges and puts weight in the label).
- **R1 — treemap result panel** — ~~*(cheap, high value for heap)*~~
  **dropped 2026-08-04, after R2 shipped.** The proposal was a thin adapter
  over the existing widget, adopting a column convention (`path
  Array(String)` + `value`, or a `tree` CTE) in the style of the kanban and
  graph panels, at an estimated 400–700 lines against cost anchors of kanban
  533 and layered-graph 632. What it was *for* was drill-navigable "heap by
  package / by call prefix".

  **Why it is dropped rather than deferred.** R2 landed first and answers
  that question on the same input: a folded `stack` + `value`, which is what
  a treemap panel would have adopted too. Between the two forms the icicle
  is the better fit for this data, not merely an alternative — it keeps the
  depth and the sibling order of a path, which is what a *stack* profile is
  about and precisely what a treemap discards (ADR-0160's own opening
  observation). A treemap panel would therefore be a second, weaker lens
  over the identical query, and the tab it occupies is not free: every
  applet negotiates every result panel per frame.

  This is a judgment about *profiles*, not about treemaps. The widget keeps
  its uses — `imztop`'s Proc Map is one — and a hierarchy whose reading
  really is "what is big" with no meaningful path order (disk usage by
  directory, say) is a live reason to revisit. Nothing here blocks that; it
  would just want its own motivation rather than this ladder's.
- **R2 — flame/icicle widget + panel** *(the real thing, and the only heavy
  piece)*: the canonical stack-profile view — horizontal partition layout
  over the same folded trie a treemap consumes (the hierarchy model is
  shareable), painter-drawn depth rows, click-to-re-root, hover with
  self/cum, breadcrumb. Anchor: the treemap widget alone is ~2.8k non-test
  lines; a first flame cut with zoom/hover/labels is plausibly 1.5–2.5k plus
  a ~300-line panel, and warrants its own ADR (id-scope discipline, painter
  budget, interaction contract). Explicitly descope-able: R0+R1 already make
  profiles *explorable*; R2 makes them *pleasant*.

## 10 Milestones (each independently landable)

| # | Deliverable | Depends on |
| --- | --- | --- |
| M0 | Fix the CPU-profile stop-path flag mismatch (+ test) | — |
| M1 | Converter package (suggested home: `public/observability/profiling/pprofarrow`): parse (P1) → O1 Arrow stream; golden fixtures incl. one recursive and one inlined stack; invariant: total value conserved | P1/P2 decision |
| M2 | Producers: `imzrt` capture affordance (bgjob CPU; instant heap/goroutine/block/mutex) → publish + launch-request handoff; CLI ingest verb; producer caps (adhoc publish, open request) | M1 |
| M3 | Applet books: `profile-top` (table+detail, `{fn:String}` param), `profile-callgraph` (`tabs: [graph, table]`), `profile-diff`; committed beside the existing dogfood books | M1–M2 |
| ~~M4~~ | ~~Treemap result panel + `profile-treemap` applet~~ — **dropped 2026-08-04** (§9 R1) | — |
| M5 | Flame widget + panel (own ADR) | R2 dialogue |

*(M5 shipped 2026-08-04, and cost less than R2 costed it. The widget and the
`play` panel came from [ADR-0160](../adr/0160-imzero2-icicle-flamegraph-widget.md)
on the ImPlot custom-item lane rather than from this ladder — which corrects
§3's aside that "a flame view is a partition layout, not an ImPlot item": an
icicle **is** cartesian, x the value domain and y depth. What M5 needed here
was therefore only a book, `profile-flame`, plus `Tab: icicle` on imzrt's
Explore seed. The projection is the converter's own output rescaled — ns to
ms, bytes to MiB — because the reader scales by SI prefix and not by unit.
M4 was dropped the same day, unstarted: the icicle answers what R1's treemap
panel was for, on the same input and keeping the path order a treemap
discards. The kill reasoning stays in §9 R1 rather than being deleted with
the rung, since the widget itself is not what was rejected. With that, the
ladder is closed — every rung either shipped or has a recorded reason not
to.)*

*(M3 shipped 2026-08-01 with two adjustments: the graph book's tab slug is
`network` — the ADR-0129 panel's id in today's play — and `profile-diff`
became `profile-heap` (in-use vs churn from the heap profile's own four
measures). A generic two-capture diff needs two live handles of the same
kind, which the republish-onto-one-handle design deliberately does not
keep; it moves to the deferred list pending a capture-pinning UX. The
books bind their declared aliases at open via the new `adhoc.resolve` —
the §8 alias-latest seam, landed as ADR-0134's 2026-08-01 update.)*

*(Follow-up, 2026-08-01: binding only at open made "capture first, then
open" an unenforced ordering that a window on the wrong side of could not
recover from — a later capture left the open applet bound to nothing,
failing every Run with `unknown keelson table "pprof_cpu"`. Unresolved
aliases are now retried after open and the window names what it is waiting
for; see ADR-0134's second 2026-08-01 update. A push notification instead
of the retry is recorded as deferred there.)*

Deferred, recorded here so they don't gate: durable history (§7); continuous
/ fleet profiling (the [ADR-0090](../adr/0090-sysmetrics-pubsub-data-plane.md)
scraper pattern is the obvious template when wanted); folded-text ingest
(O3, opportunistic); pprof labels (needs a `Map` column story); source-line
drill (O2's `frames` side table); Rust-client profiles (via O3 once a folded
exporter exists).

**Recommended first slice: M0–M3.** It reaches "capture a profile from the
running system, explore it as tables and a call graph, diff two captures" —
with zero new render surface and one new Go dependency as the only
non-obvious cost.

## 11 Open questions for the design dialogue

*(All six were settled between 2026-08-01 and 2026-08-04; the answers are
recorded with the milestones that took them, and are summarised inline below
so this list is not read as still-open.)*

1. P1 (`google/pprof` dep) vs P2 (hand-rolled decoder) — §6.
   *P1, taken at M1.*
2. Does the capture affordance live in `imzrt`, or does its read-only
   dashboard framing argue for a tiny dedicated producer app? — §8.
   *`imzrt`, taken at M2; ADR-0061 §SD6 relaxed by dated update to allow it.*
3. Wide per-kind value columns (per-kind books) vs a lowest-common
   `value`+`unit` pair — §5. *Wide, taken at M1 — `profile-heap` reads all
   four heap measures, which the narrow form could not.*
4. ~~Treemap panel input convention~~ — *moot: M4 dropped (§9 R1).*
5. Alias-latest handle resolution (`keelson('alias:…')`) — worth doing early,
   or live with catalog copy-paste? — §8. *Early, and not as a `keelson()`
   spelling: M3 landed the `adhoc.resolve` subject, so a book declares
   `datasets:` and the applet binds the newest handle at open.*
6. Edge-weight encoding in the graph tab — verify the override hooks reach
   stroke width from panel input — §9. *Not verified, and not needed:
   `profile-callgraph` took §9's stated worst case and puts the weight in
   the edge label. Whether stroke width is reachable is still unanswered —
   it is now an ADR-0129 question, not a pprof one.*
