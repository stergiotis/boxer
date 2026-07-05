// Package identsql is the ClickHouse surface of the fibonacci-coded tagged
// identifiers (ADR-0106 SD5): the LW_ID_* macro family as a nanopass
// expansion pass, and the equivalent CREATE FUNCTION statements so
// hand-written SQL against a prepared database can use the same names
// unexpanded.
//
// The SQL split mirrors identifier's Go split bit for bit (the SD2 contract):
// pairs = bitAnd(x, x<<1) marks the upper bit of every adjacent 11 pair; the
// highest such bit is the tag comma. ClickHouse has no leading-zeros builtin,
// so its position is recovered exactly with roundToExp2 (integer power-of-two
// floor) and bitCount instead of the Go bits.LeadingZeros64. Invalid ids —
// no comma anywhere — yield 0 from every macro, matching the Go methods.
//
// Expansions splice the argument expression several times; ClickHouse's
// common-subexpression elimination evaluates textually identical
// subexpressions once. LW_ID_HAS_TAG with a constant tag value folds at
// expansion time into a sargable BETWEEN over the tag's contiguous id range,
// which primary-index analysis can prune; no bit arithmetic survives into
// the query.
package identsql

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/identity/fibonaccicode"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The macro and UDF names (upper snake case per ADR-0106 SD5 update).
// Matching in SQL is case- and quoting-insensitive; the emitted UDFs use
// these exact spellings.
const (
	NameIsValid  = "LW_ID_IS_VALID"
	NameTagWidth = "LW_ID_TAG_WIDTH"
	NameTagBits  = "LW_ID_TAG_BITS"
	NameBody     = "LW_ID_BODY"
	NameTagValue = "LW_ID_TAG_VALUE"
	NameHasTag   = "LW_ID_HAS_TAG"
)

// fibWeightsSql is the ClickHouse array literal of F(2)..F(64) — the full
// uint64-safe Zeckendorf weight table, not just the uint32 tag-value span:
// arrayElement returns 0 beyond the array, so a shorter table would decode
// adversarial wide-width ids to a wrong non-zero value instead of tripping
// the >uint32 invalid guard the way the Go decoder does.
const fibWeightsSql = "[toUInt64(1),2,3,5,8,13,21,34,55,89,144,233,377,610,987,1597," +
	"2584,4181,6765,10946,17711,28657,46368,75025,121393,196418," +
	"317811,514229,832040,1346269,2178309,3524578,5702887,9227465," +
	"14930352,24157817,39088169,63245986,102334155,165580141," +
	"267914296,433494437,701408733,1134903170,1836311903,2971215073," +
	"4807526976,7778742049,12586269025,20365011074,32951280099," +
	"53316291173,86267571272,139583862445,225851433717,365435296162," +
	"591286729879,956722026041,1548008755920,2504730781961," +
	"4052739537881,6557470319842,10610209857723,17167680177565]"

// pairsExpr marks the upper bit of every adjacent 11 pair of x; 0 means x
// carries no fibonacci comma and is not a tagged id.
func pairsExpr(x string) string {
	return fmt.Sprintf("bitAnd(%s, bitShiftLeft(%s, 1))", x, x)
}

// widthRawExpr is the full tag width including the trailing comma bit,
// meaningful only when pairs != 0: with b the bit index of the highest pair
// (recovered exactly via roundToExp2), the width is 65 - b.
func widthRawExpr(pairs string) string {
	return fmt.Sprintf("(65 - bitCount(roundToExp2(%s) - 1))", pairs)
}

// bodyMaskExpr covers the 64 - width low bits, meaningful only when
// pairs != 0: the all-ones word shifted right by the width. Two server
// quirks shape this expression, both caught by the server-truth goldens:
// ClickHouse widens `UInt64 - 1` to Int64, whose bitNot goes negative and
// flips value-based comparisons even though the bit pattern is right — so
// the mask is built by shifting, keeping every intermediate UInt64; and
// bitShiftRight silently returns its input UNSHIFTED when the shift amount
// is a signed type (the bare `65 - bitCount(...)` is Int16) — so the width
// is forced to UInt8. A width of 64 (adversarial comma at the very bottom)
// shifts to exactly 0.
func bodyMaskExpr(pairs string) string {
	return fmt.Sprintf("bitShiftRight(bitNot(toUInt64(0)), toUInt8(%s))", widthRawExpr(pairs))
}

