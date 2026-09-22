package identsql

import (
	"fmt"
	"strings"
)

// The tag value of an id is the Fibonacci-weighted popcount of its digit
// bits — Σ bit_j · F(j+2) over the digit positions j = 0 (bit 63) up to the
// bit above the comma. The first decoder spelled that as
// arraySum(arrayMap(j -> …, range(width - 1))), which builds an array per
// row and evaluates the lambda per element: about 700 ns per row single-
// threaded against 25 ns for the comma search it starts from (2026-09-22,
// ClickHouse 26.8.1, 17.75 M ids of width 13–27). The sum is instead split
// into fixed-width chunks of the digit word, each a lookup into a constant
// table whose entry b holds the weights of the bits set in b at that chunk's
// positions: 8-bit chunks (six lookups, 256-entry tables) measured ~120 ns
// per row, 4-bit chunks (twelve lookups, 16-entry tables) ~330 ns. No
// lambda, so the expression also folds for a constant argument.
//
// Two guards make the tables sufficient. Positions up to 47 cover every
// width a uint32 tag value can have (47 bits: 46 digits and the comma); an
// id whose comma sits lower reads as invalid, which agrees with the Go
// decoder because the greedy code's last digit is always set, so width 48
// already means a value of at least F(48) > MaxTagValue. A width-47 code can
// still exceed MaxTagValue, hence the value guard that was there before.
//
// The UDF form takes 8-bit chunks and names the digit word and the sum with
// aliases (the analyzer scopes them to the call, see identsql_hastag_udf.go);
// the macro form takes 4-bit chunks and no aliases, because it expands into
// the query's own scope where two calls would collide, and because the
// expansion is statement text: a 4-bit body is ~9 KB per call spliced, an
// 8-bit one would be ~45 KB against the default 256 KB max_query_size.

const (
	tagValueDigitPositions = 48 // positions 0..47; a uint32 tag's digits end at 45
	udfTagValueChunkBits   = 8
	macroTagValueChunkBits = 4
)

// digitFibWeights[j] is F(j+2), the weight of the digit at position j.
var digitFibWeights = func() (w []uint64) {
	w = []uint64{1, 2}
	for len(w) < tagValueDigitPositions {
		w = append(w, w[len(w)-1]+w[len(w)-2])
	}
	return
}()

// digitsExpr is the tag's digit word: x with the comma and the body masked
// off, meaningful when pairs != 0. The comma-and-body mask is the all-ones
// word shifted right by width - 1, built shift-only for the same reasons as
// bodyMaskExpr.
func digitsExpr(x string, pairs string) string {
	return fmt.Sprintf("bitAnd(%s, bitNot(bitShiftRight(bitNot(toUInt64(0)), toUInt8(%s - 1))))", x, widthRawExpr(pairs))
}

// chunkTableSql is the constant lookup table for the chunk covering digit
// positions chunkBits*p .. chunkBits*p+chunkBits-1, entry b (1-based in
// arrayElement) being the weight sum of the bits set in b, MSB first.
func chunkTableSql(chunkBits int, p int) string {
	size := 1 << chunkBits
	entries := make([]string, 0, size)
	for b := 0; b < size; b++ {
		var s uint64
		for i := 0; i < chunkBits; i++ {
			if b&(1<<(chunkBits-1-i)) != 0 {
				s += digitFibWeights[chunkBits*p+i]
			}
		}
		entries = append(entries, fmt.Sprintf("%d", s))
	}
	return "[" + strings.Join(entries, ",") + "]::Array(UInt64)"
}

// tagValueSumExpr is the digit word's Fibonacci-weighted popcount as chunk
// lookups. With digitsAlias set, the digit word is written once and the
// alias reused (UDF form); empty, it is spliced per chunk (macro form).
func tagValueSumExpr(x string, pairs string, chunkBits int, digitsAlias string) string {
	digits := digitsExpr(x, pairs)
	nChunks := tagValueDigitPositions / chunkBits
	parts := make([]string, 0, nChunks)
	for p := 0; p < nChunks; p++ {
		d := digits
		if digitsAlias != "" {
			if p == 0 {
				d = "(" + digits + " AS " + digitsAlias + ")"
			} else {
				d = digitsAlias
			}
		}
		shift := 64 - chunkBits*(p+1)
		parts = append(parts, fmt.Sprintf("arrayElement(%s, toUInt16(bitAnd(bitShiftRight(%s, %d), %d)) + 1)", chunkTableSql(chunkBits, p), d, shift, (1<<chunkBits)-1))
	}
	return "(" + strings.Join(parts, " + ") + ")"
}

// tagValueExpr is the guarded tag value: 0 for a comma-less id, 0 when the
// comma sits below the widest uint32 code, 0 when the sum exceeds
// MaxTagValue, else the sum. sumAlias, when set, names the sum so the guard
// does not splice it twice.
func tagValueExpr(x string, chunkBits int, digitsAlias string, sumAlias string) string {
	p := pairsExpr(x)
	s := tagValueSumExpr(x, p, chunkBits, digitsAlias)
	first, again := s, s
	if sumAlias != "" {
		first, again = "("+s+" AS "+sumAlias+")", sumAlias
	}
	return fmt.Sprintf("toUInt32(if(%s = 0, 0, if(%s > 47, 0, if(%s > 4294967295, 0, %s))))", p, widthRawExpr(p), first, again)
}
