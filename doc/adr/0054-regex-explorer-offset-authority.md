---
type: adr
status: proposed
date: 2026-04-23
reviewed-by: ""
reviewed-date: ""
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0054: Regex Explorer Match-Offset Authority

## Context

The regex-explorer demo app ([`public/thestack/imzero2/egui2/demo/apps/regex_explorer/`](../../public/thestack/imzero2/egui2/demo/apps/regex_explorer)) lets a user type a pattern and a haystack, runs the regex through a [`clickhouse local`](https://clickhouse.com/docs/en/operations/utilities/clickhouse-local) subprocess (invoked via `os/exec` with `--format ArrowStream`, stdout piped into an Arrow IPC reader), and paints inline-highlighted matches on the haystack text widget using egui Atoms' `StyledTextColored`. Four tabs:

| Tab     | SQL primitive                              | Engine     |
|---------|--------------------------------------------|------------|
| Test    | `extractAllGroups(h, p)`                   | RE2        |
| List    | `extractAll(h, p)`                         | RE2        |
| Replace | `replaceRegexpAll(h, p, r)`                | RE2        |
| Multi   | `multiMatchAllIndices([h], [p1, p2, …])`   | VectorScan |

Inline highlighting requires `(start_byte, end_byte)` pairs per match and per capture group. ClickHouse's `extractAll*` family returns captured *substrings*, not offsets. Getting offsets server-side requires a per-match `position` / `positionUTF8` or a custom `arrayMap(m -> position(h, m), matches)`, with edge cases (repeated matches, capture inside capture, UTF-8 vs byte offsets) that do not cleanly survive the SQL round-trip.

Go's [`regexp`](https://pkg.go.dev/regexp) package implements RE2 syntax (minus `\C`) and RE2's linear-time matching guarantee — per the package overview: *"it is the syntax accepted by RE2 and described at https://golang.org/s/re2syntax, except for \C"*. ClickHouse's single-pattern regex functions use the [RE2 C++ library](https://github.com/google/re2) directly — per the ClickHouse docs on `match` / `extract`: *"This function uses the RE2 regular expression library"*. The two are independent implementations of the same specification. `regexp.Regexp.FindAllStringSubmatchIndex` returns `(start, end)` pairs for the full match and each capture group, in one call, at Go's in-process speed.

VectorScan has no Go port in this repo, nor is one planned; but `multiMatchAllIndices` already returns per-pattern hit indicators without needing offsets — the Multi tab's result UI is a "which patterns hit" list, not inline highlighting.

The question this ADR settles: when the UI needs byte offsets, who computes them?

### Why this decision is non-obvious

A naive design picks ClickHouse as the single source of truth — "the tool exists to predict ClickHouse behaviour; therefore ClickHouse is the oracle for everything". That reading ignores two facts. First, interactive highlighting repaints on every debounced keystroke; every round-trip adds user-visible lag. Second, Go's `regexp` and ClickHouse's single-pattern functions target the same RE2 specification. They are independent implementations, but their syntax and linear-time guarantee are the same — so in-process offset computation is specification-equivalent, not an approximation. (SD1 guards against drift between the two implementations.)

## Design space (QOC)

**Question.** Where should match-offset computation live for the regex explorer?

**Options.**

- **O1** — ClickHouse as sole authority. Extend the SQL per tab to include `arrayMap((m, i) -> (m, positionUTF8(h, m, i)), matches, arrayEnumerate(matches))`, return parallel arrays of matches-and-offsets, parse on the Go side. Same scheme for every tab, RE2 and VectorScan.
- **O2** — Go as authority for RE2 tabs; ClickHouse's `multiMatchAllIndices` for the Multi tab *(chosen)*. Server returns match strings / replacement text / pattern-hit indicators for the results surface; Go re-runs the same RE2 pattern in-process via `regexp.Regexp.FindAllStringSubmatchIndex` to derive offsets for highlighting.
- **O3** — Pre-compute everything on the client. Never ask ClickHouse for matches; run the regex locally, highlight, and use ClickHouse only as a "does it parse?" / "what would ClickHouse say on the full dataset?" oracle consulted on demand.
- **O4** — Custom ClickHouse UDF that returns `(match, start, end)` tuples. One SQL function handles every tab; the app ships with a server patch.

**Criteria.**

- **C1 — Highlight latency.** Time from keystroke to repainted highlights. Debounce is 300 ms; any additional round-trip (~10–100 ms typical, much more on a cold or remote connection) is user-visible.
- **C2 — Engine fidelity.** Do the highlighted ranges match what ClickHouse would compute in production? This is the product's reason to exist.
- **C3 — Implementation complexity.** Lines of SQL, Arrow-parsing code, memory management for arrays-of-arrays, retries on partial failure.
- **C4 — Scales to VectorScan.** Can the chosen approach extend to the Multi tab without rearchitecting?
- **C5 — Failure-mode clarity.** If ClickHouse and Go disagree on a match, is the disagreement visible to the user or silently masked?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −  | ++ | ++ | −  |
| C2 | ++ | +  | −  | ++ |
| C3 | −  | +  | ++ | −− |
| C4 | ++ | +  | −  | +  |
| C5 | −  | +  | −  | −  |

O2 dominates on C1 (highlights repaint without the round-trip; the server query feeds the detail pane and status bar but does not gate painting), is `+` on C2 (engine-compatible RE2; see [SD1](#subsidiary-design-decisions) tripwire), is `+` on C3 (two code paths — Go `regexp` for offsets, Arrow parsing for server results — each small, with boxer helpers handling SQL construction), is `+` on C4 (Multi tab uses `multiMatchAllIndices` natively; no cross-engine offset problem), and is `+` on C5 (SD1 surfaces disagreements instead of hiding them).

O1 is tempting for the single-source story but pays a round-trip per debounce cycle and pushes edge-case handling (UTF-8 offsets, repeated matches) into SQL that Go's `regexp` already covers correctly. O3 reverses the tool's purpose — ClickHouse stops being the thing the user is predicting and becomes a bystander. O4 forks the ClickHouse deployment; unmaintained forks are a worse operational burden than extra round-trips.

## Decision

Go is the authority for match offsets in all RE2-backed tabs (Test, List, Replace). ClickHouse is the authority for the Multi tab, via `multiMatchAllIndices`' native `Array(UInt64)` return.

### Subsidiary design decisions

- **SD1 — Engine-fidelity tripwire.** A startup-time self-test runs a small fixed corpus of `(haystack, pattern, expected_matches)` tuples against both Go's `regexp` and ClickHouse (one `extractAll` per tuple) and logs any disagreement via `eh` structured logging. Divergence does not block the app but is surfaced in the status bar. Rationale: Go and ClickHouse both wrap RE2, but they pin upstream versions independently; future divergence on edge-case syntax (e.g., Unicode class support, `\p{…}` semantics) should not fail silently on the user.
- **SD2 — Parameter binding via [`marshalling.EscapeString`](../../../boxer/public/db/clickhouse/dsl/marshalling/ch_marshalling_marshall.go) inlined into SQL.** `clickhouse local` does not offer an HTTP `?param_<name>=…` channel, and the SETTINGS-clause form was falsified under both transports via `clickhouse local` at Phase 1: `SELECT match({hay:String}, {pat:String}) SETTINGS param_hay='…', param_pat='…'` returns *Code 456: Substitution `hay` is not set (UNKNOWN_QUERY_PARAMETER)*. ClickHouse's SETTINGS clause is for query-level server settings (`max_execution_time` etc.); it is not a parameter-binding mechanism. Multi-statement `SET param_<name> = '…'; SELECT … {<name>:Type} …` does work, and could be layered in via `clickhouse local`'s implicit multi-statement handling, but gains nothing over inline escaping given this app's SQL shape (single `SELECT` per dispatch). The app therefore inlines escaped literals via [`marshalling.EscapeString`](../../../boxer/public/db/clickhouse/dsl/marshalling/ch_marshalling_marshall.go). Example: user pattern `it's a test` becomes the SQL literal `'it\'s a test'` (boxer-tested escape of `'`, `\`, `\n`, `\t`, `\r`, `\0`), interpolated directly into the SELECT. [`passes.WriteSettings`](../../../boxer/public/db/clickhouse/dsl/nanopass/passes/nanopass_passes_manipulate_setting.go) remains available for actual server settings (query caps like `max_execution_time` / `max_result_rows`) but is not used for parameter binding. The decision's core premise — that Go sees the same `pat` / `hay` strings it sends to ClickHouse — holds: both Go's `regexp` compile and ClickHouse's parse see the same pre-escape source string, so `regexp.MustCompile(pat).FindAllStringSubmatchIndex(hay, -1)` remains an accurate local mirror.
- **SD3 — One compile per unique pattern per session.** `regexp.MustCompile` is invoked lazily and cached on the app struct keyed by pattern string. A compile failure is treated as an empty match set plus a status-bar error — it does not stall the UI or the ClickHouse query. Rationale: interactive regex entry generates many invalid intermediate patterns; compile errors are the expected case, not an exception.
- **SD4 — Multi tab does not attempt offsets.** The Multi tab's output is "pattern X hit the haystack" (a pattern index, or a count), not inline highlighting. `multiMatchAllIndices` returns pattern indices, not byte offsets, and VectorScan's API is not structured for offset retrieval. A future extension that wants per-pattern highlighting is a new ADR, not an incremental change to this one.
- **SD5 — No attempt to reconcile capture-group numbering.** Both Go and ClickHouse RE2 number capture groups left-to-right by open paren. This ADR assumes parity; if a divergence is ever observed, SD1's tripwire surfaces it and a follow-up ADR decides the reconciliation rule.
- **SD6 — UTF-8 offsets everywhere.** Highlights are painted in byte offsets into the UTF-8 haystack. Go's `FindAllStringSubmatchIndex` already returns byte offsets; the egui Atoms painter slices the UTF-8 haystack by byte range. No code path operates in codepoints or grapheme clusters, avoiding a class of off-by-one bugs around multi-byte characters.
- **SD7 — ClickHouse transport via `clickhouse local` subprocess, not HTTP.** Every query spawns a fresh `clickhouse local --query ... --format ArrowStream` subprocess through `os/exec`; stdout is piped into the Arrow IPC reader, stderr is captured for error diagnostics, and wall-clock from dispatch to drain is the elapsed number shown in the status bar. Cold-start latency is ~60 ms on a warm cache, fast enough for interactive use given the per-change dispatcher is coalesced by per-query atomic flags. The trade-off vs. HTTP-backed `play.Client.ExecuteArrowStream` is explicit: (a) no server, no auth, no network, no `REGEX_EXPLORER_CLICKHOUSE_URL` env var; (b) server-side counters like `X-ClickHouse-Summary.read_rows` are not exposed by `clickhouse local`, so the status bar shows only wall-clock elapsed (ReadRows was zero on no-FROM queries anyway); (c) multi-tenant isolation comes for free because each tab dispatches its own subprocess with its own memory. The decision is demo-specific: `play.Client` remains the right choice for demos that exercise a real ClickHouse dataset (see case 5 in [`imzero2_demo_resolve.go`](../../public/thestack/imzero2/egui2/demo/carousel/imzero2_demo_resolve.go)); the regex explorer is an SQL-correctness tool, not a data tool.

## Alternatives

Rejection rationale for the top-level options is in the QOC matrix; notes below capture detail not visible in the ratings.

- **O1 — ClickHouse as sole authority.** The SQL:
  ```sql
  WITH positions AS (
    SELECT arrayMap((m, i) -> (m, positionUTF8({hay:String}, m, i)), matches, arrayEnumerate(matches)) AS mp
    FROM (SELECT extractAll({hay:String}, {pat:String}) AS matches)
  )
  SELECT mp FROM positions SETTINGS param_hay = '…', param_pat = '…'
  ```
  runs once per debounce cycle. On a 100 ms round-trip this adds 100 ms to every repaint; on a remote or cold connection, much more. The `arrayMap` with enumerated offsets handles the repeated-match case (`abab` with pattern `ab`), but every capture-group variant needs parallel scaffolding, and replace-preview needs its own scheme. Net: correct-ish but slow, and hostile to the "feels like a local tool" target.
- **O3 — Pre-compute on client.** ClickHouse becomes an oracle consulted only when the user hits "run on table". The interactive loop never talks to the server. This is *exactly* what this tool is trying not to be — the value proposition is "see what ClickHouse will do on this regex *before* you paste it into a production query". Ruled out on product scope.
- **O4 — Custom UDF.** Clean signature, single code path, but forks the ClickHouse deployment. Any user of the tool must patch their server or install the UDF; the demo is no longer self-contained. Rejected on operational grounds.

## Consequences

### Positive

- **Sub-debounce repaints.** Highlights refresh at Go speed; the ClickHouse query continues in the background to populate the detail pane, status bar, and summary header. The UI stays responsive even on slow networks.
- **Small SQL surface.** Each RE2 tab maps to one short query (`extractAllGroups` / `extractAll` / `replaceRegexpAll`) with `WriteSettings` parameter binding. No `arrayMap`, no `position`, no parallel-array parsing.
- **Clean VectorScan integration.** `multiMatchAllIndices` is already the right shape for the Multi tab; no cross-engine offset gymnastics.
- **Boxer reuse.** [`passes.WriteSettings`](../../../boxer/public/db/clickhouse/dsl/nanopass/passes/nanopass_passes_manipulate_setting.go) + `ExecuteArrowStream` cover the round-trip; no new SQL-construction code in the demo beyond template strings.
- **Engine-compatibility tripwire.** SD1 makes any future Go / ClickHouse RE2 version drift visible rather than silent.

### Negative

- **Two regex engines live in the same UI context.** Go's `regexp` drives highlighting; ClickHouse's RE2 drives the result list. A user who constructs a pattern where the two disagree sees highlighted ranges that do not match the result rows. SD1 surfaces this; it does not prevent it.
- **Capture-group divergence is not guarded beyond SD5's open-paren counting assumption.** If ClickHouse introduces a named-group or non-capturing-group behaviour change that Go does not mirror, the detail-pane groups and highlight groups misalign silently. A follow-up ADR will be required if that occurs.
- **Pattern is compiled twice per unique session pattern.** Once by ClickHouse, once by Go. For interactive use this is negligible; catastrophic-compile patterns show up on both engines, so the error surface is symmetric.
- **Multi tab is a dead end for inline highlighting.** By design — VectorScan is a pattern-set matcher, not a capturer — but newcomers may expect feature parity across tabs and file the absence as a bug.
- **Tripwire adds startup cost.** SD1 runs on app init. The corpus is small (expected ≤ 20 tuples, single round-trip) but a user running against an unavailable ClickHouse sees the app start degraded. Acceptable: the rest of the app requires ClickHouse too.

### Neutral

- **`play.Client.ExecuteArrowStream` is unchanged.** The param mechanism slots in via `WriteSettings` pre-processing; the client stays a pure HTTP → Arrow pipe, consistent with its role in `hn_explorer`.
- **Tests live alongside the app.** The tripwire corpus is a table-driven test in the `regex_explorer` package, not a separate fixture.
- **The decision is pattern-matcher-specific, not general.** Other demos that visualise ClickHouse output (histograms, aggregations, treemaps) do not inherit this pattern; they do not have an engine-compatible in-process equivalent to duplicate.

### Derived practices

- **Interactive previews of ClickHouse primitives compute locally when the engines match.** If a future app needs a VectorScan-, Hyperscan-, or column-codec-flavoured preview, the `regex_explorer` structure does not generalise — plan a distinct app or a distinct tab, not a refactor. The generalisation is "same engine available in-process → compute locally"; it is not "always compute locally".
- **Engine-fidelity self-tests are a first-class feature, not a test-suite item.** SD1's tripwire runs in the running app, logs via `eh`, and surfaces in the UI. Regex engines drift; silent drift is worse than visible disagreement.
- **`marshalling.EscapeString` is the default parameter-binding mechanism for ClickHouse-facing demos in this repo, pending a richer client.** Empirically verified at Phase 1 (see SD2). The next-better mechanism is HTTP `?param_<name>=value` with `{name:Type}` placeholders, which requires extending `play.Client.ExecuteArrowStream` to accept a `map[string]string` of query-string params — a worthwhile follow-up, but out of scope for this ADR. Until then, `EscapeString`-inlined literals are the safe, boxer-tested default.
- **SETTINGS clause is not a parameter-binding vehicle.** Retain `passes.WriteSettings` only for actual server settings — `max_execution_time`, `max_result_rows`, `max_memory_usage` — not for user-supplied strings. This distinction is not obvious from the pass name and is worth codifying.

## Status

Proposed — 2026-04-23. Pending first human review. Accepted on review pass; transition the front-matter `status` to `accepted`, populate `reviewed-by` / `reviewed-date`, and remove the draft banner.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only; supersession is recorded, not deleted.

## References

- [`doc/adr/0001-clickhouse-observability-pipeline.md`](0050-clickhouse-observability-pipeline.md), [`0002-query-categorization-provenance.md`](0051-query-categorization-provenance.md), [`0003-imzero2-unified-color-type.md`](0052-imzero2-unified-color-type.md), [`0004-retire-alpha-cbor-for-jsonv2.md`](0053-retire-alpha-cbor-for-jsonv2.md) — prior ADRs; template shape followed here.
- [`../../../boxer/doc/DOCUMENTATION_STANDARD.md`](../../../boxer/doc/DOCUMENTATION_STANDARD.md) — Diátaxis + ADR conventions this document follows.
- [`../../../boxer/CODINGSTANDARDS.md`](../../../boxer/CODINGSTANDARDS.md) — Go conventions the demo implementation follows.
- [`../../../boxer/public/db/clickhouse/dsl/nanopass/passes/nanopass_passes_manipulate_setting.go`](../../../boxer/public/db/clickhouse/dsl/nanopass/passes/nanopass_passes_manipulate_setting.go) — `WriteSettings` pass used for parameter binding (SD2).
- [`../../../boxer/public/db/clickhouse/dsl/marshalling/ch_marshalling_marshall.go`](../../../boxer/public/db/clickhouse/dsl/marshalling/ch_marshalling_marshall.go) — `EscapeString` / `MarshalGoValueToSQL`, the fallback if SD2 verification fails.
- [`clickhouse local` reference](https://clickhouse.com/docs/en/operations/utilities/clickhouse-local) — the zero-setup ClickHouse CLI the app's transport uses (SD7).
- `public/spinnaker/hmi/play/play_client.go` — the HTTP-based `ExecuteArrowStream` retained for other demos (case 5); not used by the regex explorer.
- [`public/thestack/imzero2/egui2/demo/apps/hn_explorer/hn_explorer.go`](../../public/thestack/imzero2/egui2/demo/apps/hn_explorer/hn_explorer.go) — structural template the new demo follows.
- [`public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_sql_demo.go`](../../public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_sql_demo.go) — prior art for `{name:Type}` placeholder SQL display.
- Go [`regexp`](https://pkg.go.dev/regexp) package — pure-Go implementation of RE2 syntax (minus `\C`) with RE2's linear-time guarantee; used for in-process offset computation.
- [RE2 syntax reference](https://github.com/google/re2/wiki/Syntax) — the specification both Go `regexp` and ClickHouse's single-pattern functions target.
- [ClickHouse string-search functions](https://clickhouse.com/docs/en/sql-reference/functions/string-search-functions) — `match` / `extract` / `extractAll` / `extractAllGroups` documentation; *"This function uses the RE2 regular expression library"*.
- [ClickHouse string-replace functions](https://clickhouse.com/docs/en/sql-reference/functions/string-replace-functions) — `replaceRegexpOne` / `replaceRegexpAll`; *"in re2 syntax"*.