// zeckendorfSumExpr is the fib-weighted popcount of the tag bits of x. The
// encoder's value bias and the code's Zeckendorf bias cancel, so the sum IS
// the tag value (no ±1 anywhere). Meaningful only when pairs != 0.
func zeckendorfSumExpr(x string, pairs string) string {
	return fmt.Sprintf(
		"arraySum(arrayMap(j -> toUInt64(bitTest(%s, 63 - toUInt8(j))) * %s[j + 1], range(toUInt64(%s) - 1)))",
		x, fibWeightsSql, widthRawExpr(pairs))
}

func expandIsValid(x string) string {
	return fmt.Sprintf("(%s != 0)", pairsExpr(x))
}

func expandTagWidth(x string) string {
	p := pairsExpr(x)
	return fmt.Sprintf("toUInt16(if(%s = 0, 0, %s))", p, widthRawExpr(p))
}

func expandTagBits(x string) string {
	p := pairsExpr(x)
	return fmt.Sprintf("if(%s = 0, toUInt64(0), bitAnd(%s, bitNot(%s)))", p, x, bodyMaskExpr(p))
}

func expandBody(x string) string {
	p := pairsExpr(x)
	return fmt.Sprintf("if(%s = 0, toUInt64(0), bitAnd(%s, %s))", p, x, bodyMaskExpr(p))
}

func expandTagValue(x string) string {
	p := pairsExpr(x)
	s := zeckendorfSumExpr(x, p)
	return fmt.Sprintf("toUInt32(if(%s = 0, 0, if(%s > 4294967295, 0, %s)))", p, s, s)
}

// expandHasTag folds a constant tag value into the sargable BETWEEN over the
// tag's contiguous id range; a non-constant tag value falls back to decoding
// the id's tag value and comparing (correct, not index-prunable).
func expandHasTag(x string, tagValueArg string) (r string, err error) {
	trimmed := strings.TrimSpace(tagValueArg)
	tv, convErr := strconv.ParseUint(trimmed, 10, 64)
	if convErr == nil {
		if tv == 0 || tv > uint64(identifier.MaxTagValue) {
			err = eb.Build().Uint64("tagValue", tv).Errorf("%s: constant tag value out of the valid domain [1, 4294967295]", NameHasTag)
			return
		}
		code, nBits := fibonaccicode.EncodeFibonacciCode(tv - 1)
		hi := code | (uint64(1)<<(64-nBits) - 1)
		r = fmt.Sprintf("(%s BETWEEN %d AND %d)", x, code, hi)
		return
	}
	// The tv != 0 guard keeps an invalid id (tag value 0) from matching a
	// zero tag-value argument — both sides would decode to 0 otherwise.
	r = fmt.Sprintf("((%s) != 0 AND %s = toUInt32(%s))", tagValueArg, expandTagValue(x), tagValueArg)
	return
}

// UdfDdlStatements returns one CREATE OR REPLACE FUNCTION statement per
// LW_ID_* name, semantically identical to the macro expansions (LW_ID_HAS_TAG
// necessarily in its generic, non-folded form). Apply them to a database once
// and unexpanded LW_ID_* SQL runs as-is; ClickHouse SQL UDFs substitute their
// body textually, exactly like the expansion pass.
func UdfDdlStatements() (stmts []string) {
	x := "x"
	hasTagGeneric, _ := expandHasTag("x", "tag_value") // non-constant arg: never errors
	stmts = []string{
		fmt.Sprintf("CREATE OR REPLACE FUNCTION %s AS (x) -> %s", NameIsValid, expandIsValid(x)),
		fmt.Sprintf("CREATE OR REPLACE FUNCTION %s AS (x) -> %s", NameTagWidth, expandTagWidth(x)),
		fmt.Sprintf("CREATE OR REPLACE FUNCTION %s AS (x) -> %s", NameTagBits, expandTagBits(x)),
		fmt.Sprintf("CREATE OR REPLACE FUNCTION %s AS (x) -> %s", NameBody, expandBody(x)),
		fmt.Sprintf("CREATE OR REPLACE FUNCTION %s AS (x) -> %s", NameTagValue, expandTagValue(x)),
		fmt.Sprintf("CREATE OR REPLACE FUNCTION %s AS (x, tag_value) -> %s", NameHasTag, hasTagGeneric),
	}
	return
}

