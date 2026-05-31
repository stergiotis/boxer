---
type: adr
status: accepted
date: 2026-04-27
reviewed-by: "@stergiotis"
reviewed-date: 2026-04-27
---

# ADR-0016: Imzero2 Time Range Picker — Grafana 7.5 Derivative with ClickHouse-Backed Expression Evaluation

## Context

pebble2impl is replacing parts of Grafana's UI with imzero2 + egui_plot (project scope: ~100k visible points per pane, M4-in-SQL upstream, layout in code, no dashboard JSON). The current imzero2 date picker — a thin wrapper over [`egui_extras::DatePickerButton`][egui-datepicker] with a packed `u64` (`YYYYMMDD`) wire format — covers about 5 % of what a Grafana-equivalent dashboard needs from a time control. It is single-point, date-only, no relative expressions, no range, no timezone awareness, no quick-range presets.

Grafana's "time range picker" is the load-bearing time control across the entire product. Five capabilities define it:

1. **Absolute from/to with date + time** (sub-second optional).
2. **Relative expressions** — `now`, `now-5m`, `now/d`, `now-1d/d+8h` — with units `s/m/h/d/w/M/Q/y` and a `/` snap-to-unit operator that truncates down to a calendar boundary.
3. **A presets registry** — "Last 5 minutes", "Last 24 hours", "Today so far" — that is the 95th-percentile click target.
4. **Timezone selection** (System / UTC / specific IANA tz) and a fiscal-year offset.
5. **Auto-refresh interval** — the picker exposes "re-evaluate every N seconds" so dashboards stay live.

Replacing dashboards without a working time range picker is a non-starter. Three sub-decisions need to be settled together so the parts compose: (a) what we derive from upstream and under what license, (b) how relative expressions are evaluated, (c) the FFFI2 wire format upgrade.

The Apache-2.0 obligations on a derivative work distribution are §4.a (provide license), §4.b (mark modified files), §4.c (retain attribution), §4.d (carry NOTICE) — same shape as [ADR-0005][adr-0015], which establishes the pattern this ADR mirrors. Grafana's license history matters here: the project switched from Apache-2.0 to AGPL-3.0 with the v8.0 release on 2021-06-08. The final Apache-2.0 line is the v7.5 series, ending at v7.5.17 (2022-04-12). Anything pinned past that is AGPL.

## Design space (QOC)

The decision is two independent sub-questions. Each meets the ≥3 options × ≥3 criteria threshold.

### Sub-question A — Derivation strategy

**Question.** How should pebble2impl obtain a Grafana-equivalent time range picker without inheriting AGPL obligations?

**Options.**

- **A1** — *Derivative of current Grafana OSS (v8.0+, AGPL-3.0).* Pin a recent commit; copy UX shape and `datemath` module; accept AGPL.
- **A2** — *Derivative of Grafana v7.5.17 (last Apache-2.0 release).* Pin v7.5.17; same per-file headers / package NOTICE / aggregate third-party file pattern as ADR-0005.
- **A3** — *Clean-room reimplementation against Grafana docs only.* Forbid agent or human access to Grafana source.

**Criteria.**

- **C1 — License containment.** Whether copyleft (AGPL) or notice-only (Apache-2.0) obligations propagate up the import graph.
- **C2 — UX fidelity.** Whether muscle memory transfers (label wording, preset list, keyboard behaviour, snap semantics).
- **C3 — Implementation cost.** Hours to a first working picker.
- **C4 — Drift cost.** Cost of pulling in upstream improvements over time.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | A1 | A2 | A3 |
|----|----|----|----|
| C1 | −− | +  | ++ |
| C2 | ++ | +  | −  |
| C3 | ++ | +  | −− |
| C4 | +  | −  | −− |

**A1's gap.** AGPL-3.0 §5 (conveying modified source) requires that any work containing the AGPL'd component, when conveyed, be licensed under AGPL as a whole. Even isolating Grafana-derived files under one Go package does not contain §5: the imzero2 binary that imports the picker's Go surface, when distributed, becomes an AGPL'd aggregate. §13 (network use) extends that obligation to remote users of a modified version. C1 is `−−`.

**A3's gap.** Grafana's `datemath.ts` has worked through edge cases that cost production-incident cycles to discover: `/` snap interaction with timezone DST boundaries, the asymmetry between `now-1d/d` (start of yesterday) and `now/d-1d` (yesterday at the current wall-clock), `M` and `Q` semantics under fiscal-year offsets. Re-discovering these by reading docs alone produces near-misses on UX. C3 = `−−`, C4 = `−−` (divergence accumulates linearly with upstream UX changes we have to re-discover).

