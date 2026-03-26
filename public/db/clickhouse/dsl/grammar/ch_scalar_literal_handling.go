//go:build llm_generated_opus46

// Package chliteral provides unescaping and unmarshalling of ClickHouse SQL literals.
//
// It handles the following ClickHouse literal forms as defined by the ANTLR4 grammar:
//
//   - STRING_LITERAL:        'hello', 'it”s', 'back\\slash'
//   - DECIMAL_LITERAL:       42, 0
//   - OCTAL_LITERAL:         0777
//   - HEXADECIMAL_LITERAL:   0xFF
//   - FLOATING_LITERAL:      3.14, 1e10, .5e2, 0x1p10
//   - INF / INFINITY:        Inf, INF, infinity
//   - NAN_SQL:               NaN, nan
//   - NULL_SQL:              NULL, null
//   - JSON_TRUE / JSON_FALSE: true, false
//   - numberLiteral:         optional +/- sign followed by any numeric literal, INF, or NAN
package grammar

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// LiteralTypeE enumerates the kinds of ClickHouse SQL literals.
type LiteralTypeE int

const (
	LiteralString  LiteralTypeE = iota // single-quoted string
	LiteralInt                         // integer (decimal, octal, hex)
	LiteralFloat                       // floating-point, Inf, NaN
	LiteralBool                        // true / false
	LiteralNull                        // NULL
	LiteralUnknown                     // unrecognised input
)

func (inst LiteralTypeE) String() string {
	switch inst {
	case LiteralString:
		return "String"
	case LiteralInt:
		return "Int"
	case LiteralFloat:
		return "Float"
	case LiteralBool:
		return "Bool"
	case LiteralNull:
		return "Null"
	default:
		return "Unknown"
	}
}

// Literal is the result of unmarshalling a ClickHouse SQL literal token.
type Literal struct {
	Type LiteralTypeE

	// Populated fields depend on Type:
	//   LiteralString → StringVal
	//   LiteralInt    → IntVal
	//   LiteralFloat  → FloatVal
	//   LiteralBool   → BoolVal
	//   LiteralNull   → (no value)
	StringVal string
	IntVal    int64
	FloatVal  float64
	BoolVal   bool
}

// UnescapeString removes the surrounding single quotes from a ClickHouse
// STRING_LITERAL token and resolves all escape sequences.
//
// The ClickHouse lexer rule is:
//
//	STRING_LITERAL: QUOTE_SINGLE ( ~([\\']) | (BACKSLASH .) | (QUOTE_SINGLE QUOTE_SINGLE) )* QUOTE_SINGLE;
//
// Supported escape sequences (after backslash):
//
//	\\  →  \
//	\'  →  '
//	\n  →  newline
//	\t  →  tab
//	\r  →  carriage return
//	\0  →  NUL
//	\b  →  backspace
//	\f  →  form feed
//	\a  →  bell
//	\v  →  vertical tab
//	\xHH          → byte value
//	\uHHHH        → Unicode code point (BMP)
//	\UHHHHHHHH    → Unicode code point (full range)
//	\<other>      → <other> (backslash is dropped, character kept)
//
// Doubled single-quotes (”) are collapsed to one single-quote.
func UnescapeString(raw string) (result string, err error) {
	if len(raw) < 2 || raw[0] != '\'' || raw[len(raw)-1] != '\'' {
		err = fmt.Errorf("chliteral.UnescapeString: input must be a single-quoted string, got %q", raw)
		return
	}
	inner := raw[1 : len(raw)-1]
	var buf strings.Builder
	buf.Grow(len(inner))

	i := 0
	for i < len(inner) {
		ch := inner[i]
		switch {
		case ch == '\\' && i+1 < len(inner):
			next := inner[i+1]
			switch next {
			case '\\':
				buf.WriteByte('\\')
				i += 2
			case '\'':
				buf.WriteByte('\'')
				i += 2
			case 'n':
				buf.WriteByte('\n')
				i += 2
			case 't':
				buf.WriteByte('\t')
				i += 2
			case 'r':
				buf.WriteByte('\r')
				i += 2
			case '0':
				buf.WriteByte(0)
				i += 2
			case 'b':
				buf.WriteByte('\b')
				i += 2
			case 'f':
				buf.WriteByte('\f')
				i += 2
			case 'a':
				buf.WriteByte('\a')
				i += 2
			case 'v':
				buf.WriteByte('\v')
				i += 2
			case 'x':
				// \xHH — two hex digits
				if i+3 >= len(inner) {
					err = fmt.Errorf("chliteral.UnescapeString: truncated \\x escape at position %d", i)
					return
				}
				val, parseErr := strconv.ParseUint(inner[i+2:i+4], 16, 8)
				if parseErr != nil {
					err = fmt.Errorf("chliteral.UnescapeString: invalid \\x escape %q at position %d: %w", inner[i:i+4], i, parseErr)
					return
				}
				buf.WriteByte(byte(val))
				i += 4
			case 'u':
				// \uHHHH — four hex digits
				if i+5 >= len(inner) {
					err = fmt.Errorf("chliteral.UnescapeString: truncated \\u escape at position %d", i)
					return
				}
				val, parseErr := strconv.ParseUint(inner[i+2:i+6], 16, 32)
				if parseErr != nil {
					err = fmt.Errorf("chliteral.UnescapeString: invalid \\u escape %q at position %d: %w", inner[i:i+6], i, parseErr)
					return
				}
				buf.WriteRune(rune(val))
				i += 6
			case 'U':
				// \UHHHHHHHH — eight hex digits
				if i+9 >= len(inner) {
					err = fmt.Errorf("chliteral.UnescapeString: truncated \\U escape at position %d", i)
					return
				}
				val, parseErr := strconv.ParseUint(inner[i+2:i+10], 16, 32)
				if parseErr != nil {
					err = fmt.Errorf("chliteral.UnescapeString: invalid \\U escape %q at position %d: %w", inner[i:i+10], i, parseErr)
					return
				}
				if !utf8.ValidRune(rune(val)) {
					err = fmt.Errorf("chliteral.UnescapeString: invalid Unicode code point U+%04X at position %d", val, i)
					return
				}
				buf.WriteRune(rune(val))
				i += 10
			default:
				// Unknown escape: drop the backslash, keep the character.
				// This matches the ANTLR rule (BACKSLASH .) which accepts any character after backslash.
				buf.WriteByte(next)
				i += 2
			}
		case ch == '\'' && i+1 < len(inner) && inner[i+1] == '\'':
			// Doubled single-quote → single quote
			buf.WriteByte('\'')
			i += 2
		default:
			buf.WriteByte(ch)
			i++
		}
	}

	result = buf.String()
	return
}

