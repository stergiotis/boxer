---
type: adr
status: accepted
date: 2026-10-10
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-10
---

# ADR-0305: LL islands — predict in LL at the positions where SLL is known to choose wrong

## Context

[ADR-0196](./0196-nanopass-two-stage-sll-parsing.md) parses under SLL first
and re-parses under full-context LL when SLL reports anything.
[ADR-0304](./0304-sll-exact-column-qualifiers.md) removed the
largest class of SLL rejections. What remained, over every statement the test
suite parses, were three positions — 131 of 15,302 grammar1 inputs, 14 of 282
in grammar2 — and they are common in authored SQL:

- the middle operand of `x BETWEEN a AND b`: after `a`, continuing with a
  binary `AND` looks as good to SLL as stopping for BETWEEN's own `AND`;
- the operand of `CAST(x AS T)`: the alias suffix `x AS name` looks as good as
  stopping for CAST's `AS`;
- a FROM or JOIN item `f(…)`: a table name followed by `(` looks as good as a
  table function, because `INSERT INTO t (…)` lets a tableIdentifier be
  followed by a parenthesis elsewhere in the grammar.

In each, SLL — which carries no call stack and continues past a rule's end into
every place the rule is invoked — sees two viable alternatives, takes the
lowest, and fails a token later. Every statement containing one of them paid
SLL plus a full LL parse, and LL never memoises the decisions it resolves with
full context: the 572-byte benchmark statement, which carries one BETWEEN,
cost 2.1 ms against 0.27 ms on the fast path.

The grammar cannot express the fix ADR-0304 used. The middle operand of
BETWEEN is the left-recursive rule columnExpr invoked at precedence 0, and
ANTLR offers no syntax to invoke it at another (`rule columnExpr has no
defined parameters`); giving the operand a rule of its own changes the tree
type every consumer of a BETWEEN reads, and restricting it narrows the
accepted language.

## Decision

We will predict in LL inside a small, named set of rule invocations — *islands*
— during the SLL stage, and in SLL everywhere else.

An island is identified by the parent of the invocation: a columnExpr under a
`ColumnExprBetweenContext` or a `ColumnExprCastContext`, a tableExpr under a
`JoinExprTableContext` (grammar2 has no CAST sugar and takes the other two). The
grammar packages name their islands in a hand-written file beside the generated
code (`LLIsland`); `antlr4utils.DFACache.AcquireMode` installs a parse
listener that follows the current context through every rule entry and exit —
including the re-parenting left-recursive rules do — and switches the
simulator's prediction mode as the parse moves in and out of an island. The LL
stage is LL throughout, unchanged.

LL mode still predicts from the shared DFA and escalates to full context only
on an SLL conflict, so an island costs nothing where SLL would have been right.

The two-stage fallback stays. SLL with islands is shown equal to LL by
measurement, not by construction, and the fallback is also what reports a
genuine syntax error with LL's diagnostics.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `antlr4utils.DFACache.AcquireMode`, `SetLLIslands`, `IslandFunc` | added | every parse seam calls `AcquireMode` (nanopass ×2, env, play) |
| `grammar1.LLIsland`, `grammar2.LLIsland` | added; registered on `SharedDFA` at init | a grammar edit that adds an SLL-misresolved position adds an island |
| `nanopass.PredictionStats` | unchanged; the fallback count drops to genuine errors on the corpus | — |

## Alternatives

- **A grammar fix per position.** Not available without changing tree types or
  the accepted language; see Context.
- **LL everywhere.** Correct, but LL escalates to full context at every SLL
  conflict, including the many it would resolve correctly anyway — the `t.c`
  ambiguity and the alias context-sensitivity ADR-0196 §SD6 measured — which
  is what made the fallback slow.
- **A hand-written predicate in the grammar.** Puts Go into a grammar that has
  none and re-derives LL's choice by hand.
- **A wider island set** (every columnExpr under a table-function argument,
  say). Measured unnecessary: the three positions cover every rejection in the
  corpus, and an island outside them only adds escalations.

## Consequences

### Positive

- Over 15,762 test-suite inputs, SLL with islands accepts every statement LL
  accepts, in both grammars, with the same tree; 613,358 outputs of every pass,
  the scopes, the analyses, highlighting, the AST and the parse tree were
  compared against the previous commit with no difference.
- Statements with BETWEEN, CAST or a table function stay on the memoised path.
  On 2026-10-10: `BenchmarkNanopassParse/medium` 2.1 → 0.27 ms,
  `/large` 44 → 3.1 ms, `BenchmarkPlayPipelineApply/medium` 65 → 8.2 ms;
  statements without those constructs, such as the applet buffer, do not move.

### Negative

- Correctness of the fast path now rests on a curated list. A grammar edit
  that introduces a new position where SLL's lowest alternative is wrong is
  caught only if a statement exercising it is in the corpus — and then as a
  fallback (slower), not as a wrong tree, unless SLL builds a different clean
  tree, which the differential tests exist to catch.
- No known valid statement reaches the LL fallback any more, so its rescue path
  is tested through `TwoStage` directly rather than through a parse.
- A parse listener runs on every rule entry and exit of the SLL stage. Measured
  within noise of plain SLL over the corpus.

### Neutral

- The islands are a property of each grammar, registered once at init on its
  shared holder, beside the FastSync table the holder already owns.

## Migration — Tier 1

- **Breaks.** Nothing; `DFACache.Acquire` is unchanged.
- **Path.** A new parse seam calls `AcquireMode(parser, mode)` and assigns the
  returned simulator, instead of `Acquire` plus `SetPredictionMode`.
- **Regeneration.** None.
- **Old shape.** `Acquire` stays for callers that manage the mode themselves.

## Verification plan — Tier 1

- **Lane.** Default `go test`: `TestLLIslandsAreLoadBearing` (each fixture is
  rejected by plain SLL, accepted by stage one with no fallback counted, and
  gets LL's tree), `TestLLIslandsMatchLL` (SLL with islands equals LL wherever
  LL accepts, over the in-tree fixtures), the `TwoStage` unit tests, and
  `TestPassGolden`, unchanged by this decision.
- **What would fail.** Removing an island fails the first test with a counted
  fallback; an island that changed a tree fails the second.
- **Gap.** The corpus-wide differential is once-off; the in-tree fixtures are
  a slice of it.

## Status

Accepted 2026-10-10.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0196](./0196-nanopass-two-stage-sll-parsing.md) — two-stage parsing.
- [ADR-0304](./0304-sll-exact-column-qualifiers.md) — the grammar repair for qualified names, and the classes it left.
- [antlr4-go — what the runtime and the generated parser cost](../adr-background-work/antlr4-go-runtime-and-codegen-review.md) — measurements.
- [Adaptive LL(\*) Parsing: The Power of Dynamic Analysis](https://www.antlr.org/papers/allstar-techreport.pdf) — Parr, Harwell, Fisher; SLL, LL and the conditions under which they differ.