### Sub-question B — Expression evaluation engine

**Question.** What evaluates a relative-expression string into a concrete `(from, to)` pair of timestamps?

**Options.**

- **B1** — *Handwritten Go recursive-descent parser + evaluator.* Port `datemath.ts` semantics to Go using `time` or `jiff`.
- **B2** — *ANTLR grammar in the existing dsl/ tooling.* Define `datemath.g4`, generate Go visitor, evaluate via `time` / `jiff`.
- **B3** — *ClickHouse SQL via `clickhouse-local` subprocess.* Anchor injected as a `WITH` scalar; expression is ClickHouse SQL; evaluation by `clickhouse-local`. Validation in-process via the existing dsl ANTLR parser.
- **B4** — *Port-and-link Grafana's `datemath` module verbatim* (TypeScript via embedded JS runtime or wasm).

**Criteria.**

- **C1 — Semantic alignment with the project's data layer.** Whether the picker's time math matches the query layer's time math (data lives in ClickHouse).
- **C2 — Timezone / DST / calendar correctness.** Coverage of IANA tz, DST transitions, leap-day, fiscal months.
- **C3 — Implementation cost.**
- **C4 — Validation latency** (interactive feedback as the user types).
- **C5 — Evaluation latency** (Apply-time round trip).
- **C6 — Test/CI portability.** Whether the evaluator runs in environments without external dependencies.

**Assessment.**

|    | B1 | B2 | B3 | B4 |
|----|----|----|----|----|
| C1 | −  | −  | ++ | −  |
| C2 | +  | +  | ++ | +  |
| C3 | +  | −  | ++ | −− |
| C4 | ++ | ++ | +  | −  |
| C5 | ++ | ++ | −  | −  |
| C6 | ++ | ++ | −  | −  |

**B3's win on C1.** pebble2impl's data lives in ClickHouse; the picker's expression string *is* expressible directly in the query layer. The picker computes `anchor_now - INTERVAL 1 HOUR`, the query plan computes `WHERE ts >= anchor_now - INTERVAL 1 HOUR` — identical SQL, identical semantics, no translation step. This eliminates a known drift source in dashboarding products (Grafana's own `datemath.ts` ↔ data source backend mismatches).

**B3's costs and their mitigations.**

