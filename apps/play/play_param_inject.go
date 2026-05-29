//go:build llm_generated_opus47

package play

import (
	"strings"
)

// SyncParamPrelude rewrites the leading `SET param_*` block of sql
// so it exactly matches the widget-authored (name, value) pairs, in
// placeholder-occurrence order. Encoding is keyed off each slot's
// Type (numeric → verbatim if numeric-shape, compound → verbatim,
// other → single-quoted with escapes); see encodeParamLiteral.
//
// Idempotent: returns (sql, false) when the existing prelude already
// matches the desired one. Returns (sql, false) on ExtractParams
// error — a transient keystroke that breaks the parse should not
// destroy the user's prelude.
//
// Trailing prelude SETs whose name isn't in values are dropped on
// rewrite; this is how a deleted placeholder stops contributing to
// the prelude. Non-param SETs intermixed with param SETs are *not*
// preserved in the leading block — they may shift downward after a
// rewrite. In practice users keep non-param SETs in a trailing
// block; the play app's pristine output stays stable across re-syncs.
func SyncParamPrelude(sql string, slots []paramSlot, values map[string]string) (out string, changed bool) {
	residual, _, err := ExtractParams(sql)
	if err != nil {
		out = sql
		return
	}
	desired := buildParamPrelude(slots, values) + residual
	if desired == sql {
		out = sql
		return
	}
	out = desired
	changed = true
	return
}

// buildParamPrelude formats one `SET param_<Name> = <encoded>;\n`
// line per slot in `slots` that has a value in `values`. Order
// follows slots, so the prelude tracks placeholder occurrence in the
// buffer.
func buildParamPrelude(slots []paramSlot, values map[string]string) string {
	if len(slots) == 0 || len(values) == 0 {
		return ""
	}
	var b strings.Builder
	for _, s := range slots {
		v, ok := values[s.Name]
		if !ok {
			continue
		}
		b.WriteString("SET param_")
		b.WriteString(s.Name)
		b.WriteString(" = ")
		b.WriteString(encodeParamLiteral(v, s.Type))
		b.WriteString(";\n")
	}
	return b.String()
}

// encodeParamLiteral renders a widget-authored value as the SQL
// literal the ClickHouse parser will accept inside `SET param_X =
// ...`, using the slot's Type to decide which encoding bucket the
// value belongs to. The inverse pairing is `chParamValue` in
// play_extract_params.go (which strips outer quotes on the way back).
//
// Buckets:
//   - empty string → `''`. CH treats `param_X=` (missing) as
//     "param wasn't set" which doesn't round-trip cleanly with a
//     deliberately cleared widget.
//   - numeric type (Int*, UInt*, Float*, Decimal*) with numeric-shape
//     value → verbatim. Non-numeric value falls through to the
//     quoted-string bucket so CH gives a typed error rather than a
//     silent corruption.
//   - compound type (Array, Tuple, Map) → verbatim. Value shape isn't
//     validated; a malformed literal lets CH surface the parse error.
//   - everything else (String, FixedString, Date, DateTime*, Enum,
//     unknown) → single-quoted with the standard backslash escapes.
func encodeParamLiteral(v, typeExpr string) string {
	if v == "" {
		return "''"
	}
	if isCompoundType(typeExpr) {
		return v
	}
	if isNumericParamType(typeExpr) && isNumericLiteral(v) {
		return v
	}
	return quoteSQLString(v)
}