// UnmarshalScalarLiteral parses a ClickHouse SQL literal token text and returns a typed Literal.
//
// The input should be the raw token text as it appears in SQL, including quotes for
// strings. Leading/trailing whitespace is trimmed.
//
// Recognised forms:
//
//	'string'       → LiteralString
//	42, 0777, 0xFF → LiteralInt
//	3.14, 1e10     → LiteralFloat
//	+42, -3.14     → signed numeric
//	Inf, NaN       → LiteralFloat
//	true, false    → LiteralBool
//	NULL           → LiteralNull
func UnmarshalScalarLiteral(token string) (result Literal, err error) {
	token = strings.TrimSpace(token)
	if len(token) == 0 {
		err = fmt.Errorf("chliteral.UnmarshalScalarLiteral: empty input")
		return
	}

	// NULL (case-insensitive)
	if strings.EqualFold(token, "NULL") {
		result.Type = LiteralNull
		return
	}

	// Boolean
	if token == "true" {
		result.Type = LiteralBool
		result.BoolVal = true
		return
	}
	if token == "false" {
		result.Type = LiteralBool
		result.BoolVal = false
		return
	}

	// String literal
	if len(token) >= 2 && token[0] == '\'' {
		result.Type = LiteralString
		result.StringVal, err = UnescapeString(token)
		if err != nil {
			err = fmt.Errorf("chliteral.UnmarshalScalarLiteral: %w", err)
		}
		return
	}

	// Numeric literal — strip optional sign
	sign := int64(1)
	signF := 1.0
	numPart := token
	if len(token) > 0 && (token[0] == '+' || token[0] == '-') {
		if token[0] == '-' {
			sign = -1
			signF = -1.0
		}
		numPart = token[1:]
		if len(numPart) == 0 {
			err = fmt.Errorf("chliteral.UnmarshalScalarLiteral: bare sign %q", token)
			return
		}
	}

	// INF / INFINITY (case-insensitive)
	upper := strings.ToUpper(numPart)
	if upper == "INF" || upper == "INFINITY" {
		result.Type = LiteralFloat
		result.FloatVal = signF * math.Inf(1)
		return
	}

	// NaN (case-insensitive)
	if upper == "NAN" {
		result.Type = LiteralFloat
		result.FloatVal = math.NaN()
		return
	}

	// Hexadecimal integer: 0x or 0X prefix
	if len(numPart) > 2 && numPart[0] == '0' && (numPart[1] == 'x' || numPart[1] == 'X') {
		// Check if it's a hex float (has 'p'/'P' or '.')
		if containsAnyByte(numPart, "pP.") {
			result.Type = LiteralFloat
			result.FloatVal, err = parseHexFloat(numPart)
			if err != nil {
				err = fmt.Errorf("chliteral.UnmarshalScalarLiteral: invalid hex float %q: %w", token, err)
				return
			}
			result.FloatVal *= signF
			return
		}
		var val uint64
		val, err = strconv.ParseUint(numPart[2:], 16, 64)
		if err != nil {
			err = fmt.Errorf("chliteral.UnmarshalScalarLiteral: invalid hex literal %q: %w", token, err)
			return
		}
		result.Type = LiteralInt
		result.IntVal = sign * int64(val)
		return
	}

	// Octal integer: leading 0 followed by octal digits only
	if len(numPart) > 1 && numPart[0] == '0' && isOctalDigit(numPart[1]) && !containsAnyByte(numPart, ".eE") {
		var val uint64
		val, err = strconv.ParseUint(numPart[1:], 8, 64)
		if err != nil {
			err = fmt.Errorf("chliteral.UnmarshalScalarLiteral: invalid octal literal %q: %w", token, err)
			return
		}
		result.Type = LiteralInt
		result.IntVal = sign * int64(val)
		return
	}

	// Floating-point: contains '.', 'e', or 'E'
	if containsAnyByte(numPart, ".eE") {
		result.Type = LiteralFloat
		result.FloatVal, err = strconv.ParseFloat(numPart, 64)
		if err != nil {
			err = fmt.Errorf("chliteral.UnmarshalScalarLiteral: invalid float literal %q: %w", token, err)
			return
		}
		result.FloatVal *= signF
		return
	}

	// Decimal integer
	{
		var val uint64
		val, err = strconv.ParseUint(numPart, 10, 64)
		if err != nil {
			err = fmt.Errorf("chliteral.UnmarshalScalarLiteral: unrecognised literal %q: %w", token, err)
			result.Type = LiteralUnknown
			return
		}
		result.Type = LiteralInt
		result.IntVal = sign * int64(val)
		return
	}
}

