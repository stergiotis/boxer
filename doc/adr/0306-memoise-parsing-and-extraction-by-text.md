---
type: adr
status: accepted
date: 2026-10-10
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-10
---

# ADR-0306: Memoise parsing and environment extraction by input text

## Context

[ADR-0002](./0002-nanopass-discipline.md) accepted that each pass re-parses
the SQL it is handed, and said to revisit that once profiling showed parsing
dominating. [ADR-0006](./0006-nanopass-environment-and-first-class-pass.md)
deferred a parse-once optimisation "at the runner layer when profiling
justifies it", and [ADR-0192](./0192-nanopass-cost-profiling.md) built the
profile it would be argued from.

On 2026-10-10, play's pre-execute stage parsed its statement 26 to 28 times per
Run over the pipeline benchmark's three fixtures: 16 to 18 `Parse` calls and
10 body scans by `env.Extract`, one per registry unit. Only 4 to 8 of those
texts were distinct. A pass that rewrites nothing hands the next pass the same
string, and the next pass parses it again. Parsing was about half the stage's
CPU and the environment scans another 18%. ADR-0304 and ADR-0305 made each
parse cheaper; the number of parses was unchanged.

Both operations are deterministic in their input text.

## Decision

We will memoise successful parses and environment extractions by their exact
input text, in process-wide, byte-budgeted LRU memos.

- `Parse` and `ParseCanonical` each keep a memo keyed by the text. A hit
  returns the earlier `ParseResult`. Failures are not kept.
- `Pass.Run` and `Pass.RunProfiled` extract the environment through a memo
  keyed by the whole statement. The cached environment is a template: each
  run gets a clone, because passes write to the environment they are given.
- **A `ParseResult` is shared and read-only.** Every caller that parses the
  same text receives the same value, on any goroutine. After a clean parse its
  token stream has fetched `EOF` and does not change. Nothing in the
  repository writes to a tree, a token or the stream after a parse; the
  contract is stated on `ParseResult`.
- A finished parser is detached from the shared DFA. It keeps the immutable
  ATN, so rule names and `GetATN` still work, but not the simulator or the
  LL-island listener (ADR-0305). Otherwise a cached result would keep a DFA
  alive after the bounded cache ([ADR-0084](./0084-nanopass-antlr-dfa-cache-bounding.md))
  had rebuilt.
- Each memo is charged an estimate of what an entry retains. A retained parse
  measured about 7.5 KB plus 57 bytes per source byte, so 570 KB for the 10 KB
  applet fixture. The default budget is 8 MiB per memo, so a statement over
  about 128 KB is never cached and still pays every parse. A count cap of 256
  entries backstops the budget against a flood of small statements; for those
  the cap binds before the budget does. `SetMemoBudget` resizes the memos, zero
  disables them, and `MemoStats` reports their hits and occupancy.

The memo is keyed by text rather than scoped to a run because passes call
`Parse` as a free function, with no handle on the run they belong to. A text
key needs no such handle, catches the same repeats within a run, and also
answers a re-run of an unchanged statement.

## Alternatives

- **Extract the environment once per registry stage**, keeping `(env, body)`
  between units instead of integrating and re-extracting. `Extract` and
  `Integrate` round-trip normalise but are not inverse. The read-only views
  (statement settings, `FORMAT`, slot types) are refreshed from the body only
  by re-extraction, so a kept environment would go stale after a pass rewrote
  the body. The memo saves the same scans for unchanged text and still
  re-extracts changed text.
- **A run-scoped CST cache threaded through the runner**, as ADR-0006
  sketched. Every pass would need a handle on the run to reach it, which
  changes every pass. It catches no repeat the text key misses.
- **One cache shared between the environment scan and `Parse`.** Text the
  scan saw had usually been parsed already, but the scan's own repeats
  covered all but at most two of those texts per run. Sharing would also
  couple env to nanopass's token stream and error handling.
- **A cache bounded by count alone.** Entries range from a few kilobytes to
  megabytes, so a count says nothing about memory; it is kept only as the
  backstop above.

## Consequences

### Positive

- On 2026-10-10, with each iteration starting from an empty memo:
  `BenchmarkPlayPipelineApply/small` went from 349 to 87 µs, `/medium` from
  8.9 to 2.5 ms, and `/applet_9kb` from 34.4 to 11.9 ms. Allocations per run
  fell by 65–78%. Benchmarks of a single pass or a bare parse did not move.
- A re-run of an unchanged statement is answered from the memo, provided its
  intermediate texts still fit the budget. A statement too large to cache, or
  a run with more distinct texts than fit, gets less or nothing.

### Negative

- Read-only is a convention. Go cannot enforce it, and a caller that writes
  to a shared `ParseResult` corrupts it for every other holder.
- Memory stays retained between runs, up to the budget of each of the three
  memos. The charge is an estimate, not a measurement.
- `PredictionStats` counts parses, not `Parse` calls: a hit counts nothing. A
  test that asserts what one parse does turns the memo off first.
- A profiled re-run (ADR-0192 §SD4) can find the memo warmed by the shipped
  run or an earlier one, so its durations describe a warm memo. That is a
  second instance of the cold-versus-warm caveat ADR-0192 already states for
  the DFA.
- Benchmarks of a pass or pipeline must empty the memo at each iteration,
  otherwise they measure lookups.

### Neutral

- Results do not depend on the memo. `TestPassGolden`, whose golden file
  predates it, still matches every pass output over its fixtures.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `nanopass.Parse`, `ParseCanonical` | memoised; results shared and read-only | callers treat `ParseResult` as read-only |
| `nanopass.Pass.Run`, `RunProfiled` | environment extraction memoised; each run gets a clone | — |
| `nanopass.SetMemoBudget`, `MemoStats`, `DefaultMemoBudget`, `MemoStat` | added | benchmarks and parse-asserting tests set the budget |
| `env.Environment.Clone` | added | — |

## Migration — Tier 1

- **Breaks.** No signature changes. A caller that wrote to a `ParseResult`
  would now share the write; none in the repository does.
- **Path.** None.
- **Old shape.** `SetMemoBudget(0)` restores parse-per-call behaviour.

## Verification plan — Tier 1

- **Lane.** Default `go test`.
  - `TestPassGolden` compares every pass output with the memo enabled.
  - The `nanopass_memo_test.go` tests cover: a shared hit; grammars kept
    apart; failures not kept; a zero budget disabling the memo; detached
    prediction; concurrent hits under `-race`; a clone per run; and
    memoised extraction equal to `env.Extract`.
  - `TestLLIslandsAreLoadBearing` turns the memo off and requires that its
    parse was counted.
- **What would fail.** Handing out the template environment instead of a
  clone fails `TestExtractMemoHandsEachRunItsOwnEnvironment`. Dropping the
  detach fails `TestParseResultIsDetachedFromPrediction`. Both were checked by
  mutation.
- **Gap.** Read-only use of a shared `ParseResult` is not checked
  mechanically.

## Status

Accepted 2026-10-10.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0002](./0002-nanopass-discipline.md) — re-parse per pass, and the condition for revisiting it.
- [ADR-0006](./0006-nanopass-environment-and-first-class-pass.md) — the environment, `Pass.Run`, and the deferred parse-once optimisation.
- [ADR-0084](./0084-nanopass-antlr-dfa-cache-bounding.md) — the bounded DFA cache a cached result must not pin.
- [ADR-0125](./0125-codeview-prepare-memo.md) — the byte-budgeted memo pattern this follows.
- [ADR-0192](./0192-nanopass-cost-profiling.md) — the cost profile, and the trace's cold-versus-warm caveat.
