---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Measurements taken 2026-10-09 against
> grammar1 as committed that day, antlr4-go v4.13.1 and ANTLR 4.13.2, on one
> developer machine (32 logical cores). Absolute timings move with cache warmth,
> load and machine; the ratios and the mechanisms are what this page asks you to
> believe. Rows marked *estimate* were not measured. It feeds
> [ADR-0304](../adr/0304-sll-exact-column-qualifiers.md) and
> [ADR-0305](../adr/0305-ll-islands-in-the-sll-stage.md), which took the
> first finding only.

# antlr4-go — what the runtime and the generated parser cost, and what could be better

The question this page answers is how much better a parse of ClickHouse SQL
could be if the generated code, the runtime, or the grammar changed. It extends
[nanopass — the full-context prediction tax on `WITH`](./nanopass-full-context-prediction-tax.md)
and [ADR-0196](../adr/0196-nanopass-two-stage-sll-parsing.md); it does not
repeat them.

The short answer: the largest lever was the grammar, not the code generator.
Two-stage parsing (ADR-0196) only pays when SLL accepts, and SLL rejected
29% of the statements the test suite parses; one mechanism produced 97% of
those rejections. After that, the runtime's locks and allocation are worth
small single-thread factors and larger parallel ones, and a custom code
generator an estimated 3–4×.

## Where a parse spends its time

Warm DFA cache, one thread, grammar1, the statements of `nanopass_bench_test`:

| input | tokens | lex only | SLL only | LL only | `nanopass.Parse` |
|---|---|---|---|---|---|
| small (27 B) | 16 | 2 µs | 8 µs | 10 µs | 9 µs |
| medium (572 B) | 239 | 28 µs | 0.17 ms | 2.7 ms | 2.8 ms |
| large (5 KB) | 3,630 | 0.35 ms | 0.9 ms | 29 ms | 40 ms |

Medium and large are rejected by SLL, so `nanopass.Parse` pays SLL and then LL.
LL costs about 8 µs per token against about 0.25 µs for warm SLL, because
full-context predictions are never memoised. Garbage collection is 31–47% of
CPU in these profiles.

## The SLL rejections

Every statement the test suite parses was harvested (an `-overlay` build that
logged each `Parse`/`ParseCanonical` input; 15,766 distinct inputs). Of the
15,302 grammar1 inputs LL accepts, SLL rejected 4,497; 4,386 of those were one
message, `mismatched input … expecting '.'` or `missing '.'`.

The mechanism: the column qualifier in `columnIdentifier : (tableIdentifier
DOT)? nestedIdentifier` reused `tableIdentifier`, whose own decision is
`(databaseIdentifier DOT)?`. SLL prediction has no call stack, so when it
reaches the end of a rule it continues into every place the rule is invoked.
For `t.c` the `db.table` reading of `tableIdentifier` therefore stays viable
whenever the next token could follow a table reference *anywhere* — `,` `)`
`WHERE` `JOIN` `GROUP` EOF — and SLL resolves the conflict to the lowest
alternative, the database prefix. The parse then fails one token later and
falls back. `t.c FROM` was accepted, because nothing can follow a table with
`FROM`, which is why the pattern looked like "dotted names are necessary but
not sufficient" in ADR-0196 §SD6.

ADR-0304 gives the qualifier its own rule, after which SLL rejects 131 of the
15,302 (0.9%). The remaining classes are each a separate grammar question:
`CAST(x AS T)` against the alias suffix, the `BETWEEN` operand against binary
`AND`, and a table function taking a `SELECT`.

### Before and after ADR-0304

Same machine, interleaved runs, moderate background load (2026-10-09):

| measurement | before | after |
|---|---|---|
| `BenchmarkNanopassParseCorpus` (87 in-tree statements) | 23.2 ms | 12.6 ms |
| `BenchmarkPlayPipelineApply/medium` | 115 ms | 96 ms |
| `BenchmarkPlayPipelineApply/applet_9kb` (already SLL-clean) | 67 ms | 69 ms |
| `BenchmarkNanopassParse/medium`, `/large` (both carry `BETWEEN`) | 3.6 / 64 ms | 3.3 / 62 ms |
| every harvested input through every pass and analysis, 16 threads | 24 min | 4.3 min |

The last row is dominated by fuzz-generated nested expressions with qualified
names, where the LL fallback is most expensive; it shows the size of the
effect on that shape, not on authored SQL.

## Runtime findings (antlr4-go v4.13.1)

Not repeated here: the missing `ClearDFA` (ADR-0084), the missing profiling API
(ADR-0196 §SD2).

