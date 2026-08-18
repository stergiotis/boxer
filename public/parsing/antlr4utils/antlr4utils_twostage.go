package antlr4utils

import (
	"github.com/antlr4-go/antlr/v4"
)

// Two-stage parsing (ADR-0196).
//
// ANTLR's adaptive prediction has two modes. SLL ignores the parser call stack
// and is cheap, and — crucially — its decisions are memoised in the DFA cache.
// LL consults the full context; its decisions are context-dependent by
// construction, so ANTLR never writes them into the DFA and they are
// re-simulated on every parse. A grammar with an ambiguity that only full
// context can resolve therefore pays that simulation forever, and no amount of
// cache warming helps.
//
// grammar1 and grammar2 have exactly such an ambiguity on the WITH clause: the
// `ctes` and `withClause` rules have byte-identical right-hand sides and are
// both reachable at the same input position. One decision accounts for 98.2% of
// all full-context work on a real 9 KB buffer, and removing it takes a parse of
// that buffer from ~95 ms to ~1.2 ms.
//
// SLL is strictly weaker: it may report a syntax error on input LL accepts.
// (It may in principle also resolve an ambiguous decision to a different
// alternative; measured over the repo's SQL corpus that never happened — see
// ADR-0196 §Verification.) So SLL is an optimisation, not a replacement, and
// the LL fallback below is load-bearing: without it four existing tests fail.
//
// ANTLR's documented recipe pairs stage one with BailErrorStrategy so it aborts
// at the first error. antlr4-go v4.13.1's BailErrorStrategy panics
// (RecoverInline returns a nil token the generated parser then dereferences),
// so stage one here runs the ordinary error strategy and is judged by its
// collected diagnostics. A rejected parse consequently runs error recovery
// before falling back.

// TwoStage attempts a parse under SLL prediction and, if that attempt reports
// any diagnostic, runs it again under LL.
//
// attempt is called with the prediction mode to use and reports whether the
// parse was clean. It must build a fresh parser each time — a parser cannot be
// re-run — and it must not surface the SLL attempt's diagnostics anywhere the
// caller can see them, because a fallback makes them moot.
//
// ok is the second attempt's verdict when one ran, so a genuine syntax error is
// reported with LL's diagnostics exactly as it was before two-stage parsing.
// fellBack reports whether the LL attempt ran, for the hit/miss accounting that
// tells an operator whether SLL is paying for their workload.
func TwoStage[T any](attempt func(predictionMode int) (T, bool)) (result T, ok bool, fellBack bool) {
	if result, ok = attempt(antlr.PredictionModeSLL); ok {
		return
	}
	fellBack = true
	result, ok = attempt(antlr.PredictionModeLL)
	return
}
