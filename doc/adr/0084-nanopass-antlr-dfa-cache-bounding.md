---
type: adr
status: proposed
date: 2026-06-13
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0084: Bounding the ANTLR DFA cache for long-running SQL parsing

## Context

`nanopass.Parse` (Grammar1) and `nanopass.ParseCanonical` (Grammar2) are the two ANTLR parser-construction seams in the ClickHouse DSL. Everything else routes through them: `marshalling` calls `Parse`, and `ast` consumes `ParseCanonical` results. So these two functions are the entire SQL-parsing surface, including any path a long-running SQL proxy would drive.

Fuzzing `nanopass` surfaced an unbounded-memory growth that the per-parse input guards ([`CheckInputGuards`](../../public/db/clickhouse/dsl/nanopass/nanopass_guard.go): `MaxInputBytes`, `MaxNestingDepth`) do not address. The guards bound the cost of a *single* parse; they say nothing about state accumulated *across* parses.

### Where the state lives

The `antlr4-go/antlr/v4` runtime (v4.13.1) memoises adaptive-prediction decisions in a **DFA cache**. Each generated grammar holds, in a package-level `…ParserStaticData` global:

- `decisionToDFA []*antlr.DFA` — one DFA per grammar decision point (138 in Grammar1),
- a `PredictionContextCache`.

Every `NewClickHouseParserGrammar1(stream)` wires *these shared globals* into the new parser's interpreter (`NewParserATNSimulator`). During prediction, `addDFAState → dfa.Put()` appends DFA states and **never evicts**; the states are reachable from the package global, so they survive every GC for the life of the process. (The per-prediction `mergeCache` *is* discarded after each predict — it is not the growth.)

Crucially, **antlr4-go v4.13.1 exposes no `ClearDFA`** — the Java and C# runtimes have one; the Go port dropped it. `reset()` is an empty unexported method, and `decisionToDFA` is assigned only inside the two simulator constructors. The cache cannot be flushed through the normal API.

### What drives the growth — measured

The cache keys on token **type** sequences, not token **text**. Measuring the retained DFA-state count directly (sum of `DFA.Len()` over the shared slice) plus post-GC heap:

| workload | distinct inputs | retained DFA states | heap |
|---|---|---|---|
| baseline (`SELECT 1`) | 1 | 21 | 2.6 MB |
| **300k varying literals**, one shape | 300,000 | **68** | 2.9 MB |
| **300k varying identifiers**, one shape | 300,000 | **74** | 2.9 MB |
| 8,000 distinct templated shapes | 8,000 | 470 | 5.0 MB |
| 124,416 templated combinations | 124,416 | **485 (plateau)** | 5.1 MB |

300,000 distinct literal values and 300,000 distinct identifiers each added ~50 states *once* and then went flat. A proxy's varying `WHERE x = <value>` constants and table/column names cost essentially nothing, and shallow templated shape variety saturates quickly.

Recursively **nested expression structure** does not saturate. Feeding deterministic pseudo-random expression trees (depth ≤ 6, every input guard-legal — tiny):

| workload | parses | DFA states | heap |
|---|---|---|---|
| adversarial nested exprs | 10,000 | 172,281 | **1.95 GB** |
| adversarial | 50,000 | 445,859 | **5.1 GB** |
| adversarial | 200,000 | 898,446 | 10.1 GB |
| adversarial | 500,000 | 1,328,006 | **14.7 GB** |

~11 KB per retained state. The incremental rate decelerates (17 → 6.8 → 3.0 → 1.4 states/parse), so it is converging on the grammar's finite ceiling — but that ceiling is millions of states / tens of GB, far past any acceptable budget for a process meant to run for weeks.

### Two facts that constrain the fix

- **Prediction mode is irrelevant to the memory.** Forcing `PredictionModeSLL` produced *byte-identical* state counts to the default `PredictionModeLL` at every checkpoint (172,281 @ 10k; 445,859 @ 50k). The mass is base ALL(\*) DFA states, not full-context ones; SLL only ran *faster* (it reached 500k where LL timed out), and it changes correctness semantics (it can reject or mis-resolve context-sensitive SQL). SLL is not a memory fix.
- **Concurrency is already safe.** antlr4-go guards DFA-state and edge mutation with ATN-level mutexes (`stateMu`/`edgeMu`): cache reads take `RLock`, writes take an exclusive `Lock`. A concurrent proxy is correct on the shared cache today; a warm cache means concurrent readers, a cold/growing one means serialised writers, so lock contention rises and falls with the growth. Any fix must preserve this safety and not serialise the common (read) path.

### Who is exposed

A proxy serving a bounded set of application/ORM templates is effectively safe — it plateaus at a few hundred states / single-digit MB. A proxy serving **ad-hoc, human-authored, or untrusted analytical SQL** — rich, variably-nested expressions — sits on the unbounded branch. This ADR targets that case.

## Decision

Bound the cache by its **actual size**, resetting it when it grows past a threshold, using only public antlr4-go APIs. The immutable `ATN` is shared and reused; only the DFA cache (the `decisionToDFA` slice + `PredictionContextCache`) is rebuilt.

A process-local cache holder, one per grammar, owns the slice and a `sync.RWMutex`:

- **`Parse`/`ParseCanonical` acquire a read-lock** for the duration of `QueryStmt()` and point the parser's `Interpreter` at the holder's current slice via `NewParserATNSimulator(parser, parser.GetATN(), decisionToDFA, pcc)`. Concurrent parses run fully in parallel under the read-lock — the common path is never serialised.
- **Every `DFACheckInterval` parses**, one parse takes the exclusive write-lock, which drains in-flight parses, so it can sum `DFA.Len()` across the slice **race-free** (no parse is mutating). If the total exceeds **`MaxDFAStates`**, it rebuilds the slice from `parser.GetATN().DecisionToState` (`antlr.NewDFA` per decision) and allocates a fresh `PredictionContextCache`. The previous slice is dropped and collected.

Why size, not parse count: literal/identifier-varied and templated traffic plateau at a small state count regardless of how many queries flow, so a size threshold **never fires for legitimate workloads** — no needless discarding of a warm cache, no latency spikes. It fires only when structural novelty actually crosses the budget, which is exactly the ad-hoc/adversarial case, and on novel input the cache provides little reuse anyway, so discarding it is nearly free in CPU.

Defaults: `MaxDFAStates = 8192` (~40–90 MB at the observed per-state weight; ~17× the templated plateau, so legitimate diversity has wide headroom) and `DFACheckInterval = 256` (worst-case overshoot ≈ 256 × growth-rate states above the threshold before a reset; the check's drain-stall is a sub-millisecond event amortised over 256 parses). Both are exported and runtime-settable so a proxy operator can trade memory against re-warm frequency. `DFACacheStats()` exposes the last-measured per-grammar state count and a cumulative reset counter for monitoring.

The fix lives at the two seams, so it covers Grammar1 and Grammar2 — hence `nanopass`, `marshalling`, and `ast` — without touching call sites.

## Alternatives considered

- **SLL / two-stage (SLL→LL) prediction.** Rejected as a memory fix: measured state counts under SLL were byte-identical to LL (the growth is base DFA states, not full-context). It is faster and would help the contention dimension, but it does not bound memory and it changes correctness semantics. Out of scope here; could be revisited purely for parse latency.
- **Per-parse private cache** (fresh interpreter every parse, GC'd with the parser). Bounded, but every parse starts cold and loses all memoisation — a large CPU cost on the templated traffic that is the common case. Only sensible when query shapes are near-unique, which is not the general proxy.
- **Parse-count-triggered reset** (reset every N parses). Race-free and simpler, but parse count is a poor proxy for memory: legitimate high-QPS templated traffic plateaus at a tiny state count yet would have its warm cache discarded every N parses (latency spikes), while a small enough N to cap the adversarial branch (~hundreds of parses) would cripple the common path. Size-based thresholding breaks this trade.
- **Pool of resettable caches** (`sync.Pool`, each owned by one goroutine at a time, measured lock-free). Also correct and avoids the write-lock drain, but total memory becomes `pool_size × MaxDFAStates`, warmth is lost on every GC (the pool drops entries), and a returned `pr.Parser` reused for re-parsing would mutate a pooled cache another goroutine now owns. The single shared cache gives one clear memory number, the best hit rate, and keeps `pr.Parser` reuse memory-safe.
- **Forking antlr4-go to restore `ClearDFA` / add eviction.** Heaviest option and a standing maintenance burden for a behaviour we can get from the public API. Rejected.
- **Tightening the input guards.** Orthogonal — guards bound per-parse cost, not cumulative cache growth. Every adversarial input above was tiny and within the guards. Kept as-is.

## Consequences

- Memory for the DFA cache is bounded at roughly `MaxDFAStates` (+ one check-interval of overshoot) per grammar, independent of process lifetime and input diversity. The 14.7 GB adversarial run becomes a flat ~tens of MB (a reset-every-2000 prototype held heap flat at ~2.8 MB through 10k adversarial parses where the unmitigated run was at 1.95 GB).
- The common path (literal/identifier-varied, templated SQL) is unaffected: it never crosses the threshold, so it never resets, and the read-lock adds only an uncontended `RLock`/`RUnlock` per parse.
- Under a novelty flood, a reset is a sub-millisecond exclusive-lock event every `DFACheckInterval` parses; it briefly stalls new parses on that grammar while in-flight ones drain. This is the intended back-pressure and is acceptable given how rarely it fires.
- `pr.Parser` remains valid after `Parse` returns and is memory-safe to inspect for rule names/vocabulary; it must not be reused to *re-parse* (it never should have been).
- This is an antlr4-go-wide property. Any future antlr-based parser added to the codebase needs the same seam; the cache holder is written to be reused.

The thresholds are first-cut and intentionally conservative (descope over gate); they are the obvious knobs to tune once a real proxy workload is observed. Wiring them into the typed env-var registry ([ADR-0009](./0009-environment-variable-registry.md)) is a candidate follow-up, not a blocker.

## Status

Proposed — awaiting review by p@stergiotis. Decision under consideration; do not implement as if accepted.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See `doc/DOCUMENTATION_STANDARD.md` §1 ADR for the edit-policy tiers (Tier 1 in-place / Tier 2 `## Updates` H3 / Tier 3 superseding ADR).
