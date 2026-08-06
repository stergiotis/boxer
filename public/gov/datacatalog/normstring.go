package datacatalog

import "strings"

// The normalized schema string is the one description an opaque table gets, and
// the only thing a panel shape is matched against (ADR-0170 §SD4). It is a flat
// string rather than a structure so that a shape can be a row in a table: an
// RE2 pattern, storable, servable over the introspection plane, and editable
// without a recompile of anything that consumes it.
//
// The form is columns in system.columns.position order, each rendered
// `name:type;`, with a `;` sentinel at both ends:
//
//	;ts:DateTime64(3);label:String?;value:Float64;
//
// The sentinels are what make anchoring possible without lookahead — `;value:`
// matches a whole column name, where `value:` would also match `myvalue:`. A
// table with no columns is the bare sentinel, `;`.
const (
	// FieldSep separates and terminates columns, and opens the string.
	FieldSep = ';'
	// NameSep separates a column's name from its type.
	NameSep = ':'
	// EscapeChar escapes [FieldSep], [NameSep] and itself.
	EscapeChar = '\\'
)

// NormalizedSchema renders columns as the §SD4 normalized schema string. The
// columns must already be in position order; this function does not reorder
// them, because for an opaque table position order is part of the shape.
//
// It is emitted for leeway tables too, even though shapes are matched against
// opaque ones only: it is the cheapest way for a reader to see a leeway table's
// physical surface next to an opaque one's without a second query.
func NormalizedSchema(columns []ColumnMeta) (s string) {
	// 24 bytes per column is a rough middle for `name:Type;`; the builder grows
	// from there rather than reallocating from nothing.
	var b strings.Builder
	b.Grow(1 + 24*len(columns))
	b.WriteByte(FieldSep)
	for _, c := range columns {
		appendEscaped(&b, c.Name)
		b.WriteByte(NameSep)
		appendEscaped(&b, NormalizeType(c.Type))
		b.WriteByte(FieldSep)
	}
	return b.String()
}

// appendEscaped writes s with the three structural characters backslashed. It
// is applied to the type as well as the name: the ADR's escape rule names
// column names, which is where the characters realistically occur, but an Enum
// literal can carry a `;` and would otherwise split the string into a column
// that does not exist. For every type that contains none of the three — that
// is, every type in practice — the two readings coincide.
func appendEscaped(b *strings.Builder, s string) {
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == FieldSep || ch == NameSep || ch == EscapeChar {
			b.WriteByte(EscapeChar)
		}
		b.WriteByte(ch)
	}
}

// NormalizeType rewrites a ClickHouse type into the form the normalized schema
// string carries: `LowCardinality(…)` is stripped and `Nullable(T)` becomes
// `T?`. Both are storage and null-tracking decisions rather than facts about
// what a column holds, and a shape that had to spell them would have to spell
// every combination of them.
//
// The rewrite is top-level only: `Array(Nullable(String))` is left verbatim.
// Descending would need a real ClickHouse type parser (Enum literals, Map,
// named Tuple), which is a lot of machinery for a case no seed shape needs;
// a shape that cares can spell the inner type as ClickHouse writes it.
func NormalizeType(chType string) (s string) {
	s = strings.TrimSpace(chType)
	for {
		inner, ok := unwrap(s, "LowCardinality")
		if !ok {
			break
		}
		s = strings.TrimSpace(inner)
	}
	inner, ok := unwrap(s, "Nullable")
	if ok {
		return NormalizeType(inner) + "?"
	}
	return
}

// unwrap returns the argument of `ctor(<inner>)` when s is exactly that call.
// The trailing `)` must be the one that closes the opening paren — `Tuple(a
// Nullable(UInt8), b UInt8)` is not a Nullable — so the inner text is scanned
// for balance, honouring single-quoted literals (an Enum value may contain a
// parenthesis).
func unwrap(s string, ctor string) (inner string, ok bool) {
	if !strings.HasPrefix(s, ctor+"(") || !strings.HasSuffix(s, ")") {
		return
	}
	inner = s[len(ctor)+1 : len(s)-1]
	depth := 0
	inQuote := false
	for i := 0; i < len(inner); i++ {
		ch := inner[i]
		if inQuote {
			switch ch {
			case EscapeChar:
				i++
			case '\'':
				inQuote = false
			}
			continue
		}
		switch ch {
		case '\'':
			inQuote = true
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				// The final `)` closed something opened inside inner, so it is
				// not ours: s is a longer expression that merely starts with
				// the constructor.
				return "", false
			}
		}
	}
	if depth != 0 {
		return "", false
	}
	return inner, true
}
