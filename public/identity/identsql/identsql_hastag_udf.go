package identsql

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/identity/identifier"
)

// The UDF form of LW_ID_HAS_TAG derives the tag's id range from the tag value
// with scalar integer arithmetic only, so that ClickHouse folds it to a
// constant when the tag value is a literal and the primary-key analysis
// (KeyCondition) sees `intDiv(id, divisor) = code`, a monotonic function of
// the key with a constant on the other side, and prunes granules the way it
// prunes the macro's BETWEEN. A lambda anywhere in that arithmetic — arrayFold,
// arrayMap — would defeat this: the analyzer folds a call only when every
// argument is a constant node, and a lambda never is. The derivation and the
// server observations behind these choices are in
// doc/explanation/clickhouse-udf-primary-key-pruning.md.
//
// The arithmetic is the greedy Zeckendorf decomposition of the tag value over
// F(47)..F(2) — the same code the Go encoder produces for tagValue - 1 with
// its bias cancelled, so the fib-weighted sum of the code bits is the tag
// value itself. Each digit is intDiv(remainder, F(k)) and is 0 or 1 because
// the greedy remainder stays below F(k+1) < 2·F(k); the next remainder is
// remainder % F(k). The remainders are threaded through expression aliases
// so every one is written once: the analyzer resolves aliases inside a UDF
// body in the lambda's own scope, so two calls in one query do not collide.
// (The pre-analyzer path, `enable_analyzer = 0`, inlined UDFs at AST level and
// leaked those aliases into the query scope; the emitted body assumes the
// analyzer.)
//
// The code bits are MSB-aligned: the F(2) digit sits at bit 63 and the F(K)
// digit, K the largest index used, at bit 65-K; the comma follows at bit
// 64-K, which is also the body divisor. The lowest set bit of the code is
// therefore twice the divisor, recovered as bitAnd(code, bitNot(code) + 1) —
// all UInt64, never the `UInt64 - 1` that widens to Int64. greatest(·, 1)
// keeps tag value 0 (code 0) from dividing by zero; the domain guard then
// makes the whole predicate false, as the macro's error does at expansion
// time. intDiv, not intDivOrZero: only intDiv reports monotonicity.

// tagFibs holds F(2)..F(47), every Fibonacci number a uint32 tag value can
// carry (F(48) exceeds MaxTagValue). Index i is F(i+2).
var tagFibs = func() (fibs []uint64) {
	fibs = []uint64{1, 2}
	for fibs[len(fibs)-1]+fibs[len(fibs)-2] <= uint64(identifier.MaxTagValue) {
		fibs = append(fibs, fibs[len(fibs)-1]+fibs[len(fibs)-2])
	}
	return
}()

// hasTagCodeDivisor is the Go twin of the SQL arithmetic in
// expandHasTagUdfBody, for the unit lock against the encoder: code is the
// MSB-aligned tag code without its comma, divisor the comma bit. For tv = 0
// or tv > MaxTagValue the result is meaningless (the SQL guards it away).
func hasTagCodeDivisor(tv uint64) (code uint64, divisor uint64) {
	rem := tv
	weight := uint64(1) << (65 - (len(tagFibs) + 1)) // bit of the F(47) digit
	for i := len(tagFibs) - 1; i >= 0; i-- {
		f := tagFibs[i]
		code += (rem / f) * weight
		rem %= f
		weight <<= 1
	}
	if code != 0 {
		divisor = (code & (^code + 1)) / 2
	}
	return
}

// expandHasTagUdfBody returns the LW_ID_HAS_TAG predicate over the argument
// expressions x (the id) and tv (the tag value) in the range form described
// at the top of this file. It is the UDF body; the macro expansion keeps
// expandHasTag, whose constant fold produces a smaller statement and whose
// non-constant fallback carries no aliases (a macro expands into the query's
// own scope, where aliases from two calls would collide).
func expandHasTagUdfBody(x string, tv string) string {
	var sb strings.Builder
	sb.Grow(4096)
	fmt.Fprintf(&sb, "(toUInt64(%s) BETWEEN 1 AND %d) AND intDiv(%s, (greatest(intDiv(bitAnd((", tv, uint64(identifier.MaxTagValue), x)
	prev := fmt.Sprintf("toUInt64(%s)", tv)
	weight := uint64(1) << (65 - (len(tagFibs) + 1))
	for i := len(tagFibs) - 1; i >= 0; i-- {
		k := i + 2
		if i != len(tagFibs)-1 {
			sb.WriteString(" + ")
		}
		fmt.Fprintf(&sb, "intDiv((%s AS _lw_r%d), %d) * %d", prev, k, tagFibs[i], weight)
		prev = fmt.Sprintf("_lw_r%d %% %d", k, tagFibs[i])
		weight <<= 1
	}
	sb.WriteString(") AS _lw_code, bitNot(_lw_code) + 1), 2), 1) AS _lw_div)) = intDiv(_lw_code, _lw_div) + 1")
	return sb.String()
}