- *Fork/exec latency (~50–200 ms)* — prohibitive on every keystroke. Mitigation: validation uses the in-tree ClickHouse SQL parser at [`public/db/clickhouse/dsl/`](../../public/db/clickhouse/dsl) (sub-millisecond, in-process). Evaluation is deferred to **Apply / focus-out** and uses a single long-running `clickhouse-local` subprocess for the picker's lifetime.
- *Tool-availability variance* — `clickhouse-local` (or the older `clickhouse local` invocation) may not be on `PATH` in all CI / dev environments. Mitigation: the evaluator returns a sentinel `ErrEvaluatorUnavailable`; dependent tests SKIP via a build-flag-gated probe; production environments are expected to install it (it is already the project's offline-verification tool, per established practice).

**B4's collapse.** Embedding a JS runtime to execute `datemath.ts` (or compiling it to wasm) imports a much larger runtime dependency than B3's subprocess and produces UX in TypeScript-idiomatic patterns rather than Go-idiomatic ones. C3 = `−−`.

## Decision

We will:

1. **License path (A2).** Adopt **Grafana v7.5.17** (last Apache-2.0 release) as the upstream pin. The port lives at:
   - `public/thestack/imzero2/egui2/widgets/timerangepicker/` (Go side — multiple files justify a sub-package over the single-file convention used for the legacy date picker).
   - `rust/imzero2/src/imzero2/time_range_picker.rs` (Rust side, sibling to existing `date_picker_button.rs`).

   Per-file Apache-2.0 headers (upstream copyright + pebble2impl modification copyright + §4.b modification line), a package-level `NOTICE`, and an entry in `doc/legal/third_party.md` mirror the [ADR-0005][adr-0015] pattern.

2. **Expression engine (B3).** ClickHouse SQL is the expression language. The picker injects a stable per-Apply `anchor_now`, validates expressions in-process via the dsl ANTLR parser, and evaluates via a long-running `clickhouse-local` subprocess on Apply / focus-out:

   ```sql
   WITH toDateTime64('2026-04-27 12:00:00.000', 3, 'UTC') AS anchor_now
   SELECT
     anchor_now - INTERVAL 1 HOUR AS from_ts,
     anchor_now              AS to_ts
   FORMAT TSV
   ```

   Subprocess discovery: probe `clickhouse-local` on `PATH` first, fall back to `clickhouse local` (older invocation). If neither is available, the evaluator constructor returns `ErrEvaluatorUnavailable` and any test depending on evaluation calls `t.Skip(...)`.

3. **Presets registry.** A `Presets` registry maps human labels ("Last 5 minutes", "Today so far", "Yesterday") to ClickHouse SQL fragments. The default registry mirrors Grafana 7.5's defaults (translated to ClickHouse SQL). Customers register their own presets — this is the surface that lets pebble2impl encode site-specific quick-ranges (e.g. "Yesterday's batch window 02:00–06:00 UTC") as first-class.

4. **Wire format.** The current `u64 YYYYMMDD` is **kept for the legacy single-date widget** for backward compatibility. The new range picker uses a separate FFFI2 wire shape: `(from_epoch_ms i64, to_epoch_ms i64, tz_id u16)`, where `tz_id` is an interned IANA-tz index (zone catalogue lives Go-side, indices are stable per-process). Expression strings stay Go-side; only the *evaluated* result crosses FFFI2. The picker is a stateful widget per [ADR-0013][adr-0013] (`SendRespVal`-bound).

5. **Phased plan.** Implementation detail goes in a follow-up howto seeded after this ADR is accepted (`doc/howto/imzero2-time-range-picker-port.md`). High-level phases:
   - **P1** — datetime-aware single point (date + time + tz on the existing date picker's footprint, behind a feature flag).
   - **P2** — `TimeRange` value type + `clickhouse-local` evaluator + presets registry, all Go-side, unit-tested standalone.
   - **P3** — composite picker UI (from/to fields, presets sidebar, calendar pop, Apply/Cancel) wired to the evaluator.
   - **P4** — timezone selector + auto-refresh interval value (interval is exposed; the auto-runner that consumes it is a separate component).

   Each phase ends with screenshot-tour coverage (per the `IMZERO2_SCREENSHOT_DIR` convention).

**Out of scope for this ADR.** Fiscal-year offsets (parked; covered by a follow-up if a customer needs it); the auto-refresh runner itself (the picker exposes the interval value, a separate timer drives re-Apply); dashboard-JSON compatibility (project policy: no dashboard JSON).

## Alternatives

- **A1 — Derive from current AGPL Grafana.** Rejected: AGPL-3.0 §5 propagates copyleft across the imzero2 binary aggregate; isolating Grafana-derived files at the package level does not contain the obligation in any way that survives legal review.
- **A3 — Clean-room reimplementation.** Rejected: relative-expression edge cases (snap × DST, fiscal `M`/`Q`, `now-1d/d` vs `now/d-1d`) cost production-incident cycles to discover; agent-assisted clean-room is also hard to defend honestly when v7.5.17 is freely available under Apache-2.0.
- **B1 — Handwritten Go RD parser.** Rejected as primary engine: produces a *second* time-math implementation parallel to whatever ClickHouse already does in the query layer; drift between the two is inevitable. Retained as a **fallback engine** if the subprocess strategy proves unworkable in production (e.g. cold-start latency on first Apply).
- **B2 — ANTLR datemath grammar.** Rejected as v1: the existing dsl/ ANTLR parser already handles ClickHouse SQL syntax; reusing it (B3) is strictly less code than writing a parallel `datemath.g4`. Reconsider if a v2 syntactic-sugar layer (`now-5m` → ClickHouse SQL) is added via dsl/nanopass.
- **B4 — Embed JS runtime to run `datemath.ts` verbatim.** Rejected: large runtime footprint, alien idioms, no project-data alignment.
- **Two int64 epoch-ms only, no expression language.** Rejected: fails the project goal — Grafana's relative-expression UX is the load-bearing 95th-percentile control.

## Consequences

### Positive

- **One time-math source of truth.** Picker expression and query-layer time math are the same SQL, eliminating a known drift source in dashboarding products.
- **Production-grade calendar / tz / DST.** ClickHouse's IANA tz database, leap-day handling, and DST awareness are inherited for free.
- **Small license obligation.** Apache-2.0 §4.a–d on a contained subtree; no copyleft.
- **In-process validation is fast.** The dsl ANTLR parser gives sub-millisecond syntax feedback as the user types.
- **Open presets registry.** Site-specific quick-ranges become first-class without forking the picker.
- **Re-applying upstream improvements is mechanical** for the v7.5.17 baseline (matches ADR-0005's experience with the franz-go port).

### Negative

- **`clickhouse-local` becomes a build-time concern.** CI does **not** install it; tests touching the evaluator probe `exec.LookPath("clickhouse-local")` (with `clickhouse local` fallback) and call `t.Skip(...)` when the binary is missing. Surface the SKIP reason in test output so it is auditable in CI logs.
- **Per-file Apache headers and NOTICE drift.** Same maintenance tax as ADR-0005; mitigation: doclint extension or CI grep before merge.
- **Expression verbosity.** `now() - INTERVAL 5 MINUTE` is ~4× longer than Grafana's `now-5m`. The presets registry covers most cases; a v2 syntactic-sugar pass via dsl/nanopass is recorded as a follow-up if real usage demands it.
- **Subprocess lifecycle management.** A long-running `clickhouse-local` needs supervision (restart on crash, graceful shutdown, no stdin races, bounded stdout buffering). The evaluator package owns this and ships its own tests.
- **Wire format split.** Two FFFI2 wire shapes coexist (`u64 YYYYMMDD` for the legacy widget, `(i64,i64,u16)` for the range picker). Some duplication in codegen; acceptable for backward compatibility.

### Neutral

- The pinned upstream is Grafana v7.5.17 (April 2022). Grafana's UX has evolved upstream since; we are not on its release cadence. Re-pinning is an explicit decision recorded in NOTICE + this ADR's References + a follow-up commit message.
- The "now" anchor is **per-Apply**, not per-frame: refresh-interval re-Apply is the path to live updates, not continuous re-evaluation. This matches Grafana's own behaviour.
- Fiscal-year support is parked; covered by a follow-up ADR if a customer needs it.
- The `Jeffail/checkpoint`-style supervisor library may or may not be reused for the subprocess wrapper; that is a Phase 2 implementation choice, not an ADR-level commitment.

## Status

Accepted on 2026-04-27 by @stergiotis.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## References

- Upstream pin: **Grafana v7.5.17** (2022-04-12) — last Apache-2.0 release before the v8.0 AGPL-3.0 switch. Tag SHA verification at the v7.5.17 tag is part of the Phase 0 merge checklist.
- Grafana 7.5 datemath module: [`grafana/grafana@v7.5.17/packages/grafana-data/src/datetime/datemath.ts`][grafana-datemath].
- Grafana 7.5 time picker UI: [`grafana/grafana@v7.5.17/public/app/core/components/TimePicker/`][grafana-timepicker].
- Grafana license switch announcement (2021-04-20): [grafana.com/blog/2021/04/20/grafana-loki-tempo-relicensing-to-agplv3/][grafana-license-change].
- Grafana v7.5.17 release page: [github.com/grafana/grafana/releases/tag/v7.5.17][grafana-7-5-17-release].
- ClickHouse local docs: [clickhouse.com/docs/en/operations/utilities/clickhouse-local][ch-local-docs].
- ClickHouse DateTime functions: [clickhouse.com/docs/en/sql-reference/functions/date-time-functions][ch-datetime-fns].
- In-tree dsl ANTLR parser: [`public/db/clickhouse/dsl/`](../../public/db/clickhouse/dsl).
- Existing single-date picker (kept for backward compatibility):
  [Go wrapper](../../public/thestack/imzero2/egui2/bindings/egui2_datepicker.go),
  [Rust apply](../../rust/imzero2/src/imzero2/date_picker_button.rs),
  [demo](../../public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_datepicker_demo.go).
- Local license text: `licenses/Apache-2.0.txt`.
- Aggregate third-party attribution: `doc/legal/third_party.md`.
- Related: [ADR-0055 — adopt boxer standards](0055-adopt-boxer-standards.md); [ADR-0013 — imzero2 stateful widget contract](0013-imzero2-stateful-widget-contract.md) (the picker is a stateful widget under that contract); [ADR-0005 — port Connect's franz-go Kafka I/O](0005-streaming-persisted-kafka-from-connect.md) (the Apache-2.0 derivative pattern this ADR mirrors).

[egui-datepicker]: https://docs.rs/egui_extras/latest/egui_extras/struct.DatePickerButton.html
[grafana-datemath]: https://github.com/grafana/grafana/blob/v7.5.17/packages/grafana-data/src/datetime/datemath.ts
[grafana-timepicker]: https://github.com/grafana/grafana/tree/v7.5.17/public/app/core/components/TimePicker
[grafana-license-change]: https://grafana.com/blog/2021/04/20/grafana-loki-tempo-relicensing-to-agplv3/
[grafana-7-5-17-release]: https://github.com/grafana/grafana/releases/tag/v7.5.17
[ch-local-docs]: https://clickhouse.com/docs/en/operations/utilities/clickhouse-local
[ch-datetime-fns]: https://clickhouse.com/docs/en/sql-reference/functions/date-time-functions
[adr-0013]: 0013-imzero2-stateful-widget-contract.md
[adr-0015]: 0015-streaming-persisted-kafka-from-connect.md