// EscapeString produces a ClickHouse single-quoted string literal from a Go string.
// This is the inverse of UnescapeString.
func EscapeString(s string) string {
	var buf strings.Builder
	buf.Grow(len(s) + 2)
	buf.WriteByte('\'')
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch ch {
		case '\\':
			buf.WriteString("\\\\")
		case '\'':
			buf.WriteString("\\'")
		case '\n':
			buf.WriteString("\\n")
		case '\t':
			buf.WriteString("\\t")
		case '\r':
			buf.WriteString("\\r")
		case 0:
			buf.WriteString("\\0")
		default:
			buf.WriteByte(ch)
		}
	}
	buf.WriteByte('\'')
	return buf.String()
}

// MarshalScalarLiteral converts a Literal back into its ClickHouse SQL text representation.
func MarshalScalarLiteral(lit Literal) (result string, err error) {
	switch lit.Type {
	case LiteralNull:
		result = "NULL"
	case LiteralBool:
		if lit.BoolVal {
			result = "true"
		} else {
			result = "false"
		}
	case LiteralString:
		result = EscapeString(lit.StringVal)
	case LiteralInt:
		result = strconv.FormatInt(lit.IntVal, 10)
	case LiteralFloat:
		if math.IsInf(lit.FloatVal, 1) {
			result = "Inf"
		} else if math.IsInf(lit.FloatVal, -1) {
			result = "-Inf"
		} else if math.IsNaN(lit.FloatVal) {
			result = "NaN"
		} else {
			result = strconv.FormatFloat(lit.FloatVal, 'g', -1, 64)
		}
	default:
		err = fmt.Errorf("chliteral.MarshalScalarLiteral: unknown literal type %v", lit.Type)
	}
	return
}

// containsAnyByte returns true if s contains any byte from chars.
func containsAnyByte(s string, chars string) bool {
	for i := 0; i < len(s); i++ {
		for j := 0; j < len(chars); j++ {
			if s[i] == chars[j] {
				return true
			}
		}
	}
	return false
}

// isOctalDigit returns true if b is an ASCII octal digit.
func isOctalDigit(b byte) bool {
	return b >= '0' && b <= '7'
}

// parseHexFloat parses a C99/Go hex float like 0x1.fp10 or 0xAp-3.
func parseHexFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}