// quoteSQLString wraps v in single quotes and escapes the standard
// ClickHouse SQL string-literal control characters.
func quoteSQLString(v string) string {
	var b strings.Builder
	b.Grow(len(v) + 2)
	b.WriteByte('\'')
	for i := 0; i < len(v); i++ {
		ch := v[i]
		switch ch {
		case '\\':
			b.WriteString(`\\`)
		case '\'':
			b.WriteString(`\'`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteByte(ch)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

// unwrapNullable strips a `Nullable( ... )` outer wrapper, leaving
// the inner type string for downstream bucket classification.
// Case-insensitive on the keyword; whitespace inside the parens is
// preserved.
func unwrapNullable(t string) string {
	s := strings.TrimSpace(t)
	if len(s) > 10 && strings.EqualFold(s[:9], "nullable(") && s[len(s)-1] == ')' {
		return strings.TrimSpace(s[9 : len(s)-1])
	}
	return s
}

// isNumericParamType reports whether t is a CH integer / float /
// decimal type (after Nullable-unwrap). Family-match — any width /
// precision counts, parameterised forms like `Decimal(18, 4)`
// included. Named with the `Param` infix to disambiguate from the
// Arrow-typed `isNumericType` in play_timeline.go.
func isNumericParamType(t string) bool {
	s := strings.ToLower(unwrapNullable(t))
	return strings.HasPrefix(s, "int") ||
		strings.HasPrefix(s, "uint") ||
		strings.HasPrefix(s, "float") ||
		strings.HasPrefix(s, "decimal")
}

// isCompoundType reports whether t is a CH container type (after
// Nullable-unwrap). Only types whose canonical literal form is a
// bracketed / parenthesised expression CH accepts inside `SET
// param_X = ...`.
func isCompoundType(t string) bool {
	s := strings.ToLower(unwrapNullable(t))
	return strings.HasPrefix(s, "array(") ||
		strings.HasPrefix(s, "tuple(") ||
		strings.HasPrefix(s, "map(")
}

// isDateTimeType reports whether t is a DateTime / DateTime64 column
// type (after Nullable-unwrap). Used by the from/to range widget to
// decide whether to claim a slot pair.
func isDateTimeType(t string) bool {
	s := strings.ToLower(unwrapNullable(t))
	return s == "datetime" ||
		strings.HasPrefix(s, "datetime(") ||
		s == "datetime64" ||
		strings.HasPrefix(s, "datetime64(")
}

// MirrorSync is the result of one recomposeMirror call: the new
// canonical SQL, the new mirror, the new syncedFrom snapshot, and
// the prelude string sliced off for the read-only label render.
// OK=false means ExtractParams failed and the caller should leave
// state untouched and fall back to the unsliced editor.
type MirrorSync struct {
	Canonical  string
	Mirror     string
	SyncedFrom string
	Prelude    string
	OK         bool
}

// recomposeMirror resolves the canonical-vs-mirror state machine
// the hide-prelude editor mode runs each frame. Three transitions
// are possible:
//
//  1. Canonical changed under us (debounce parse refreshed the
//     SET prelude, a widget mutated it, or the toggle just flipped
//     on) — `syncedFrom != residual`. Refresh the mirror; take
//     residual as the new sync point.
//
//  2. Mirror changed (user typed in the residual editor) —
//     `mirror != syncedFrom`. Recompose canonical = prelude +
//     mirror; take mirror as the new sync point.
//
//  3. Steady state — return inputs unchanged.
//
// Canonical-driven refresh takes priority when both could fire on
// the same frame: the canonical buffer is the source of truth, so
// user-typed mirror edits lose to a fresh parse.
//
// Pure function — no PlayApp dependency — so the state machine is
// directly unit-testable. The renderSqlEditor caller owns the
// assignments back into PlayApp fields.
func recomposeMirror(canonical, mirror, syncedFrom string) (out MirrorSync) {
	residual, _, err := ExtractParams(canonical)
	if err != nil {
		out.Canonical = canonical
		out.Mirror = mirror
		out.SyncedFrom = syncedFrom
		return
	}
	out.Prelude = strings.TrimSuffix(canonical, residual)
	out.OK = true

	switch {
	case syncedFrom != residual:
		out.Canonical = canonical
		out.Mirror = residual
		out.SyncedFrom = residual
	case mirror != syncedFrom:
		out.Canonical = out.Prelude + mirror
		out.Mirror = mirror
		out.SyncedFrom = mirror
	default:
		out.Canonical = canonical
		out.Mirror = mirror
		out.SyncedFrom = syncedFrom
	}
	return
}

// isNumericLiteral reports whether v parses as a (signed) integer or
// decimal literal — the shape CH accepts verbatim for numeric param
// values. Scientific / hex / underscore-separated forms are rejected
// conservatively; users with those go through the quoted-string
// bucket and either parse on the CH side or surface a typed error.
func isNumericLiteral(v string) bool {
	if v == "" {
		return false
	}
	i := 0
	if v[0] == '+' || v[0] == '-' {
		i = 1
	}
	if i == len(v) {
		return false
	}
	hasDigit := false
	hasDot := false
	for ; i < len(v); i++ {
		ch := v[i]
		switch {
		case ch >= '0' && ch <= '9':
			hasDigit = true
		case ch == '.' && !hasDot:
			hasDot = true
		default:
			return false
		}
	}
	return hasDigit
}
