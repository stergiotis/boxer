package nanopass

import (
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Input guards make Parse/ParseCanonical total over arbitrary input. The
// ANTLR parser has two pathological regimes, measured empirically
// (2026-06-12, probe over synthetic inputs):
//
//   - Nested parentheses trigger adaptive-prediction lookahead that grows
//     roughly quadratically: depth 100 ≈ 1.5s, depth 400 ≈ 20s. CPU
//     exhaustion, not stack.
//   - Nested CASE expressions exhaust the goroutine stack (fatal, not
//     recoverable) somewhere between depth 16k and 64k. Prefix chains
//     (NOT …, unary minus) and bracket nesting stay linear and were fine
//     at depth 256k.
//
// MaxNestingDepth bounds both regimes: 128 keeps worst-case parse latency
// in low seconds while being an order of magnitude beyond human-written or
// generator-emitted SQL. MaxInputBytes rejects absurd payloads before any
// parsing work.
const (
	// MaxInputBytes is the maximum accepted SQL length in bytes.
	MaxInputBytes = 1 << 20

	// MaxNestingDepth is the maximum accepted nesting depth, counted
	// separately for brackets ((, [, {) and CASE…END expressions.
	MaxNestingDepth = 128
)

// CheckInputGuards validates sql against MaxInputBytes, MaxNestingDepth,
// and UTF-8 validity. The nesting scan is quote- and comment-aware: bracket
// or CASE text inside string literals, quoted identifiers, or comments does
// not count. Parse and ParseCanonical call this first; callers that want to
// pre-validate user input can call it directly.
//
// Invalid UTF-8 is rejected because the ANTLR runtime decodes input to
// runes: undecodable bytes become U+FFFD, so any rewriting pass would
// silently corrupt the original bytes (e.g. raw binary inside a string
// literal). Escape binary data instead of embedding it raw.
func CheckInputGuards(sql string) error {
	if len(sql) > MaxInputBytes {
		return eb.Build().
			Int("len", len(sql)).
			Int("max", MaxInputBytes).
			Errorf("input exceeds MaxInputBytes")
	}
	if !utf8.ValidString(sql) {
		return eb.Build().Errorf("input is not valid UTF-8")
	}

	bracketDepth := 0
	caseDepth := 0
	for i := 0; i < len(sql); {
		c := sql[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			i = skipQuoted(sql, i)
			continue
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			continue
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			i += 2
			for i+1 < len(sql) && !(sql[i] == '*' && sql[i+1] == '/') {
				i++
			}
			i += 2
			continue
		case c == '(' || c == '[' || c == '{':
			bracketDepth++
			if bracketDepth > MaxNestingDepth {
				return eb.Build().
					Int("max", MaxNestingDepth).
					Errorf("input exceeds MaxNestingDepth (brackets)")
			}
		case c == ')' || c == ']' || c == '}':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case isWordStart(sql, i, "case"):
			caseDepth++
			if caseDepth > MaxNestingDepth {
				return eb.Build().
					Int("max", MaxNestingDepth).
					Errorf("input exceeds MaxNestingDepth (CASE)")
			}
			i += 4
			continue
		case isWordStart(sql, i, "end"):
			if caseDepth > 0 {
				caseDepth--
			}
			i += 3
			continue
		}
		i++
	}
	return nil
}

// isWordStart reports whether the ASCII-case-insensitive keyword word
// starts at sql[i] with identifier boundaries on both sides.
func isWordStart(sql string, i int, word string) bool {
	if i+len(word) > len(sql) {
		return false
	}
	for j := 0; j < len(word); j++ {
		if lowerASCII(sql[i+j]) != word[j] {
			return false
		}
	}
	if i > 0 && isIdentByte(sql[i-1]) {
		return false
	}
	if i+len(word) < len(sql) && isIdentByte(sql[i+len(word)]) {
		return false
	}
	return true
}

func lowerASCII(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '$' ||
		(c >= '0' && c <= '9') ||
		(c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		c >= 0x80 // conservative: any non-ASCII byte continues an identifier
}