| finding | effect | evidence | fix needs |
|---|---|---|---|
| `DefaultErrorStrategy.Sync` → `ATN.NextTokensNoContext` takes an exclusive mutex even when the set is cached; generated code calls `Sync` ~2.8× per token | ~6% of a warm SLL parse single-threaded, ~20% at 32 goroutines (measured with a lock-free override) | `atn.go` `NextTokensNoContext` | an error-strategy override in this repo, no fork |
| A read-lock on every DFA edge lookup, lexer and parser | warm SLL throughput at 32 goroutines is ~1.6× one thread; with `antlr.nomutex` ~5.8× | `getExistingTargetState` in both simulators | a fork, or per-goroutine caches plus `antlr.nomutex` (binary-wide, so unsafe for any other ANTLR user in the binary) |
| Lexer DFA edges stop at code point 127; every other character runs ATN simulation under an exclusive lock | ~95× slower per character on a non-ASCII literal | `LexerATNSimulator` `MaxDFAEdge` | a fork (sparse edge map) |
| `BailErrorStrategy` | the panic is `ParseCancellationException.GetMessage` → `panic("implement me")`, reached through `ReportError`, which first prints to stdout; when the first error comes from prediction the generated error exit calls `SetError(nil)` and the parse continues, truncated | `errors.go`, generated `errorExit` | a ~15-line strategy in this repo that panics with a sentinel from `Recover`/`RecoverInline`/`Sync` |
| `LexerATNConfig` equality returns true when both action executors are nil without comparing state, alt or context | silent mis-tokenisation on a 32-bit hash collision; not observed | `atn_config.go` `LEquals` | a one-line runtime patch |
| `safeMatch` swallows panics that are not recognition errors; `GetChild(len)` panics instead of returning nil; `GetText` concatenates quadratically | masks bugs; a trap; ~2 ms / 8.5 MB on an 8k-token subtree | `lexer.go`, `parser_rule_context.go` | runtime patches |
| Missing against the Java runtime: parser/lexer interpreter, unbuffered streams, XPath, tree patterns | the `.interp` files cannot be used at runtime | — | — |

No data race was found in DFA mutation; the lock order is consistent.

## Generated-code findings (ANTLR 4.13.2 Go target)

Static shape of grammar1: 76 rule functions; 61 `AdaptivePredict` sites against
13 `LA(1)` switches and 127 one-token tests; 172 `Sync` calls; 137 context types.
The generated code uses no reflection and no `defer`. On the 10 KB applet
fixture, a warm SLL parse makes 1.6 rule entries, 1.8 predictions and 2.8 `Sync`
calls per token, and allocates 8,115 objects / 740 KB.

| item | share or cost | what a generator could do | gain |
|---|---|---|---|
| unconditional `Sync` before every loop and optional | ~11% of parse CPU | call it only when the lookahead test fails | measured share |
| `AdaptivePredict` for decisions needing more than one token | ~25% of parse CPU | compile small decisions into switches | *estimate* 15–20% |
| lexer runs entirely in the ATN simulator; keywords are case-insensitive fragment chains | ~31% of a warm SLL parse | a generated keyword lexer | *estimate* 5–10× on lexing |
| allocation per node: 128-byte contexts, labelled alternatives copy a throwaway base context, child slices grow | GC is 31–47% of CPU | pre-sized or arena-allocated nodes | *estimate* ~2× fewer allocations |
| cold first parse | 220–238 ms against 1.1–1.3 ms warm; every ADR-0084 reset pays it again | compiled decisions or a pre-warmed DFA | removes it |
| 2,584 wrapper methods from embedding `BaseParserRuleContext` | ~300 KB of the package's 484 KB code | no embedding | measured size |
| child accessors scan all children with interface assertions | ~55× slower than an index, but under 1% of a run | typed child fields | small |

Taken together, a warm SLL parse could plausibly go from ~1.1 ms to
~0.25–0.4 ms on the applet (*estimate*). Any generator that changes the
context API meets the rewrite cost ADR-0196 recorded: dozens of consumer files,
and type switches that break silently rather than failing to compile.

## Options, cheapest first

1. **Grammar: make SLL exact where it is not.** Taken for qualified names in
   ADR-0304. The remaining positions — `CAST`, `BETWEEN`, a table function in
   a FROM item — are predicted in LL inside the SLL stage instead
   ([ADR-0305](../adr/0305-ll-islands-in-the-sll-stage.md)), after
   which no statement in the corpus falls back.
2. **In-repo runtime workarounds, no fork:** a lock-free `Sync`, a working bail
   strategy for stage one. Small single-thread gains; the bail only helps the
   rejected path.
3. **Runtime patches (fork or upstream):** edge locks, non-ASCII lexing,
   `LEquals`. The only route to parallel throughput.
4. **A custom generator:** the estimates above, at the cost of the consumer
   rewrite.