// arity of every macro; LW_ID_HAS_TAG is the only two-argument one.
var macroArity = map[string]int{
	nanopass.NormalizeCallName(NameIsValid):  1,
	nanopass.NormalizeCallName(NameTagWidth): 1,
	nanopass.NormalizeCallName(NameTagBits):  1,
	nanopass.NormalizeCallName(NameBody):     1,
	nanopass.NormalizeCallName(NameTagValue): 1,
	nanopass.NormalizeCallName(NameHasTag):   2,
}

// ExpandPass rewrites every LW_ID_* call into its bit-arithmetic expansion.
// A wrong arity is an error (an unexpanded macro would only run against a
// database that has the UDFs installed — inside this pass it is a misuse).
// Nested calls (an LW_ID_* inside another's argument) expand on the next
// fixpoint iteration, hence NeedsFixedPoint.
var ExpandPass = nanopass.LiftBodyPass("ExpandLwIdMacros", expandLwIdMacrosImpl, nanopass.PassProperties{
	NeedsFixedPoint: true,
	Reads:           nanopass.RegionBody,
	Writes:          nanopass.RegionBody,
})

func expandLwIdMacrosImpl(sql string) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eb.Build().Errorf("ExpandLwIdMacros: %w", err)
		return
	}
	rw := nanopass.NewRewriter(pr)
	err = expandWalk(pr, rw, pr.Tree)
	if err != nil {
		return
	}
	result = nanopass.GetText(rw)
	return
}

func expandWalk(pr *nanopass.ParseResult, rw nanopass.RewriterI, node antlr.Tree) (err error) {
	ctx, ok := node.(antlr.ParserRuleContext)
	if !ok {
		return
	}
	if funcExpr, isFn := ctx.(*grammar1.ColumnExprFunctionContext); isFn {
		if ident := funcExpr.Identifier(); ident != nil {
			name := nanopass.NormalizeCallName(ident.GetText())
			if wantArity, registered := macroArity[name]; registered {
				var args []string
				args, err = callArgTexts(pr, funcExpr)
				if err != nil {
					return
				}
				if len(args) != wantArity {
					err = eb.Build().Str("macro", ident.GetText()).Int("want", wantArity).Int("got", len(args)).Errorf("wrong LW_ID macro arity")
					return
				}
				var expansion string
				expansion, err = expandOne(name, args)
				if err != nil {
					return
				}
				nanopass.ReplaceNode(rw, funcExpr, expansion)
				return // subtree consumed; nested macros expand on the next fixpoint iteration
			}
		}
	}
	for i := 0; i < ctx.GetChildCount(); i++ {
		err = expandWalk(pr, rw, ctx.GetChild(i))
		if err != nil {
			return
		}
	}
	return
}

func expandOne(normalizedName string, args []string) (expansion string, err error) {
	x := "(" + args[0] + ")"
	switch normalizedName {
	case nanopass.NormalizeCallName(NameIsValid):
		expansion = expandIsValid(x)
	case nanopass.NormalizeCallName(NameTagWidth):
		expansion = expandTagWidth(x)
	case nanopass.NormalizeCallName(NameTagBits):
		expansion = expandTagBits(x)
	case nanopass.NormalizeCallName(NameBody):
		expansion = expandBody(x)
	case nanopass.NormalizeCallName(NameTagValue):
		expansion = expandTagValue(x)
	case nanopass.NormalizeCallName(NameHasTag):
		expansion, err = expandHasTag(x, args[1])
	}
	return
}

// callArgTexts returns the original source text of every argument expression.
func callArgTexts(pr *nanopass.ParseResult, funcExpr *grammar1.ColumnExprFunctionContext) (args []string, err error) {
	argList := funcExpr.ColumnArgList()
	if argList == nil {
		args = make([]string, 0)
		return
	}
	argListCtx, isArgList := argList.(*grammar1.ColumnArgListContext)
	if !isArgList {
		err = eb.Build().Errorf("unexpected argument list shape")
		return
	}
	args = make([]string, 0, argListCtx.GetChildCount())
	for i := 0; i < argListCtx.GetChildCount(); i++ {
		child, isArg := argListCtx.GetChild(i).(*grammar1.ColumnArgExprContext)
		if !isArg {
			continue
		}
		args = append(args, strings.TrimSpace(nanopass.NodeText(pr, child)))
	}
	return
}
